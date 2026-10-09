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

// testTeamMonthlyQuotaWarningLifecycle runs against each supported database in
// the fresh-database harness. Warmup establishes real journal coverage before
// any notified resource is created; no historical accounting is manufactured.
func testTeamMonthlyQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
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
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
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
	cipher, err := store.Seal("crd_team_warning", "team-warning-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_team_warning"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_team_warning", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_team_warning", ProviderID: "prv_team_warning", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_warning", ConnectionID: "con_team_warning", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_warning", ConnectionID: "con_team_warning", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_warning", ProviderModelID: "pmd_team_warning"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "team-warning-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_team_warning", ModelID: modelID, ProviderModelID: "pmd_team_warning", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_team_warning_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_warning_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_team_warning", ProviderModelID: "pmd_team_warning", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_team_warning_%d", index), ModelPriceID: "price_team_warning", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	spool := filepath.Join(t.TempDir(), "warnings.db")
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
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
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"team-warning-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_team_warning/reservation-bound"
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
	member, err := svc.CreateMember(ctx, admin.User.ID, "team-warning-member@example.invalid", "monthly-notification-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "team-warning-outsider@example.invalid", "monthly-notification-password", "Other member", "member")
	if err != nil {
		t.Fatal(err)
	}

	owner, err := svc.CreateMember(ctx, admin.User.ID, "team-warning-owner@example.invalid", "monthly-notification-password", "Team owner", "member")
	if err != nil {
		t.Fatal(err)
	}
	later, err := svc.CreateMember(ctx, admin.User.ID, "team-warning-later@example.invalid", "monthly-notification-password", "Later member", "member")
	if err != nil {
		t.Fatal(err)
	}
	teamID := "tem_team_warning"
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-warning-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
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
	create(&entity.APIKey{ID: "key_team_warning_member", UserID: member.User.ID, Name: "Personal warning", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_team_warning_member", ModelID: modelID})
	create(&entity.Team{ID: teamID, Name: "Frozen aggregate Team", Status: entity.ResourceActive})
	create(&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	create(&entity.TeamMembership{ID: "tmb_team_warning_caller", TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	create(&entity.TeamMembership{ID: "tmb_team_warning_owner", TeamID: teamID, UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
	pending, err := svc.CreateMember(ctx, admin.User.ID, "team-warning-pending@example.invalid", "monthly-notification-password", "Pending Team member", "member")
	if err != nil {
		t.Fatal(err)
	}
	applicationID := "raa_01j00000000000000000000000"
	create(&entity.RegistrationApprovalApplication{ID: applicationID, UserID: pending.User.ID, UserCreatedAt: pending.User.CreatedAt, CreatedAt: pending.User.CreatedAt, State: "pending", Revision: strings.Repeat("a", 64)})
	if err := db.Model(&entity.User{}).Where("id = ?", pending.User.ID).UpdateColumn("approval_application_id", applicationID).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.TeamMembership{ID: "tmb_team_warning_pending", TeamID: teamID, UserID: pending.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	// A collation alias may be retained by a permissive driver, but never gains
	// exact recipient authority. The late member later receives a canonical link.
	aliasTeamID := "tmb_team_warning_alias_team"
	aliasError := db.Create(&entity.TeamMembership{ID: aliasTeamID, TeamID: strings.ToUpper(teamID), UserID: later.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error
	if aliasError != nil && !errors.Is(aliasError, gorm.ErrForeignKeyViolated) {
		t.Fatal("alias Team membership failed for unrelated reason", aliasError)
	}
	aliasStored := aliasError == nil
	teamCall := func(target string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/api/v1/teams/"+target+"/chat/completions", strings.NewReader(`{"model":"team-warning-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
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
	userPath := "/api/v1/teams/" + teamID + "/limits"
	write := func(path string, values map[string]any) entity.ResourceLimit {
		t.Helper()
		current := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200)
		complete := teamWarningCompletePolicyInput(values)
		result := decodeCatalogResponse[service.LimitRecord](t, adminRequest("PUT", path, complete, current.ETag), 200)
		if !result.Enforced {
			t.Fatal("Team policy not applied")
		}
		target := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/teams/"), "/limits")
		var row entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "team", target).Take(&row).Error; err != nil || row.ScopeID != target {
			t.Fatal("stored Team revision unavailable", err)
		}
		return row // Team reviewed HTTP ETag differs from the recorded policy revision.
	}
	assertObserverReady := func(stage string) service.RuntimeStatus {
		t.Helper()
		status := svc.RuntimeStatus()
		if !status.Enabled || !status.Ready || status.ErrorCode != "" || status.SnapshotID == "" || status.AuthorizationValidUntil == nil || !time.Now().Before(*status.AuthorizationValidUntil) {
			t.Fatalf("%s: manually published observer authorization is not ready", stage)
		}
		return status
	}
	reconcileUnpublished := func() {
		t.Helper()
		// StopRuntime joins the publisher but retains manual publication. Require
		// its original live lease throughout this cycle so expiry cannot pass an
		// unpublished-mismatch negative merely by skipping every observation.
		before := assertObserverReady("before reconcile")
		err := svc.ReconcileMonthlyQuotaNotifications(ctx)
		after := assertObserverReady("after reconcile")
		if after.SnapshotID != before.SnapshotID || !after.AuthorizationValidUntil.Equal(*before.AuthorizationValidUntil) || !reflect.DeepEqual(after.PublishedAt, before.PublishedAt) || !reflect.DeepEqual(after.LastRefreshAt, before.LastRefreshAt) {
			t.Fatal("unpublished reconcile changed its manually captured publication")
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() {
		t.Helper()
		refresh()
		assertObserverReady("before positive reconcile")
		if err := svc.ReconcileMonthlyQuotaNotifications(ctx); err != nil {
			t.Fatal(err)
		}
		refresh()
		assertObserverReady("after positive reconcile")
	}
	page := func() service.NotificationPage {
		t.Helper()
		return decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	}
	count := func(owner string) int64 {
		t.Helper()
		var value int64
		if err := db.Model(&entity.TeamQuotaWarningObservation{}).Where(database.ExactText(db, clause.Column{Name: "team_id"}, owner)).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	assertCount := func(owner string, expected int64) {
		t.Helper()
		if got := count(owner); got != expected {
			t.Fatalf("owner warning count %d, want %d", got, expected)
		}
	}
	// More than one 32-row page precedes the notified owner. Full manual reconcile
	// must reach it; these covered-zero positive caps cannot manufacture warnings.
	cap100 := int64(100)
	for index := range 33 {
		id := fmt.Sprintf("tem_000_warn_%02d", index)
		create(&entity.Team{ID: id, Name: "Covered zero Team scan", Status: entity.ResourceActive})
		create(&entity.ResourceLimit{ScopeKind: "team", ScopeID: id, ETag: fmt.Sprintf("lim_warning_scan_%02d", index), ActorID: admin.User.ID, Reason: "Bounded fair warning scan", TokensMonth: &cap100, IPMode: "none", IPRangesJSON: "[]"})
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
	assertCount(teamID, 0)
	for index := range 3 {
		expectStatus(t, teamCall(teamID), 200)
		flush()
		reconcile()
		assertCount(teamID, 0)
		if index == 2 && dispatches.Load() != 5 {
			t.Fatal("unexpected dispatch before near level")
		}
	}
	expectStatus(t, teamCall(teamID), 200)
	flush()
	// Settlement alone never sends an inbox warning through the gateway.
	assertCount(teamID, 0)
	reconcile()
	assertCount(teamID, 1)
	nearPage := page()
	if len(nearPage.Items) != 1 || nearPage.UnreadCount != 1 {
		t.Fatal("near warning missing from recipient inbox")
	}
	near := nearPage.Items[0]
	if near.QuotaWarning == nil || near.QuotaWarning.ScopeKind != "team" || near.QuotaWarning.ThresholdGeneration != "team-monthly-80-90-v1" || near.SubjectType != "team" || near.SubjectID != teamID || near.SubjectName != "Frozen aggregate Team" || near.Quota != nil || near.Kind != "monthly_quota_warning" || near.Severity != "medium" || near.QuotaWarning.Level != "near" || near.QuotaWarning.Threshold != 80 || near.QuotaWarning.Settled != "8" || near.QuotaWarning.Limit != "10" || near.QuotaWarning.Currency != nil || near.QuotaWarning.PolicyRevision != policy.ETag || near.QuotaWarning.ScopeID != teamID {
		t.Fatal("near snapshot was not exact")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("warning read was not recorded")
	}
	var readInbox entity.TeamQuotaWarningInbox
	if err := db.Take(&readInbox, "id = ?", near.ID).Error; err != nil || readInbox.ReadAt == nil {
		t.Fatal("persisted read timestamp missing", err)
	}
	var recipientRows []entity.TeamQuotaWarningInbox
	if err := db.Where("observation_id = ?", near.QuotaWarningObservationID).Order("recipient_id").Find(&recipientRows).Error; err != nil || len(recipientRows) != 2 {
		t.Fatal("bounded original Team fanout", err)
	}
	var pendingReceipts int64
	if err := db.Model(&entity.TeamQuotaWarningInbox{}).Where("recipient_id = ?", pending.User.ID).Count(&pendingReceipts).Error; err != nil || pendingReceipts != 0 {
		t.Fatal("pending account inherited Team warning receipt", err)
	}
	expectedBirths := map[string]time.Time{member.User.ID: member.User.CreatedAt, owner.User.ID: owner.User.CreatedAt}
	for _, receipt := range recipientRows {
		birth, ok := expectedBirths[receipt.RecipientID]
		if !ok || !receipt.RecipientCreatedAt.Equal(birth) {
			t.Fatal("recipient birth not captured privately")
		}
	}
	var original entity.TeamQuotaWarningObservation
	if err := db.Take(&original, "id = ?", near.QuotaWarningObservationID).Error; err != nil {
		t.Fatal(err)
	}
	// Renew the manually published authorization after inbox and fanout checks.
	refresh()
	single.Store(true)
	expectStatus(t, teamCall(teamID), 200)
	single.Store(false)
	flush()
	reconcile()
	reconcile()
	assertCount(teamID, 2)
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
		t.Fatal("Team settlement charged Personal account", err)
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
	assertCount(teamID, 3)
	// Independent money levels use already settled, known USD amounts. No money
	// reservation or fictitious earlier crossing is needed to produce these facts.
	moneyNear := write(userPath, map[string]any{"money_month": "11.25", "currency": "USD"})
	reconcile()
	assertCount(teamID, 4)
	moneyCritical := write(userPath, map[string]any{"money_month": "10", "currency": "USD"})
	reconcile()
	assertCount(teamID, 5)
	precise := write(userPath, map[string]any{"money_month": "10.000000000000000001", "currency": "USD"})
	reconcile()
	assertCount(teamID, 6)
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
	assertCount(teamID, 6)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	assertCount(teamID, 7)
	for _, item := range page().Items {
		if item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == unpublished.ETag && (item.QuotaWarning.Currency == nil || *item.QuotaWarning.Currency != "USD") {
			t.Fatal("denomination restoration changed historical currency")
		}
	}
	// Stale runtime policy cannot prove warning eligibility; exact restoration
	// leaves the original immutable records intact.
	var savedPolicy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team", teamID).Take(&savedPolicy).Error; err != nil {
		t.Fatal(err)
	}
	assertPolicyRow := func(want entity.ResourceLimit) {
		t.Helper()
		var got entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "team", teamID).Take(&got).Error; err != nil {
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
	// Renew only the restored baseline, never the deliberate unpublished policy.
	refresh()
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", teamID).UpdateColumn("ETag", "lim_warning_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	changedPolicy := savedPolicy
	changedPolicy.ETag = "lim_warning_unpublished"
	assertPolicyRow(changedPolicy)
	reconcileUnpublished()
	assertCount(teamID, 7)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", teamID).UpdateColumn("ETag", savedPolicy.ETag).Error; err != nil {
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
		// Each independent negative starts from the restored calendar publication.
		refresh()
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
		assertCount(teamID, 7)
		if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).UpdateColumn(change.field, change.original).Error; err != nil {
			t.Fatal(err)
		}
		assertCalendarRow(savedCalendar)
	}
	reconcile()
	assertCount(teamID, 8)
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
	var savedTeam entity.Team
	if err := db.Take(&savedTeam, "id = ?", teamID).Error; err != nil {
		t.Fatal(err)
	}
	refresh() // Publish the original Team birth before its independent raw mutation.
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("created_at", savedTeam.CreatedAt.Add(time.Millisecond)).Error; err != nil {
		t.Fatal(err)
	}
	var changedTeam entity.Team
	if err := db.Take(&changedTeam, "id = ?", teamID).Error; err != nil || !changedTeam.CreatedAt.Equal(savedTeam.CreatedAt.Add(time.Millisecond)) || changedTeam.CreatedAt.Equal(savedTeam.CreatedAt) {
		t.Fatal("Team birth negative was not persisted distinctly", err)
	}
	if len(page().Items) != 0 {
		t.Fatal("recreated Team inherited original warning history")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	reconcileUnpublished()
	assertCount(teamID, 8)
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("created_at", savedTeam.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	// A different birth with the same retained ID must hide old private history.
	var savedUser entity.User
	if err := db.Take(&savedUser, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	changedBirth := savedUser.CreatedAt.Add(time.Millisecond)
	refresh() // Team birth is restored; publish the original recipient birth only.
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
	if err := db.Model(&entity.TeamQuotaWarningInbox{}).Where("recipient_id = ? AND read_at IS NOT NULL", member.User.ID).Count(&birthReadCount).Error; err != nil || birthReadCount != 1 {
		t.Fatal("read-all crossed recipient birth", err)
	}
	reconcileUnpublished()
	assertCount(teamID, 8)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("created_at", savedUser.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	// NULL and zero retain existing exhaustion behavior, never threshold warnings.
	write(userPath, map[string]any{})
	reconcile()
	assertCount(teamID, 8)
	write(userPath, map[string]any{"tokens_month": 0})
	reconcile()
	assertCount(teamID, 8)
	write(userPath, map[string]any{"tokens_month": 9, "money_month": "9", "currency": "USD"})
	reconcile()
	assertCount(teamID, 8)
	beforeDenied := dispatches.Load()
	deniedResponse := teamCall(teamID)
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
	if deniedCall.Status != "error" || deniedCall.ErrorCode != "quota_exceeded" || deniedCall.TeamID != teamID || deniedCall.TeamMembershipID != "tmb_team_warning_caller" || deniedCall.UserID != member.User.ID || deniedCall.KeyID != "" || deniedCall.ProjectID != "" {
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
		if item.SubjectID == teamID {
			t.Fatal("Team warning/exhaustion fanned out to nonmember administrator")
		}
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?recipient_id="+outsider.User.ID, nil), 400)

	ownerLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-warning-owner@example.invalid","password":"monthly-notification-password"}`, nil, "")
	ownerAuth, ownerCookie := readIdentity(t, ownerLogin)
	ownerPage := decodeCatalogResponse[service.NotificationPage](t, request(ownerCookie, ownerAuth.CSRFToken, "GET", "/api/v1/notifications?status=all", nil, ""), 200)
	if len(ownerPage.Items) != 11 || ownerPage.UnreadCount != 11 {
		t.Fatal("original active owner did not receive complete bounded fanout")
	}
	lateLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-warning-later@example.invalid","password":"monthly-notification-password"}`, nil, "")
	lateAuth, lateCookie := readIdentity(t, lateLogin)
	if aliasStored {
		if err := db.Delete(&entity.TeamMembership{}, "id = ?", aliasTeamID).Error; err != nil {
			t.Fatal(err)
		}
	}
	create(&entity.TeamMembership{ID: "tmb_team_warning_later", TeamID: teamID, UserID: later.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("name", "Renamed current Team").Error; err != nil {
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

	if err := db.Delete(&entity.TeamMembership{}, "id = ?", "tmb_team_warning_caller").Error; err != nil {
		t.Fatal(err)
	}
	refresh()
	if len(page().Items) != 0 {
		t.Fatal("departed original recipient retained Team history")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	var inaccessibleReceipt entity.TeamQuotaWarningInbox
	if err := db.Take(&inaccessibleReceipt, "id = ?", near.ID).Error; err != nil || !reflect.DeepEqual(inaccessibleReceipt, readInbox) {
		t.Fatal("read-all changed inaccessible original receipt", err)
	}
	create(&entity.TeamMembership{ID: "tmb_team_warning_rejoin", TeamID: teamID, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	refresh()
	if !reflect.DeepEqual(page(), merged) {
		t.Fatal("rejoin changed original history/read state or name")
	}
	for _, item := range page().Items {
		if item.SubjectName != "Frozen aggregate Team" {
			t.Fatal("replay rewrote recorded Team name")
		}
	}
	for _, state := range []struct {
		model             any
		id, field         string
		invalid, original any
	}{
		{&entity.TeamMembership{}, "tmb_team_warning_rejoin", "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceDisabled, entity.ResourceActive},
		{&entity.Team{}, teamID, "status", entity.ResourceArchived, entity.ResourceActive},
		{&entity.TeamMembership{}, "tmb_team_warning_rejoin", "user_id", strings.ToUpper(member.User.ID), member.User.ID},
		{&entity.TeamMembership{}, "tmb_team_warning_rejoin", "team_id", strings.ToUpper(teamID), teamID},
	} {

		var membershipBefore entity.TeamMembership
		if state.field == "user_id" || state.field == "team_id" {
			if err := db.Take(&membershipBefore, "id = ?", state.id).Error; err != nil {
				t.Fatal(err)
			}
		}
		if err := db.Model(state.model).Where("id = ?", state.id).UpdateColumn(state.field, state.invalid).Error; err != nil {
			if (state.field != "user_id" && state.field != "team_id") || !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal("membership negative failed for unrelated reason", err)
			}
			var unchanged entity.TeamMembership
			if err := db.Take(&unchanged, "id = ?", state.id).Error; err != nil || !reflect.DeepEqual(unchanged, membershipBefore) || !reflect.DeepEqual(page(), merged) {
				t.Fatal("FK-rejected alias changed canonical authority", err)
			}
			continue // Exact FK rejection preserves the original authorized relationship.
		}
		if len(page().Items) != 0 {
			t.Fatal("inactive or alias membership exposed historical Team warning")
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
		observation.ID = "two_wa_" + kind
		observation.PolicyRevision = "lim_warning_alias_" + kind
		recipient, join := member.User.ID, observation.ID
		switch kind {
		case "owner":
			observation.TeamID = strings.ToUpper(teamID)
			if observation.TeamID == teamID {
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
			observation.ResourceCreatedAt = original.ResourceCreatedAt.Add(-time.Microsecond)
		}
		create(&observation)
		inboxID := "twi_wa_" + kind
		aliasIDs = append(aliasIDs, inboxID)
		receiptBirth := savedUser.CreatedAt
		if kind == "recipient_birth" {
			receiptBirth = receiptBirth.Add(-time.Microsecond)
		}
		create(&entity.TeamQuotaWarningInbox{ID: inboxID, ObservationID: join, RecipientID: recipient, RecipientCreatedAt: receiptBirth, CreatedAt: observation.AsOf})
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
	var hiddenInboxes []entity.TeamQuotaWarningInbox
	if err := db.Where("id IN ?", aliasIDs).Find(&hiddenInboxes).Error; err != nil || len(hiddenInboxes) != 5 {
		t.Fatal("private alias fixtures missing", err)
	}
	for _, inbox := range hiddenInboxes {
		if inbox.ReadAt != nil {
			t.Fatal("read-all altered hidden history")
		}
	}
	// Current disabled lifecycle denies the existing recipient Session and observation.
	refresh() // Renew the enabled baseline after the independent privacy checks.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 401)
	reconcileUnpublished()
	assertCount(teamID, 12) //8 original+4 canonical-Team corrupt histories.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Unknown usage and a live hold require separate real completed native calls.
	makeTarget := func(label string) (string, string) {
		t.Helper()
		target := "tem_team_warning_" + label
		create(&entity.Team{ID: target, Name: "Warning " + label, Status: entity.ResourceActive})
		create(&entity.TeamModelGrant{TeamID: target, ModelID: modelID})
		create(&entity.TeamMembership{ID: "tmb_team_warn_" + label, TeamID: target, UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive})
		create(&entity.TeamMembership{ID: "tmb_team_owner_" + label, TeamID: target, UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
		refresh()
		return target, "/api/v1/teams/" + target + "/limits"
	}
	unknownID, unknownPath := makeTarget("unknown")
	write(unknownPath, map[string]any{"tokens_month": 2})
	unknown.Store(true)
	expectStatus(t, teamCall(unknownID), 200)
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
		heldResponse <- teamCall(heldID)
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
	var finalOriginal entity.TeamQuotaWarningObservation
	if err := db.Take(&finalOriginal, "id = ?", original.ID).Error; err != nil || !reflect.DeepEqual(finalOriginal, original) {
		t.Fatal("policy/level/replay changed original observation", err)
	}
	var allCalls []entity.CallRecord
	if err := db.Where("team_id IN ? OR key_id IN ?", []string{teamID, unknownID, heldID}, []string{"key_team_warning_member", "key_team_warning_admin"}).Order("request_id").Find(&allCalls).Error; err != nil {
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
		if fact.ProjectID != "" || fact.Status != "success" || len(attempts) != 1 || attempts[0].CredentialID != "crd_team_warning" || attempts[0].SnapshotID == "" || attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("native attribution/completion changed")
		}
		if fact.TeamID != "" {
			membership := "tmb_team_warning_caller"
			if fact.TeamID == unknownID {
				membership = "tmb_team_warn_unknown"
			}
			if fact.TeamID == heldID {
				membership = "tmb_team_warn_held"
			}
			if fact.UserID != member.User.ID || fact.KeyID != "" || fact.TeamMembershipID != membership {
				t.Fatal("Team native used another identity or Key")
			}
		} else if fact.TeamMembershipID != "" || fact.KeyID != "key_team_warning_member" && fact.KeyID != "key_team_warning_admin" {
			t.Fatal("Personal calibration borrowed Team identity")
		}
		attemptFacts[attempts[0].ID] = attempts[0]
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("Team warnings created operational/SMTP fanout", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := svc.ReconcileMonthlyQuotaNotifications(canceled); err == nil {
		t.Fatal("canceled reconciliation ignored cancellation")
	}
	assertCount(teamID, 12)
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
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	flush()
	refresh()
	reconcile()
	afterRestart := page()
	var afterCalls []entity.CallRecord
	if err := db.Where("team_id IN ? OR key_id IN ?", []string{teamID, unknownID, heldID}, []string{"key_team_warning_member", "key_team_warning_admin"}).Order("request_id").Find(&afterCalls).Error; err != nil || !reflect.DeepEqual(afterCalls, allCalls) {
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

// Complete replacement clears all seven caps; denomination belongs only to a
// non-null money cap, as required by the real Team policy request contract.
func teamWarningCompletePolicyInput(values map[string]any) map[string]any {
	complete := map[string]any{"tokens_5h": nil, "tokens_7d": nil, "tokens_month": nil, "money_month": nil, "rpm": nil, "tpm": nil, "concurrency": nil, "reason": "Team aggregate monthly warning acceptance"}
	for key, value := range values {
		complete[key] = value
	}
	if complete["money_month"] == nil {
		delete(complete, "currency")
	} else if _, ok := complete["currency"]; !ok {
		complete["currency"] = "USD"
	}
	return complete
}

func TestTeamWarningCompletePolicyInput(t *testing.T) {
	for _, values := range []map[string]any{{"tokens_month": 10}, {"money_month": nil, "currency": "USD"}, {"money_month": "11.25", "currency": "USD"}, {"money_month": "10"}} {
		got := teamWarningCompletePolicyInput(values)
		money := got["money_month"] != nil
		currency, hasCurrency := got["currency"]
		if hasCurrency != money || money && currency != "USD" {
			t.Fatal("denomination must exist exactly for a non-null money cap")
		}
		for _, field := range []string{"tokens_5h", "tokens_7d", "tokens_month", "money_month", "rpm", "tpm", "concurrency"} {
			value, present := got[field]
			if !present || !reflect.DeepEqual(value, values[field]) {
				t.Fatal("complete replacement changed a reviewed cap", field)
			}
		}
		if got["reason"] != "Team aggregate monthly warning acceptance" {
			t.Fatal("review reason changed")
		}
	}
}
