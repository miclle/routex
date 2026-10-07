package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// Additive case144: one coverage warmup, six Team probes, then one rejoin probe.
// Exactly eight Calls/four native Attempts; four quota denials have no Attempt.
func testTeamMemberMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var publicationFailure, auditFailure atomic.Bool
	const faultName = "test_team_member_monthly_fault"
	if err := db.Callback().Query().Before("gorm:query").Register(faultName, func(tx *gorm.DB) {
		if publicationFailure.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled member publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register(faultName, func(tx *gorm.DB) {
		if auditFailure.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled member audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Register before any runtime/worker. Later cleanup stops every instance first.
	defer func() {
		publicationFailure.Store(false)
		auditFailure.Store(false)
		if err := db.Callback().Query().Remove(faultName); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Create().Remove(faultName); err != nil {
			t.Error(err)
		}
	}()

	store, err := secretstore.New(bytes.Repeat([]byte{121}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var nativePosts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			t.Error("unexpected native route")
			http.NotFound(w, r)
			return
		}
		nativePosts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":0,"cache_write_tokens":0}}}`)
	}))
	defer upstream.Close()
	var instances []*service.Service
	defer func() {
		for _, svc := range instances {
			svc.StopRuntime()
			if err := svc.StopCallRecorder(); err != nil {
				t.Error(err)
			}
		}
	}()
	makeService := func() *service.Service {
		t.Helper()
		svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, svc)
		return svc
	}
	svc := makeService()
	admin, err := svc.Initialize(ctx, "team-member-behavior-admin@example.invalid", "test-only-team-member-behavior-password", "Team behavior admin")
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	adminSession, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-member-behavior-admin@example.invalid","password":"test-only-team-member-behavior-password"}`, nil, ""))
	_, tokenCookie, tokenCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-mode-token", []string{"teams.tokens.write"})
	_, moneyCookie, moneyCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-mode-money", []string{"teams.money.write"})
	create := func(rows ...any) {
		t.Helper()
		for _, row := range rows {
			if err := db.Create(row).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	modelID := "mdl_team_behavior"
	bearer := "rx_" + strings.Repeat("b", 43)
	cipher, err := store.Seal("crd_team_behavior", "controlled-native-secret")
	if err != nil {
		t.Fatal(err)
	}
	create(&entity.Provider{ID: "prv_team_behavior", Name: "Team behavior provider"},
		&entity.ProviderConnection{ID: "con_team_behavior", ProviderID: "prv_team_behavior", Name: "Chat", Protocol: entity.ProtocolOpenAIChat, BaseURL: upstream.URL + "/v1"},
		&entity.ProviderCredential{ID: "crd_team_behavior", ConnectionID: "con_team_behavior", Name: "Verified", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"},
		&entity.ProviderModel{ID: "pmd_team_behavior", ConnectionID: "con_team_behavior", UpstreamName: "native"},
		&entity.CredentialModelAccess{CredentialID: "crd_team_behavior", ProviderModelID: "pmd_team_behavior"},
		&entity.Model{ID: modelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "team-behavior-native", ModelID: modelID, CurrentModelID: &modelID},
		&entity.ModelProviderBinding{ID: "bnd_team_behavior", ModelID: modelID, ProviderModelID: "pmd_team_behavior", Weight: 100},
		&entity.UserModelGrant{UserID: admin.User.ID, ModelID: modelID},
		&entity.APIKey{ID: "key_team_behavior", UserID: admin.User.ID, Name: "Coverage warmup", Prefix: "rx_masked", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}, &entity.APIKeyModel{KeyID: "key_team_behavior", ModelID: modelID},
		&entity.ModelPrice{ID: "price_team_behavior", ProviderModelID: "pmd_team_behavior", UpdateSource: "api"},
		&entity.ReservationBound{ProviderModelID: "pmd_team_behavior", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bound_team_behavior", Evidence: "Controlled maximum", Reason: "Team behavior fixture"})
	for _, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		create(&entity.PriceRate{ID: "rate_tb_" + metric, ModelPriceID: "price_team_behavior", Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "1000000", Enabled: true})
	}
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	spool := filepath.Join(t.TempDir(), "team-behavior.db")
	if err := svc.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	body := `{"model":"team-behavior-native","messages":[{"role":"user","content":"Hello"}],"max_completion_tokens":1}`
	warmup := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	warmup.Header.Set("Authorization", "Bearer "+bearer)
	warmup.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, warmup)
	expectStatus(t, response, 200)
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	member, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-mode-member", nil)
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Monthly Team behavior", "Independent aggregate/member caps", []string{admin.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	var membership entity.TeamMembership
	if err := db.Take(&membership, "team_id = ? AND user_id = ?", team.ID, member.User.ID).Error; err != nil || membership.TeamID != team.ID || membership.UserID != member.User.ID || membership.ID == "" {
		t.Fatal("exact current member identity", err)
	}
	path := "/api/v1/teams/" + team.ID + "/limits"
	childPath := "/api/v1/teams/" + team.ID + "/members/" + member.User.ID + "/limits"
	get := func(path string, cookie *http.Cookie) service.LimitRecord {
		t.Helper()
		response := identityRequest(router, "GET", path, "", cookie, "")
		expectStatus(t, response, 200)
		var record service.LimitRecord
		if err := json.Unmarshal(response.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if response.Header().Get("ETag") != `"`+record.ETag+`"` {
			t.Fatal("incoherent review")
		}
		return record
	}
	put := func(path, body, etag string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		t.Helper()
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(requestCtx, "PUT", "http://routex.test"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"`+etag+`"`)
		request.AddCookie(cookie)
		request.Header.Set("X-CSRF-Token", csrf)
		request.Header.Set("Origin", "http://routex.test")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	write := func(path, body string) service.LimitRecord {
		t.Helper()
		before := get(path, adminCookie)
		expectStatus(t, put(path, body, before.ETag, adminCookie, adminSession.CSRFToken), 200)
		after := get(path, adminCookie)
		if !after.Enforced {
			t.Fatal("saved mode not applied")
		}
		return after
	}
	initial := get(childPath, memberCookie)
	if initial.Stored.TokensMonthBehavior != "stop" || initial.Stored.MoneyMonthBehavior != "stop" || len(initial.IPPolicies) != 2 || initial.AccountID == "" || len(initial.EditableFields) != 0 {
		t.Fatal("member defaults/own read projection", initial)
	}
	tokenView := get(childPath, tokenCookie)
	moneyView := get(childPath, moneyCookie)
	if !slices.Contains(tokenView.EditableFields, "tokens_month_behavior") || slices.Contains(tokenView.EditableFields, "money_month_behavior") || !slices.Contains(moneyView.EditableFields, "money_month_behavior") {
		t.Fatal("independent editable modes")
	}
	expectStatus(t, put(childPath, `{"money_month_behavior":"alert_only","reason":"Denied dimension"}`, tokenView.ETag, tokenCookie, tokenCSRF), 403)
	expectStatus(t, put(childPath, `{"tokens_month_behavior":"alert_only","reason":"Denied dimension"}`, moneyView.ETag, moneyCookie, moneyCSRF), 403)
	expectStatus(t, put(childPath, `{"tokens_month_behavior":"alert_only","reason":"Read is not write"}`, initial.ETag, memberCookie, memberCSRF), 403)
	for _, bad := range []string{`{"tokens_month_behavior":null,"reason":"bad"}`, `{"tokens_month_behavior":"ALERT_ONLY","reason":"bad"}`, `{"tokens_month_behavior":"stop","tokens_month_behavior":"alert_only","reason":"bad"}`} {
		expectStatus(t, put(childPath, bad, get(childPath, adminCookie).ETag, adminCookie, adminSession.CSRFToken), 400)
	}
	stale := get(childPath, adminCookie)
	write(childPath, `{"tokens_month":0,"tokens_month_behavior":"alert_only","reason":"Soft member tokens"}`)
	expectStatus(t, put(childPath, `{"tokens_month_behavior":"stop","reason":"Stale mode"}`, stale.ETag, adminCookie, adminSession.CSRFToken), 409)
	write(childPath, `{"rpm":20,"reason":"Sparse rate patch preserves member mode"}`)
	if get(childPath, adminCookie).Stored.TokensMonthBehavior != "alert_only" {
		t.Fatal("sparse patch reset member mode")
	}

	auditBefore := get(childPath, adminCookie)
	auditFailure.Store(true)
	expectStatus(t, put(childPath, `{"money_month_behavior":"alert_only","reason":"Audit rollback"}`, auditBefore.ETag, adminCookie, adminSession.CSRFToken), 500)
	auditFailure.Store(false)
	auditAfter := get(childPath, adminCookie)
	if auditAfter.ETag != auditBefore.ETag || !reflect.DeepEqual(auditAfter.Stored, auditBefore.Stored) {
		t.Fatal("failed typed audit committed policy")
	}
	uncertain := `{"money_month_behavior":"alert_only","reason":"Exact original member publication intent"}`
	publicationFailure.Store(true)
	expectStatus(t, put(childPath, uncertain, auditAfter.ETag, adminCookie, adminSession.CSRFToken), 503)
	publicationFailure.Store(false)
	if observed := get(childPath, adminCookie); observed.Stored.MoneyMonthBehavior != "alert_only" || observed.Enforced {
		t.Fatal("uncertain commit falsely acknowledged")
	}
	expectStatus(t, put(childPath, uncertain, auditAfter.ETag, adminCookie, adminSession.CSRFToken), 200)
	if !get(childPath, adminCookie).Enforced {
		t.Fatal("explicit exact retry not applied")
	}
	write(childPath, `{"money_month_behavior":"stop","reason":"Return money to hard baseline"}`)
	write(path, `{"tokens_month":10,"reason":"Hard Team ceiling"}`)
	native := func(want int) {
		t.Helper()
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(requestCtx, "POST", "http://routex.test/api/v1/teams/"+team.ID+"/chat/completions", strings.NewReader(body))
		req.AddCookie(memberCookie)
		req.Header.Set("X-CSRF-Token", memberCSRF)
		req.Header.Set("Origin", "http://routex.test")
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		expectStatus(t, response, want)
		if want == 429 {
			var failure struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &failure); err != nil || failure.Error.Code != "quota_exceeded" {
				t.Fatal("quota denial code", err)
			}
		}
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	native(200) // soft member0 does not reject the otherwise valid hard Team10 request.
	write(path, `{"tokens_month":0,"reason":"Hard Team zero"}`)
	native(429)
	write(path, `{"tokens_month_behavior":"alert_only","reason":"Soft Team threshold"}`)
	write(childPath, `{"tokens_month_behavior":"stop","reason":"Hard member zero"}`)
	native(429)
	write(childPath, `{"tokens_month_behavior":"alert_only","money_month":"0","currency":"USD","money_month_behavior":"stop","reason":"Independent hard member money"}`)
	native(429)
	write(childPath, `{"money_month_behavior":"alert_only","reason":"Independent member money alert"}`)
	native(200)
	write(path, `{"tokens_5h":0,"reason":"Rolling still hard"}`)
	native(429)
	write(path, `{"tokens_5h":null,"reason":"Clear rolling before stable rejoin"}`)
	saved := get(childPath, memberCookie)
	if saved.QuotaUsage == nil || saved.QuotaUsage.Month.TokensUsed != 10 || saved.QuotaUsage.Month.MoneyUsed["USD"] != "10" {
		t.Fatal("settled usage not independent", saved.QuotaUsage)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", childPath, "", memberCookie, ""), 404)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	rejoined := get(childPath, memberCookie)
	if rejoined.AccountID != saved.AccountID || rejoined.Stored.TokensMonthBehavior != "alert_only" || rejoined.Stored.MoneyMonthBehavior != "alert_only" || rejoined.QuotaUsage.Month.TokensUsed != 10 {
		t.Fatal("rejoin reset modes/usage/account")
	}
	native(200)
	var callsBefore []entity.CallRecord
	var attemptsBefore []entity.CallAttempt
	if err := db.Order("request_id").Find(&callsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&attemptsBefore).Error; err != nil {
		t.Fatal(err)
	}
	if len(callsBefore) != 8 || len(attemptsBefore) != 4 || nativePosts.Load() != 4 {
		t.Fatal("exact native/record budget", len(callsBefore), len(attemptsBefore), nativePosts.Load())
	}
	denied := 0
	success := 0
	for _, call := range callsBefore {
		if call.TeamID == "" {
			continue
		}
		if call.UserID != member.User.ID || call.ModelID != modelID || call.ProjectID != "" || call.KeyID != "" {
			t.Fatal("Team call scope crossed")
		}
		if call.Status == "error" {
			denied++
			if !projectBehaviorBlockedFact(call, attemptsBefore, "quota_exceeded") {
				t.Fatal("denial fabricated usage/attempt")
			}
		} else {
			success++
			if call.ChargeAmount == nil || *call.ChargeAmount != "5" || call.ChargeCurrency == nil || *call.ChargeCurrency != "USD" {
				t.Fatal("exact settled charge")
			}
		}
	}

	for _, attempt := range attemptsBefore {
		if attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown || attempt.CredentialID != "crd_team_behavior" || attempt.SnapshotID == "" || attempt.ConnectionID != "con_team_behavior" || attempt.ProviderModelID != "pmd_team_behavior" {
			t.Fatal("exact native completion/credential/snapshot changed")
		}
	}
	personal, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 || len(personal.QuotaUsage.Month.MoneyUsed) != 0 {
		t.Fatal("Team calls borrowed Personal accounting", err)
	}
	if denied != 4 || success != 3 {
		t.Fatal("Team success/denial split", denied, success)
	}
	parentBefore := get(path, adminCookie)
	type sessionFact struct {
		ID, UserID           string
		ExpiresAt, CreatedAt time.Time
	}
	var sessionsBefore []sessionFact
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&sessionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
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
	after := get(childPath, memberCookie)
	if !after.Enforced || after.AccountID != saved.AccountID || after.Stored.TokensMonthBehavior != "alert_only" || after.Stored.MoneyMonthBehavior != "alert_only" || after.QuotaUsage.Month.TokensUsed != 15 || after.QuotaUsage.Month.MoneyUsed["USD"] != "15" {
		t.Fatal("original Session/restart accounting", after)
	}
	parentAfter := get(path, adminCookie)
	var sessionsAfter []sessionFact
	if err := db.Model(&entity.Session{}).Select("id,user_id,expires_at,created_at").Order("id").Scan(&sessionsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parentBefore.Stored, parentAfter.Stored) || parentAfter.QuotaUsage.Month.TokensUsed != 15 || parentAfter.QuotaUsage.Month.MoneyUsed["USD"] != "15" || !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
		t.Fatal("restart changed independent aggregate policy/accounting or original Sessions")
	}
	var callsAfter []entity.CallRecord
	var attemptsAfter []entity.CallAttempt
	if err := db.Order("request_id").Find(&callsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&attemptsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(callsBefore, callsAfter) || !reflect.DeepEqual(attemptsBefore, attemptsAfter) || nativePosts.Load() != 4 {
		t.Fatal("restart rewrote calls/attempts or dispatched")
	}
}
