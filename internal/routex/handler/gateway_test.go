package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/secretstore"
)

func TestGatewayPayloadSanitization(t *testing.T) {
	for _, raw := range []string{`null`, `{`, `{"error":{"message":"provider-secret"}}`} {
		if _, err := rewriteGatewayModel([]byte(raw), "public"); err == nil {
			t.Fatal("invalid upstream payload accepted")
		}
	}
	got, err := rewriteGatewayModel([]byte(`{"model":"upstream-private","choices":[],"usage":{"prompt_tokens":2}}`), "public")
	if err != nil || strings.Contains(string(got), "upstream-private") || !strings.Contains(string(got), "prompt_tokens") {
		t.Fatalf("response rewrite failed: %v", err)
	}
	_, public := gatewayErrorBody(errors.New("provider-secret"))
	encoded, err := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), "provider-secret") {
		t.Fatal("raw error leaked")
	}
}

func TestGatewayStreamSafety(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		failure      bool
	}{
		{"complete", ": keepalive\n\ndata: {\"model\":\"private-model\",\"choices\":[]}\n\ndata: [DONE]\n\n", false},
		{"truncated", "data: {\"choices\":[]}\n\n", true},
		{"error", "data: {\"error\":{\"message\":\"provider-secret\"}}\n\n", true},
		{"invalid", "data: not-json\n\n", true},
		{"large", "data: " + strings.Repeat("x", gatewayEventLimit) + "\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			_, streamErr := proxyGatewayStream(context.Background(), writer, strings.NewReader(tc.stream), "public")
			if (streamErr != nil) != tc.failure {
				t.Fatal("incorrect stream completion status")
			}
			text := writer.Body.String()
			if strings.Contains(text, "provider-secret") || strings.Contains(text, "private-model") {
				t.Fatal("private upstream details leaked")
			}
			if strings.Contains(text, `"error"`) != tc.failure {
				t.Fatalf("stream failure contract: %s", text)
			}
			if !tc.failure && !strings.Contains(text, "[DONE]") {
				t.Fatal("terminal event missing")
			}
		})
	}
}

func testGatewayLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	storageServer, storageFixture := newStorageFixture(t)
	store, err := secretstore.New(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Value
	mode.Store("ordinary")
	var expectedCredential atomic.Value
	expectedCredential.Store("Bearer upstream-test-secret")
	var chatCalls atomic.Int32
	var retryCalls atomic.Int32
	canceled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		activeMode := mode.Load().(string)
		multiRouteMode := activeMode == "retry" || activeMode == "stream_incomplete" || activeMode == "stream_empty"
		if r.URL.Path != "/v1/models" && !multiRouteMode && r.Header.Get("Authorization") != expectedCredential.Load().(string) {
			t.Error("wrong upstream credential priority")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"provider-model"}]}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("incorrect upstream path")
			w.WriteHeader(404)
			return
		}
		chatCalls.Add(1)
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if string(payload["model"]) != `"provider-model"` {
			t.Error("public model was not routed")
		}
		if mode.Load().(string) == "attachment" {
			encoded, _ := json.Marshal(payload)
			if strings.Count(string(encoded), "data:application/pdf;base64,") != 2 || strings.Contains(string(encoded), "routex://attachments/") {
				t.Errorf("attachment references were not rewritten exactly once per occurrence: %s", encoded)
			}
		}
		if !strings.HasPrefix(r.Header.Get("X-Request-ID"), "req_") {
			t.Error("request ID missing upstream")
		}
		switch activeMode {
		case "error":
			w.Header().Set("Set-Cookie", "provider-secret=secret")
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"upstream-test-secret"}}`)
		case "retry":
			if retryCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"id":"chat-1","object":"chat.completion","model":"provider-model","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
		case "stream", "cancel", "stream_incomplete", "stream_empty":
			w.Header().Set("Content-Type", "text/event-stream")
			if activeMode == "stream_empty" {
				return
			}
			_, _ = io.WriteString(w, "data: {\"id\":\"chat-1\",\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
			w.(http.Flusher).Flush()
			if activeMode == "stream_incomplete" {
				return
			}
			if activeMode == "cancel" {
				<-r.Context().Done()
				canceled <- struct{}{}
				return
			}
			_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n")
		default:
			_, _ = io.WriteString(w, `{"id":"chat-1","object":"chat.completion","model":"provider-model","choices":[{"message":{"role":"assistant","content":"Hello"}}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
		}
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithStoragePolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "gateway@example.invalid", "test-only-gateway-password", "Gateway Test")
	if err != nil {
		t.Fatal(err)
	}
	storage, err := svc.StorageSettings(ctx, admin.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WriteStorageSettings(ctx, admin.User.ID, service.StorageInput{
		Enabled:  true,
		Endpoint: storageServer.URL,
		Region:   "us-east-1",
		Bucket:   "routex-test",
		Prefix:   "gateway",
		ETag:     storage.ETag,
		Auth: service.StorageAuthInput{
			Action:    "replace",
			AccessKey: "test-only-access",
			SecretKey: "test-only-secret",
		},
	}); err != nil {
		t.Fatal(err)
	}
	attachment, err := svc.UploadAttachment(ctx, admin.User.ID, "request.pdf", []byte("%PDF-1.7\ncontrolled attachment\n%%EOF"))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Gateway Provider", service.CreateConnectionInput{Name: "Primary", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Primary", Secret: "upstream-test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	verified, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID)
	if err != nil {
		t.Fatal(err)
	}
	_ = verified
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.First(&pm, "connection_id = ?", provider.Connections[0].Connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&pm).Updates(map[string]any{"supports_image_input": true, "supports_pdf_input": true}).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "public-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Gateway Key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, created.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	expectedSnapshots := map[string]string{}
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		snapshotID := svc.RuntimeStatus().SnapshotID
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectedSnapshots[response.Header().Get("X-Request-ID")] = snapshotID
		return response
	}
	body := `{"model":"public-model","messages":[{"role":"user","content":"Hello"}],"temperature":0.125}`
	list := request("GET", "/v1/models", "", created.Secret)
	expectStatus(t, list, 200)
	var modelList struct {
		Data []service.GatewayModel `json:"data"`
	}
	if json.Unmarshal(list.Body.Bytes(), &modelList) != nil || len(modelList.Data) != 1 || modelList.Data[0].ID != "public-model" {
		t.Fatal("authorized model missing")
	}
	if capabilities := modelList.Data[0].InputCapabilities[entity.ProtocolOpenAIChat]; len(capabilities) != 2 || capabilities[0] != "image" || capabilities[1] != "pdf" {
		t.Fatalf("input capabilities = %v, want stable image and pdf order", modelList.Data[0].InputCapabilities)
	}
	if !modelList.Data[0].PersonalAttachments {
		t.Fatal("personal Key model did not advertise session-owned attachment references")
	}
	storageFixture.mu.Lock()
	attachmentReads := storageFixture.getCalls
	storageFixture.mu.Unlock()
	mode.Store("attachment")
	attachmentBody := `{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"summarize"},{"type":"file","file":{"file_data":"routex://attachments/` + attachment.ID + `"}},{"type":"file","file":{"file_data":"routex://attachments/` + attachment.ID + `"}}]}]}`
	expectStatus(t, request("POST", "/v1/chat/completions", attachmentBody, created.Secret), 200)
	storageFixture.mu.Lock()
	if storageFixture.getCalls != attachmentReads+1 {
		t.Fatalf("duplicate attachment caused %d storage reads, want one", storageFixture.getCalls-attachmentReads)
	}
	storageFixture.mu.Unlock()
	mode.Store("ordinary")
	expectStatus(t, request("GET", "/v1/models", "", ""), 401)
	ordinary := request("POST", "/v1/chat/completions", body, created.Secret)
	expectStatus(t, ordinary, 200)
	if !strings.HasPrefix(ordinary.Header().Get("X-Request-ID"), "req_") || strings.Contains(ordinary.Body.String(), "provider-model") || !strings.Contains(ordinary.Body.String(), "public-model") {
		t.Fatal("public response identity incorrect")
	}
	var ordinaryFact entity.CallRecord
	if err := db.First(&ordinaryFact, "request_id = ?", ordinary.Header().Get("X-Request-ID")).Error; err != nil {
		t.Fatal(err)
	}
	if ordinaryFact.Status != "success" || ordinaryFact.InputTokens == nil || *ordinaryFact.InputTokens != 3 || ordinaryFact.OutputTokens == nil || *ordinaryFact.OutputTokens != 2 {
		t.Fatal("ordinary usage fact was not persisted")
	}
	assertNativeAttemptAttribution(t, db, ordinaryFact.RequestID, credentialID, "", "success", 1)
	testProviderModelState(t, db, router, store, pm, body, created.Secret, &chatCalls)
	mode.Store("stream")
	streamed := request("POST", "/v1/chat/completions", strings.Replace(body, `"temperature":0.125`, `"stream":true`, 1), created.Secret)
	expectStatus(t, streamed, 200)
	if !strings.Contains(streamed.Body.String(), "[DONE]") || !strings.Contains(streamed.Body.String(), "Hello") {
		t.Fatal("stream incomplete")
	}
	var streamFact entity.CallRecord
	if err := db.First(&streamFact, "request_id = ?", streamed.Header().Get("X-Request-ID")).Error; err != nil {
		t.Fatal(err)
	}
	if streamFact.Status != "success" || !streamFact.Stream || streamFact.InputTokens == nil || *streamFact.InputTokens != 3 || streamFact.OutputTokens == nil || *streamFact.OutputTokens != 2 {
		t.Fatal("stream usage fact was not persisted")
	}
	assertNativeAttemptAttribution(t, db, streamFact.RequestID, credentialID, "", "success", 1)
	mode.Store("error")
	beforeFailure := chatCalls.Load()
	failure := request("POST", "/v1/chat/completions", body, created.Secret)
	expectStatus(t, failure, 502)
	if chatCalls.Load() != beforeFailure+1 {
		t.Fatal("failed upstream request was retried")
	}
	if strings.Contains(failure.Body.String(), "upstream-test-secret") || failure.Header().Get("Set-Cookie") != "" {
		t.Fatal("upstream secret or headers leaked")
	}
	mode.Store("ordinary")
	secondary, err := svc.CreateCredential(ctx, admin.User.ID, provider.Connections[0].Connection.ID, "Secondary", "secondary-test-secret", 5)
	if err != nil {
		t.Fatal(err)
	}
	if verified, err := svc.VerifyCredential(ctx, admin.User.ID, secondary.ID); err != nil || !verified.Verified {
		t.Fatal("secondary credential verification failed")
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, secondary.ID, true); err != nil {
		t.Fatal(err)
	}
	// The first credential retains priority while both are eligible.
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 200)
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, false); err != nil {
		t.Fatal(err)
	}
	expectedCredential.Store("Bearer secondary-test-secret")
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 200)
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, secondary.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	expectedCredential.Store("Bearer upstream-test-secret")
	aliasExpiry := time.Now().Add(time.Hour)
	if _, err := svc.RenameModel(ctx, admin.User.ID, model.Model.ID, "renamed-model", &aliasExpiry); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 200)
	if err := db.Model(&entity.ModelName{}).Where("name = ?", "public-model").Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 404)
	body = strings.Replace(body, "public-model", "renamed-model", 1)

	mode.Store("cancel")
	gateway := httptest.NewServer(router)
	defer gateway.Close()
	cancelCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(cancelCtx, "POST", gateway.URL+"/v1/chat/completions", strings.NewReader(strings.Replace(body, `"temperature":0.125`, `"stream":true`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+created.Secret)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	one := make([]byte, 1)
	if _, err := response.Body.Read(one); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	if err := response.Body.Close(); err != nil {
		t.Error(err)
	}
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("client cancellation did not cancel upstream")
	}
	mode.Store("ordinary")
	deadline := time.Now().Add(3 * time.Second)
	for {
		var canceledFact entity.CallRecord
		err := db.First(&canceledFact, "request_id = ?", response.Header.Get("X-Request-ID")).Error
		if err == nil {
			if canceledFact.Status != "canceled" || canceledFact.InputTokens != nil || canceledFact.OutputTokens != nil {
				t.Fatal("canceled request should retain unknown usage")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled request fact was not persisted")
		}
		time.Sleep(10 * time.Millisecond)
	}
	retryProvider, err := svc.CreateProvider(ctx, admin.User.ID, "Retry Provider", service.CreateConnectionInput{Name: "Retry", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Retry", Secret: "retry-upstream-secret"})
	if err != nil {
		t.Fatal(err)
	}
	retryCredentialID := retryProvider.Connections[0].Credentials[0].ID
	if verified, err := svc.VerifyCredential(ctx, admin.User.ID, retryCredentialID); err != nil || !verified.Verified {
		t.Fatal("retry credential verification failed")
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, retryCredentialID, true); err != nil {
		t.Fatal(err)
	}
	var retryProviderModel entity.ProviderModel
	if err := db.First(&retryProviderModel, "connection_id = ?", retryProvider.Connections[0].Connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	withRetry, err := svc.AddModelBinding(ctx, admin.User.ID, model.Model.ID, retryProviderModel.ID)
	if err != nil {
		t.Fatal(err)
	}
	primaryBindingID, retryBindingID := "", ""
	for _, binding := range withRetry.Bindings {
		if binding.Binding.ProviderModelID == pm.ID {
			primaryBindingID = binding.Binding.ID
		}
		if binding.Binding.ProviderModelID == retryProviderModel.ID {
			retryBindingID = binding.Binding.ID
		}
	}
	if primaryBindingID == "" || retryBindingID == "" {
		t.Fatal("retry bindings missing")
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: primaryBindingID, Weight: 50}, {BindingID: retryBindingID, Weight: 50}}); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	mode.Store("retry")
	retryCalls.Store(0)
	beforeRetry := chatCalls.Load()
	retried := request("POST", "/v1/chat/completions", body, created.Secret)
	expectStatus(t, retried, http.StatusOK)
	if chatCalls.Load() != beforeRetry+2 {
		t.Fatalf("retry dispatches = %d, want 2", chatCalls.Load()-beforeRetry)
	}
	var retryRecord entity.CallRecord
	if err := db.First(&retryRecord, "request_id = ?", retried.Header().Get("X-Request-ID")).Error; err != nil {
		t.Fatal(err)
	}
	var retryAttempts []entity.CallAttempt
	if err := db.Where("request_id = ?", retryRecord.RequestID).Order("attempt_number").Find(&retryAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if retryRecord.RouteStopReason != "succeeded" || len(retryAttempts) != 2 || retryAttempts[0].FailureClass != "rate_limited" || retryAttempts[0].WorkEvidence != "rejected_without_work" || retryAttempts[1].FailureClass != "success" || retryAttempts[1].WorkEvidence != "completed" {
		t.Fatalf("retry diagnostics: record=%+v attempts=%+v", retryRecord, retryAttempts)
	}
	var finalConnection entity.ProviderConnection
	if err := db.First(&finalConnection, "id = ?", retryAttempts[1].ConnectionID).Error; err != nil {
		t.Fatal(err)
	}
	var finalProvider entity.Provider
	if err := db.First(&finalProvider, "id = ?", finalConnection.ProviderID).Error; err != nil {
		t.Fatal(err)
	}
	var finalProviderModel entity.ProviderModel
	if err := db.First(&finalProviderModel, "connection_id = ?", finalConnection.ID).Error; err != nil {
		t.Fatal(err)
	}
	if retryRecord.ConnectionID != retryAttempts[1].ConnectionID || retryRecord.ProviderID != finalProvider.ID || retryRecord.ProviderName != finalProvider.Name || retryRecord.ConnectionName != finalConnection.Name || retryRecord.UpstreamModelName != finalProviderModel.UpstreamName {
		t.Fatalf("retry record did not retain the final successful route: record=%+v attempts=%+v", retryRecord, retryAttempts)
	}
	credentialByConnection := map[string]string{provider.Connections[0].Connection.ID: credentialID, retryProvider.Connections[0].Connection.ID: retryCredentialID}
	for _, attempt := range retryAttempts {
		if attempt.CredentialID != credentialByConnection[attempt.ConnectionID] || attempt.SnapshotID != expectedSnapshots[retryRecord.RequestID] || attempt.SnapshotID == "" {
			t.Fatalf("retry attempt lost its exact credential/configuration: %+v", attempt)
		}
	}
	if retryAttempts[0].CredentialID == retryAttempts[1].CredentialID {
		t.Fatal("cross-Provider retry merged distinct credential identities")
	}
	for _, streamMode := range []string{"stream_empty", "stream_incomplete"} {
		mode.Store(streamMode)
		beforeStreamFailure := chatCalls.Load()
		failedStream := request("POST", "/v1/chat/completions", strings.Replace(body, `"temperature":0.125`, `"stream":true`, 1), created.Secret)
		expectStatus(t, failedStream, http.StatusOK)
		if chatCalls.Load() != beforeStreamFailure+1 {
			t.Fatalf("%s dispatched a fallback after 2xx", streamMode)
		}
		var failedRecord entity.CallRecord
		if err := db.First(&failedRecord, "request_id = ?", failedStream.Header().Get("X-Request-ID")).Error; err != nil {
			t.Fatal(err)
		}
		var failedAttempts []entity.CallAttempt
		if err := db.Where("request_id = ?", failedRecord.RequestID).Find(&failedAttempts).Error; err != nil {
			t.Fatal(err)
		}
		assertNativeAttemptAttribution(t, db, failedRecord.RequestID, credentialByConnection[failedRecord.ConnectionID], expectedSnapshots[failedRecord.RequestID], "error", 1)
		if failedRecord.RouteStopReason != "unsafe_to_replay" || len(failedAttempts) != 1 || failedAttempts[0].WorkEvidence != "unknown" || !failedAttempts[0].OutputStarted {
			t.Fatalf("%s diagnostics: record=%+v attempts=%+v", streamMode, failedRecord, failedAttempts)
		}
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: primaryBindingID, Weight: 100}, {BindingID: retryBindingID, Weight: 0}}); err != nil {
		t.Fatal(err)
	}
	mode.Store("ordinary")
	before := chatCalls.Load()
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, false); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 503)
	if chatCalls.Load() != before {
		t.Fatal("disabled credential reached upstream")
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, model.Model.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 404)
	if chatCalls.Load() != before {
		t.Fatal("revoked grant reached upstream")
	}
	if err := svc.RevokePersonalKey(ctx, admin.User.ID, created.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("POST", "/v1/chat/completions", body, created.Secret), 401)
	testDirectGatewayRecorderCheckpoint(t, db, admin.User.ID, store)
}

