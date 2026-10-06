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

// testMonthlyQuotaNotificationLifecycle runs against each supported database in
// the fresh-database harness. Warmup establishes real journal coverage before
// any notified resource is created; no historical accounting is manufactured.
func testMonthlyQuotaNotificationLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	// Reuse the proven fixture-owned publisher fence; ordinary API/observer reads
	// remain unmarked and each positive observation runs exactly once.
	var publicationBarrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &publicationBarrier)
	const publicationCallback = "test:monthly-quota-notification-positive-publication"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationCallback, publicationBarrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		publicationBarrier.armed.Store(false)
		if err := db.Callback().Query().Remove(publicationCallback); err != nil {
			t.Error(err)
		}
	}()
	store, err := secretstore.New(bytes.Repeat([]byte{97}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var unknown atomic.Bool
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if hold.Load() {
			entered <- struct{}{}
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Unknown usage"},"finish_reason":"stop"}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
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
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"quota-notification-admin@example.invalid","password":"monthly-notification-password","name":"Quota admin"}`, nil, "")
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
	cipher, err := store.Seal("crd_quota_notice", "quota-notice-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	modelID := "mdl_quota_notice"
	adminBearer := "rx_" + strings.Repeat("a", 43)
	for _, row := range []any{
		&entity.Provider{ID: "prv_quota_notice", Name: "Quota notification provider"},
		&entity.ProviderConnection{ID: "con_quota_notice", ProviderID: "prv_quota_notice", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_quota_notice", ConnectionID: "con_quota_notice", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_quota_notice", ConnectionID: "con_quota_notice", UpstreamName: "quota-native"},
		&entity.CredentialModelAccess{CredentialID: "crd_quota_notice", ProviderModelID: "pmd_quota_notice"},
		&entity.Model{ID: modelID, Status: "active"}, &entity.ModelName{Name: "quota-notice-model", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_quota_notice", ModelID: modelID, ProviderModelID: "pmd_quota_notice", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_quota_notice_admin", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(adminBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_quota_notice_admin", ModelID: modelID},
		&entity.ModelPrice{ID: "price_quota_notice", ProviderModelID: "pmd_quota_notice", UpdateSource: "api"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		if err := db.Create(&entity.PriceRate{ID: fmt.Sprintf("rate_notice_%d", index), ModelPriceID: "price_quota_notice", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
	}
	spool := filepath.Join(t.TempDir(), "quota-notifications.db")
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder() }()
	call := func(bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"quota-notice-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":10}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	boundPath := "/api/v1/admin/provider-models/pmd_quota_notice/reservation-bound"
	expectStatus(t, adminRequest("PUT", boundPath, map[string]any{"max_input_tokens": 10, "max_output_tokens": 10, "evidence": "Controlled native response capacity", "reason": "Monthly notification acceptance"}, "0"), 200)
	expectStatus(t, call(adminBearer), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	// First use changes accounting activation; publish that real generation before
	// creating later resources or asking the observer for current-policy evidence.
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "quota-notice-member@example.invalid", "monthly-notification-password", "Quota member", "member")
	if err != nil {
		t.Fatal(err)
	}
	outsider, err := svc.CreateMember(ctx, admin.User.ID, "quota-notice-outsider@example.invalid", "monthly-notification-password", "Other member", "member")
	if err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-notice-member@example.invalid","password":"monthly-notification-password"}`, nil, "")
	memberAuth, memberCookie := readIdentity(t, login)
	memberRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return request(memberCookie, memberAuth.CSRFToken, method, path, body, "")
	}
	personalBearer := "rx_" + strings.Repeat("m", 43)
	projectBearer := "rxp_" + strings.Repeat("p", 43)
	for _, row := range []any{
		&entity.UserModelGrant{UserID: member.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_notice_member", UserID: member.User.ID, Name: "Personal", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(personalBearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_notice_member", ModelID: modelID},
		&entity.Project{ID: "prj_quota_notice", Name: "Frozen quota Project", CreatorID: admin.User.ID, Status: entity.ResourceActive},
		&entity.ProjectManager{ID: "pmg_quota_notice", ProjectID: "prj_quota_notice", UserID: member.User.ID},
		&entity.ProjectModelGrant{ProjectID: "prj_quota_notice", ModelID: modelID},
		&entity.ProjectKey{ID: "pky_quota_notice", ProjectID: "prj_quota_notice", CreatorID: admin.User.ID, Name: "Project", Prefix: "rxp_masked", TokenHash: secret.SHA256Hex(projectBearer), Status: entity.KeyActive, DeliveryMode: "manual"},
		&entity.ProjectKeyModel{KeyID: "pky_quota_notice", ModelID: modelID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	empty := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications", nil), 200)
	if len(empty.Items) != 0 || empty.UnreadCount != 0 {
		t.Fatal("ordinary member inbox was not empty")
	}
	expectStatus(t, memberRequest("GET", "/api/v1/notification-settings", nil), 403)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	userPath := "/api/v1/admin/members/" + member.User.ID + "/limits"
	projectPath := "/api/v1/projects/prj_quota_notice/limits"
	write := func(path string, values map[string]any) service.LimitRecord {
		current := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200)
		values["reason"] = "Monthly quota notification acceptance"
		return decodeCatalogResponse[service.LimitRecord](t, adminRequest("PUT", path, values, current.ETag), 200)
	}
	reconcile := func() {
		t.Helper()
		if err := publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }); err != nil {
			t.Fatal(err)
		}
	}
	personalPolicy := write(userPath, map[string]any{"tokens_month": 20})
	write(projectPath, map[string]any{"tokens_month": 20})
	// A hold consumes admission capacity but cannot fabricate settled exhaustion.
	hold.Store(true)
	heldResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() { heldResponse <- call(projectBearer) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("held native request never dispatched")
	}
	reconcile()
	page := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications", nil), 200)
	if len(page.Items) != 0 {
		t.Fatal("outstanding reservation created an exhaustion observation")
	}
	close(release)
	hold.Store(false)
	expectStatus(t, <-heldResponse, 200)
	expectStatus(t, call(personalBearer), 200)
	expectStatus(t, call(personalBearer), 429)
	for _, path := range []string{userPath, projectPath} {
		usage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", path, nil, ""), 200).QuotaUsage
		if usage == nil || usage.Month == nil || usage.Month.TokensUsed != 20 || usage.Month.TokensUnknown != 0 || !usage.Month.Covered {
			t.Fatalf("native settled usage was not authoritative before observation: %+v", usage)
		}
	}
	reconcile()
	reconcile()
	page = decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=unread", nil), 200)
	if len(page.Items) != 2 || page.UnreadCount != 2 {
		var observations []entity.QuotaNotificationObservation
		var inboxes []entity.QuotaNotificationInbox
		_ = db.Find(&observations).Error
		_ = db.Find(&inboxes).Error
		personalUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", userPath, nil, ""), 200)
		projectUsage := decodeCatalogResponse[service.LimitRecord](t, adminRequest("GET", projectPath, nil, ""), 200)
		t.Fatalf("settled owner/Project observations missing: page=%+v observations=%+v inboxes=%+v personal=%+v project=%+v", page, observations, inboxes, personalUsage.QuotaUsage, projectUsage.QuotaUsage)
	}
	var personal, project service.NotificationRecord
	for _, record := range page.Items {
		if record.Quota == nil || record.AlertID != "" || record.DeliveryStatus != "" || record.Quota.Currency != nil || record.Quota.Limit != "20" || record.Quota.Settled != "20" || record.Quota.PolicyRevision == "" || record.Quota.TimeZone != "UTC" {
			t.Fatalf("quota snapshot not exact: %+v", record)
		}
		if record.Quota.ScopeKind == "user" {
			personal = record
		} else {
			project = record
		}
	}
	if personal.Quota.ScopeID != member.User.ID || personal.Quota.PolicyRevision != personalPolicy.ETag || project.Quota.ScopeID != "prj_quota_notice" || project.SubjectName != "Frozen quota Project" {
		t.Fatal("observation inherited a creator or mutable scope")
	}
	adminPage := decodeCatalogResponse[service.NotificationPage](t, adminRequest("GET", "/api/v1/notifications", nil, ""), 200)
	for _, record := range adminPage.Items {
		if record.Quota != nil {
			t.Fatal("system operator/Project creator received quota recipient fanout")
		}
	}
	expectStatus(t, adminRequest("POST", "/api/v1/notifications/"+project.ID+"/read", nil, ""), 404)
	expectStatus(t, memberRequest("GET", "/api/v1/notifications?recipient_id="+outsider.User.ID, nil), 400)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/notifications/"+personal.ID+"/read", "", memberCookie, ""), 403)
	decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+personal.ID+"/read", nil), 200)
	decodeCatalogResponse[service.NotificationRecord](t, memberRequest("POST", "/api/v1/notifications/"+personal.ID+"/read", nil), 200)
	reconcile()
	if err := db.Model(&entity.Project{}).Where("id = ?", "prj_quota_notice").Update("name", "Renamed Project").Error; err != nil {
		t.Fatal(err)
	}
	afterRead := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	if afterRead.UnreadCount != 1 {
		t.Fatal("observer replay reset read state")
	}
	for _, record := range afterRead.Items {
		if record.ID == project.ID && record.SubjectName != "Frozen quota Project" {
			t.Fatal("inbox replaced frozen Project name")
		}
	}
	// Removal hides a recorded Project recipient for list, count and both writes.
	if err := db.Delete(&entity.ProjectManager{}, "id = ?", "pmg_quota_notice").Error; err != nil {
		t.Fatal(err)
	}
	removed := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	if len(removed.Items) != 1 || removed.UnreadCount != 0 {
		t.Fatal("removed manager retained Project inbox access")
	}
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/"+project.ID+"/read", nil), 404)
	expectStatus(t, memberRequest("POST", "/api/v1/notifications/read-all", nil), 204)
	var projectInbox entity.QuotaNotificationInbox
	if err := db.First(&projectInbox, "id = ?", project.ID).Error; err != nil || projectInbox.ReadAt != nil {
		t.Fatal("removed manager read-all changed inaccessible historical recipient")
	}
	if err := db.Create(&entity.ProjectManager{ID: "pmg_notice_restored", ProjectID: "prj_quota_notice", UserID: member.User.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pmg_notice_new", ProjectID: "prj_quota_notice", UserID: outsider.User.ID}).Error; err != nil {
		t.Fatal(err)
	}
	reconcile()
	var outsiderInbox int64
	if err := db.Model(&entity.QuotaNotificationInbox{}).Where("recipient_id = ?", outsider.User.ID).Count(&outsiderInbox).Error; err != nil || outsiderInbox != 0 {
		t.Fatal("replay added a newly appointed manager")
	}
	// Current policy revisions create new immutable observations without changing
	// the original observation or claiming the old threshold was crossed again.
	write(userPath, map[string]any{"tokens_month": 0})
	reconcile()
	var personalObservations int64
	if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_kind = ? AND scope_id = ?", "user", member.User.ID).Count(&personalObservations).Error; err != nil || personalObservations != 2 {
		t.Fatal("new finite zero policy lost independent dedup identity")
	}
	write(userPath, map[string]any{"money_month": "20.000000000000000001", "currency": "USD"})
	reconcile()
	var moneyCount int64
	if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_id = ? AND dimension = ?", member.User.ID, "money").Count(&moneyCount).Error; err != nil || moneyCount != 0 {
		t.Fatal("money comparison rounded across the exact decimal limit")
	}
	write(userPath, map[string]any{"money_month": "20", "currency": "USD"})
	reconcile()
	moneyPage := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	foundMoney := false
	for _, record := range moneyPage.Items {
		if record.Quota != nil && record.Quota.Dimension == "money" {
			foundMoney = true
			if record.Quota.Currency == nil || *record.Quota.Currency != "USD" || record.Quota.Settled != "20" {
				t.Fatalf("money observation changed authoritative denomination: %+v", record)
			}
		}
	}
	if !foundMoney {
		t.Fatal("known exact settled money was not observed")
	}
	// Unknown usage suppresses even a zero finite policy; null unlimited never
	// produces an observation. The outsider has no Project Key accounting.
	otherBearer := "rx_" + strings.Repeat("o", 43)
	for _, row := range []any{&entity.UserModelGrant{UserID: outsider.User.ID, ModelID: modelID}, &entity.APIKey{ID: "key_notice_other", UserID: outsider.User.ID, Name: "Unknown", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(otherBearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_notice_other", ModelID: modelID}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	unknown.Store(true)
	expectStatus(t, call(otherBearer), 200)
	unknown.Store(false)
	otherPath := "/api/v1/admin/members/" + outsider.User.ID + "/limits"
	write(otherPath, map[string]any{"tokens_month": 0, "money_month": "0", "currency": "USD"})
	reconcile()
	if err := db.Model(&entity.QuotaNotificationInbox{}).Where("recipient_id = ?", outsider.User.ID).Count(&outsiderInbox).Error; err != nil || outsiderInbox != 0 {
		t.Fatal("unknown settlement was replaced by zero usage")
	}

	// Quota projection must never reuse operational fanout or create SMTP work.
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var count int64
		if err := db.Model(model).Where("kind = ?", "monthly_quota_exhausted").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("quota observation entered operational delivery %T", model)
		}
	}
	testQuotaMixedNotificationSources(t, db, member.User.ID, memberRequest, func(method, path string, body any) *httptest.ResponseRecorder {
		return adminRequest(method, path, body, "")
	})
	testQuotaInboxAliasesAndPagination(t, db, svc, member.User.ID, memberRequest)
	// SQL rows and read state survive recorder/runtime restart without a new call.
	if err := svc.FlushCallRecorder(ctx); err != nil {
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
	reconcile()
	restored := decodeCatalogResponse[service.NotificationPage](t, memberRequest("GET", "/api/v1/notifications?status=all", nil), 200)
	for _, record := range restored.Items {
		if record.ID == personal.ID && !record.Read {
			t.Fatal("restart reset historical read state")
		}
	}
	// A single pool connection cannot be held while another helper waits for it.
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	originalMax := pool.Stats().MaxOpenConnections
	pool.SetMaxOpenConns(1)
	defer pool.SetMaxOpenConns(originalMax)
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wait sync.WaitGroup
	failures := make(chan error, 2)
	wait.Go(func() {
		_, err := svc.ListNotifications(deadline, member.User.ID, service.NotificationFilter{})
		failures <- err
	})
	wait.Go(func() {
		failures <- publicationBarrier.observePublished(func() error { return svc.RefreshRuntime(deadline) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(deadline) })
	})
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testQuotaInboxAliasesAndPagination(t *testing.T, db *gorm.DB, svc *service.Service, actor string, request func(string, string, any) *httptest.ResponseRecorder) {
	t.Helper()
	before := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all", nil), 200)
	now := time.Now().UTC().Truncate(time.Microsecond)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	makeObservation := func(suffix, scopeID string) entity.QuotaNotificationObservation {
		return entity.QuotaNotificationObservation{ID: "qob_alias_" + suffix, ScopeKind: "user", ScopeID: scopeID, Dimension: "tokens", PolicyRevision: "rev_alias_" + suffix, MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: now, Limit: "0", Settled: "0", CoverageStart: now, ResourceCreatedAt: now}
	}
	aliases := []struct{ suffix, scope, recipient string }{{"scope", strings.ToUpper(actor), actor}, {"recipient", actor, strings.ToUpper(actor)}}
	for _, alias := range aliases {
		observation := makeObservation(alias.suffix, alias.scope)
		if err := db.Create(&observation).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.QuotaNotificationInbox{ID: "qni_alias_" + alias.suffix, ObservationID: observation.ID, RecipientID: alias.recipient, CreatedAt: now}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A case-folded observation join must not turn a raw alias into authority.
	valid := makeObservation("join", actor)
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.QuotaNotificationInbox{ID: "qni_alias_join", ObservationID: strings.ToUpper(valid.ID), RecipientID: actor, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	after := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all", nil), 200)
	if len(after.Items) != len(before.Items) || after.UnreadCount != before.UnreadCount {
		t.Fatal("case-folded scope/recipient/join polluted visible rows or counts")
	}
	for _, suffix := range []string{"scope", "recipient", "join"} {
		expectStatus(t, request("POST", "/api/v1/notifications/qni_alias_"+suffix+"/read", nil), 404)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		path := "/api/v1/notifications?status=all&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := decodeCatalogResponse[service.NotificationPage](t, request("GET", path, nil), 200)
		if page.UnreadCount != before.UnreadCount {
			t.Fatal("pagination changed scoped unread count")
		}
		for _, row := range page.Items {
			if seen[row.ID] || strings.Contains(row.ID, "alias") {
				t.Fatal("pagination repeated or leaked an unauthorized alias")
			}
			seen[row.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(before.Items) {
		t.Fatal("hidden aliases consumed cursor slots")
	}
	expectStatus(t, request("POST", "/api/v1/notifications/read-all", nil), 204)
	var changed int64
	if err := db.Model(&entity.QuotaNotificationInbox{}).Where("id IN ? AND read_at IS NOT NULL", []string{"qni_alias_scope", "qni_alias_recipient", "qni_alias_join"}).Count(&changed).Error; err != nil || changed != 0 {
		t.Fatal("read-all changed an aliased inaccessible row")
	}
	if _, err := svc.ListNotifications(context.Background(), strings.ToUpper(actor), service.NotificationFilter{}); err == nil {
		t.Fatal("case-folded actor identity was accepted")
	}
}

func testQuotaMixedNotificationSources(t *testing.T, db *gorm.DB, actor string, request, adminRequest func(string, string, any) *httptest.ResponseRecorder) {
	t.Helper()
	for _, row := range []any{
		&entity.Role{ID: "rol_quota_inbox_ops", Name: "Inbox operations", NameKey: "quota-inbox-ops"},
		&entity.RolePermission{RoleID: "rol_quota_inbox_ops", Permission: "system.read"},
		&entity.UserRole{UserID: actor, RoleID: "rol_quota_inbox_ops"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	completed := time.Now().UTC().Add(-time.Second).Truncate(time.Microsecond)
	job := entity.SystemJob{ID: "job_quota_inbox_ops", Code: service.SystemJobRuntimePublication, Status: "failed", ItemsTotal: 1, DetailCode: "publication_failed", StartedAt: completed, UpdatedAt: completed, CompletedAt: &completed}
	if err := db.Create(&job).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, adminRequest("GET", "/api/v1/admin/system/jobs", nil), 200)
	all := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all", nil), 200)
	var operational service.NotificationRecord
	for _, record := range all.Items {
		if record.Quota == nil && record.Kind == "system_job_failure" {
			operational = record
		}
	}
	if operational.ID == "" {
		t.Fatalf("operational recipient was not merged with quota source: %+v", all)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		path := "/api/v1/notifications?status=all&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := decodeCatalogResponse[service.NotificationPage](t, request("GET", path, nil), 200)
		if page.UnreadCount != all.UnreadCount {
			t.Fatal("mixed-source pagination changed unread count")
		}
		for _, record := range page.Items {
			if seen[record.ID] {
				t.Fatal("mixed-source cursor repeated row")
			}
			seen[record.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(all.Items) {
		t.Fatal("mixed-source pagination omitted records")
	}
	if err := db.Delete(&entity.UserRole{}, "user_id = ? AND role_id = ?", actor, "rol_quota_inbox_ops").Error; err != nil {
		t.Fatal(err)
	}
	// A warning has its own typed snapshot; absence of exhaustion metadata no
	// longer identifies an operational notification. Accept only this actor's
	// explicit Personal warning shape while retaining operational privacy gates.
	personalWarning := func(record service.NotificationRecord) bool {
		warning := record.QuotaWarning
		return record.Kind == "monthly_quota_warning" && record.Quota == nil && warning != nil &&
			warning.ScopeKind == "user" && warning.ScopeID == actor &&
			record.SubjectType == "user" && record.SubjectID == actor &&
			warning.ThresholdGeneration == "personal-monthly-80-90-v1" &&
			((warning.Level == "near" && warning.Threshold == 80 && record.Severity == "medium") ||
				(warning.Level == "critical" && warning.Threshold == 90 && record.Severity == "high"))
	}
	revoked := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all", nil), 200)
	for _, record := range revoked.Items {
		if record.Quota == nil && !personalWarning(record) {
			t.Fatal("revoked system.read still exposed operational history")
		}
	}
	expectStatus(t, request("POST", "/api/v1/notifications/"+operational.ID+"/read", nil), 404)
	expectStatus(t, request("POST", "/api/v1/notifications/read-all", nil), 204)
	var unchanged entity.Notification
	if err := db.First(&unchanged, "id = ?", operational.ID).Error; err != nil || unchanged.Read {
		t.Fatal("quota-only read-all changed inaccessible operational row")
	}
	for _, alias := range []entity.UserRole{
		{UserID: strings.ToUpper(actor), RoleID: "rol_quota_inbox_ops"},
		{UserID: actor, RoleID: "ROL_QUOTA_INBOX_OPS"},
	} {
		err := db.Create(&alias).Error
		if err != nil {
			if !errors.Is(err, gorm.ErrForeignKeyViolated) {
				t.Fatal(err)
			}
			t.Log("database rejected an aliased operational role binding")
			continue
		}
		hidden := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all", nil), 200)
		for _, record := range hidden.Items {
			if record.Quota == nil && !personalWarning(record) {
				t.Fatal("aliased role assignment granted operational history")
			}
		}
		if hidden.UnreadCount != 0 {
			t.Fatal("aliased role assignment polluted unread count")
		}
		expectStatus(t, request("POST", "/api/v1/notifications/"+operational.ID+"/read", nil), 404)
		expectStatus(t, request("POST", "/api/v1/notifications/read-all", nil), 204)
		if err := db.First(&unchanged, "id = ?", operational.ID).Error; err != nil || unchanged.Read {
			t.Fatal("aliased operator read-all changed inaccessible row")
		}
		if err := db.Delete(&alias).Error; err != nil {
			t.Fatal(err)
		}
	}
}
