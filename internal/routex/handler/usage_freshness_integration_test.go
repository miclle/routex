package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

// The root-owned harness supplies a fresh database. Every inference fact below
// comes from a real controlled native response; no report fixture is a substitute
// for native completion or a claimed ingestion watermark.
func testUsageFreshnessLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var dispatched atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"native-freshness"}]}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		dispatched.Add(1)
		var input struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Messages) != 1 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mode := input.Messages[0].Content
		if mode == "first" {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, usageFreshnessResponse(mode))
	}))
	store, err := secretstore.New(bytes.Repeat([]byte{91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var services []*service.Service
	firstStarted := false
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		cancel()
		if firstStarted {
			select {
			case <-finished:
			case <-time.After(5 * time.Second):
				t.Error("native barrier worker did not stop")
			}
		}
		for _, svc := range services {
			svc.StopRuntime()
			if err := svc.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
		upstream.Close()
	})
	newService := func() *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		services = append(services, svc)
		return svc
	}
	svc := newService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"freshness-admin@example.invalid","password":"test-only-freshness-password","name":"Freshness administrator"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, cookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	emptyActor := entity.User{ID: "usr_freshness_empty", Email: "freshness-empty@example.invalid", Name: "Empty account", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&emptyActor).Error; err != nil {
		t.Fatal(err)
	}
	emptyLogin := identityRequest(router, http.MethodPost, "/api/v1/auth/login", `{"email":"freshness-empty@example.invalid","password":"test-only-freshness-password"}`, nil, "")
	expectStatus(t, emptyLogin, http.StatusOK)
	_, emptyCookie := readIdentity(t, emptyLogin)
	provider, err := svc.CreateProvider(ctx, admin.User.ID, "Freshness provider", service.CreateConnectionInput{Name: "Connection", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "Credential", Secret: "test-only-freshness-provider-secret"})
	if err != nil {
		t.Fatal(err)
	}
	credentialID := provider.Connections[0].Credentials[0].ID
	if _, err := svc.VerifyCredential(ctx, admin.User.ID, credentialID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credentialID, true); err != nil {
		t.Fatal(err)
	}
	var pm entity.ProviderModel
	if err := db.First(&pm, "connection_id = ?", provider.Connections[0].Connection.ID).Error; err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "freshness-text", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, model.Model.ID, []service.ModelWeight{{BindingID: model.Bindings[0].Binding.ID, Weight: 100}}); err != nil {
		t.Fatal(err)
	}
	key, err := svc.CreatePersonalKey(ctx, admin.User.ID, "Freshness key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, admin.User.ID, key.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	readPrice := func() service.PricePage {
		t.Helper()
		return decodeCatalogResponse[service.PricePage](t, identityRequest(router, http.MethodGet, "/api/v1/admin/prices", "", cookie, ""), http.StatusOK)
	}
	writePrice := func(path string, body any) service.PricePage {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		response := identityRequest(router, http.MethodPut, path, string(raw), cookie, admin.CSRFToken)
		return decodeCatalogResponse[service.PricePage](t, response, http.StatusOK)
	}
	page := readPrice()
	page = writePrice("/api/v1/admin/prices/currency", WritePricingCurrencyRequest{ETag: page.ETag, Currency: pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "7"}}})
	enabled := true
	rates := []PriceRateInput{}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		rates = append(rates, PriceRateInput{Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: []string{"2", "4", "0.5", "3"}[index], Enabled: &enabled})
	}
	writeRates := func() service.PricePage {
		t.Helper()
		return writePrice("/api/v1/admin/prices", WritePricesRequest{ETag: readPrice().ETag, Items: []PriceWriteItem{{ProviderModelID: pm.ID, Rates: rates}}})
	}
	page = writeRates()
	oldETag := page.ETag
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(t.TempDir(), "freshness-journal.db")
	pauseRecorder := func(s *service.Service) {
		t.Helper()
		run, stop := context.WithCancel(ctx)
		defer stop()
		if err := s.StartCallRecorder(run, journal); err != nil {
			t.Fatal(err)
		}
		stop() // Stop only automatic delivery; admission and completion stay live.
	}
	pauseRecorder(svc)
	from := time.Now().UTC().Add(-time.Hour)
	to := from.Add(2 * time.Hour)
	filters := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "timezone": {"UTC"}, "granularity": {"hour"}, "compare": {"true"}}
	readReport := func(r http.Handler, principal *http.Cookie, actorID string, query url.Values) service.UsageReport {
		t.Helper()
		path := "/api/v1/usage?" + query.Encode()
		queriedBefore := time.Now().UTC()
		report := decodeCatalogResponse[service.UsageReport](t, identityRequest(r, http.MethodGet, path, "", principal, ""), http.StatusOK)
		queriedAfter := time.Now().UTC()
		from, fromErr := time.Parse(time.RFC3339Nano, query.Get("from"))
		to, toErr := time.Parse(time.RFC3339Nano, query.Get("to"))
		if fromErr != nil || toErr != nil || !report.Current.From.Equal(from) || !report.Current.To.Equal(to) || report.QueriedAt.Before(queriedBefore) || report.QueriedAt.After(queriedAfter) {
			t.Fatalf("report did not capture the fixed server-queried half-open range: %+v", report)
		}
		if report.Source != "persisted_call_records" || !report.MayLag || report.QueriedAt.IsZero() || report.Previous == nil {
			t.Fatalf("freshness metadata falsely claimed completeness: %+v", report)
		}
		if !report.Previous.To.Equal(from) || !report.Previous.From.Equal(from.Add(-to.Sub(from))) {
			t.Fatal("comparison did not retain its exact adjacent elapsed range")
		}
		before := time.Now().UTC()
		csv := identityRequest(r, http.MethodGet, "/api/v1/usage/export.csv?"+query.Encode(), "", principal, "")
		assertUsageExportCSV(t, csv, "personal", actorID, query, report, before, time.Now().UTC())
		return report
	}
	assertEmpty := func(report service.UsageReport) {
		t.Helper()
		if report.Current.Summary.Requests != 0 || report.Previous.Summary.Requests != 0 || report.LatestCompletedAt != nil || len(report.Current.Models) != 0 || len(report.Current.Keys) != 0 || report.Current.Summary.SuccessRate != nil || report.Current.Summary.Tokens.Total.Value == nil || *report.Current.Summary.Tokens.Total.Value != "0" || report.Current.Summary.Tokens.Total.UnknownCalls != 0 || len(report.Current.Summary.Amounts) != 0 {
			t.Fatalf("queued/foreign facts or completion watermark leaked: %+v", report)
		}
	}
	invoke := func(mode string) *httptest.ResponseRecorder {
		raw := fmt.Sprintf(`{"model":"freshness-text","messages":[{"role":"user","content":%q}]}`, mode)
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/v1/chat/completions", strings.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+key.Secret)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	firstResult := make(chan *httptest.ResponseRecorder, 1)
	firstStarted = true
	go func() {
		defer close(finished)
		firstResult <- invoke("first")
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("original native dispatch did not reach its barrier")
	}
	// The first request started before this boundary but finishes after it.
	// A comparison query therefore assigns it by start, not completion time.
	split := time.Now().UTC()
	t.Log("freshness: admitted native barrier; persisted JSON/CSV remain empty")
	assertEmpty(readReport(router, cookie, admin.User.ID, filters))
	assertEmpty(readReport(router, emptyCookie, emptyActor.ID, filters))
	t.Log("freshness: real reviewed price and same-denomination FX writes while native call is in flight")
	rates[0].Amount = "3"
	page = writeRates()
	page = writePrice("/api/v1/admin/prices/currency", WritePricingCurrencyRequest{ETag: page.ETag, Currency: pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "2"}}})
	newETag := page.ETag
	if newETag == oldETag {
		t.Fatal("reviewed price/FX writes failed to advance their generation")
	}
	releaseOnce.Do(func() { close(release) })
	var first *httptest.ResponseRecorder
	select {
	case first = <-firstResult:
	case <-ctx.Done():
		t.Fatal("native barrier did not complete after price publication")
	}
	t.Log("freshness: original completion then new-generation native call")
	responses := []*httptest.ResponseRecorder{first, invoke("second")}
	// Denomination changes are allowed only after the two known calls settle.
	// An in-flight unknown monetary hold must never be bypassed for setup.
	t.Log("freshness: settled-known denomination cutover, zero/unknown native counters and explicit-free rates")
	page = writePrice("/api/v1/admin/prices/currency", WritePricingCurrencyRequest{ETag: readPrice().ETag, Currency: pricing.FX{PlatformCurrency: "EUR", Rates: map[string]string{"USD": "2"}}})
	eurETag := page.ETag
	for _, mode := range []string{"zero", "unknown"} {
		responses = append(responses, invoke(mode))
	}
	for index := range rates {
		rates[index].Amount = "0"
	}
	freeETag := writeRates().ETag
	responses = append(responses, invoke("free"))
	requestIDs := make([]string, len(responses))
	for index, response := range responses {
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"finish_reason":"stop"`) {
			t.Fatalf("native mode %d: HTTP%d %s", index, response.Code, response.Body.String())
		}
		requestIDs[index] = response.Header().Get("X-Request-ID")
		if !strings.HasPrefix(requestIDs[index], "req_") {
			t.Fatal("native completion lost canonical request identity")
		}
	}
	if dispatched.Load() != 5 || len(slices.Compact(slices.Sorted(slices.Values(requestIDs)))) != 5 {
		t.Fatal("native fixture dispatched/replayed an unexpected number of calls")
	}
	t.Log("freshness: all five native completions queued; automatic delivery remains paused")
	assertEmpty(readReport(router, cookie, admin.User.ID, filters))
	assertEmpty(readReport(router, emptyCookie, emptyActor.ID, filters))
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	// Model a real delivery transaction committed just before journal Ack.
	// Use the exact native payload, without editing its times, usage or receipt.
	queue, err := eventqueue.Open(journal, 4096, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	entries, readErr := queue.Read(64)
	closeErr := queue.Close()
	if readErr != nil || closeErr != nil || len(entries) != 5 {
		t.Fatalf("closed native journal: entries=%d read=%v close=%v", len(entries), readErr, closeErr)
	}
	var last service.CallFact
	journalFacts := make(map[string]service.CallFact, len(entries))
	for _, entry := range entries {
		var fact service.CallFact
		if err := json.Unmarshal(entry.Payload, &fact); err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{key.Secret, "test-only-freshness-provider-secret", "completed text"} {
			if bytes.Contains(entry.Payload, []byte(forbidden)) {
				t.Fatal("journal stored native content or a credential secret")
			}
		}
		if entry.ID != fact.RequestID || !slices.Contains(requestIDs, fact.RequestID) || len(fact.Attempts) != 1 || fact.Attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("journal contained fabricated, duplicate or nonterminal facts")
		}
		journalFacts[fact.RequestID] = fact
		if fact.RequestID == requestIDs[4] {
			last = fact
		}
	}
	if last.RequestID == "" {
		t.Fatal("last real native completion missing from journal")
	}
	t.Log("freshness: exact last native payload committed without journal acknowledgement")
	if err := svc.RecordCall(ctx, last); err != nil {
		t.Fatal(err)
	}
	partial := readReport(router, cookie, admin.User.ID, filters)
	if partial.Current.Summary.Requests != 1 || partial.LatestCompletedAt == nil || partial.Current.Summary.Tokens.Total.Value == nil || *partial.Current.Summary.Tokens.Total.Value != "9" {
		t.Fatalf("selected persisted subset was not exact: %+v", partial)
	}
	assertEmpty(readReport(router, emptyCookie, emptyActor.ID, filters))
	var accepted entity.CallRecord
	if err := db.First(&accepted, "request_id = ?", last.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	t.Log("freshness: fresh Service and actual journal replay; earlier arrivals do not advance selected latest completion")
	restarted := newService()
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	pauseRecorder(restarted)
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	restartRouter := fox.New()
	New(restarted).RegisterRoutes(restartRouter)
	full := readReport(restartRouter, cookie, admin.User.ID, filters)
	assertEmpty(readReport(restartRouter, emptyCookie, emptyActor.ID, filters))
	if full.Current.Summary.Requests != 5 || full.Current.Summary.Successes != 5 || full.Current.Summary.Tokens.Total.Value != nil || full.Current.Summary.Tokens.Total.Known != "3000009" || full.Current.Summary.Tokens.Total.UnknownCalls != 1 || full.Current.Summary.UnknownAmountCalls != 1 || full.LatestCompletedAt == nil || !full.LatestCompletedAt.Equal(*partial.LatestCompletedAt) || full.Previous.Summary.Requests != 0 {
		t.Fatalf("late durable delivery fabricated usage/ingestion completeness: %+v", full)
	}
	wantAmounts := []service.UsageAmount{{Currency: "CNY", Amount: "35.6", Calls: 2}, {Currency: "EUR", Amount: "0", Calls: 2}}
	if !reflect.DeepEqual(full.Current.Summary.Amounts, wantAmounts) || !reflect.DeepEqual(full.Current.Summary.PricingStatuses, map[string]int64{"priced": 4, "unknown_usage": 1}) {
		t.Fatalf("historical charges were converted or unknown treated as free: %+v", full.Current.Summary)
	}
	var records []entity.CallRecord
	var attempts []entity.CallAttempt
	if err := db.Order("request_id").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 5 || len(attempts) != 5 {
		t.Fatalf("replay changed canonical cardinality: records=%d attempts=%d", len(records), len(attempts))
	}
	byID := make(map[string]entity.CallRecord, len(records))
	for _, record := range records {
		byID[record.RequestID] = record
		native := journalFacts[record.RequestID]
		if native.Pricing == nil || record.PricingSnapshotJSON == nil || native.Pricing.SnapshotJSON == nil || *record.PricingSnapshotJSON != *native.Pricing.SnapshotJSON || native.SnapshotID != record.SnapshotID || native.Attempts[0].ID == "" {
			t.Fatal("delivery recomputed the captured native pricing or routing basis")
		}
		if record.UserID != admin.User.ID || record.KeyID != key.Record.Key.ID || record.ModelID != model.Model.ID || record.TeamID != "" || record.ProjectID != "" || record.SnapshotID == "" {
			t.Fatalf("native immutable source attribution changed: %+v", record)
		}
	}
	if !reflect.DeepEqual(byID[last.RequestID], accepted) {
		t.Fatal("commit-before-Ack replay replaced first accepted native record")
	}
	for _, attempt := range attempts {
		record := byID[attempt.RequestID]
		if attempt.ID != journalFacts[attempt.RequestID].Attempts[0].ID || !strings.HasPrefix(attempt.ID, "att_") || attempt.SnapshotID != record.SnapshotID || attempt.CredentialID != credentialID || attempt.ProviderModelID != pm.ID || attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" {
			t.Fatalf("native terminal attempt snapshot was lost: %+v", attempt)
		}
	}
	assertPrice := func(record entity.CallRecord, etag, currency, amount, inputAmount, exchange string) {
		t.Helper()
		if record.PriceETag != etag || record.PricingStatus != "priced" || record.ChargeAmount == nil || *record.ChargeAmount != amount || record.ChargeCurrency == nil || *record.ChargeCurrency != currency || record.PricingSnapshotJSON == nil {
			t.Fatalf("immutable price receipt changed: %+v", record.CallPricingFields)
		}
		var snapshot struct {
			Basis *service.CallPriceBasis `json:"basis"`
		}
		if err := json.Unmarshal([]byte(*record.PricingSnapshotJSON), &snapshot); err != nil || snapshot.Basis == nil || snapshot.Basis.ETag != etag || snapshot.Basis.Currency.PlatformCurrency != currency || snapshot.Basis.Currency.Rates["USD"] != exchange {
			t.Fatalf("captured price/FX basis missing: %v %+v", err, snapshot.Basis)
		}
		want := map[string]string{pricing.Input: inputAmount, pricing.Output: "4", pricing.CacheRead: "0.5", pricing.CacheWrite: "3"}
		if inputAmount == "0" {
			for metric := range want {
				want[metric] = "0"
			}
		}
		if snapshot.Basis.Schedule.ProviderModelID != pm.ID || snapshot.Basis.Schedule.PriceID == "" || snapshot.Basis.Schedule.Protocol != entity.ProtocolOpenAIChat || len(snapshot.Basis.Schedule.Rates) != len(want) {
			t.Fatal("captured complete supplier schedule identity was lost")
		}
		for _, rate := range snapshot.Basis.Schedule.Rates {
			amount, exists := want[rate.Metric]
			if !exists || rate.Tier != pricing.Base || rate.Unit != pricing.Unit || rate.Currency != "USD" || rate.Amount != amount || !rate.Enabled || rate.ID == "" {
				t.Fatal("captured supplier rate was replaced by the current catalogue")
			}
			delete(want, rate.Metric)
		}
		if len(want) != 0 {
			t.Fatal("captured supplier rate set was incomplete")
		}
	}
	original, updated, zero, unknown, free := byID[requestIDs[0]], byID[requestIDs[1]], byID[requestIDs[2]], byID[requestIDs[3]], byID[requestIDs[4]]
	assertPrice(original, oldETag, "CNY", "26.6", "2", "7")
	assertPrice(updated, newETag, "CNY", "9", "3", "2")
	assertPrice(zero, eurETag, "EUR", "0", "3", "2")
	assertPrice(free, freeETag, "EUR", "0", "0", "2")
	if original.SnapshotID == updated.SnapshotID || zero.InputTokens == nil || *zero.InputTokens != 0 || zero.OutputTokens == nil || *zero.OutputTokens != 0 || unknown.InputTokens != nil || unknown.OutputTokens != nil || unknown.ChargeAmount != nil || unknown.PricingStatus != "unknown_usage" || unknown.PriceETag != eurETag {
		t.Fatal("known zero, unknown terminal usage or published generations were conflated")
	}
	t.Log("freshness: current/previous half-open native starts and complete JSON/CSV projection")
	comparison := filters.Clone()
	comparison.Set("from", split.Format(time.RFC3339Nano))
	compared := readReport(restartRouter, cookie, admin.User.ID, comparison)
	if compared.Current.Summary.Requests != 4 || compared.Previous.Summary.Requests != 1 || !original.StartedAt.Before(split) || !original.CompletedAt.After(split) || compared.Previous.Summary.Tokens.Total.Known != "1500000" || !reflect.DeepEqual(compared.Previous.Summary.Amounts, []service.UsageAmount{{Currency: "CNY", Amount: "26.6", Calls: 1}}) {
		t.Fatalf("half-open started_at comparison used completion/delivery time: %+v", compared)
	}
	if err := restarted.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	restarted.StopRuntime()
	if err := restarted.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	t.Log("freshness: second fresh Service restart and repeat flush preserve exact records/attempts without native replay")
	again := newService()
	if err := again.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	pauseRecorder(again)
	if err := again.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	againRouter := fox.New()
	New(again).RegisterRoutes(againRouter)
	afterRestart := readReport(againRouter, cookie, admin.User.ID, filters)
	afterCompare := readReport(againRouter, cookie, admin.User.ID, comparison)
	if !reflect.DeepEqual(full.Current, afterRestart.Current) || !reflect.DeepEqual(full.Previous, afterRestart.Previous) || afterRestart.LatestCompletedAt == nil || !full.LatestCompletedAt.Equal(*afterRestart.LatestCompletedAt) || !reflect.DeepEqual(compared.Current, afterCompare.Current) || !reflect.DeepEqual(compared.Previous, afterCompare.Previous) || dispatched.Load() != 5 {
		t.Fatal("flush/restart changed historical report facts or replayed native POSTs")
	}
	var replayRecords []entity.CallRecord
	var replayAttempts []entity.CallAttempt
	if err := db.Order("request_id").Find(&replayRecords).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&replayAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records, replayRecords) || !reflect.DeepEqual(attempts, replayAttempts) {
		t.Fatal("restart replaced immutable request/attempt IDs or pricing snapshots")
	}
}

func usageFreshnessResponse(mode string) string {
	usage := `"prompt_tokens":1000000,"completion_tokens":500000,"prompt_tokens_details":{"cached_tokens":200000,"cache_write_tokens":100000}`
	switch mode {
	case "zero":
		usage = `"prompt_tokens":0,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}`
	case "unknown":
		usage = `"prompt_tokens":null,"completion_tokens":null,"prompt_tokens_details":{"cached_tokens":null,"cache_write_tokens":null}`
	case "free":
		usage = `"prompt_tokens":6,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}`
	}
	return fmt.Sprintf(`{"object":"chat.completion","model":"native-freshness","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"completed text"}}],"usage":{%s}}`, usage)
}

func TestUsageFreshnessControlledResponses(t *testing.T) {
	for _, mode := range []string{"first", "second", "zero", "unknown", "free"} {
		t.Run(mode, func(t *testing.T) {
			observed := parseGatewayUsage([]byte(usageFreshnessResponse(mode)))
			if !observed.Complete || observed.NativeCompletionEvidence != "completed" {
				t.Fatal("controlled fixture response lacks native terminal usage evidence")
			}
			if mode == "unknown" {
				if observed.Input != nil || observed.Output != nil || observed.CacheRead != nil || observed.CacheWrite != nil {
					t.Fatal("explicit unknown counters became fabricated zero")
				}
				return
			}
			input, output := int64(1000000), int64(500000)
			switch mode {
			case "zero":
				input, output = 0, 0
			case "free":
				input, output = 6, 3
			}
			if observed.Input == nil || *observed.Input != input || observed.Output == nil || *observed.Output != output || observed.CacheRead == nil || observed.CacheWrite == nil {
				t.Fatal("controlled fixture response lost authoritative exact counters")
			}
		})
	}
}
