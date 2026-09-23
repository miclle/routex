package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// The shared database harness calls this on a fresh, fully migrated database.
func testCallPricingLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{63}, 32))
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"native-text"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"native-text","choices":[{"finish_reason":"stop","message":{"content":"hello"}}],"usage":{"prompt_tokens":1000000,"completion_tokens":500000,"prompt_tokens_details":{"cached_tokens":200000,"cache_write_tokens":100000}}}`)
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"call-price@example.com","password":"call-price-password","name":"Price calls"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Price provider", service.CreateConnectionInput{Name: "Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Credential", Secret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credential := provider.Connections[0].Credentials[0].ID
	if _, err := svc.VerifyCredential(ctx, admin.User.ID, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credential, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.First(&pm, "connection_id = ?", provider.Connections[0].Connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "metered-text", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Metered key", []string{model.Model.ID}, nil)
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
	page, err = svc.WritePricingCurrency(ctx, admin.User.ID, page.ETag, pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "7"}})
	if err != nil {
		t.Fatal(err)
	}
	rates := []pricing.Rate{}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		rates = append(rates, pricing.Rate{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: []string{"2", "4", "0.5", "3"}[index], Enabled: true})
	}
	page, err = svc.WritePrices(ctx, admin.User.ID, page.ETag, []service.PriceInput{{ProviderModelID: pm.ID, Rates: rates}})
	if err != nil {
		t.Fatal(err)
	}
	initialETag := page.ETag
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	// Pause automatic delivery, not journal admission/completion. This simulates
	// database delivery lag while catalogue edits and a process restart occur.
	stopped, stop := context.WithCancel(ctx)
	stop()
	path := filepath.Join(t.TempDir(), "pricing-journal.db")
	if err := svc.StartCallRecorder(stopped, path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.StopCallRecorder() }()
	invoke := func(extra string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"metered-text","messages":[{"role":"user","content":"test"}]`+extra+`}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key.Secret)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectStatus(t, response, 200)
		return response
	}
	first := invoke("")
	var before int64
	if err := db.Model(&entity.CallRecord{}).Count(&before).Error; err != nil || before != 0 {
		t.Fatal("paused recorder unexpectedly delivered")
	}
	rates[0].Amount = "3"
	page, err = svc.WritePrices(ctx, admin.User.ID, page.ETag, []service.PriceInput{{ProviderModelID: pm.ID, Rates: rates}})
	if err != nil {
		t.Fatal(err)
	}
	second := invoke("")
	unsupported := invoke(`,"service_tier":"flex"`)
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartCallRecorder(stopped, path); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restarted.StopCallRecorder() }()
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	read := func(response *httptest.ResponseRecorder) entity.CallRecord {
		var record entity.CallRecord
		if err := db.First(&record, "request_id = ?", response.Header().Get("X-Request-ID")).Error; err != nil {
			t.Fatal(err)
		}
		return record
	}
	original, updated, unpriced := read(first), read(second), read(unsupported)
	if original.ChargeAmount == nil || *original.ChargeAmount != "26.6" || original.PriceETag != initialETag || *original.ChargeCurrency != "CNY" {
		t.Fatalf("old receipt repriced: %+v", original.CallPricingFields)
	}
	if updated.ChargeAmount == nil || *updated.ChargeAmount != "31.5" || updated.PriceETag != page.ETag || original.SnapshotID == updated.SnapshotID {
		t.Fatalf("price write failed to publish: %+v", updated.CallPricingFields)
	}
	if unpriced.PricingStatus != "unsupported" || unpriced.ChargeAmount != nil || unpriced.PricingSnapshotJSON == nil || !strings.Contains(*unpriced.PricingSnapshotJSON, "request_service_tier") {
		t.Fatal("unsupported service tier was charged or not classified")
	}
	// Delivery retries cannot replace a canonical request's monetary fact.
	errorsCh := make(chan error, 3)
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			errorsCh <- restarted.RecordCall(ctx, service.CallFact{RequestID: original.RequestID, UserID: admin.User.ID, Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: time.Now(), CompletedAt: time.Now()})
		})
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	preserved := read(first)
	if preserved.ChargeAmount == nil || *preserved.ChargeAmount != "26.6" || *preserved.PricingSnapshotJSON != *original.PricingSnapshotJSON {
		t.Fatal("duplicate delivery replaced monetary fact")
	}
	// Owners see assessed amounts and nullable counters; only administrators see
	// normalized supplier-rate snapshots, and neither response includes secrets.
	personal := identityRequest(router, "GET", "/api/v1/calls/"+original.RequestID, "", cookie, "")
	expectStatus(t, personal, 200)
	if !strings.Contains(personal.Body.String(), `"charge_amount":"26.6"`) || strings.Contains(personal.Body.String(), "pricing_snapshot") || strings.Contains(personal.Body.String(), pm.ID) {
		t.Fatal("personal monetary DTO omitted amount or leaked supplier details")
	}
	detail := identityRequest(router, "GET", "/api/v1/admin/calls/"+original.RequestID, "", cookie, "")
	expectStatus(t, detail, 200)
	if !strings.Contains(detail.Body.String(), `"pricing_snapshot":{`) || strings.Contains(detail.Body.String(), "test-secret") || strings.Contains(detail.Body.String(), `"content"`) {
		t.Fatal("administrative snapshot missing or contains raw content")
	}
}
