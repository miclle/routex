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

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type personalMonthlyBehaviorFaultContext struct{}

// Callbacks are installed before workers start. Only immutable target pointers
// and atomic fault state change while the shared GORM processor is executing.
type personalMonthlyBehaviorFaults struct {
	auditRoot       atomic.Pointer[string]
	publicationRoot atomic.Pointer[string]
	committed       atomic.Bool
}

func (f *personalMonthlyBehaviorFaults) beforeCreate(tx *gorm.DB) {
	root := f.auditRoot.Load()
	if root == nil || tx.Statement.Table != "audit_events" {
		return
	}
	if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "limits.update" && row.ResourceID == *root {
		_ = tx.AddError(errors.New("controlled audit persistence failure"))
	}
}

func (f *personalMonthlyBehaviorFaults) afterCreate(tx *gorm.DB) {
	root := f.publicationRoot.Load()
	if root == nil || tx.Error != nil || tx.Statement.Table != "audit_events" || tx.Statement.Context.Value(personalMonthlyBehaviorFaultContext{}) != true {
		return
	}
	if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "limits.update" && row.ResourceID == *root {
		f.committed.Store(true)
	}
}

func (f *personalMonthlyBehaviorFaults) beforeQuery(tx *gorm.DB) {
	if f.publicationRoot.Load() != nil && f.committed.Load() && tx.Statement.Context.Value(personalMonthlyBehaviorFaultContext{}) == true {
		_ = tx.AddError(errors.New("controlled postcommit publication read failure"))
	}
}