func TestGatewayUsageUnknownAndBearerParsing(t *testing.T) {
	for _, raw := range []string{`{}`, `{"usage":null}`, `{"usage":{"prompt_tokens":-1,"completion_tokens":-2}}`, `{"usage":{"prompt_tokens":0.5}}`} {
		usage := parseGatewayUsage([]byte(raw))
		if usage.Input != nil || usage.Output != nil {
			t.Fatal("missing or invalid usage became a numeric fact")
		}
	}
	usage := parseGatewayUsage([]byte(`{"usage":{"prompt_tokens":0,"completion_tokens":2}}`))
	if usage.Input == nil || *usage.Input != 0 || usage.Output == nil || *usage.Output != 2 {
		t.Fatal("explicit zero usage was lost")
	}
	for _, header := range []string{"", "Bearer", "Basic value", "Bearer one two"} {
		req := httptest.NewRequest("GET", "/v1/models", nil)
		req.Header.Set("Authorization", header)
		if gatewayBearer(req) != "" {
			t.Fatal("malformed bearer accepted")
		}
	}
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Add("Authorization", "Bearer first")
	req.Header.Add("Authorization", "Bearer second")
	if gatewayBearer(req) != "" {
		t.Fatal("ambiguous authorization headers accepted")
	}
}

func assertNativeAttemptAttribution(t *testing.T, db *gorm.DB, requestID, credentialID, snapshotID, status string, count int) {
	t.Helper()
	var attempts []entity.CallAttempt
	if err := db.Where("request_id = ?", requestID).Order("attempt_number").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(attempts) != count {
		t.Fatalf("native attempt count = %d, want %d", len(attempts), count)
	}
	for _, attempt := range attempts {
		if credentialID == "" || attempt.CredentialID != credentialID || attempt.SnapshotID != snapshotID || attempt.Status != status {
			t.Fatalf("native attempt lost exact attribution or terminal status: %+v; want credential=%s snapshot=%s status=%s", attempt, credentialID, snapshotID, status)
		}
		if (attempt.FailureClass == "success") != (status == "success") || status == "success" && attempt.WorkEvidence != "completed" {
			t.Fatalf("accepted HTTP response became false terminal success: %+v", attempt)
		}
	}
}

