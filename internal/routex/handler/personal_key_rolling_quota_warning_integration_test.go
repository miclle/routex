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

type personalKeyRollingConcurrencyContext struct{}

func testPersonalKeyRollingQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationBarrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &publicationBarrier)
	const publicationCallback = "fixture:personal-key-rolling-publication"
	if e := db.Callback().Query().Before("gorm:query").Register(publicationCallback, publicationBarrier.beforeQuery); e != nil {
		t.Fatal(e)
	}
	defer func() {
		publicationBarrier.armed.Store(false)
		if e := db.Callback().Query().Remove(publicationCallback); e != nil {
			t.Error(e)
		}
	}()
	// Force a real transaction snapshot before the second governance-lock wait.
	// The first observer holds that lock after finding the episode absent. With
	// repeatable read, the second observer later attempts a duplicate INSERT;
	// read committed must see the first observer's committed episode instead.
	firstStateRead := make(chan struct{})
	secondSnapshotRead := make(chan struct{})
	var firstStatePaused, secondSnapshotSeeded, secondStateRead atomic.Bool
	var secondTransaction gorm.ConnPool
	const concurrencyBefore = "fixture:personal-key-rolling-snapshot"
	const concurrencyAfter = "fixture:personal-key-rolling-overlap"
	if e := db.Callback().Query().Before("gorm:query").Register(concurrencyBefore, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(personalKeyRollingConcurrencyContext{}) != "second" || tx.Statement.Table != "governance_settings" || !secondSnapshotSeeded.CompareAndSwap(false, true) {
			return
		}
		secondTransaction = tx.Statement.ConnPool
		var setting entity.GovernanceSetting
		snapshotCtx := context.WithValue(tx.Statement.Context, personalKeyRollingConcurrencyContext{}, "snapshot")
		// NewDB clears the outer FOR UPDATE clause but preserves this transaction.
		err := tx.Session(&gorm.Session{NewDB: true, Context: snapshotCtx}).Select("id").Take(&setting, 1).Error
		close(secondSnapshotRead)
		if err != nil {
			_ = tx.AddError(err)
		}
	}); e != nil {
		t.Fatal(e)
	}
	if e := db.Callback().Query().After("gorm:after_query").Register(concurrencyAfter, func(tx *gorm.DB) {
		if tx.Statement.Table != "personal_key_rolling_quota_warning_states" {
			return
		}
		switch tx.Statement.Context.Value(personalKeyRollingConcurrencyContext{}) {
		case "first":
			if !errors.Is(tx.Error, gorm.ErrRecordNotFound) || !firstStatePaused.CompareAndSwap(false, true) {
				return
			}
			close(firstStateRead)
			select {
			case <-secondSnapshotRead:
			case <-tx.Statement.Context.Done():
				_ = tx.AddError(tx.Statement.Context.Err())
			}
		case "second":
			// Fail closed if an unrelated earlier transaction consumed the barrier.
			if tx.Statement.ConnPool != secondTransaction {
				_ = tx.AddError(errors.New("snapshot barrier did not bind the rolling observer transaction"))
				return
			}
			secondStateRead.Store(true)
		}
	}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		for _, name := range []string{concurrencyBefore, concurrencyAfter} {
			if e := db.Callback().Query().Remove(name); e != nil {
				t.Error(e)
			}
		}
	}()
	store, e := secretstore.New(bytes.Repeat([]byte{81}, 32))
	if e != nil {
		t.Fatal(e)
	}
	var tokens atomic.Int64
	tokens.Store(2)
	var unknown, inboxFault atomic.Bool
	var inboxFaultHits atomic.Int64
	errInboxFault := errors.New("controlled rolling inbox failure")
	var dispatches atomic.Int64
	// Registry installation precedes all runtime/recorder workers. Only the atomic
	// arming flag changes while they exist; removal follows their cancellation/join.
	callback := "fixture:personal-rolling-inbox"
	if e := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if inboxFault.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "personal_key_rolling_quota_warning_inboxes" {
			inboxFaultHits.Add(1)
			_ = tx.AddError(errInboxFault)
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
	if e := svc.StartRuntime(workerCtx); e != nil {
		t.Fatal(e)
	}
	// The Key observer rejects a closed runtime.done, even with a cached snapshot.
	// Keep the real publisher live; bounded positive observations fence its timer.
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
	if e := db.Take(&member.User, "id = ?", member.User.ID).Error; e != nil {
		t.Fatal(e)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"rolling-member@example.invalid","password":"rolling-warning-password"}`, nil, "")
	identity, cookie := readIdentity(t, login)
	bearer := "rx_" + strings.Repeat("b", 43)
	create(&entity.UserModelGrant{UserID: member.User.ID, ModelID: model})
	create(&entity.APIKey{ID: "key_00000000000000000000000001", LifecycleRevision: "kvr_00000000000000000000000001", UserID: member.User.ID, Name: "Member", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_00000000000000000000000001", ModelID: model})
	target := service.LimitTarget{Kind: "personal_key", ID: "key_00000000000000000000000001"}
	cap := int64(100)

	// Public limit targets are owner-scoped Personal Keys. The persisted account
	// kind is only an internal resolution result, never a control-plane target.
	if _, e := svc.GetResourceLimit(ctx, admin.User.ID, target); e == nil {
		t.Fatal("administrator borrowed owner-only Key limits")
	}
	if _, e := svc.GetResourceLimit(ctx, member.User.ID, service.LimitTarget{Kind: "key", ID: target.ID}); e == nil {
		t.Fatal("persisted account kind accepted as public target")
	}
	current, e := svc.GetResourceLimit(ctx, member.User.ID, target)
	if e != nil {
		t.Fatal(e)
	}

	if current.Kind != "personal_key" || current.ID != target.ID || current.AccountID != "key_"+target.ID {
		t.Fatal("public Key target did not resolve exact root account")
	}
	policy, e := svc.SetResourceLimit(ctx, member.User.ID, target, current.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Rolling warning fixture"})
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
	observePublished := func(observe func() error) error {
		return publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, observe)
	}
	reconcile := func() {
		t.Helper()
		if e := observePublished(func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); e != nil {
			t.Fatal(e)
		}
	}
	assertUsage := func(settled, held int64) {
		t.Helper()
		record, e := svc.GetResourceLimit(ctx, member.User.ID, target)
		if e != nil || record.AccountID != current.AccountID || record.QuotaUsage == nil || !record.QuotaUsage.Activated {
			t.Fatal("exact root usage proof unavailable", e)
		}
		for _, window := range []*service.QuotaWindowRecord{record.QuotaUsage.FiveHours, record.QuotaUsage.SevenDays} {
			if window == nil || !window.Covered || window.TokensUsed != settled || window.TokensHeld != held || window.TokensUnknown != 0 {
				t.Fatal("unexpected covered root usage", window, settled, held)
			}
		}
	}
	count := func(want int64) {
		t.Helper()
		var n int64
		if e := db.Model(&entity.PersonalKeyRollingQuotaWarningObservation{}).Count(&n).Error; e != nil || n != want {
			t.Fatal("rolling count", n, want, e)
		}
	}
	tokens.Store(80)
	call(bearer)
	assertUsage(80, 0)
	inboxFault.Store(true)
	if e := observePublished(func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); !errors.Is(e, errInboxFault) || inboxFaultHits.Load() != 1 {
		t.Fatal("exact inbox failure not surfaced", e, inboxFaultHits.Load())
	}
	inboxFault.Store(false)
	count(0)
	var states int64
	if e := db.Model(&entity.PersonalKeyRollingQuotaWarningState{}).Where("owner_id = ?", member.User.ID).Count(&states).Error; e != nil || states != 0 {
		t.Fatal("failed inbox committed episode", e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	if e := observePublished(func() error {
		raceCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		firstCtx := context.WithValue(raceCtx, personalKeyRollingConcurrencyContext{}, "first")
		secondCtx := context.WithValue(raceCtx, personalKeyRollingConcurrencyContext{}, "second")
		wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(firstCtx) })
		select {
		case <-firstStateRead:
			wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(secondCtx) })
		case <-raceCtx.Done():
			wg.Wait()
			return errors.New("first rolling observer did not reach the absent-state barrier")
		}
		wg.Wait()
		close(errs)
		var failure error
		for e := range errs {
			failure = errors.Join(failure, e)
		}
		return failure
	}); e != nil {
		t.Fatal(e)
	}
	if !firstStatePaused.Load() || !secondSnapshotSeeded.Load() || !secondStateRead.Load() {
		t.Fatal("concurrent rolling observers did not exercise the same transaction snapshot barrier")
	}
	count(2)
	page, e := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(page.Items) != 2 || page.UnreadCount != 2 {
		t.Fatal("private rolling inbox", e)
	}
	for _, n := range page.Items {
		v := n.PersonalKeyRollingQuotaWarning
		if v == nil || v.ScopeID != "key_00000000000000000000000001" || v.OwnerID != member.User.ID || v.PolicyRevision != policy.ETag || v.Settled != "80" || v.Limit != "100" || v.Threshold != 80 || n.Kind != "personal_key_rolling_quota_warning" || n.DeliveryStatus != "" {
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

	// A real retained rotation graph shares one account. The revoked original is
	// still the historical subject; only the current enabled descendant admits calls.
	parent := "key_00000000000000000000000001"
	childBearer := "rx_" + strings.Repeat("c", 43)
	create(&entity.APIKey{ID: "key_00000000000000000000000002", UserID: member.User.ID, Name: "Current replacement", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(childBearer), Status: entity.KeyActive, LifecycleRevision: "kvr_00000000000000000000000002", ReplacesKeyID: &parent})
	create(&entity.APIKeyModel{KeyID: "key_00000000000000000000000002", ModelID: model})
	if e := db.Model(&entity.APIKey{}).Where("id = ?", parent).Updates(map[string]any{"status": entity.KeyRevoked, "lifecycle_revision": "kvr_00000000000000000000000003", "name": "Renamed original"}).Error; e != nil {
		t.Fatal(e)
	}

	bearer = childBearer
	// A revoked predecessor remains readable, but policy writes use the current
	// live descendant and resolve back to the same immutable root account.
	if _, e := svc.SetResourceLimit(ctx, member.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Rejected revoked predecessor write"}); e == nil {
		t.Fatal("revoked predecessor admitted limit write")
	}
	target.ID = "key_00000000000000000000000002"
	descendant, e := svc.GetResourceLimit(ctx, member.User.ID, target)
	if e != nil || descendant.AccountID != current.AccountID || descendant.ETag != policy.ETag {
		t.Fatal("live descendant lost retained root policy", e)
	}
	reconcile()
	count(2)
	historical, e := svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(historical.Items) != 2 {
		t.Fatal("retained revoked-root inbox lost", e)
	}
	for _, n := range historical.Items {
		if n.SubjectID != parent || n.SubjectName != "Member" {
			t.Fatal("historical root name rewritten")
		}
	}
	// A general revision change with unchanged rolling caps must not spam.
	policy, e = svc.SetResourceLimit(ctx, member.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Unrelated reason edit"})
	if e != nil || policy.Stored.Tokens5H == nil || *policy.Stored.Tokens5H != cap || policy.Stored.Tokens7D == nil || *policy.Stored.Tokens7D != cap {
		t.Fatal("reason edit changed monitored caps", e)
	}
	reconcile()
	count(2)
	setBound(10)
	tokens.Store(10)
	call(bearer)
	assertUsage(90, 0)
	reconcile()
	count(4)
	// A current policy save and producer race share the normal application fence.
	errs = make(chan error, 2)
	if e := observePublished(func() error {
		wg.Go(func() {
			_, e := svc.SetResourceLimit(ctx, member.User.ID, target, policy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Concurrent reason edit"})
			errs <- e
		})
		wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
		wg.Wait()
		close(errs)
		var failure error
		for e := range errs {
			failure = errors.Join(failure, e)
		}
		return failure
	}); e != nil {
		t.Fatal(e)
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

	// Key accounts have no creation-default/reset endpoint. A reviewed change of
	// their stored own cap starts the new sampled episode without inventing one.
	freshPolicy, e := svc.GetResourceLimit(ctx, member.User.ID, target)
	if e != nil {
		t.Fatal(e)
	}
	nextCap := int64(99)
	policy, e = svc.SetResourceLimit(ctx, member.User.ID, target, freshPolicy.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &nextCap, Tokens7D: &nextCap}, Reason: "Reviewed own Key cap change"})
	if e != nil || !policy.Enforced {
		t.Fatal("own Key cap change not applied", e)
	}
	reconcile()
	count(6)
	page, e = svc.ListNotifications(ctx, member.User.ID, service.NotificationFilter{})
	if e != nil || len(page.Items) != 6 {
		t.Fatal("changed-cap episode inbox", e)
	}
	var critical int
	for _, n := range page.Items {
		if n.PersonalKeyRollingQuotaWarning != nil && n.PersonalKeyRollingQuotaWarning.Level == "critical" {
			critical++
		}
	}
	if critical != 4 {
		t.Fatal("first critical emitted lower reminder")
	}
	setBound(1)
	var retained []entity.PersonalKeyRollingQuotaWarningObservation
	var inboxes []entity.PersonalKeyRollingQuotaWarningInbox
	var episodes []entity.PersonalKeyRollingQuotaWarningState
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
	held, e := svc.GetResourceLimit(ctx, member.User.ID, target)
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
	var unchanged []entity.PersonalKeyRollingQuotaWarningState
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
		if !personalKeyRollingRetainedFactsEqual(before, after) {
			t.Fatal("held usage rearmed episode")
		}
	}
	if e := svc.StopCallRecorder(); e != nil {
		t.Fatal(e)
	}
	svc.StopRuntime()
	svc = makeService()
	if e := svc.StartRuntime(workerCtx); e != nil {
		t.Fatal(e)
	}
	if e := svc.StartCallRecorder(ctx, spool); e != nil {
		t.Fatal(e)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	reconcile()
	count(6)
	var after []entity.PersonalKeyRollingQuotaWarningObservation
	var afterInbox []entity.PersonalKeyRollingQuotaWarningInbox
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