// The root appends this new scenario after the unchanged 130-case predecessor.
// All native facts come from the existing native parser and real admission path.
func testPersonalMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var faults personalMonthlyBehaviorFaults
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
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"monthly-behavior-native","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	makeService := func() *service.Service {
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	// Registered earlier: workers are joined before callback processor teardown.
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"behavior-admin@example.invalid","password":"monthly-behavior-password","name":"Behavior admin"}`, nil, "")
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
		if strings.HasPrefix(path, "/api/v1/keys/") {
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
			req = req.WithContext(context.WithValue(req.Context(), personalMonthlyBehaviorFaultContext{}, true))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	const modelID = "mdl_behavior70"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	cipher, err := store.Seal("crd_behavior70", "private-upstream-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_behavior70", Name: "Behavior provider"},
		&entity.ProviderConnection{ID: "con_behavior70", ProviderID: "prv_behavior70", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_behavior70", ConnectionID: "con_behavior70", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_behavior70", ConnectionID: "con_behavior70", UpstreamName: "monthly-behavior-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_behavior70", ProviderModelID: "pmd_behavior70"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "monthly-behavior-model", ModelID: modelID, CurrentModelID: func() *string { x := modelID; return &x }()},
		&entity.ModelProviderBinding{ID: "bnd_behavior70", ModelID: modelID, ProviderModelID: "pmd_behavior70", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_behavior_admin", UserID: admin.User.ID, Name: "Warm coverage", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_behavior_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_behavior70", ProviderModelID: "pmd_behavior70", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: []string{"rate_behavior_in", "rate_behavior_out", "rate_behavior_read", "rate_behavior_write"}[i], ModelPriceID: "price_behavior70", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.000000000001", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	spool := filepath.Join(t.TempDir(), "monthly-behavior.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"monthly-behavior-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":50}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	// The first genuine unbounded admission activates coverage before the new User.
	expectStatus(t, call(adminBearer), 200)
	member, err := svc.CreateMember(ctx, admin.User.ID, "behavior-member@example.invalid", "monthly-behavior-password", "Behavior member", "member")
	if err != nil {
		t.Fatal(err)
	}
	memberLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"behavior-member@example.invalid","password":"monthly-behavior-password"}`, nil, "")
	expectStatus(t, memberLogin, http.StatusOK)
	memberIdentity, memberCookie = readIdentity(t, memberLogin)
	if memberIdentity.User.ID != member.User.ID {
		t.Fatal("Key review Session must belong to the immutable Key owner")
	}
	memberBearer := "rx_" + strings.Repeat("b", 43)
	for _, row := range []any{&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID}, &entity.APIKey{ID: "key_behavior_member", UserID: member.User.ID, Name: "Hard independent Key", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(memberBearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_behavior_member", ModelID: modelID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	boundPath := "/api/v1/admin/provider-models/pmd_behavior70/reservation-bound"
	bound := decodeCatalogResponse[service.ReservationBoundRecord](t, request("PUT", boundPath, map[string]any{"max_input_tokens": 100, "max_output_tokens": 50, "evidence": "Controlled native fixture", "reason": "Finite reservation"}, "0", false), 200)
	if !bound.Configured {
		t.Fatal("real capacity unavailable")
	}
	userPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	keyPath := "/api/v1/keys/key_behavior_member/limits"
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, "", false), 200)
	}
	write := func(path string, body map[string]any) service.LimitRecord {
		body["reason"] = "Reviewed independent behavior"
		return decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, body, read(path).ETag, false), 200)
	}
	canonical := func(record service.LimitRecord, token, money string) {
		t.Helper()
		if record.Stored.TokensMonthBehavior != token || record.Stored.MoneyMonthBehavior != money || record.IPPolicies[0].TokensMonthBehavior != token || record.IPPolicies[0].MoneyMonthBehavior != money {
			t.Fatal("canonical User modes missing")
		}
		raw, _ := json.Marshal(record.Effective)
		if bytes.Contains(raw, []byte("behavior")) {
			t.Fatal("effective projected synthetic behavior")
		}
	}
	canonical(read(userPath), "stop", "stop")
	body := map[string]any{"tokens_month": 100, "tokens_month_behavior": "alert_only", "money_month": nil, "money_month_behavior": "stop", "reason": "Original exact reviewed soft policy"}
	original := read(userPath)
	applied := decodeCatalogResponse[service.LimitRecord](t, request("PUT", userPath, body, original.ETag, false), 200)
	canonical(applied, "alert_only", "stop")
	if !applied.Enforced {
		t.Fatal("current mode publication not confirmed")
	}
	key := write(keyPath, map[string]any{"tokens_month": 200})
	if key.Stored.TokensMonthBehavior != "stop" || key.IPPolicies[0].TokensMonthBehavior != "alert_only" || key.Effective.TokensMonth == nil || *key.Effective.TokensMonth != 100 {
		t.Fatal("separate account and numeric summary contract")
	}
	for _, mode := range []any{nil, "ALERT_ONLY", "alert_only ", []string{"stop"}} {
		expectStatus(t, request("PUT", userPath, map[string]any{"tokens_month_behavior": mode, "reason": "Invalid"}, applied.ETag, false), 400)
	}
	key = decodeCatalogResponse[service.LimitRecord](t, request("PUT", keyPath, map[string]any{"tokens_month": 200, "tokens_month_behavior": "stop", "reason": "Canonical Personal Key hard policy"}, key.ETag, false), 200)
	if key.Stored.TokensMonthBehavior != "stop" || key.Stored.MoneyMonthBehavior != "stop" || !key.Enforced {
		t.Fatal("canonical Personal Key stop policy not published")
	}
	memberAuth, deniedMemberCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"behavior-member@example.invalid","password":"monthly-behavior-password"}`, nil, ""))
	denied := httptest.NewRequest("PUT", userPath, strings.NewReader(`{"tokens_month":100,"tokens_month_behavior":"alert_only","reason":"No platform permission"}`))
	denied.Header.Set("Content-Type", "application/json")
	denied.Header.Set("X-CSRF-Token", memberAuth.CSRFToken)
	denied.Header.Set("If-Match", `"`+applied.ETag+`"`)
	denied.AddCookie(deniedMemberCookie)
	deniedResult := httptest.NewRecorder()
	router.ServeHTTP(deniedResult, denied)
	expectStatus(t, deniedResult, 403)
	expectStatus(t, call(memberBearer), 200)
	usage := read(userPath)
	if usage.QuotaUsage == nil || usage.QuotaUsage.Month.TokensUsed != 150 || usage.QuotaUsage.Active.TokensHeld != 0 {
		t.Fatal("soft account lost settled/hold proof")
	}
	before := dispatches.Load()
	expectStatus(t, call(memberBearer), 429)
	if dispatches.Load() != before {
		t.Fatal("soft User bypassed hard Key")
	}
	// Exact identical retry has no extra revision/audit and still needs publication.
	countAudit := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "user", member.User.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	auditBefore := countAudit()
	retry := decodeCatalogResponse[service.LimitRecord](t, request("PUT", userPath, body, original.ETag, false), 200)
	if retry.ETag != applied.ETag || countAudit() != auditBefore || !retry.Enforced {
		t.Fatal("identical mode retry duplicated or unconfirmed")
	}
	changed := map[string]any{"tokens_month": 100, "tokens_month_behavior": "stop", "reason": "Reject stale different behavior"}
	expectStatus(t, request("PUT", userPath, changed, original.ETag, false), 409)
	if read(userPath).ETag != applied.ETag {
		t.Fatal("conflict rewrote policy")
	}
	// Full-policy omission resets hard stop, including previously saved soft mode.
	hard := write(userPath, map[string]any{"tokens_month": 100})
	canonical(hard, "stop", "stop")
	expectStatus(t, call(memberBearer), 429)
	// Money and Token behaviors are independent. Clear the Key cap before zero tests.
	write(keyPath, map[string]any{})
	write(userPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "stop"})
	expectStatus(t, call(memberBearer), 429)
	write(userPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "stop", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	expectStatus(t, call(memberBearer), 429)
	both := write(userPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	canonical(both, "alert_only", "alert_only")
	expectStatus(t, call(memberBearer), 200)
	usage = read(userPath)
	if usage.QuotaUsage.Month.TokensUsed != 300 || usage.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000003" || usage.QuotaUsage.Month.MoneyUnknown != 0 {
		t.Fatal("soft exact settlement changed", usage.QuotaUsage)
	}
	// Typed audit failure rolls both modes/revision back in the same transaction.
	faultTarget := member.User.ID // Immutable value before atomic publication.
	faults.auditRoot.Store(&faultTarget)
	expectStatus(t, request("PUT", userPath, map[string]any{"tokens_month_behavior": "stop", "money_month_behavior": "stop", "reason": "Atomic rollback"}, both.ETag, false), 500)
	faults.auditRoot.Store(nil)
	if got := read(userPath); got.ETag != both.ETag || got.Stored.TokensMonthBehavior != "alert_only" || got.Stored.MoneyMonthBehavior != "alert_only" {
		t.Fatal("failed audit committed behavior")
	}
	// A scoped read fault happens only after the genuine audit insert. It forces
	// publication unavailable without disabling the live worker or changing SQL.
	faults.committed.Store(false)
	faults.publicationRoot.Store(&faultTarget)
	faultBody := map[string]any{"tokens_month": nil, "tokens_month_behavior": "alert_only", "money_month": nil, "money_month_behavior": "alert_only", "reason": "Original uncertain null-cap request"}
	expectStatus(t, request("PUT", userPath, faultBody, both.ETag, true), 503)
	if !faults.committed.Load() {
		t.Fatal("failure did not follow genuine commit")
	}
	faults.publicationRoot.Store(nil)
	var persisted entity.ResourceLimit
	if err := db.Take(&persisted, "scope_kind = ? AND scope_id = ?", "user", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ETag == both.ETag || persisted.TokensMonth != nil || persisted.MoneyMonth != nil || persisted.TokensMonthBehavior != "alert_only" || persisted.MoneyMonthBehavior != "alert_only" {
		t.Fatal("uncertain current configuration not exact")
	}
	uncertainAudit := countAudit()
	confirmed := decodeCatalogResponse[service.LimitRecord](t, request("PUT", userPath, faultBody, both.ETag, false), 200)
	canonical(confirmed, "alert_only", "alert_only")
	if confirmed.ETag != persisted.ETag || !confirmed.Enforced || countAudit() != uncertainAudit {
		t.Fatal("uncertain exact retry not confirmed/noop")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
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
	if dispatches.Load() != 3 || len(attempts) != 3 {
		t.Fatal("unexpected native dispatch count", dispatches.Load(), len(attempts))
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	restarted := read(userPath)
	canonical(restarted, "alert_only", "alert_only")
	if !restarted.Enforced || restarted.ETag != confirmed.ETag || restarted.QuotaUsage.Month.TokensUsed != 300 || restarted.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000003" {
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
	if !reflect.DeepEqual(afterCalls, calls) || !reflect.DeepEqual(afterAttempts, attempts) || dispatches.Load() != 3 {
		t.Fatal("restart changed immutable facts or dispatched")
	}
}

func personalMonthlyBehaviorFaultTx(table string, dest any, tagged bool) *gorm.DB {
	ctx := context.Background()
	if tagged {
		ctx = context.WithValue(ctx, personalMonthlyBehaviorFaultContext{}, true)
	}
	return &gorm.DB{Config: &gorm.Config{}, Statement: &gorm.Statement{Table: table, Dest: dest, Context: ctx}}
}

func TestPersonalMonthlyBehaviorFaultScopes(t *testing.T) {
	const root = "usr_exact_root"
	var faults personalMonthlyBehaviorFaults
	target := root // Fully initialized immutable copy before atomic publication.
	row := &entity.AuditEvent{Action: "limits.update", ResourceID: root}
	plain := personalMonthlyBehaviorFaultTx("audit_events", row, false)
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
		{"other-root", "audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "usr_other_root"}, false},
		{"other-action", "audit_events", &entity.AuditEvent{Action: "resource.create", ResourceID: root}, false},
		{"other-table", "call_records", row, false},
		{"other-destination", "audit_events", &entity.ResourceLimit{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := personalMonthlyBehaviorFaultTx(test.table, test.dest, false)
			faults.beforeCreate(tx)
			if (tx.Error != nil) != test.want {
				t.Fatal("audit rollback escaped exact armed target", tx.Error)
			}
		})
	}
	faults.auditRoot.Store(nil)
	plain = personalMonthlyBehaviorFaultTx("audit_events", row, false)
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
		{"other-root", "audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "usr_other_root"}, true, false},
		{"other-action", "audit_events", &entity.AuditEvent{Action: "resource.create", ResourceID: root}, true, false},
	} {
		t.Run("publication-"+test.name, func(t *testing.T) {
			faults.committed.Store(false)
			tx := personalMonthlyBehaviorFaultTx(test.table, test.dest, test.tagged)
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
	before := personalMonthlyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(before)
	if before.Error != nil {
		t.Fatal("publication read failed before the audit insert")
	}
	faults.afterCreate(personalMonthlyBehaviorFaultTx("audit_events", row, true))
	if !faults.committed.Load() {
		t.Fatal("exact tagged successful audit did not arm postcommit read")
	}
	untagged := personalMonthlyBehaviorFaultTx("resource_limits", nil, false)
	faults.beforeQuery(untagged)
	if untagged.Error != nil {
		t.Fatal("publication fault affected the untagged background worker")
	}
	tagged := personalMonthlyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(tagged)
	if tagged.Error == nil {
		t.Fatal("exact committed tagged read did not inject publication failure")
	}
	faults.publicationRoot.Store(nil)
	after := personalMonthlyBehaviorFaultTx("resource_limits", nil, true)
	faults.beforeQuery(after)
	if after.Error != nil || !faults.committed.Load() {
		t.Fatal("disarming did not preserve committed fact and release later reads")
	}
}

func TestPersonalMonthlyBehaviorFaultConcurrentArming(t *testing.T) {
	var faults personalMonthlyBehaviorFaults
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for range 1000 {
				target := "usr_immutable_concurrent" // Never mutate after Store.
				if worker%2 == 0 {
					faults.auditRoot.Store(&target)
					faults.publicationRoot.Store(&target)
					faults.committed.Store(false)
					faults.auditRoot.Store(nil)
					faults.publicationRoot.Store(nil)
				} else {
					row := &entity.AuditEvent{Action: "limits.update", ResourceID: target}
					faults.beforeCreate(personalMonthlyBehaviorFaultTx("audit_events", row, false))
					faults.afterCreate(personalMonthlyBehaviorFaultTx("audit_events", row, true))
					faults.beforeQuery(personalMonthlyBehaviorFaultTx("resource_limits", nil, true))
				}
			}
		})
	}
	workers.Wait()
	faults.auditRoot.Store(nil)
	faults.publicationRoot.Store(nil)
	plain := personalMonthlyBehaviorFaultTx("audit_events", &entity.AuditEvent{Action: "limits.update", ResourceID: "usr_immutable_concurrent"}, true)
	faults.beforeCreate(plain)
	faults.beforeQuery(plain)
	if plain.Error != nil {
		t.Fatal("concurrent disarming left the fixture fault active")
	}
}
