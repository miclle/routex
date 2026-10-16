package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Extend the existing real-driver Insights scenario and controlled upstream.
// No fabricated historical settlements or additional runtime process are used.
func testInsightsQuotaIntegration(t *testing.T, ctx context.Context, db *gorm.DB, svc *service.Service, router http.Handler, adminID, modelID string, native insightsAITeamNativeWitness) {
	t.Helper()
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"insights-admin@example.invalid","password":"test-only-insights-password"}`, nil, "")
	expectStatus(t, login, 200)
	admin, adminCookie := readIdentity(t, login)
	if admin.User.ID != adminID {
		t.Fatal("quota fixture changed actor")
	}
	analyst, err := svc.CreateMember(ctx, adminID, "quota-analyst@example.invalid", "quota-analysis-password", "Quota analyst", entity.RoleMember)
	if err != nil {
		t.Fatalf("quota fixture create analyst: %v", err)
	}
	role, err := svc.CreateRoleWithDescription(ctx, adminID, "Quota analyst", "Read-only analysis", []string{"calls.read_all"})
	if err != nil {
		t.Fatalf("quota fixture create analyst role: %v", err)
	}
	if _, err = svc.SetMemberRoles(ctx, adminID, analyst.User.ID, []string{role.Role.ID}); err != nil {
		t.Fatalf("quota fixture assign analyst role: %v", err)
	}
	analystLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"quota-analyst@example.invalid","password":"quota-analysis-password"}`, nil, "")
	expectStatus(t, analystLogin, 200)
	analystSession, analystCookie := readIdentity(t, analystLogin)
	team, err := svc.CreateResource(ctx, adminID, service.TeamResource, "=Quota Team", "", []string{adminID, analyst.User.ID})
	if err != nil {
		t.Fatalf("quota fixture create shared Team: %v", err)
	}
	privateTeam, err := svc.CreateResource(ctx, adminID, service.TeamResource, "Private quota Team", "", []string{adminID})
	if err != nil {
		t.Fatalf("quota fixture create private Team: %v", err)
	}
	if _, err = svc.SetResourceModels(ctx, adminID, service.TeamResource, team.ID, []string{modelID}); err != nil {
		t.Fatalf("quota fixture grant Team model: %v", err)
	}
	managers := []string{analyst.User.ID}
	project, err := svc.CreateProject(ctx, adminID, service.ProjectCreationInput{Name: "Quota Project", ManagerIDs: &managers})
	if err != nil {
		t.Fatalf("quota fixture create managed Project: %v", err)
	}
	privateProject, err := svc.CreateProject(ctx, adminID, service.ProjectCreationInput{Name: "Private quota Project"})
	if err != nil {
		t.Fatalf("quota fixture create private Project: %v", err)
	}
	key, err := svc.CreatePersonalKey(ctx, adminID, "=Quota Key", []string{modelID}, nil)
	if err != nil {
		t.Fatalf("quota fixture create personal Key: %v", err)
	}
	if _, err = svc.ConfirmKeyDelivery(ctx, adminID, key.Record.Key.ID); err != nil {
		t.Fatalf("quota fixture confirm Key delivery: %v", err)
	}
	if err = svc.RefreshRuntime(ctx); err != nil {
		t.Fatalf("quota fixture refresh runtime: %v", err)
	}
	request := func(cookie *http.Cookie, csrf, method, path, body, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://routex.test"+path, strings.NewReader(body)).WithContext(ctx)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	// These actual native requests settle14 Tokens each after accounting was
	// activated by the earlier scenario; both resources have complete birth-to-now
	// monthly coverage, but honestly lack a full168-hour forecast baseline.
	body := `{"model":"private-analysis-model","stream":false,"max_completion_tokens":2048,"messages":[{"role":"system","content":"Review quota."},{"role":"user","content":"Quota sample."}]}`
	expectStatus(t, request(nil, "", "POST", "/v1/chat/completions", body, key.Secret), 200)
	expectStatus(t, request(adminCookie, admin.CSRFToken, "POST", "/api/v1/teams/"+team.ID+"/chat/completions", body, ""), 200)
	if err = svc.FlushCallRecorder(ctx); err != nil {
		t.Fatalf("quota fixture flush call recorder: %v", err)
	}
	if _, err = svc.RotatePersonalKey(ctx, adminID, key.Record.Key.ID); err != nil {
		t.Fatalf("quota fixture rotate personal Key: %v", err)
	}
	cap := int64(5000)
	keyTarget := service.LimitTarget{Kind: "personal_key", ID: key.Record.Key.ID}
	beforeKey, err := svc.GetResourceLimit(ctx, adminID, keyTarget)
	if err != nil {
		t.Fatalf("quota fixture read personal_key limit: %v", err)
	}
	keyPolicy := beforeKey.Stored
	keyPolicy.TokensMonth = &cap
	if _, err = svc.SetResourceLimit(ctx, adminID, keyTarget, beforeKey.ETag, service.LimitInput{Policy: keyPolicy, Reason: "Review local quota analysis cap"}); err != nil {
		t.Fatalf("quota fixture write personal_key limit: %v", err)
	}
	beforeTeam, err := svc.GetTeamResourceLimit(ctx, adminID, team.ID, "")
	if err != nil {
		t.Fatalf("quota fixture read team limit: %v", err)
	}
	if _, err = svc.SetTeamResourceLimit(ctx, adminID, team.ID, "", beforeTeam.ETag, service.TeamLimitInput{Reason: "Review local quota analysis cap", Fields: map[string]json.RawMessage{"tokens_month": json.RawMessage("5000")}}); err != nil {
		t.Fatalf("quota fixture write team limit: %v", err)
	}
	keyPath := "/api/v1/admin/insights/quota/keys?dimension=tokens&scope_kind=member&scope_id=" + url.QueryEscape(adminID)
	teamPath := "/api/v1/admin/insights/quota/teams?dimension=tokens"
	projectPath := "/api/v1/admin/insights/quota/keys?dimension=tokens&scope_kind=project&scope_id=" + url.QueryEscape(project.ID)
	read := func(t *testing.T, cookie *http.Cookie, path string) service.InsightsQuotaReport {
		t.Helper()
		res := request(cookie, "", "GET", path, "", "")
		expectStatus(t, res, 200)
		if res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("quota response lost private headers", res.Header())
		}
		var report service.InsightsQuotaReport
		if err := json.Unmarshal(res.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(res.Body.String(), key.Secret) || strings.Contains(res.Body.String(), key.Record.Key.Prefix) || strings.Contains(res.Body.String(), "token_hash") || strings.Contains(res.Body.String(), "input_tokens") {
			t.Fatal("quota report exposed Key secret or raw Call fields")
		}
		return report
	}
	t.Run("quota_reports_settled_local_cap_and_roots", func(t *testing.T) {
		report := read(t, adminCookie, keyPath)
		if len(report.Items) != 1 {
			t.Fatal("rotation lineage must count one account", report)
		}
		row := report.Items[0]
		if row.ID != key.Record.Key.ID || row.SharedKeyCount != 2 || row.LocalCap == nil || *row.LocalCap != "5000" || row.MonthSettled == nil || *row.MonthSettled != "14" || !row.MonthCovered || row.BaselineCovered || row.EstimateAt != nil || row.EstimateStatus != "incomplete_coverage" || *row.UtilizationPercent != "0.28" {
			t.Fatal("settled local root facts or honest short history changed", row)
		}
		if report.AsOf == nil || report.MonthReset == nil || report.BaselineBasis != "settled_admissions_168h" {
			t.Fatal("coherent frame missing", report)
		}
		var root entity.APIKey
		if err := db.Where("id = ?", key.Record.Key.ID).Take(&root).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.APIKey{}).Where("id = ?", root.ID).UpdateColumn("created_at", root.CreatedAt.Add(time.Microsecond)).Error; err != nil {
			t.Fatal(err)
		}
		changedBirth := read(t, adminCookie, keyPath).Items[0]
		if err := db.Model(&entity.APIKey{}).Where("id = ?", root.ID).UpdateColumn("created_at", root.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
		if changedBirth.EstimateStatus != "unavailable" || changedBirth.MonthSettled != nil || changedBirth.UtilizationPercent != nil {
			t.Fatal("a different SQL birth borrowed an old journal identity", changedBirth)
		}
		teams := read(t, analystCookie, teamPath)
		if len(teams.Items) != 1 || teams.Items[0].ID != team.ID || teams.Items[0].MonthSettled == nil || *teams.Items[0].MonthSettled != "14" || strings.Contains(string(mustJSON(teams)), privateTeam.ID) {
			t.Fatal("readable Team scope escaped membership", teams)
		}
		money := read(t, analystCookie, strings.Replace(teamPath, "dimension=tokens", "dimension=money", 1))
		if money.Dimension != "money" || money.Currency == "" || money.Items[0].EstimateAt != nil || money.Items[0].MonthUnknown == nil || *money.Items[0].MonthUnknown != "1" {
			t.Fatal("unknown native money must stay explicit", money)
		}
		// The parent has independent unknown usage from the earlier canceled
		// call. Its stored cap does not become this root account's denominator.
		parentCap := int64(1000)
		var parent entity.ResourceLimit
		if err := db.Where("scope_kind = ? AND scope_id = ?", "user", adminID).Take(&parent).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", adminID).UpdateColumn("tokens_month", parentCap).Error; err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", adminID).UpdateColumn("tokens_month", parent.TokensMonth).Error; err != nil {
				t.Error(err)
			}
		}()
		if got := read(t, adminCookie, keyPath).Items[0]; *got.LocalCap != "5000" || *got.MonthSettled != "14" {
			t.Fatal("parent cap contaminated local account", got)
		}
	})
	t.Run("quota_reports_scope_authority_and_export", func(t *testing.T) {
		expectStatus(t, request(nil, "", "GET", teamPath, "", ""), 401)
		expectStatus(t, request(analystCookie, "", "GET", keyPath, "", ""), 403)
		if report := read(t, analystCookie, projectPath); len(report.Items) != 0 {
			t.Fatal("empty authorized scope invented usage", report)
		}
		deniedProject := strings.Replace(projectPath, project.ID, privateProject.ID, 1)
		expectStatus(t, request(analystCookie, "", "GET", deniedProject, "", ""), 404)
		directoryPermission := entity.RolePermission{RoleID: role.Role.ID, Permission: "projects.read_all"}
		if err := db.Create(&directoryPermission).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(analystCookie, "", "GET", deniedProject, "", ""), 404)
		if err := db.Where("role_id = ? AND permission = ?", directoryPermission.RoleID, directoryPermission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(adminCookie, "", "GET", strings.Replace(keyPath, adminID, strings.ToUpper(adminID), 1), "", ""), 404)
		permission := entity.RolePermission{RoleID: role.Role.ID, Permission: "members.read"}
		if err := db.Create(&permission).Error; err != nil {
			t.Fatal(err)
		}
		read(t, analystCookie, keyPath)
		if err := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(analystCookie, "", "GET", keyPath, "", ""), 403)
		for _, path := range []string{teamPath, keyPath, projectPath} {
			exportPath := strings.Replace(path, "?", "/export.csv?", 1)
			res := request(adminCookie, "", "GET", exportPath, "", "")
			expectStatus(t, res, 200)
			filename := "routex-key-quota.csv"
			if path == teamPath {
				filename = "routex-team-quota.csv"
			}
			if res.Header().Get("Content-Type") != "text/csv; charset=utf-8" || res.Header().Get("Content-Disposition") != `attachment; filename="`+filename+`"` || res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("scoped export headers changed", res.Header())
			}
			rows, err := csv.NewReader(strings.NewReader(res.Body.String())).ReadAll()
			if err != nil || len(rows) < 2 || rows[1][0] != "metadata" || rows[1][11] != "settled_admissions_168h" {
				t.Fatal("export lacks current bounded capture metadata", rows, err)
			}
		}
		first := read(t, adminCookie, teamPath+"&limit=1")
		if len(first.Items) != 1 || first.NextCursor == nil {
			t.Fatal("bounded Team pagination missing", first)
		}
		next := read(t, adminCookie, teamPath+"&limit=1&cursor="+url.QueryEscape(*first.NextCursor))
		if len(next.Items) != 1 || next.Items[0].ID == first.Items[0].ID {
			t.Fatal("Team keyset pagination repeated an account", next)
		}
		expectStatus(t, request(analystCookie, "", "GET", teamPath+"&cursor="+url.QueryEscape(*first.NextCursor), "", ""), 400)
	})
	t.Run("quota_reports_current_actor_and_analysis_authority", func(t *testing.T) {
		permission := entity.RolePermission{RoleID: role.Role.ID, Permission: "calls.read_all"}
		removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal(removed.Error, removed.RowsAffected)
		}
		expectStatus(t, request(analystCookie, "", "GET", teamPath, "", ""), 403)
		expectStatus(t, request(analystCookie, "", "GET", strings.Replace(projectPath, "?", "/export.csv?", 1), "", ""), 403)
		if err := db.Create(&permission).Error; err != nil {
			t.Fatal(err)
		}
		read(t, analystCookie, teamPath)
		membership := db.Model(&entity.TeamMembership{}).Where("team_id = ? AND user_id = ?", team.ID, analyst.User.ID).UpdateColumn("status", entity.ResourceDisabled)
		if membership.Error != nil || membership.RowsAffected != 1 {
			t.Fatal(membership.Error, membership.RowsAffected)
		}
		if report := read(t, analystCookie, teamPath); len(report.Items) != 0 {
			t.Fatal("revoked Team membership retained private rows", report)
		}
		if err := db.Model(&entity.TeamMembership{}).Where("team_id = ? AND user_id = ?", team.ID, analyst.User.ID).UpdateColumn("status", entity.ResourceActive).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", analyst.User.ID).UpdateColumn("disabled", true).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(analystCookie, "", "GET", teamPath, "", ""), 401)
		if err := db.Model(&entity.User{}).Where("id = ?", analyst.User.ID).UpdateColumn("disabled", false).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request(analystCookie, analystSession.CSRFToken, "POST", "/api/v1/auth/logout", "", ""), 204)
		expectStatus(t, request(analystCookie, "", "GET", teamPath, "", ""), 401)
	})
	testInsightsAITeamQuotaIntegration(t, ctx, db, svc, router, adminID, analyst.User.ID, role.Role.ID, team.ID, privateTeam.ID, modelID, native)
	testInsightsAIKeyQuotaIntegration(t, ctx, db, svc, router, adminID, analyst.User.ID, role.Role.ID, project.ID, privateProject.ID, key.Record.Key.ID, modelID, native)
	testInsightsQuotaBatchIntegration(t, db, adminID, privateTeam.ID, func(path string) *httptest.ResponseRecorder {
		return request(adminCookie, "", "GET", path, "", "")
	})
}
