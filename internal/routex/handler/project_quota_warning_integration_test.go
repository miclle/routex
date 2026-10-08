package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// testProjectMonthlyQuotaWarningLifecycle runs against each supported database in
// the fresh-database harness. Warmup establishes real journal coverage before
// any notified resource is created; no historical accounting is manufactured.
func testProjectMonthlyQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{97}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var unknown atomic.Bool
	var single atomic.Bool
	var dispatches atomic.Int64
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	// Register release before any held request can start, including late entry.
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
		_, _ = io.WriteString(w, quotaWarningNativeBody(single.Load(), unknown.Load()))
	}))
	defer func() {
		// This later defer runs first: release before waiting for server handlers.
		releaseOnce.Do(func() { close(release) })
		upstream.Close()
	}()
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
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"warning-admin@example.invalid","password":"monthly-notification-password","name":"Quota admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	// Capture router by reference so restart exercises the same persisted sessions.
	request := func(cookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(cookie)
		if etag != "" {
			req.Header.Set("If-Match", `"`+etag+`"`)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	adminRequest := func(method, path string, body any, etag string) *httptest.ResponseRecorder {
		return request(adminCookie, admin.CSRFToken, method, path, body, etag)
	}
	cipher, err := store.Seal("crd_project_warning", "project-warning-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_project_warning"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_project_warning", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_project_warning", ProviderID: "prv_project_warning", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_project_warning", ConnectionID: "con_project_warning", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_project_warning", ConnectionID: "con_project_warning", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_project_warning", ProviderModelID: "pmd_project_warning"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "project-warning-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_project_warning", ModelID: modelID, ProviderModelID: "pmd_project_warning", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_project_warning_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_project_warning_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_project_warning", ProviderModelID: "pmd_project_warning", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_project_warning_%d", index), ModelPriceID: "price_project_warning", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	spool := filepath.Join(t.TempDir(), "warnings.db")
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime() // Deterministic publication: later changes use only explicit RefreshRuntime.
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		releaseOnce.Do(func() { close(release) })
		nativeRequests.Wait() // Each request owns its existing bounded context.
		svc.StopRuntime()
		_ = svc.StopCallRecorder()
	}()
	call := func(bearer string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"project-warning-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_project_warning/reservation-bound"
	expectStatus(t, adminRequest("PUT", boundPath, map[string]any{"max_input_tokens": 1, "max_output_tokens": 1, "evidence": "Controlled native response capacity", "reason": "Project warning acceptance"}, "0"), 200)
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// First use changes accounting activation; publish that real generation before
	// creating later resources or asking the observer for current-policy evidence.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "project-warning-member@example.invalid", "monthly-notification-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "project-warning-outsider@example.invalid", "monthly-notification-password", "Other member", "member")
	if err != nil {
		t.Fatal(err)
	}

	owner, err := svc.CreateMember(ctx, admin.User.ID, "project-warning-owner@example.invalid", "monthly-notification-password", "Project owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	later, err := svc.CreateMember(ctx, admin.User.ID, "project-warning-later@example.invalid", "monthly-notification-password", "Later member", "member")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []*service.MemberRecord{member, owner, later, outsider} {
		if err := db.Take(&record.User, "id = ?", record.User.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	projectID := "prj_project_warning"
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-warning-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
	memberAuth, memberCookie := readIdentity(t, login)
	memberRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return request(memberCookie, memberAuth.CSRFToken, method, path, body, "")
	}
	personalBearer := "rx_" + strings.Repeat("w", 43)
	create := func(row any) {
		t.Helper()
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID})
	create(&entity.APIKey{ID: "key_project_warning_member", UserID: member.User.ID, Name: "Personal warning", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_project_warning_member", ModelID: modelID})
	create(&entity.Project{ID: projectID, Name: "Frozen aggregate Project", Status: entity.ResourceActive, CreatorID: admin.User.ID})
	create(&entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID})
	create(&entity.ProjectManager{ID: "pmg_project_warning_caller", ProjectID: projectID, UserID: member.User.ID})
	create(&entity.ProjectManager{ID: "pmg_project_warning_owner", ProjectID: projectID, UserID: owner.User.ID})
	pending, err := svc.CreateMember(ctx, admin.User.ID, "project-warning-pending@example.invalid", "monthly-notification-password", "Pending Project member", "member")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&pending.User, "id = ?", pending.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	applicationID := "raa_01j00000000000000000000000"
	create(&entity.RegistrationApprovalApplication{ID: applicationID, UserID: pending.User.ID, UserCreatedAt: pending.User.CreatedAt, CreatedAt: pending.User.CreatedAt, State: "pending", Revision: strings.Repeat("a", 64)})
	if err := db.Model(&entity.User{}).Where("id = ?", pending.User.ID).UpdateColumn("approval_application_id", applicationID).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.ProjectManager{ID: "pmg_project_warning_pending", ProjectID: projectID, UserID: pending.User.ID})
	// A collation alias may be retained by a permissive driver, but never gains
	// exact recipient authority. The late member later receives a canonical link.
	aliasProjectID := "pmg_warning_alias_project"
	aliasError := db.Create(&entity.ProjectManager{ID: aliasProjectID, ProjectID: strings.ToUpper(projectID), UserID: later.User.ID}).Error
	if aliasError != nil && !errors.Is(aliasError, gorm.ErrForeignKeyViolated) {
		t.Fatal("alias Project membership failed for unrelated reason", aliasError)
	}
	aliasStored := aliasError == nil
	projectBearers := map[string]string{}
	projectKeyIDs := map[string]string{}
	createProjectKey := func(target string) {
		t.Helper()
		created, err := svc.CreateProjectKey(ctx, member.User.ID, target, "Controlled warning Key", "manual", []string{modelID}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ConfirmProjectKey(ctx, member.User.ID, target, created.Record.Key.ID); err != nil {
			t.Fatal(err)
		}
		projectBearers[target], projectKeyIDs[target] = created.Secret, created.Record.Key.ID
	}
	createProjectKey(projectID)
	projectCall := func(target string) *httptest.ResponseRecorder {
		t.Helper()
		bearer, ok := projectBearers[target]
		if !ok || bearer == "" {
			t.Fatal("exact Project Key not prepared")
		}
		return call(bearer)
	}
	var lastRefreshElapsed, lastReconcileElapsed time.Duration
	var lastReconcileBefore, lastReconcileAfter service.RuntimeStatus
	refresh := func() {
		t.Helper()
		started := time.Now()
		err := svc.RefreshRuntime(ctx)
		lastRefreshElapsed = time.Since(started)
		if err != nil {
			t.Fatal(err)
		}
	}
	flush := func() {
		t.Helper()
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	userPath := "/api/v1/projects/" + projectID + "/limits"
	write := func(path string, values map[string]any) entity.ResourceLimit {
		t.Helper()
		current := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200)
		complete := map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": nil, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "currency": "USD", "reason": "Project aggregate monthly warning acceptance"}
		for key, value := range values {
			complete[key] = value
		}
		result := decodeCatalogResponse[service.LimitRecord](t, adminRequest("PUT", path, complete, current.ETag), 200)
		if !result.Enforced {
			t.Fatal("Project policy not applied")
		}
		target := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/projects/"), "/limits")
		var row entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "project", target).Take(&row).Error; err != nil || row.ScopeID != target {
			t.Fatal("stored Project revision unavailable", err)
		}
		return row // Project reviewed HTTP ETag differs from the recorded policy revision.
	}
	reconcileUnpublished := func() {
		t.Helper()
		lastReconcileBefore = svc.RuntimeStatus()
		started := time.Now()
		err := svc.ReconcileMonthlyQuotaNotifications(ctx)
		lastReconcileElapsed = time.Since(started)
		lastReconcileAfter = svc.RuntimeStatus()
		if err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() { t.Helper(); refresh(); reconcileUnpublished() }
	page := func() service.NotificationPage {
		t.Helper()
		return decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	}
	count := func(owner string) int64 {
		t.Helper()
		var value int64
		if err := db.Model(&entity.ProjectQuotaWarningObservation{}).Where(database.ExactText(db, clause.Column{Name: "project_id"}, owner)).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertCount := func(owner string, expected int64) {
		t.Helper()
		if got := count(owner); got != expected {
			t.Fatalf("owner warning count %d, want %d; last_refresh=%s last_reconcile=%s runtime_before=%+v runtime_after=%+v runtime_current=%+v", got, expected, lastRefreshElapsed, lastReconcileElapsed, lastReconcileBefore, lastReconcileAfter, svc.RuntimeStatus())
		}
	}
	// More than one 32-row page precedes the notified owner. Full manual reconcile
	// must reach it; these covered-zero positive caps cannot manufacture warnings.
	cap100 := int64(100)
	scanIDs := make([]string, 0, 33)
	for index := range 33 {
		id := fmt.Sprintf("prj_000_warn_%02d", index)
		scanIDs = append(scanIDs, id)
		create(&entity.Project{ID: id, Name: "Covered zero Project scan", Status: entity.ResourceActive, CreatorID: admin.User.ID})
		create(&entity.ResourceLimit{ScopeKind: "project", ScopeID: id, ETag: fmt.Sprintf("lim_warning_scan_%02d", index), ActorID: admin.User.ID, Reason: "Bounded fair warning scan", TokensMonth: &cap100, IPMode: "none", IPRangesJSON: "[]"})
	}
	refresh()
	expectStatus(t, call(personalBearer), 200)
	flush()
	refresh()
	personalUsage, err := svc.GetResourceLimit(ctx, member.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personalUsage.QuotaUsage == nil || personalUsage.QuotaUsage.Month == nil || personalUsage.QuotaUsage.Month.TokensUsed != 2 {
		t.Fatal("real Personal isolation calibration failed", err)
	}
	policy := write(userPath, map[string]any{"tokens_month": 10})
	reconcile()
	assertCount(projectID, 0)
	for index := range 3 {
		expectStatus(t, projectCall(projectID), 200)
		flush()
		reconcile()
		assertCount(projectID, 0)
		if index == 2 && dispatches.Load() != 5 {
			t.Fatal("unexpected dispatch before near level")
		}
	}
	expectStatus(t, projectCall(projectID), 200)
	flush()
	// Settlement alone never sends an inbox warning through the gateway.
	assertCount(projectID, 0)
	reconcile()
	assertCount(projectID, 1)
	nearPage := page()
	if len(nearPage.Items) != 1 || nearPage.UnreadCount != 1 {
		t.Fatal("near warning missing from recipient inbox")
	}
	near := nearPage.Items[0]
	if near.QuotaWarning == nil || near.QuotaWarning.ScopeKind != "project" || near.QuotaWarning.ThresholdGeneration != "project-monthly-80-90-v1" || near.SubjectType != "project" || near.SubjectID != projectID || near.SubjectName != "Frozen aggregate Project" || near.Quota != nil || near.Kind != "monthly_quota_warning" || near.Severity != "medium" || near.QuotaWarning.Level != "near" || near.QuotaWarning.Threshold != 80 || near.QuotaWarning.Settled != "8" || near.QuotaWarning.Limit != "10" || near.QuotaWarning.Currency != nil || near.QuotaWarning.PolicyRevision != policy.ETag || near.QuotaWarning.ScopeID != projectID {
		t.Fatal("near snapshot was not exact")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("warning read was not recorded")
	}
	var readInbox entity.ProjectQuotaWarningInbox
	if err := db.Take(&readInbox, "id = ?", near.ID).Error; err != nil || readInbox.ReadAt == nil {
		t.Fatal("persisted read timestamp missing", err)
	}
	var recipientRows []entity.ProjectQuotaWarningInbox
	if err := db.Where("observation_id = ?", near.QuotaWarningObservationID).Order("recipient_id").Find(&recipientRows).Error; err != nil || len(recipientRows) != 2 {
		t.Fatal("bounded original Project fanout", err)
	}
	var pendingReceipts int64
	if err := db.Model(&entity.ProjectQuotaWarningInbox{}).Where("recipient_id = ?", pending.User.ID).Count(&pendingReceipts).Error; err != nil || pendingReceipts != 0 {
		t.Fatal("pending account inherited Project warning receipt", err)
	}
	expectedBirths := map[string]time.Time{member.User.ID: member.User.CreatedAt, owner.User.ID: owner.User.CreatedAt}
	for _, receipt := range recipientRows {
		birth, ok := expectedBirths[receipt.RecipientID]
		if !ok || !receipt.RecipientCreatedAt.Equal(birth) {
			t.Fatal("recipient birth not captured privately")
		}
	}
	var original entity.ProjectQuotaWarningObservation
	if err := db.Take(&original, "id = ?", near.QuotaWarningObservationID).Error; err != nil {
		t.Fatal(err)
	}
	// The genuine second-page near crossing and original fanout/read proof above
	// complete the fair-scan calibration. Retain its Projects and empty history,
	// but remove only their artificial positive caps from later policy scans.
	if err := db.Transaction(func(tx *gorm.DB) error {
		var projects []entity.Project
		if err := tx.Where("id IN ?", scanIDs).Order("id").Find(&projects).Error; err != nil {
			return err
		}
		var policies []entity.ResourceLimit
		query := tx.Model(&entity.ResourceLimit{}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where("scope_id IN ?", scanIDs)
		if err := query.Clauses(clause.Locking{Strength: "UPDATE"}).Order("scope_id").Find(&policies).Error; err != nil {
			return err
		}
		if len(projects) != 33 || len(policies) != 33 {
			return errors.New("fair-scan calibration identities incomplete")
		}
		for index, row := range policies {
			project := projects[index]
			if row.ScopeKind != "project" || row.ScopeID != scanIDs[index] || row.ETag != fmt.Sprintf("lim_warning_scan_%02d", index) || row.ActorID != admin.User.ID || row.Reason != "Bounded fair warning scan" || row.TokensMonth == nil || *row.TokensMonth != 100 || row.MoneyMonth != nil || row.Tokens5H != nil || row.Tokens7D != nil || row.RPM != nil || row.TPM != nil || row.Concurrency != nil || row.IPMode != "none" || row.IPRangesJSON != "[]" || project.ID != scanIDs[index] || project.Name != "Covered zero Project scan" || project.Status != entity.ResourceActive || project.CreatorID != admin.User.ID || project.CreatedAt.IsZero() {
				return errors.New("fair-scan calibration identity or policy changed")
			}
		}
		var observations int64
		if err := tx.Model(&entity.ProjectQuotaWarningObservation{}).Where("project_id IN ?", scanIDs).Count(&observations).Error; err != nil {
			return err
		}
		if observations != 0 {
			return errors.New("covered-zero fair-scan calibration created warnings")
		}
		updated := tx.Model(&entity.ResourceLimit{}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where("scope_id IN ? AND tokens_month = ?", scanIDs, 100).UpdateColumn("tokens_month", nil)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 33 {
			return errors.New("fair-scan calibration retirement was not exact")
		}
		var retainedPolicies []entity.ResourceLimit
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "project")).Where("scope_id IN ?", scanIDs).Order("scope_id").Find(&retainedPolicies).Error; err != nil {
			return err
		}
		for index := range policies {
			policies[index].TokensMonth = nil
		}
		var retainedProjects []entity.Project
		if err := tx.Where("id IN ?", scanIDs).Order("id").Find(&retainedProjects).Error; err != nil {
			return err
		}
		if !reflect.DeepEqual(policies, retainedPolicies) || !reflect.DeepEqual(projects, retainedProjects) {
			return errors.New("fair-scan retirement changed retained policy or Project facts")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	refresh()
	single.Store(true)
	expectStatus(t, projectCall(projectID), 200)
	single.Store(false)
	flush()
	reconcile()
	reconcile()
	assertCount(projectID, 2)
	criticalPage := page()
	if len(criticalPage.Items) != 2 || criticalPage.UnreadCount != 1 {
		t.Fatal("escalation reset near read state or lost critical")
	}
	for _, item := range criticalPage.Items {
		if item.ID == near.ID {
			if !item.Read || item.ReadAt == nil || !item.ReadAt.Equal(*readInbox.ReadAt) || !reflect.DeepEqual(item.QuotaWarning, near.QuotaWarning) {
				t.Fatal("reconcile changed immutable near snapshot/read state")
			}
			continue
		}
		if item.QuotaWarning == nil || item.QuotaWarning.Level != "critical" || item.QuotaWarning.Threshold != 90 || item.QuotaWarning.Settled != "9" || item.QuotaWarning.PolicyRevision != policy.ETag || item.Severity != "high" {
			t.Fatal("critical was not a separate exact current level")
		}
	}
	usage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", userPath, nil, ""), 200).QuotaUsage
	if usage == nil || usage.Month == nil || !usage.Month.Covered || usage.Month.TokensUsed != 9 || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUsed["USD"] != "9" || usage.Month.MoneyUnknown != 0 {
		t.Fatal("native known settlement was not exact")
	}
	personalUsage, err = svc.GetResourceLimit(ctx, member.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personalUsage.QuotaUsage == nil || personalUsage.QuotaUsage.Month == nil || personalUsage.QuotaUsage.Month.TokensUsed != 2 {
		t.Fatal("Project settlement charged Personal account", err)
	}
	for _, item := range criticalPage.Items {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"recipient_id", "recipient_created_at", "resource_created_at", "coverage_start"} {
			if strings.Contains(string(raw), private) {
				t.Fatal("private warning proof leaked", private)
			}
		}
	}
	// A first critical observation under a new revision creates only critical.
	write(userPath, map[string]any{"tokens_month": 10, "rpm": 100})
	reconcile()
	assertCount(projectID, 3)
	// Independent money levels use already settled, known USD amounts. No money
	// reservation or fictitious earlier crossing is needed to produce these facts.
	moneyNear := write(userPath, map[string]any{"money_month": "11.25", "currency": "USD"})
	reconcile()
	assertCount(projectID, 4)
	moneyCritical := write(userPath, map[string]any{"money_month": "10", "currency": "USD"})
	reconcile()
	assertCount(projectID, 5)
	precise := write(userPath, map[string]any{"money_month": "10.000000000000000001", "currency": "USD"})
	reconcile()
	assertCount(projectID, 6)
	found := map[string]bool{}
	for _, item := range page().Items {
		q := item.QuotaWarning
		if q == nil || q.Dimension != "money" {
			continue
		}
		if q.Currency == nil || *q.Currency != "USD" || q.Settled != "9" {
			t.Fatal("money converted currency or lost settlement")
		}
		switch q.PolicyRevision {
		case moneyNear.ETag:
			if q.Level != "near" || q.Limit != "11.25" {
				t.Fatal("exact money80 failed")
			}
			found["near"] = true
		case moneyCritical.ETag:
			if q.Level != "critical" || q.Limit != "10" {
				t.Fatal("exact money90 failed")
			}
			found["critical"] = true
		case precise.ETag:
			if q.Level != "near" || q.Limit != "10.000000000000000001" {
				t.Fatal("18-place money boundary rounded upward")
			}
			found["precise"] = true
		}
	}
	if len(found) != 3 {
		t.Fatal("independent exact money observations missing")
	}
	// An unpublished denomination must not create a current-policy money level.
	unpublished := write(userPath, map[string]any{"money_month": "11", "currency": "USD"})
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	reconcileUnpublished()
	assertCount(projectID, 6)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	assertCount(projectID, 7)
	for _, item := range page().Items {
		if item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == unpublished.ETag && (item.QuotaWarning.Currency == nil || *item.QuotaWarning.Currency != "USD") {
			t.Fatal("denomination restoration changed historical currency")
		}
	}
	// Stale runtime policy cannot prove warning eligibility; exact restoration
	// leaves the original immutable records intact.
	var savedPolicy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "project", projectID).Take(&savedPolicy).Error; err != nil {
		t.Fatal(err)
	}
	assertPolicyRow := func(want entity.ResourceLimit) {
		t.Helper()
		var got entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "project", projectID).Take(&got).Error; err != nil {
			t.Fatal("stored policy readback failed", err)
		}
		if !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatal("policy mutation changed its original timestamp")
		}
		got.UpdatedAt = want.UpdatedAt // Compare instants independently of driver time location.
		if !reflect.DeepEqual(got, want) {
			t.Fatal("policy mutation did not preserve every unrelated stored field")
		}
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "project", projectID).UpdateColumn("ETag", "lim_warning_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	changedPolicy := savedPolicy
	changedPolicy.ETag = "lim_warning_unpublished"
	assertPolicyRow(changedPolicy)
	reconcileUnpublished()
	assertCount(projectID, 7)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "project", projectID).UpdateColumn("ETag", savedPolicy.ETag).Error; err != nil {
		t.Fatal(err)
	}
	assertPolicyRow(savedPolicy)

	// A new eligible generation has no prior dedup row, so each stale-calendar
	// rejection is distinguished from a harmless replay of an existing warning.
	calendarPolicy := write(userPath, map[string]any{"money_month": "11.125", "currency": "USD"})
	var savedCalendar entity.QuotaSetting
	if err := db.Take(&savedCalendar, 1).Error; err != nil {
		t.Fatal(err)
	}
	assertCalendarRow := func(want entity.QuotaSetting) {
		t.Helper()
		var got entity.QuotaSetting
		if err := db.Take(&got, 1).Error; err != nil {
			t.Fatal("calendar readback failed", err)
		}
		if !got.UpdatedAt.Equal(want.UpdatedAt) {
			t.Fatal("calendar mutation changed its original timestamp")
		}
		got.UpdatedAt = want.UpdatedAt
		if !reflect.DeepEqual(got, want) {
			t.Fatal("calendar mutation did not preserve every unrelated stored field")
		}
	}
	for _, change := range []struct {
		field             string
		invalid, original any
	}{
		{"ETag", strings.Repeat("c", 64), savedCalendar.ETag},
		{"time_zone", "Etc/UTC", savedCalendar.TimeZone},
		{"accounting_started", false, savedCalendar.AccountingStarted},
	} {
		if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).UpdateColumn(change.field, change.invalid).Error; err != nil {
			t.Fatal(err)
		}
		changedCalendar := savedCalendar
		switch change.field {
		case "ETag":
			changedCalendar.ETag = change.invalid.(string)
		case "time_zone":
			changedCalendar.TimeZone = change.invalid.(string)
		case "accounting_started":
			changedCalendar.AccountingStarted = change.invalid.(bool)
		}
		assertCalendarRow(changedCalendar)
		reconcileUnpublished()
		assertCount(projectID, 7)
		if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).UpdateColumn(change.field, change.original).Error; err != nil {
			t.Fatal(err)
		}
		assertCalendarRow(savedCalendar)
	}
	reconcile()
	assertCount(projectID, 8)
	calendarFound := false
	for _, item := range page().Items {
		if item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == calendarPolicy.ETag {
			if item.QuotaWarning.Level != "near" || item.QuotaWarning.Settled != "9" || item.QuotaWarning.TimeZone != savedCalendar.TimeZone {
				t.Fatal("calendar restoration changed exact settled snapshot")
			}
			calendarFound = true
		}
	}
	if !calendarFound {
		t.Fatal("fresh restored calendar lost eligible observation")
	}
	var savedProject entity.Project
	if err := db.Take(&savedProject, "id = ?", projectID).Error; err != nil {
		t.Fatal(err)
	}
	// Begin each unpublished identity negative with a fresh real authorization
	// lease. The stopped refresh worker must not make prior HTTP checks consume
	// the lease intended for this independent birth-mismatch assertion.
	refresh()
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).UpdateColumn("created_at", savedProject.CreatedAt.Add(time.Millisecond)).Error; err != nil {
		t.Fatal(err)
	}
	var changedProject entity.Project
	if err := db.Take(&changedProject, "id = ?", projectID).Error; err != nil || !changedProject.CreatedAt.Equal(savedProject.CreatedAt.Add(time.Millisecond)) || changedProject.CreatedAt.Equal(savedProject.CreatedAt) {
		t.Fatal("Project birth negative was not persisted distinctly", err)
	}
	if len(page().Items) != 0 {
		t.Fatal("recreated Project inherited original warning history")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	reconcileUnpublished()
	assertCount(projectID, 8)
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).UpdateColumn("created_at", savedProject.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	var restoredProject entity.Project
	if err := db.Take(&restoredProject, "id = ?", projectID).Error; err != nil || !restoredProject.CreatedAt.Equal(savedProject.CreatedAt) {
		t.Fatal("exact Project birth restoration failed", err)
	}
	refresh()
	// A different birth with the same retained ID must hide old private history.
	var savedUser entity.User
	if err := db.Take(&savedUser, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	changedBirth := savedUser.CreatedAt.Add(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("created_at", changedBirth).Error; err != nil {
		t.Fatal(err)
	}
	var changedUser entity.User
	if err := db.Take(&changedUser, "id = ?", member.User.ID).Error; err != nil || !changedUser.CreatedAt.Equal(changedBirth) || changedUser.CreatedAt.Equal(savedUser.CreatedAt) {
		t.Fatal("recipient birth negative was not persisted distinctly", err)
	}
	if len(page().Items) != 0 {
		t.Fatal("different birth exposed old warnings")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	var birthReadCount int64
	if err := db.Model(&entity.ProjectQuotaWarningInbox{}).Where("recipient_id = ? AND read_at IS NOT NULL", member.User.ID).Count(&birthReadCount).Error; err != nil || birthReadCount != 1 {
		t.Fatal("read-all crossed recipient birth", err)
	}
	reconcileUnpublished()
	assertCount(projectID, 8)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("created_at", savedUser.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	var restoredUser entity.User
	if err := db.Take(&restoredUser, "id = ?", savedUser.ID).Error; err != nil || !restoredUser.CreatedAt.Equal(savedUser.CreatedAt) {
		t.Fatal("exact recipient birth restoration failed", err)
	}
	// NULL and zero retain existing exhaustion behavior, never threshold warnings.
	write(userPath, map[string]any{})
	reconcile()
	assertCount(projectID, 8)
	write(userPath, map[string]any{"tokens_month": 0})
	reconcile()
	assertCount(projectID, 8)
	write(userPath, map[string]any{"tokens_month": 9, "money_month": "9", "currency": "USD"})
	reconcile()
	assertCount(projectID, 8)
	beforeDenied := dispatches.Load()
	deniedResponse := projectCall(projectID)
	expectStatus(t, deniedResponse, 429)
	if dispatches.Load() != beforeDenied {
		t.Fatal("warning observer changed >=100 quota enforcement")
	}
	flush()
	var deniedCall entity.CallRecord
	if requestID := deniedResponse.Header().Get("X-Request-ID"); requestID == "" {
		t.Fatal("quota denial omitted durable request identity")
	} else if err := db.Take(&deniedCall, "request_id = ?", requestID).Error; err != nil {
		t.Fatal("quota denial was not durably recorded", err)
	}
	if deniedCall.Status != "error" || deniedCall.ErrorCode != "quota_exceeded" || deniedCall.ProjectID != projectID || deniedCall.TeamMembershipID != "" || deniedCall.TeamID != "" || deniedCall.UserID != "" || deniedCall.KeyID != projectKeyIDs[projectID] {
		t.Fatal("quota denial changed exact scoped identity or classification")
	}
	var deniedAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", deniedCall.RequestID).Count(&deniedAttempts).Error; err != nil || deniedAttempts != 0 {
		t.Fatal("quota denial invented an upstream/native attempt", err)
	}
	merged := page()
	warningCount, exhausted := 0, 0
	for _, item := range merged.Items {
		if item.QuotaWarning != nil {
			warningCount++
		}
		if item.Quota != nil {
			exhausted++
		}
	}
	if warningCount != 8 || exhausted != 3 {
		t.Fatal("warning/exhaustion merge changed existing zero or >=100 semantics", warningCount, exhausted)
	}
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest("GET", "/api/v1/notifications?status=all", nil, ""), 200)
	for _, item := range adminPage.Items {
		if item.SubjectID == projectID {
			t.Fatal("Project warning/exhaustion fanned out to nonmember administrator")
		}
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?recipient_id="+outsider.User.ID, nil), 400)

	ownerLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-warning-owner@example.invalid","password":"monthly-notification-password"}`, nil, "")
	ownerAuth, ownerCookie := readIdentity(t, ownerLogin)
	ownerPage := decodeCatalogResponse[service.NotificationPage](t, request(ownerCookie, ownerAuth.CSRFToken, "GET", "/api/v1/notifications?status=all", nil, ""), 200)
	if len(ownerPage.Items) != 11 || ownerPage.UnreadCount != 11 {
		t.Fatal("original active owner did not receive complete bounded fanout")
	}
	lateLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"project-warning-later@example.invalid","password":"monthly-notification-password"}`, nil, "")
	lateAuth, lateCookie := readIdentity(t, lateLogin)
	if aliasStored {
		if err := db.Delete(&entity.ProjectManager{}, "id = ?", aliasProjectID).Error; err != nil {
			t.Fatal(err)
		}
	}
	create(&entity.ProjectManager{ID: "pmg_project_warning_later", ProjectID: projectID, UserID: later.User.ID})
	if err := db.Model(&entity.Project{}).Where("id = ?", projectID).UpdateColumn("name", "Renamed current Project").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	latePage := decodeCatalogResponse[service.NotificationPage](t, request(lateCookie, lateAuth.CSRFToken, "GET", "/api/v1/notifications?status=all", nil, ""), 200)
	if len(latePage.Items) != 0 || latePage.UnreadCount != 0 {
		t.Fatal("replay expanded original recipients")
	}
	expectStatus(t, request(lateCookie, lateAuth.CSRFToken, "POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	expectStatus(t, request(lateCookie, lateAuth.CSRFToken, "POST", "/api/v1/notifications/read-all", nil, ""), 204)
	expectStatus(t, request(ownerCookie, ownerAuth.CSRFToken, "POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)

	if err := db.Delete(&entity.ProjectManager{}, "id = ?", "pmg_project_warning_caller").Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	if len(page().Items) != 0 {
		t.Fatal("departed original recipient retained Project history")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	var inaccessibleReceipt entity.ProjectQuotaWarningInbox
	if err := db.Take(&inaccessibleReceipt, "id = ?", near.ID).Error; err != nil || !projectWarningInboxEqual(inaccessibleReceipt, readInbox) {
		t.Fatal("read-all changed inaccessible original receipt", err)
	}
	create(&entity.ProjectManager{ID: "pmg_project_warning_rejoin", ProjectID: projectID, UserID: member.User.ID})
	refresh()
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("rejoin changed original history/read state or name")
	}
	for _, item := range page().Items {
		if item.SubjectName != "Frozen aggregate Project" {
			t.Fatal("replay rewrote recorded Project name")
		}
	}
	for _, state := range []struct {
		model             any
		id, field         string
		invalid, original any
	}{
		{&entity.Project{}, projectID, "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Project{}, projectID, "status", entity.ResourceArchived, entity.ResourceActive},
		{&entity.ProjectManager{}, "pmg_project_warning_rejoin", "user_id", strings.ToUpper(member.User.ID), member.User.ID},
		{&entity.ProjectManager{}, "pmg_project_warning_rejoin", "project_id", strings.ToUpper(projectID), projectID},
	} {

		var membershipBefore entity.ProjectManager
		if state.field == "user_id" || state.field == "project_id" {
			if err := db.Take(&membershipBefore, "id = ?", state.id).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(state.model).Where("id = ?", state.id).UpdateColumn(state.field, state.invalid).Error; err != nil {
			if (state.field != "user_id" && state.field != "project_id") || !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal("membership negative failed for unrelated reason", err)
			}
			var unchanged entity.ProjectManager
			if err := db.Take(&unchanged, "id = ?", state.id).Error; err != nil || !reflect.DeepEqual(unchanged, membershipBefore) || !reflect.DeepEqual(page(), merged) {
				t.Fatal("FK-rejected alias changed canonical authority", err)
			}
			continue // Exact FK rejection preserves the original authorized relationship.
		}
		if len(page().Items) != 0 {
			t.Fatal("inactive or alias membership exposed historical Project warning")
		}
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
		if err := db.Model(state.model).Where("id = ?", state.id).UpdateColumn(state.field, state.original).Error; err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	// Exact corrupt history cannot borrow the current owner's rows, count or writes.
	aliasIDs := []string{}
	for _, kind := range []string{"owner", "recipient", "join", "birth", "recipient_birth"} {
		observation := original
		observation.ID = "pwo_warn_alias_" + kind
		observation.PolicyRevision = "lim_warning_alias_" + kind
		recipient, join := member.User.ID, observation.ID
		switch kind {
		case "owner":
			observation.ProjectID = strings.ToUpper(projectID)
			if observation.ProjectID == projectID {
				t.Fatal("owner case alias was a no-op")
			}
		case "recipient":
			recipient = strings.ToUpper(member.User.ID)
			if recipient == member.User.ID {
				t.Fatal("recipient case alias was a no-op")
			}
		case "join":
			join = strings.ToUpper(observation.ID)
			if join == observation.ID {
				t.Fatal("join case alias was a no-op")
			}
		case "birth":
			observation.ResourceCreatedAt = original.ResourceCreatedAt.Add(-time.Millisecond)
		}
		create(&observation)
		inboxID := "pwi_warn_alias_" + kind
		aliasIDs = append(aliasIDs, inboxID)
		receiptBirth := savedUser.CreatedAt
		if kind == "recipient_birth" {
			receiptBirth = receiptBirth.Add(-time.Millisecond)
		}
		create(&entity.ProjectQuotaWarningInbox{ID: inboxID, ObservationID: join, RecipientID: recipient, RecipientCreatedAt: receiptBirth, CreatedAt: observation.AsOf})
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+inboxID+"/read", nil), 404)
	}
	scoped := page()
	if len(scoped.Items) != len(merged.Items) || scoped.UnreadCount != merged.UnreadCount {
		t.Fatal("aliased or wrong-birth history polluted merged page/count")
	}

	// Cursor continuation keeps one global order across warning and exhaustion.
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 20; pages++ {
		path := "/api/v1/notifications?status=all&limit=2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		batch := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", path, nil), 200)
		for _, item := range batch.Items {
			if seen[item.ID] {
				t.Fatal("merged cursor duplicated a notification")
			}
			seen[item.ID] = true
		}
		if batch.NextCursor == "" {
			break
		}
		cursor = batch.NextCursor
	}
	if len(seen) != len(merged.Items) {
		t.Fatal("merged cursor omitted notification")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	if page().UnreadCount != 0 {
		t.Fatal("read-all did not cover warning and exhaustion")
	}
	var hiddenInboxes []entity.ProjectQuotaWarningInbox
	if err := db.Where("id IN ?", aliasIDs).Find(&hiddenInboxes).Error; err != nil || len(hiddenInboxes) != 5 {
		t.Fatal("private alias fixtures missing", err)
	}
	for _, inbox := range hiddenInboxes {
		if inbox.ReadAt != nil {
			t.Fatal("read-all altered hidden history")
		}
	}
	// Current disabled lifecycle denies the existing recipient Session and observation.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 401)
	reconcileUnpublished()
	assertCount(projectID, 12) //8 original+4 canonical-Project corrupt histories.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("OffboardedAt", time.Now().UTC()).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 401)
	reconcileUnpublished()
	assertCount(projectID, 12)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("OffboardedAt", nil).Error; err != nil {
		t.Fatal(err)
	}
	// Unknown usage and a live hold require separate real completed native calls.
	makeTarget := func(label string) (string, string) {
		t.Helper()
		target := "prj_project_warning_" + label
		create(&entity.Project{ID: target, Name: "Warning " + label, Status: entity.ResourceActive, CreatorID: admin.User.ID})
		create(&entity.ProjectModelGrant{ProjectID: target, ModelID: modelID})
		create(&entity.ProjectManager{ID: "pmg_project_warn_" + label, ProjectID: target, UserID: member.User.ID})
		create(&entity.ProjectManager{ID: "pmg_project_owner_" + label, ProjectID: target, UserID: owner.User.ID})
		createProjectKey(target)
		refresh()
		return target, "/api/v1/projects/" + target + "/limits"
	}
	unknownID, unknownPath := makeTarget("unknown")
	write(unknownPath, map[string]any{"tokens_month": 2})
	unknown.Store(true)
	expectStatus(t, projectCall(unknownID), 200)
	unknown.Store(false)
	flush()
	write(unknownPath, map[string]any{"tokens_month": 1, "money_month": "0.000000000000000001", "currency": "USD"})
	reconcile()
	assertCount(unknownID, 0)
	unknownUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", unknownPath, nil, ""), 200).QuotaUsage
	// Admission constrained only tokens: missing usage retains that terminal
	// token bound. A later money policy cannot invent a historical money bound;
	// the unknown charge remains unknown and cannot form a settled warning.
	if unknownUsage == nil || unknownUsage.Month == nil || !unknownUsage.Month.Covered || unknownUsage.Month.TokensUsed != 0 || unknownUsage.Month.TokensHeld != 2 || unknownUsage.Month.TokensUnknown != 0 || len(unknownUsage.Month.MoneyUsed) != 0 || len(unknownUsage.Month.MoneyHeld) != 0 || unknownUsage.Month.MoneyUnknown != 1 || unknownUsage.Active == nil || unknownUsage.Active.TokensHeld != 0 || len(unknownUsage.Active.MoneyHeld) != 0 || unknownUsage.Active.TokensUnknown != 0 || unknownUsage.Active.MoneyUnknown != 0 {
		t.Fatal("missing native usage lost its admission-time token bound or invented a later monetary bound")
	}
	heldID, heldPath := makeTarget("held")
	write(heldPath, map[string]any{"tokens_month": 2})
	hold.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	nativeRequests.Add(1)
	go func() {
		defer nativeRequests.Done()
		heldResponse <- projectCall(heldID)
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("held call did not dispatch")
	}
	reconcile()
	assertCount(heldID, 0)
	heldUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", heldPath, nil, ""), 200).QuotaUsage
	if heldUsage == nil || heldUsage.Month == nil || heldUsage.Month.TokensUsed != 0 || heldUsage.Month.TokensHeld != 0 || heldUsage.Active == nil || heldUsage.Active.TokensHeld != 2 {
		t.Fatal("live reservation did not remain separate from settled tokens")
	}
	releaseOnce.Do(func() { close(release) })
	hold.Store(false)
	expectStatus(t, <-heldResponse, 200)
	flush()
	reconcile()
	assertCount(heldID, 0)
	var finalOriginal entity.ProjectQuotaWarningObservation
	if err := db.Take(&finalOriginal, "id = ?", original.ID).Error; err != nil || !projectWarningObservationEqual(finalOriginal, original) {
		t.Fatal("policy/level/replay changed original observation", err)
	}
	var allCalls []entity.CallRecord
	if err := db.Where("project_id IN ? OR key_id IN ?", []string{projectID, unknownID, heldID}, []string{"key_project_warning_member", "key_project_warning_admin"}).Order("request_id").Find(&allCalls).Error; err != nil {
		t.Fatal(err)
	}
	native := make([]entity.CallRecord, 0, 9)
	denials := 0
	for _, fact := range allCalls {
		if fact.RequestID == deniedCall.RequestID {
			if !reflect.DeepEqual(fact, deniedCall) {
				t.Fatal("later warning activity changed the immutable quota denial")
			}
			denials++
		} else {
			native = append(native, fact)
		}
	}
	if dispatches.Load() != 9 || len(allCalls) != 10 || len(native) != 9 || denials != 1 {
		t.Fatal("unexpected native dispatch, durable call or denial count", dispatches.Load(), len(allCalls), len(native), denials)
	}
	attemptFacts := map[string]entity.CallAttempt{}
	for _, fact := range native {
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", fact.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if fact.TeamID != "" || fact.TeamMembershipID != "" || fact.Status != "success" || len(attempts) != 1 || attempts[0].CredentialID != "crd_project_warning" || attempts[0].SnapshotID == "" || attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("native attribution/completion changed")
		}
		if fact.ProjectID != "" {
			if fact.UserID != "" || projectKeyIDs[fact.ProjectID] == "" || fact.KeyID != projectKeyIDs[fact.ProjectID] {
				t.Fatal("Project Key borrowed creator/manager identity")
			}
		} else if fact.KeyID != "key_project_warning_member" && fact.KeyID != "key_project_warning_admin" {
			t.Fatal("Personal calibration borrowed Project identity")
		}
		attemptFacts[attempts[0].ID] = attempts[0]
	}
	for _, model := range []any{&entity.QuotaWarningObservation{}, &entity.TeamQuotaWarningObservation{}} {
		var unrelated int64
		if err := db.Model(model).Count(&unrelated).Error; err != nil || unrelated != 0 {
			t.Fatal("Project warnings polluted Personal/Team warning scope", err)
		}
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("Project warnings created operational/SMTP fanout", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := svc.ReconcileMonthlyQuotaNotifications(canceled); err == nil {
		t.Fatal("canceled reconciliation ignored cancellation")
	}
	assertCount(projectID, 12)
	// Restart reuses original Session, SQL and journal; no inference is replayed.
	beforeRestart := page()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	svc = makeService()
	router = fox.New()
	New(svc).RegisterRoutes(router)
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	flush()
	refresh()
	reconcile()
	afterRestart := page()
	var afterCalls []entity.CallRecord
	if err := db.Where("project_id IN ? OR key_id IN ?", []string{projectID, unknownID, heldID}, []string{"key_project_warning_member", "key_project_warning_admin"}).Order("request_id").Find(&afterCalls).Error; err != nil || !reflect.DeepEqual(afterCalls, allCalls) {
		t.Fatal("restart changed immutable call facts", err)
	}
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", deniedCall.RequestID).Count(&deniedAttempts).Error; err != nil || deniedAttempts != 0 {
		t.Fatal("restart invented a native attempt for quota denial", err)
	}
	for id, before := range attemptFacts {
		var after entity.CallAttempt
		if err := db.Take(&after, "id = ?", id).Error; err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("restart changed immutable attempt facts", err)
		}
	}
	if !reflect.DeepEqual(afterRestart, beforeRestart) || dispatches.Load() != 9 {
		t.Fatal("restart altered recipient history/read state or replayed inference")
	}
}
