package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// The shared database harness calls this on a fresh, fully migrated database.
func testResponsesLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Value
	mode.Store("ordinary")
	var attempts atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"native-model"}]}`)
			return
		}
		attempts.Add(1)
		var payload map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&payload) != nil || string(payload["model"]) != `"native-model"` || r.Header.Get("Authorization") != "Bearer upstream-secret" {
			t.Error("upstream request lost native model/credential")
			w.WriteHeader(400)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native-model","choices":[{"finish_reason":"stop","message":{"content":"hello"}}]}`)
			return
		}
		if r.URL.Path != "/v1/responses" || string(payload["input"]) != `"sensitive-input"` {
			t.Error("Responses translated or wrong endpoint")
			w.WriteHeader(400)
			return
		}
		switch mode.Load().(string) {
		case "error":
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded","type":"rate_limit_error","message":"upstream-secret","debug":"sensitive-input"}}`)
		case "stream", "cancel":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, responseEventFixture("response.created", "in_progress", "null", 0))
			w.(http.Flusher).Flush()
			if mode.Load().(string) == "cancel" {
				<-r.Context().Done()
				return
			}
			_, _ = io.WriteString(w, responseEventFixture("response.completed", "completed", responsesFinalUsage, 1))
		default:
			_, _ = io.WriteString(w, responseFixture("completed", responsesFinalUsage))
		}
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"responses@example.com","password":"responses-password","name":"Responses"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Native provider", service.CreateConnectionInput{Name: "Responses", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIResponses, CredentialName: "Responses credential", Secret: "upstream-secret"})
	if err != nil {
		t.Fatal(err)
	}
	ready := func(connection service.ConnectionCatalog) entity.ProviderModel {
		credential := connection.Credentials[0].ID
		if _, err := svc.VerifyCredential(ctx, admin.User.ID, credential); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credential, true); err != nil {
			t.Fatal(err)
		}
		var pm entity.ProviderModel
		if err := db.First(&pm, "connection_id = ?", connection.Connection.ID).Error; err != nil {
			t.Fatal(err)
		}
		return pm
	}
	pm := ready(provider.Connections[0])
	model, err := svc.CreateModel(ctx, admin.User.ID, "public-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	chat, err := svc.CreateConnection(ctx, admin.User.ID, provider.Provider.ID, service.CreateConnectionInput{Name: "Chat", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Chat credential", Secret: "upstream-secret"})
	if err != nil {
		t.Fatal(err)
	}
	chatPM := ready(*chat)
	model, err = svc.AddModelBinding(ctx, admin.User.ID, model.Model.ID, chatPM.ID)
	if err != nil {
		t.Fatal(err)
	}
	weights := []service.ModelWeight{}
	for _, binding := range model.Bindings {
		weights = append(weights, service.ModelWeight{BindingID: binding.Binding.ID, Weight: 100})
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, weights); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Native key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, key.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	page, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
	if err != nil {
		t.Fatal(err)
	}
	rates := []pricing.Rate{}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		rates = append(rates, pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: []string{"1", "2", "0.25", "1.5"}[index], Enabled: true})
	}
	page, err = svc.WritePrices(ctx, admin.User.ID, page.ETag, []service.PriceInput{{ProviderModelID: pm.ID, Rates: rates}})
	if err != nil {
		t.Fatal(err)
	}
	originalETag := page.ETag
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	recorderCtx, stop := context.WithCancel(ctx)
	defer stop()
	path := filepath.Join(t.TempDir(), "responses.db")
	if err := svc.StartCallRecorder(recorderCtx, path); err != nil {
		t.Fatal(err)
	}
	stop()
	defer func() { _ = svc.StopCallRecorder() }()
	invoke := func(body string, status int) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectStatus(t, response, status)
		return response
	}
	body := `{"model":"public-model","input":"sensitive-input"}`
	ordinary := invoke(body, 200)
	if strings.Contains(ordinary.Body.String(), "private-model") || !strings.Contains(ordinary.Body.String(), `"model":"public-model"`) {
		t.Fatal("native model was not rewritten")
	}
	listed, err := svc.GatewayModels(ctx, key.Secret)
	if err != nil || len(listed) != 1 || !slices.Equal(listed[0].Protocols, []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses}) {
		t.Fatalf("actual protocols: %+v %v", listed, err)
	}
	mode.Store("stream")
	stream := invoke(strings.TrimSuffix(body, "}")+`,"stream":true}`, 200)
	if !strings.Contains(stream.Body.String(), "response.completed") || strings.Contains(stream.Body.String(), "[DONE]") {
		t.Fatal("native SSE lost")
	}
	mode.Store("error")
	failed := invoke(body, 429)
	if strings.Contains(failed.Body.String(), "upstream-secret") || strings.Contains(failed.Body.String(), "sensitive-input") || !strings.Contains(failed.Body.String(), "rate_limit_exceeded") {
		t.Fatal("unsafe or nonnative error")
	}
	before := attempts.Load()
	rejected := invoke(strings.TrimSuffix(body, "}")+`,"previous_response_id":"resp_foreign"}`, 400)
	if attempts.Load() != before {
		t.Fatal("foreign response reference reached upstream")
	}
	canceled := []*httptest.ResponseRecorder{}
	for _, final := range []bool{false, true} {
		trigger := "response.created"
		mode.Store("cancel")
		if final {
			trigger = "response.completed"
			mode.Store("stream")
		}
		cancelCtx, cancel := context.WithCancel(ctx)
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(strings.TrimSuffix(body, "}")+`,"stream":true}`)).WithContext(cancelCtx)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(&cancelResponseWriter{recorder, cancel, trigger}, req)
		cancel()
		canceled = append(canceled, recorder)
	}
	if _, err := svc.SetProviderModelState(ctx, admin.User.ID, pm.ID, pm.ETag, false); err != nil {
		t.Fatal(err)
	}
	listed, err = svc.GatewayModels(ctx, key.Secret)
	if err != nil || len(listed) != 1 || !slices.Equal(listed[0].Protocols, []string{entity.ProtocolOpenAIChat}) {
		t.Fatalf("disabled supply still advertised: %+v %v", listed, err)
	}
	mode.Store("ordinary")
	blocked := invoke(body, 503)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"public-model","messages":[{"role":"user","content":"test"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key.Secret)
	chatReply := httptest.NewRecorder()
	router.ServeHTTP(chatReply, req)
	expectStatus(t, chatReply, 200)
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	replayCtx, stopReplay := context.WithCancel(ctx)
	defer stopReplay()
	if err := restarted.StartCallRecorder(replayCtx, path); err != nil {
		t.Fatal(err)
	}
	stopReplay()
	defer func() { _ = restarted.StopCallRecorder() }()
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		response *httptest.ResponseRecorder
		status   string
		priced   bool
		attempts int64
	}{{ordinary, "success", true, 1}, {stream, "success", true, 1}, {failed, "error", false, 1}, {rejected, "error", false, 0}, {canceled[0], "canceled", false, 1}, {canceled[1], "canceled", true, 1}, {blocked, "error", false, 0}}
	for _, item := range cases {
		var fact entity.CallRecord
		requestID := item.response.Header().Get("X-Request-ID")
		if err := db.First(&fact, "request_id = ?", requestID).Error; err != nil {
			t.Fatal(err)
		}
		if fact.Protocol != entity.ProtocolOpenAIResponses || fact.Status != item.status || (fact.ChargeAmount != nil) != item.priced {
			t.Fatalf("native call fact %+v", fact)
		}
		if item.priced && (*fact.ChargeAmount != "0.000017" || fact.PriceETag != originalETag) {
			t.Fatalf("immutable native price: %+v", fact.CallPricingFields)
		}
		var count int64
		if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", requestID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("duplicated call fact")
		}
		if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", requestID).Count(&count).Error; err != nil || count != item.attempts {
			t.Fatalf("attempt count %d expected %d", count, item.attempts)
		}
	}
	reportReply := identityRequest(router, "GET", "/api/v1/usage?protocol=openai_responses", "", cookie, "")
	expectStatus(t, reportReply, 200)
	var report service.UsageReport
	if err := json.Unmarshal(reportReply.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	stats := report.Current.Summary
	if stats.Requests != 7 || stats.Successes != 2 || stats.Canceled != 2 || stats.Errors != 3 || len(stats.Amounts) != 1 || stats.Amounts[0].Currency != "USD" || stats.Amounts[0].Amount != "0.000051" || stats.Amounts[0].Calls != 3 {
		t.Fatalf("native usage includes chat or loses native pricing: %+v", stats)
	}

}
