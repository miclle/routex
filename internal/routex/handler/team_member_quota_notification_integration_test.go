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

func testTeamMemberQuotaNotificationLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{117}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int32
	var held, unknown, truncated atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatches.Add(1)
		if held.Load() {
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
		if truncated.Load() {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Interrupted\"},\"finish_reason\":null}]}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if unknown.Load() {
			_, _ = io.WriteString(w, teamMemberNoticeNativeBody(true))
			return
		}
		_, _ = io.WriteString(w, teamMemberNoticeNativeBody(false))
	}))
	defer upstream.Close()
	var failInbox atomic.Bool
	const failureCallback = "team_member_notice_fixture_rollback"
	if err := db.Callback().Create().Before("gorm:create").Register(failureCallback, func(tx *gorm.DB) {
		if failInbox.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "quota_notification_inboxes" {
			_ = tx.AddError(errors.New("controlled self inbox insert failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(failureCallback); err != nil {
			t.Error(err)
		}
	}()
	var instances []*service.Service
	defer func() {
		unblock()
		for _, instance := range instances {
			instance.StopRuntime()
			_ = instance.StopCallRecorder()
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		instance, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, instance)
		return instance
	}
	svc := makeService()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-notice-admin@example.invalid","password":"team-notification-password","name":"Notification administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatalf("seed %T: %v", row, err)
			}
		}
	}
	request := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("X-CSRF-Token", csrf)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	modelID := "mdl_team_notice"
	bearer := "rx_" + strings.Repeat("n", 43)
	cipher, err := store.Seal("crd_team_notice", "disposable-notification-upstream")
	if err != nil {
		t.Fatal(err)
	}
	create(
		&entity.Provider{ID: "prv_team_notice", Name: "Notification provider"},
		&entity.ProviderConnection{ID: "con_team_notice", ProviderID: "prv_team_notice", Name: "Native Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_notice", ConnectionID: "con_team_notice", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_notice", ConnectionID: "con_team_notice", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_notice", ProviderModelID: "pmd_team_notice"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-notice-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_team_notice", ModelID: modelID, ProviderModelID: "pmd_team_notice", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_team_notice_warm", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive},
		&entity.APIKeyModel{KeyID: "key_team_notice_warm", ModelID: modelID},
		&entity.ModelPrice{ID: "price_team_notice", ProviderModelID: "pmd_team_notice", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_team_notice", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bnd_team_notice", Evidence: "Controlled native maximum", Reason: "Team notice acceptance"},
	)
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: fmt.Sprintf("rate_team_notice_%d", index), ModelPriceID: "price_team_notice", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	// This Team predates actual journal activation, so zero usage lacks coverage.
	create(&entity.Team{ID: "tea_notice_uncovered", Name: "Before coverage", Status: entity.ResourceActive, CreatedAt: time.Now().UTC().Add(-time.Hour)})
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime() // Explicit publication keeps stale-generation assertions deterministic.
	spool := filepath.Join(t.TempDir(), "team-notifications.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"team-notice-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmup.Header.Set("Content-Type", "application/json")
	warmupResponse := httptest.NewRecorder()
	router.ServeHTTP(warmupResponse, warmup)
	expectStatus(t, warmupResponse, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	users := map[string]entity.User{}
	for _, name := range []string{"owner", "caller", "peer", "disabled", "inactive", "offboarded", "operator", "new"} {
		member, err := svc.CreateMember(ctx, admin.User.ID, "team-notice-"+name+"@example.invalid", "team-notification-password", "Notice "+name, entity.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		users[name] = member.User
	}
	offboardedAt := time.Now().UTC()
	if err := db.Model(&entity.User{}).Where("id = ?", users["disabled"].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users["offboarded"].ID).Update("offboarded_at", offboardedAt).Error; err != nil {
		t.Fatal(err)
	}
	create(&entity.Role{ID: "rol_team_notice_ops", Name: "Notification operator", NameKey: "team-notice-operator"}, &entity.RolePermission{RoleID: "rol_team_notice_ops", Permission: "system.read"}, &entity.UserRole{UserID: users["operator"].ID, RoleID: "rol_team_notice_ops"})
	teamID := "tea_notice_main"
	create(&entity.Team{ID: teamID, Name: "Frozen Team notice name", Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: teamID, ModelID: modelID})
	for _, name := range []string{"owner", "caller", "peer", "disabled", "inactive", "offboarded"} {
		role, status := entity.TeamMember, entity.ResourceActive
		if name == "owner" {
			role = entity.TeamOwner
		}
		if name == "inactive" {
			status = entity.ResourceDisabled
		}
		create(&entity.TeamMembership{ID: "tmm_notice_" + name, TeamID: teamID, UserID: users[name].ID, Role: role, Status: status})
	}
	for _, suffix := range []string{"unknown", "truncated", "stale", "inactive"} {
		id := "tea_notice_" + suffix
		create(&entity.Team{ID: id, Name: "Suppressed " + suffix, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_notice_" + suffix + "_own", TeamID: id, UserID: users["owner"].ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_notice_" + suffix + "_call", TeamID: id, UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive}, &entity.TeamModelGrant{TeamID: id, ModelID: modelID})
	}
	create(&entity.TeamMembership{ID: "tmm_notice_uncovered", TeamID: "tea_notice_uncovered", UserID: users["owner"].ID, Role: entity.TeamOwner, Status: entity.ResourceActive})
	// Raw aliases may be rejected by PostgreSQL FK checks. If persisted under a
	// case-insensitive schema, neither may become a Team recipient.
	for _, row := range []*entity.TeamMembership{
		{ID: "tmm_notice_alias_user", TeamID: teamID, UserID: strings.ToUpper(users["operator"].ID), Role: entity.TeamMember, Status: entity.ResourceActive},
		{ID: "tmm_notice_alias_team", TeamID: strings.ToUpper(teamID), UserID: users["new"].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
	} {
		if err := db.Create(row).Error; err != nil && !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("alias seed", err)
		}
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	type identity struct {
		csrf   string
		cookie *http.Cookie
	}
	identities := map[string]identity{}
	for _, name := range []string{"owner", "caller", "peer", "operator", "new"} {
		auth, cookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-notice-`+name+`@example.invalid","password":"team-notification-password"}`, nil, ""))
		identities[name] = identity{auth.CSRFToken, cookie}
	}
	actorRequest := func(name, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		actor := identities[name]
		return request(actor.cookie, actor.csrf, method, path, nil)
	}
	page := func(name string) service.NotificationPage {
		t.Helper()
		return decodeCatalogResponse[service.NotificationPage](t, actorRequest(name, "GET", "/api/v1/notifications?status=all"), 200)
	}
	readLimit := func(team, user string) *service.LimitRecord {
		t.Helper()
		value, err := svc.GetTeamResourceLimit(ctx, admin.User.ID, team, user)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	writeLimit := func(team, user, patch string) *service.LimitRecord {
		t.Helper()
		var input service.TeamLimitInput
		if err := json.Unmarshal([]byte(patch), &input); err != nil {
			t.Fatal(err)
		}
		value, err := svc.SetTeamResourceLimit(ctx, admin.User.ID, team, user, readLimit(team, user).ETag, input)
		if err != nil || !value.Enforced {
			t.Fatal("Team policy was not applied", value, err)
		}
		return value
	}
	native := func(name, team string, stream bool) *httptest.ResponseRecorder {
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		payload := body
		if stream {
			payload = strings.TrimSuffix(body, "}") + `,"stream":true,"stream_options":{"include_usage":true}}`
		}
		req := httptest.NewRequestWithContext(callCtx, "POST", "http://routex.test/api/v1/teams/"+team+"/chat/completions", strings.NewReader(payload))
		req.AddCookie(identities[name].cookie)
		req.Header.Set("X-CSRF-Token", identities[name].csrf)
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	reconcileSavedSnapshot := func() {
		t.Helper()
		if err := svc.ReconcileMonthlyQuotaNotifications(ctx); err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() {
		t.Helper()
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Fatal(err)
		}
		reconcileSavedSnapshot()
	}
	count := func(team string) int64 {
		t.Helper()
		var value int64
		if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_kind = ? AND team_id = ?", "team_member", team).Count(&value).Error; err != nil {
			t.Fatal(err)
		}
		return value
	}
	quotaJSON := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal("encode quota failure context", err)
		}
		return string(raw)
	}
	// Parent remains below its cap while the caller's own stored child cap
	// exhausts. Four independently rounded components need two decimal units
	// above settlement when budgeting the initial money reservation.
	writeLimit(teamID, "", `{"tokens_month":100,"money_month":"100","currency":"USD","reason":"Separate aggregate budget"}`)
	writeLimit(teamID, users["caller"].ID, `{"tokens_month":5,"money_month":"5.000000000000000002","currency":"USD","reason":"Budget exact child admission"}`)
	writeLimit(teamID, users["peer"].ID, `{"tokens_month":20,"money_month":"20","currency":"USD","reason":"Independent peer budget"}`)
	captured, err := svc.RuntimeAuthenticateTeamSession(ctx, identities["caller"].cookie.Value, teamID)
	if err != nil {
		t.Fatal(err)
	}
	held.Store(true)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() { result <- native("caller", teamID, false) }()
	select {
	case <-entered:
	case response := <-result:
		t.Fatalf("child reservation did not dispatch: status=%d body=%s child=%s", response.Code, response.Body.String(), quotaJSON(readLimit(teamID, users["caller"].ID)))
	case <-time.After(10 * time.Second):
		t.Fatal("child reservation dispatch timed out")
	}
	for _, user := range []string{"", users["caller"].ID} {
		usage := readLimit(teamID, user).QuotaUsage
		if usage == nil || usage.Month == nil || usage.Active == nil || usage.Month.TokensUsed != 0 || usage.Month.TokensHeld != 0 || usage.Active.TokensHeld != 5 || usage.Active.MoneyHeld["USD"] != "5.000000000000000002" {
			t.Fatalf("separate live reservation missing for %q: %s", user, quotaJSON(usage))
		}
	}
	reconcile()
	if count(teamID) != 0 {
		t.Fatal("active holds became settled child exhaustion")
	}
	unblock()
	held.Store(false)
	expectStatus(t, <-result, 200)
	expectStatus(t, native("peer", teamID, false), 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct {
		user   string
		tokens int64
		money  string
	}{{"", 10, "10"}, {users["caller"].ID, 5, "5"}, {users["peer"].ID, 5, "5"}} {
		usage := readLimit(teamID, target.user).QuotaUsage
		if usage == nil || usage.Month == nil || !usage.Month.Covered || usage.Month.TokensUsed != target.tokens || usage.Month.MoneyUsed["USD"] != target.money || usage.Month.TokensUnknown != 0 || usage.Month.MoneyUnknown != 0 || usage.Active == nil || usage.Active.TokensHeld != 0 {
			t.Fatalf("independent settled account changed: %s", quotaJSON(usage))
		}
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id = ?", teamID).Order("started_at").Find(&calls).Error; err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, call := range calls {
		if call.Status != "success" {
			continue
		} // The later denied attempt remains a separate immutable fact.
		if call.UserID != users["caller"].ID && call.UserID != users["peer"].ID {
			t.Fatal("foreign native attribution", call.UserID)
		}
		expectedMembership := "tmm_notice_caller"
		if call.UserID == users["peer"].ID {
			expectedMembership = "tmm_notice_peer"
		}
		if seen[call.UserID] || call.TeamMembershipID != expectedMembership || call.KeyID != "" || call.ProjectID != "" || call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 || call.ChargeAmount == nil || *call.ChargeAmount != "5" || call.ChargeCurrency == nil || *call.ChargeCurrency != "USD" {
			t.Fatalf("native immutable accounting changed: %s", quotaJSON(call))
		}
		var attempt entity.CallAttempt
		if err := db.Where("request_id = ?", call.RequestID).First(&attempt).Error; err != nil || attempt.NativeCompletionEvidence != "completed" {
			t.Fatal("native completion evidence missing", err)
		}
		seen[call.UserID] = true
	}
	if len(seen) != 2 {
		t.Fatal("two independently settled native contributors missing", seen)
	}
	personal, err := svc.GetResourceLimit(ctx, users["caller"].ID, service.LimitTarget{Kind: "user", ID: users["caller"].ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 || len(personal.QuotaUsage.Month.MoneyUsed) != 0 {
		t.Fatal("child calls borrowed Personal attribution", err)
	}
	current := writeLimit(teamID, users["caller"].ID, `{"money_month":"5","currency":"USD","reason":"Observe exact settled child ceiling"}`)
	var policy entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", strings.TrimPrefix(current.AccountID, "team_member_")).First(&policy).Error; err != nil || policy.ScopeID != strings.TrimPrefix(current.AccountID, "team_member_") || len(policy.ScopeID) != 52 || policy.ETag == "" {
		t.Fatal("canonical child policy revision missing", current.ID, err)
	}
	before := dispatches.Load()
	expectStatus(t, native("caller", teamID, false), 429)
	if dispatches.Load() != before {
		t.Fatal("exhausted child dispatched upstream")
	}
	reconcile()
	reconcile()
	if count(teamID) != 2 {
		t.Fatal("exact child observations missing/duplicated", count(teamID))
	}
	var selfRows []entity.QuotaNotificationInbox
	if err := db.Joins("JOIN quota_notification_observations q ON q.id = quota_notification_inboxes.observation_id").Where("q.scope_kind = ? AND q.team_id = ?", "team_member", teamID).Find(&selfRows).Error; err != nil || len(selfRows) != 2 {
		t.Fatal("sole self recipient rows missing", len(selfRows), err)
	}
	for _, row := range selfRows {
		if row.RecipientID != users["caller"].ID {
			t.Fatal("owner/admin/operator fanout added", row.RecipientID)
		}
	}
	var parentCount int64
	if err := db.Model(&entity.QuotaNotificationObservation{}).Where("scope_kind = ? AND scope_id = ?", "team", teamID).Count(&parentCount).Error; err != nil || parentCount != 0 {
		t.Fatal("child notice claimed aggregate exhaustion", parentCount, err)
	}
	inbox := page("caller")
	if len(inbox.Items) != 2 || inbox.UnreadCount != 2 {
		t.Fatalf("self recipient missing: %s", quotaJSON(inbox))
	}
	var tokenNotice, moneyNotice service.NotificationRecord
	for _, record := range inbox.Items {
		q := record.Quota
		if record.SubjectType != "team_member" || record.SubjectID != policy.ScopeID || record.SubjectName != "Frozen Team notice name" || q == nil || q.ScopeKind != "team_member" || q.ScopeID != policy.ScopeID || q.TeamID == nil || *q.TeamID != teamID || q.MemberUserID == nil || *q.MemberUserID != users["caller"].ID || q.PolicyRevision != policy.ETag || q.Limit != "5" || q.Settled != "5" || !q.MonthEnd.After(q.MonthStart) || q.AsOf.Before(q.MonthStart) || !q.AsOf.Before(q.MonthEnd) {
			t.Fatalf("immutable child snapshot changed: %s", quotaJSON(record))
		}
		switch record.DetailCode {
		case "tokens_month_exhausted":
			tokenNotice = record
			if q.Currency != nil {
				t.Fatal("token snapshot has denomination")
			}
		case "money_month_exhausted":
			moneyNotice = record
			if q.Currency == nil || *q.Currency != "USD" {
				t.Fatal("money snapshot lost denomination")
			}
		default:
			t.Fatal("unexpected detail", record.DetailCode)
		}
	}
	if tokenNotice.ID == "" || moneyNotice.ID == "" {
		t.Fatal("independent dimensions missing")
	}
	for _, name := range []string{"owner", "peer", "operator", "new"} {
		if p := page(name); len(p.Items) != 0 || p.UnreadCount != 0 {
			t.Fatalf("%s borrowed private child notice: %s", name, quotaJSON(p))
		}
		expectStatus(t, actorRequest(name, "POST", "/api/v1/notifications/"+tokenNotice.ID+"/read"), 404)
	}
	expectStatus(t, request(adminCookie, admin.CSRFToken, "POST", "/api/v1/notifications/"+tokenNotice.ID+"/read", nil), 404)
	for _, alias := range []struct {
		name   string
		id     string
		status int
	}{{"case_alias", strings.ToUpper(tokenNotice.ID), 404}, {"overlong_trailing_space", tokenNotice.ID + " ", 400}} {
		t.Run("notification_id_alias/"+alias.name, func(t *testing.T) {
			expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+url.PathEscape(alias.id)+"/read"), alias.status)
		})
	}
	// Neither a valid-length exactness denial nor an overlong transport denial
	// may mark an original private notice read.
	afterAliases := page("caller")
	if len(afterAliases.Items) != len(inbox.Items) || afterAliases.UnreadCount != inbox.UnreadCount {
		t.Fatal("notification ID alias changed original private inbox/count")
	}
	for _, row := range afterAliases.Items {
		if row.ID != tokenNotice.ID && row.ID != moneyNotice.ID || row.Read || row.ReadAt != nil {
			t.Fatal("notification ID alias mutated an original read receipt", row.ID)
		}
	}
	for _, alias := range []string{strings.ToUpper(users["caller"].ID), users["caller"].ID + " "} {
		if _, err := svc.ListNotifications(ctx, alias, service.NotificationFilter{}); err == nil {
			t.Fatal("aliased current actor borrowed inbox", alias)
		}
	}
	expectStatus(t, actorRequest("caller", "GET", "/api/v1/notifications?recipient_id="+users["owner"].ID), 400)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/notifications/"+tokenNotice.ID+"/read", "", identities["caller"].cookie, ""), 403)
	testTeamMemberNoticeHistoricalAliases(t, db, policy.ScopeID, teamID, users["caller"].ID, func(method, path string) *httptest.ResponseRecorder { return actorRequest("caller", method, path) })
	firstRead := decodeCatalogResponse[service.NotificationRecord](t, actorRequest("caller", "POST", "/api/v1/notifications/"+tokenNotice.ID+"/read"), 200)
	if !firstRead.Read || firstRead.ReadAt == nil {
		t.Fatal("first read did not persist")
	}
	read := decodeCatalogResponse[service.NotificationRecord](t, actorRequest("caller", "POST", "/api/v1/notifications/"+tokenNotice.ID+"/read"), 200)
	if !read.Read || read.ReadAt == nil {
		t.Fatal("read state missing")
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).Update("name", "Renamed after observation").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&entity.TeamMembership{}, "id = ?", "tmm_notice_caller").Error; err != nil {
		t.Fatal(err)
	}
	if p := page("caller"); len(p.Items) != 0 || p.UnreadCount != 0 {
		t.Fatal("departed member retained inbox")
	}
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+moneyNotice.ID+"/read"), 404)
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/read-all"), 204)
	var moneyRow entity.QuotaNotificationInbox
	if err := db.First(&moneyRow, "id = ?", moneyNotice.ID).Error; err != nil || moneyRow.ReadAt != nil {
		t.Fatal("hidden row was marked read", err)
	}
	create(&entity.TeamMembership{ID: "tmm_member_rejoin", TeamID: teamID, UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	reconcile()
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	freshIdentity, err := svc.RuntimeAuthenticateTeamSession(ctx, identities["caller"].cookie.Value, teamID)
	if err != nil || freshIdentity.TeamMembershipID != "tmm_member_rejoin" {
		t.Fatal("current rejoined identity is not freshly authorized", err)
	}
	if err := svc.ReauthorizeTeamSession(ctx, freshIdentity, modelID); err != nil {
		t.Fatal("current model/lease does not authorize rejoined identity", err)
	}
	var denied *service.GatewayError
	if err := svc.ReauthorizeTeamSession(ctx, captured, modelID); !errors.As(err, &denied) || denied.Status != 403 || denied.Code != "team_access_denied" {
		t.Fatal("old native membership survived or failed for unrelated authority", err)
	}
	if p := page("caller"); len(p.Items) != 2 || p.UnreadCount != 1 {
		t.Fatalf("rejoin lost original private history/read state: %s", quotaJSON(p))
	}
	for _, record := range page("caller").Items {
		if record.SubjectName != "Frozen Team notice name" {
			t.Fatal("replay replaced frozen name")
		}
	}
	// Some drivers reject alias foreign keys/checks at persistence. A successful
	// update also may truncate an excess trailing space in a full-width UserID;
	// read back every complete row before claiming noncanonical authority.
	var originalMembership entity.TeamMembership
	if err := db.First(&originalMembership, "id = ?", "tmm_member_rejoin").Error; err != nil || originalMembership.ID != "tmm_member_rejoin" || originalMembership.TeamID != teamID || originalMembership.UserID != users["caller"].ID || originalMembership.Role != entity.TeamMember || originalMembership.Status != entity.ResourceActive {
		t.Fatal("canonical rejoined membership missing before alias checks", err)
	}
	for index, alias := range []struct{ field, invalid, canonical string }{{"team_id", strings.ToUpper(teamID), teamID}, {"team_id", teamID + " ", teamID}, {"user_id", strings.ToUpper(users["caller"].ID), users["caller"].ID}, {"user_id", users["caller"].ID + " ", users["caller"].ID}, {"status", "ACTIVE", entity.ResourceActive}, {"status", "active ", entity.ResourceActive}, {"role", "MEMBER", entity.TeamMember}, {"role", "member ", entity.TeamMember}} {
		if alias.invalid == alias.canonical {
			t.Fatalf("current membership alias case %d (%s) is not a corruption", index, alias.field)
		}
		expectedAlias := originalMembership
		switch alias.field {
		case "team_id":
			expectedAlias.TeamID = alias.invalid
		case "user_id":
			expectedAlias.UserID = alias.invalid
		case "status":
			expectedAlias.Status = alias.invalid
		case "role":
			expectedAlias.Role = alias.invalid
		}
		err := db.Model(&entity.TeamMembership{}).Where("id = ?", originalMembership.ID).Update(alias.field, alias.invalid).Error
		var persisted entity.TeamMembership
		if readErr := db.First(&persisted, "id = ?", originalMembership.ID).Error; readErr != nil {
			t.Fatalf("current membership alias case %d (%s) readback failed: %v", index, alias.field, readErr)
		}
		if err != nil {
			if persisted != originalMembership {
				t.Fatalf("rejected current membership alias case %d (%s) changed the canonical row", index, alias.field)
			}
			continue
		}
		if persisted == originalMembership {
			if alias.field != "user_id" || len(alias.canonical) != 30 || alias.invalid != alias.canonical+" " {
				t.Fatalf("current membership alias case %d (%s) unexpectedly did not persist", index, alias.field)
			}
			t.Logf("current membership alias case %d: canonical 30-byte UserID retained after excess trailing-space normalization", index)
			continue
		}
		if persisted != expectedAlias {
			t.Fatalf("current membership alias case %d (%s) changed more than the selected field", index, alias.field)
		}
		if p := page("caller"); len(p.Items) != 0 || p.UnreadCount != 0 {
			t.Fatalf("persisted noncanonical current membership case %d (%s) borrowed private history", index, alias.field)
		}
		expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/"+moneyNotice.ID+"/read"), 404)
		if err := db.Model(&entity.TeamMembership{}).Where("id = ?", originalMembership.ID).Update(alias.field, alias.canonical).Error; err != nil {
			t.Fatal(err)
		}
		var restored entity.TeamMembership
		if err := db.First(&restored, "id = ?", originalMembership.ID).Error; err != nil || restored != originalMembership {
			t.Fatalf("current membership alias case %d (%s) failed exact restoration: %v", index, alias.field, err)
		}
	}
	for _, mutation := range []struct {
		model          any
		id, field      string
		value, restore any
	}{{&entity.TeamMembership{}, "tmm_member_rejoin", "status", entity.ResourceDisabled, entity.ResourceActive}, {&entity.Team{}, teamID, "status", entity.ResourceDisabled, entity.ResourceActive}, {&entity.Team{}, teamID, "status", entity.ResourceArchived, entity.ResourceActive}} {
		if err := db.Model(mutation.model).Where("id = ?", mutation.id).Update(mutation.field, mutation.value).Error; err != nil {
			t.Fatal(err)
		}
		if p := page("caller"); len(p.Items) != 0 || p.UnreadCount != 0 {
			t.Fatal("inactive target retained private inbox")
		}
		expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/read-all"), 204)
		if err := db.Model(mutation.model).Where("id = ?", mutation.id).Update(mutation.field, mutation.restore).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"disabled", "offboarded_at"} {
		var value, restore any = true, false
		if field == "offboarded_at" {
			value, restore = time.Now().UTC(), nil
		}
		if err := db.Model(&entity.User{}).Where("id = ?", users["caller"].ID).Update(field, value).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ListNotifications(ctx, users["caller"].ID, service.NotificationFilter{}); err == nil {
			t.Fatal("inactive actor read private inbox", field)
		}
		if err := svc.MarkAllNotificationsRead(ctx, users["caller"].ID); err == nil {
			t.Fatal("inactive actor marked inbox", field)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", users["caller"].ID).Update(field, restore).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Unknown terminal usage and truncated streams retain accounting uncertainty.
	unknown.Store(true)
	expectStatus(t, native("caller", "tea_notice_unknown", false), 200)
	unknown.Store(false)
	truncated.Store(true)
	expectStatus(t, native("caller", "tea_notice_truncated", true), 200)
	truncated.Store(false)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"unknown", "truncated"} {
		team := "tea_notice_" + suffix
		writeLimit(team, users["caller"].ID, `{"tokens_month":0,"money_month":"0","currency":"USD","reason":"Unknown child settlement remains unknown"}`)
		usage := readLimit(team, users["caller"].ID).QuotaUsage
		if usage == nil || usage.Month == nil || usage.Month.TokensUnknown == 0 || usage.Month.MoneyUnknown == 0 {
			t.Fatalf("unknown became known zero: %s", quotaJSON(usage))
		}
	}
	reconcile()
	for _, suffix := range []string{"unknown", "truncated"} {
		if count("tea_notice_"+suffix) != 0 {
			t.Fatal("unknown child emitted exhaustion", suffix)
		}
	}
	writeLimit("tea_notice_uncovered", users["owner"].ID, `{"tokens_month":0,"reason":"Uncovered child has no zero proof"}`)
	reconcile()
	if count("tea_notice_uncovered") != 0 {
		t.Fatal("incomplete coverage became known zero")
	}
	stale := writeLimit("tea_notice_stale", users["caller"].ID, `{"tokens_month":10,"reason":"Published child baseline"}`)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", strings.TrimPrefix(stale.AccountID, "team_member_")).Updates(map[string]any{"tokens_month": 0, "ETag": "lim_member_unpublished"}).Error; err != nil {
		t.Fatal(err)
	}
	reconcileSavedSnapshot()
	if count("tea_notice_stale") != 0 {
		t.Fatal("unpublished child generation emitted notice")
	}
	// Calendar and lease are independently stale against an otherwise current
	// covered finite-zero child; use server lease time without a fake clock.
	create(&entity.Team{ID: "tea_member_clock", Name: "Calendar and lease proof", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_member_clock", TeamID: "tea_member_clock", UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	writeLimit("tea_member_clock", users["caller"].ID, `{"tokens_month":0,"reason":"Covered child requires current calendar and lease"}`)
	var calendar entity.QuotaSetting
	if err := db.First(&calendar, 1).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("ETag", "cal_member_unpublished").Error; err != nil {
		t.Fatal(err)
	}
	reconcileSavedSnapshot()
	if count("tea_member_clock") != 0 {
		t.Fatal("stale calendar revision emitted notice")
	}
	if err := db.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("ETag", calendar.ETag).Error; err != nil {
		t.Fatal(err)
	}
	status := svc.RuntimeStatus()
	if status.AuthorizationValidUntil == nil {
		t.Fatal("real runtime lease missing")
	}
	timer := time.NewTimer(time.Until(*status.AuthorizationValidUntil) + time.Millisecond)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		t.Fatal(ctx.Err())
	}
	reconcileSavedSnapshot()
	if count("tea_member_clock") != 0 {
		t.Fatal("expired private lease emitted notice")
	}
	reconcile()
	if count("tea_member_clock") != 1 {
		t.Fatal("current lease/calendar failed covered-zero child")
	}
	// Publishing the complete current snapshot also observes the previously
	// unpublished zero-token policy. That historical fact must remain immutable.
	var staleObservations []entity.QuotaNotificationObservation
	if err := db.Where("scope_kind = ? AND team_id = ?", "team_member", "tea_notice_stale").Find(&staleObservations).Error; err != nil {
		t.Fatal(err)
	}
	if len(staleObservations) != 1 || staleObservations[0].Dimension != "tokens" || staleObservations[0].PolicyRevision != "lim_member_unpublished" || staleObservations[0].Limit != "0" || staleObservations[0].Settled != "0" || staleObservations[0].Currency != "" {
		t.Fatal("published stale child did not retain its exact zero-token observation")
	}
	staleSnapshot := quotaJSON(staleObservations[0])
	assertStaleObservation := func() {
		t.Helper()
		var retained []entity.QuotaNotificationObservation
		if err := db.Where("scope_kind = ? AND team_id = ?", "team_member", "tea_notice_stale").Find(&retained).Error; err != nil {
			t.Fatal(err)
		}
		if len(retained) != 1 || quotaJSON(retained[0]) != staleSnapshot {
			t.Fatal("denomination checks changed the complete historical token observation")
		}
	}
	var denomination entity.PricingSetting
	if err := db.First(&denomination, 1).Error; err != nil || denomination.PlatformCurrency != "USD" {
		t.Fatal("controlled USD denomination missing", err)
	}
	create(&entity.Team{ID: "tea_member_money", Name: "Isolated denomination proof", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_member_money", TeamID: "tea_member_money", UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	moneyPolicy := writeLimit("tea_member_money", users["caller"].ID, `{"money_month":"0","currency":"USD","reason":"Separate zero money proof"}`)
	var denominationPolicyRow entity.ResourceLimit
	if err := db.Where("scope_kind = ? AND scope_id = ?", "team_member", strings.TrimPrefix(moneyPolicy.AccountID, "team_member_")).First(&denominationPolicyRow).Error; err != nil || denominationPolicyRow.TokensMonth != nil || denominationPolicyRow.MoneyMonth == nil || *denominationPolicyRow.MoneyMonth != "0" || denominationPolicyRow.Currency != "USD" {
		t.Fatal("isolated exact money-only policy missing", err)
	}
	if count("tea_member_money") != 0 {
		t.Fatal("fresh money Team already had an observation before reconciliation")
	}
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", "EUR").Error; err != nil {
		t.Fatal(err)
	}
	reconcileSavedSnapshot()
	if count("tea_member_money") != 0 {
		t.Fatal("unpublished denomination emitted money notice")
	}
	assertStaleObservation()
	if err := db.Model(&entity.PricingSetting{}).Where("id = ?", 1).Update("platform_currency", denomination.PlatformCurrency).Error; err != nil {
		t.Fatal(err)
	}
	// Finite zero is independently meaningful when publication and coverage agree.
	reconcile()
	var moneyObservations []entity.QuotaNotificationObservation
	if err := db.Where("scope_kind = ? AND team_id = ?", "team_member", "tea_member_money").Find(&moneyObservations).Error; err != nil {
		t.Fatal(err)
	}
	if len(moneyObservations) != 1 {
		t.Fatal("covered finite zero child did not emit exactly one money observation")
	}
	moneyObservation := moneyObservations[0]
	if moneyObservation.ScopeID != denominationPolicyRow.ScopeID || moneyObservation.TeamID == nil || *moneyObservation.TeamID != "tea_member_money" || moneyObservation.MemberUserID == nil || *moneyObservation.MemberUserID != users["caller"].ID || moneyObservation.Dimension != "money" || moneyObservation.PolicyRevision != denominationPolicyRow.ETag || moneyObservation.Currency != "USD" || moneyObservation.Limit != "0" || moneyObservation.Settled != "0" {
		t.Fatal("covered finite zero money observation lost exact current policy or member facts")
	}
	assertStaleObservation()
	create(&entity.Team{ID: "tea_member_atomic", Name: "Atomic self inbox", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_member_atomic", TeamID: "tea_member_atomic", UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	writeLimit("tea_member_atomic", users["caller"].ID, `{"tokens_month":0,"reason":"Atomic observation and self recipient"}`)
	failInbox.Store(true)
	if err := svc.ReconcileMonthlyQuotaNotifications(ctx); err == nil {
		t.Fatal("controlled recipient failure was ignored")
	}
	failInbox.Store(false)
	if count("tea_member_atomic") != 0 {
		t.Fatal("recipient failure left an orphan committed observation")
	}
	reconcile()
	if count("tea_member_atomic") != 1 {
		t.Fatal("atomic rollback could not reconcile original current facts")
	}
	// More than one bounded candidate page must complete in one explicit cycle.
	for index := range 35 {
		id := fmt.Sprintf("tea_member_fair_%02d", index)
		create(&entity.Team{ID: id, Name: "Fair child scan", Status: entity.ResourceActive}, &entity.TeamMembership{ID: fmt.Sprintf("tmm_member_fair_%02d", index), TeamID: id, UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
		if index == 34 {
			writeLimit(id, users["caller"].ID, `{"tokens_month":0,"reason":"Qualifying final member page"}`)
		}
	}
	reconcile()
	if count("tea_member_fair_34") != 1 {
		t.Fatal("candidate beyond first32 was starved")
	}
	for _, model := range []any{&entity.Notification{}, &entity.NotificationDeliveryIntent{}, &entity.OperationalAlert{}} {
		var count int64
		if err := db.Model(model).Where("kind = ?", "monthly_quota_exhausted").Count(&count).Error; err != nil || count != 0 {
			t.Fatal("private child notice entered operational/SMTP channel", count, err)
		}
	}
	create(&entity.Team{ID: "tea_member_closed", Name: "Unavailable recorder", Status: entity.ResourceActive}, &entity.TeamMembership{ID: "tmm_member_closed", TeamID: "tea_member_closed", UserID: users["caller"].ID, Role: entity.TeamMember, Status: entity.ResourceActive})
	writeLimit("tea_member_closed", users["caller"].ID, `{"tokens_month":0,"reason":"Unavailable journal must stay unknown"}`)
	// Reopening the same recorder delivers no new native calls or new recipients.
	expectStatus(t, actorRequest("caller", "POST", "/api/v1/notifications/read-all"), 204)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	_ = svc.ReconcileMonthlyQuotaNotifications(ctx) // A closed recorder may defer the cycle.
	if count("tea_member_closed") != 0 {
		t.Fatal("closed journal manufactured known zero")
	}
	svc = makeService()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	router = fox.New()
	New(svc).RegisterRoutes(router)
	var wait sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wait.Go(func() { failures <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
	}
	wait.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal("concurrent reconciliation", err)
		}
	}
	if count(teamID) != 2 || count("tea_member_fair_34") != 1 {
		t.Fatal("restart/concurrency duplicated observations")
	}
	found := false
	for _, record := range page("caller").Items {
		if record.ID == tokenNotice.ID {
			found = true
			if !record.Read || record.ReadAt == nil || !record.ReadAt.Equal(*read.ReadAt) {
				t.Fatal("restart reset read receipt")
			}
		}
	}
	if !found {
		t.Fatal("restart lost original notice")
	}
	if dispatches.Load() != 5 {
		t.Fatal("native replay/extra dispatch", dispatches.Load())
	}
	if usage := readLimit(teamID, users["caller"].ID).QuotaUsage; usage == nil || usage.Month == nil || usage.Month.TokensUsed != 5 || usage.Month.MoneyUsed["USD"] != "5" {
		t.Fatal("restart reset stable child account")
	}
}

func TestTeamMemberQuotaNotificationNativeFixture(t *testing.T) {
	for _, tc := range []struct {
		name  string
		known bool
	}{{"known", true}, {"unknown", false}} {
		t.Run(tc.name, func(t *testing.T) {
			value := parseGatewayUsage([]byte(teamMemberNoticeNativeBody(!tc.known)))
			if value.NativeCompletionEvidence != "completed" {
				t.Fatal("controlled fixture lost native terminal evidence")
			}
			if tc.known {
				if !value.Complete || value.Input == nil || *value.Input != 4 || value.Output == nil || *value.Output != 1 || value.CacheRead == nil || *value.CacheRead != 0 || value.CacheWrite == nil || *value.CacheWrite != 0 {
					t.Fatal("controlled fixture usage does not prove 4+1 with known zero cache components")
				}
			} else if value.Input != nil || value.Output != nil {
				t.Fatal("missing usage was invented")
			}
		})
	}
}

// Persisted historical proofs must not borrow case-folded current authority or
// joins. These malformed facts never stand in for native completion evidence.
func testTeamMemberNoticeHistoricalAliases(t *testing.T, db *gorm.DB, scope, team, user string, request func(string, string) *httptest.ResponseRecorder) {
	t.Helper()
	before := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all"), 200)
	now := time.Now().UTC().Truncate(time.Microsecond)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var observationIDs, inboxIDs []string
	for index, change := range []string{"team_case", "team_space", "user_case", "scope_case", "scope_foreign", "recipient_case", "join_case"} {
		row := entity.QuotaNotificationObservation{ID: fmt.Sprintf("qob_member_alias_%d", index), ScopeKind: "team_member", ScopeID: scope, ScopeName: "Hidden historical alias", Dimension: "tokens", PolicyRevision: fmt.Sprintf("rev_member_alias_%d", index), MonthStart: month, MonthEnd: month.AddDate(0, 1, 0), TimeZone: "UTC", AsOf: now, Limit: "0", Settled: "0", CoverageStart: month, ResourceCreatedAt: month}
		teamProof, userProof, recipient := team, user, user
		switch change {
		case "team_case":
			teamProof = strings.ToUpper(team)
		case "team_space":
			teamProof = team + " "
		case "user_case":
			userProof = strings.ToUpper(user)
		case "scope_case":
			// Stable pair digests use uppercase unpadded Base32. The fixture
			// must change that exact identity rather than reseed a valid pair.
			row.ScopeID = strings.ToLower(scope)
			if row.ScopeID == scope {
				t.Fatal("scope case corruption did not change canonical identity")
			}
		case "scope_foreign":
			row.ScopeID = strings.Repeat("x", 52)
		case "recipient_case":
			recipient = strings.ToUpper(user)
		}
		row.TeamID, row.MemberUserID = &teamProof, &userProof
		if err := db.Create(&row).Error; err != nil {
			t.Fatal("historical alias seed", change, err)
		}
		observationIDs = append(observationIDs, row.ID)
		joined := row.ID
		if change == "join_case" {
			joined = strings.ToUpper(joined)
		}
		inbox := entity.QuotaNotificationInbox{ID: fmt.Sprintf("qni_member_alias_%d", index), ObservationID: joined, RecipientID: recipient, CreatedAt: now}
		if err := db.Create(&inbox).Error; err != nil {
			t.Fatal(err)
		}
		inboxIDs = append(inboxIDs, inbox.ID)
	}
	after := decodeCatalogResponse[service.NotificationPage](t, request("GET", "/api/v1/notifications?status=all"), 200)
	if len(after.Items) != len(before.Items) || after.UnreadCount != before.UnreadCount {
		t.Fatal("aliased pair/recipient/join borrowed private history")
	}
	for _, id := range inboxIDs {
		expectStatus(t, request("POST", "/api/v1/notifications/"+id+"/read"), 404)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		path := "/api/v1/notifications?status=all&limit=1"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		page := decodeCatalogResponse[service.NotificationPage](t, request("GET", path), 200)
		if page.UnreadCount != before.UnreadCount {
			t.Fatal("private paging changed unread count")
		}
		for _, row := range page.Items {
			if seen[row.ID] || strings.Contains(row.ID, "alias") {
				t.Fatal("alias consumed/repeated authorized page")
			}
			seen[row.ID] = true
		}
		cursor = page.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(before.Items) {
		t.Fatal("hidden proofs consumed cursor slots")
	}
	var changed int64
	if err := db.Model(&entity.QuotaNotificationInbox{}).Where("id IN ? AND read_at IS NOT NULL", inboxIDs).Count(&changed).Error; err != nil || changed != 0 {
		t.Fatal("denied alias mutation changed history", changed, err)
	}
	// Remove only malformed fixture rows so subsequent observation counts remain
	// solely the native/current-policy evidence under test.
	if err := db.Where("id IN ?", inboxIDs).Delete(&entity.QuotaNotificationInbox{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id IN ?", observationIDs).Delete(&entity.QuotaNotificationObservation{}).Error; err != nil {
		t.Fatal(err)
	}
}

func teamMemberNoticeNativeBody(unknown bool) string {
	if unknown {
		return `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Unknown usage"},"finish_reason":"stop"}]}`
	}
	return `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Completed"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`
}
