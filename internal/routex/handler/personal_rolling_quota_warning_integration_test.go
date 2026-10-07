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
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testPersonalRollingQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, e := secretstore.New(bytes.Repeat([]byte{81}, 32))
	if e != nil {
		t.Fatal(e)
	}
	var tokens atomic.Int64
	tokens.Store(2)
	var unknown, inboxFault atomic.Bool
	var dispatches atomic.Int64
	// Registry installation precedes all runtime/recorder workers. Only the atomic
	// arming flag changes while they exist; removal follows their cancellation/join.
	callback := "fixture:personal-rolling-inbox"
	if e := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if inboxFault.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "personal_rolling_quota_warning_inboxes" {
			_ = tx.AddError(errors.New("controlled rolling inbox failure"))
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		usage := fmt.Sprintf(`,"usage":{"prompt_tokens":%d,"completion_tokens":0,"total_tokens":%d}`, tokens.Load(), tokens.Load())
		if unknown.Load() {
			usage = ""
		}
		_, _ = io.WriteString(w, `{"id":"rolling-native","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]`+usage+`}`)
	}))
	defer upstream.Close()
	makeService := func() *service.Service {
		t.Helper()
		svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if e != nil {
			t.Fatal(e)
		}
		return svc
	}
	svc := makeService()
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"rolling-admin@example.invalid","password":"rolling-warning-password","name":"Rolling admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, _ := readIdentity(t, setup)
	create := func(v any) {
		t.Helper()
		if e := db.Create(v).Error; e != nil {
			t.Fatal(e)
		}
	}
	cipher, e := store.Seal("crd_rolling_warning", "controlled-rolling-credential")
	if e != nil {
		t.Fatal(e)
	}
	model := "mdl_rolling_warning"
	warmup := "rx_" + strings.Repeat("a", 43)
	for _, v := range []any{&entity.Provider{ID: "prv_rolling_warning", Name: "Rolling warning"}, &entity.ProviderConnection{ID: "con_rolling_warning", ProviderID: "prv_rolling_warning", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1", Enabled: true}, &entity.ProviderCredential{ID: "crd_rolling_warning", ConnectionID: "con_rolling_warning", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_rolling_warning", ConnectionID: "con_rolling_warning", UpstreamName: "rolling-native"}, &entity.CredentialModelAccess{CredentialID: "crd_rolling_warning", ProviderModelID: "pmd_rolling_warning"}, &entity.Model{ID: model, Status: "active"}, &entity.ModelName{Name: "rolling-warning-model", ModelID: model, CurrentModelID: &model}, &entity.ModelProviderBinding{ID: "bnd_rolling_warning", ModelID: model, ProviderModelID: "pmd_rolling_warning", Weight: 100}, &entity.UserModelGrant{UserID: admin.User.ID, ModelID: model}, &entity.APIKey{ID: "key_rolling_warmup", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmup), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_rolling_warmup", ModelID: model}} {
		create(v)
	}
	spool := filepath.Join(t.TempDir(), "rolling.db")
	if e := svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	svc.StopRuntime()
	if e := svc.StartCallRecorder(ctx, spool); e != nil {
		t.Fatal(e)
	}
	call := func(key string) {
		t.Helper()
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"rolling-warning-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+key)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if e := svc.FlushCallRecorder(ctx); e != nil {
			t.Fatal(e)
		}
	}
	call(warmup)
	if e := svc.RefreshRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	member, e := svc.CreateMember(ctx, admin.User.ID, "rolling-member@example.invalid", "rolling-warning-password", "Rolling member", "member")
	if e != nil {
		t.Fatal(e)
	}
	outsider, e := svc.CreateMember(ctx, admin.User.ID, "rolling-other@example.invalid", "rolling-warning-password", "Other", "member")
	if e != nil {
		t.Fatal(e)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"rolling-member@example.invalid","password":"rolling-warning-password"}`, nil, "")
	identity, cookie := readIdentity(t, login)
	bearer := "rx_" + strings.Repeat("b", 43)
	create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: model})
	create(&entity.APIKey{ID: "key_rolling_member", UserID: member.User.ID, Name: "Member", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_rolling_member", ModelID: model})
	target := service.LimitTarget{Kind: "user", ID: member.User.ID}
	cap := int64(100)
	current, e := svc.GetResourceLimit(ctx, admin.User.ID, target)
	if e != nil {
		t.Fatal(e)
	}
	policy, e := svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Rolling warning fixture"})
	if e != nil {
		t.Fatal(e)
	}
	// Finite source capacity, separate from settled native usage.
	// Use the normal bound API so quota admission still exercises its hard gates.
	_, adminCookie := readIdentity(t, setup)
	setBound := func(input int64) {
		t.Helper()
		get := identityRequest(router, "GET", "/api/v1/admin/provider-models/pmd_rolling_warning/reservation-bound", "", adminCookie, "")
		expectStatus(t, get, 200)
		reviewed := decodeCatalogResponse[service.ReservationBoundRecord](t, get, 200)
		req := httptest.NewRequest("PUT", "http://routex.test/api/v1/admin/provider-models/pmd_rolling_warning/reservation-bound", strings.NewReader(fmt.Sprintf(`{"max_input_tokens":%d,"max_output_tokens":1,"evidence":"Controlled native fixture","reason":"Rolling quota test"}`, input)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("If-Match", `"`+reviewed.ETag+`"`)
		req.AddCookie(adminCookie)
		req.Header.Set("X-CSRF-Token", admin.CSRFToken)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
	}
	setBound(80)
	reconcile := func() {
		t.Helper()
		if e := svc.RefreshRuntime(ctx); e != nil {
			t.Fatal(e)
		}
		if e := svc.ReconcileMonthlyQuotaNotifications(ctx); e != nil {
			t.Fatal(e)
		}
	}
	count := func(want int64) {
		t.Helper()
		var n int64
		if e := db.Model(&entity.PersonalRollingQuotaWarningObservation{}).Count(&n).Error; e != nil || n != want {
			t.Fatal("rolling count", n, want, e)
		}
	}
	tokens.Store(80)
	call(bearer)
	inboxFault.Store(true)
	if e := svc.ReconcileMonthlyQuotaNotifications(ctx); e == nil {
		t.Fatal("inbox failure not surfaced")
	}
	inboxFault.Store(false)
	count(0)
	var states int64
	if e := db.Model(&entity.PersonalRollingQuotaWarningState{}).Where("owner_id = ?", member.User.ID).Count(&states).Error; e != nil || states != 0 {
		t.Fatal("failed inbox committed episode", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	count(2)
	page, e := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(page.Items) != 2 || page.UnreadCount != 2 {
		t.Fatal("private rolling inbox", e)
	}
	for _, n := range page.Items {
		v := n.RollingQuotaWarning
		if v == nil || v.ScopeID != member.User.ID || v.PolicyRevision != policy.ETag || v.Settled != "80" || v.Limit != "100" || v.Threshold != 80 || n.Kind != "personal_rolling_quota_warning" || n.DeliveryStatus != "" {
			t.Fatal("exact settled snapshot lost")
		}
	}
	other, e := svc.ListNotifications(ctx, outsider.User.ID, service.NotificationFilter{})
	if e != nil || len(other.Items) != 0 {
		t.Fatal("cross-owner leak", e)
	}
	if _, e := svc.MarkNotificationRead(ctx, outsider.User.ID, page.Items[0].ID); e == nil {
		t.Fatal("outsider marked private rolling notice")
	}
	if _, e := svc.MarkNotificationRead(ctx, member.User.ID, page.Items[0].ID); e != nil {
		t.Fatal(e)
	}
	// A general revision change with unchanged rolling caps must not spam.
	policy, e = svc.SetResourceLimit(ctx, admin.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Unrelated reason edit"})
	if e != nil || policy.Stored.Tokens5H == nil || *policy.Stored.Tokens5H != cap || policy.Stored.Tokens7D == nil || *policy.Stored.Tokens7D != cap {
		t.Fatal("reason edit changed monitored caps", e)
	}
	reconcile()
	count(2)
	setBound(10)
	tokens.Store(10)
	call(bearer)
	reconcile()
	count(4)
	// A current policy save and producer race share the normal application fence.
	errs = make(chan error, 2)
	wg.Go(func() {
		_, e := svc.SetResourceLimit(ctx, admin.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Concurrent reason edit"})
		errs <- e
	})
	wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	count(4)
	// An exact birth mismatch cannot borrow either application or private inbox.
	birth := member.User.CreatedAt
	if e := db.Model(&entity.User{}).Where("id = ?", member.User.ID).Update("created_at", birth.Add(time.Microsecond)).Error; e != nil {
		t.Fatal(e)
	}
	if e := svc.ReconcileMonthlyQuotaNotifications(ctx); e != nil {
		t.Fatal(e)
	}
	count(4)
	changedBirth, e := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(changedBirth.Items) != 0 {
		t.Fatal("new birth borrowed old inbox", e)
	}
	if e := db.Model(&entity.User{}).Where("id = ?", member.User.ID).Update("created_at", birth).Error; e != nil {
		t.Fatal(e)
	}
	defaults, e := svc.GetDefaultLimit(ctx, admin.User.ID, "user")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := svc.SetDefaultLimit(ctx, admin.User.ID, "user", defaults.ETag, service.DefaultLimitInput{Policy: service.EffectiveLimitValues{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Reviewed rolling default"}); e != nil {
		t.Fatal(e)
	}
	review, e := svc.GetDefaultLimitResetContext(ctx, admin.User.ID, target)
	if e != nil {
		t.Fatal(e)
	}
	reset, e := svc.ResetResourceLimitToDefault(ctx, admin.User.ID, target, review.ETag, "Explicit rolling reset")
	if e != nil || !reset.Saved || !reset.RuntimeApplied {
		t.Fatal("reset not applied", e)
	}
	reconcile()
	count(6)
	page, e = svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(page.Items) != 6 {
		t.Fatal("reset episode inbox", e)
	}
	var critical int
	for _, n := range page.Items {
		if n.RollingQuotaWarning != nil && n.RollingQuotaWarning.Level == "critical" {
			critical++
		}
	}
	if critical != 4 {
		t.Fatal("first critical emitted lower reminder")
	}
	setBound(1)
	var retained []entity.PersonalRollingQuotaWarningObservation
	var inboxes []entity.PersonalRollingQuotaWarningInbox
	var episodes []entity.PersonalRollingQuotaWarningState
	capture := func() {
		t.Helper()
		if e := db.Order("id").Find(&retained).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Order("id").Find(&inboxes).Error; e != nil {
			t.Fatal(e)
		}
		if e := db.Order("owner_id,window_kind").Find(&episodes).Error; e != nil {
			t.Fatal(e)
		}
	}
	unknown.Store(true)
	tokens.Store(0)
	call(bearer)
	// Missing terminal usage with a finite attestation remains an economic hold,
	// distinct from an unbounded TokensUnknown fact. Inspect authoritative windows.
	held, e := svc.GetResourceLimit(ctx, admin.User.ID, target)
	if e != nil || held.QuotaUsage == nil || !held.QuotaUsage.Activated {
		t.Fatal("finite missing usage proof unavailable", e)
	}
	for _, window := range []*service.QuotaWindowRecord{held.QuotaUsage.FiveHours, held.QuotaUsage.SevenDays} {
		if window == nil || !window.Covered || window.TokensUsed != 90 || window.TokensHeld != 2 || window.TokensUnknown != 0 {
			t.Fatal("finite missing usage classification changed")
		}
	}
	capture()
	reconcile()
	count(6)
	var unchanged []entity.PersonalRollingQuotaWarningState
	if e := db.Order("owner_id,window_kind").Find(&unchanged).Error; e != nil || len(unchanged) != len(episodes) {
		t.Fatal("held usage changed episode set", e)
	}
	for i, after := range unchanged {
		before := episodes[i]
		if after.LastAsOf.Before(before.LastAsOf) {
			t.Fatal("held sample time regressed")
		}
		// A valid known-settled sample can advance ordering without rearming.
		// Every owner, birth, window, cap, reset, episode and sent flag stays exact.
		before.LastAsOf = after.LastAsOf
		if !personalRollingRetainedFactsEqual(before, after) {
			t.Fatal("held usage rearmed episode")
		}
	}
	if e := svc.StopCallRecorder(); e != nil {
		t.Fatal(e)
	}
	svc = makeService()
	if e := svc.StartRuntime(ctx); e != nil {
		t.Fatal(e)
	}
	svc.StopRuntime()
	if e := svc.StartCallRecorder(ctx, spool); e != nil {
		t.Fatal(e)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	reconcile()
	count(6)
	var after []entity.PersonalRollingQuotaWarningObservation
	var afterInbox []entity.PersonalRollingQuotaWarningInbox
	if e := db.Order("id").Find(&after).Error; e != nil || !reflect.DeepEqual(after, retained) {
		t.Fatal("restart rewrote observations", e)
	}
	if e := db.Order("id").Find(&afterInbox).Error; e != nil || !reflect.DeepEqual(afterInbox, inboxes) {
		t.Fatal("restart rewrote read history", e)
	}
	session := identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, "")
	expectStatus(t, session, 200)
	// Session reads return the existing identity body without issuing a cookie.
	var restored SessionResponse
	if e := json.Unmarshal(session.Body.Bytes(), &restored); e != nil {
		t.Fatal(e)
	}
	if restored.User.ID != identity.User.ID || restored.CSRFToken != identity.CSRFToken {
		t.Fatal("original session changed")
	}
	if len(session.Result().Cookies()) != 0 {
		t.Fatal("session read issued a replacement cookie")
	}
	if dispatches.Load() != 4 {
		t.Fatal("reconciliation dispatched native request")
	}
	if e := svc.MarkAllNotificationsRead(ctx, member.User.ID); e != nil {
		t.Fatal(e)
	}
	page, e = svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || page.UnreadCount != 0 {
		t.Fatal("rolling read-all", e)
	}
}
