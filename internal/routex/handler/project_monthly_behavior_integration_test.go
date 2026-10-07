package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"go/ast"
	"go/parser"
	"go/token"
	"gorm.io/gorm"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type projectMonthlyBehaviorFaultContext struct{}

// The private frozen predecessor is copied literally from released V73; only
// this new fixture uses it to reconstruct an actual upgrade boundary.
type projectBehaviorFrozenV73 struct {
	ScopeKind           string `gorm:"size:20;check:ck_resource_limits_monthly_behavior_scope_v73,(OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR (OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109) OR (OCTET_LENGTH(scope_kind) = 3 AND ASCII(SUBSTRING(scope_kind,1,1)) = 107 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 121) OR ((OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) AND (OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112))"`
	TokensMonthBehavior string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_tokens_month_behavior,(OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(tokens_month_behavior) = 10 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(tokens_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(tokens_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(tokens_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,10,1)) = 121)"`
	MoneyMonthBehavior  string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_money_month_behavior,(OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(money_month_behavior) = 10 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(money_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(money_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(money_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,10,1)) = 121)"`
}

func (projectBehaviorFrozenV73) TableName() string { return "resource_limits" }

// The coordinator registers these two scenarios only after the V73 predecessor.
func testProjectMonthlyBehaviorMigration(t *testing.T, db *gorm.DB) {
	if db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v75") {
		projectBehaviorHistoricalReplay(t, db, testProjectMonthlyBehaviorMigrationV74)
		return
	}
	testProjectMonthlyBehaviorMigrationV74(t, db)
}

func testProjectMonthlyBehaviorMigrationV74(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	const current = "ck_resource_limits_monthly_behavior_scope_v74"
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, current) {
		t.Fatal("V74 not registered")
	}
	type entry struct {
		Version   int
		AppliedAt string
	}
	var before []entry
	if err := db.Table("schema_migrations").Order("version").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if len(before) < 74 {
		t.Fatal("missing ordered predecessor")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("noncontiguous ledger")
		}
	}
	var row entity.ResourceLimit
	if err := db.Create(&entity.ResourceLimit{ScopeKind: "project", ScopeID: "prj_upgrade74", ETag: "unchanged", TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&row, "scope_kind = ? AND scope_id = ?", "project", "prj_upgrade74").Error; err != nil {
		t.Fatal(err)
	}
	replay := func() {
		t.Helper()
		if err := db.Table("schema_migrations").Where("version = ?", 74).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		var after []entry
		if err := db.Table("schema_migrations").Order("version").Find(&after).Error; err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatal("ledger cardinality")
		}
		for i, old := range before {
			if after[i].Version != old.Version || old.Version != 74 && after[i].AppliedAt != old.AppliedAt {
				t.Fatal("unrelated migration history changed")
			}
		}
		var retained entity.ResourceLimit
		if err := db.Take(&retained, "scope_kind = ? AND scope_id = ?", "project", "prj_upgrade74").Error; err != nil || !reflect.DeepEqual(row, retained) {
			t.Fatal("upgrade rewrote saved policy", err)
		}
	}
	// Genuine V73 upgrade: retain the saved hard Project and soft Personal Key,
	// reconstruct only the old scope fence, and reject Project soft before V74.
	legacyKey := entity.ResourceLimit{ScopeKind: "key", ScopeID: "key_upgrade74", ETag: "retained-personal", TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop"}
	if err := db.Create(&legacyKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&legacyKey, "scope_kind = ? AND scope_id = ?", "key", "key_upgrade74").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, current); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&projectBehaviorFrozenV73{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "project", "prj_upgrade74").Update("tokens_month_behavior", "alert_only").Error
	}); err == nil {
		t.Fatal("V73 boundary allowed Project soft")
	}
	replay() // Repair interrupted DDL from a recorded predecessor, preserving rows.
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, current) || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73") {
		t.Fatal("scope replacement incomplete")
	}
	var retainedKey entity.ResourceLimit
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&retainedKey, "scope_kind = ? AND scope_id = ?", "key", "key_upgrade74").Error; err != nil || !reflect.DeepEqual(legacyKey, retainedKey) {
		t.Fatal("upgrade rewrote retained Personal policy", err)
	}
	// Partially applied MySQL DDL: both valid scope fences can exist, and replay
	// must remove only V73 without changing either retained policy.
	if err := db.Migrator().CreateConstraint(&projectBehaviorFrozenV73{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	replay()
	// Concurrent genuine startup from the same missing V74 ledger entry.
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, current); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&projectBehaviorFrozenV73{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	if err := db.Table("schema_migrations").Where("version = ?", 74).Delete(&struct{}{}).Error; err != nil {
		t.Fatal(err)
	}
	var startup sync.WaitGroup
	startupErrors := make(chan error, 2)
	for range 2 {
		startup.Go(func() { startupErrors <- database.Migrate(ctx, db) })
	}
	startup.Wait()
	close(startupErrors)
	for err := range startupErrors {
		if err != nil {
			t.Fatal("concurrent startup", err)
		}
	}
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, current) || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73") {
		t.Fatal("concurrent fence restoration incomplete")
	}
	for _, kind := range []string{"project", "Project", "project ", "project_key", "team_member"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&entity.ResourceLimit{ScopeKind: kind, ScopeID: "prj_mode74", ETag: "constraint", TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop"}).Error
		})
		if (err == nil) != (kind == "project") {
			t.Fatal("collation-independent scope constraint", kind, err)
		}
	}
	for _, mode := range []string{"ALERT_ONLY", "alert_only ", "", "unknown"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "project", "prj_mode74").Update("tokens_month_behavior", mode).Error
		})
		if err == nil {
			t.Fatal("invalid persisted mode accepted", mode)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent/repeat migration", err)
		}
	}
}

func testProjectMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationBarrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &publicationBarrier)
	const publicationCallback = "test:personal-project-behavior-observation"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, publicationBarrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationBarrier.armed.Store(false)
		if err := db.Callback().Query().Remove(publicationCallback); err != nil {
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
			_, _ = io.WriteString(w, `{"object":"chat.completion","model":"project-monthly-behavior-native","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","model":"project-monthly-behavior-native","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
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
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"project-behavior-admin@example.invalid","password":"monthly-behavior-password","name":"Behavior admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	var memberIdentity SessionResponse
	var memberCookie *http.Cookie
	requestAs := func(actorCookie *http.Cookie, csrf, method, path string, body any, etag string, fault bool) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(actorCookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		if fault {
			req = req.WithContext(context.WithValue(req.Context(), projectMonthlyBehaviorFaultContext{}, true))
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		return out
	}
	request := func(method, path string, body any, etag string, fault bool) *httptest.ResponseRecorder {
		if strings.Contains(path, "/keys") {
			if memberCookie == nil {
				t.Fatal("Project Key controls require the exact current manager Session")
			}
			return requestAs(memberCookie, memberIdentity.CSRFToken, method, path, body, etag, fault)
		}
		return requestAs(cookie, admin.CSRFToken, method, path, body, etag, fault)
	}
	const modelID = "mdl_behavior74"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	cipher, err := store.Seal("crd_behavior74", "private-upstream-fixture")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&entity.Provider{ID: "prv_behavior74", Name: "Behavior provider"},
		&entity.ProviderConnection{ID: "con_behavior74", ProviderID: "prv_behavior74", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_behavior74", ConnectionID: "con_behavior74", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_behavior74", ConnectionID: "con_behavior74", UpstreamName: "project-monthly-behavior-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_behavior74", ProviderModelID: "pmd_behavior74"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "project-monthly-behavior-model", ModelID: modelID, CurrentModelID: func() *string { x := modelID; return &x }()},
		&entity.ModelProviderBinding{ID: "bnd_behavior74", ModelID: modelID, ProviderModelID: "pmd_behavior74", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_behavior_admin", UserID: admin.User.ID, Name: "Warm coverage", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_behavior_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_behavior74", ProviderModelID: "pmd_behavior74", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for i, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: []string{"rate_behavior_in", "rate_behavior_out", "rate_behavior_read", "rate_behavior_write"}[i], ModelPriceID: "price_behavior74", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0.000000000001", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var auditTarget atomic.Value
	auditTarget.Store("")
	var auditRollbackArmed, publicationFaultArmed, committed atomic.Bool
	const auditCallback = "test:project-behavior-audit-rollback"
	const armCallback = "test:project-behavior-publication-arm"
	const readCallback = "test:project-behavior-publication-read"
	if err := db.Callback().Create().Before("gorm:create").Register(auditCallback, func(tx *gorm.DB) {
		if !auditRollbackArmed.Load() || tx.Statement.Table != "audit_events" {
			return
		}
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "limits.update" && row.ResourceID == auditTarget.Load().(string) {
			_ = tx.AddError(errors.New("controlled audit persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		auditRollbackArmed.Store(false)
		if err := db.Callback().Create().Remove(auditCallback); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Callback().Create().After("gorm:create").Register(armCallback, func(tx *gorm.DB) {
		if publicationFaultArmed.Load() && tx.Statement.Table == "audit_events" && tx.Statement.Context.Value(projectMonthlyBehaviorFaultContext{}) == true {
			committed.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationFaultArmed.Store(false)
		if err := db.Callback().Create().Remove(armCallback); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Callback().Query().Before("gorm:query").Register(readCallback, func(tx *gorm.DB) {
		if publicationFaultArmed.Load() && committed.Load() && tx.Statement.Context.Value(projectMonthlyBehaviorFaultContext{}) == true {
			_ = tx.AddError(errors.New("controlled postcommit publication read failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(readCallback); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		nativeRequests.Wait()
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	spool := filepath.Join(t.TempDir(), "monthly-behavior.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	call := func(bearer string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "/v1/chat/completions", strings.NewReader(`{"model":"project-monthly-behavior-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":50}`))
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
	member, err := svc.CreateMember(ctx, admin.User.ID, "project-behavior-member@example.invalid", "monthly-behavior-password", "Behavior member", "member")
	if err != nil {
		t.Fatal(err)
	}
	memberLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-behavior-member@example.invalid","password":"monthly-behavior-password"}`, nil, "")
	expectStatus(t, memberLogin, http.StatusOK)
	memberIdentity, memberCookie = readIdentity(t, memberLogin)
	if memberIdentity.User.ID != member.User.ID {
		t.Fatal("Project Key review Session must belong to the exact current manager")
	}
	peer, peerCookie, peerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-behavior-peer", nil)
	_, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-behavior-writer", []string{"projects.limits.write"})
	outsider, outsiderCookie, outsiderCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "project-behavior-outsider", nil)
	project, err := svc.CreateResource(ctx, admin.User.ID, service.ProjectResource, "Independent monthly Project", "Creator is not a manager", []string{member.User.ID, peer.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Legacy CreateResource initializes the creator only; use the real complete
	// manager replacement before owner-scoped Key creation and warning delivery.
	assignManagers := func(projectID string) {
		t.Helper()
		managers, err := svc.SetProjectManagers(ctx, admin.User.ID, projectID, []string{member.User.ID, peer.User.ID})
		if err != nil {
			t.Fatal(err)
		}
		if managers.ID != projectID || len(managers.Managers) != 2 {
			t.Fatal("exact target and two admitted Project managers required")
		}
		seenManagers := map[string]bool{}
		for _, manager := range managers.Managers {
			if manager.ID == "" || manager.UserID != member.User.ID && manager.UserID != peer.User.ID || seenManagers[manager.UserID] {
				t.Fatal("unexpected/duplicate Project manager or creator authority")
			}
			seenManagers[manager.UserID] = true
		}
	}
	assignManagers(project.ID)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, project.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	auditTarget.Store(project.ID)
	projectBase := "/api/v1/projects/" + project.ID
	keyBase := projectBase + "/keys"
	created := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", keyBase, map[string]any{"name": "Independent hard Project Key", "delivery_mode": "manual", "model_ids": []string{modelID}}, "", false), 201)
	memberBearer := created.Secret
	if created.Key.ID == "" || memberBearer == "" || created.Key.ProjectID != project.ID || created.Key.CreatorID != member.User.ID {
		t.Fatal("normal Project Key identity/secret missing")
	}
	expectStatus(t, request("POST", keyBase+"/"+created.Key.ID+"/confirm", nil, "", false), 200)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	boundPath := "/api/v1/admin/provider-models/pmd_behavior74/reservation-bound"
	bound := decodeCatalogResponse[service.ReservationBoundRecord](t, request("PUT", boundPath, map[string]any{"max_input_tokens": 100, "max_output_tokens": 50, "evidence": "Controlled native fixture", "reason": "Finite reservation"}, "0", false), 200)
	if !bound.Configured {
		t.Fatal("real capacity unavailable")
	}
	primaryPath := projectBase + "/limits"
	primaryKeyPath := keyBase + "/" + created.Key.ID + "/limits"
	read := func(path string) service.LimitRecord {
		return decodeCatalogResponse[service.LimitRecord](t, request("GET", path, nil, "", false), 200)
	}
	write := func(path string, input map[string]any) service.LimitRecord {
		input["reason"] = "Reviewed independent Project behavior"
		return decodeCatalogResponse[service.LimitRecord](t, request("PUT", path, input, read(path).ETag, false), 200)
	}
	canonical := func(record service.LimitRecord, token, money string) {
		t.Helper()
		if record.Kind != "project" || record.ID != project.ID || record.AccountID != "project_"+project.ID || record.Stored.TokensMonthBehavior != token || record.Stored.MoneyMonthBehavior != money || len(record.IPPolicies) != 1 || record.IPPolicies[0].TokensMonthBehavior != token || record.IPPolicies[0].MoneyMonthBehavior != money {
			t.Fatal("canonical Project modes/identity missing")
		}
		raw, _ := json.Marshal(record.Effective)
		if bytes.Contains(raw, []byte("behavior")) {
			t.Fatal("effective projected synthetic behavior")
		}
	}
	original := read(primaryPath)
	canonical(original, "stop", "stop")
	readerRecord := decodeCatalogResponse[service.LimitRecord](t, requestAs(memberCookie, memberIdentity.CSRFToken, "GET", primaryPath, nil, "", false), 200)
	if readerRecord.ETag != original.ETag {
		t.Fatal("manager scoped review diverged")
	}
	body := map[string]any{"tokens_month": 100, "tokens_month_behavior": "alert_only", "reason": "Original exact reviewed soft Project"}
	expectStatus(t, requestAs(memberCookie, memberIdentity.CSRFToken, "PUT", primaryPath, body, original.ETag, false), 403)
	expectStatus(t, requestAs(outsiderCookie, outsiderCSRF, "GET", primaryPath, nil, "", false), 404)
	applied := decodeCatalogResponse[service.LimitRecord](t, requestAs(writerCookie, writerCSRF, "PUT", primaryPath, body, original.ETag, false), 200)
	canonical(applied, "alert_only", "stop")
	if !applied.Enforced {
		t.Fatal("mode publication unconfirmed")
	}
	write(primaryKeyPath, map[string]any{"tokens_month": 200})
	key := read(primaryKeyPath)
	if key.Kind != "project_key" || key.ID != created.Key.ID || key.AccountID != "key_"+created.Key.ID || key.Stored.TokensMonthBehavior != "stop" || key.Stored.MoneyMonthBehavior != "stop" || len(key.IPPolicies) != 2 || key.IPPolicies[0].TokensMonthBehavior != "alert_only" || key.IPPolicies[1].TokensMonthBehavior != "stop" || key.IPPolicies[1].MoneyMonthBehavior != "stop" || key.Effective.TokensMonth == nil || *key.Effective.TokensMonth != 100 {
		t.Fatal("hard child/soft Project numeric projection changed")
	}
	for _, mode := range []any{nil, "ALERT_ONLY", "alert_only ", []string{"stop"}} {
		expectStatus(t, request("PUT", primaryPath, map[string]any{"tokens_month_behavior": mode, "reason": "Invalid"}, applied.ETag, false), 400)
	}
	for _, mode := range []any{nil, "ALERT_ONLY", "alert_only ", []string{"stop"}} {
		expectStatus(t, request("PUT", primaryKeyPath, map[string]any{"tokens_month_behavior": mode, "reason": "Reject noncanonical Project Key mode"}, key.ETag, false), 400)
	}
	expectStatus(t, call(memberBearer), 200)
	usage := read(primaryPath)
	if usage.QuotaUsage == nil || usage.QuotaUsage.Month.TokensUsed != 150 || usage.QuotaUsage.Active.TokensHeld != 0 {
		t.Fatal("soft Project settlement lost")
	}
	before := dispatches.Load()
	expectStatus(t, call(memberBearer), 429)
	if dispatches.Load() != before {
		t.Fatal("soft Project bypassed hard Key200")
	}
	countAudit := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "project", project.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	auditBefore := countAudit()
	retry := decodeCatalogResponse[service.LimitRecord](t, requestAs(writerCookie, writerCSRF, "PUT", primaryPath, body, original.ETag, false), 200)
	if retry.ETag != applied.ETag || countAudit() != auditBefore || !retry.Enforced {
		t.Fatal("exact retry duplicated revision or failed publication")
	}
	expectStatus(t, request("PUT", primaryPath, map[string]any{"tokens_month": 101, "tokens_month_behavior": "stop", "reason": "stale changed"}, original.ETag, false), 409)
	if read(primaryPath).ETag != applied.ETag {
		t.Fatal("conflict rewrote policy")
	}
	// Clearing the child must not weaken a hard Project parent.
	write(primaryKeyPath, map[string]any{})
	hard := write(primaryPath, map[string]any{"tokens_month": 100})
	canonical(hard, "stop", "stop")
	expectStatus(t, call(memberBearer), 429)
	softChild := decodeCatalogResponse[service.LimitRecord](t, request("PUT", primaryKeyPath, map[string]any{"tokens_month_behavior": "alert_only", "reason": "Reviewed soft Key under hard Project"}, read(primaryKeyPath).ETag, false), 200)
	if softChild.Kind != "project_key" || softChild.ID != created.Key.ID || softChild.AccountID != "key_"+created.Key.ID || softChild.Stored.TokensMonthBehavior != "alert_only" || softChild.Stored.MoneyMonthBehavior != "stop" || len(softChild.IPPolicies) != 2 || softChild.IPPolicies[0].TokensMonthBehavior != "stop" || softChild.IPPolicies[1].TokensMonthBehavior != "alert_only" || softChild.IPPolicies[1].MoneyMonthBehavior != "stop" || !softChild.Enforced {
		t.Fatal("independent soft Key changed hard Project identity or modes")
	}
	expectStatus(t, call(memberBearer), 429)
	// Sparse numeric requests preserve both reviewed modes and unrelated controls.
	write(primaryPath, map[string]any{"tokens_month": 250, "tokens_month_behavior": "alert_only", "money_month": "0", "money_month_behavior": "alert_only", "currency": "USD", "tokens_5h": 1000})
	for _, application := range []struct {
		kind, id string
		patch    map[string]any
	}{
		{"QUOTA", "req_project74_quota", map[string]any{"tokens_month": 500}},
		{"RATE_LIMIT", "req_project74_rate", map[string]any{"rpm": 120}},
	} {
		reviewPath := projectBase + "/request-quota-context"
		if application.kind == "RATE_LIMIT" {
			reviewPath = projectBase + "/request-limits-context"
		}
		var reviewETag string
		if application.kind == "QUOTA" {
			record := decodeCatalogResponse[service.ProjectQuotaRequestContext](t, requestAs(memberCookie, memberIdentity.CSRFToken, "GET", reviewPath, nil, "", false), 200)
			reviewETag = record.ReviewETag
		} else {
			record := decodeCatalogResponse[service.ProjectRequestLimitsContext](t, requestAs(memberCookie, memberIdentity.CSRFToken, "GET", reviewPath, nil, "", false), 200)
			reviewETag = record.ReviewETag
		}
		input := map[string]any{"request_id": application.id, "kind": application.kind, "reason": "Sparse numeric capacity request"}
		if application.kind == "QUOTA" {
			input["quota"] = application.patch
		} else {
			input["rate_limit"] = application.patch
		}
		createdRequest := decodeCatalogResponse[service.ProjectRequestRecord](t, requestAs(memberCookie, memberIdentity.CSRFToken, "POST", projectBase+"/requests", input, reviewETag, false), 201)
		detailPath := projectBase + "/requests/" + createdRequest.ID
		detail := decodeCatalogResponse[service.ProjectRequestRecord](t, request("GET", detailPath, nil, "", false), 200)
		decision := map[string]any{"action": "approve", "reason": "Preserve explicit Project behavior"}
		expectStatus(t, requestAs(memberCookie, memberIdentity.CSRFToken, "POST", detailPath+"/decision", decision, detail.ApprovalReviewETag, false), 403)
		approved := decodeCatalogResponse[service.ProjectRequestRecord](t, request("POST", detailPath+"/decision", decision, detail.ApprovalReviewETag, false), 200)
		if approved.RuntimeApplied == nil || !*approved.RuntimeApplied {
			t.Fatal("sparse approval unconfirmed")
		}
		current := read(primaryPath)
		canonical(current, "alert_only", "alert_only")
		if current.Stored.TokensMonth == nil || *current.Stored.TokensMonth != 500 || current.Stored.MoneyMonth == nil || *current.Stored.MoneyMonth != "0" || current.Stored.Tokens5H == nil || *current.Stored.Tokens5H != 1000 {
			t.Fatal("sparse approval rewrote unrelated reviewed controls")
		}
	}
	if current := read(primaryPath); current.Stored.RPM == nil || *current.Stored.RPM != 120 {
		t.Fatal("rate approval lost numeric patch")
	}
	expectStatus(t, request("PUT", primaryPath, map[string]any{"money_month": "0.000000000000000001", "money_month_behavior": "alert_only", "currency": "EUR", "reason": "Wrong denomination"}, read(primaryPath).ETag, false), 400)
	// Money and Token behaviors are independent; the child cap is already clear.
	write(primaryPath, map[string]any{})
	write(primaryPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "stop"})
	expectStatus(t, call(memberBearer), 429)
	write(primaryPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "stop", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	expectStatus(t, call(memberBearer), 429)
	both := write(primaryPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "currency": "USD", "money_month_behavior": "alert_only"})
	canonical(both, "alert_only", "alert_only")
	expectStatus(t, call(memberBearer), 200)
	usage = read(primaryPath)
	if usage.QuotaUsage.Month.TokensUsed != 300 || usage.QuotaUsage.Month.MoneyUsed["USD"] != "0.0000000000000003" || usage.QuotaUsage.Month.MoneyUnknown != 0 {
		t.Fatal("soft exact settlement changed", usage.QuotaUsage)
	}
	// Fault callbacks were registered before any runtime worker started. The
	// live interval changes only atomics, never GORM's callback processors.
	auditRollbackArmed.Store(true)
	expectStatus(t, request("PUT", primaryPath, map[string]any{"tokens_month_behavior": "stop", "money_month_behavior": "stop", "reason": "Atomic rollback"}, both.ETag, false), 500)
	auditRollbackArmed.Store(false)
	if got := read(primaryPath); got.ETag != both.ETag || got.Stored.TokensMonthBehavior != "alert_only" || got.Stored.MoneyMonthBehavior != "alert_only" {
		t.Fatal("failed audit committed behavior")
	}
	committed.Store(false)
	publicationFaultArmed.Store(true)
	faultBody := map[string]any{"tokens_month": nil, "tokens_month_behavior": "alert_only", "money_month": nil, "money_month_behavior": "alert_only", "reason": "Original uncertain null-cap request"}
	expectStatus(t, request("PUT", primaryPath, faultBody, both.ETag, true), 503)
	if !committed.Load() {
		t.Fatal("failure did not follow genuine commit")
	}
	publicationFaultArmed.Store(false)
	var persisted entity.ResourceLimit
	if err := db.Take(&persisted, "scope_kind = ? AND scope_id = ?", "project", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.ETag == both.ETag || persisted.TokensMonth != nil || persisted.MoneyMonth != nil || persisted.TokensMonthBehavior != "alert_only" || persisted.MoneyMonthBehavior != "alert_only" {
		t.Fatal("uncertain current configuration not exact")
	}
	uncertainAudit := countAudit()
	confirmed := decodeCatalogResponse[service.LimitRecord](t, request("PUT", primaryPath, faultBody, both.ETag, false), 200)
	canonical(confirmed, "alert_only", "alert_only")
	if confirmed.ETag != persisted.ETag || !confirmed.Enforced || countAudit() != uncertainAudit {
		t.Fatal("uncertain exact retry not confirmed/noop")
	}
	// Null caps retain saved inert modes; omission resets both to stop.
	expectStatus(t, call(memberBearer), 200)
	omitted := write(primaryPath, map[string]any{})
	canonical(omitted, "stop", "stop")
	if omitted.Stored.TokensMonth != nil || omitted.Stored.MoneyMonth != nil {
		t.Fatal("omitted caps fabricated capacity")
	}
	expectStatus(t, call(memberBearer), 200)
	// Thresholds use exact settled evidence even under a soft Key policy.
	warning80 := write(primaryPath, map[string]any{"tokens_month": 750, "tokens_month_behavior": "alert_only", "money_month": "0.00000000000000075", "money_month_behavior": "alert_only", "currency": "USD"})
	projectBehaviorAssertWarnings(t, svc, &publicationBarrier, db, project.ID, []string{member.User.ID, peer.User.ID}, warning80.ETag, 80)
	warning90 := write(primaryPath, map[string]any{"tokens_month": 660, "tokens_month_behavior": "alert_only", "money_month": "0.00000000000000066", "money_month_behavior": "alert_only", "currency": "USD"})
	projectBehaviorAssertWarnings(t, svc, &publicationBarrier, db, project.ID, []string{member.User.ID, peer.User.ID}, warning90.ETag, 90)
	ownerPage, err := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	marked := false
	for _, item := range ownerPage.Items {
		if item.QuotaWarning != nil && item.SubjectID == project.ID {
			if _, err := svc.MarkNotificationRead(ctx, member.User.ID, item.ID); err != nil {
				t.Fatal(err)
			}
			marked = true
			break
		}
	}
	if !marked {
		t.Fatal("no current-manager warning to mark")
	}
	for _, forbidden := range []string{admin.User.ID, outsider.User.ID} {
		page, err := svc.ListNotifications(ctx, forbidden, service.NotificationFilter{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if item.QuotaWarning != nil && item.SubjectID == project.ID {
				t.Fatal("warning manager/creator override")
			}
		}
	}
	// Rotation retains the immutable Key root, its saved modes and the same Project account.
	rotated := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", keyBase+"/"+created.Key.ID+"/rotate", map[string]any{"delivery_mode": "manual"}, "", false), 201)
	expectStatus(t, request("POST", keyBase+"/"+rotated.Key.ID+"/confirm", nil, "", false), 200)
	expectStatus(t, request("DELETE", keyBase+"/"+created.Key.ID, nil, "", false), 204)
	replacementPath := keyBase + "/" + rotated.Key.ID + "/limits"
	if got := read(replacementPath); got.AccountID != "key_"+created.Key.ID || got.Stored.TokensMonthBehavior != "alert_only" || got.Stored.MoneyMonthBehavior != "stop" || got.IPPolicies[0].TokensMonthBehavior != "alert_only" || got.QuotaUsage.Month.TokensUsed != 600 {
		t.Fatal("rotation lost shared root/accounting", got.AccountID)
	}
	expectStatus(t, call(rotated.Secret), 200)
	confirmed = read(primaryPath)
	canonical(confirmed, "alert_only", "alert_only")
	if confirmed.QuotaUsage.Month.TokensUsed != 750 || confirmed.QuotaUsage.Month.MoneyUsed["USD"] != "0.00000000000000075" {
		t.Fatal("replacement settlement did not share Project")
	}
	if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	var retainedWarnings int64
	if err := db.Model(&entity.ProjectQuotaWarningObservation{}).Where("project_id = ?", project.ID).Count(&retainedWarnings).Error; err != nil || retainedWarnings != 4 {
		t.Fatal("at/above100 fabricated warning", err, retainedWarnings)
	}
	// Unchanged rolling and rate gates remain hard under two soft monthly dimensions.
	write(primaryPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "money_month_behavior": "alert_only", "currency": "USD", "tokens_5h": 0})
	expectStatus(t, call(rotated.Secret), 429)
	write(primaryPath, map[string]any{"tokens_month": 0, "tokens_month_behavior": "alert_only", "money_month": "0", "money_month_behavior": "alert_only", "currency": "USD", "rpm": 0})
	rateResponse := call(rotated.Secret)
	expectStatus(t, rateResponse, 429)
	confirmed = write(primaryPath, map[string]any{"tokens_month": 660, "tokens_month_behavior": "alert_only", "money_month": "0.00000000000000066", "money_month_behavior": "alert_only", "currency": "USD"})
	// Removed managers lose reads; rejoining the same birth restores retained history, not replay fanout.
	if _, err := svc.SetProjectManagers(ctx, member.User.ID, project.ID, []string{member.User.ID}); err != nil {
		t.Fatal(err)
	}
	projectBehaviorNoPublicWarnings(t, svc, peer.User.ID, project.ID)
	expectStatus(t, requestAs(peerCookie, peerCSRF, "GET", primaryPath, nil, "", false), 404)
	if _, err := svc.SetProjectManagers(ctx, member.User.ID, project.ID, []string{member.User.ID, peer.User.ID}); err != nil {
		t.Fatal(err)
	}
	// A separate Project isolates active holds and unknown terminal coverage.
	coverageProject, err := svc.CreateResource(ctx, admin.User.ID, service.ProjectResource, "Held and unknown Project", "Separate journal coverage", []string{member.User.ID, peer.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	assignManagers(coverageProject.ID)
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, coverageProject.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	coverageBase := "/api/v1/projects/" + coverageProject.ID
	coverage := decodeCatalogResponse[CreatedProjectKeyResponse](t, request("POST", coverageBase+"/keys", map[string]any{"name": "Held Project Key", "delivery_mode": "manual", "model_ids": []string{modelID}}, "", false), 201)
	expectStatus(t, request("POST", coverageBase+"/keys/"+coverage.Key.ID+"/confirm", nil, "", false), 200)
	coveragePath := coverageBase + "/limits"
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
	projectBehaviorNoWarnings(t, db, coverageProject.ID)
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
	// Known settled Tokens stay below 80%; unknown money excludes only its own dimension.
	write(coveragePath, map[string]any{"tokens_month": 200, "tokens_month_behavior": "alert_only", "money_month": "0.000000000000000187", "money_month_behavior": "alert_only", "currency": "USD"})
	unknownUsage := read(coveragePath).QuotaUsage
	if unknownUsage == nil || unknownUsage.Month == nil || unknownUsage.Month.TokensUsed != 150 || unknownUsage.Month.TokensHeld != 150 || unknownUsage.Month.TokensUnknown != 0 || unknownUsage.Month.MoneyUnknown != 1 || unknownUsage.Month.MoneyUsed["USD"] != "0.00000000000000015" || len(unknownUsage.Month.MoneyHeld) != 0 {
		t.Fatal("unknown terminal evidence fabricated money bound/known usage", unknownUsage)
	}
	if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	projectBehaviorNoWarnings(t, db, coverageProject.ID)
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
	// Retained manager links grant no current authority while Project is disabled.
	expectStatus(t, request("PATCH", projectBase, map[string]any{"status": "disabled"}, "", false), 200)
	expectStatus(t, request("PUT", primaryPath, map[string]any{"tokens_month_behavior": "stop", "reason": "Inactive Project"}, confirmed.ETag, false), 409)
	projectBehaviorNoPublicWarnings(t, svc, member.User.ID, project.ID)
	projectBehaviorNoPublicWarnings(t, svc, peer.User.ID, project.ID)
	expectStatus(t, request("PATCH", projectBase, map[string]any{"status": "active"}, "", false), 200)
	if read(primaryPath).ETag != confirmed.ETag {
		t.Fatal("lifecycle rewrote immutable policy")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var warningHistory []entity.ProjectQuotaWarningObservation
	var warningInbox []entity.ProjectQuotaWarningInbox
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
	if dispatches.Load() != 8 || len(attempts) != 8 || len(calls) != 16 {
		t.Fatal("unexpected native dispatch count", dispatches.Load(), len(attempts))
	}
	deniedCounts := map[string]int{}
	primaryCalls, primaryKnown := 0, 0
	for _, row := range calls {
		if row.ProjectID == project.ID {
			primaryCalls++
			if row.ErrorCode == "" {
				primaryKnown++
			}
		}
		switch row.ErrorCode {
		case "quota_exceeded", "rate_limit_exceeded", "quota_usage_unknown":
			deniedCounts[row.ErrorCode]++
			if !projectBehaviorBlockedFact(row, attempts, row.ErrorCode) {
				t.Fatal("blocked call fabricated native/accounting facts", row.ErrorCode)
			}
			if row.ErrorCode == "rate_limit_exceeded" && row.RequestID != rateResponse.Header().Get("X-Request-ID") {
				t.Fatal("RPM denial misattributed")
			}
			if row.ErrorCode == "quota_usage_unknown" && row.RequestID != unknownResponse.Header().Get("X-Request-ID") {
				t.Fatal("unknown denial misattributed")
			}
		}
	}
	if !reflect.DeepEqual(deniedCounts, map[string]int{"quota_exceeded": 6, "rate_limit_exceeded": 1, "quota_usage_unknown": 1}) || primaryCalls != 12 || primaryKnown != 5 {
		t.Fatal("exact call/denial counts", deniedCounts, primaryCalls, primaryKnown)
	}
	knownAttempts := 0
	for _, attempt := range attempts {
		if attempt.CredentialID != "crd_behavior74" || attempt.SnapshotID == "" || attempt.ConnectionID != "con_behavior74" || attempt.ProviderModelID != "pmd_behavior74" || attempt.NativeCompletionEvidence != "completed" || attempt.Status != "success" {
			t.Fatal("attempt lost parser-owned completion/route/publication proof")
		}
		if attempt.FinalUsageKnown {
			knownAttempts++
		}
	}
	if knownAttempts != 7 {
		t.Fatal("unknown native metering was silently repaired", knownAttempts)
	}
	for _, row := range calls {
		if row.ProjectID == project.ID && row.ErrorCode == "" {
			if row.InputTokens == nil || *row.InputTokens != 100 || row.OutputTokens == nil || *row.OutputTokens != 50 || row.PricingStatus != "priced" || row.ChargeAmount == nil || *row.ChargeAmount != "0.00000000000000015" || row.ChargeCurrency == nil || *row.ChargeCurrency != "USD" || row.PricingSnapshotJSON == nil {
				t.Fatal("known primary call lost exact immutable pricing")
			}
		}
	}
	type sessionFact struct {
		ID, UserID           string
		ExpiresAt, CreatedAt time.Time
	}
	var originalSessions []sessionFact
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&originalSessions).Error; err != nil {
		t.Fatal(err)
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
	restarted := read(primaryPath)
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
	var afterHistory []entity.ProjectQuotaWarningObservation
	var afterInbox []entity.ProjectQuotaWarningInbox
	if err := db.Order("id").Find(&afterHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterInbox).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(warningHistory, afterHistory) || !reflect.DeepEqual(warningInbox, afterInbox) {
		t.Fatal("restart expanded warning recipients/history or reset read state")
	}
	var afterSessions []sessionFact
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&afterSessions).Error; err != nil || !reflect.DeepEqual(originalSessions, afterSessions) {
		t.Fatal("restart changed original persisted Session facts", err)
	}
	if len(warningHistory) != 4 || len(warningInbox) != 8 {
		t.Fatal("exact four observations/eight manager inboxes lost", len(warningHistory), len(warningInbox))
	}
	if !reflect.DeepEqual(afterCalls, calls) || !reflect.DeepEqual(afterAttempts, attempts) || dispatches.Load() != 8 {
		t.Fatal("restart changed immutable facts or dispatched")
	}
}

// This fixture observes only genuine known settlement on the Project account.
func projectBehaviorAssertWarnings(t *testing.T, svc *service.Service, barrier *personalKeyWarningFixturePublicationBarrier, db *gorm.DB, project string, managers []string, revision string, threshold int) {
	t.Helper()
	ctx := context.Background()
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := barrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
		t.Fatal(err)
	}
	var rows []entity.ProjectQuotaWarningObservation
	if err := db.Where("project_id = ? AND policy_revision = ?", project, revision).Order("dimension").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatal("both known dimensions must warn", threshold, len(rows))
	}
	for _, row := range rows {
		if row.ProjectID != project || row.Threshold != threshold || row.ThresholdGeneration != "project-monthly-80-90-v1" || row.ResourceCreatedAt.IsZero() || !row.MonthStart.Before(row.MonthEnd) || row.CoverageStart.After(row.ResourceCreatedAt) || row.AsOf.Before(row.MonthStart) || row.TimeZone == "" {
			t.Fatal("warning lost exact calendar/birth/current-policy proof")
		}
		if row.Dimension == "tokens" && row.Settled != "600" || row.Dimension == "money" && (row.Settled != "0.0000000000000006" || row.Currency != "USD") {
			t.Fatal("warning lost exact settled evidence", row.Dimension, row.Settled)
		}
		var inbox []entity.ProjectQuotaWarningInbox
		if err := db.Where("observation_id = ?", row.ID).Order("recipient_id").Find(&inbox).Error; err != nil {
			t.Fatal(err)
		}
		if len(inbox) != 2 {
			t.Fatal("current-manager fanout", len(inbox))
		}
		seen := map[string]bool{}
		for _, item := range inbox {
			if item.ObservationID != row.ID || item.RecipientCreatedAt.IsZero() || seen[item.RecipientID] || item.RecipientID != managers[0] && item.RecipientID != managers[1] {
				t.Fatal("warning recipient alias/duplicate/creator override")
			}
			seen[item.RecipientID] = true
		}
	}
	for _, manager := range managers {
		page, err := svc.ListNotifications(ctx, manager, service.NotificationFilter{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, item := range page.Items {
			if item.SubjectID == project && item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == revision {
				found++
				raw, _ := json.Marshal(item.QuotaWarning)
				var fields map[string]json.RawMessage
				if json.Unmarshal(raw, &fields) != nil || len(fields) != 14 || bytes.Contains(raw, []byte("recipient")) || bytes.Contains(raw, []byte("created_at")) {
					t.Fatal("public warning projected private manager/birth")
				}
			}
		}
		if found != 2 {
			t.Fatal("manager inbox lost exact current warning", found)
		}
	}
}

func projectBehaviorNoPublicWarnings(t *testing.T, svc *service.Service, actor, project string) {
	t.Helper()
	page, err := svc.ListNotifications(context.Background(), actor, service.NotificationFilter{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.SubjectID == project && item.QuotaWarning != nil {
			t.Fatal("removed manager/creator gained historical warning")
		}
	}
}

func projectBehaviorNoWarnings(t *testing.T, db *gorm.DB, project string) {
	t.Helper()
	var n int64
	if err := db.Model(&entity.ProjectQuotaWarningObservation{}).Where("project_id = ?", project).Count(&n).Error; err != nil || n != 0 {
		t.Fatal("held/unknown usage became percentage warning", err, n)
	}
}

func projectBehaviorBlockedFact(row entity.CallRecord, attempts []entity.CallAttempt, code string) bool {
	if code != "quota_exceeded" && code != "rate_limit_exceeded" && code != "quota_usage_unknown" {
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

func TestProjectBehaviorBlockedFactsDistinguishRateUnknownAndQuota(t *testing.T) {
	for _, code := range []string{"quota_exceeded", "rate_limit_exceeded", "quota_usage_unknown"} {
		row := entity.CallRecord{RequestID: "call_project_denied", Status: "error", ErrorCode: code, CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
		if !projectBehaviorBlockedFact(row, nil, code) {
			t.Fatal("real blocked fact rejected", code)
		}
		if projectBehaviorBlockedFact(row, []entity.CallAttempt{{RequestID: row.RequestID}}, code) {
			t.Fatal("blocked native attempt accepted")
		}
		zero := int64(0)
		bad := row
		bad.InputTokens = &zero
		if projectBehaviorBlockedFact(bad, nil, code) {
			t.Fatal("fabricated zero usage accepted")
		}
		value := "0"
		bad = row
		bad.ChargeAmount = &value
		if projectBehaviorBlockedFact(bad, nil, code) {
			t.Fatal("fabricated zero charge accepted")
		}
		if projectBehaviorBlockedFact(row, nil, "other") || projectBehaviorBlockedFact(row, nil, map[string]string{"quota_exceeded": "rate_limit_exceeded", "rate_limit_exceeded": "quota_usage_unknown", "quota_usage_unknown": "quota_exceeded"}[code]) {
			t.Fatal("distinct errors collapsed")
		}
	}
}

// V74 is the only new fence removed here. The existing V70/V71/V73 scenarios
// keep their original functions, negative assertions and historical companions.
func projectBehaviorHistoricalReplay(t *testing.T, db *gorm.DB, historical func(*testing.T, *gorm.DB)) {
	t.Helper()
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	hasV75 := db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v75")
	if hasV75 {
		if (len(before) != 75 && len(before) != 76 && len(before) != 77 && len(before) != 78 && len(before) != 79 && len(before) != 80 && len(before) != 81) || before[74].Version != 75 || len(before) >= 76 && before[75].Version != 76 || len(before) >= 77 && before[76].Version != 77 || len(before) >= 78 && before[77].Version != 78 || len(before) >= 79 && before[78].Version != 79 || len(before) >= 80 && before[79].Version != 80 || len(before) == 81 && before[80].Version != 81 {
			t.Fatal("exact V75 predecessor required")
		}
		if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v75"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateConstraint(&teamMemberBehaviorFrozenV74{}, "ck_resource_limits_monthly_behavior_scope_v74"); err != nil {
			t.Fatal(err)
		}
	}
	if (len(before) != 74 && len(before) != 75 && len(before) != 76 && len(before) != 77 && len(before) != 78 && len(before) != 79 && len(before) != 80 && len(before) != 81) || before[73].Version != 74 || len(before) == 81 && before[80].Version != 81 || !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v74") {
		t.Fatal("exact V74 predecessor required")
	}
	defer func() {
		if err := db.Table("schema_migrations").Where("version = ?", 74).Delete(&struct{}{}).Error; err != nil {
			t.Error(err)
			return
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Error("restore V74", err)
			return
		}
		replayed := []int{74, 73, 71}
		if hasV75 {
			if err := db.Table("schema_migrations").Where("version = ?", 75).Delete(&struct{}{}).Error; err != nil {
				t.Error(err)
				return
			}
			if err := database.Migrate(ctx, db); err != nil {
				t.Error("restore V75", err)
				return
			}
			replayed = append(replayed, 75)
		}
		if reflect.ValueOf(historical).Pointer() == reflect.ValueOf(testPersonalMonthlyBehaviorMigration).Pointer() {
			replayed = append(replayed, 70)
		}
		if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), replayed...) {
			t.Error("unrelated ledger timestamp/version changed")
		}
		currentFence := "ck_resource_limits_monthly_behavior_scope_v74"
		if hasV75 {
			currentFence = "ck_resource_limits_monthly_behavior_scope_v75"
		}
		if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, currentFence) || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73") || hasV75 && db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v74") {
			t.Error("current exact monthly fence not restored")
		}
	}()
	if reflect.ValueOf(historical).Pointer() == reflect.ValueOf(testProjectMonthlyBehaviorMigrationV74).Pointer() {
		historical(t, db)
		return
	}
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v74"); err != nil {
		t.Fatal(err)
	}
	// Keep V74 recorded so every genuine Migrate inside the historical scenario
	// skips that later fence. Reconstruct V73 explicitly after V74 removed it.
	q := db.Table("schema_migrations").Where("version = ?", 73).Delete(&struct{}{})
	if q.Error != nil || q.RowsAffected != 1 {
		t.Fatal("reconstruct only V73 fixture ledger", q.Error)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore historical V73 fence", err)
	}
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73") || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v74") {
		t.Fatal("exact historical V73 fence required")
	}
	if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 73) {
		t.Fatal("historical setup changed V74 or unrelated ledger timestamps")
	}
	switch reflect.ValueOf(historical).Pointer() {
	case reflect.ValueOf(testPersonalMonthlyBehaviorMigration).Pointer(), reflect.ValueOf(testTeamMonthlyBehaviorMigration).Pointer():
		personalKeyBehaviorHistoricalReplay(t, db, historical)
	case reflect.ValueOf(testPersonalKeyMonthlyBehaviorMigration).Pointer():
		historical(t, db)
	default:
		t.Fatal("unsupported historical scenario")
	}
}

