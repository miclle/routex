package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/prices"
	"gorm.io/gorm"
)

type repositoryFailureFixtureKey struct{}

func repositoryFixtureSource(t *testing.T, threshold int64, amount, currency string, omitCacheWrite bool) *prices.Snapshot {
	t.Helper()
	models := []any{}
	for _, key := range []string{"first", "second"} {
		rates := []any{}
		for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
			if omitCacheWrite && metric == pricing.CacheWrite {
				continue
			}
			rates = append(rates, map[string]any{"key": key + "/" + metric, "metric": metric, "tier": pricing.Base, "unit": pricing.Unit, "currency": currency, "amount": []string{amount, "2", "0.25", "3"}[i], "enabled": true})
		}
		models = append(models, map[string]any{"key": "source/" + key, "provider_key": "controlled/provider", "model": "Unrelated source name " + key, "protocol": entity.ProtocolOpenAIChat, "context_threshold": threshold, "rates": rates})
	}
	raw, err := json.Marshal(map[string]any{"schema_version": 1, "models": models})
	if err != nil {
		t.Fatal(err)
	}
	source, err := prices.Parse(raw)
	if err != nil {
		t.Fatal("controlled source must pass the actual parser", err)
	}
	return source
}

func repositoryFixtureError(t *testing.T, err error, code int) {
	t.Helper()
	var app *apperrors.Error
	if !errors.As(err, &app) || app.Code != code {
		t.Fatalf("repository error = %v, want HTTP %d", err, code)
	}
}

