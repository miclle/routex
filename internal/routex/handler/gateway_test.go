package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
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
	store, err := secretstore.New(bytes.Repeat([]byte{17}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Value
	mode.Store("ordinary")
	var expectedCredential atomic.Value
	expectedCredential.Store("Bearer upstream-test-secret")
	var chatCalls atomic.Int32
	canceled := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" && r.Header.Get("Authorization") != expectedCredential.Load().(string) {
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
		if !strings.HasPrefix(r.Header.Get("X-Request-ID"), "req_") {
			t.Error("request ID missing upstream")
		}
		switch mode.Load().(string) {
		case "error":
			w.Header().Set("Set-Cookie", "provider-secret=secret")
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"upstream-test-secret"}}`)
		case "stream", "cancel":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"id\":\"chat-1\",\"model\":\"provider-model\",\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
			w.(http.Flusher).Flush()
			if mode.Load().(string) == "cancel" {
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
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	admin, err := svc.Initialize(ctx, "gateway@example.invalid", "test-only-gateway-password", "Gateway Test")
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
	request := func(method, path, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	body := `{"model":"public-model","messages":[{"role":"user","content":"Hello"}],"temperature":0.125}`
	list := request("GET", "/v1/models", "", created.Secret)
	expectStatus(t, list, 200)
	if !strings.Contains(list.Body.String(), "public-model") {
		t.Fatal("authorized model missing")
	}
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
