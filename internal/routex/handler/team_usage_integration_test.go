package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// The sole integration runner supplies a fresh migrated PostgreSQL or MySQL database.
func testTeamUsageLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-usage-admin@example.invalid","password":"test-only-team-usage-password","name":"Usage administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{
		{ID: "usr_team_usage_owner", Email: "team-usage-owner@example.invalid", Name: "Private owner", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_team_usage_member", Email: "team-usage-member@example.invalid", Name: "Private member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_team_usage_reviewer", Email: "team-usage-reviewer@example.invalid", Name: "Usage reviewer", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_team_usage_directory", Email: "team-usage-directory@example.invalid", Name: "Directory reader", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(user entity.User) *http.Cookie {
		t.Helper()
		res := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-team-usage-password"}`, user.Email), nil, "")
		expectStatus(t, res, 200)
		_, cookie := readIdentity(t, res)
		return cookie
	}
	ownerCookie, memberCookie := login(users[0]), login(users[1])
	reviewerCookie, directoryCookie := login(users[2]), login(users[3])
	const teamID = "tea_usage_scope"
	const otherTeamID = "tea_usage_other"
	const emptyTeamID = "tea_usage_empty"
	for _, row := range []any{
		&entity.Team{ID: teamID, Name: "Usage Team", Status: entity.ResourceActive},
		&entity.Team{ID: otherTeamID, Name: "Other Team", Status: entity.ResourceActive},
		&entity.Team{ID: emptyTeamID, Name: "Empty Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_usage_owner", TeamID: teamID, UserID: users[0].ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_usage_member", TeamID: teamID, UserID: users[1].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_usage_other", TeamID: otherTeamID, UserID: users[1].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_usage_empty", TeamID: emptyTeamID, UserID: users[0].ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.Role{ID: "rol_usage_reviewer", Name: "Usage reviewer", NameKey: "usage-reviewer"},
		&entity.Role{ID: "rol_usage_directory", Name: "Directory reader", NameKey: "usage-directory"},
		&entity.RolePermission{RoleID: "rol_usage_reviewer", Permission: "calls.read_all"},
		&entity.RolePermission{RoleID: "rol_usage_directory", Permission: "teams.read_all"},
		&entity.UserRole{UserID: users[2].ID, RoleID: "rol_usage_reviewer"},
		&entity.UserRole{UserID: users[3].ID, RoleID: "rol_usage_directory"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	input, output, zero := int64(10), int64(2), int64(0)
	amountA, amountB, usd, eur, snapshot := "0.1", "0.2", "USD", "EUR", `{"private":"not a public report"}`
	base := service.CallFact{
		RequestID: "req_team_usage_first", UserID: users[0].ID,
		TeamID: teamID, TeamMembershipID: "tmm_removed_historical",
		ModelID: "mdl_team_primary", ModelName: "Recorded primary",
		ProviderID: "prv_team_private", ProviderName: "Private provider",
		ProviderModelID: "pmd_team_private", UpstreamModelName: "Private upstream",
		ConnectionID: "con_team_private", ConnectionName: "Private connection",
		Protocol: entity.ProtocolOpenAIChat, Status: "success",
		StartedAt: started, CompletedAt: started.Add(150 * time.Millisecond),
		InputTokens: &input, OutputTokens: &output,
		Pricing: &service.CallPricing{Status: "priced", Amount: &amountA, Currency: &usd, SnapshotJSON: &snapshot},
	}
	record := func(fact service.CallFact) {
		t.Helper()
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal(err)
		}
	}
	record(base)
	record(base) // Durable delivery replay is one canonical fact, not another request.
	second := base
	second.RequestID, second.UserID, second.TeamMembershipID = "req_team_usage_second", users[1].ID, "tmm_old_rejoined"
	second.ModelID, second.ModelName, second.Stream = "mdl_team_secondary", "Recorded secondary", true
	second.Pricing = &service.CallPricing{Status: "priced", Amount: &amountB, Currency: &usd, SnapshotJSON: &snapshot}
	record(second)
	partial := base
	partial.RequestID, partial.Status, partial.InputTokens, partial.Pricing = "req_team_usage_partial", "error", nil, nil
	record(partial)
	canceled := second
	canceled.RequestID, canceled.Status, canceled.Stream, canceled.Protocol = "req_team_usage_canceled", "canceled", false, entity.ProtocolOpenAIResponses
	canceled.Pricing = &service.CallPricing{Status: "priced", Amount: &amountA, Currency: &eur, SnapshotJSON: &snapshot}
	record(canceled)
	noWork := base
	noWork.RequestID, noWork.Status, noWork.NoWork, noWork.UsageComplete = "req_team_usage_no_work", "error", true, true
	noWork.InputTokens, noWork.OutputTokens, noWork.CacheReadTokens, noWork.CacheWriteTokens = &zero, &zero, &zero, &zero
	noWork.Pricing = &service.CallPricing{Status: "no_work"}
	record(noWork)
	previous := base
	previous.RequestID, previous.StartedAt = "req_team_usage_previous", started.Add(-24*time.Hour)
	previous.CompletedAt = previous.StartedAt.Add(time.Second)
	record(previous)
	other := base
	other.RequestID, other.TeamID, other.UserID = "req_team_usage_other", otherTeamID, users[1].ID
	record(other)
	personal := base
	personal.RequestID, personal.TeamID, personal.TeamMembershipID, personal.KeyID = "req_team_usage_personal", "", "", "key_usage_personal"
	record(personal)
	project := personal
	project.RequestID, project.UserID, project.ProjectID, project.KeyID = "req_team_usage_project", "", "prj_usage_independent", "key_usage_project"
	record(project)
	if err := db.Create(&entity.Model{ID: base.ModelID, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ModelName{Name: "renamed-current-model", ModelID: base.ModelID, CurrentModelID: &base.ModelID}).Error; err != nil {
		t.Fatal(err)
	}
	query := "?from=2026-08-02T00%3A00%3A00Z&to=2026-08-03T00%3A00%3A00Z&granularity=hour&compare=true"
	path := "/api/v1/teams/" + teamID + "/usage"
	read := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		return identityRequest(router, "GET", path, "", cookie, "")
	}
	response := read(path+query, memberCookie)
	report := decodeCatalogResponse[service.UsageReport](t, response, 200)
	assertTeamUsageReport(t, report, teamID)
	assertTeamUsagePrivacy(t, response.Body.String(), append(users, administrator), base)
	ownerReport := decodeCatalogResponse[service.UsageReport](t, read(path+query, ownerCookie), 200)
	if !reflect.DeepEqual(report.Current, ownerReport.Current) || !reflect.DeepEqual(report.Previous, ownerReport.Previous) {
		t.Fatal("owner/member identity changed the Team aggregate")
	}
	for _, suffix := range []string{"&key_id=key_usage_personal", "&user_id=" + users[0].ID, "&project_id=prj_usage_independent", "&team_id=" + teamID, "&provider_id=prv_team_private", "&provider_model_id=pmd_team_private", "&connection_id=con_team_private", "&model_id=mdl_team_primary&model_id=mdl_team_secondary", "&unknown=true", "&model_id="} {
		expectStatus(t, read(path+query+suffix, memberCookie), 400)
	}
	for _, filter := range []struct {
		suffix string
		count  int64
	}{
		{"&model_id=mdl_team_primary", 3}, {"&model_id=mdl_absent", 0},
		{"&stream=true", 1}, {"&status=canceled", 1}, {"&protocol=openai_chat", 4},
	} {
		filtered := decodeCatalogResponse[service.UsageReport](t, read(path+query+filter.suffix, memberCookie), 200)
		if filtered.Current.Summary.Requests != filter.count {
			t.Fatal("Team filter was not applied within the source scope", filter.suffix, filtered.Current.Summary)
		}
	}
	empty := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/teams/"+emptyTeamID+"/usage"+query, ownerCookie), 200)
	if empty.TeamID != emptyTeamID || empty.Current.Summary.Requests != 0 || empty.Current.Summary.Tokens.Total.Value == nil || *empty.Current.Summary.Tokens.Total.Value != "0" || len(empty.Current.Keys) != 0 {
		t.Fatal("empty Team fabricated use or lost its explicit context", empty)
	}
	expectStatus(t, read(path+query, nil), 401)
	for _, cookie := range []*http.Cookie{adminCookie, reviewerCookie, directoryCookie} {
		expectStatus(t, read(path+query, cookie), 404)
	}
	expectStatus(t, read("/api/v1/teams/tea_usage_missing/usage"+query, directoryCookie), 404)
	expectStatus(t, read("/api/v1/teams/"+strings.ToUpper(teamID)+"/usage"+query, memberCookie), 404)
	adminPath := "/api/v1/admin/usage" + query + "&team_id=" + teamID
	adminReport := decodeCatalogResponse[service.UsageReport](t, read(adminPath, reviewerCookie), 200)
	if adminReport.Current.Summary.Requests != 5 || len(adminReport.Current.Keys) != 0 || len(adminReport.Current.Providers) != 1 || !reflect.DeepEqual(adminReport.AvailableDimensions, []string{"model", "provider", "provider_model", "connection"}) {
		t.Fatal("independent platform reviewer lost the exact historical Team filter", adminReport)
	}
	expectStatus(t, read(adminPath, directoryCookie), 403)
	for _, suffix := range []string{"&user_id=" + users[0].ID, "&project_id=prj_usage_independent", "&team_id=" + otherTeamID} {
		expectStatus(t, read(adminPath+suffix, adminCookie), 400)
	}
	aliasAdmin := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/admin/usage"+query+"&team_id="+strings.ToUpper(teamID), adminCookie), 200)
	if aliasAdmin.Current.Summary.Requests != 0 {
		t.Fatal("collated Team alias broadened a platform fact filter")
	}
	personalReport := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query, ownerCookie), 200)
	if personalReport.Current.Summary.Requests != 1 {
		t.Fatal("Team contribution was also counted as Personal use")
	}
	// A separate pool and Service reconstruct the persisted report without runtime
	// state, current contributor relationships or live catalogue names.
	reopened, err := database.Open(ctx, db.Name(), os.Getenv("ROUTEX_TEST_"+strings.ToUpper(db.Name())+"_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	reopenedSQL, err := reopened.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopenedSQL.Close(); err != nil {
			t.Error(err)
		}
	}()
	reopenedService, err := service.New(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}
	from, to := started.Add(-12*time.Hour), started.Add(12*time.Hour)
	rebuilt, err := reopenedService.TeamUsage(ctx, users[1].ID, teamID, service.UsageFilter{From: &from, To: &to, Timezone: "UTC", Granularity: "hour", Compare: true})
	if err != nil || !reflect.DeepEqual(rebuilt.Current, report.Current) || !reflect.DeepEqual(rebuilt.Previous, report.Previous) {
		t.Fatal("independent pool changed durable Team statistics", rebuilt, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	oldMaximum := sqlDB.Stats().MaxOpenConnections
	sqlDB.SetMaxOpenConns(1)
	defer sqlDB.SetMaxOpenConns(oldMaximum)
	oneConnectionContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = svc.TeamUsage(oneConnectionContext, users[1].ID, teamID, service.UsageFilter{From: &from, To: &to, Timezone: "UTC", Granularity: "hour"})
	cancel()
	if err != nil {
		t.Fatal("Team authorization borrowed another connection inside its report snapshot", err)
	}
	sqlDB.SetMaxOpenConns(oldMaximum)
	// Current authority controls access, while historical membership IDs remain facts.
	memberQuery := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_usage_member")
	if err := memberQuery.Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, read(path+query, memberCookie), 404)
	if err := db.Where("id = ?", "tmm_usage_member").Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.TeamMembership{ID: "tmm_usage_rejoined", TeamID: teamID, UserID: users[1].ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	assertTeamUsageReport(t, decodeCatalogResponse[service.UsageReport](t, read(path+query, memberCookie), 200), teamID)
	for _, field := range []string{"team_id", "user_id", "status", "role"} {
		original := map[string]string{"team_id": teamID, "user_id": users[1].ID, "status": entity.ResourceActive, "role": entity.TeamMember}[field]
		mutation := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_usage_rejoined")
		if err := mutation.Update(field, strings.ToUpper(original)).Error; err == nil {
			expectStatus(t, read(path+query, memberCookie), 404)
			if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_usage_rejoined").Update(field, original).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, status := range []string{entity.ResourceDisabled, entity.ResourceArchived} {
		if err := db.Model(&entity.Team{}).Where("id = ?", teamID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, read(path+query, memberCookie), 404)
		history := decodeCatalogResponse[service.UsageReport](t, read(adminPath, reviewerCookie), 200)
		if history.Current.Summary.Requests != 5 {
			t.Fatal("inactive Team erased platform-authorized immutable history")
		}
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).Update("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	for _, updates := range []map[string]any{{"disabled": true}, {"offboarded_at": time.Now().UTC()}} {
		if err := db.Model(&entity.User{}).Where("id = ?", users[1].ID).Updates(updates).Error; err != nil {
			t.Fatal(err)
		}
		if result, err := svc.TeamUsage(ctx, users[1].ID, teamID, service.UsageFilter{From: &from, To: &to}); err == nil || result != nil {
			t.Fatal("disabled/offboarded actor retained service-level Team statistics", result, err)
		}
		if err := db.Model(&entity.User{}).Where("id = ?", users[1].ID).Updates(map[string]any{"disabled": false, "offboarded_at": nil}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// A collated custom-role assignment is never independent platform authority.
	aliasRole := entity.UserRole{UserID: strings.ToUpper(users[3].ID), RoleID: "rol_usage_reviewer"}
	if err := db.Create(&aliasRole).Error; err == nil {
		expectStatus(t, read(adminPath, directoryCookie), 403)
		if err := db.Where("role_id = ? AND user_id = ?", aliasRole.RoleID, aliasRole.UserID).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Alias facts must be filtered before limits, not trimmed after aggregation.
	aliasFact := base
	aliasFact.RequestID, aliasFact.TeamID = "req_team_usage_alias", strings.ToUpper(teamID)
	record(aliasFact)
	assertTeamUsageReport(t, decodeCatalogResponse[service.UsageReport](t, read(path+query, memberCookie), 200), teamID)
	rows := make([]entity.CallRecord, 10001)
	for index := range rows {
		rows[index] = entity.CallRecord{
			RequestID: fmt.Sprintf("req_team_usage_overflow_%d", index),
			UserID:    users[1].ID, TeamID: otherTeamID, TeamMembershipID: "tmm_historical_bulk",
			ModelID: "mdl_team_overflow", ModelName: "Overflow", Protocol: entity.ProtocolOpenAIChat,
			Status: "success", StartedAt: started, CompletedAt: started,
			CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"},
		}
	}
	if err := db.CreateInBatches(&rows, 250).Error; err != nil {
		t.Fatal(err)
	}
	assertTeamUsageReport(t, decodeCatalogResponse[service.UsageReport](t, read(path+query, memberCookie), 200), teamID)
	if err := db.Model(&entity.CallRecord{}).Where("model_id = ?", "mdl_team_overflow").Update("team_id", teamID).Error; err != nil {
		t.Fatal(err)
	}
	overflow := read(path+query+"&model_id=mdl_team_overflow", memberCookie)
	expectStatus(t, overflow, 422)
	if strings.Contains(overflow.Body.String(), `"current"`) {
		t.Fatal("Team overflow returned misleading partial statistics")
	}
	if err := db.Where("team_id = ?", teamID).Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", teamID).Delete(&entity.Team{}).Error; err != nil {
		t.Fatal(err)
	}
	missingCatalogue := decodeCatalogResponse[service.UsageReport](t, read(adminPath+"&model_id=mdl_team_primary", reviewerCookie), 200)
	if missingCatalogue.Current.Summary.Requests != 3 {
		t.Fatal("missing current Team catalogue erased platform-authorized historical facts")
	}
}

func assertTeamUsageReport(t *testing.T, report service.UsageReport, teamID string) {
	t.Helper()
	summary := report.Current.Summary
	if report.TeamID != teamID || !slices.Equal(report.AvailableDimensions, []string{"model"}) || report.Current.Keys == nil || len(report.Current.Keys) != 0 || len(report.Current.Providers) != 0 || report.Previous == nil || report.Previous.Keys == nil || len(report.Previous.Keys) != 0 {
		t.Fatal("Team report lost its safe scoped projection", report)
	}
	if summary.Requests != 5 || summary.Successes != 2 || summary.Errors != 2 || summary.Canceled != 1 || summary.Tokens.Input.Value != nil || summary.Tokens.Input.Known != "30" || summary.Tokens.Input.UnknownCalls != 1 || summary.Tokens.Output.Value == nil || *summary.Tokens.Output.Value != "8" || summary.Tokens.Total.Value != nil || summary.Tokens.Total.Known != "38" || summary.Tokens.Total.UnknownCalls != 1 || summary.UnknownAmountCalls != 1 {
		t.Fatal("Team known and unknown canonical totals were altered", summary)
	}
	if len(summary.Amounts) != 2 || summary.Amounts[0].Currency != "EUR" || summary.Amounts[0].Amount != "0.1" || summary.Amounts[1].Currency != "USD" || summary.Amounts[1].Amount != "0.3" || summary.Amounts[1].Calls != 2 || summary.PricingStatuses["no_work"] != 1 {
		t.Fatal("Team exact historical currencies or no-work classification changed", summary)
	}
	modelNames := map[string]string{}
	for _, model := range report.Current.Models {
		modelNames[model.ID] = model.Name
	}
	if report.Previous.Summary.Requests != 1 || len(report.Current.Trend) != 24 || report.Current.Trend[12].Stats.Requests != 5 || len(report.Current.Models) != 2 || modelNames["mdl_team_primary"] != "Recorded primary" || modelNames["mdl_team_secondary"] != "Recorded secondary" || report.Source != "persisted_call_records" || !report.MayLag || report.LatestCompletedAt == nil {
		t.Fatal("Team range, model or authoritative freshness semantics changed", report)
	}
}

func assertTeamUsagePrivacy(t *testing.T, body string, users []entity.User, fact service.CallFact) {
	t.Helper()
	private := []string{fact.RequestID, fact.TeamMembershipID, fact.ProviderID, fact.ProviderName, fact.ProviderModelID, fact.UpstreamModelName, fact.ConnectionID, fact.ConnectionName, "key_usage_personal", "key_usage_project", `"user_id"`, `"team_membership_id"`, `"providers"`, `"provider_models"`, `"connections"`, `"snapshot_json"`}
	for _, user := range users {
		private = append(private, user.ID, user.Name, user.Email)
	}
	for _, value := range private {
		if strings.Contains(body, value) {
			t.Fatalf("Team aggregate disclosed private contributor or route value %q", value)
		}
	}
}
