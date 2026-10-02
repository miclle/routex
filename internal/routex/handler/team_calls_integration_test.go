package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testTeamCallLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"team-calls-admin@example.invalid","password":"test-only-team-calls-password","name":"Team calls admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_team_calls", Email: "team-calls-member@example.invalid", Name: "Team caller", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"team-calls-member@example.invalid","password":"test-only-team-calls-password"}`, nil, "")
	expectStatus(t, login, 200)
	_, memberCookie := readIdentity(t, login)
	teamID := "tem_call_scope"
	for _, row := range []any{
		&entity.Team{ID: teamID, Name: "Team calls", Status: entity.ResourceActive},
		&entity.Team{ID: "tem_call_other", Name: "Other calls", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_call_owner", TeamID: teamID, UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_call_member", TeamID: teamID, UserID: member.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_call_other", TeamID: "tem_call_other", UserID: member.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Hour)
	input, output := int64(4), int64(1)
	base := service.CallFact{RequestID: "req_team_a", UserID: member.ID, TeamID: teamID, TeamMembershipID: "tmm_historical_removed", ModelID: "mdl_team_history", ModelName: "Historical model", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started.Add(time.Second), InputTokens: &input, OutputTokens: &output}
	for index := range 2 {
		fact := base
		fact.RequestID = fmt.Sprintf("req_team_%d", index)
		if err := svc.RecordCall(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	other := base
	other.RequestID, other.TeamID = "req_team_other", "tem_call_other"
	if err := svc.RecordCall(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	owner := base
	owner.RequestID, owner.UserID = "req_team_owner", admin.User.ID
	if err := svc.RecordCall(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	personal := base
	personal.RequestID, personal.TeamID, personal.TeamMembershipID, personal.KeyID = "req_team_personal", "", "", "key_personal"
	if err := svc.RecordCall(context.Background(), personal); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/teams/" + teamID + "/calls"
	first := identityRequest(router, "GET", path+"?limit=1", "", memberCookie, "")
	expectStatus(t, first, 200)
	var page CallsResponse
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].RequestID != "req_team_1" || page.NextCursor == nil {
		t.Fatal("Team actor pagination scope", first.Body.String(), err)
	}
	second := identityRequest(router, "GET", path+"?limit=1&cursor="+*page.NextCursor, "", memberCookie, "")
	expectStatus(t, second, 200)
	if !strings.Contains(second.Body.String(), "req_team_0") || strings.Contains(second.Body.String(), "req_team_owner") || strings.Contains(second.Body.String(), "req_team_other") {
		t.Fatal("Team actor cursor broadened authority", second.Body.String())
	}
	for _, suffix := range []string{"?user_id=" + admin.User.ID, "?key_id=key_personal"} {
		expectStatus(t, identityRequest(router, "GET", path+suffix, "", memberCookie, ""), 400)
	}
	expectStatus(t, identityRequest(router, "GET", path+"/req_team_owner", "", memberCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path+"/req_team_other", "", memberCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path+"/req_team_personal", "", memberCookie, ""), 404)
	ownerPage := identityRequest(router, "GET", path, "", adminCookie, "")
	expectStatus(t, ownerPage, 200)
	if strings.Contains(ownerPage.Body.String(), "req_team_0") || !strings.Contains(ownerPage.Body.String(), "req_team_owner") {
		t.Fatal("Team owner/platform permission leaked member history", ownerPage.Body.String())
	}
	personalPage := identityRequest(router, "GET", "/api/v1/calls", "", memberCookie, "")
	expectStatus(t, personalPage, 200)
	if !strings.Contains(personalPage.Body.String(), "req_team_personal") || strings.Contains(personalPage.Body.String(), "req_team_0") {
		t.Fatal("Team facts entered Personal history", personalPage.Body.String())
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/calls/req_team_0", "", memberCookie, ""), 404)
	export, err := svc.ExportPersonalCalls(context.Background(), member.ID, service.CallFilter{})
	if err != nil || !strings.Contains(string(export.CSV), "req_team_personal") || strings.Contains(string(export.CSV), "req_team_0") {
		t.Fatal("Team facts entered Personal export", err)
	}
	from, to := started.Add(-time.Hour), started.Add(time.Hour)
	usage, err := svc.PersonalUsage(context.Background(), member.ID, service.UsageFilter{From: &from, To: &to, Timezone: "UTC", Granularity: "hour"})
	if err != nil || usage.Current.Summary.Requests != 1 {
		t.Fatal("Team facts entered Personal usage", usage, err)
	}
	adminDetail := identityRequest(router, "GET", "/api/v1/admin/calls/req_team_0", "", adminCookie, "")
	expectStatus(t, adminDetail, 200)
	if !strings.Contains(adminDetail.Body.String(), `"team_id":"`+teamID+`"`) || !strings.Contains(adminDetail.Body.String(), `"team_membership_id":"tmm_historical_removed"`) {
		t.Fatal("platform call lost immutable Team facts", adminDetail.Body.String())
	}
	for _, status := range []string{"ACTIVE", "disabled"} {
		if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_call_member").Update("status", status).Error; err != nil {
			// Released checks reject aliases; either the database or exact read must deny.
			if status == "ACTIVE" {
				continue
			}
			t.Fatal(err)
		}
		expectStatus(t, identityRequest(router, "GET", path, "", memberCookie, ""), 404)
		expectStatus(t, identityRequest(router, "GET", path+"/req_team_0", "", memberCookie, ""), 404)
	}
	var preserved entity.CallRecord
	if err := db.First(&preserved, "request_id = ?", "req_team_0").Error; err != nil || preserved.TeamID != teamID || preserved.TeamMembershipID != base.TeamMembershipID {
		t.Fatal("membership changes rewrote Team history", err)
	}
}