func testProjectBehaviorPersonalKeyMigrationAtCurrentSchema(t *testing.T, db *gorm.DB) {
	projectBehaviorHistoricalReplay(t, db, testPersonalKeyMonthlyBehaviorMigration)
}

func TestProjectBehaviorRegistryAppendAndHistoricalWrapperGuard(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, 0, len(matches))
	for _, pair := range matches {
		names = append(names, pair[1]+":"+pair[2])
	}
	if !teamMemberMonthlyRegistryTail(names) {
		t.Fatal("exact144 registry tail changed")
	}
	names = names[:142]
	if len(names) != 142 || names[141] != "project_key_monthly_behavior:testProjectKeyMonthlyBehaviorLifecycle" || !personalKeyBehaviorRegistryMatches(names[:141]) {
		t.Fatal("exact139 inherited names+reviewed wrapper+Project pair changed", len(names))
	}
	// Preserve every historical prefix mutation oracle against exactly that prefix.
	names = names[:141]
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:140] },
		func(x []string) []string { x[139], x[140] = x[140], x[139]; return x },
		func(x []string) []string { x[137] = "personal_key_monthly_behavior_migration:unreviewed"; return x },
		func(x []string) []string { x[140] = x[139]; return x },
		func(x []string) []string { return append(x, "extra:unreviewed") },
	} {
		copyNames := append([]string(nil), names...)
		if personalKeyBehaviorRegistryMatches(mutate(copyNames)) {
			t.Fatal("missing/reordered/extra/unreviewed scenario accepted")
		}
	}
	if !strings.Contains(string(raw), "versions != 81") || !strings.Contains(string(raw), "projectBehaviorHistoricalReplay(t, db, test.run)") || !strings.Contains(string(raw), "personalKeyBehaviorHistoricalReplay(t, db, test.run)") {
		t.Fatal("current74 or retained historical71/73 companion not bound")
	}
}

