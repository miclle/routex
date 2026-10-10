package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

// Additive proposed case142. Native/accounting counts are independent of every
// earlier fixture: warm1 + owned4 completions + six pre-admission denials.
func testProjectKeyMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var barrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &barrier)
	const callback = "test:project-key-monthly-behavior-publication"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, barrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		barrier.armed.Store(false)
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	var faults personalKeyMonthlyBehaviorFaults
	const auditCallback = "test:project-key-monthly-behavior-audit"
	const armCallback = "test:project-key-monthly-behavior-arm"
	const readCallback = "test:project-key-monthly-behavior-read"
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
		if err := db.Callback().Query().Remove(readCallback); err != nil {
			t.Error(err)
		}
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{103}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected native route")
			http.NotFound(w, r)
			return
		}
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"project-key-monthly-behavior-native","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
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
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-key-mode-admin@example.invalid","password":"monthly-behavior-password","name":"Key mode admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	requestAs := func(cookie *http.Cookie, csrf, method, path string, body any, etag string, fault bool) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(cookie)
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
	const modelID = "mdl_project_key_mode"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	cipher, err := store.Seal("crd_project_key_mode", "private-upstream-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_project_key_mode", Name: "Behavior provider"},
		&entity.ProviderConnection{ID: "con_project_key_mode", ProviderID: "prv_project_key_mode", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_project_key_mode", ConnectionID: "con_project_key_mode", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_project_key_mode", ConnectionID: "con_project_key_mode", UpstreamName: "project-key-monthly-behavior-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_project_key_mode", ProviderModelID: "pmd_project_key_mode"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "project-key-monthly-behavior-model", ModelID: modelID, CurrentModelID: func() *string { x := modelID; return &x }()},
		&entity.ModelProviderBinding{ID: "bnd_project_key_mode", ModelID: modelID, ProviderModelID: "pmd_project_key_mode", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_pkmode_admin", UserID: admin.User.ID, Name: "Warm coverage", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_pkmode_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_project_key_mode", ProviderModelID: "pmd_project_key_mode", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: []string{"rate_pkmode_in", "rate_pkmode_out", "rate_pkmode_read", "rate_pkmode_write"}[i], ModelPriceID: "price_project_key_mode", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.000000000001", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	spool := filepath.Join(t.TempDir(), "project-key-monthly.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	call := func(bearer string, status int) {
		t.Helper()
		before := dispatches.Load()
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(c, "POST", "/v1/chat/completions", strings.NewReader(`{"model":"project-key-monthly-behavior-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":50}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		expectStatus(t, out, status)
		if status != 200 && dispatches.Load() != before {
			t.Fatal("denial dispatched native request")
		}
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	call(adminBearer, 200)
	manager, managerCookie, managerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-key-mode-manager", nil)
	peer, peerCookie, peerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-key-mode-peer", nil)
	_, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-key-mode-outsider", nil)
	project, err := svc.CreateResource(ctx, admin.User.ID, service.ProjectResource, "Independent Project Key modes", "Creator attribution is not manager authority", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, project.ID, []string{manager.User.ID, peer.User.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, project.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/projects/" + project.ID
	keys := base + "/keys"
	request := func(method, path string, body any, etag string, fault bool) *httptest.ResponseRecorder {
		return requestAs(managerCookie, managerCSRF, method, path, body, etag, fault)
	}
	created := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", keys, map[string]any{"name": "Original Project root", "delivery_mode": "manual", "model_ids": []string{modelID}}, "", false), 201)
	expectStatus(t, request("POST", keys+"/"+created.Key.ID+"/confirm", nil, "", false), 200)
	keyPath := keys + "/" + created.Key.ID + "/limits"
	parentPath := base + "/limits"
	capacityReview := decodeCatalogResponse[service.ReservationBoundRecord](t, requestAs(adminCookie, admin.CSRFToken, "GET", "/api/v1/admin/provider-models/pmd_project_key_mode/reservation-bound", nil, "", false), 200)
	bound := decodeCatalogResponse[service.ReservationBoundRecord](t, requestAs(adminCookie, admin.CSRFToken, "PUT", "/api/v1/admin/provider-models/pmd_project_key_mode/reservation-bound", map[string]any{"max_input_tokens": 100, "max_output_tokens": 50, "evidence": "Controlled native fixture", "reason": "Finite capacity"}, capacityReview.ETag, false), 200)
	if !bound.Configured {
		t.Fatal("finite reservation missing")
	}
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, "", false), 200)
	}
	write := func(path string, body map[string]any) service.LimitRecord {
		body["reason"] = "Reviewed exact monthly modes"
		etag := read(path).ETag
		if path == parentPath {
			return decodeCatalogResponse[service.LimitRecord](t, requestAs(adminCookie, admin.CSRFToken, "PUT", path, body, etag, false), 200)
		}
		return decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, body, etag, false), 200)
	}
	canonical := func(r service.LimitRecord, token, money string) {
		t.Helper()
		if r.Kind != "project_key" || r.ID != created.Key.ID || r.AccountID != "key_"+created.Key.ID || r.Stored.TokensMonthBehavior != token || r.Stored.MoneyMonthBehavior != money || len(r.IPPolicies) != 2 || r.IPPolicies[1].TokensMonthBehavior != token || r.IPPolicies[1].MoneyMonthBehavior != money {
			t.Fatal("exact root/modes/chain lost")
		}
		raw, _ := json.Marshal(r.Effective)
		if bytes.Contains(raw, []byte("behavior")) {
			t.Fatal("synthetic effective behavior")
		}
	}
	original := read(keyPath)
	canonical(original, "stop", "stop")
	for _, mode := range []any{nil, "ALERT_ONLY", "alert_only ", []string{"stop"}} {
		expectStatus(t, request("PUT", keyPath, map[string]any{"tokens_month_behavior": mode, "reason": "Invalid"}, original.ETag, false), 400)
	}
	expectStatus(t, requestAs(outsiderCookie, outsiderCSRF, "GET", keyPath, nil, "", false), 404)
	// Save the child under an unbounded parent, then independently tighten parent.
	applied := write(keyPath, map[string]any{"tokens_month": 166, "tokens_month_behavior": "alert_only", "money_month": "0.000000000000000166", "money_month_behavior": "alert_only", "currency": "USD"})
	canonical(applied, "alert_only", "alert_only")
	if !applied.Enforced {
		t.Fatal("soft root publication unconfirmed")
	}
	write(parentPath, map[string]any{"tokens_month": 100})
	call(created.Secret, 429)
	write(parentPath, map[string]any{"tokens_month": 200})
	call(created.Secret, 200)
	if err := barrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	var warnings []entity.ProjectKeyQuotaWarningObservation
	if err := db.Where("root_key_id = ? AND policy_revision = ?", created.Key.ID, applied.ETag).Order("dimension").Find(&warnings).Error; err != nil || len(warnings) != 2 {
		t.Fatal("settled90 warning dimensions", len(warnings), err)
	}
	for _, w := range warnings {
		if w.RootKeyID != created.Key.ID || w.ProjectID != project.ID || w.Threshold != 90 || w.ThresholdGeneration != "project-key-monthly-80-90-v1" || w.ResourceCreatedAt.IsZero() || w.ProjectCreatedAt.IsZero() {
			t.Fatal("warning root/calendar evidence lost")
		}
		switch w.Dimension {
		case "tokens":
			if w.Limit != "166" || w.Settled != "150" || w.Currency != "" {
				t.Fatal("exact settled Tokens warning lost")
			}
		case "money":
			if w.Limit != "0.000000000000000166" || w.Settled != "0.00000000000000015" || w.Currency != "USD" {
				t.Fatal("exact settled money warning lost")
			}
		default:
			t.Fatal("unexpected warning dimension")
		}
		var recipients []entity.ProjectKeyQuotaWarningInbox
		if err := db.Where("observation_id = ?", w.ID).Find(&recipients).Error; err != nil || len(recipients) != 2 {
			t.Fatal("exact manager inboxes", err)
		}
		seen := map[string]bool{}
		for _, r := range recipients {
			if r.RecipientID != manager.User.ID && r.RecipientID != peer.User.ID || seen[r.RecipientID] {
				t.Fatal("creator/outsider/duplicate recipient")
			}
			seen[r.RecipientID] = true
		}
	}
	call(created.Secret, 429)
	write(parentPath, map[string]any{"tokens_month": 100, "tokens_month_behavior": "alert_only"})
	write(keyPath, map[string]any{"tokens_month": 200})
	call(created.Secret, 429)
	write(keyPath, map[string]any{"tokens_month": 300})
	call(created.Secret, 200)
	write(keyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "stop"})
	call(created.Secret, 429)
	write(keyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "stop", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	call(created.Secret, 429)
	both := write(keyPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	call(created.Secret, 200)
	// Audit rollback and an actual postcommit publication failure preserve intent.
	target := created.Key.ID
	faults.auditRoot.Store(&target)
	expectStatus(t, request("PUT", keyPath, map[string]any{"tokens_month_behavior": "stop", "reason": "Atomic rollback"}, both.ETag, false), 500)
	faults.auditRoot.Store(nil)
	if read(keyPath).ETag != both.ETag {
		t.Fatal("failed audit committed")
	}
	faultBody := map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only", "reason": "Original uncertain Project Key intent"}
	faults.committed.Store(false)
	faults.publicationRoot.Store(&target)
	expectStatus(t, request("PUT", keyPath, faultBody, both.ETag, true), 503)
	faults.publicationRoot.Store(nil)
	var persisted entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "key", created.Key.ID).Take(&persisted).Error; err != nil || persisted.ETag == both.ETag {
		t.Fatal("uncertain write did not persist", err)
	}
	retry := decodeCatalogResponse[service.LimitRecord](t, request("PUT", keyPath, faultBody, both.ETag, false), 200)
	canonical(retry, "alert_only", "alert_only")
	if retry.ETag != persisted.ETag || !retry.Enforced {
		t.Fatal("immutable retry did not publish exact persisted revision")
	}
	expectStatus(t, request("PUT", keyPath, map[string]any{"tokens_month_behavior": "stop", "reason": "Different retry"}, both.ETag, false), 409)
	rotated := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", keys+"/"+created.Key.ID+"/rotate", map[string]any{"delivery_mode": "manual"}, "", false), 201)
	expectStatus(t, request("POST", keys+"/"+rotated.Key.ID+"/confirm", nil, "", false), 200)
	expectStatus(t, request("DELETE", keys+"/"+created.Key.ID, nil, "", false), 204)
	descendantPath := keys + "/" + rotated.Key.ID + "/limits"
	if read(descendantPath).AccountID != original.AccountID {
		t.Fatal("rotation reset original quota account")
	}
	call(rotated.Secret, 200)
	usage := read(descendantPath)
	if usage.QuotaUsage == nil || usage.QuotaUsage.Month.TokensUsed != 600 || usage.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000006" || usage.QuotaUsage.Month.TokensUnknown != 0 || usage.QuotaUsage.Active.TokensHeld != 0 {
		t.Fatal("exact shared settled/coverage/holds lost")
	}
	hard := write(descendantPath, map[string]any{"tokens_month": 0})
	if hard.Stored.TokensMonthBehavior != "stop" || hard.Stored.MoneyMonthBehavior != "stop" {
		t.Fatal("omission failed to reset stop")
	}
	call(rotated.Secret, 429)
	// Removing management authority changes neither historical account nor owner graph.
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, project.ID, []string{peer.User.ID}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", descendantPath, nil, "", false), 404)
	peerRead := decodeCatalogResponse[service.LimitRecord](t, requestAs(peerCookie, peerCSRF, "GET", descendantPath, nil, "", false), 200)
	if peerRead.AccountID != original.AccountID {
		t.Fatal("current manager borrowed a Personal account")
	}
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, project.ID, []string{manager.User.ID, peer.User.ID}); err != nil {
		t.Fatal(err)
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
	if len(calls) != 11 || len(attempts) != 5 || dispatches.Load() != 5 {
		t.Fatal("independent planned call/native counts", len(calls), len(attempts), dispatches.Load())
	}
	denied, primary := 0, 0
	for _, c := range calls {
		if c.ErrorCode != "" {
			denied++
			if !projectBehaviorBlockedFact(c, attempts, "quota_exceeded") {
				t.Fatal("denial fabricated native/charge")
			}
		} else if c.ProjectID == project.ID {
			primary++
			if c.InputTokens == nil || *c.InputTokens != 100 || c.OutputTokens == nil || *c.OutputTokens != 50 || c.ChargeAmount == nil || *c.ChargeAmount != "0.00000000000000015" || c.ChargeCurrency == nil || *c.ChargeCurrency != "USD" || c.PricingSnapshotJSON == nil {
				t.Fatal("exact native settlement lost")
			}
		}
	}
	if denied != 6 || primary != 4 {
		t.Fatal("exact denial/primary counts", denied, primary)
	}
	for _, a := range attempts {
		if a.CredentialID != "crd_project_key_mode" || a.SnapshotID == "" || a.ProviderModelID != "pmd_project_key_mode" || a.ConnectionID != "con_project_key_mode" || a.NativeCompletionEvidence != "completed" || a.Status != "success" || !a.FinalUsageKnown {
			t.Fatal("parser/route/publication evidence lost")
		}
	}
	var retainedWarnings []entity.ProjectKeyQuotaWarningObservation
	var retainedInboxes []entity.ProjectKeyQuotaWarningInbox
	if err := db.Order("id").Find(&retainedWarnings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&retainedInboxes).Error; err != nil {
		t.Fatal(err)
	}
	type sessionFact struct {
		ID, UserID           string
		ExpiresAt, CreatedAt time.Time
	}
	var sessions []sessionFact
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if r := read(descendantPath); !r.Enforced || r.ETag != hard.ETag || r.QuotaUsage.Month.TokensUsed != 600 || r.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000006" {
		t.Fatal("same journal/artifact root proof lost")
	}
	var afterCalls []entity.CallRecord
	var afterAttempts []entity.CallAttempt
	var afterSessions []sessionFact
	if err := db.Order("request_id").Find(&afterCalls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterAttempts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&afterSessions).Error; err != nil {
		t.Fatal(err)
	}
	var afterWarnings []entity.ProjectKeyQuotaWarningObservation
	var afterInboxes []entity.ProjectKeyQuotaWarningInbox
	if err := db.Order("id").Find(&afterWarnings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterInboxes).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retainedWarnings, afterWarnings) || !reflect.DeepEqual(retainedInboxes, afterInboxes) {
		t.Fatal("restart rewrote original warning history")
	}
	if !reflect.DeepEqual(calls, afterCalls) || !reflect.DeepEqual(attempts, afterAttempts) || !reflect.DeepEqual(sessions, afterSessions) || dispatches.Load() != 5 {
		t.Fatal("restart mutated original immutable facts or dispatched")
	}
}

func projectKeyMonthlyBehaviorRegistryMatches(pairs []string) bool {
	if len(pairs) == 156 || len(pairs) == 158 || len(pairs) == 160 || len(pairs) == 162 || len(pairs) == 164 || len(pairs) == 166 || len(pairs) == 168 || len(pairs) == 170 || len(pairs) == 172 || len(pairs) == 174 || len(pairs) == 176 || len(pairs) == 177 || len(pairs) == 179 {
		parent, ok := personalRollingWarningRegistryParent(pairs)
		if !ok {
			return false
		}
		pairs = parent
	}
	if len(pairs) == 154 {
		parent, ok := providerCleanupRegistryParent(pairs)
		if !ok {
			return false
		}
		pairs = parent
	}
	if len(pairs) == 152 {
		parent, ok := azureDeploymentRegistryParent(pairs)
		if !ok {
			return false
		}
		pairs = parent
	}
	if len(pairs) == 150 {
		parent, ok := vaultAppRoleRegistryParent(pairs)
		if !ok {
			return false
		}
		pairs = parent
	}
	if len(pairs) == 146 || len(pairs) == 148 {
		if !connectionEnablementRegistryPrefix(pairs) {
			return false
		}
		pairs = pairs[:144]
	}
	if len(pairs) == 144 {
		if !teamMemberMonthlyRegistryTail(pairs) {
			return false
		}
		pairs = pairs[:142]
	}
	return len(pairs) == 142 && pairs[141] == "project_key_monthly_behavior:testProjectKeyMonthlyBehaviorLifecycle" && personalKeyBehaviorRegistryMatches(pairs[:141])
}
func TestProjectKeyMonthlyBehaviorRegistryTail(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	pairs := make([]string, 0, len(matches))
	for _, pair := range matches {
		pairs = append(pairs, pair[1]+":"+pair[2])
	}
	if !googleRegistry200Current(pairs) {
		t.Fatal("exact200 Google successor changed")
	}
	pairs = pairs[:197]
	if !githubRegistry197Current(pairs) {
		t.Fatal("exact197 GitHub successor changed")
	}
	pairs = pairs[:181]
	if !credentialAttemptStatisticsRegistry181Current(pairs) {
		t.Fatal("exact V93/181 successor changed")
	}
	pairs = pairs[:179]
	if len(pairs) != 179 || !strings.Contains(string(raw), "versions != 99") {
		t.Fatal("current exact172 registry/V89 ledger changed")
	}
	if !projectKeyMonthlyBehaviorRegistryMatches(pairs) {
		t.Fatal("original141 prefix or exact142 tail changed")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:141] },
		func(x []string) []string { x[141] = "project_key_monthly_behavior:unreviewed"; return x },
		func(x []string) []string { x[140], x[141] = x[141], x[140]; return x },
		func(x []string) []string { x[137] = "personal_key_monthly_behavior_migration:unreviewed"; return x },
		func(x []string) []string { return x[:175] },
		func(x []string) []string { x[174], x[175] = x[175], x[174]; return x },
		func(x []string) []string { x[174] = "provider_enablement_migration:unreviewed"; return x },
		func(x []string) []string { x[175] = "provider_status:unreviewed"; return x },
		func(x []string) []string { return append(x, "extra:unreviewed") },
	} {
		if projectKeyMonthlyBehaviorRegistryMatches(mutate(append([]string(nil), pairs...))) {
			t.Fatal("missing/reordered/unreviewed/extra registry accepted")
		}
	}
}
