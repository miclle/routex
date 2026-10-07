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
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type personalKeyMonthlyBehaviorFaultContext struct{}

// Callbacks are installed before workers start. Only immutable target pointers
// and atomic fault state change while the shared GORM processor is executing.
type personalKeyMonthlyBehaviorFaults struct {
	auditRoot       atomic.Pointer[string]
	publicationRoot atomic.Pointer[string]
	committed       atomic.Bool
}

func (f *personalKeyMonthlyBehaviorFaults) beforeCreate(tx *gorm.DB) {
	root := f.auditRoot.Load()
	if root == nil || tx.Statement.Table != "audit_events" {
		return
	}
	if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "limits.update" && row.ResourceID == *root {
		_ = tx.AddError(errors.New("controlled audit persistence failure"))
	}
}

func (f *personalKeyMonthlyBehaviorFaults) afterCreate(tx *gorm.DB) {
	root := f.publicationRoot.Load()
	if root == nil || tx.Error != nil || tx.Statement.Table != "audit_events" || tx.Statement.Context.Value(personalKeyMonthlyBehaviorFaultContext{}) != true {
		return
	}
	if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "limits.update" && row.ResourceID == *root {
		f.committed.Store(true)
	}
}

func (f *personalKeyMonthlyBehaviorFaults) beforeQuery(tx *gorm.DB) {
	if f.publicationRoot.Load() != nil && f.committed.Load() && tx.Statement.Context.Value(personalKeyMonthlyBehaviorFaultContext{}) == true {
		_ = tx.AddError(errors.New("controlled postcommit publication read failure"))
	}
}

