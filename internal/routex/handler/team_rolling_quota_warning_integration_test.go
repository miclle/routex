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

// Every observation uses a live publisher and actual journal facts. A bounded
// fixture-owned publisher barrier prevents an unrelated periodic pointer swap;
// it does not stop the runtime, bypass proof, or replay a failed observation.
func testTeamRollingQuotaWarningLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{83}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var tokens atomic.Int64
	tokens.Store(2)
	var unknown, inboxFault atomic.Bool
	var dispatches, inboxFaultHits atomic.Int64
	var barrier personalKeyWarningFixturePublicationBarrier
	workerCtx := context.WithValue(ctx, personalKeyWarningFixtureWorkerContext{}, &barrier)
	callback := "fixture:team-rolling-inbox"
	publisherCallback := "fixture:team-rolling-publisher"
	fault := errors.New("controlled Team rolling inbox failure")
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if inboxFault.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "team_rolling_quota_warning_inboxes" {
			inboxFaultHits.Add(1)
			_ = tx.AddError(fault)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(publisherCallback, barrier.beforeQuery); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(publisherCallback); err != nil {
			t.Error(err)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		usage := fmt.Sprintf(`,"usage":{"prompt_tokens":%d,"completion_tokens":0,"total_tokens":%d}`, tokens.Load(), tokens.Load())
		if unknown.Load() {
			usage = ""
		}
		_, _ = io.WriteString(w, `{"id":"team-rolling-native","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]`+usage+`}`)
	}))
	defer upstream.Close()
	makeService := func() *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		return svc
	}
	svc := makeService()
	defer func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-rolling-admin@example.invalid","password":"rolling-warning-password","name":"Team admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	create := func(v any) {
		t.Helper()
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	cipher, err := store.Seal("crd_team_rolling", "controlled-Team-credential")
	if err != nil {
		t.Fatal(err)
	}
	model := "mdl_team_rolling"
	warmup := "rx_" + strings.Repeat("a", 43)
	for _, v := range []any{&entity.Provider{ID: "prv_team_rolling", Name: "Team rolling"}, &entity.ProviderConnection{ID: "con_team_rolling", ProviderID: "prv_team_rolling", Name: "Native", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1", Enabled: true}, &entity.ProviderCredential{ID: "crd_team_rolling", ConnectionID: "con_team_rolling", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"}, &entity.ProviderModel{ID: "pmd_team_rolling", ConnectionID: "con_team_rolling", UpstreamName: "team-rolling-native"}, &entity.CredentialModelAccess{CredentialID: "crd_team_rolling", ProviderModelID: "pmd_team_rolling"}, &entity.Model{ID: model, Status: "active"}, &entity.ModelName{Name: "team-rolling-model", ModelID: model, CurrentModelID: &model}, &entity.ModelProviderBinding{ID: "bnd_team_rolling", ModelID: model, ProviderModelID: "pmd_team_rolling", Weight: 100}, &entity.UserModelGrant{UserID: admin.User.ID, ModelID: model}, &entity.APIKey{ID: "key_team_rolling_warmup", UserID: admin.User.ID, Name: "Warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(warmup), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_team_rolling_warmup", ModelID: model}} {
		create(v)
	}
	spool := filepath.Join(t.TempDir(), "team-rolling.db")
	if err := svc.StartRuntime(workerCtx); err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	call := func(bearer string) {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test/v1/chat/completions", strings.NewReader(`{"model":"team-rolling-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	call(warmup)
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	members := []*service.MemberRecord{}
	for _, name := range []string{"original", "owner", "later", "outsider"} {
		member, err := svc.CreateMember(ctx, admin.User.ID, "team-rolling-"+name+"@example.invalid", "rolling-warning-password", name, "member")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Take(&member.User, "id = ?", member.User.ID).Error; err != nil {
			t.Fatal(err)
		}
		members = append(members, member)
	}
	member, owner, later, outsider := members[0], members[1], members[2], members[3]
	selected := []service.TeamMemberInput{{UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, {UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Recorded Team", "Rolling aggregate", []string{owner.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, selected); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{model}); err != nil {
		t.Fatal(err)
	}
	target := service.LimitTarget{Kind: "team", TeamID: team.ID}
	current, err := svc.GetResourceLimit(ctx, member.User.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	cap := int64(100)
	policy, err := svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Reviewed Team rolling cap"})
	if err != nil || !policy.Enforced {
		t.Fatal("Team cap not applied", err)
	}
	request := func(cookie *http.Cookie, csrf, method, path string, body any, etag string) *httptest.ResponseRecorder {
		t.Helper()
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
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-rolling-original@example.invalid","password":"rolling-warning-password"}`, nil, "")
	expectStatus(t, login, 200)
	identity, cookie := readIdentity(t, login)
	teamCall := func() {
		t.Helper()
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(bounded, "POST", "http://routex.test/api/v1/teams/"+team.ID+"/chat/completions", strings.NewReader(`{"model":"team-rolling-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("X-CSRF-Token", identity.CSRFToken)
		req.AddCookie(cookie)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		expectStatus(t, res, 200)
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	setBound := func(n int64) {
		t.Helper()
		path := "/api/v1/admin/provider-models/pmd_team_rolling/reservation-bound"
		res := request(adminCookie, admin.CSRFToken, "GET", path, nil, "")
		record := decodeCatalogResponse[service.ReservationBoundRecord](t, res, 200)
		expectStatus(t, request(adminCookie, admin.CSRFToken, "PUT", path, map[string]any{"max_input_tokens": n, "max_output_tokens": 1, "evidence": "Controlled native fixture", "reason": "Finite capacity"}, record.ETag), 200)
	}
	observe := func(fn func() error) {
		t.Helper()
		if err := barrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, fn); err != nil {
			t.Fatal(err)
		}
	}
	reconcile := func() { observe(func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) }) }
	count := func(want int64) {
		t.Helper()
		var n int64
		if err := db.Model(&entity.TeamRollingQuotaWarningObservation{}).Where("team_id = ?", team.ID).Count(&n).Error; err != nil || n != want {
			t.Fatal("Team observation count", n, want, err)
		}
	}
	page := func(actor string) *service.NotificationPage {
		t.Helper()
		p, err := svc.ListNotifications(ctx, actor, service.NotificationFilter{})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	setBound(20)
	tokens.Store(20)
	for range 4 {
		teamCall()
	}
	count(0)
	reconcile()
	count(2)
	near := page(member.User.ID)
	if len(near.Items) != 2 || near.UnreadCount != 2 {
		t.Fatal("two rolling windows missing")
	}
	for _, n := range near.Items {
		v := n.TeamRollingQuotaWarning
		if v == nil || v.ScopeKind != "team" || v.ScopeID != team.ID || v.Limit != "100" || v.Settled != "80" || v.Level != "near" || v.PolicyRevision != policy.ETag || n.SubjectName != "Recorded Team" || n.QuotaWarning != nil {
			t.Fatal("Team snapshot mismatch")
		}
	}
	for _, actor := range []struct{ category, id string }{{"administrator", admin.User.ID}, {"outsider", outsider.User.ID}, {"later_member", later.User.ID}} {
		if got := page(actor.id); len(got.Items) != 0 {
			for _, notice := range got.Items {
				t.Logf("foreign actor category=%s received notice kind=%s id=%s", actor.category, notice.Kind, notice.ID)
			}
			t.Fatal("foreign actor received history")
		}
	}
	if len(page(owner.User.ID).Items) != 2 {
		t.Fatal("original member fanout missing")
	}
	marked := decodeCatalogResponse[service.NotificationRecord](t, request(cookie, identity.CSRFToken, "POST", "/api/v1/notifications/"+near.Items[0].ID+"/read", nil, ""), 200)
	if !marked.Read || marked.ReadAt == nil {
		t.Fatal("read receipt missing")
	}
	tokens.Store(10)
	setBound(10)
	teamCall()
	// Two concurrent producers must commit one observation per episode/level/window.
	observe(func() error {
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Go(func() { errs <- svc.ReconcileMonthlyQuotaNotifications(ctx) })
		}
		wg.Wait()
		close(errs)
		var e error
		for err := range errs {
			e = errors.Join(e, err)
		}
		return e
	})
	count(4)
	setBound(1)
	unknown.Store(true)
	teamCall()
	unknown.Store(false)
	record, err := svc.GetResourceLimit(ctx, member.User.ID, target)
	if err != nil || record.QuotaUsage == nil {
		t.Fatal(err)
	}
	for _, window := range []*service.QuotaWindowRecord{record.QuotaUsage.FiveHours, record.QuotaUsage.SevenDays} {
		if window == nil || !window.Covered || window.TokensUsed != 90 || window.TokensHeld != 2 || window.TokensUnknown != 0 {
			t.Fatal("finite held counter proof")
		}
	}
	reconcile()
	count(4)
	// Unrelated edits never create a new episode or expand old recipient snapshots.
	current, err = svc.GetResourceLimit(ctx, member.User.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	policy, err = svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &cap, Tokens7D: &cap}, Reason: "Unrelated reason edit"})
	if err != nil || !policy.Enforced {
		t.Fatal(err)
	}
	reconcile()
	count(4)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, []service.TeamMemberInput{{UserID: owner.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: later.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	if len(page(member.User.ID).Items) != 0 || len(page(later.User.ID).Items) != 0 {
		t.Fatal("removed or later member borrowed history")
	}
	expectStatus(t, request(cookie, identity.CSRFToken, "POST", "/api/v1/notifications/"+near.Items[0].ID+"/read", nil, ""), 404)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, selected); err != nil {
		t.Fatal(err)
	}
	if len(page(member.User.ID).Items) != 4 {
		t.Fatal("exact original member history not restored")
	}
	for _, kind := range []string{"recipient", "team"} {
		birth := member.User.CreatedAt
		model := any(&entity.User{})
		id := member.User.ID
		if kind == "team" {
			birth = team.CreatedAt
			model = &entity.Team{}
			id = team.ID
		}
		// A sub-millisecond update can round to the existing birth. Prove the
		// changed and restored database identity before asserting its privacy.
		setBirth := func(want time.Time) {
			t.Helper()
			updated := db.Model(model).Where("id = ?", id).Update("created_at", want)
			if updated.Error != nil || updated.RowsAffected != 1 {
				t.Fatalf("%s birth update: rows=%d error=%v", kind, updated.RowsAffected, updated.Error)
			}
			var stored struct{ CreatedAt time.Time }
			if err := db.Model(model).Where("id = ?", id).Select("created_at").Take(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if !stored.CreatedAt.Equal(want) {
				t.Fatalf("%s birth did not persist: want=%s actual=%s", kind, want.Format(time.RFC3339Nano), stored.CreatedAt.Format(time.RFC3339Nano))
			}
		}
		setBirth(birth.Add(time.Second))
		if got := page(member.User.ID); len(got.Items) != 0 {
			for _, notice := range got.Items {
				t.Logf("recreated %s received notice kind=%s id=%s", kind, notice.Kind, notice.ID)
			}
			t.Fatalf("recreated %s identity borrowed history", kind)
		}
		setBirth(birth)
	}
	current, err = svc.GetResourceLimit(ctx, member.User.ID, target)
	if err != nil {
		t.Fatal(err)
	}
	next := int64(99)
	policy, err = svc.SetResourceLimit(ctx, admin.User.ID, target, current.ETag, service.LimitInput{Policy: limits.Policy{Tokens5H: &next, Tokens7D: &next}, Reason: "Reviewed cap change"})
	if err != nil || !policy.Enforced {
		t.Fatal(err)
	}
	inboxFault.Store(true)
	err = barrier.observePublished(func() error { return svc.RefreshRuntime(ctx) }, func() error { return svc.ReconcileMonthlyQuotaNotifications(ctx) })
	inboxFault.Store(false)
	if !errors.Is(err, fault) || inboxFaultHits.Load() == 0 {
		t.Fatal("actual inbox rollback seam not exercised", err)
	}
	count(4)
	reconcile()
	count(6)
	var observations []entity.TeamRollingQuotaWarningObservation
	var inboxes []entity.TeamRollingQuotaWarningInbox
	if err := db.Order("id").Find(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&inboxes).Error; err != nil || len(inboxes) != 12 {
		t.Fatal("exact recipient cardinality", err)
	}
	calls, attempts := dispatches.Load(), int64(0)
	if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil || attempts != calls {
		t.Fatal("original native attempts", err)
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
	reconcile()
	count(6)
	var saved []entity.TeamRollingQuotaWarningObservation
	var receipts []entity.TeamRollingQuotaWarningInbox
	if err := db.Order("id").Find(&saved).Error; err != nil || len(saved) != len(observations) {
		t.Fatal(err)
	}
	for i := range saved {
		if !teamRollingRetainedFactsEqual(saved[i], observations[i]) {
			t.Fatal("immutable observation changed on restart")
		}
	}
	if err := db.Order("id").Find(&receipts).Error; err != nil || len(receipts) != len(inboxes) {
		t.Fatal(err)
	}
	for i := range receipts {
		if !teamRollingRetainedFactsEqual(receipts[i], inboxes[i]) {
			t.Fatal("original recipient/read receipt changed")
		}
	}
	sessionRes := identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, "")
	expectStatus(t, sessionRes, 200)
	var session SessionResponse
	if err := json.Unmarshal(sessionRes.Body.Bytes(), &session); err != nil || session.User.ID != identity.User.ID || session.CSRFToken != identity.CSRFToken || len(sessionRes.Result().Cookies()) != 0 {
		t.Fatal("original Session or CSRF changed")
	}
	if dispatches.Load() != calls {
		t.Fatal("observation/restart dispatched native traffic")
	}
}
