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

// testPersonalMonthlyQuotaWarningLifecycle runs against each supported database in
// the fresh-database harness. Warmup establishes real journal coverage before
// any notified resource is created; no historical accounting is manufactured.
func testPersonalMonthlyQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
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
	cipher, err := store.Seal("crd_quota_warning", "quota-warning-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_quota_warning"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_quota_warning", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_quota_warning", ProviderID: "prv_quota_warning", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_quota_warning", ConnectionID: "con_quota_warning", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_quota_warning", ConnectionID: "con_quota_warning", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_quota_warning", ProviderModelID: "pmd_quota_warning"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "quota-warning-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_quota_warning", ModelID: modelID, ProviderModelID: "pmd_quota_warning", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_warning_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_warning_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_quota_warning", ProviderModelID: "pmd_quota_warning", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_warning_%d", index), ModelPriceID: "price_quota_warning", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
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
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"quota-warning-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_quota_warning/reservation-bound"
	expectStatus(t, adminRequest("PUT", boundPath, map[string]any{"max_input_tokens": 1, "max_output_tokens": 1, "evidence": "Controlled native response capacity", "reason": "Personal warning acceptance"}, "0"), 200)
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// First use changes accounting activation; publish that real generation before
	// creating later resources or asking the observer for current-policy evidence.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "quota-warning-member@example.invalid", "monthly-notification-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "quota-warning-outsider@example.invalid", "monthly-notification-password", "Other member", "member")
	if err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-warning-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
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
	create(&entity.APIKey{ID: "key_warning_member", UserID: member.User.ID, Name: "Personal warning", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive})
	create(&entity.APIKeyModel{KeyID: "key_warning_member", ModelID: modelID})
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
	userPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	write := func(path string, values map[string]any) service.LimitRecord {
		t.Helper()
		current := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200)
		values["reason"] = "Personal monthly warning acceptance"
		return decodeCatalogResponse[service.LimitRecord](t, adminRequest("PUT", path, values, current.ETag), 200)
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
	count := func(owner string) int64 {
		t.Helper()
		var value int64
		if err := db.Model(&entity.QuotaWarningObservation{}).Where(database.ExactText(db, clause.Column{Name: "owner_id"}, owner)).Count(&value).Error; err != nil {
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
		id := fmt.Sprintf("usr_000_warn_%02d", index)
		create(&entity.User{ID: id, Email: fmt.Sprintf("warning-scan-%02d@example.invalid", index), Name: "Covered zero scan", Role: "member"})
		create(&entity.ResourceLimit{ScopeKind: "user", ScopeID: id, ETag: fmt.Sprintf("lim_warning_scan_%02d", index), ActorID: admin.User.ID, Reason: "Bounded fair warning scan", TokensMonth: &cap100, IPMode: "none", IPRangesJSON: "[]"})
	}
	refresh()
	policy := write(userPath, map[string]any{"tokens_month": 10})
	reconcile()
	assertCount(member.User.ID, 0)
	for index := range 3 {
		expectStatus(t, call(personalBearer), 200)
		flush()
		reconcile()
		assertCount(member.User.ID, 0)
		if index == 2 && dispatches.Load() != 4 {
			t.Fatal("unexpected dispatch before near level")
		}
	}
	expectStatus(t, call(personalBearer), 200)
	flush()
	// Settlement alone never sends an inbox warning through the gateway.
	assertCount(member.User.ID, 0)
	reconcile()
	assertCount(member.User.ID, 1)
	nearPage := page()
	if len(nearPage.Items) != 1 || nearPage.UnreadCount != 1 {
		t.Fatal("near warning missing from recipient inbox")
	}
	near := nearPage.Items[0]
	if near.QuotaWarning == nil || near.Quota != nil || near.Kind != "monthly_quota_warning" || near.Severity != "medium" || near.QuotaWarning.Level != "near" || near.QuotaWarning.Threshold != 80 || near.QuotaWarning.Settled != "8" || near.QuotaWarning.Limit != "10" || near.QuotaWarning.Currency != nil || near.QuotaWarning.PolicyRevision != policy.ETag || near.QuotaWarning.ScopeID != member.User.ID {
		t.Fatal("near snapshot was not exact")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("warning read was not recorded")
	}
	var readInbox entity.QuotaWarningInbox
	if err := db.Take(&readInbox, "id = ?", near.ID).Error; err != nil || readInbox.ReadAt == nil {
		t.Fatal("persisted read timestamp missing", err)
	}
	var original entity.QuotaWarningObservation
	if err := db.Take(&original, "id = ?", near.QuotaWarningObservationID).Error; err != nil {
		t.Fatal(err)
	}
	single.Store(true)
	expectStatus(t, call(personalBearer), 200)
	single.Store(false)
	flush()
	reconcile()
	reconcile()
	assertCount(member.User.ID, 2)
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
	// A first critical observation under a new revision creates only critical.
	write(userPath, map[string]any{"tokens_month": 10, "rpm": 100})
	reconcile()
	assertCount(member.User.ID, 3)
	// Independent money levels use already settled, known USD amounts. No money
	// reservation or fictitious earlier crossing is needed to produce these facts.
	moneyNear := write(userPath, map[string]any{"money_month": "11.25", "currency": "USD"})
	reconcile()
	assertCount(member.User.ID, 4)
	moneyCritical := write(userPath, map[string]any{"money_month": "10", "currency": "USD"})
	reconcile()
	assertCount(member.User.ID, 5)
	precise := write(userPath, map[string]any{"money_month": "10.000000000000000001", "currency": "USD"})
	reconcile()
	assertCount(member.User.ID, 6)
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
	refresh() // Publish the valid baseline before this deliberate unpublished mutation.
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	reconcileUnpublished()
	assertCount(member.User.ID, 6)
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).UpdateColumn("platform_currency", "USD").Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	assertCount(member.User.ID, 7)
	for _, item := range page().Items {
		if item.QuotaWarning != nil && item.QuotaWarning.PolicyRevision == unpublished.ETag && (item.QuotaWarning.Currency == nil || *item.QuotaWarning.Currency != "USD") {
			t.Fatal("denomination restoration changed historical currency")
		}
	}
	// Stale runtime policy cannot prove warning eligibility; exact restoration
	// leaves the original immutable records intact.
	var savedPolicy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).Take(&savedPolicy).Error; err != nil {
		t.Fatal(err)
	}
	refresh() // Publish the valid baseline before this deliberate unpublished mutation.
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).UpdateColumn("ETag", "lim_warning_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	var changedPolicy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).Take(&changedPolicy).Error; err != nil {
		t.Fatal(err)
	}
	expectedPolicy := savedPolicy
	expectedPolicy.ETag = "lim_warning_unpublished"
	if !reflect.DeepEqual(changedPolicy, expectedPolicy) {
		t.Fatal("unpublished revision did not persist as the sole policy change")
	}
	reconcileUnpublished()
	assertCount(member.User.ID, 7)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).UpdateColumn("ETag", savedPolicy.ETag).Error; err != nil {
		t.Fatal(err)
	}
	var restoredPolicy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).Take(&restoredPolicy).Error; err != nil || !reflect.DeepEqual(restoredPolicy, savedPolicy) {
		t.Fatal("exact stale-policy restoration changed retained policy", err)
	}
	// A different birth with the same retained ID must hide old private history.
	// One millisecond survives the existing default MySQL User datetime precision.
	var savedUser entity.User
	if err := db.Take(&savedUser, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	changedBirth := savedUser.CreatedAt.Add(time.Millisecond)
	refresh() // Publish the valid baseline before this deliberate unpublished mutation.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("created_at", changedBirth).Error; err != nil {
		t.Fatal(err)
	}
	var changedUser entity.User
	if err := db.Take(&changedUser, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	expectedUser := savedUser
	expectedUser.CreatedAt = changedBirth
	if changedUser.CreatedAt.Equal(savedUser.CreatedAt) || !changedUser.CreatedAt.Equal(changedBirth) || !reflect.DeepEqual(changedUser, expectedUser) {
		t.Fatal("different birth was not a genuinely persisted sole User change")
	}
	if len(page().Items) != 0 {
		t.Fatal("different birth exposed old warnings")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil), 404)
	reconcileUnpublished()
	assertCount(member.User.ID, 7)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("created_at", savedUser.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	var restoredUser entity.User
	if err := db.Take(&restoredUser, "id = ?", member.User.ID).Error; err != nil || !restoredUser.CreatedAt.Equal(savedUser.CreatedAt) || !reflect.DeepEqual(restoredUser, savedUser) {
		t.Fatal("exact User birth restoration changed retained facts", err)
	}
	if len(page().Items) != 7 {
		t.Fatal("exact birth restoration did not restore private history")
	}
	// NULL and zero retain existing exhaustion behavior, never threshold warnings.
	write(userPath, map[string]any{})
	reconcile()
	assertCount(member.User.ID, 7)
	write(userPath, map[string]any{"tokens_month": 0})
	reconcile()
	assertCount(member.User.ID, 7)
	write(userPath, map[string]any{"tokens_month": 9, "money_month": "9", "currency": "USD"})
	reconcile()
	assertCount(member.User.ID, 7)
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
	if warningCount != 7 || exhausted != 3 {
		t.Fatal("warning/exhaustion merge changed existing zero or >=100 semantics", warningCount, exhausted)
	}
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest("GET", "/api/v1/notifications?status=all", nil, ""), 200)
	for _, item := range adminPage.Items {
		if item.SubjectID == member.User.ID {
			t.Fatal("Personal warning/exhaustion fanned out to administrator")
		}
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+near.ID+"/read", nil, ""), 404)
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?recipient_id="+outsider.User.ID, nil), 400)

	// Exact corrupt history cannot borrow the current owner's rows, count or writes.
	aliasIDs := []string{}
	for _, kind := range []string{"owner", "recipient", "join", "birth"} {
		observation := original
		observation.ID = "qwo_warning_alias_" + kind
		observation.PolicyRevision = "lim_warning_alias_" + kind
		recipient, join := member.User.ID, observation.ID
		switch kind {
		case "owner":
			observation.OwnerID = strings.ToUpper(member.User.ID)
			if observation.OwnerID == member.User.ID {
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
		inboxID := "qwi_warning_alias_" + kind
		aliasIDs = append(aliasIDs, inboxID)
		create(&entity.QuotaWarningInbox{ID: inboxID, ObservationID: join, RecipientID: recipient, CreatedAt: observation.AsOf})
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
	var hiddenInboxes []entity.QuotaWarningInbox
	if err := db.Where("id IN ?", aliasIDs).Find(&hiddenInboxes).Error; err != nil || len(hiddenInboxes) != 4 {
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
	assertCount(member.User.ID, 10) //7 original+3 canonical-owner corrupt histories.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	// Unknown usage and a live hold require separate real completed native calls.
	makeTarget := func(label, bearer string) (string, string) {
		t.Helper()
		record, err := svc.CreateMember(ctx, admin.User.ID, "warning-"+label+"@example.invalid", "monthly-notification-password", "Warning "+label, "member")
		if err != nil {
			t.Fatal(err)
		}
		keyID := "key_warning_" + label
		create(&entity.UserModelGrant{UserID: record.User.ID, ModelID: modelID})
		create(&entity.APIKey{ID: keyID, UserID: record.User.ID, Name: label, Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive})
		create(&entity.APIKeyModel{KeyID: keyID, ModelID: modelID})
		refresh()
		return record.User.ID, "/api/v1/admin/members/" + record.User.ID + "/limits"
	}
	unknownBearer := "rx_" + strings.Repeat("u", 43)
	unknownID, unknownPath := makeTarget("unknown", unknownBearer)
	write(unknownPath, map[string]any{"tokens_month": 2})
	unknown.Store(true)
	expectStatus(t, call(unknownBearer), 200)
	unknown.Store(false)
	flush()
	write(unknownPath, map[string]any{"tokens_month": 1, "money_month": "0.000000000000000001", "currency": "USD"})
	reconcile()
	assertCount(unknownID, 0)
	unknownUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", unknownPath, nil, ""), 200).QuotaUsage
	// Admission had a finite token cap but no money cap: missing actual usage
	// retains the two-token bound and one unknown-money receipt. The later money
	// policy cannot retroactively add a monetary bound or form a warning.
	if unknownUsage == nil {
		t.Fatal("missing native usage quota projection unavailable")
	}
	if unknownUsage.Month == nil || !unknownUsage.Month.Covered || unknownUsage.Month.TokensUsed != 0 || unknownUsage.Month.TokensHeld != 2 || unknownUsage.Month.TokensUnknown != 0 || len(unknownUsage.Month.MoneyUsed) != 0 || len(unknownUsage.Month.MoneyHeld) != 0 || unknownUsage.Month.MoneyUnknown != 1 || unknownUsage.Active == nil || unknownUsage.Active.TokensHeld != 0 || len(unknownUsage.Active.MoneyHeld) != 0 || unknownUsage.Active.TokensUnknown != 0 || unknownUsage.Active.MoneyUnknown != 0 {
		t.Fatalf("missing native usage terminal bound mismatch: month=%+v active=%+v", unknownUsage.Month, unknownUsage.Active)
	}
	heldBearer := "rx_" + strings.Repeat("h", 43)
	heldID, heldPath := makeTarget("held", heldBearer)
	write(heldPath, map[string]any{"tokens_month": 2})
	hold.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() { heldResponse <- call(heldBearer) }()
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
	var finalOriginal entity.QuotaWarningObservation
	if err := db.Take(&finalOriginal, "id = ?", original.ID).Error; err != nil || !reflect.DeepEqual(finalOriginal, original) {
		t.Fatal("policy/level/replay changed original observation", err)
	}
	var native []entity.CallRecord
	if err := db.Where("key_id IN ?", []string{"key_warning_member", "key_warning_unknown", "key_warning_held", "key_quota_warning_admin"}).Order("request_id").Find(&native).Error; err != nil {
		t.Fatal(err)
	}
	if dispatches.Load() != 8 || len(native) != 8 {
		t.Fatal("unexpected native dispatch/durable call count", dispatches.Load(), len(native))
	}
	attemptFacts := map[string]entity.CallAttempt{}
	for _, fact := range native {
		var attempts []entity.CallAttempt
		if err := db.Where("request_id = ?", fact.RequestID).Find(&attempts).Error; err != nil {
			t.Fatal(err)
		}
		if fact.ProjectID != "" || fact.TeamID != "" || fact.TeamMembershipID != "" || fact.Status != "success" || len(attempts) != 1 || attempts[0].CredentialID != "crd_quota_warning" || attempts[0].SnapshotID == "" || attempts[0].NativeCompletionEvidence != "completed" {
			t.Fatal("native attribution/completion changed")
		}
		attemptFacts[attempts[0].ID] = attempts[0]
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("Personal warnings created operational/SMTP fanout", err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := svc.ReconcileMonthlyQuotaNotifications(canceled); err == nil {
		t.Fatal("canceled reconciliation ignored cancellation")
	}
	assertCount(member.User.ID, 10)
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
	var afterNative []entity.CallRecord
	if err := db.Where("key_id IN ?", []string{"key_warning_member", "key_warning_unknown", "key_warning_held", "key_quota_warning_admin"}).Order("request_id").Find(&afterNative).Error; err != nil || !reflect.DeepEqual(afterNative, native) {
		t.Fatal("restart changed immutable call facts", err)
	}
	for id, before := range attemptFacts {
		var after entity.CallAttempt
		if err := db.Take(&after, "id = ?", id).Error; err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("restart changed immutable attempt facts", err)
		}
	}
	if !reflect.DeepEqual(afterRestart, beforeRestart) || dispatches.Load() != 8 {
		t.Fatal("restart altered recipient history/read state or replayed inference")
	}
}

// These are genuine parser-native completions; lack of usage remains unknown.
func quotaWarningNativeBody(single, unknown bool) string {
	if unknown {
		return `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Unknown usage"},"finish_reason":"stop"}]}`
	}
	output := 1
	if single {
		output = 0
	}
	return fmt.Sprintf(`{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":%d,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`, output)
}
func TestQuotaWarningNativeFixtures(t *testing.T) {
	for _, tt := range []struct {
		name            string
		single, unknown bool
		output          int64
	}{{"known_two", false, false, 1}, {"known_one", true, false, 0}, {"unknown_usage", false, true, 0}} {
		t.Run(tt.name, func(t *testing.T) {
			observed := parseGatewayUsage([]byte(quotaWarningNativeBody(tt.single, tt.unknown)))
			if observed.NativeCompletionEvidence != "completed" {
				t.Fatal("controlled response lacks native completion")
			}
			if tt.unknown {
				if observed.Complete || observed.Input != nil || observed.Output != nil {
					t.Fatal("unknown controlled usage fabricated known counters")
				}
				return
			}
			if !observed.Complete || observed.Input == nil || *observed.Input != 1 || observed.Output == nil || *observed.Output != tt.output {
				t.Fatal("controlled exact usage was not authoritative")
			}
		})
	}
}
