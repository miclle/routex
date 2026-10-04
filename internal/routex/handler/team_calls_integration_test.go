package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
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
	// Existing seeded history is projection evidence, not new native completion.
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_call_member").UpdateColumn("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	large, zero := int64(9007199254740993), int64(0)
	amount, currency := "0.000000000000000001", "EUR"
	exportedFact := entity.CallRecord{
		CallPricingFields: entity.CallPricingFields{PricingStatus: "priced", ChargeAmount: &amount, ChargeCurrency: &currency},
		RequestID:         "req_team_export_exact", UserID: member.ID, TeamID: teamID, TeamMembershipID: base.TeamMembershipID,
		ModelID: "mdl_export_exact", ModelName: "\u2003\t=HYPERLINK(\"https://invalid.example\")", Protocol: entity.ProtocolOpenAIChat, Status: "error",
		StartedAt: started.Add(time.Minute), CompletedAt: started.Add(time.Minute + time.Second), DurationMS: 1000, InputTokens: &large, OutputTokens: &zero,
		ProviderModelID: "pmd_private", ConnectionID: "con_private", ErrorCode: "private_error",
	}
	unknownFact := exportedFact
	unknownFact.RequestID, unknownFact.ModelName = "req_team_export_unknown", "Unknown history"
	unknownFact.StartedAt, unknownFact.CompletedAt = started.Add(2*time.Minute), started.Add(2*time.Minute+time.Second)
	unknownFact.InputTokens, unknownFact.OutputTokens = nil, nil
	unknownFact.PricingStatus, unknownFact.ChargeAmount, unknownFact.ChargeCurrency = "not_captured", nil, nil
	projectFact := exportedFact
	projectFact.RequestID, projectFact.UserID, projectFact.ProjectID = "req_team_export_project", "", "prj_export_other"
	projectFact.TeamID, projectFact.TeamMembershipID, projectFact.KeyID = "", "", "key_project_other"
	if err := db.Create([]entity.CallRecord{exportedFact, unknownFact, projectFact}).Error; err != nil {
		t.Fatal(err)
	}
	var beforeHistory []entity.CallRecord
	if err := db.Order("request_id").Find(&beforeHistory).Error; err != nil {
		t.Fatal(err)
	}
	var beforeAudits []entity.AuditEvent
	if err := db.Order("id").Find(&beforeAudits).Error; err != nil {
		t.Fatal(err)
	}
	exportPath := path + "/export.csv"
	requireExport := func(query string, cookie *http.Cookie) [][]string {
		t.Helper()
		response := identityRequest(router, "GET", exportPath+query, "", cookie, "")
		rows := readCallCSV(t, response, 200)
		if len(rows) == 0 || !slices.Equal(rows[0], callExportMemberColumns) || response.Header().Get("Content-Disposition") != `attachment; filename="routex-team-calls.csv"` || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Content-Type") != "text/csv; charset=utf-8" || strings.HasPrefix(response.Body.String(), "\ufeff") {
			t.Fatal("Team CSV metadata/schema changed", response.Header())
		}
		if strings.Contains(response.Body.String(), "private") {
			t.Fatal("Team CSV exposed administrator facts")
		}
		return rows
	}
	failExport := func(target string, cookie *http.Cookie, code int) {
		t.Helper()
		response := identityRequest(router, "GET", target, "", cookie, "")
		expectStatus(t, response, code)
		if response.Header().Get("Content-Disposition") != "" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/csv") {
			t.Fatal("denied export became partial download")
		}
	}
	rows := requireExport("", memberCookie)
	if len(rows) != 5 || rows[1][0] != unknownFact.RequestID || rows[2][0] != exportedFact.RequestID || rows[3][0] != "req_team_1" || rows[4][0] != "req_team_0" {
		t.Fatal("own-Team complete ordered export widened or paginated", rows)
	}
	for field, want := range map[string]string{"key_id": "", "model_name": "'" + exportedFact.ModelName, "input_tokens": strconv.FormatInt(large, 10), "output_tokens": "0", "cache_read_tokens": "", "charge_amount": amount, "charge_currency": currency, "started_at": exportedFact.StartedAt.UTC().Format(time.RFC3339Nano)} {
		if rows[2][callCSVColumn(t, rows[0], field)] != want {
			t.Fatal("safe exact Team CSV value changed", field, rows[2])
		}
	}
	for _, field := range []string{"input_tokens", "output_tokens", "charge_amount", "charge_currency"} {
		if rows[1][callCSVColumn(t, rows[0], field)] != "" {
			t.Fatal("unknown value became zero", field)
		}
	}
	// Compare the complete server file with all existing independently authorized JSON pages.
	var pagedIDs []string
	cursor := ""
	for {
		requestPath := path + "?limit=1"
		if cursor != "" {
			requestPath += "&cursor=" + url.QueryEscape(cursor)
		}
		response := identityRequest(router, "GET", requestPath, "", memberCookie, "")
		expectStatus(t, response, 200)
		var current CallsResponse
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		for _, row := range current.Items {
			pagedIDs = append(pagedIDs, row.RequestID)
		}
		if current.NextCursor == nil {
			break
		}
		cursor = *current.NextCursor
		if len(pagedIDs) > 4 {
			t.Fatal("unbounded Team cursor")
		}
	}
	if !slices.Equal(pagedIDs, []string{rows[1][0], rows[2][0], rows[3][0], rows[4][0]}) {
		t.Fatal("CSV/JSON retained scope mismatch", pagedIDs, rows)
	}
	filter := url.Values{"status": {"error"}, "model_id": {"mdl_export_exact"}, "from": {exportedFact.StartedAt.Format(time.RFC3339Nano)}, "to": {exportedFact.StartedAt.Format(time.RFC3339Nano)}}
	filtered := requireExport("?"+filter.Encode(), memberCookie)
	if len(filtered) != 2 || filtered[1][0] != exportedFact.RequestID {
		t.Fatal("applied status/model/time conjunction changed", filtered)
	}
	if empty := requireExport("?model_id=mdl_absent", memberCookie); len(empty) != 1 {
		t.Fatal("empty export is not header-only")
	}
	ownerExport := requireExport("", adminCookie)
	if len(ownerExport) != 2 || ownerExport[1][0] != "req_team_owner" {
		t.Fatal("owner/platform role exported peer Team history", ownerExport)
	}
	for _, query := range []string{"user_id=", "key_id=", "project_id=", "team_id=", "cursor=", "limit=", "page=", "unknown=", "status=success&status=success", "status=invalid", "model_id=mdl_one%20", "from=bad", "from=2026-10-05T00:00:00Z&to=2026-10-04T00:00:00Z"} {
		failExport(exportPath+"?"+query, memberCookie, 400)
	}
	for _, alias := range []string{strings.ToUpper(teamID), teamID + " "} {
		failExport("/api/v1/teams/"+url.PathEscape(alias)+"/calls/export.csv", memberCookie, 404)
	}
	if result, err := svc.ExportTeamCalls(context.Background(), strings.ToUpper(member.ID), teamID, service.CallFilter{}); err == nil || result != nil {
		t.Fatal("aliased actor exported history")
	}
	var retainedOwner entity.TeamMembership
	if err := db.First(&retainedOwner, "id = ?", "tmm_call_owner").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&retainedOwner).Error; err != nil {
		t.Fatal(err)
	}
	failExport(exportPath, adminCookie, 404)
	if err := db.Create(&retainedOwner).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&member).UpdateColumn("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	failExport(exportPath, memberCookie, 401)
	if err := db.Model(&member).UpdateColumn("disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_call_member").UpdateColumn("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	failExport(exportPath, memberCookie, 404)
	if err := db.Where("id = ?", "tmm_call_member").Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	failExport(exportPath, memberCookie, 404)
	if err := db.Create(&entity.TeamMembership{ID: "tmm_call_rejoined", TeamID: teamID, UserID: member.ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if recovered := requireExport("", memberCookie); !reflect.DeepEqual(recovered, rows) {
		t.Fatal("rejoin rewrote or lost historical membership rows")
	}
	for _, state := range []string{entity.ResourceDisabled, entity.ResourceArchived} {
		if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("status", state).Error; err != nil {
			t.Fatal(err)
		}
		failExport(exportPath, memberCookie, 404)
	}
	if err := db.Model(&entity.Team{}).Where("id = ?", teamID).UpdateColumn("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	var afterHistory []entity.CallRecord
	var afterAudits []entity.AuditEvent
	if err := db.Order("request_id").Find(&afterHistory).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Order("id").Find(&afterAudits).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeHistory, afterHistory) || !reflect.DeepEqual(beforeAudits, afterAudits) {
		t.Fatal("read-only Team export/lifecycle fixtures rewrote immutable history or audit")
	}

}
