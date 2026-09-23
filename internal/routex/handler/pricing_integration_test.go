package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
)

func testPricingLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"prices@example.com","password":"pricing-password","name":"Pricing admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	send := func(c *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), c, csrf)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		return send(cookie, admin.CSRFToken, method, path, body)
	}
	expectStatus(t, request("POST", "/api/v1/admin/members", map[string]any{"name": "Pricing reader", "email": "pricing-reader@example.com", "password": "pricing-password"}), 201)
	member, memberCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"pricing-reader@example.com","password":"pricing-password"}`, nil, ""))
	memberRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return send(memberCookie, member.CSRFToken, method, path, body)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_price", Name: "Price provider"},
		&entity.ProviderConnection{ID: "con_price", ProviderID: "prv_price", Name: "Text connection", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.com"},
		&entity.ProviderConnection{ID: "con_price_audio", ProviderID: "prv_price", Name: "Unsupported native protocol", Protocol: "native_audio", BaseURL: "https://example.com"},
		&entity.ProviderModel{ID: "pmo_price_a", ConnectionID: "con_price", UpstreamName: "Text A"},
		&entity.ProviderModel{ID: "pmo_price_b", ConnectionID: "con_price", UpstreamName: "Text B"},
		&entity.ProviderModel{ID: "pmo_price_audio", ConnectionID: "con_price_audio", UpstreamName: "Audio"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/v1/admin/prices"
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, memberRequest("GET", path, nil), 403)
	expectStatus(t, identityRequest(router, "GET", path+"/currency", "", nil, ""), 401)
	expectStatus(t, memberRequest("GET", path+"/currency", nil), 403)
	page := decodeCatalogResponse[service.PricePage](t, request("GET", path, nil), 200)
	if page.ETag == "" || page.Currency.PlatformCurrency != "USD" || len(page.Items) != 0 {
		t.Fatal("invalid initial pricing catalogue")
	}
	quoteInput := func(model string, input, output, read, write int64) map[string]any {
		return map[string]any{"provider_model_id": model, "usage": pricing.Usage{InputTokens: input, OutputTokens: output, CacheReadTokens: read, CacheWriteTokens: write}}
	}
	expectStatus(t, request("POST", path+"/quote", quoteInput("pmo_price_a", 0, 0, 0, 0)), 422)
	rate := func(metric, tier, amount string) map[string]any {
		return map[string]any{"metric": metric, "tier": tier, "amount": amount, "unit": pricing.Unit, "currency": "USD", "enabled": true}
	}
	item := func(model string, rates ...map[string]any) map[string]any {
		return map[string]any{"provider_model_id": model, "context_threshold": 200000, "rates": rates}
	}
	batch := func(etag string, items ...map[string]any) map[string]any {
		return map[string]any{"etag": etag, "items": items}
	}
	rates := []map[string]any{rate(pricing.Input, pricing.Base, "1.00"), rate(pricing.Output, pricing.Base, "2"), rate(pricing.CacheRead, pricing.Base, "0.1"), rate(pricing.CacheWrite, pricing.Base, "1.25"), rate(pricing.Input, pricing.Long, "2"), rate(pricing.Output, pricing.Long, "4"), rate(pricing.CacheRead, pricing.Long, "0.2"), rate(pricing.CacheWrite, pricing.Long, "2.5")}
	initial := batch(page.ETag, item("pmo_price_a", rates...))
	expectStatus(t, send(cookie, "", "PUT", path, initial), 403)
	expectStatus(t, memberRequest("PUT", path, initial), 403)
	expectStatus(t, request("PUT", path, batch(page.ETag, item("pmo_price_audio", rates[0]))), 400)
	expectStatus(t, request("PUT", path, batch(page.ETag, item("pmo_price_a", rates[0]), item("pmo_missing", rates[0]))), 404)
	intact := decodeCatalogResponse[service.PricePage](t, request("GET", path, nil), 200)
	if intact.ETag != page.ETag || len(intact.Items) != 0 {
		t.Fatal("invalid batch partially persisted")
	}
	page = decodeCatalogResponse[service.PricePage](t, request("PUT", path, initial), 200)
	currencyView := decodeCatalogResponse[service.PricingCurrencyPage](t, request("GET", path+"/currency", nil), 200)
	if currencyView.ETag != page.ETag || currencyView.Currency.PlatformCurrency != "USD" || len(currencyView.RequiredCurrencies) != 1 || currencyView.RequiredCurrencies[0] != "USD" {
		t.Fatal("currency view omitted complete enabled-rate requirements")
	}
	firstID, firstRateID := page.Items[0].ID, page.Items[0].Rates[0].ID
	if page.Items[0].ProviderID != "prv_price" || page.Items[0].Protocol != entity.ProtocolOpenAIChat || page.Items[0].FollowRepository || page.Items[0].UpdateSource != "api" {
		t.Fatal("incorrect derived identity or provenance")
	}
	expectStatus(t, request("PUT", path, initial), 409)
	quote := decodeCatalogResponse[service.PriceQuote](t, request("POST", path+"/quote", quoteInput("pmo_price_a", 200001, 1, 100000, 100000)), 200)
	if quote.Quote.Total != "0.270006" || quote.Quote.Tier != pricing.Long {
		t.Fatalf("incorrect tier charge: %+v", quote)
	}
	expectStatus(t, request("POST", path+"/quote", map[string]any{"provider_model_id": "pmo_price_a", "usage": map[string]any{"input_tokens": 1, "output_tokens": 0}}), 400)
	badQuote := quoteInput("pmo_price_a", 1, 0, 0, 0)
	badQuote["batch"] = true
	expectStatus(t, request("POST", path+"/quote", badQuote), 400)
	expectStatus(t, request("POST", path+"/quote", quoteInput("pmo_price_a", 1, 0, 1, 1)), 400)
	unknown := batch(page.ETag, item("pmo_price_a", rates[0]))
	unknown["source"] = "repository"
	expectStatus(t, request("PUT", path, unknown), 400)
	// A submitted metric is replaced, while omitted metrics and stable IDs survive.
	changed := rate(pricing.Input, pricing.Base, "3")
	page = decodeCatalogResponse[service.PricePage](t, request("PUT", path, batch(page.ETag, item("pmo_price_a", changed))), 200)
	if page.Items[0].ID != firstID || page.Items[0].Rates[0].ID != firstRateID || len(page.Items[0].Rates) != 8 {
		t.Fatal("partial upsert replaced unrelated data")
	}
	if quote.Quote.Total != "0.270006" {
		t.Fatal("captured quote was repriced")
	}
	// Same catalogue ETag arbitrates price changes and FX changes atomically.
	stale := page.ETag
	currency := map[string]any{"etag": stale, "currency": pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "7.1"}}}
	page = decodeCatalogResponse[service.PricePage](t, request("PUT", path+"/currency", currency), 200)
	expectStatus(t, request("PUT", path, batch(stale, item("pmo_price_a", changed))), 409)
	expectStatus(t, request("PUT", path+"/currency", map[string]any{"etag": page.ETag, "currency": pricing.FX{PlatformCurrency: "CNY"}}), 422)
	expectStatus(t, request("PUT", path+"/currency", map[string]any{"etag": page.ETag, "currency": pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "0"}}}), 400)
	quote = decodeCatalogResponse[service.PriceQuote](t, request("POST", path+"/quote", quoteInput("pmo_price_a", 100, 0, 0, 0)), 200)
	if quote.Quote.Total != "0.00213" || quote.Quote.Components[0].ExchangeRate != "7.1" {
		t.Fatal("FX not applied exactly")
	}
	// Two editors with one opaque catalogue ETag cannot silently overwrite.
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, amount := range []string{"4", "5"} {
		wg.Go(func() {
			response := request("PUT", path, batch(page.ETag, item("pmo_price_a", rate(pricing.Input, pricing.Base, amount))))
			results <- response.Code
		})
	}
	wg.Wait()
	close(results)
	statuses := map[int]int{}
	for status := range results {
		statuses[status]++
	}
	if statuses[200] != 1 || statuses[409] != 1 {
		t.Fatalf("concurrent updates: %v", statuses)
	}
	page = decodeCatalogResponse[service.PricePage](t, request("GET", path, nil), 200)
	// Zero is an explicit rate. Known-zero usage does not require unrelated rates.
	page = decodeCatalogResponse[service.PricePage](t, request("PUT", path, batch(page.ETag, item("pmo_price_b", rate(pricing.Input, pricing.Base, "0")))), 200)
	free := decodeCatalogResponse[service.PriceQuote](t, request("POST", path+"/quote", quoteInput("pmo_price_b", 1, 0, 0, 0)), 200)
	if free.Quote.Total != "0" {
		t.Fatal("explicit free price lost")
	}
	expectStatus(t, request("POST", path+"/quote", quoteInput("pmo_price_b", 0, 1, 0, 0)), 422)
	expectStatus(t, request("POST", path+"/quote", quoteInput("pmo_price_b", 0, 0, 0, 0)), 200)
	disabled := rate(pricing.Input, pricing.Base, "0")
	disabled["enabled"] = false
	page = decodeCatalogResponse[service.PricePage](t, request("PUT", path, batch(page.ETag, item("pmo_price_b", disabled))), 200)
	expectStatus(t, request("POST", path+"/quote", quoteInput("pmo_price_b", 0, 0, 0, 0)), 422)
	one := decodeCatalogResponse[service.PricePage](t, request("GET", path+"?limit=1", nil), 200)
	if len(one.Items) != 1 || one.NextCursor == "" {
		t.Fatal("first price page invalid")
	}
	two := decodeCatalogResponse[service.PricePage](t, request("GET", path+"?limit=1&cursor="+one.NextCursor, nil), 200)
	if len(two.Items) != 1 || two.Items[0].ID == one.Items[0].ID || two.NextCursor != "" {
		t.Fatal("price cursor repeated item")
	}
	expectStatus(t, request("GET", "/api/v1/admin/provider-models/pmo_price_a/price", nil), 200)
	// Delegation permits catalogue reads independently of member/provider admin.
	role := entity.Role{ID: "rol_price_reader", Name: "Price reader", NameKey: "pricing-reader-role"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{&entity.RolePermission{RoleID: role.ID, Permission: "prices.read"}, &entity.UserRole{UserID: member.User.ID, RoleID: role.ID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	expectStatus(t, memberRequest("GET", path, nil), 200)
	expectStatus(t, memberRequest("GET", path+"/currency", nil), 200)
	expectStatus(t, memberRequest("GET", "/api/v1/admin/providers", nil), 403)
	expectStatus(t, memberRequest("PUT", path, batch(page.ETag, item("pmo_price_a", changed))), 403)
	var audits []entity.AuditEvent
	if err := db.Where("action IN ?", []string{"prices.update", "prices.currency.update"}).Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) != 6 {
		t.Fatalf("expected exactly six committed pricing audits, got %d", len(audits))
	}
	for _, audit := range audits {
		if audit.DetailsJSON == nil || !json.Valid([]byte(*audit.DetailsJSON)) || !strings.Contains(*audit.DetailsJSON, `"before"`) || !strings.Contains(*audit.DetailsJSON, `"after"`) || !strings.Contains(*audit.DetailsJSON, `"source":"api"`) {
			t.Fatal("pricing audit missing normalized change details")
		}
	}
	testPricingReadSnapshot(t, db, admin.User.ID)
	testPricingCurrencyRequirements(t, db, admin.User.ID)
}

// Pausing after authorization's first SELECT recreates a MySQL repeatable-read
// snapshot predating a committed catalogue write. ETag and prices must still agree.
func testPricingReadSnapshot(t *testing.T, db *gorm.DB, actorID string) {
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	before, err := svc.ListPrices(context.Background(), actorID, service.PriceFilter{ProviderModelID: "pmo_price_a"})
	if err != nil {
		t.Fatal(err)
	}
	type pauseKey struct{}
	paused, release := make(chan struct{}), make(chan struct{})
	var once atomic.Bool
	callbackName := "test:pricing_snapshot_pause"
	err = db.Callback().Query().After("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && tx.Statement.Context.Value(pauseKey{}) == true && once.CompareAndSwap(false, true) {
			close(paused)
			<-release
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(callbackName); err != nil {
			t.Error(err)
		}
	}()
	defer close(release)
	results := make(chan *service.PricePage, 1)
	failures := make(chan error, 1)
	go func() {
		page, err := svc.ListPrices(context.WithValue(context.Background(), pauseKey{}, true), actorID, service.PriceFilter{ProviderModelID: "pmo_price_a"})
		results <- page
		failures <- err
	}()
	select {
	case <-paused:
	case <-time.After(10 * time.Second):
		t.Fatal("pricing read did not reach snapshot barrier")
	}
	changed, err := svc.WritePrices(context.Background(), actorID, before.ETag, []service.PriceInput{{ProviderModelID: "pmo_price_a", Rates: []pricing.Rate{{Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "9", Enabled: true}}}})
	if err != nil {
		t.Fatal(err)
	}
	release <- struct{}{}
	var page *service.PricePage
	select {
	case page = <-results:
	case <-time.After(10 * time.Second):
		t.Fatal("pricing read did not complete")
	}
	if err := <-failures; err != nil {
		t.Fatal(err)
	}
	if page.ETag != changed.ETag {
		t.Fatal("pricing read returned stale ETag")
	}
	found := false
	for _, rate := range page.Items[0].Rates {
		if rate.Metric == pricing.Input && rate.Tier == pricing.Base {
			found = true
			if rate.Amount != "9" {
				t.Fatal("new ETag was paired with stale price data")
			}
		}
	}
	if !found {
		t.Fatal("updated rate missing")
	}
}
