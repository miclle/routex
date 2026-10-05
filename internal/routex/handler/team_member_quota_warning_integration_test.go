package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// testTeamMemberMonthlyQuotaWarningLifecycle runs against each supported database in
// the fresh-database harness. Warmup establishes real journal coverage before
// any notified resource is created; no historical accounting is manufactured.
func testTeamMemberMonthlyQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
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
	cipher, err := store.Seal("crd_tmw", "tmw-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_tmw"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_tmw", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_tmw", ProviderID: "prv_tmw", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_tmw", ConnectionID: "con_tmw", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_tmw", ConnectionID: "con_tmw", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_tmw", ProviderModelID: "pmd_tmw"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "tmw-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_tmw", ModelID: modelID, ProviderModelID: "pmd_tmw", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_tmw_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_tmw_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_tmw", ProviderModelID: "pmd_tmw", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_tmw_%d", index), ModelPriceID: "price_tmw", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
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
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"tmw-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_tmw/reservation-bound"
	expectStatus(t, adminRequest("PUT", boundPath, map[string]any{"max_input_tokens": 1, "max_output_tokens": 1, "evidence": "Controlled native response capacity", "reason": "Team warning acceptance"}, "0"), 200)
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// First use changes accounting activation; publish that real generation before
	// creating later resources or asking the observer for current-policy evidence.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "tmw-member@example.invalid", "monthly-notification-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "tmw-outsider@example.invalid", "monthly-notification-password", "Other member", "member")
	if err != nil {
		t.Fatal(err)
	}

	owner, err := svc.CreateMember(ctx, admin.User.ID, "tmw-owner@example.invalid", "monthly-notification-password", "Team owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	later, err := svc.CreateMember(ctx, admin.User.ID, "tmw-later@example.invalid", "monthly-notification-password", "Later member", "member")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range []*service.MemberRecord{member, owner, later, outsider} {
		if err := db.Take(&record.User, "id = ?", record.User.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	teamID := "tem_tmw"
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"tmw-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
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
	create(&entity.APIKey{ID: "key_tmw_member", UserID: member.User.ID, Name: "Personal warning", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_tmw_member", ModelID: modelID})
	create(&entity.Team{ID: teamID, Name: "Frozen member Team", Status: entity.ResourceActive})
	create(&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	create(&entity.TeamMembership{ID: "tmb_tmw_caller", TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	create(&entity.TeamMembership{ID: "tmb_tmw_owner", TeamID: teamID, UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
	pending, err := svc.CreateMember(ctx, admin.User.ID, "tmw-pending@example.invalid", "monthly-notification-password", "Pending Team member", "member")
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
	create(&entity.TeamMembership{ID: "tmb_tmw_pending", TeamID: teamID, UserID: pending.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	teamCall := func(target string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/api/v1/teams/"+target+"/chat/completions", strings.NewReader(`{"model":"tmw-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", memberAuth.CSRFToken)
		req.AddCookie(memberCookie)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	refresh := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
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
	parentPath := "/api/v1/teams/" + teamID + "/limits"
	memberPath := func(team string) string { return "/api/v1/teams/" + team + "/members/" + member.User.ID + "/limits" }
	write := func(path string, values map[string]any) entity.ResourceLimit {
		t.Helper()
		current := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200)
		body := teamMemberWarningFixturePolicy(path, values)
		result := decodeCatalogResponse[service.LimitRecord](t, adminRequest("PUT", path, body, current.ETag), 200)
		if !result.Enforced {
			t.Fatal("reviewed policy not applied")
		}
		kind, id := "team", strings.Split(path, "/")[4]
		if strings.Contains(path, "/members/") {
			kind, id = "team_member", teamMemberWarningFixtureScope(id, member.User.ID)
		}
		var row entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", kind, id).Take(&row).Error; err != nil || row.ScopeKind != kind || row.ScopeID != id {
			t.Fatal("persisted policy identity mismatch", err)
		}
		return row
	}
	reconcileUnpublished := func() {
		t.Helper()
		if err := svc.ReconcileMonthlyQuotaNotifications(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() { t.Helper(); refresh(); reconcileUnpublished() }
	page := func() service.NotificationPage {
		t.Helper()
		return decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	}
	scope := teamMemberWarningFixtureScope(teamID, member.User.ID)
	count := func(scope string) int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.TeamMemberQuotaWarningObservation{}).Where(database.ExactText(db, clause.Column{Name: "scope_id"}, scope)).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	assertCount := func(scope string, n int64) {
		t.Helper()
		if got := count(scope); got != n {
			t.Fatalf("warning count %d want %d", got, n)
		}
	}
	// Covered Personal settlement does not create Team-member quota usage.
	expectStatus(t, call(personalBearer), 200)
	flush()
	refresh()
	write(parentPath, map[string]any{"tokens_month": 100})
	path := memberPath(teamID)
	policy := write(path, map[string]any{"tokens_month": 10})
	reconcile()
	assertCount(scope, 0)
	for range 3 {
		expectStatus(t, teamCall(teamID), 200)
		flush()
		reconcile()
		assertCount(scope, 0)
	}
	expectStatus(t, teamCall(teamID), 200)
	flush()
	assertCount(scope, 0)
	reconcile()
	assertCount(scope, 1)
	nearPage := page()
	if len(nearPage.Items) != 1 || nearPage.UnreadCount != 1 {
		t.Fatal("sole-self near warning missing")
	}
	near := nearPage.Items[0]
	q := near.QuotaWarning
	if q == nil || q.ScopeKind != "team_member" || q.ScopeID != scope || q.ThresholdGeneration != "team-member-monthly-80-90-v1" || q.Level != "near" || q.Threshold != 80 || q.Settled != "8" || q.Limit != "10" || q.Currency != nil || q.PolicyRevision != policy.ETag || near.SubjectType != "team_member" || near.SubjectID != scope || near.SubjectName != "Frozen member Team" || near.Severity != "medium" || near.Quota != nil {
		t.Fatal("exact near snapshot mismatch")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("read state missing")
	}
	var original entity.TeamMemberQuotaWarningObservation
	var receipt entity.TeamMemberQuotaWarningInbox
	if err := db.Take(&original, "id = ?", near.QuotaWarningObservationID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Take(&receipt, "id = ?", near.ID).Error; err != nil {
		t.Fatal(err)
	}
	var receipts []entity.TeamMemberQuotaWarningInbox
	if err := db.Where("observation_id = ?", original.ID).Find(&receipts).Error; err != nil || len(receipts) != 1 || receipts[0].RecipientID != member.User.ID || !receipts[0].RecipientCreatedAt.Equal(member.User.CreatedAt) || original.TeamID != teamID || original.MemberUserID != member.User.ID || !original.UserCreatedAt.Equal(member.User.CreatedAt) {
		t.Fatal("private sole-self identity/birth proof missing", err)
	}
	single.Store(true)
	expectStatus(t, teamCall(teamID), 200)
	single.Store(false)
	flush()
	reconcile()
	reconcile()
	assertCount(scope, 2)
	criticalPage := page()
	if len(criticalPage.Items) != 2 || criticalPage.UnreadCount != 1 {
		t.Fatal("escalation changed near read")
	}
	for _, item := range criticalPage.Items {
		if item.ID == near.ID {
			if !item.Read || item.ReadAt == nil || !item.ReadAt.Equal(*receipt.ReadAt) || !reflect.DeepEqual(item.QuotaWarning, near.QuotaWarning) {
				t.Fatal("near immutable facts changed")
			}
		} else if item.QuotaWarning == nil || item.QuotaWarning.Level != "critical" || item.QuotaWarning.Settled != "9" || item.QuotaWarning.Threshold != 90 || item.Severity != "high" {
			t.Fatal("critical exact snapshot missing")
		}
	}
	usage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200).QuotaUsage
	if usage == nil || usage.Month == nil || !usage.Month.Covered || usage.Month.TokensUsed != 9 || usage.Month.MoneyUsed["USD"] != "9" || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUnknown != 0 {
		t.Fatal("settled stable pair usage mismatch")
	}
	personal, err := svc.GetResourceLimit(ctx, member.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 2 {
		t.Fatal("Team native charged Personal", err)
	}
	// Parent quota is inherited admission, not a child warning definition.
	parentBefore := write(parentPath, map[string]any{"tokens_month": 100, "rpm": 200})
	reconcile()
	assertCount(scope, 2)
	write(path, map[string]any{"tokens_month": 10, "rpm": 100})
	reconcile()
	assertCount(scope, 3)
	moneyNear := write(path, map[string]any{"money_month": "11.25"})
	reconcile()
	assertCount(scope, 4)
	moneyCritical := write(path, map[string]any{"money_month": "10"})
	reconcile()
	assertCount(scope, 5)
	precise := write(path, map[string]any{"money_month": "10.000000000000000001"})
	reconcile()
	assertCount(scope, 6)
	found := map[string]bool{}
	for _, item := range page().Items {
		q := item.QuotaWarning
		if q == nil || q.Dimension != "money" {
			continue
		}
		if q.Currency == nil || *q.Currency != "USD" || q.Settled != "9" {
			t.Fatal("exact money settlement changed")
		}
		switch q.PolicyRevision {
		case moneyNear.ETag:
			if q.Level != "near" || q.Limit != "11.25" {
				t.Fatal("money80")
			}
			found["near"] = true
		case moneyCritical.ETag:
			if q.Level != "critical" || q.Limit != "10" {
				t.Fatal("money90")
			}
			found["critical"] = true
		case precise.ETag:
			if q.Level != "near" || q.Limit != "10.000000000000000001" {
				t.Fatal("18-place money rounded")
			}
			found["precise"] = true
		}
	}
	if len(found) != 3 {
		t.Fatal("independent money levels missing")
	}
	write(path, map[string]any{"tokens_month": 10, "rpm": 101})

	var childBefore entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", scope).Take(&childBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", scope).UpdateColumn("ETag", "unpublished-child").Error; err != nil {
		t.Fatal(err)
	}
	var childChanged entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", scope).Take(&childChanged).Error; err != nil || childChanged.ETag != "unpublished-child" {
		t.Fatal("child ETag negative did not persist", err)
	}
	reconcileUnpublished()
	assertCount(scope, 6)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", scope).UpdateColumn("ETag", childBefore.ETag).Error; err != nil {
		t.Fatal(err)
	}
	var childRestored entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", scope).Take(&childRestored).Error; err != nil || !reflect.DeepEqual(childRestored, childBefore) {
		t.Fatal("child restoration differs", err)
	}
	var calendarBefore entity.QuotaSetting
	if err := db.Take(&calendarBefore, "id = ?", 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).UpdateColumn("ETag", "unpublished-calendar").Error; err != nil {
		t.Fatal(err)
	}
	var calendarChanged entity.QuotaSetting
	if err := db.Take(&calendarChanged, "id = ?", 1).Error; err != nil || calendarChanged.ETag != "unpublished-calendar" {
		t.Fatal("calendar ETag negative did not persist", err)
	}
	reconcileUnpublished()
	assertCount(scope, 6)
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).UpdateColumn("ETag", calendarBefore.ETag).Error; err != nil {
		t.Fatal(err)
	}
	var calendarRestored entity.QuotaSetting
	if err := db.Take(&calendarRestored, "id = ?", 1).Error; err != nil || !reflect.DeepEqual(calendarRestored, calendarBefore) {
		t.Fatal("calendar restoration differs", err)
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", parentBefore.ScopeKind, parentBefore.ScopeID).UpdateColumn("ETag", "unpublished-parent").Error; err != nil {
		t.Fatal(err)
	}
	var parentChanged entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", parentBefore.ScopeKind, parentBefore.ScopeID).Take(&parentChanged).Error; err != nil || parentChanged.ETag != "unpublished-parent" {
		t.Fatal("GORM ETag mapping", err)
	}
	reconcileUnpublished()
	assertCount(scope, 6)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", parentBefore.ScopeKind, parentBefore.ScopeID).UpdateColumn("ETag", parentBefore.ETag).Error; err != nil {
		t.Fatal(err)
	}
	var restoredParent entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", parentBefore.ScopeKind, parentBefore.ScopeID).Take(&restoredParent).Error; err != nil || !reflect.DeepEqual(restoredParent, parentBefore) {
		t.Fatal("parent corruption did not restore exact stored row", err)
	}
	reconcile()
	assertCount(scope, 7)
	// Platform currency and accounting generation must match the published proof.
	write(path, map[string]any{"money_month": "11", "currency": "USD"})
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	reconcileUnpublished()
	assertCount(scope, 7)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	assertCount(scope, 8)
	// Positive reached limit remains exhaustion; warning observations stop below100.
	write(path, map[string]any{"tokens_month": 9})
	reconcile()
	assertCount(scope, 8)
	merged := page()
	warnings, exhausted := 0, 0
	for _, item := range merged.Items {
		if item.QuotaWarning != nil {
			warnings++
		} else if item.Kind == "monthly_quota_exhausted" {
			exhausted++
		}
	}
	if warnings != 8 || exhausted != 1 {
		t.Fatal("warning/exhaustion merged oracle", warnings, exhausted)
	}
	denied := teamCall(teamID)
	expectStatus(t, denied, 429)
	flush()
	if dispatches.Load() != 7 {
		t.Fatal("hardstop dispatched native call")
	}
	var deniedCall entity.CallRecord
	if requestID := denied.Header().Get("X-Request-ID"); requestID == "" {
		t.Fatal("denial omitted request identity")
	} else if err := db.Take(&deniedCall, "request_id = ?", requestID).Error; err != nil {
		t.Fatal("durable denial absent", err)
	}
	if deniedCall.Status != "error" || deniedCall.ErrorCode != "quota_exceeded" || deniedCall.TeamID != teamID || deniedCall.TeamMembershipID != "tmb_tmw_caller" || deniedCall.UserID != member.User.ID || deniedCall.KeyID != "" || deniedCall.ProjectID != "" {
		t.Fatal("quota denial identity/classification changed")
	}
	var denialAttempts int64
	if err := db.Model(&entity.CallAttempt{}).Where("request_id = ?", deniedCall.RequestID).Count(&denialAttempts).Error; err != nil || denialAttempts != 0 {
		t.Fatal("denial created attempt", err)
	}
	// Owners, administrators and later members never inherit this private self row.
	for _, person := range []*service.MemberRecord{owner, later, outsider} {
		loginBody := map[string]any{"email": person.User.Email, "password": "monthly-notification-password"}
		raw, _ := json.Marshal(loginBody)
		login := identityRequest(router, "POST", "/api/v1/auth/login", string(raw), nil, "")
		auth, cookie := readIdentity(t, login)
		if person == later {
			create(&entity.TeamMembership{ID: "tmb_tmw_late", TeamID: teamID, UserID: later.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
			refresh()
		}
		inbox := decodeCatalogResponse[service.NotificationPage](t, request(cookie, auth.CSRFToken, "GET", "/api/v1/notifications?status=all", nil, ""), 200)
		for _, item := range inbox.Items {
			if item.QuotaWarning != nil && item.QuotaWarning.ScopeKind == "team_member" && item.SubjectID == scope {
				t.Fatal("private member warning exposed to peer")
			}
		}
		expectStatus(t, request(cookie, auth.CSRFToken, "POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
		expectStatus(t, request(cookie, auth.CSRFToken, "POST", "/api/v1/notifications/read-all", nil, ""), 204)
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest("GET", "/api/v1/notifications?status=all", nil, ""), 200)
	for _, item := range adminPage.Items {
		if item.SubjectID == scope {
			t.Fatal("administrator private override")
		}
	}
	if err := db.Delete(&entity.TeamMembership{}, "id = ?", "tmb_tmw_caller").Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	if len(page().Items) != 0 {
		t.Fatal("removed member retained access")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	var hidden entity.TeamMemberQuotaWarningInbox
	if err := db.Take(&hidden, "id = ?", receipt.ID).Error; err != nil || !teamMemberWarningInboxEqual(hidden, receipt) {
		t.Fatal("inaccessible read-all changed history", err)
	}
	create(&entity.TeamMembership{ID: "tmb_tmw_rejoin", TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	refresh()
	reconcileUnpublished()
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("rejoin reset stable pair history/read")
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("name", "Renamed current Team").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("replay overwrote recorded name")
	}

	// Current inactive Team/membership hides all current Team history without changing rows.
	for _, state := range []struct {
		model     any
		id, field string
		bad, good any
	}{
		{&entity.TeamMembership{}, "tmb_tmw_rejoin", "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceArchived, entity.ResourceActive},
		{&entity.TeamMembership{}, "tmb_tmw_rejoin", "user_id", strings.ToUpper(member.User.ID), member.User.ID},
		{&entity.TeamMembership{}, "tmb_tmw_rejoin", "team_id", strings.ToUpper(teamID), teamID},
	} {
		var beforeMembership entity.TeamMembership
		if state.field == "user_id" || state.field == "team_id" {
			if err := db.Take(&beforeMembership, "id = ?", state.id).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(state.model).Where("id = ?", state.id).UpdateColumn(state.field, state.bad).Error; err != nil {
			if (state.field != "user_id" && state.field != "team_id") || !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal(err)
			}
			var unchanged entity.TeamMembership
			if err := db.Take(&unchanged, "id = ?", state.id).Error; err != nil || !reflect.DeepEqual(unchanged, beforeMembership) || !reflect.DeepEqual(page(), merged) {
				t.Fatal("FK rejection changed authority", err)
			}
			continue
		}
		refresh()
		if len(page().Items) != 0 {
			t.Fatal("inactive/alias current membership leaked private history")
		}
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
		if err := db.Model(state.model).Where("id = ?", state.id).UpdateColumn(state.field, state.good).Error; err != nil {
			t.Fatal(err)
		}
		refresh()
		if !reflect.DeepEqual(page(), merged) {
			t.Fatal("restored current authority changed history")
		}
	}
	// Reused Team/User IDs with different persisted births cannot inherit self warnings.
	for _, model := range []struct {
		row any
		id  string
	}{{&entity.Team{}, teamID}, {&entity.User{}, member.User.ID}} {
		var oldRow struct{ CreatedAt time.Time }
		if err := db.Model(model.row).Select("created_at").Where("id = ?", model.id).Take(&oldRow).Error; err != nil {
			t.Fatal(err)
		}
		old := oldRow.CreatedAt
		changed := old.Add(time.Millisecond)
		if err := db.Model(model.row).Where("id = ?", model.id).UpdateColumn("created_at", changed).Error; err != nil {
			t.Fatal(err)
		}
		var stored struct{ CreatedAt time.Time }
		if err := db.Model(model.row).Select("created_at").Where("id = ?", model.id).Take(&stored).Error; err != nil || !stored.CreatedAt.Equal(changed) || stored.CreatedAt.Equal(old) {
			t.Fatal("birth corruption did not persist", err)
		}
		fresh := page()
		for _, item := range fresh.Items {
			if item.QuotaWarning != nil && item.QuotaWarning.ScopeKind == "team_member" {
				t.Fatal("new incarnation inherited history")
			}
		}
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
		var kept entity.TeamMemberQuotaWarningInbox
		if err := db.Take(&kept, "id = ?", receipt.ID).Error; err != nil || !teamMemberWarningInboxEqual(kept, receipt) {
			t.Fatal("birth change altered old receipt", err)
		}
		if err := db.Model(model.row).Where("id = ?", model.id).UpdateColumn("created_at", old).Error; err != nil {
			t.Fatal(err)
		}
	}
	refresh()
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("birth restoration changed saved history")
	}
	// A disabled admitted actor's existing Session cannot read or create observations.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 401)
	reconcileUnpublished()
	assertCount(scope, 8)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	refresh()

	var privateRecipients []string
	if err := db.Model(&entity.TeamMemberQuotaWarningInbox{}).Distinct("recipient_id").Pluck("recipient_id", &privateRecipients).Error; err != nil || len(privateRecipients) != 1 || privateRecipients[0] != member.User.ID {
		t.Fatal("private warning fanout expanded beyond sole-self", err)
	}
	// Private stored birth/alias corruptions cannot borrow a current identity.
	badIDs := []string{}
	for index, kind := range []string{"team_alias", "user_alias", "team_birth", "user_birth", "recipient_birth", "recipient_alias", "join_alias", "scope"} {
		bad := original
		bad.ID = fmt.Sprintf("mwo_bad_%d", index)
		bad.PolicyRevision = fmt.Sprintf("lim_bad_%d", index)
		recipient, join, birth := member.User.ID, bad.ID, member.User.CreatedAt
		switch kind {
		case "team_alias":
			bad.TeamID = strings.ToUpper(teamID)
		case "user_alias":
			bad.MemberUserID = strings.ToUpper(member.User.ID)
		case "team_birth":
			bad.ResourceCreatedAt = bad.ResourceCreatedAt.Add(-time.Millisecond)
		case "user_birth":
			bad.UserCreatedAt = bad.UserCreatedAt.Add(-time.Millisecond)
		case "recipient_birth":
			birth = birth.Add(-time.Millisecond)
		case "recipient_alias":
			recipient = strings.ToUpper(recipient)
		case "join_alias":
			join = strings.ToUpper(join)
		case "scope":
			bad.ScopeID = strings.Repeat("A", 52)
		}
		create(&bad)
		id := fmt.Sprintf("mwi_bad_%d", index)
		badIDs = append(badIDs, id)
		create(&entity.TeamMemberQuotaWarningInbox{ID: id, ObservationID: join, RecipientID: recipient, RecipientCreatedAt: birth, CreatedAt: bad.AsOf})
		expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+id+"/read", nil), 404)
	}
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("alias/birth polluted page/unread")
	}
	for _, item := range page().Items {
		raw, _ := json.Marshal(item)
		for _, private := range []string{"team_id", "member_user_id", "user_created_at", "recipient_created_at", "resource_created_at", "coverage_start"} {
			if item.QuotaWarning != nil && strings.Contains(string(raw), `"`+private+`"`) {
				t.Fatal("private tuple/proof leaked", private)
			}
		}
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	if page().UnreadCount != 0 {
		t.Fatal("read-all omitted merged warning/exhaustion")
	}
	var badInboxes []entity.TeamMemberQuotaWarningInbox
	if err := db.Where("id IN ?", badIDs).Find(&badInboxes).Error; err != nil || len(badInboxes) != 8 {
		t.Fatal(err)
	}
	for _, row := range badInboxes {
		if row.ReadAt != nil {
			t.Fatal("read-all touched private hidden rows")
		}
	}
	// Active holds and terminal unknown usage are separate from known settled facts.
	makeTarget := func(label string) (string, string) {
		t.Helper()
		target := "tem_tmw_" + label
		create(&entity.Team{ID: target, Name: "Member " + label, Status: entity.ResourceActive})
		create(&entity.TeamModelGrant{TeamID: target, ModelID: modelID})
		create(&entity.TeamMembership{ID: "tmb_tmw_" + label, TeamID: target, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
		refresh()
		return target, memberPath(target)
	}
	unknownID, unknownPath := makeTarget("unknown")
	write(unknownPath, map[string]any{"tokens_month": 2})
	unknown.Store(true)
	expectStatus(t, teamCall(unknownID), 200)
	unknown.Store(false)
	flush()
	write(unknownPath, map[string]any{"tokens_month": 1, "money_month": "0.000000000000000001"})
	reconcile()
	assertCount(teamMemberWarningFixtureScope(unknownID, member.User.ID), 0)
	unknownUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", unknownPath, nil, ""), 200).QuotaUsage
	if unknownUsage == nil || unknownUsage.Month == nil || !unknownUsage.Month.Covered || unknownUsage.Month.TokensUsed != 0 || unknownUsage.Month.TokensHeld != 2 || unknownUsage.Month.TokensUnknown != 0 || len(unknownUsage.Month.MoneyUsed) != 0 || len(unknownUsage.Month.MoneyHeld) != 0 || unknownUsage.Month.MoneyUnknown != 1 {
		t.Fatal("unknown monetary bound fabricated settled usage")
	}
	heldID, heldPath := makeTarget("held")
	write(heldPath, map[string]any{"tokens_month": 2})
	hold.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	nativeRequests.Add(1)
	go func() { defer nativeRequests.Done(); heldResponse <- teamCall(heldID) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("held dispatch timeout")
	}
	reconcile()
	assertCount(teamMemberWarningFixtureScope(heldID, member.User.ID), 0)
	heldUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", heldPath, nil, ""), 200).QuotaUsage
	if heldUsage == nil || heldUsage.Month == nil || heldUsage.Month.TokensUsed != 0 || heldUsage.Month.TokensHeld != 0 || heldUsage.Active == nil || heldUsage.Active.TokensHeld != 2 {
		t.Fatal("live hold counted settled")
	}
	releaseOnce.Do(func() { close(release) })
	hold.Store(false)
	select {
	case response := <-heldResponse:
		expectStatus(t, response, 200)
	case <-time.After(15 * time.Second):
		t.Fatal("held request did not finish")
	}
	flush()
	reconcile()
	assertCount(teamMemberWarningFixtureScope(heldID, member.User.ID), 0)
	var originalAfter entity.TeamMemberQuotaWarningObservation
	if err := db.Take(&originalAfter, "id = ?", original.ID).Error; err != nil || !teamMemberWarningObservationEqual(originalAfter, original) {
		t.Fatal("original immutable observation changed", err)
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id IN ? OR key_id IN ?", []string{teamID, unknownID, heldID}, []string{"key_tmw_admin", "key_tmw_member"}).Order("request_id").Find(&calls).Error; err != nil || len(calls) != 10 || dispatches.Load() != 9 {
		t.Fatal("native/durable counts", err, len(calls), dispatches.Load())
	}
	attemptsBefore := map[string]entity.CallAttempt{}
	nativeCount := 0
	denialCount := 0
	for _, fact := range calls {
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", fact.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if fact.RequestID == deniedCall.RequestID {
			denialCount++
			if len(attempts) != 0 || !reflect.DeepEqual(fact, deniedCall) {
				t.Fatal("denial changed")
			}
			continue
		}
		nativeCount++
		if fact.Status != "success" || fact.ProjectID != "" || len(attempts) != 1 || attempts[0].NativeCompletionEvidence != "completed" || attempts[0].CredentialID != "crd_tmw" || attempts[0].SnapshotID == "" {
			t.Fatal("native completion/attribution missing")
		}
		if fact.TeamID != "" {
			membership := "tmb_tmw_caller"
			if fact.TeamID == unknownID {
				membership = "tmb_tmw_unknown"
			}
			if fact.TeamID == heldID {
				membership = "tmb_tmw_held"
			}
			if fact.UserID != member.User.ID || fact.KeyID != "" || fact.TeamMembershipID != membership {
				t.Fatal("Team Session native attribution changed")
			}
		} else if fact.TeamMembershipID != "" || fact.KeyID != "key_tmw_admin" && fact.KeyID != "key_tmw_member" {
			t.Fatal("Personal isolation changed")
		}
		attemptsBefore[attempts[0].ID] = attempts[0]
	}
	if nativeCount != 9 || denialCount != 1 {
		t.Fatal("exact native9/denial1")
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var n int64
		if err := db.Model(model).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("unexpected operational/SMTP fanout", err)
		}
	}
	beforeRestart := page()
	var inboxesBefore []entity.TeamMemberQuotaWarningInbox
	if err := db.Order("id").Find(&inboxesBefore).Error; err != nil {
		t.Fatal(err)
	}
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
	reconcile()
	if !reflect.DeepEqual(page(), beforeRestart) || dispatches.Load() != 9 {
		t.Fatal("same Session restart changed history/replayed native")
	}
	var callsAfter []entity.CallRecord
	if err := db.Where("team_id IN ? OR key_id IN ?", []string{teamID, unknownID, heldID}, []string{"key_tmw_admin", "key_tmw_member"}).Order("request_id").Find(&callsAfter).Error; err != nil || !reflect.DeepEqual(callsAfter, calls) {
		t.Fatal("restart changed immutable calls", err)
	}
	for id, before := range attemptsBefore {
		var after entity.CallAttempt
		if err := db.Take(&after, "id = ?", id).Error; err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("restart changed immutable attempt", err)
		}
	}
	var inboxesAfter []entity.TeamMemberQuotaWarningInbox
	if err := db.Order("id").Find(&inboxesAfter).Error; err != nil || !reflect.DeepEqual(inboxesAfter, inboxesBefore) {
		t.Fatal("restart changed original recipients/readstate", err)
	}
}

func teamMemberWarningFixtureScope(teamID, userID string) string {
	raw, _ := json.Marshal([2]string{teamID, userID})
	sum := sha256.Sum256(raw)
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
}

func TestTeamMemberWarningFixtureStablePairAndNative(t *testing.T) {
	first := teamMemberWarningFixtureScope("tem_exact", "usr_exact")
	if len(first) != 52 || first[len(first)-1] != 'A' && first[len(first)-1] != 'Q' {
		t.Fatal("noncanonical complete Base32 digest")
	}
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(first)
	if err != nil || len(decoded) != sha256.Size {
		t.Fatal("incomplete pair digest", err)
	}
	for _, pair := range [][2]string{{"TEM_EXACT", "usr_exact"}, {"tem_exact", "USR_EXACT"}, {"tem_exact", "usr_other"}, {"tem_other", "usr_exact"}} {
		if teamMemberWarningFixtureScope(pair[0], pair[1]) == first {
			t.Fatal("pair/case identity collapsed")
		}
	}
	for _, caseValue := range []struct {
		single, unknown bool
		want            int
	}{{false, false, 1}, {true, false, 0}, {false, true, 0}} {
		var body struct {
			Choices []struct {
				Index  *int   `json:"index"`
				Finish string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Input  int `json:"prompt_tokens"`
				Output int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(quotaWarningNativeBody(caseValue.single, caseValue.unknown)), &body); err != nil || len(body.Choices) != 1 || body.Choices[0].Index == nil || *body.Choices[0].Index != 0 || body.Choices[0].Finish != "stop" {
			t.Fatal("native fixture lacks completion shape", err)
		}
		if caseValue.unknown {
			if body.Usage != nil {
				t.Fatal("unknown fixture manufactured terminal usage")
			}
		} else if body.Usage == nil || body.Usage.Input != 1 || body.Usage.Output != caseValue.want {
			t.Fatal("known fixture calibration changed")
		}
	}
}

// Fixture writes follow the presence-aware Team API, including member-only fields.
func teamMemberWarningFixturePolicy(path string, values map[string]any) map[string]any {
	body := map[string]any{"tokens_month": nil, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "reason": "Team member warning acceptance"}
	if !strings.Contains(path, "/members/") {
		body["tokens_5h"], body["tokens_7d"] = nil, nil
	}
	for field, value := range values {
		body[field] = value
	}
	if body["money_month"] != nil {
		if _, present := body["currency"]; !present {
			body["currency"] = "USD"
		}
	} else {
		delete(body, "currency")
	}
	return body
}

func TestTeamMemberWarningFixturePolicyShape(t *testing.T) {
	for _, path := range []string{"/api/v1/teams/tea_test/limits", "/api/v1/teams/tea_test/members/usr_test/limits"} {
		for _, money := range []any{nil, "11.25", "10.000000000000000001"} {
			t.Run(path+fmt.Sprint(money), func(t *testing.T) {
				body := teamMemberWarningFixturePolicy(path, map[string]any{"tokens_month": 10, "money_month": money})
				_, currency := body["currency"]
				if currency != (money != nil) {
					t.Fatal("currency presence must match finite money")
				}
				for _, field := range []string{"tokens_5h", "tokens_7d"} {
					_, present := body[field]
					if present == strings.Contains(path, "/members/") {
						t.Fatal("unsupported member field", field)
					}
				}
				if body["tokens_month"] != 10 || body["money_month"] != money {
					t.Fatal("exact fixture values changed")
				}
			})
		}
	}
}

func TestTeamMemberWarningFixturePolicyDecoder(t *testing.T) {
	for _, path := range []string{"/api/v1/teams/tea_test/limits", "/api/v1/teams/tea_test/members/usr_test/limits"} {
		for _, money := range []any{nil, "10.000000000000000001"} {
			t.Run(path+fmt.Sprint(money), func(t *testing.T) {
				body := teamMemberWarningFixturePolicy(path, map[string]any{"tokens_month": 10, "money_month": money})
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				var input service.TeamLimitInput
				if err := input.UnmarshalJSON(raw); err != nil {
					t.Fatal("fixture must use the real strict policy decoder", err)
				}
				if input.Reason != "Team member warning acceptance" || string(input.Fields["tokens_month"]) != "10" {
					t.Fatal("exact reason or integer changed")
				}
				for _, field := range []string{"tokens_5h", "tokens_7d"} {
					_, present := input.Fields[field]
					if present == strings.Contains(path, "/members/") {
						t.Fatal("strict decoded scope fields", field)
					}
				}
				if money == nil {
					if string(input.Fields["money_month"]) != "null" || input.Fields["currency"] != nil {
						t.Fatal("clear money requires omitted currency")
					}
				} else if string(input.Fields["money_month"]) != `"10.000000000000000001"` || string(input.Fields["currency"]) != `"USD"` {
					t.Fatal("exact finite money and denomination changed")
				}
			})
		}
	}
}
