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
	"slices"
	"strings"
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

// Proposed case 135. One real coverage warmup precedes Team birth, then six Team
// probes produce three native completions and three independent quota denials.
func testTeamMonthlyBehaviorLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
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
	admin, err := svc.Initialize(ctx, "team-behavior-admin@example.invalid", "test-only-team-behavior-password", "Team behavior admin")
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	adminSession, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-behavior-admin@example.invalid","password":"test-only-team-behavior-password"}`, nil, ""))
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
	initial := get(path, memberCookie)
	if initial.Stored.TokensMonthBehavior != "stop" || initial.Stored.MoneyMonthBehavior != "stop" || len(initial.EditableFields) != 0 {
		t.Fatal("legacy Team defaults/reader authority changed", initial)
	}
	tokenView := get(path, tokenCookie)
	moneyView := get(path, moneyCookie)
	if !slices.Contains(tokenView.EditableFields, "tokens_month_behavior") || slices.Contains(tokenView.EditableFields, "money_month_behavior") || !slices.Contains(moneyView.EditableFields, "money_month_behavior") {
		t.Fatal("independent mode authority")
	}
	expectStatus(t, put(path, `{"money_month_behavior":"alert_only","reason":"Forbidden money mode"}`, tokenView.ETag, tokenCookie, tokenCSRF), 403)
	expectStatus(t, put(path, `{"tokens_month_behavior":"alert_only","reason":"Forbidden Token mode"}`, moneyView.ETag, moneyCookie, moneyCSRF), 403)
	expectStatus(t, put(path, `{"tokens_month_behavior":"alert_only","reason":"Read is not write"}`, initial.ETag, memberCookie, memberCSRF), 403)
	for _, bad := range []string{`{"tokens_month_behavior":null,"reason":"bad"}`, `{"tokens_month_behavior":"ALERT_ONLY","reason":"bad"}`, `{"tokens_month_behavior":"stop","tokens_month_behavior":"alert_only","reason":"bad"}`} {
		expectStatus(t, put(path, bad, get(path, adminCookie).ETag, adminCookie, adminSession.CSRFToken), 400)
	}
	expectStatus(t, put(childPath, `{"tokens_month_behavior":"stop","reason":"Child must stay hard"}`, get(childPath, adminCookie).ETag, adminCookie, adminSession.CSRFToken), 400)
	reviewed := get(path, tokenCookie)
	exact := `{"tokens_month_behavior":"alert_only","reason":"Reviewed independent Team Token mode"}`
	expectStatus(t, put(path, exact, reviewed.ETag, tokenCookie, tokenCSRF), 200)
	stale := get(path, adminCookie)
	write(path, `{"rpm":20,"reason":"Fresh concurrent rate"}`)
	expectStatus(t, put(path, `{"tokens_month_behavior":"stop","reason":"Stale mode edit"}`, stale.ETag, adminCookie, adminSession.CSRFToken), 409)
	if get(path, adminCookie).Stored.TokensMonthBehavior != "alert_only" {
		t.Fatal("sparse rate edit reset mode")
	}
	var failPublication atomic.Bool
	callback := "test_team_monthly_behavior_publication"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled Team mode publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		failPublication.Store(false)
		_ = db.Callback().Query().Remove(callback)
		if err := svc.RefreshRuntime(ctx); err != nil {
			t.Error(err)
		}
	}()
	before := get(path, adminCookie)
	uncertain := `{"money_month_behavior":"alert_only","reason":"Immutable uncertain Team money mode"}`
	failPublication.Store(true)
	expectStatus(t, put(path, uncertain, before.ETag, adminCookie, adminSession.CSRFToken), 503)
	savedUnknown := get(path, adminCookie)
	if savedUnknown.Enforced || savedUnknown.Stored.MoneyMonthBehavior != "alert_only" {
		t.Fatal("matching configuration claimed application", savedUnknown)
	}
	var auditCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", team.ID, "limits.update").Count(&auditCount).Error; err != nil {
		t.Fatal(err)
	}
	failPublication.Store(false)
	expectStatus(t, put(path, uncertain, before.ETag, adminCookie, adminSession.CSRFToken), 200)
	var afterCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_id = ? AND action = ?", team.ID, "limits.update").Count(&afterCount).Error; err != nil || afterCount != auditCount {
		t.Fatal("retry duplicated audit", err)
	}
	write(path, `{"tokens_month":0,"tokens_month_behavior":"alert_only","money_month":"10.000000000000000002","currency":"USD","money_month_behavior":"stop","reason":"Soft aggregate Tokens with hard money"}`)
	write(childPath, `{"tokens_month":10,"reason":"Hard child ceiling"}`)
	child := get(childPath, memberCookie)
	if child.Stored.TokensMonthBehavior != "" || child.IPPolicies[0].TokensMonthBehavior != "alert_only" {
		t.Fatal("mode chain merged or child softened")
	}
	native := func(want int) {
		t.Helper()
		requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		request := httptest.NewRequestWithContext(requestCtx, "POST", "http://routex.test/api/v1/teams/"+team.ID+"/chat/completions", strings.NewReader(body))
		request.AddCookie(memberCookie)
		request.Header.Set("X-CSRF-Token", memberCSRF)
		request.Header.Set("Origin", "http://routex.test")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		expectStatus(t, response, want)
		if err := svc.FlushCallRecorder(ctx); err != nil {
			t.Fatal(err)
		}
	}
	native(200)
	native(200)
	posts := nativePosts.Load()
	native(429)
	if nativePosts.Load() != posts {
		t.Fatal("hard child denial dispatched")
	}
	write(path, `{"tokens_month_behavior":"stop","money_month_behavior":"alert_only","reason":"Hard aggregate Token ceiling"}`)
	native(429)
	write(path, `{"tokens_month_behavior":"alert_only","money_month":"0","currency":"USD","money_month_behavior":"stop","reason":"Hard aggregate money ceiling"}`)
	native(429)
	if nativePosts.Load() != posts {
		t.Fatal("independent aggregate hard denial dispatched")
	}
	write(path, `{"money_month_behavior":"alert_only","reason":"Both aggregate monthly dimensions soft"}`)
	write(childPath, `{"tokens_month":null,"reason":"Clear child Token ceiling"}`)
	native(200)
	if nativePosts.Load() != 4 {
		t.Fatal("expected one warmup plus three native Team completions", nativePosts.Load())
	}
	final := get(path, adminCookie)
	if final.QuotaUsage == nil || final.QuotaUsage.Month == nil || !final.QuotaUsage.Month.Covered || final.QuotaUsage.Month.TokensUsed != 15 || final.QuotaUsage.Month.MoneyUsed["USD"] != "15" || final.QuotaUsage.Month.TokensUnknown != 0 || final.QuotaUsage.Month.MoneyUnknown != 0 || final.QuotaUsage.Active.TokensHeld != 0 {
		t.Fatal("settled aggregate facts", final.QuotaUsage)
	}
	var calls []entity.CallRecord
	if err := db.Where("team_id = ?", team.ID).Order("request_id").Find(&calls).Error; err != nil || len(calls) != 6 {
		t.Fatal("exact six Team logical probes", err, len(calls))
	}
	successes := 0
	for _, call := range calls {
		if call.UserID != member.User.ID || call.TeamMembershipID != membership.ID || call.ModelID != modelID || call.ProjectID != "" || call.KeyID != "" {
			t.Fatal("Team durable attribution changed")
		}
		if call.Status == "success" {
			successes++
			if call.InputTokens == nil || *call.InputTokens != 4 || call.OutputTokens == nil || *call.OutputTokens != 1 || call.ChargeAmount == nil || *call.ChargeAmount != "5" || call.ChargeCurrency == nil || *call.ChargeCurrency != "USD" {
				t.Fatal("known Team completion pricing/usage changed")
			}
		} else if call.Status != "error" || call.ErrorCode != "quota_exceeded" {
			t.Fatal("expected exact quota denial", call.Status, call.ErrorCode)
		}
	}
	if successes != 3 {
		t.Fatal("Team completion count", successes)
	}
	var attempts []entity.CallAttempt
	if err := db.Order("id").Find(&attempts).Error; err != nil || len(attempts) != 4 {
		t.Fatal("denials created attempts", err, len(attempts))
	}
	for _, attempt := range attempts {
		if attempt.Status != "success" || attempt.NativeCompletionEvidence != "completed" || !attempt.FinalUsageKnown || attempt.CredentialID != "crd_team_behavior" || attempt.SnapshotID == "" || attempt.ConnectionID != "con_team_behavior" || attempt.ProviderModelID != "pmd_team_behavior" {
			t.Fatal("exact completed native attempt attribution changed")
		}
	}
	personal, err := svc.GetResourceLimit(ctx, admin.User.ID, service.LimitTarget{Kind: "user", ID: member.User.ID})
	if err != nil || personal.QuotaUsage == nil || personal.QuotaUsage.Month == nil || personal.QuotaUsage.Month.TokensUsed != 0 || len(personal.QuotaUsage.Month.MoneyUsed) != 0 {
		t.Fatal("Team inference debited Personal account", err)
	}
	// Same journal/database and original admin/member Sessions after process service restart.
	svc.StopRuntime()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	restarted := makeService()
	if err := restarted.StartCallRecorder(ctx, spool); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	restarted.StopRuntime()
	router = fox.New()
	New(restarted).RegisterRoutes(router)
	identity := identityRequest(router, "GET", "/api/v1/auth/session", "", memberCookie, "")
	expectStatus(t, identity, 200)
	after := get(path, adminCookie)
	if after.Stored.TokensMonthBehavior != "alert_only" || after.Stored.MoneyMonthBehavior != "alert_only" || after.QuotaUsage.Month.TokensUsed != 15 || after.QuotaUsage.Month.MoneyUsed["USD"] != "15" || !after.Enforced {
		t.Fatal("restart lost modes/facts/current application")
	}
	if nativePosts.Load() != 4 {
		t.Fatal("restart replayed native work")
	}
}