// Real-driver data/report acceptance. Seeded pricing facts below are immutable
// preservation evidence; they do not claim a native inference completion.
func testPricingRepositoryLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	source := repositoryFixtureSource(t, 0, "9007199254740993.000000000000000001", "USD", false)
	var publicationFailure atomic.Bool
	names := []string{"fixture:repository_audit", "fixture:repository_published", "fixture:repository_publication"}
	if err := db.Callback().Create().Before("gorm:create").Register(names[0], func(tx *gorm.DB) {
		if tx.Statement.Table == "audit_events" && tx.Statement.Context.Value(repositoryFailureFixtureKey{}) == 1 {
			_ = tx.AddError(errors.New("controlled repository audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(names[1], func(tx *gorm.DB) {
		if tx.Error == nil && tx.Statement.Table == "audit_events" && tx.Statement.Context.Value(repositoryFailureFixtureKey{}) == 2 {
			publicationFailure.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(names[2], func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled repository publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	services := []*service.Service{}
	defer func() {
		for _, svc := range services {
			svc.StopRuntime()
		}
		for _, name := range names[:2] {
			if err := db.Callback().Create().Remove(name); err != nil {
				t.Error(err)
			}
		}
		if err := db.Callback().Query().Remove(names[2]); err != nil {
			t.Error(err)
		}
	}()
	newService := func(snapshot *prices.Snapshot) *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithRepositoryPriceSource(snapshot))
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, svc)
		return svc
	}
	svc := newService(source)
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"repository@example.invalid","password":"repository-price-password","name":"Repository administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	send := func(method, path string, body any, etag string, c *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		raw := ""
		if body != nil {
			encoded, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			raw = string(encoded)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(raw))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if c != nil {
			req.AddCookie(c)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	base := "/api/v1/admin/prices/repository"
	request := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
		return send(method, path, body, etag, cookie, admin.CSRFToken)
	}
	read := func() service.RepositoryPriceConfigView {
		t.Helper()
		res := request("GET", base, nil, "")
		view := decodeCatalogResponse[service.RepositoryPriceConfigView](t, res, 200)
		if res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("ETag") != `"`+view.ReviewETag+`"` {
			t.Fatal("private coherent configuration headers")
		}
		return view
	}
	next := 0
	uuid := func() string { next++; return fmt.Sprintf("49000000-0000-4000-8000-%012d", next) }
	for _, row := range []any{&entity.Provider{ID: "prv_repo", Name: "Controlled source target"}, &entity.ProviderConnection{ID: "con_repo", ProviderID: "prv_repo", Name: "Chat", BaseURL: "https://example.invalid/v1", Protocol: entity.ProtocolOpenAIChat}, &entity.ProviderModel{ID: "pmo_repo_a", ConnectionID: "con_repo", UpstreamName: "Literal_% first"}, &entity.ProviderModel{ID: "pmo_repo_b", ConnectionID: "con_repo", UpstreamName: "Second local label"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Log("repository source default and exact candidate picker")
	initial := read()
	if initial.Enabled || len(initial.Mappings) != 0 || initial.Source.Digest != source.Digest() || initial.Source.ModelCount != 2 || initial.LastAttemptAt != nil || initial.LastSuccessAt != nil {
		t.Fatal("configuration fabricated enablement/mapping/attempt")
	}
	candidates := decodeCatalogResponse[service.RepositoryPriceCandidatePage](t, request("GET", base+"/candidates?q=_%25&limit=1", nil, ""), 200)
	if len(candidates.Items) != 1 || candidates.Items[0].ProviderModelID != "pmo_repo_a" {
		t.Fatal("literal wildcard candidate search broadened", candidates)
	}
	first := decodeCatalogResponse[service.RepositoryPriceCandidatePage](t, request("GET", base+"/candidates?limit=1", nil, ""), 200)
	second := decodeCatalogResponse[service.RepositoryPriceCandidatePage](t, request("GET", base+"/candidates?limit=1&cursor="+first.NextCursor, nil, ""), 200)
	if len(first.Items) != 1 || first.NextCursor == "" || len(second.Items) != 1 || first.Items[0].ProviderModelID == second.Items[0].ProviderModelID || second.NextCursor != "" {
		t.Fatal("bounded candidate cursor repeated/omitted subjects")
	}
	for _, query := range []string{"?q=", "?limit=0", "?limit=51", "?limit=01", "?limit=1&limit=2", "?actor=usr_other", "?cursor=bad%20", "?page=2"} {
		expectStatus(t, request("GET", base+"/candidates"+query, nil, ""), 400)
	}
	for _, query := range []string{"?actor=other", "?source=other", "?"} {
		expectStatus(t, request("GET", base+query, nil, ""), 400)
	}
	config := service.RepositoryPriceConfigInput{RequestID: uuid(), Enabled: true, Mappings: []service.RepositoryPriceMappingInput{{ProviderModelID: "pmo_repo_b", SourceModelKey: "source/second"}, {ProviderModelID: "pmo_repo_a", SourceModelKey: "source/first"}}, Reason: "Reviewed exact local source mappings"}
	// Mutations retain strict bodies and reviewed strong preconditions.
	expectStatus(t, request("PUT", base, config, ""), 400)
	expectStatus(t, send("PUT", base, config, initial.ReviewETag, cookie, ""), 403)
	for _, body := range []string{
		`null`,
		`{"request_id":"49000000-9999-4999-8999-999999999999","enabled":true,"enabled":false,"mappings":[],"reason":"Reviewed"}`,
		`{"request_id":"49000000-9999-4999-8999-999999999999","enabled":null,"mappings":[],"reason":"Reviewed"}`,
		`{"request_id":"49000000-9999-4999-8999-999999999999","enabled":true,"mappings":[],"reason":"Reviewed","source":"remote"}`,
	} {
		req := httptest.NewRequest("PUT", "http://routex.test"+base, strings.NewReader(body))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		req.Header.Set("If-Match", `"`+initial.ReviewETag+`"`)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 400)
	}
	for _, reason := range []string{"", " trimmed", "line\nbreak", strings.Repeat("界", 1001)} {
		bad := config
		bad.RequestID = uuid()
		bad.Reason = reason
		expectStatus(t, request("PUT", base, bad, initial.ReviewETag), 400)
	}
	configured := decodeCatalogResponse[service.RepositoryPriceResult](t, request("PUT", base, config, initial.ReviewETag), 201)
	if !configured.Committed || !configured.ConfigurationApplied || configured.RuntimeApplied || configured.Receipt.Mode != "configure" {
		t.Fatal("config claimed pricing runtime publication")
	}
	retry := decodeCatalogResponse[service.RepositoryPriceResult](t, request("PUT", base, config, initial.ReviewETag), 200)
	if !reflect.DeepEqual(configured.Receipt, retry.Receipt) {
		t.Fatal("configuration retry changed persisted receipt precision")
	}
	selection := service.RepositoryPriceSelection{Mode: "sync", ProviderModelIDs: []string{"pmo_repo_b", "pmo_repo_a"}, RateIDs: []string{}}
	preview := func(s *service.Service, sel service.RepositoryPriceSelection) service.RepositoryPricePreview {
		t.Helper()
		p, err := s.PreviewRepositoryPrices(ctx, admin.User.ID, sel)
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}
	applyInput := func(p service.RepositoryPricePreview, sel service.RepositoryPriceSelection) service.RepositoryPriceApplyInput {
		return service.RepositoryPriceApplyInput{RequestID: uuid(), PreviewDigest: p.PreviewDigest, Selection: sel, Reason: "Apply exact reviewed repository prices"}
	}
	list := func() service.PricePage {
		t.Helper()
		p, err := svc.ListPrices(ctx, admin.User.ID, service.PriceFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return *p
	}
	modelPrice := func(id string) service.PriceRecord {
		t.Helper()
		for _, p := range list().Items {
			if p.ProviderModelID == id {
				return p
			}
		}
		t.Fatal("missing price", id)
		return service.PriceRecord{}
	}
	p := preview(svc, selection)
	if !p.Valid || len(p.Changes) != 8 || p.PreviewDigest == "" {
		t.Fatal("explicit two-model addition preview", p)
	}
	if !reflect.DeepEqual(configured.Configuration.LastAttemptAt, read().LastAttemptAt) || len(list().Items) != 0 {
		t.Fatal("preview mutated attempt or current catalogue")
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	peer := newService(source)
	input := applyInput(p, selection)
	t.Log("same immutable apply intent concurrently across independent Services")
	type outcome struct {
		result *service.RepositoryPriceResult
		err    error
	}
	ch := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, s := range []*service.Service{svc, peer} {
		wg.Go(func() { r, e := s.ApplyRepositoryPrices(ctx, admin.User.ID, p.ReviewETag, input); ch <- outcome{r, e} })
	}
	wg.Wait()
	close(ch)
	created := 0
	var applied *service.RepositoryPriceResult
	for o := range ch {
		if o.err != nil {
			t.Fatal(o.err)
		}
		if o.result.Created {
			created++
		}
		if applied != nil && !reflect.DeepEqual(applied.Receipt, o.result.Receipt) {
			t.Fatal("concurrent intent has different receipt")
		}
		applied = o.result
	}
	if created != 1 {
		t.Fatal("same UUID committed more than once", created)
	}
	var auditCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "prices.repository.apply").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatal("concurrent apply audit cardinality", auditCount, err)
	}
	receipt := decodeCatalogResponse[service.RepositoryPriceResult](t, request("GET", base+"/receipts/"+input.RequestID, nil, ""), 200)
	if !receipt.RuntimeApplied || receipt.ApplicationStatus != "applied" || !reflect.DeepEqual(receipt.Receipt, applied.Receipt) {
		t.Fatal("exact current publication receipt", receipt)
	}
	a, b := modelPrice("pmo_repo_a"), modelPrice("pmo_repo_b")
	rates := map[string]pricing.Rate{}
	for _, r := range a.Rates {
		rates[r.Metric] = r
		if a.RateSources[r.ID].Kind != "repository" {
			t.Fatal("source ownership omitted", r)
		}
	}
	if rates[pricing.Input].Amount != "9007199254740993.000000000000000001" || !a.FollowRepository || a.ContextThresholdSource.Kind != "repository" {
		t.Fatal("exact decimal source precision or threshold proof lost")
	}
	t.Log("unpublished raw price change cannot borrow an unchanged catalogue ETag")
	originalAmount := rates[pricing.Input].Amount
	if err := db.Model(&entity.PriceRate{}).Where("id = ?", rates[pricing.Input].ID).UpdateColumn("amount", "7").Error; err != nil {
		t.Fatal(err)
	}
	rawChanged, err := svc.GetRepositoryPriceReceipt(ctx, admin.User.ID, input.RequestID)
	if err != nil || rawChanged.RuntimeApplied || rawChanged.ApplicationStatus != "superseded" {
		t.Fatal("raw price mutation retained false current receipt application", rawChanged, err)
	}
	rawReplay, err := svc.ApplyRepositoryPrices(ctx, admin.User.ID, p.ReviewETag, input)
	if err != nil || rawReplay.ApplicationStatus != "superseded" {
		t.Fatal("historical intent replay reapplied unpublished raw change", err)
	}
	var rawRate entity.PriceRate
	if err := db.Take(&rawRate, "id = ?", rates[pricing.Input].ID).Error; err != nil || rawRate.Amount != "7" {
		t.Fatal("replay restored old source amount", err)
	}
	if err := db.Model(&entity.PriceRate{}).Where("id = ?", rawRate.ID).UpdateColumn("amount", originalAmount).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	conflicting := input
	conflicting.Reason = "Different intent cannot borrow UUID"
	expectStatus(t, request("POST", base+"/apply", conflicting, p.ReviewETag), 409)
	collision := input
	collision.RequestID = config.RequestID
	expectStatus(t, request("POST", base+"/apply", collision, p.ReviewETag), 409)
	// A captured pricing basis and historical amount must never be recalculated.
	quote, err := svc.QuotePrice(ctx, admin.User.ID, "pmo_repo_a", pricing.Usage{InputTokens: 1, OutputTokens: 1})
	if err != nil {
		t.Fatal(err)
	}
	money, currency := quote.Quote.Total, quote.Quote.Currency
	captured, _ := json.Marshal(quote)
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	basis := string(captured)
	history := entity.CallRecord{RequestID: "req_repo_history", UserID: admin.User.ID, KeyID: "key_repo_history", ModelID: "mdl_repo_history", ProviderModelID: "pmo_repo_a", SnapshotID: "snp_repo_history", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: stamp, CompletedAt: stamp, CallPricingFields: entity.CallPricingFields{PricingStatus: "priced", PriceETag: quote.ETag, ChargeAmount: &money, ChargeCurrency: &currency, PricingSnapshotJSON: &basis}}
	if err := db.Create(&history).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&history, "request_id = ?", history.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	manual := func(id string, threshold *int64, submitted ...pricing.Rate) {
		t.Helper()
		page := list()
		if _, err := svc.WritePrices(ctx, admin.User.ID, page.ETag, []service.PriceInput{{ProviderModelID: id, ContextThreshold: threshold, Rates: submitted}}); err != nil {
			t.Fatal(err)
		}
	}
	zero, disabled, same := rates[pricing.Input], rates[pricing.Output], rates[pricing.CacheRead]
	zero.ID, disabled.ID, same.ID = "", "", ""
	zero.Amount = "0"
	disabled.Enabled = false
	t.Log("sparse API edits protect custom zero, disabled and identical values")
	manual("pmo_repo_a", nil, zero, disabled, same)
	changed := modelPrice("pmo_repo_a")
	for _, r := range changed.Rates {
		want := "custom"
		if r.Metric == pricing.CacheWrite {
			want = "repository"
		}
		if changed.RateSources[r.ID].Kind != want {
			t.Fatal("sparse write changed omitted ownership", r.Metric)
		}
	}
	protection := preview(svc, service.RepositoryPriceSelection{Mode: "sync", ProviderModelIDs: []string{"pmo_repo_a"}, RateIDs: []string{}})
	counts := map[string]int{}
	for _, c := range protection.Changes {
		counts[c.Action]++
	}
	if !protection.Valid || counts["protected_custom"] != 3 || counts["unchanged"] != 1 {
		t.Fatal("custom protection scope", protection)
	}
	oldRetry, err := svc.ApplyRepositoryPrices(ctx, admin.User.ID, p.ReviewETag, input)
	if err != nil || oldRetry.ApplicationStatus != "superseded" || !reflect.DeepEqual(oldRetry.Receipt, applied.Receipt) || !reflect.DeepEqual(changed, modelPrice("pmo_repo_a")) {
		t.Fatal("historical retry restored a later custom edit", err)
	}
	csv := "provider_model_id,metric,tier,unit,currency,amount,enabled,context_threshold,upstream_name\npmo_repo_b,OUTPUT_TOKEN,base,1M_TOKEN,USD,2,true,,Ignored\n"
	importPreview, err := svc.PreviewPriceImport(ctx, admin.User.ID, csv)
	if err != nil || !importPreview.Valid {
		t.Fatal("sparse same-value import", err)
	}
	if _, err := svc.CommitPriceImport(ctx, admin.User.ID, csv, importPreview.ETag, importPreview.Digest); err != nil {
		t.Fatal(err)
	}
	imported := modelPrice("pmo_repo_b")
	for _, r := range imported.Rates {
		want := "repository"
		if r.Metric == pricing.Output {
			want = "custom"
		}
		if imported.RateSources[r.ID].Kind != want {
			t.Fatal("import replaced omitted ownership", r.Metric)
		}
	}
	current := read()
	disable := config
	disable.RequestID = uuid()
	disable.Enabled = false
	disabledConfig := decodeCatalogResponse[service.RepositoryPriceResult](t, request("PUT", base, disable, current.ReviewETag), 201)
	configRetry := decodeCatalogResponse[service.RepositoryPriceResult](t, request("PUT", base, config, initial.ReviewETag), 200)
	if configRetry.ApplicationStatus != "superseded" || configRetry.Configuration.Enabled || !reflect.DeepEqual(configRetry.Receipt, configured.Receipt) {
		t.Fatal("old config retry restored later enablement")
	}
	if disabledConfig.RuntimeApplied {
		t.Fatal("disable claimed runtime prices changed")
	}
	invalid := preview(svc, selection)
	if invalid.Valid || invalid.PreviewDigest != "" {
		t.Fatal("disabled sync valid")
	}
	restore := service.RepositoryPriceSelection{Mode: "restore", ProviderModelIDs: []string{"pmo_repo_a"}, RateIDs: []string{rates[pricing.Input].ID}}
	restoredPreview := preview(svc, restore)
	if !restoredPreview.Valid || len(restoredPreview.Changes) != 1 {
		t.Fatal("explicit restore while disabled", restoredPreview)
	}
	restoreInput := applyInput(restoredPreview, restore)
	restored := decodeCatalogResponse[service.RepositoryPriceResult](t, request("POST", base+"/apply", restoreInput, restoredPreview.ReviewETag), 201)
	if !restored.RuntimeApplied {
		t.Fatal("restore current runtime publication missing")
	}
	afterRestore := modelPrice("pmo_repo_a")
	for _, r := range afterRestore.Rates {
		if r.Metric == pricing.Input && r.Amount != "9007199254740993.000000000000000001" {
			t.Fatal("restore rounded decimal")
		}
		if r.Metric == pricing.Output && r.Enabled {
			t.Fatal("restore enabled unselected custom output")
		}
	}
	// Source/config/catalogue changes each invalidate a previously reviewed apply.
	t.Log("stale review, source digest and protected shared-threshold transitions")
	stale := preview(svc, restore)
	staleInput := applyInput(stale, restore)
	manual("pmo_repo_a", nil, zero)
	_, err = svc.ApplyRepositoryPrices(ctx, admin.User.ID, stale.ReviewETag, staleInput)
	repositoryFixtureError(t, err, 409)
	source2 := repositoryFixtureSource(t, 0, "8", "USD", false)
	alternate := newService(source2)
	fresh := preview(svc, restore)
	freshInput := applyInput(fresh, restore)
	_, err = alternate.ApplyRepositoryPrices(ctx, admin.User.ID, fresh.ReviewETag, freshInput)
	repositoryFixtureError(t, err, 409)
	thresholdSource := newService(repositoryFixtureSource(t, 128000, "8", "USD", false))
	ambiguous := preview(thresholdSource, restore)
	hasProtected := false
	for _, e := range ambiguous.Errors {
		hasProtected = hasProtected || e.Code == "protected_threshold"
	}
	if ambiguous.Valid || !hasProtected {
		t.Fatal("unselected custom token threshold reinterpreted", ambiguous)
	}
	eurSource := newService(repositoryFixtureSource(t, 0, "8", "EUR", false))
	fx := preview(eurSource, restore)
	hasFX := false
	for _, e := range fx.Errors {
		hasFX = hasFX || e.Code == "missing_exchange_rate"
	}
	if fx.Valid || !hasFX {
		t.Fatal("missing conversion silently treated free", fx)
	}
	current = read()
	enableAgain := config
	enableAgain.RequestID = uuid()
	decodeCatalogResponse[service.RepositoryPriceResult](t, request("PUT", base, enableAgain, current.ReviewETag), 201)
	missingRateSource := newService(repositoryFixtureSource(t, 0, "8", "USD", true))
	retained := preview(missingRateSource, service.RepositoryPriceSelection{Mode: "restore", ProviderModelIDs: []string{"pmo_repo_a"}, RateIDs: []string{rates[pricing.CacheWrite].ID}})
	if retained.Valid || len(retained.Errors) == 0 {
		t.Fatal("missing source silently deleted selected rate")
	}
	warningPreview := preview(missingRateSource, service.RepositoryPriceSelection{Mode: "sync", ProviderModelIDs: []string{"pmo_repo_a"}, RateIDs: []string{}})
	warningFound := false
	for _, warning := range warningPreview.Warnings {
		warningFound = warningFound || warning.Code == "missing_source_rate" && warning.RateID != nil && *warning.RateID == rates[pricing.CacheWrite].ID
	}
	if !warningPreview.Valid || !warningFound {
		t.Fatal("missing source rate did not retain local rate with located warning", warningPreview)
	}
	for _, sel := range []service.RepositoryPriceSelection{{Mode: "sync", ProviderModelIDs: []string{"PMO_REPO_A"}}, {Mode: "sync", ProviderModelIDs: []string{"pmo_repo_a "}}, {Mode: "sync", ProviderModelIDs: []string{"pmo_repo_a", "pmo_repo_a"}}, {Mode: "restore", ProviderModelIDs: []string{"pmo_repo_a"}, RateIDs: []string{b.Rates[0].ID}}} {
		if p, e := svc.PreviewRepositoryPrices(ctx, admin.User.ID, sel); e == nil && p.Valid {
			t.Fatal("alias/duplicate/foreign selected rate accepted", sel)
		}
	}
	t.Log("audit failure rolls back complete changeset; publication failure preserves receipt")
	transactionPreview := preview(svc, restore)
	transactionInput := applyInput(transactionPreview, restore)
	beforeFailure := modelPrice("pmo_repo_a")
	if _, err := svc.ApplyRepositoryPrices(context.WithValue(ctx, repositoryFailureFixtureKey{}, 1), admin.User.ID, transactionPreview.ReviewETag, transactionInput); err == nil {
		t.Fatal("audit failure committed")
	}
	var absent int64
	if err := db.Model(&entity.RepositoryPriceReceipt{}).Where("request_id = ?", transactionInput.RequestID).Count(&absent).Error; err != nil || absent != 0 || !reflect.DeepEqual(beforeFailure, modelPrice("pmo_repo_a")) {
		t.Fatal("audit failure retained receipt or partial rates", err)
	}
	transactionInput.RequestID = uuid()
	pending, err := svc.ApplyRepositoryPrices(context.WithValue(ctx, repositoryFailureFixtureKey{}, 2), admin.User.ID, transactionPreview.ReviewETag, transactionInput)
	if err != nil || !pending.Committed || pending.RuntimeApplied || pending.ApplicationStatus != "pending" {
		t.Fatal("publication failure lost durable receipt or claimed applied", pending, err)
	}
	publicationFailure.Store(false)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	reconciled, err := svc.ApplyRepositoryPrices(ctx, admin.User.ID, transactionPreview.ReviewETag, transactionInput)
	if err != nil || !reconciled.RuntimeApplied || !reflect.DeepEqual(pending.Receipt, reconciled.Receipt) {
		t.Fatal("publication retry changed intent", err)
	}
	var afterHistory entity.CallRecord
	if err := db.Take(&afterHistory, "request_id = ?", history.RequestID).Error; err != nil || !reflect.DeepEqual(history, afterHistory) {
		t.Fatal("repository maintenance repriced history", err)
	}
	t.Log("current independent read/write actor permission and private receipt ownership")
	member, err := svc.CreateMember(ctx, admin.User.ID, "repository-reader@example.invalid", "repository-price-password", "Independent reader", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	role := entity.Role{ID: "rol_repo_reader", Name: "Repository reader", NameKey: "repository-reader"}
	for _, row := range []any{&role, &entity.RolePermission{RoleID: role.ID, Permission: "prices.read"}, &entity.UserRole{UserID: member.User.ID, RoleID: role.ID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if v, err := svc.GetRepositoryPriceSource(ctx, member.User.ID); err != nil || v.CanWrite {
		t.Fatal("read permission borrowed write", err)
	}
	_, err = svc.GetRepositoryPriceReceipt(ctx, member.User.ID, input.RequestID)
	repositoryFixtureError(t, err, 403)
	if err := db.Create(&entity.RolePermission{RoleID: role.ID, Permission: "prices.write"}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetRepositoryPriceReceipt(ctx, member.User.ID, input.RequestID)
	repositoryFixtureError(t, err, 404)
	_, err = svc.ApplyRepositoryPrices(ctx, member.User.ID, p.ReviewETag, input)
	repositoryFixtureError(t, err, 409)
	for _, alias := range []string{strings.ToUpper(admin.User.ID), admin.User.ID + " "} {
		_, err = svc.GetRepositoryPriceSource(ctx, alias)
		repositoryFixtureError(t, err, 401)
	}
	memberReview, err := svc.GetRepositoryPriceSource(ctx, member.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	memberConfig := enableAgain
	memberConfig.RequestID = uuid()
	memberConfig.Reason = "Independent delegated writer configuration"
	memberCommit, err := svc.WriteRepositoryPriceSource(ctx, member.User.ID, memberReview.ReviewETag, memberConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Where("role_id = ? AND permission = ?", role.ID, "prices.read").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetRepositoryPriceSource(ctx, member.User.ID)
	repositoryFixtureError(t, err, 403)
	writeOnlyReplay, err := svc.WriteRepositoryPriceSource(ctx, member.User.ID, memberReview.ReviewETag, memberConfig)
	if err != nil || !reflect.DeepEqual(writeOnlyReplay.Receipt, memberCommit.Receipt) {
		t.Fatal("write-only own intent could not reconcile", err)
	}
	if err := db.Where("role_id = ? AND permission = ?", role.ID, "prices.write").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetRepositoryPriceReceipt(ctx, member.User.ID, memberConfig.RequestID)
	repositoryFixtureError(t, err, 403)
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.GetRepositoryPriceReceipt(ctx, admin.User.ID, input.RequestID)
	repositoryFixtureError(t, err, 401)
	if err := db.Model(&entity.User{}).Where("id = ?", admin.User.ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	restarted := newService(source)
	saved, err := restarted.GetRepositoryPriceReceipt(ctx, admin.User.ID, transactionInput.RequestID)
	if err != nil || saved.RuntimeApplied || saved.ApplicationStatus != "pending" {
		t.Fatal("restart fabricated runtime application", err)
	}
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	saved, err = restarted.GetRepositoryPriceReceipt(ctx, admin.User.ID, transactionInput.RequestID)
	if err != nil || !saved.RuntimeApplied || !reflect.DeepEqual(saved.Receipt, pending.Receipt) {
		t.Fatal("fresh runtime did not reconcile saved receipt", err)
	}
}
