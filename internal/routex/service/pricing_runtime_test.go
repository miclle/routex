package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func runtimePriceFixture(data *runtimeData) {
	basis := testPriceBasis()
	data.Pricing = &runtimePricingData{Setting: entity.PricingSetting{ID: 1, ETag: basis.ETag, PlatformCurrency: basis.Currency.PlatformCurrency}, FX: []entity.PricingExchangeRate{{Currency: "USD", Rate: "7"}}, Prices: []entity.ModelPrice{{ID: basis.Schedule.PriceID, ProviderModelID: "pmd_one"}}}
	for _, rate := range basis.Schedule.Rates {
		data.Pricing.Rates = append(data.Pricing.Rates, entity.PriceRate{ID: rate.ID, ModelPriceID: basis.Schedule.PriceID, Metric: rate.Metric, Tier: rate.Tier, Unit: rate.Unit, Currency: rate.Currency, Amount: rate.Amount, Enabled: rate.Enabled})
	}
}
func TestRuntimeCapturesPriceBeforeDispatchWithoutDatabase(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	defer svc.upstream.CloseIdleConnections()
	runtimePriceFixture(data)
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_old", Models: routes, PublishedAt: time.Now()})
	oldDigest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		result *GatewayResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"content":"hello"}]}`), "req_price_snapshot")
		done <- response{result, err}
	}()
	<-started
	data.Pricing.Setting.ETag = "etag_new"
	data.Pricing.Rates[0].Amount = "900"
	data.Pricing.FX[0].Rate = "100"
	digest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	if oldDigest == digest {
		t.Fatal("pricing mutation excluded from runtime digest")
	}
	routes, err = svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_new", Models: routes, PublishedAt: time.Now()})
	close(release)
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	defer func() { _ = got.result.Response.Body.Close() }()
	if got.result.SnapshotID != "cfg_old" || got.result.PriceBasis.ETag != "etag_old" || got.result.PriceBasis.Currency.Rates["USD"] != "7" || got.result.PriceBasis.Schedule.Rates[0].Amount != "2" {
		t.Fatal("in-flight request changed pricing generation")
	}
	route, _, err := svc.runtimeRoute("mdl_one")
	if err != nil {
		t.Fatal(err)
	}
	if route.PriceBasis.ETag != "etag_new" {
		t.Fatal("new request retained old price")
	}
}
func TestCallPriceJournalRestartsWithAcceptedReceipt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	queue, err := eventqueue.Open(path, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{recorder: &callRecorder{queue: queue}}
	basis := testPriceBasis()
	result := &GatewayResult{UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model", ProviderModelID: "pmd_one", PriceBasis: basis}
	if err := svc.AdmitGatewayCall("req_crash", result); err != nil {
		t.Fatal(err)
	}
	if err := svc.AdmitGatewayCall("req_final", result); err != nil {
		t.Fatal(err)
	}
	fact := pricedFact()
	fact.RequestID = "req_final"
	fact.UserID = "usr_one"
	fact.KeyID = "key_one"
	fact.Status = "canceled"
	fact.Protocol = "openai_chat"
	fact.StartedAt = time.Now()
	fact.CompletedAt = fact.StartedAt
	if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	queue, err = eventqueue.Open(path, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	entries, err := queue.Read(8)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("recovered %d entries", len(entries))
	}
	for _, entry := range entries {
		var recovered CallFact
		if err := json.Unmarshal(entry.Payload, &recovered); err != nil {
			t.Fatal(err)
		}
		if recovered.PriceBasis == nil || recovered.PriceBasis.ETag != "etag_old" {
			t.Fatal("crash/replay lost captured price")
		}
		if entry.ID == "req_crash" {
			if recovered.Pricing.Status != "not_final" || recovered.Pricing.Amount != nil {
				t.Fatal("crash invented a charge")
			}
		} else if recovered.Pricing.Amount == nil || *recovered.Pricing.Amount != "26.6" || recovered.Status != "canceled" {
			t.Fatal("final canceled receipt not durable")
		}
	}
}