// Proposed case139 follows the exact Vault V72 / 137-case predecessor.
// All native facts come from the existing native parser and real admission path.
func testPersonalKeyMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationBarrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &publicationBarrier)
	const publicationCallback = "test:personal-key-behavior-observation"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, publicationBarrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationBarrier.armed.Store(false)
		if err := db.Callback().Query().Remove(publicationCallback); err != nil {
			t.Error(err)
		}
	}()
	var faults personalKeyMonthlyBehaviorFaults
	const auditCallback = "test:monthly-behavior-audit-rollback"
	const armCallback = "test:monthly-behavior-publication-arm"
	const readCallback = "test:monthly-behavior-publication-read"
	if err := db.Callback().Create().Before("gorm:create").Register(auditCallback, faults.beforeCreate); err != nil {
		t.Fatal(err)
	}
	defer func() {
		faults.auditRoot.Store(nil)
		if err := db.Callback().Create().Remove(auditCallback); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Callback().Create().After("gorm:create").Register(armCallback, faults.afterCreate); err != nil {
		t.Fatal(err)
	}
	defer func() {
		faults.publicationRoot.Store(nil)
		if err := db.Callback().Create().Remove(armCallback); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Callback().Query().Before("gorm:query").Register(readCallback, faults.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		faults.publicationRoot.Store(nil)
		if err := db.Callback().Query().Remove(readCallback); err != nil {
			t.Error(err)
		}
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{103}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int64
	var hold, unknown atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	var nativeRequests sync.WaitGroup
	defer releaseOnce.Do(func() { close(release) })
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if hold.Load() {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"key-monthly-behavior-native","choices":[{"finish_reason":"stop"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"key-monthly-behavior-native","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer func() { releaseOnce.Do(func() { close(release) }); nativeRequests.Wait(); upstream.Close() }()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"key-behavior-admin@example.invalid","password":"monthly-behavior-password","name":"Behavior admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	var memberIdentity SessionResponse
	var memberCookie *http.Cookie
	request := func(method, path string, body any, etag string, fault bool) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if strings.HasPrefix(path, "/api/v1/keys") {
			if memberCookie == nil {
				t.Fatal("Personal Key controls require the exact owner Session")
			}
			req.Header.Set("X-CSRF-Token", memberIdentity.CSRFToken)
			req.AddCookie(memberCookie)
		} else {
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
			req.AddCookie(cookie)
		}
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		if fault {
			req = req.WithContext(context.WithValue(req.Context(), personalKeyMonthlyBehaviorFaultContext{}, true))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	const modelID = "mdl_behavior73"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	cipher, err := store.Seal("crd_behavior73", "private-upstream-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_behavior73", Name: "Behavior provider"},
		&entity.ProviderConnection{ID: "con_behavior73", ProviderID: "prv_behavior73", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_behavior73", ConnectionID: "con_behavior73", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_behavior73", ConnectionID: "con_behavior73", UpstreamName: "key-monthly-behavior-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_behavior73", ProviderModelID: "pmd_behavior73"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "key-monthly-behavior-model", ModelID: modelID, CurrentModelID: func() *string { x := modelID; return &x }()},
		&entity.ModelProviderBinding{ID: "bnd_behavior73", ModelID: modelID, ProviderModelID: "pmd_behavior73", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_behavior_admin", UserID: admin.User.ID, Name: "Warm coverage", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_behavior_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_behavior73", ProviderModelID: "pmd_behavior73", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: []string{"rate_behavior_in", "rate_behavior_out", "rate_behavior_read", "rate_behavior_write"}[i], ModelPriceID: "price_behavior73", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.000000000001", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "monthly-behavior.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		nativeRequests.Wait()
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	call := func(bearer string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "/v1/chat/completions", strings.NewReader(`{"model":"key-monthly-behavior-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":50}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	// The first genuine unbounded admission activates coverage before the new User.
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "key-behavior-member@example.invalid", "monthly-behavior-password", "Behavior member", "member")
	if err != nil {
		t.Fatal(err)
	}
	memberLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"key-behavior-member@example.invalid","password":"monthly-behavior-password"}`, nil, "")
	expectStatus(t, memberLogin, http.StatusOK)
	memberIdentity, memberCookie = readIdentity(t, memberLogin)
	if memberIdentity.User.ID != member.User.ID {
		t.Fatal("Key review Session must belong to the immutable Key owner")
	}
	if err := db.Create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	created := decodeCatalogResponse[CreatedKeyResponse](t, request("POST", "/api/v1/keys", map[string]any{"name": "Independent Personal Key", "model_ids": []string{modelID}}, "", false), 201)
	memberBearer := created.Secret
	if created.Key.ID == "" || memberBearer == "" {
		t.Fatal("normal Key creation returned no transient secret")
	}
	expectStatus(t, request("POST", "/api/v1/keys/"+created.Key.ID+"/confirm", nil, "", false), 200)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	boundPath := "/api/v1/admin/provider-models/pmd_behavior73/reservation-bound"
	bound := decodeCatalogResponse[service.ReservationBoundRecord](t, request("PUT", boundPath, map[string]any{"max_input_tokens": 100, "max_output_tokens": 50, "evidence": "Controlled native fixture", "reason": "Finite reservation"}, "0", false), 200)
	if !bound.Configured {
		t.Fatal("real capacity unavailable")
	}
	parentUserPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	primaryKeyPath := "/api/v1/keys/" + created.Key.ID + "/limits"
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, "", false), 200)
	}
	write := func(path string, body map[string]any) service.LimitRecord {
		body["reason"] = "Reviewed independent behavior"
		return decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, body, read(path).ETag, false), 200)
	}
	canonical := func(record service.LimitRecord, token, money string) {
		t.Helper()
		if record.Stored.TokensMonthBehavior != token || record.Stored.MoneyMonthBehavior != money || len(record.IPPolicies) != 2 || record.IPPolicies[1].TokensMonthBehavior != token || record.IPPolicies[1].MoneyMonthBehavior != money {
			t.Fatal("canonical Personal Key modes missing")
		}
		raw, _ := json.Marshal(record.Effective)
		if bytes.Contains(raw, []byte("behavior")) {
			t.Fatal("effective projected synthetic behavior")
		}
	}
	canonical(read(primaryKeyPath), "stop", "stop")
	body := map[string]any{"tokens_month": 100, "tokens_month_behavior": "alert_only", "money_month": nil, "money_month_behavior": "stop", "reason": "Original exact reviewed soft policy"}
	original := read(primaryKeyPath)
	applied := decodeCatalogResponse[service.LimitRecord](t, request("PUT", primaryKeyPath, body, original.ETag, false), 200)
	canonical(applied, "alert_only", "stop")
	if !applied.Enforced {
		t.Fatal("current mode publication not confirmed")
	}
	parent := write(parentUserPath, map[string]any{"tokens_month": 200})
	key := read(primaryKeyPath)
	if parent.Stored.TokensMonthBehavior != "stop" || key.IPPolicies[0].TokensMonthBehavior != "stop" || key.Stored.TokensMonthBehavior != "alert_only" || key.Effective.TokensMonth == nil || *key.Effective.TokensMonth != 100 {
		t.Fatal("independent hard User/soft Key projection")
	}
	for _, mode := range []any{nil, "ALERT_ONLY", "alert_only ", []string{"stop"}} {
		expectStatus(t, request("PUT", primaryKeyPath, map[string]any{"tokens_month_behavior": mode, "reason": "Invalid"}, applied.ETag, false), 400)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "key-behavior-outsider@example.invalid", "monthly-behavior-password", "Other owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsiderAuth, outsiderCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"key-behavior-outsider@example.invalid","password":"monthly-behavior-password"}`, nil, ""))
	if outsiderAuth.User.ID != outsider.User.ID {
		t.Fatal("wrong outsider Session")
	}
	denied := httptest.NewRequest("PUT", primaryKeyPath, strings.NewReader(`{"tokens_month_behavior":"alert_only","reason":"Wrong owner"}`))
	denied.Header.Set("Content-Type", "application/json")
	denied.Header.Set("X-CSRF-Token", outsiderAuth.CSRFToken)
	denied.Header.Set("If-Match", `"`+applied.ETag+`"`)
	denied.AddCookie(outsiderCookie)
	deniedResult := httptest.NewRecorder()
	router.ServeHTTP(deniedResult, denied)
	expectStatus(t, deniedResult, 404)
	expectStatus(t, call(memberBearer), 200)
	usage := read(primaryKeyPath)
	if usage.QuotaUsage == nil || usage.QuotaUsage.Month.TokensUsed != 150 || usage.QuotaUsage.Active.TokensHeld != 0 {
		t.Fatal("soft account lost settled/hold proof")
	}
	before := dispatches.Load()
	expectStatus(t, call(memberBearer), 429)
	if dispatches.Load() != before {
		t.Fatal("soft Key bypassed hard User")
	}
	// Exact identical retry has no extra revision/audit and still needs publication.
	countAudit := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "key", created.Key.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	auditBefore := countAudit()
	retry := decodeCatalogResponse[service.LimitRecord](t, request("PUT", primaryKeyPath, body, original.ETag, false), 200)
	if retry.ETag != applied.ETag || countAudit() != auditBefore || !retry.Enforced {
		t.Fatal("identical mode retry duplicated or unconfirmed")
	}
	changed := map[string]any{"tokens_month": 100, "tokens_month_behavior": "stop", "reason": "Reject stale different behavior"}
	expectStatus(t, request("PUT", primaryKeyPath, changed, original.ETag, false), 409)
	if read(primaryKeyPath).ETag != applied.ETag {
		t.Fatal("conflict rewrote policy")
	}
	// A soft User parent must not suppress the independent hard Key.
	write(parentUserPath, map[string]any{"tokens_month": 100, "tokens_month_behavior": "alert_only"})
	write(primaryKeyPath, map[string]any{"tokens_month": 200})
	if got := read(primaryKeyPath); got.Effective.TokensMonth == nil || *got.Effective.TokensMonth != 100 || got.IPPolicies[0].TokensMonthBehavior != "alert_only" || got.Stored.TokensMonthBehavior != "stop" {
		t.Fatal("mixed parent numeric minimum changed")
	}
	expectStatus(t, call(memberBearer), 429)
	// Full-policy omission resets hard stop, including previously saved soft mode.
	hard := write(primaryKeyPath, map[string]any{"tokens_month": 100})
	canonical(hard, "stop", "stop")
	expectStatus(t, call(memberBearer), 429)
	// Money and Token behaviors are independent. Clear the Key cap before zero tests.
	write(parentUserPath, map[string]any{})
	write(primaryKeyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "stop"})
	expectStatus(t, call(memberBearer), 429)
	write(primaryKeyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "stop", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	expectStatus(t, call(memberBearer), 429)
	both := write(primaryKeyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	canonical(both, "alert_only", "alert_only")
	expectStatus(t, call(memberBearer), 200)
	usage = read(primaryKeyPath)
	if usage.QuotaUsage.Month.TokensUsed != 300 || usage.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000003" || usage.QuotaUsage.Month.MoneyUnknown != 0 {
		t.Fatal("soft exact settlement changed", usage.QuotaUsage)
	}
	// Typed audit failure rolls both modes/revision back in the same transaction.
	auditRoot := created.Key.ID
	faults.auditRoot.Store(&auditRoot)
	expectStatus(t, request("PUT", primaryKeyPath, map[string]any{"tokens_month_behavior": "stop", "money_month_behavior": "stop", "reason": "Atomic rollback"}, both.ETag, false), 500)
	faults.auditRoot.Store(nil)
	if got := read(primaryKeyPath); got.ETag != both.ETag || got.Stored.TokensMonthBehavior != "alert_only" || got.Stored.MoneyMonthBehavior != "alert_only" {
		t.Fatal("failed audit committed behavior")
	}
	// A scoped read fault happens only after the genuine audit insert. It forces
	// publication unavailable without disabling the live worker or changing SQL.
	publicationRoot := created.Key.ID
	faults.committed.Store(false)
	faults.publicationRoot.Store(&publicationRoot)
	faultBody := map[string]any{"tokens_month": nil, "tokens_month_behavior": "alert_only", "money_month": nil, "money_month_behavior": "alert_only", "reason": "Original uncertain null-cap request"}
	expectStatus(t, request("PUT", primaryKeyPath, faultBody, both.ETag, true), 503)
	if !faults.committed.Load() {
		t.Fatal("failure did not follow genuine commit")
	}
	faults.publicationRoot.Store(nil)
	var persisted entity.ResourceLimit
	if err := db.Take(&persisted, "scope_kind = ? AND scope_id = ?", "key", created.Key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ETag == both.ETag || persisted.TokensMonth != nil || persisted.MoneyMonth != nil || persisted.TokensMonthBehavior != "alert_only" || persisted.MoneyMonthBehavior != "alert_only" {
		t.Fatal("uncertain current configuration not exact")
	}
	uncertainAudit := countAudit()
	confirmed := decodeCatalogResponse[service.LimitRecord](t, request("PUT", primaryKeyPath, faultBody, both.ETag, false), 200)
	canonical(confirmed, "alert_only", "alert_only")
	if confirmed.ETag != persisted.ETag || !confirmed.Enforced || countAudit() != uncertainAudit {
		t.Fatal("uncertain exact retry not confirmed/noop")
	}
	// Null caps retain saved inert modes; omission resets both to stop.
	expectStatus(t, call(memberBearer), 200)
	omitted := write(primaryKeyPath, map[string]any{})
	canonical(omitted, "stop", "stop")
	if omitted.Stored.TokensMonth != nil || omitted.Stored.MoneyMonth != nil {
		t.Fatal("omitted caps fabricated capacity")
	}
	expectStatus(t, call(memberBearer), 200)
	// Thresholds use exact settled evidence even under a soft Key policy.
	warning80 := write(primaryKeyPath, map[string]any{"tokens_month": 750, "tokens_month_behavior": "alert_only", "money_month": "0.00000000000000075", "money_month_behavior": "alert_only", "currency": "USD"})
	personalKeyBehaviorAssertWarnings(t, svc, &publicationBarrier, db, member.User.ID, created.Key.ID, warning80.ETag, 80)
	warning90 := write(primaryKeyPath, map[string]any{"tokens_month": 660, "tokens_month_behavior": "alert_only", "money_month": "0.00000000000000066", "money_month_behavior": "alert_only", "currency": "USD"})
	personalKeyBehaviorAssertWarnings(t, svc, &publicationBarrier, db, member.User.ID, created.Key.ID, warning90.ETag, 90)
	ownerPage, err := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	marked := false
	for _, item := range ownerPage.Items {
		if item.QuotaWarning != nil && item.SubjectID == created.Key.ID {
			if _, err := svc.MarkNotificationRead(ctx, member.User.ID, item.ID); err != nil {
				t.Fatal(err)
			}
			marked = true
			break
		}
	}
	if !marked {
		t.Fatal("no sole-self warning to mark")
	}
	for _, forbidden := range []string{admin.User.ID, outsider.User.ID} {
		page, err := svc.ListNotifications(ctx, forbidden, service.NotificationFilter{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if item.QuotaWarning != nil && item.SubjectID == created.Key.ID {
				t.Fatal("warning owner/admin override")
			}
		}
	}
	// The replacement retains its exact immutable policy root and sole owner.
	rotated := decodeCatalogResponse[CreatedKeyResponse](t, request("POST", "/api/v1/keys/"+created.Key.ID+"/rotate", nil, "", false), 201)
	expectStatus(t, request("POST", "/api/v1/keys/"+rotated.Key.ID+"/confirm", nil, "", false), 200)
	replacementPath := "/api/v1/keys/" + rotated.Key.ID + "/limits"
	if got := read(replacementPath); got.ETag != warning90.ETag || got.AccountID != "key_"+created.Key.ID || got.Stored.TokensMonthBehavior != "alert_only" || got.QuotaUsage.Month.TokensUsed != 600 {
		t.Fatal("rotation changed policy root/accounting", got)
	}
	expectStatus(t, call(rotated.Secret), 200)
	confirmed = read(primaryKeyPath)
	canonical(confirmed, "alert_only", "alert_only")
	if confirmed.QuotaUsage.Month.TokensUsed != 750 || confirmed.QuotaUsage.Month.MoneyUsed["USD"] != "0.00000000000000075" {
		t.Fatal("replacement settlement did not share root")
	}
	if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	var retainedWarnings int64
	if err := db.Model(&entity.PersonalKeyQuotaWarningObservation{}).Where("root_key_id = ?", created.Key.ID).Count(&retainedWarnings).Error; err != nil || retainedWarnings != 4 {
		t.Fatal("at/above100 fabricated warning or rewrote history", err, retainedWarnings)
	}
	// A separate soft root keeps active reservations and unknown terminal
	// coverage distinct from known settled totals, rather than inventing capacity.
	coverage := decodeCatalogResponse[CreatedKeyResponse](t, request("POST", "/api/v1/keys", map[string]any{"name": "Held and unknown root", "model_ids": []string{modelID}}, "", false), 201)
	expectStatus(t, request("POST", "/api/v1/keys/"+coverage.Key.ID+"/confirm", nil, "", false), 200)
	coveragePath := "/api/v1/keys/" + coverage.Key.ID + "/limits"
	write(coveragePath, map[string]any{"tokens_month": 250, "tokens_month_behavior": "alert_only"})
	hold.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	nativeRequests.Add(1)
	go func() { defer nativeRequests.Done(); heldResponse <- call(coverage.Secret) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("held native timeout")
	}
	held := read(coveragePath).QuotaUsage
	if held == nil || held.Month == nil || held.Active == nil || held.Month.TokensUsed != 0 || held.Active.TokensHeld != 150 {
		t.Fatal("soft policy lost active reservation", held)
	}
	if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	personalKeyBehaviorNoWarnings(t, db, coverage.Key.ID)
	releaseOnce.Do(func() { close(release) })
	hold.Store(false)
	select {
	case response := <-heldResponse:
		expectStatus(t, response, 200)
	case <-time.After(15 * time.Second):
		t.Fatal("held native did not complete")
	}
	unknown.Store(true)
	expectStatus(t, call(coverage.Secret), 200)
	unknown.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	write(coveragePath, map[string]any{"tokens_month": 187, "tokens_month_behavior": "alert_only", "money_month": "0.000000000000000187", "money_month_behavior": "alert_only", "currency": "USD"})
	unknownUsage := read(coveragePath).QuotaUsage
	if unknownUsage == nil || unknownUsage.Month == nil || unknownUsage.Month.TokensUsed != 150 || unknownUsage.Month.TokensHeld != 150 || unknownUsage.Month.TokensUnknown != 0 || unknownUsage.Month.MoneyUnknown != 1 || unknownUsage.Month.MoneyUsed["USD"] != "0.00000000000000015" || len(unknownUsage.Month.MoneyHeld) != 0 {
		t.Fatal("unknown terminal evidence fabricated money bound/known usage", unknownUsage)
	}
	if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	personalKeyBehaviorNoWarnings(t, db, coverage.Key.ID)
	unknownResponse := call(coverage.Secret)
	expectStatus(t, unknownResponse, 503)
	var unknownError struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(unknownResponse.Body.Bytes(), &unknownError); err != nil || unknownError.Error.Code != "quota_usage_unknown" {
		t.Fatal("unknown money changed fail-closed error contract", err, unknownError.Error.Code)
	}
	personalKeyBehaviorProjectNegative(t, ctx, svc, db, admin.User.ID, member.User.ID, modelID, request, call)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var warningHistory []entity.PersonalKeyQuotaWarningObservation
	var warningInbox []entity.PersonalKeyQuotaWarningInbox
	if err := db.Order("id").Find(&warningHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&warningInbox).Error; err != nil {
		t.Fatal(err)
	}
	var calls []entity.CallRecord
	var attempts []entity.CallAttempt
	if err := db.Order("request_id").Find(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&attempts).Error; err != nil {
		t.Fatal(err)
	}
	if dispatches.Load() != 8 || len(attempts) != 8 || len(calls) != 15 {
		t.Fatal("unexpected native dispatch count", dispatches.Load(), len(attempts))
	}
	allDenied, unknownDenied := 0, 0
	for _, row := range calls {
		if row.ErrorCode == "quota_exceeded" {
			allDenied++
			if !personalKeyBehaviorBlockedFact(row, attempts) {
				t.Fatal("Project/unknown/root denial fabricated native or pricing facts")
			}
		}
		if row.ErrorCode == "quota_usage_unknown" {
			unknownDenied++
			if row.RequestID != unknownResponse.Header().Get("X-Request-ID") || row.KeyID != coverage.Key.ID || !personalKeyBehaviorBlockedFactCode(row, attempts, "quota_usage_unknown") {
				t.Fatal("unknown accounting denial fabricated native or pricing facts")
			}
		}
	}
	if allDenied != 6 || unknownDenied != 1 {
		t.Fatal("exact no-attempt denial counts", allDenied, unknownDenied)
	}
	memberCalls := 0
	blocked := 0
	for _, row := range calls {
		if row.UserID != member.User.ID || row.KeyID != created.Key.ID && row.KeyID != rotated.Key.ID {
			continue
		}
		memberCalls++
		if row.ErrorCode == "quota_exceeded" {
			blocked++
			if !personalKeyBehaviorBlockedFact(row, attempts) {
				t.Fatal("blocked pre-admission evidence fabricated")
			}
			for _, attempt := range attempts {
				if attempt.RequestID == row.RequestID {
					t.Fatal("denied call acquired native attempt")
				}
			}
		}
	}
	if memberCalls != 10 || blocked != 5 {
		t.Fatal("logical call/denial count", memberCalls, blocked)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	restarted := read(primaryKeyPath)
	canonical(restarted, "alert_only", "alert_only")
	if !restarted.Enforced || restarted.ETag != confirmed.ETag || restarted.QuotaUsage.Month.TokensUsed != 750 || restarted.QuotaUsage.Month.MoneyUsed["USD"] != "0.00000000000000075" {
		t.Fatal("restart reset modes/accounting")
	}
	var afterCalls []entity.CallRecord
	var afterAttempts []entity.CallAttempt
	if err := db.Order("request_id").Find(&afterCalls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterAttempts).Error; err != nil {
		t.Fatal(err)
	}
	var afterHistory []entity.PersonalKeyQuotaWarningObservation
	var afterInbox []entity.PersonalKeyQuotaWarningInbox
	if err := db.Order("id").Find(&afterHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterInbox).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warningHistory, afterHistory) || !reflect.DeepEqual(warningInbox, afterInbox) {
		t.Fatal("restart expanded warning recipients/history or reset read state")
	}
	if !reflect.DeepEqual(afterCalls, calls) || !reflect.DeepEqual(afterAttempts, attempts) || dispatches.Load() != 8 {
		t.Fatal("restart changed immutable facts or dispatched")
	}
}

// One untagged current-publication observation, with only the fixture-owned
// periodic publisher fenced; no observer retries or weakened lease checks.
func personalKeyBehaviorAssertWarnings(t *testing.T, svc *service.Service, barrier *personalKeyWarningFixturePublicationBarrier, db *gorm.DB, owner, root, revision string, threshold int) {
	t.Helper()
	ctx := context.Background()
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := barrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	var rows []entity.PersonalKeyQuotaWarningObservation
	if err := db.Where("root_key_id = ? AND policy_revision = ?", root, revision).Order("dimension").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal("both known settled dimensions must warn", threshold, len(rows))
	}
	for _, row := range rows {
		if row.OwnerID != owner || row.Threshold != threshold || row.ThresholdGeneration != "personal-key-monthly-80-90-v1" || row.OwnerCreatedAt.IsZero() || row.ResourceCreatedAt.IsZero() {
			t.Fatal("warning scope/threshold/private birth lost", row.Dimension)
		}
		if row.Dimension == "tokens" && row.Settled != "600" || row.Dimension == "money" && row.Settled != "0.0000000000000006" {
			t.Fatal("warning lost exact settled evidence", row.Dimension, row.Settled)
		}
		var inbox []entity.PersonalKeyQuotaWarningInbox
		if err := db.Where("observation_id = ?", row.ID).Find(&inbox).Error; err != nil || len(inbox) != 1 || inbox[0].RecipientID != owner || inbox[0].RecipientCreatedAt.IsZero() {
			t.Fatal("warning recipient expanded", err, len(inbox))
		}
	}
	page, err := svc.ListNotifications(ctx, owner, service.NotificationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, item := range page.Items {
		if item.SubjectID == root && item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == revision {
			found++
			raw, _ := json.Marshal(item)
			if bytes.Contains(raw, []byte("created_at")) || bytes.Contains(raw, []byte("owner_id")) {
				t.Fatal("private warning birth/owner fields exposed")
			}
		}
	}
	if found != 2 {
		t.Fatal("warning public inbox mismatch", found)
	}
}

func personalKeyBehaviorProjectNegative(t *testing.T, ctx context.Context, svc *service.Service, db *gorm.DB, admin, manager, model string, request func(string, string, any, string, bool) *httptest.ResponseRecorder, call func(string) *httptest.ResponseRecorder) {
	t.Helper()
	const project = "prj_behavior73"
	for _, row := range []any{&entity.Project{ID: project, Name: "Hard Project", Status: entity.ResourceActive, CreatorID: admin}, &entity.ProjectManager{ID: "pmg_behavior73", ProjectID: project, UserID: manager}, &entity.ProjectModelGrant{ProjectID: project, ModelID: model}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	created, err := svc.CreateProjectKey(ctx, manager, project, "Hard Project Key", "manual", []string{model}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmProjectKey(ctx, manager, project, created.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + project + "/keys/" + created.Record.Key.ID + "/limits"
	// Project routes need the same exact manager cookie as Personal routes.
	// This helper's default admin is independently authorized for Project limits.
	original := decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, "", false), 200)
	if original.Kind != "project_key" || original.ID != created.Record.Key.ID || original.AccountID != "key_"+created.Record.Key.ID || original.Stored.TokensMonthBehavior != "stop" || original.Stored.MoneyMonthBehavior != "stop" || len(original.IPPolicies) != 2 || original.IPPolicies[1].TokensMonthBehavior != "stop" || original.IPPolicies[1].MoneyMonthBehavior != "stop" {
		t.Fatal("Project Key default lost canonical independent stop modes")
	}
	expectStatus(t, request("PUT", path, map[string]any{"tokens_month_behavior": "alert_only ", "reason": "Reject noncanonical Project Key mode"}, original.ETag, false), 400)
	hard := decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, map[string]any{"tokens_month": 0, "reason": "Explicit Project hard zero"}, original.ETag, false), 200)
	expectStatus(t, call(created.Secret), 429)
	var before entity.ResourceLimit
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&before, "scope_kind = ? AND scope_id = ?", "key", created.Record.Key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if hard.ETag != before.ETag {
		t.Fatal("Project hard policy revision mismatch")
	}
	// Current Project Keys may be soft, but only after exact immutable parent
	// identity is proved. A root born before its Project must never publish soft.
	var originalRoot entity.ProjectKey
	var originalProject entity.Project
	if err := db.Take(&originalRoot, "id = ?", created.Record.Key.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&originalProject, "id = ?", project).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Model(&entity.ProjectKey{}).Where("id = ?", originalRoot.ID).UpdateColumn("CreatedAt", originalRoot.CreatedAt).Error; err != nil {
			t.Error(err)
		}
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", created.Record.Key.ID).UpdateColumns(map[string]any{"TokensMonthBehavior": before.TokensMonthBehavior, "MoneyMonthBehavior": before.MoneyMonthBehavior}).Error; err != nil {
			t.Error(err)
		}
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", created.Record.Key.ID).UpdateColumn("TokensMonthBehavior", "alert_only").Error; err != nil {
		t.Fatal(err)
	}
	invalidBirth := originalProject.CreatedAt.Truncate(time.Second).Add(-time.Second)
	if err := db.Model(&entity.ProjectKey{}).Where("id = ?", originalRoot.ID).UpdateColumn("CreatedAt", invalidBirth).Error; err != nil {
		t.Fatal(err)
	}
	var invalidRoot entity.ProjectKey
	if err := db.Take(&invalidRoot, "id = ?", originalRoot.ID).Error; err != nil || !invalidRoot.CreatedAt.Equal(invalidBirth) || !invalidRoot.CreatedAt.Before(originalProject.CreatedAt) {
		t.Fatal("Project invalid birth was not stored exactly", err)
	}
	if err := svc.RefreshRuntime(ctx); err == nil {
		t.Fatal("unproved Project root birth published a soft policy")
	}
	if err := db.Model(&entity.ProjectKey{}).Where("id = ?", originalRoot.ID).UpdateColumn("CreatedAt", originalRoot.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", created.Record.Key.ID).UpdateColumn("TokensMonthBehavior", before.TokensMonthBehavior).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	var after entity.ResourceLimit
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&after, "scope_kind = ? AND scope_id = ?", "key", created.Record.Key.ID).Error; err != nil || !teamBehaviorSameLimit(before, after) {
		t.Fatal("Project negative changed original policy", err)
	}
	var restoredRoot entity.ProjectKey
	if err := db.Take(&restoredRoot, "id = ?", originalRoot.ID).Error; err != nil || !reflect.DeepEqual(restoredRoot, originalRoot) {
		t.Fatal("Project negative changed retained root identity", err)
	}
}

func personalKeyBehaviorNoWarnings(t *testing.T, db *gorm.DB, root string) {
	t.Helper()
	var n int64
	if err := db.Model(&entity.PersonalKeyQuotaWarningObservation{}).Where("root_key_id = ?", root).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("held/unknown usage became percentage warning", err, n)
	}
}

func personalKeyBehaviorBlockedFact(row entity.CallRecord, attempts []entity.CallAttempt) bool {
	return personalKeyBehaviorBlockedFactCode(row, attempts, "quota_exceeded")
}

func personalKeyBehaviorBlockedFactCode(row entity.CallRecord, attempts []entity.CallAttempt, code string) bool {
	if code != "quota_exceeded" && code != "quota_usage_unknown" {
		return false
	}
	if row.Status != "error" || row.ErrorCode != code || row.InputTokens != nil || row.OutputTokens != nil || row.PricingStatus != "not_captured" || row.ChargeAmount != nil || row.ChargeCurrency != nil || row.PricingSnapshotJSON != nil {
		return false
	}
	for _, attempt := range attempts {
		if attempt.RequestID == row.RequestID {
			return false
		}
	}
	return true
}

func TestPersonalKeyBehaviorFixtureBlockedFactsDoNotInventZero(t *testing.T) {
	row := entity.CallRecord{RequestID: "call_denied", Status: "error", ErrorCode: "quota_exceeded", CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	if !personalKeyBehaviorBlockedFact(row, nil) {
		t.Fatal("exact pre-admission denial rejected")
	}
	zero := int64(0)
	money := "0"
	fabricated := row
	fabricated.InputTokens = &zero
	if personalKeyBehaviorBlockedFact(fabricated, nil) {
		t.Fatal("fabricated zero usage accepted")
	}
	fabricated = row
	fabricated.PricingStatus = "priced"
	fabricated.ChargeAmount = &money
	if personalKeyBehaviorBlockedFact(fabricated, nil) {
		t.Fatal("fabricated pricing accepted")
	}
	if personalKeyBehaviorBlockedFact(row, []entity.CallAttempt{{RequestID: row.RequestID}}) {
		t.Fatal("denial with upstream attempt accepted")
	}
}

func TestPersonalKeyBehaviorFixtureUnknownAccountingRetainsExactError(t *testing.T) {
	row := entity.CallRecord{RequestID: "req_unknown", Status: "error", ErrorCode: "quota_usage_unknown", CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	if !personalKeyBehaviorBlockedFactCode(row, nil, "quota_usage_unknown") || personalKeyBehaviorBlockedFact(row, nil) || personalKeyBehaviorBlockedFactCode(row, nil, "quota_exceeded") || personalKeyBehaviorBlockedFactCode(row, nil, "") {
		t.Fatal("unknown accounting collapsed into quota exhaustion")
	}
	for _, field := range []string{"status", "code", "input", "output", "price", "charge", "currency", "snapshot"} {
		bad := row
		zero := int64(0)
		money := "0"
		currency := "USD"
		snapshot := "{}"
		switch field {
		case "status":
			bad.Status = "success"
		case "code":
			bad.ErrorCode = "quota_price_unavailable"
		case "input":
			bad.InputTokens = &zero
		case "output":
			bad.OutputTokens = &zero
		case "price":
			bad.PricingStatus = "priced"
		case "charge":
			bad.ChargeAmount = &money
		case "currency":
			bad.ChargeCurrency = &currency
		case "snapshot":
			bad.PricingSnapshotJSON = &snapshot
		}
		if personalKeyBehaviorBlockedFactCode(bad, nil, "quota_usage_unknown") {
			t.Fatal("fabricated accounting", field)
		}
	}
	if personalKeyBehaviorBlockedFactCode(row, []entity.CallAttempt{{RequestID: row.RequestID}}, "quota_usage_unknown") {
		t.Fatal("unknown admission acquired native attempt")
	}
	row.ErrorCode = "quota_exceeded"
	if personalKeyBehaviorBlockedFactCode(row, nil, "quota_usage_unknown") || !personalKeyBehaviorBlockedFact(row, nil) {
		t.Fatal("exhaustion accepted as unknown accounting")
	}
}

func personalKeyBehaviorFaultTx(table string, dest any, tagged bool) *gorm.DB {
	ctx := context.Background()
	if tagged {
		ctx = context.WithValue(ctx, personalKeyMonthlyBehaviorFaultContext{}, true)
	}
	return &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Table: table, Dest: dest, Context: ctx}}
}

func TestPersonalKeyBehaviorFaultScopes(t *testing.T) {
	const root = "key_exact_root"
	var faults personalKeyMonthlyBehaviorFaults
	target := root // Fully initialized immutable copy before atomic publication.
	row := &entity.AuditEvent{Action: "limits.update", ResourceID: root}
	plain := personalKeyBehaviorFaultTx("audit_events", row, false)
	faults.beforeCreate(plain)
	if plain.Error != nil {
		t.Fatal("unarmed audit callback changed a normal write")
	}
	faults.auditRoot.Store(&target)
	for _, test := range []struct {
		name  string
		table string
		dest  any
		want  bool
	}{
		{"exact", "audit_events", row, true},
		{"other-root", "audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "key_other_root"}, false},
		{"other-action", "audit_events", &entity.AuditEvent{Action: "resource.create", ResourceID: root}, false},
		{"other-table", "call_records", row, false},
		{"other-destination", "audit_events", &entity.ResourceLimit{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := personalKeyBehaviorFaultTx(test.table, test.dest, false)
			faults.beforeCreate(tx)
			if (tx.Error != nil) != test.want {
				t.Fatal("audit rollback escaped exact armed target", tx.Error)
			}
		})
	}
	faults.auditRoot.Store(nil)
	plain = personalKeyBehaviorFaultTx("audit_events", row, false)
	faults.beforeCreate(plain)
	if plain.Error != nil {
		t.Fatal("disarmed rollback callback remained active")
	}
	faults.publicationRoot.Store(&target)
	for _, test := range []struct {
		name   string
		table  string
		dest   any
		tagged bool
		failed bool
	}{
		{"untagged", "audit_events", row, false, false},
		{"failed-create", "audit_events", row, true, true},
		{"other-table", "call_records", row, true, false},
		{"other-root", "audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "key_other_root"}, true, false},
		{"other-action", "audit_events", &entity.AuditEvent{Action: "resource.create", ResourceID: root}, true, false},
	} {
		t.Run("publication-"+test.name, func(t *testing.T) {
			faults.committed.Store(false)
			tx := personalKeyBehaviorFaultTx(test.table, test.dest, test.tagged)
			if test.failed {
				tx.Error = errors.New("controlled insert failure")
			}
			faults.afterCreate(tx)
			if faults.committed.Load() {
				t.Fatal("publication fault claimed an unrelated or failed audit")
			}
		})
	}
	faults.committed.Store(false)
	before := personalKeyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(before)
	if before.Error != nil {
		t.Fatal("publication read failed before the audit insert")
	}
	faults.afterCreate(personalKeyBehaviorFaultTx("audit_events", row, true))
	if !faults.committed.Load() {
		t.Fatal("exact tagged successful audit did not arm postcommit read")
	}
	untagged := personalKeyBehaviorFaultTx("resource_limits", nil, false)
	faults.beforeQuery(untagged)
	if untagged.Error != nil {
		t.Fatal("publication fault affected the untagged background worker")
	}
	tagged := personalKeyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(tagged)
	if tagged.Error == nil {
		t.Fatal("exact committed tagged read did not inject publication failure")
	}
	faults.publicationRoot.Store(nil)
	after := personalKeyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(after)
	if after.Error != nil || !faults.committed.Load() {
		t.Fatal("disarming did not preserve committed fact and release later reads")
	}
}

func TestPersonalKeyBehaviorFaultConcurrentArming(t *testing.T) {
	var faults personalKeyMonthlyBehaviorFaults
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for range 1000 {
				target := "key_immutable_concurrent" // Never mutate after Store.
				if worker%2 == 0 {
					faults.auditRoot.Store(&target)
					faults.publicationRoot.Store(&target)
					faults.committed.Store(false)
					faults.auditRoot.Store(nil)
					faults.publicationRoot.Store(nil)
				} else {
					row := &entity.AuditEvent{Action: "limits.update", ResourceID: target}
					faults.beforeCreate(personalKeyBehaviorFaultTx("audit_events", row, false))
					faults.afterCreate(personalKeyBehaviorFaultTx("audit_events", row, true))
					faults.beforeQuery(personalKeyBehaviorFaultTx("resource_limits", nil, true))
				}
			}
		})
	}
	workers.Wait()
	faults.auditRoot.Store(nil)
	faults.publicationRoot.Store(nil)
	plain := personalKeyBehaviorFaultTx("audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "key_immutable_concurrent"}, true)
	faults.beforeCreate(plain)
	faults.beforeQuery(plain)
	if plain.Error != nil {
		t.Fatal("concurrent disarming left the fixture fault active")
	}
}