// Callback processors are immutable for the entire worker lifetime, including
// StartCallRecorder failures. Only fault flags change after StartRuntime.
func projectBehaviorCallbackLifetime(raw []byte) bool {
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", raw, 0)
	if err != nil {
		return false
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "testProjectMonthlyBehaviorLifecycle" {
			fn = f
		}
	}
	if fn == nil {
		return false
	}
	firstStart := token.Pos(0)
	registers, removes := []token.Pos{}, []token.Pos{}
	deferred := []*ast.DeferStmt{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if d, ok := n.(*ast.DeferStmt); ok {
			deferred = append(deferred, d)
		}
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "StartRuntime":
			if firstStart == 0 || c.Pos() < firstStart {
				firstStart = c.Pos()
			}
		case "Register":
			registers = append(registers, c.Pos())
		case "Remove":
			removes = append(removes, c.Pos())
		}
		return true
	})
	if firstStart == 0 || len(registers) != 4 || len(removes) != 4 {
		return false
	}
	for _, pos := range registers {
		if pos >= firstStart {
			return false
		}
	}
	for _, pos := range removes {
		safe := false
		for _, d := range deferred {
			if d.Pos() < firstStart && pos >= d.Pos() && pos <= d.End() {
				safe = true
			}
		}
		if !safe {
			return false
		}
	}
	shutdown := false
	for _, d := range deferred {
		if d.Pos() < firstStart {
			continue
		}
		runtime, recorder := false, false
		ast.Inspect(d, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
					switch sel.Sel.Name {
					case "StopRuntime":
						runtime = true
					case "StopCallRecorder":
						recorder = true
					}
				}
			}
			return true
		})
		if runtime && recorder {
			shutdown = true
		}
	}
	return shutdown
}

func TestProjectBehaviorFixtureCallbackProcessorsRemainQuiescent(t *testing.T) {
	raw, err := os.ReadFile("project_monthly_behavior_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !projectBehaviorCallbackLifetime(raw) {
		t.Fatal("callbacks can mutate while runtime workers are alive")
	}
	for _, invalid := range []string{
		strings.Replace(string(raw), `spool := filepath.Join(t.TempDir(), "monthly-behavior.db")`, `db.Callback().Query().Remove(readCallback)
 spool := filepath.Join(t.TempDir(), "monthly-behavior.db")`, 1),
		strings.Replace(string(raw), `spool := filepath.Join(t.TempDir(), "monthly-behavior.db")`, `db.Callback().Query().Before("gorm:query").Register("late", publicationBarrier.beforeQuery)
 spool := filepath.Join(t.TempDir(), "monthly-behavior.db")`, 1),
		strings.Replace(string(raw), "svc.StopRuntime()", "svc.MissingRuntimeShutdown()", 1),
	} {
		if projectBehaviorCallbackLifetime([]byte(invalid)) {
			t.Fatal("live mutation or missing shutdown accepted")
		}
	}
}
