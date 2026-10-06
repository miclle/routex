package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testTeamResourceLimitsLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var failPublication atomic.Bool
	callback := "test_team_limits_publication_outage"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "models" {
			_ = tx.AddError(errors.New("controlled policy publication outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-limits-admin@example.invalid","password":"test-only-team-limits-password","name":"Team limits admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	member, memberCookie, memberCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-limit-member", nil)
	other, otherCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "team-limit-other", nil)
	tokenWriter, tokenCookie, tokenCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-limit-token-writer", []string{"teams.tokens.write"})
	_, rateCookie, rateCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-limit-rate-writer", []string{"teams.rates.write"})
	_, moneyCookie, moneyCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "team-limit-money-writer", []string{"teams.money.write"})
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Finite Team", "Controlled Team limits", []string{admin.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	members := []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: member.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}, {UserID: other.User.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	ownerTeam, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Owned Team", "Owner read does not grant policy write", []string{member.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "team-limits.db")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	}()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	path := "/api/v1/teams/" + team.ID + "/limits"
	childPath := "/api/v1/teams/" + team.ID + "/members/" + member.User.ID + "/limits"
	get := func(path string, cookie *http.Cookie) service.LimitRecord {
		response := identityRequest(router, "GET", path, "", cookie, "")
		expectStatus(t, response, 200)
		var result service.LimitRecord
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if response.Header().Get("ETag") != `"`+result.ETag+`"` || len(result.ETag) != 64 || result.TeamID != team.ID && path != "/api/v1/teams/"+ownerTeam.ID+"/limits" {
			t.Fatal("incoherent Team review response", response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"editable_fields":`) {
			t.Fatal("missing explicit field authority")
		}
		return result
	}
	put := func(path, body, etag string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("PUT", "http://routex.test"+path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", `"`+etag+`"`)
		if cookie != nil {
			request.AddCookie(cookie)
		}
		if csrf != "" {
			request.Header.Set("X-CSRF-Token", csrf)
		}
		result := httptest.NewRecorder()
		router.ServeHTTP(result, request)
		return result
	}
	initial := get(path, memberCookie)
	if initial.Kind != "team" || initial.ID != team.ID || len(initial.EditableFields) != 0 || initial.Stored.TokensMonth != nil || !initial.Enforced {
		t.Fatal("incorrect member parent policy", initial)
	}
	child := get(childPath, memberCookie)
	if child.Kind != "team_member" || child.ID != member.User.ID || len(child.AccountID) != 64 || child.ParentETag != initial.ETag {
		t.Fatal("incorrect stable member policy", child)
	}
	get(childPath, adminCookie) // Current scoped owner may review another member.
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/teams/"+strings.ToUpper(team.ID)+"/limits", "", adminCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", childPath, "", otherCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path+"?user_id="+other.User.ID, "", memberCookie, ""), 400)
	expectStatus(t, put(path, `{"tokens_month":100,"reason":"Owner cannot write"}`, initial.ETag, memberCookie, memberCSRF), 403)
	owned := get("/api/v1/teams/"+ownerTeam.ID+"/limits", memberCookie)
	expectStatus(t, put("/api/v1/teams/"+ownerTeam.ID+"/limits", `{"tokens_month":1,"reason":"Owner cannot edit"}`, owned.ETag, memberCookie, memberCSRF), 403)
	minimal := identityRequest(router, "GET", "/api/v1/teams/"+team.ID, "", tokenCookie, "")
	expectStatus(t, minimal, 200)
	for _, forbidden := range []string{"\"members\"", "\"model_ids\"", "\"created_at\"", "\"creator_id\""} {
		if strings.Contains(minimal.Body.String(), forbidden) {
			t.Fatal("limit editor fetched relationship metadata", minimal.Body.String())
		}
	}
	if !strings.Contains(minimal.Body.String(), `"resource_limit_workspace_only":true`) {
		t.Fatal("minimal workspace marker missing")
	}
	token := get(path, tokenCookie)
	if !slices.Equal(token.EditableFields, []string{"tokens_5h", "tokens_7d", "tokens_month", "tokens_month_behavior"}) {
		t.Fatal("token authority broadened", token.EditableFields)
	}
	for _, validator := range []string{"", token.ETag, `W/"` + token.ETag + `"`, `"` + strings.ToUpper(token.ETag) + `"`} {
		request := httptest.NewRequest("PUT", "http://routex.test"+path, strings.NewReader(`{"tokens_month":100,"reason":"Strong reviewed validator"}`))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(tokenCookie)
		request.Header.Set("X-CSRF-Token", tokenCSRF)
		if validator != "" {
			request.Header.Set("If-Match", validator)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		expectStatus(t, response, 400)
	}
	crossOrigin := httptest.NewRequest("PUT", "http://routex.test"+path, strings.NewReader(`{"tokens_month":100,"reason":"Origin-bound review"}`))
	crossOrigin.Header.Set("Content-Type", "application/json")
	crossOrigin.Header.Set("Origin", "https://foreign.example.invalid")
	crossOrigin.Header.Set("If-Match", `"`+token.ETag+`"`)
	crossOrigin.Header.Set("X-CSRF-Token", tokenCSRF)
	crossOrigin.AddCookie(tokenCookie)
	crossResponse := httptest.NewRecorder()
	router.ServeHTTP(crossResponse, crossOrigin)
	expectStatus(t, crossResponse, 403)
	expectStatus(t, put(path, `{"tokens_month":100,"reason":"No CSRF"}`, token.ETag, tokenCookie, ""), 403)
	expectStatus(t, put(path, `{"rpm":1,"reason":"Wrong dimension"}`, token.ETag, tokenCookie, tokenCSRF), 403)
	for _, body := range []string{`{"tokens_month":1,"tokens_month":2,"reason":"Duplicate"}`, `{"tokens_month":-1,"reason":"Negative"}`, `{"ip_mode":"none","reason":"Unsupported"}`, `{"tokens_month":1,"reason":""}`} {
		expectStatus(t, put(path, body, token.ETag, tokenCookie, tokenCSRF), 400)
	}
	expectStatus(t, put(path, `{"tokens_month":100,"reason":"Shared monthly cap"}`, token.ETag, tokenCookie, tokenCSRF), 200)
	token = get(path, tokenCookie)
	child = get(childPath, tokenCookie)
	expectStatus(t, put(childPath, `{"tokens_month":101,"reason":"Beyond parent"}`, child.ETag, tokenCookie, tokenCSRF), 400)
	expectStatus(t, put(childPath, `{"tokens_5h":1,"reason":"No rolling child"}`, child.ETag, tokenCookie, tokenCSRF), 400)
	expectStatus(t, put(childPath, `{"tokens_month":60,"reason":"Member monthly cap"}`, child.ETag, tokenCookie, tokenCSRF), 200)
	// Removal/rejoin changes authority and review, never the saved pair policy/account.
	priorChild := get(childPath, tokenCookie)
	withoutMember := []service.TeamMemberInput{members[0], members[2]}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, withoutMember); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", path, "", memberCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", childPath, "", tokenCookie, ""), 404)
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, members); err != nil {
		t.Fatal(err)
	}
	rejoined := get(childPath, memberCookie)
	if rejoined.AccountID != priorChild.AccountID || rejoined.ETag == priorChild.ETag || rejoined.Stored.TokensMonth == nil || *rejoined.Stored.TokensMonth != 60 {
		t.Fatal("rejoin reset stable policy or accepted obsolete membership review")
	}
	expectStatus(t, put(childPath, `{"tokens_month":55,"reason":"Obsolete member review"}`, priorChild.ETag, tokenCookie, tokenCSRF), 409)
	before := get(childPath, tokenCookie)
	expectStatus(t, put(path, `{"tokens_month":50,"reason":"Reduce shared cap"}`, token.ETag, tokenCookie, tokenCSRF), 200)
	reduced := get(childPath, memberCookie)
	if reduced.Stored.TokensMonth == nil || *reduced.Stored.TokensMonth != 60 || reduced.Effective.TokensMonth == nil || *reduced.Effective.TokensMonth != 50 || !reduced.Enforced {
		t.Fatal("parent reduction rewrote member or failed to narrow", reduced)
	}
	expectStatus(t, put(childPath, `{"tokens_month":40,"reason":"Stale parent review"}`, before.ETag, tokenCookie, tokenCSRF), 409)
	rate := get(path, rateCookie)
	expectStatus(t, put(path, `{"rpm":0,"concurrency":1,"reason":"Independent rate cap"}`, rate.ETag, rateCookie, rateCSRF), 200)
	money := get(path, moneyCookie)
	body := `{"money_month":"1.000000000000000001","currency":"` + money.PlatformCurrency + `","reason":"Exact shared money"}`
	expectStatus(t, put(path, body, money.ETag, moneyCookie, moneyCSRF), 200)
	exact := get(path, memberCookie)
	if exact.Stored.MoneyMonth == nil || *exact.Stored.MoneyMonth != "1.000000000000000001" || exact.Stored.TokensMonth == nil || *exact.Stored.TokensMonth != 50 || exact.Stored.RPM == nil || *exact.Stored.RPM != 0 {
		t.Fatal("independent write erased policy dimensions", exact)
	}
	// A finite money-only replacement saves a new revision and immutable before/after.
	moneyBefore := get(path, moneyCookie)
	var moneyAuditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "team", team.ID).Count(&moneyAuditBefore).Error; err != nil {
		t.Fatal(err)
	}
	moneyIntent := `{"money_month":"1.500000000000000001","currency":"` + money.PlatformCurrency + `","reason":"Revise exact shared money"}`
	expectStatus(t, put(path, moneyIntent, moneyBefore.ETag, moneyCookie, moneyCSRF), 200)
	moneyAfter := get(path, moneyCookie)
	if moneyAfter.ETag == moneyBefore.ETag || moneyAfter.Stored.MoneyMonth == nil || *moneyAfter.Stored.MoneyMonth != "1.500000000000000001" {
		t.Fatal("finite money change was falsely acknowledged as a no-op", moneyAfter)
	}
	expectStatus(t, put(path, moneyIntent, moneyBefore.ETag, moneyCookie, moneyCSRF), 200)
	var moneyAuditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "team", team.ID).Count(&moneyAuditAfter).Error; err != nil || moneyAuditAfter != moneyAuditBefore+1 {
		t.Fatal("finite money change/retry did not audit exactly once", err, moneyAuditBefore, moneyAuditAfter)
	}
	var moneyAudit entity.AuditEvent
	if err := db.Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "team", team.ID).Order("created_at DESC, id DESC").First(&moneyAudit).Error; err != nil {
		t.Fatal(err)
	}
	if moneyAudit.DetailsJSON == nil {
		t.Fatal("money audit omitted immutable details")
	}
	var detail struct {
		Before, After struct {
			MoneyMonth *string `json:"money_month"`
		}
	}
	if err := json.Unmarshal([]byte(*moneyAudit.DetailsJSON), &detail); err != nil || detail.Before.MoneyMonth == nil || *detail.Before.MoneyMonth != "1.000000000000000001" || detail.After.MoneyMonth == nil || *detail.After.MoneyMonth != "1.500000000000000001" {
		t.Fatal("money before/after historical values mutated", err, *moneyAudit.DetailsJSON)
	}
	moneyChild := get(childPath, moneyCookie)
	expectStatus(t, put(childPath, `{"money_month":"2","currency":"`+money.PlatformCurrency+`","reason":"Over shared money"}`, moneyChild.ETag, moneyCookie, moneyCSRF), 400)
	expectStatus(t, put(childPath, `{"money_month":null,"currency":"`+money.PlatformCurrency+`","reason":"Ambiguous currency"}`, moneyChild.ETag, moneyCookie, moneyCSRF), 400)
	// A lifecycle ABA invalidates the captured review even when status returns active.
	old := get(path, tokenCookie)
	disabled, active := entity.ResourceDisabled, entity.ResourceActive
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateResource(ctx, admin.User.ID, service.TeamResource, team.ID, service.ResourceUpdate{Status: &active}); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(path, `{"tokens_month":70,"reason":"Old lifecycle review"}`, old.ETag, tokenCookie, tokenCSRF), 409)
	fresh := get(path, tokenCookie)
	var auditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "team", team.ID).Count(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	intent := `{"tokens_month":70,"reason":"Persisted exact retry"}`
	failPublication.Store(true)
	expectStatus(t, put(path, intent, fresh.ETag, tokenCookie, tokenCSRF), 503)
	failPublication.Store(false)
	expectStatus(t, put(path, intent, fresh.ETag, tokenCookie, tokenCSRF), 200)
	var auditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", "limits.update", "team", team.ID).Count(&auditAfter).Error; err != nil || auditAfter != auditBefore+1 {
		t.Fatal("exact retry duplicated audit", auditBefore, auditAfter, err)
	}
	// Every retry rechecks the exact submitted field authority after persistence.
	next := get(path, tokenCookie)
	failPublication.Store(true)
	intent = `{"tokens_month":80,"reason":"Permission-sensitive retry"}`
	expectStatus(t, put(path, intent, next.ETag, tokenCookie, tokenCSRF), 503)
	failPublication.Store(false)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, tokenWriter.User.ID, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, put(path, intent, next.ETag, tokenCookie, tokenCSRF), 403)
	// Concurrent reviewed writes may save only one distinct target.
	reviewed := get(path, adminCookie)
	outcomes := make(chan int, 2)
	var wait sync.WaitGroup
	for _, target := range []string{"90", "95"} {
		wait.Add(1)
		go func(target string) {
			defer wait.Done()
			response := put(path, `{"tokens_month":`+target+`,"reason":"Concurrent policy"}`, reviewed.ETag, adminCookie, admin.CSRFToken)
			outcomes <- response.Code
		}(target)
	}
	wait.Wait()
	close(outcomes)
	success, conflict := 0, 0
	for status := range outcomes {
		switch status {
		case 200:
			success++
		case 409:
			conflict++
		default:
			t.Fatalf("unexpected concurrent response %d", status)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent policy lost reviewed validator", success, conflict)
	}
}