func testDirectGatewayRecorderCheckpoint(t *testing.T, db *gorm.DB, actorID string, store *secretstore.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"direct-upstream"}]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer direct-fixture-secret" {
			t.Error("direct dispatch lost the exact reviewed credential")
		}
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	defer unblock()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	provider, err := svc.CreateProvider(ctx, actorID, "Direct journal provider", service.CreateConnectionInput{Name: "Direct", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Direct credential", Secret: "direct-fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	if verified, err := svc.VerifyCredential(ctx, actorID, credentialID); err != nil || !verified.Verified {
		t.Fatalf("direct fixture verification = %+v, error = %v", verified, err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, actorID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.First(&pm, "connection_id = ?", provider.Connections[0].Connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, actorID, "direct-journal-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, actorID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, actorID, "Direct journal key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, actorID, key.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "direct-journal.db")
	recorderCtx, stop := context.WithCancel(ctx)
	if err := svc.StartCallRecorder(recorderCtx, path); err != nil {
		stop()
		t.Fatal(err)
	}
	stop()
	defer func() { _ = svc.StopCallRecorder() }()
	if svc.RuntimeStatus().Enabled {
		t.Fatal("compatibility fixture unexpectedly started runtime publication")
	}
	done := make(chan error, 1)
	go func() {
		result, err := svc.GatewayChat(ctx, key.Secret, []byte(`{"model":"direct-journal-model","messages":[{"role":"user","content":"fixture"}]}`), "req_direct_interrupted")
		if result != nil && result.Response != nil {
			_ = result.Response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("direct dispatch ended before controlled upstream: %v", err)
	case <-ctx.Done():
		t.Fatal("direct dispatch did not enter controlled upstream")
	}
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	queue, err := eventqueue.Open(path, 8, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := queue.Read(8)
	if err != nil || len(entries) != 1 {
		_ = queue.Close()
		t.Fatalf("direct DB interruption recovery = %+v, error = %v", entries, err)
	}
	var recovered service.CallFact
	if err := json.Unmarshal(entries[0].Payload, &recovered); err != nil {
		_ = queue.Close()
		t.Fatal(err)
	}
	if len(recovered.Attempts) != 1 || recovered.Attempts[0].CredentialID != credentialID || recovered.Attempts[0].SnapshotID != "" || recovered.Attempts[0].Status != "error" || recovered.Attempts[0].WorkEvidence != "unknown" || recovered.Attempts[0].ErrorCode != "process_interrupted" {
		_ = queue.Close()
		t.Fatalf("direct DB dispatch lost exact attempt or invented applied configuration: %+v", recovered)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartCallRecorder(ctx, path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.StopCallRecorder() }()
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.GetCall(ctx, "", "req_direct_interrupted")
	if err != nil || len(stored.Attempts) != 1 || stored.Attempts[0].CredentialID != credentialID || stored.Attempts[0].SnapshotID != "" || stored.Attempts[0].Status != "error" {
		t.Fatalf("direct interrupted attempt did not survive replay: %+v, error = %v", stored, err)
	}
}
