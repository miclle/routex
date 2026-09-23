package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

// Called only by the shared fresh-database integration harness.
func testGeminiLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{84}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var mode atomic.Value
	mode.Store("ordinary")
	var attempts atomic.Int64
	var discoveryFailure atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("x-goog-api-key") != "upstream-secret" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("key") != "" {
			t.Error("credential transport wrong")
		}
		if r.URL.Path == "/v1beta/models" {
			if discoveryFailure.Load() {
				w.WriteHeader(401)
				return
			}
			_, _ = io.WriteString(w, `{"models":[{"name":"models/native-model","supportedGenerationMethods":["generateContent"]},{"name":"models/embed","supportedGenerationMethods":["embedContent"]}]}`)
			return
		}
		attempts.Add(1)
		var payload map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&payload) != nil || payload["contents"] == nil || payload["model"] != nil || payload["stream"] != nil || !strings.HasPrefix(r.URL.Path, "/v1beta/models/native-model:") {
			t.Error("native request translated")
			w.WriteHeader(400)
			return
		}
		switch mode.Load().(string) {
		case "error":
			w.WriteHeader(429)
			_, _ = io.WriteString(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"upstream-secret","details":["sensitive-input"]}}`)
		case "stream", "cancel":
			if r.URL.Query().Get("alt") != "sse" {
				t.Error("SSE transport missing")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			if mode.Load().(string) == "cancel" {
				_, _ = io.WriteString(w, `data: {"candidates":[{"index":0,"content":{"parts":[{"text":"partial"}]}}]}`+"\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			_, _ = io.WriteString(w, "data: "+geminiFixture()+"\n\n")
		default:
			_, _ = io.WriteString(w, geminiFixture())
		}
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"gemini@example.com","password":"gemini-password","name":"Gemini"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Native provider", service.CreateConnectionInput{Name: "Gemini", BaseURL: upstream.URL + "/v1beta", Protocol: entity.ProtocolGeminiGenerateContent, CredentialName: "Gemini credential", Secret: "upstream-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	verification, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID)
	if err != nil || !verification.Verified || verification.DiscoveredModels != 1 {
		t.Fatal("native discovery", err)
	}
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
	path := filepath.Join(t.TempDir(), "gemini.db")
	if err := svc.StartCallRecorder(recorderCtx, path); err != nil {
		stop()
		t.Fatal(err)
	}
	stop()
	defer func() { _ = svc.StopCallRecorder() }()
	body := `{"contents":[{"role":"user","parts":[{"text":"sensitive-input"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":100}}}`
	invoke := func(body, action string, status int) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1beta/models/public-model:"+action+"?key="+key.Secret, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectStatus(t, response, status)
		return response
	}
	ordinary := invoke(body, "generateContent", 200)
	if strings.Contains(ordinary.Body.String(), "private-model") || !strings.Contains(ordinary.Body.String(), `"modelVersion":"public-model"`) {
		t.Fatal("native model identity leaked")
	}
	mode.Store("stream")
	stream := invoke(body, "streamGenerateContent", 200)
	if !strings.Contains(stream.Body.String(), "data:") || strings.Contains(stream.Body.String(), "[DONE]") {
		t.Fatal("native SSE changed")
	}
	mode.Store("error")
	failed := invoke(body, "generateContent", 429)
	if strings.Contains(failed.Body.String(), "upstream-secret") || strings.Contains(failed.Body.String(), "sensitive-input") || !strings.Contains(failed.Body.String(), "RESOURCE_EXHAUSTED") {
		t.Fatal("native error leaked")
	}
	before := attempts.Load()
	rejected := invoke(strings.TrimSuffix(body, "}")+`,"cached_content":"cachedContents/foreign"}`, "generateContent", 400)
	if attempts.Load() != before {
		t.Fatal("foreign resource reached upstream")
	}
	mode.Store("cancel")
	cancelCtx, cancel := context.WithCancel(ctx)
	req := httptest.NewRequest("POST", "/v1beta/models/public-model:streamGenerateContent", strings.NewReader(body)).WithContext(cancelCtx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", key.Secret)
	canceled := httptest.NewRecorder()
	router.ServeHTTP(&cancelResponseWriter{canceled, cancel, "partial"}, req)
	cancel()
	if _, err := svc.SetProviderModelState(ctx, admin.User.ID, pm.ID, pm.ETag, false); err != nil {
		t.Fatal(err)
	}
	blocked := invoke(body, "generateContent", 503)
	discoveryFailure.Store(true)
	verification, err = svc.VerifyCredential(ctx, admin.User.ID, credentialID)
	if err != nil || verification.Verified {
		t.Fatal("failed verification accepted", err)
	}
	var credential entity.ProviderCredential
	var evidence int64
	if err := db.First(&credential, "id = ?", credentialID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.CredentialModelAccess{}).Where("credential_id = ?", credentialID).Count(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if credential.Enabled || credential.VerificationStatus != "failed" || evidence != 1 {
		t.Fatal("failed reverify authorization/evidence")
	}
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	replayCtx, stopReplay := context.WithCancel(ctx)
	if err := restarted.StartCallRecorder(replayCtx, path); err != nil {
		stopReplay()
		t.Fatal(err)
	}
	stopReplay()
	defer func() { _ = restarted.StopCallRecorder() }()
	for i := 0; i < 2; i++ {
		if err := restarted.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		response *httptest.ResponseRecorder
		status   string
		priced   bool
		attempts int64
	}{{ordinary, "success", true, 1}, {stream, "success", true, 1}, {failed, "error", false, 1}, {rejected, "error", false, 0}, {canceled, "canceled", false, 1}, {blocked, "error", false, 0}} {
		requestID := item.response.Header().Get("X-Request-ID")
		var fact entity.CallRecord
		if err := db.First(&fact, "request_id = ?", requestID).Error; err != nil {
			t.Fatal(err)
		}
		if fact.Protocol != entity.ProtocolGeminiGenerateContent || fact.Status != item.status || (fact.ChargeAmount != nil) != item.priced {
			t.Fatalf("native call fact %+v", fact)
		}
		if item.priced && (*fact.ChargeAmount != "0.000017" || fact.PriceETag != originalETag) {
			t.Fatalf("native price %+v", fact.CallPricingFields)
		}
		var count int64
		if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", requestID).Count(&count).Error; err != nil || count != 1 {
			t.Fatal("duplicate call fact")
		}
		if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", requestID).Count(&count).Error; err != nil || count != item.attempts {
			t.Fatal("wrong attempt count")
		}
	}
	reportReply := identityRequest(router, "GET", "/api/v1/usage?protocol=gemini_generate_content", "", cookie, "")
	expectStatus(t, reportReply, 200)
	var report service.UsageReport
	if err := json.Unmarshal(reportReply.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	summary := report.Current.Summary
	if summary.Requests != 6 || summary.Successes != 2 || summary.Canceled != 1 || summary.Errors != 3 || len(summary.Amounts) != 1 || summary.Amounts[0].Amount != "0.000034" || summary.Amounts[0].Calls != 2 {
		t.Fatalf("native usage report %+v", summary)
	}
}
