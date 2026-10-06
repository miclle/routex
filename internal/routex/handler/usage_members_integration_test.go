package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// Proposed usage_members scenario; the root lifecycle harness owns registration.
// Recorded requests are seeded through the durable recorder, not native inference.
func testUsageMembersLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"usage-members-admin@example.invalid","password":"test-only-usage-members-password","name":"Usage administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{
		{ID: "usr_usage_members_a", Email: "usage-members-a@example.invalid", Name: "Member A", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_usage_members_b", Email: "usage-members-b@example.invalid", Name: "Member B", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_usage_members_out", Email: "usage-members-out@example.invalid", Name: "Outsider", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(user entity.User) *http.Cookie {
		t.Helper()
		response := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-usage-members-password"}`, user.Email), nil, "")
		expectStatus(t, response, 200)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	memberCookie, secondCookie, outsiderCookie := login(users[0]), login(users[1]), login(users[2])
	const teamID = "tea_usage_members"
	const projectID = "prj_usage_members"
	const modelID = "mdl_usage_members"
	const memberID = "tmm_usage_members_a"
	for _, row := range []any{
		&entity.Team{ID: teamID, Name: "Recorded usage", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: memberID, TeamID: teamID, UserID: users[0].ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_usage_members_b", TeamID: teamID, UserID: users[1].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.Project{ID: projectID, Name: "Recorded Project", Status: entity.ResourceActive, CreatorID: users[2].ID},
		&entity.ProjectManager{ID: "pjm_usage_members", ProjectID: projectID, UserID: users[0].ID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	one := int64(1)
	amount, currency, snapshot := "1.000000000000000001", "USD", "{}"
	base := service.CallFact{RequestID: "req_members_team_a", UserID: users[0].ID, TeamID: teamID, TeamMembershipID: memberID, ModelID: modelID, ModelName: "Recorded Model", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started.Add(time.Second), InputTokens: &one, OutputTokens: &one, Pricing: &service.CallPricing{Status: "priced", Amount: &amount, Currency: &currency, SnapshotJSON: &snapshot}}
	record := func(fact service.CallFact) {
		t.Helper()
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal(err)
		}
	}
	record(base)
	record(base) // One immutable RequestID remains one recorded request.
	second := base
	second.RequestID, second.UserID, second.TeamMembershipID, second.Status = "req_members_team_b", users[1].ID, "tmm_historical_removed", "canceled"
	record(second)
	failed := base
	failed.RequestID, failed.TeamMembershipID, failed.Status = "req_members_team_error", "tmm_prior_a", "error"
	record(failed)
	previous := second
	previous.RequestID = "req_members_previous"
	previous.StartedAt = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	previous.CompletedAt = previous.StartedAt.Add(time.Second)
	record(previous)
	end := base
	end.RequestID = "req_members_at_end"
	end.StartedAt = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end.CompletedAt = end.StartedAt.Add(time.Second)
	record(end)
	personal := base
	personal.RequestID, personal.TeamID, personal.TeamMembershipID, personal.KeyID = "req_members_personal", "", "", "key_usage_members"
	record(personal)
	project := personal
	project.RequestID, project.UserID, project.ProjectID = "req_members_project", "", projectID
	record(project)
	query := "?from=2026-09-01T00%3A00%3A00Z&to=2026-10-01T00%3A00%3A00Z&timezone=UTC&granularity=day&compare=true"
	teamPath := "/api/v1/teams/" + teamID + "/usage"
	read := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		return identityRequest(router, "GET", path, "", cookie, "")
	}
	coverage := func(report service.UsageReport, id string, requests, known, unknown int64) {
		t.Helper()
		if report.MemberCountBasis != "distinct_recorded_actors" {
			t.Fatal("missing basis", report.MemberCountBasis)
		}
		for _, group := range report.Current.Models {
			if group.ID == id {
				m := group.Members
				if m == nil || group.Stats.Requests != requests || m.Known != known || m.UnknownCalls != unknown || m.Known+m.UnknownCalls > requests {
					t.Fatal("incorrect recorded coverage", group)
				}
				if unknown == 0 {
					if m.Value == nil || *m.Value != known {
						t.Fatal("complete known count lost", m)
					}
				} else if m.Value != nil {
					t.Fatal("partial coverage asserted total", m)
				}
				return
			}
		}
		t.Fatal("missing Model group", id)
	}
	teamResponse := read(teamPath+query, memberCookie)
	team := decodeCatalogResponse[service.UsageReport](t, teamResponse, 200)
	coverage(team, modelID, 3, 2, 0)
	if team.Previous == nil || len(team.Previous.Models) != 1 || team.Previous.Models[0].Members == nil || team.Previous.Models[0].Members.Value == nil || *team.Previous.Models[0].Members.Value != 1 || team.Previous.Summary.Requests != 1 || !team.Current.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !team.Current.To.Equal(end.StartedAt) || team.Timezone != "UTC" {
		t.Fatal("period coverage changed", team)
	}
	if team.Current.Summary.Successes != 1 || team.Current.Summary.Errors != 1 || team.Current.Summary.Canceled != 1 || len(team.Current.Summary.Amounts) != 1 || team.Current.Summary.Amounts[0].Amount != "3.000000000000000003" {
		t.Fatal("member coverage changed recorded status or money", team.Current.Summary)
	}
	checkPrivacy := func(response *httptest.ResponseRecorder) {
		t.Helper()
		var wire map[string]json.RawMessage
		if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{users[0].ID, users[1].ID, users[2].ID, `"user_id"`, `"team_membership_id"`, `"actors"`} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("private actor attribution escaped", private)
			}
		}
		for _, periodName := range []string{"current", "previous"} {
			if len(wire[periodName]) == 0 {
				continue
			}
			var period map[string]json.RawMessage
			if err := json.Unmarshal(wire[periodName], &period); err != nil {
				t.Fatal(err)
			}
			var models []map[string]json.RawMessage
			if err := json.Unmarshal(period["models"], &models); err != nil {
				t.Fatal(err)
			}
			for _, model := range models {
				var m map[string]json.RawMessage
				if err := json.Unmarshal(model["members"], &m); err != nil {
					t.Fatal(err)
				}
				if len(m) != 3 || m["value"] == nil || m["known"] == nil || m["unknown_calls"] == nil {
					t.Fatal("unexpected member fields", m)
				}
			}
			for _, field := range []string{"summary", "trend", "keys", "providers", "provider_models", "connections"} {
				if strings.Contains(string(period[field]), `"members"`) {
					t.Fatal("non-Model coverage exposed", field)
				}
			}
		}
	}
	checkPrivacy(teamResponse)
	coverage(decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query, memberCookie), 200), modelID, 1, 1, 0)
	empty := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query, outsiderCookie), 200)
	if empty.MemberCountBasis != "distinct_recorded_actors" || empty.Current.Summary.Requests != 0 || len(empty.Current.Models) != 0 {
		t.Fatal("authoritative empty Personal report changed", empty)
	}
	projectResponse := read("/api/v1/projects/"+projectID+"/usage"+query, memberCookie)
	coverage(decodeCatalogResponse[service.UsageReport](t, projectResponse, 200), modelID, 1, 0, 1)
	checkPrivacy(projectResponse)
	expectStatus(t, read("/api/v1/projects/"+projectID+"/usage"+query, outsiderCookie), 404)
	expectStatus(t, read(teamPath+query, adminCookie), 404) // Platform authority is not Team membership.
	expectStatus(t, read(teamPath+query, outsiderCookie), 404)
	expectStatus(t, read("/api/v1/teams/"+strings.ToUpper(teamID)+"/usage"+query, memberCookie), 404)
	expectStatus(t, read("/api/v1/admin/usage"+query, memberCookie), 403)
	platformResponse := read("/api/v1/admin/usage"+query, adminCookie)
	coverage(decodeCatalogResponse[service.UsageReport](t, platformResponse, 200), modelID, 5, 2, 1)
	checkPrivacy(platformResponse)
	// Current membership controls reads; a deleted relationship never rewrites recorded callers.
	if err := db.Where("id = ?", memberID).Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, read(teamPath+query, memberCookie), 404)
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query, secondCookie), 200), modelID, 3, 2, 0)
	if err := db.Create(&entity.TeamMembership{ID: "tmm_usage_members_rejoin", TeamID: teamID, UserID: users[0].ID, Role: entity.TeamOwner, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query, memberCookie), 200), modelID, 3, 2, 0)
	// Exact safe historical identities need no current directory row; aliases are distinct strings.
	ghost := base
	ghost.RequestID, ghost.UserID = "req_members_ghost", "usr_missing_directory"
	record(ghost)
	alias := base
	alias.RequestID, alias.UserID = "req_members_case", "usr_usage_members_A"
	record(alias)
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query, memberCookie), 200), modelID, 5, 4, 0)
	invalid := base
	invalid.RequestID, invalid.UserID = "req_members_invalid", "USR_usage_members_a"
	record(invalid)
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query, memberCookie), 200), modelID, 6, 4, 1)
	legacy := entity.CallRecord{RequestID: "req_members_legacy", TeamID: teamID, TeamMembershipID: "tmm_historical_unknown", UserID: "historical_unknown_actor", ModelID: "", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: started, CompletedAt: started, CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query, memberCookie), 200), "", 1, 0, 1)
	modelAlias := base
	modelAlias.RequestID, modelAlias.ModelID = "req_members_model_alias", strings.ToUpper(modelID)
	record(modelAlias)
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query+"&model_id="+modelID, memberCookie), 200), modelID, 6, 4, 1)
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+query+"&model_id="+modelAlias.ModelID, memberCookie), 200), modelAlias.ModelID, 1, 1, 0)
	// Retain independent query authority and one-connection transaction behavior.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	maximum := sqlDB.Stats().MaxOpenConnections
	sqlDB.SetMaxOpenConns(1)
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	_, err = svc.TeamUsage(bounded, users[0].ID, teamID, service.UsageFilter{From: &started, To: &end.StartedAt})
	cancel()
	sqlDB.SetMaxOpenConns(maximum)
	if err != nil {
		t.Fatal("coverage required extra pool connection", err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users[0].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if result, err := svc.TeamUsage(ctx, users[0].ID, teamID, service.UsageFilter{From: &started, To: &end.StartedAt}); err == nil || result != nil {
		t.Fatal("disabled actor retained aggregate authority", result, err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users[0].ID).Update("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	csvResponse := read(teamPath+"/export.csv"+query, memberCookie)
	expectStatus(t, csvResponse, 200)
	csvRows, err := csv.NewReader(strings.NewReader(csvResponse.Body.String())).ReadAll()
	if err != nil || len(csvRows) == 0 {
		t.Fatal("CSV report lost", err)
	}
	for _, column := range csvRows[0] {
		if strings.Contains(column, "member") || column == "user_id" {
			t.Fatal("JSON coverage altered CSV schema", column)
		}
	}
	// Exactly 10,000 scoped facts are complete; one more retains existing overflow behavior.
	bulk := make([]entity.CallRecord, 10000)
	for i := range bulk {
		bulk[i] = entity.CallRecord{RequestID: fmt.Sprintf("req_members_bulk_%05d", i), UserID: fmt.Sprintf("usr_historical_%05d", i), TeamID: teamID, TeamMembershipID: "tmm_historical_bulk", ModelID: "mdl_members_bulk", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started, CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	}
	if err := db.CreateInBatches(&bulk, 250).Error; err != nil {
		t.Fatal(err)
	}
	// Coverage reuses the single bounded immutable-fact SELECT, including at the row ceiling.
	factQueries := 0
	const callbackName = "usage_members_fact_projection"
	if err := db.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "call_records" {
			return
		}
		factQueries++
		selectedActors := 0
		for _, column := range tx.Statement.Selects {
			if column == "user_id" {
				selectedActors++
			}
		}
		if selectedActors != 1 || len(tx.Statement.Joins) != 0 {
			t.Error("aggregate borrows directory joins or misses private recorded identity")
		}
	}); err != nil {
		t.Fatal(err)
	}
	callbackActive := true
	defer func() {
		if callbackActive {
			if err := db.Callback().Query().Remove(callbackName); err != nil {
				t.Error(err)
			}
		}
	}()
	bulkQuery := query + "&model_id=mdl_members_bulk"
	coverage(decodeCatalogResponse[service.UsageReport](t, read(teamPath+bulkQuery, memberCookie), 200), "mdl_members_bulk", 10000, 10000, 0)
	if factQueries != 1 {
		t.Fatal("member coverage added fact queries", factQueries)
	}
	if err := db.Callback().Query().Remove(callbackName); err != nil {
		t.Fatal(err)
	}
	callbackActive = false
	extra := bulk[0]
	extra.RequestID, extra.UserID = "req_members_bulk_extra", "usr_historical_extra"
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	overflow := read(teamPath+bulkQuery, memberCookie)
	expectStatus(t, overflow, 422)
	if strings.Contains(overflow.Body.String(), `"current"`) || strings.Contains(overflow.Body.String(), `"members"`) {
		t.Fatal("overflow leaked a partial report")
	}
	var persisted entity.CallRecord
	if err := db.First(&persisted, "request_id = ?", base.RequestID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.UserID != base.UserID || persisted.TeamMembershipID != base.TeamMembershipID {
		t.Fatal("report mutated immutable attribution")
	}
	// A second same-actor read is stable apart from report timestamps.
	first := decodeCatalogResponse[service.UsageReport](t, read(teamPath+query+"&model_id="+modelID, memberCookie), 200)
	secondReport := decodeCatalogResponse[service.UsageReport](t, read(teamPath+query+"&model_id="+modelID, memberCookie), 200)
	if !reflect.DeepEqual(first.Current, secondReport.Current) || !reflect.DeepEqual(first.Previous, secondReport.Previous) {
		t.Fatal("repeat aggregate changed historical coverage")
	}
}
