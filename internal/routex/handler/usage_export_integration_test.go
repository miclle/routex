package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// The root lifecycle harness supplies an independently migrated database.
// Persisted facts exercise report/export semantics, not native completion.
func testUsageExportLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	var observing atomic.Bool
	var captures atomic.Int64
	var catalogReads atomic.Int64
	const callback = "test_usage_csv_capture"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
		if observing.Load() {
			switch query.Statement.Table {
			case "call_records":
				captures.Add(1)
			case "providers", "models", "model_names", "keys":
				catalogReads.Add(1)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		observing.Store(false)
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	read := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		return identityRequest(router, http.MethodGet, path, "", cookie, "")
	}
	setup := identityRequest(router, http.MethodPost, "/api/v1/setup", `{"email":"csv-admin@example.invalid","password":"test-only-usage-csv-password","name":"CSV administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{
		{ID: "usr_csv_actor", Email: "csv-actor@example.invalid", Name: "CSV actor", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_csv_peer", Email: "csv-peer@example.invalid", Name: "CSV peer", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(email string) *http.Cookie {
		t.Helper()
		response := identityRequest(router, http.MethodPost, "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-usage-csv-password"}`, email), nil, "")
		expectStatus(t, response, 200)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	actorCookie, peerCookie := login(users[0].Email), login(users[1].Email)
	const teamID, projectID = "tea_csv_scope", "prj_csv_scope"
	if err := db.Create(&entity.Team{ID: teamID, Name: "CSV Team", Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]entity.TeamMembership{
		{ID: "tmm_csv_original", TeamID: teamID, UserID: users[0].ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		{ID: "tmm_csv_owner", TeamID: teamID, UserID: users[1].ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Project{ID: projectID, Name: "Archived CSV Project", Status: entity.ResourceArchived, CreatorID: users[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pjm_csv_original", ProjectID: projectID, UserID: users[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2024, 3, 9, 12, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 12, 6, 0, 0, 0, time.UTC)
	query := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "timezone": {"America/New_York"}, "granularity": {"day"}, "compare": {"true"}, "protocol": {entity.ProtocolOpenAIChat}, "stream": {"false"}}
	base := service.CallFact{UserID: users[0].ID, KeyID: "key_csv_history", ModelID: "mdl_csv_history", ModelName: "=SUM(A1:A2)", ProviderID: "prv_csv_history", ProviderName: "\r@historical provider", ProviderModelID: "pmd_csv_history", UpstreamModelName: "Historical upstream", ConnectionID: "con_csv_history", ConnectionName: "=historical connection", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: from, CompletedAt: from.Add(100 * time.Millisecond)}
	known := func(fact service.CallFact, input, output int64, currency, amount string) service.CallFact {
		fact.InputTokens, fact.OutputTokens = &input, &output
		if currency != "" {
			privateSnapshot := `{"private_snapshot":"not_exportable"}`
			fact.Pricing = &service.CallPricing{Status: "priced", Currency: &currency, Amount: &amount, SnapshotJSON: &privateSnapshot}
		}
		return fact
	}
	record := func(fact service.CallFact) {
		t.Helper()
		if err := svc.RecordCall(context.Background(), fact); err != nil {
			t.Fatalf("record %s: %v", fact.RequestID, err)
		}
	}
	names := []string{"=SUM(A1:A2)", "\t+unsafe", "\u2003@SUM", "\n-unsafe", "'original apostrophe", "Unicode 中,\"quoted\"\nline"}
	inputs := []int64{9007199254740993, 0, 0, 1, 2, 3}
	outputs := []int64{2, 2, 0, 0, 1, 1}
	currencies := []string{"USD", "", "EUR", "USD", "EUR", "USD"}
	amounts := []string{"0.123456789012345678", "", "0", "0.000000000000000001", "0.2", "0.1"}
	for index, name := range names {
		fact := known(base, inputs[index], outputs[index], currencies[index], amounts[index])
		fact.RequestID = fmt.Sprintf("req_csv_personal_%d", index)
		fact.ModelID, fact.ModelName = fmt.Sprintf("mdl_csv_%d", index), name
		fact.StartedAt = from.Add(time.Duration(index) * time.Hour)
		fact.CompletedAt = fact.StartedAt.Add(100 * time.Millisecond)
		if index == 1 {
			fact.Status, fact.InputTokens = "error", nil
		}
		if index == 2 {
			fact.KeyID = "key_csv_zero"
		}
		if index == 3 {
			fact.Status = "canceled"
		}
		record(fact)
		if index == 0 {
			record(fact) // Immutable request identity prevents duplicate totals.
		}
	}
	zero := int64(0)
	noWork := known(base, 0, 0, "", "")
	noWork.RequestID, noWork.ModelID, noWork.ModelName, noWork.KeyID = "req_csv_no_work", "", "", ""
	noWork.Status, noWork.NoWork, noWork.UsageComplete = "error", true, true
	noWork.CacheReadTokens, noWork.CacheWriteTokens = &zero, &zero
	noWork.Pricing = &service.CallPricing{Status: "no_work"}
	noWork.StartedAt, noWork.CompletedAt = to.Add(-time.Millisecond), to.Add(-time.Millisecond)
	record(noWork)
	teamFact := known(base, 3, 1, "USD", "0.5")
	teamFact.RequestID, teamFact.TeamID, teamFact.TeamMembershipID, teamFact.KeyID = "req_csv_team_actor", teamID, "tmm_csv_original", ""
	record(teamFact)
	teamFact.RequestID, teamFact.UserID, teamFact.TeamMembershipID = "req_csv_team_peer", users[1].ID, "tmm_csv_owner"
	record(teamFact)
	projectFact := known(base, 3, 1, "EUR", "0.4")
	projectFact.RequestID, projectFact.UserID, projectFact.ProjectID, projectFact.KeyID = "req_csv_project", "", projectID, "key_csv_project"
	record(projectFact)
	other := known(base, 3, 1, "USD", "0.5")
	other.RequestID, other.UserID, other.KeyID, other.ModelID = "req_csv_other", users[1].ID, "key_csv_other", "mdl_csv_other"
	record(other)
	for index, source := range []service.CallFact{known(base, 1, 1, "USD", "0.01"), teamFact, projectFact} {
		source.RequestID = fmt.Sprintf("req_csv_previous_%d", index)
		source.StartedAt, source.CompletedAt = from.Add(-time.Hour), from.Add(-time.Hour+100*time.Millisecond)
		record(source)
	}
	for index, at := range []time.Time{from.Add(-2 * to.Sub(from)).Add(-time.Millisecond), to} {
		outside := known(base, 99, 99, "USD", "99")
		outside.RequestID = fmt.Sprintf("req_csv_outside_%d", index)
		outside.StartedAt, outside.CompletedAt = at, at.Add(time.Millisecond)
		record(outside)
	}
	if err := db.Create(&entity.Provider{ID: base.ProviderID, Name: "Live renamed provider"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Model{ID: "mdl_csv_0", Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	currentModelID := "mdl_csv_0"
	if err := db.Create(&entity.ModelName{Name: "Live renamed model", ModelID: currentModelID, CurrentModelID: &currentModelID}).Error; err != nil {
		t.Fatal(err)
	}

	check := func(path, scope, target string, cookie *http.Cookie, filters url.Values) service.UsageReport {
		t.Helper()
		t.Logf("CSV scope=%s target=%q filters=%s", scope, target, filters.Encode())
		suffix := "?" + filters.Encode()
		report := decodeCatalogResponse[service.UsageReport](t, read(path+suffix, cookie), 200)
		captures.Store(0)
		catalogReads.Store(0)
		observing.Store(true)
		before := time.Now().UTC()
		response := read(path+"/export.csv"+suffix, cookie)
		after := time.Now().UTC()
		observing.Store(false)
		if captures.Load() != 1 {
			t.Fatalf("%s export captured %d fact queries; want one complete report", scope, captures.Load())
		}
		if catalogReads.Load() != 0 {
			t.Fatalf("%s export read %d mutable catalog/Key tables", scope, catalogReads.Load())
		}
		assertUsageExportCSV(t, response, scope, target, filters, report, before, after)
		return report
	}
	personal := check("/api/v1/usage", "personal", users[0].ID, actorCookie, query)
	stats := personal.Current.Summary
	if stats.Requests != 7 || stats.Successes != 4 || stats.Errors != 2 || stats.Canceled != 1 || stats.Tokens.Input.Value != nil || stats.Tokens.Input.Known != "9007199254740999" || stats.Tokens.Input.UnknownCalls != 1 || stats.Tokens.Output.Known != "6" || stats.Tokens.Total.Known != "9007199254741005" || stats.UnknownAmountCalls != 1 || personal.Previous == nil || personal.Previous.Summary.Requests != 1 {
		t.Fatalf("deduplication, half-open scope or exact partial totals changed: %+v", personal)
	}
	if !slices.Equal(stats.Amounts, []service.UsageAmount{{Currency: "EUR", Amount: "0.2", Calls: 2}, {Currency: "USD", Amount: "0.223456789012345679", Calls: 3}}) || stats.PricingStatuses["no_work"] != 1 || stats.PricingStatuses["not_captured"] != 1 || stats.Tokens.Total.Value != nil || stats.Tokens.Output.Value == nil || *stats.Tokens.Output.Value != "6" || personal.LatestCompletedAt == nil || !personal.LatestCompletedAt.Equal(noWork.CompletedAt) {
		t.Fatalf("exact currency, null coverage or selected freshness changed: %+v", personal)
	}
	if !slices.ContainsFunc(personal.Current.Models, func(group service.UsageGroup) bool {
		return group.ID == "mdl_csv_2" && slices.Equal(group.Stats.Amounts, []service.UsageAmount{{Currency: "EUR", Amount: "0", Calls: 1}})
	}) || !slices.ContainsFunc(personal.Current.Models, func(group service.UsageGroup) bool { return group.ID == "" && group.Name == "" && group.Unknown }) {
		t.Fatal("explicit monetary zero or unknown historical identity collapsed")
	}
	// Query selection stays half-open and exact; day buckets retain complete
	// local calendar boundaries, including the partially selected edge days.
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	calendarStart := time.Date(2024, 3, 9, 0, 0, 0, 0, location)
	calendarEnd := time.Date(2024, 3, 13, 0, 0, 0, 0, location)
	if !personal.Current.From.Equal(from) || !personal.Current.To.Equal(to) || len(personal.Current.Trend) != 4 || !personal.Current.Trend[0].Start.Equal(calendarStart) || !personal.Current.Trend[3].End.Equal(calendarEnd) || personal.Current.Trend[1].End.Sub(personal.Current.Trend[1].Start) != 23*time.Hour {
		t.Fatalf("exact selection or DST/calendar edge buckets changed: %+v", personal.Current)
	}
	for _, name := range names {
		if !slices.ContainsFunc(personal.Current.Models, func(group service.UsageGroup) bool { return group.Name == name }) {
			t.Fatalf("historical label lost: %q", name)
		}
	}
	projectPath, teamPath := "/api/v1/projects/"+projectID+"/usage", "/api/v1/teams/"+teamID+"/usage"
	project := check(projectPath, "project", projectID, actorCookie, query)
	team := check(teamPath, "team", teamID, actorCookie, query)
	platform := check("/api/v1/admin/usage", "platform", "", adminCookie, query)
	if project.Current.Summary.Requests != 1 || team.Current.Summary.Requests != 2 || team.Current.Summary.Tokens.Total.Known != "8" || platform.Current.Summary.Requests != 11 {
		t.Fatalf("scope totals changed: project=%+v team=%+v platform=%+v", project.Current.Summary, team.Current.Summary, platform.Current.Summary)
	}
	if !slices.ContainsFunc(platform.Current.Providers, func(group service.UsageGroup) bool {
		return group.ID == base.ProviderID && group.Name == base.ProviderName
	}) {
		t.Fatal("platform topology used live labels instead of immutable history")
	}
	selected := query.Clone()
	selected.Set("team_id", teamID)
	teamPlatform := check("/api/v1/admin/usage", "platform", "", adminCookie, selected)
	if slices.Contains(teamPlatform.AvailableDimensions, "key") || teamPlatform.Current.Summary.Requests != 2 {
		t.Fatal("Team-filtered platform export gained Key attribution")
	}
	selected = query.Clone()
	selected.Set("user_id", users[0].ID)
	if report := check("/api/v1/admin/usage", "platform", "", adminCookie, selected); report.Current.Summary.Requests != 7 {
		t.Fatal("platform Personal user selection borrowed Team/Project facts")
	}
	selected = query.Clone()
	selected.Set("model_id", "mdl_csv_absent")
	selected.Set("compare", "false")
	if report := check("/api/v1/usage", "personal", users[0].ID, actorCookie, selected); report.Current.Summary.Requests != 0 || report.LatestCompletedAt != nil || report.Current.Summary.SuccessRate != nil {
		t.Fatal("empty export fabricated nonempty freshness or success facts")
	}
	selected = query.Clone()
	selected.Set("key_id", "key_csv_other")
	if report := check("/api/v1/usage", "personal", users[0].ID, actorCookie, selected); report.Current.Summary.Requests != 0 {
		t.Fatal("guessed Key crossed Personal ownership")
	}
	if report := check("/api/v1/usage", "personal", admin.User.ID, adminCookie, query); report.Current.Summary.Requests != 0 {
		t.Fatal("administrator Personal export borrowed platform history")
	}

	fail := func(path string, cookie *http.Cookie, status int) {
		t.Helper()
		response := read(path, cookie)
		if response.Code != status {
			t.Fatalf("GET %s: status %d want %d: %s", path, response.Code, status, response.Body.String())
		}
		if response.Header().Get("Content-Disposition") != "" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/csv") || !strings.Contains(response.Header().Get("Content-Type"), "json") || strings.Contains(response.Body.String(), "not_exportable") {
			t.Fatalf("failed export published a download or private snapshot: %v %s", response.Header(), response.Body.String())
		}
	}
	personalExport := "/api/v1/usage/export.csv?" + query.Encode()
	fail(personalExport, nil, 401)
	fail("/api/v1/admin/usage/export.csv?"+query.Encode(), actorCookie, 403)
	fail(projectPath+"/export.csv?"+query.Encode(), peerCookie, 404) // Creator has no permanent authority.
	fail(teamPath+"/export.csv?"+query.Encode(), adminCookie, 404)   // Platform role is not Team membership.
	for _, suffix := range []string{"&status=", "&from=" + url.QueryEscape(from.Format(time.RFC3339)), "&unexpected=x", "&limit=10", "&cursor=x", "&currency=USD", "&user_id=" + users[1].ID, "&provider_id=" + base.ProviderID, "&period=7d", "&stream=maybe"} {
		fail(personalExport+suffix, actorCookie, 400)
	}
	for _, suffix := range []string{"&key_id=key_csv_history", "&user_id=" + users[0].ID, "&project_id=" + projectID, "&provider_id=" + base.ProviderID} {
		fail(teamPath+"/export.csv?"+query.Encode()+suffix, actorCookie, 400)
	}
	fail("/api/v1/admin/usage/export.csv?"+query.Encode()+"&team_id="+teamID+"&user_id="+users[0].ID, adminCookie, 400)
	for _, alias := range []string{strings.ToUpper(teamID), teamID + "%20"} {
		fail("/api/v1/teams/"+alias+"/usage/export.csv?"+query.Encode(), actorCookie, 404)
	}
	fail("/api/v1/projects/"+strings.ToUpper(projectID)+"/usage/export.csv?"+query.Encode(), actorCookie, 404)
	if err := db.Where("id = ?", "pjm_csv_original").Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	fail(projectPath+"/export.csv?"+query.Encode(), actorCookie, 404)
	if err := db.Create(&entity.ProjectManager{ID: "pjm_csv_rejoined", ProjectID: projectID, UserID: users[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if restored := check(projectPath, "project", projectID, actorCookie, query); !reflect.DeepEqual(restored.Current, project.Current) {
		t.Fatal("renewed manager relationship rewrote archived Project history")
	}
	if err := db.Where("id = ?", "tmm_csv_original").Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	fail(teamPath+"/export.csv?"+query.Encode(), actorCookie, 404)
	check(teamPath, "team", teamID, peerCookie, query)
	if err := db.Create(&entity.TeamMembership{ID: "tmm_csv_rejoined", TeamID: teamID, UserID: users[0].ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if restored := check(teamPath, "team", teamID, actorCookie, query); !reflect.DeepEqual(restored.Current, team.Current) {
		t.Fatal("membership rejoin rewrote historical aggregate facts")
	}
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_csv_rejoined").Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	fail(teamPath+"/export.csv?"+query.Encode(), actorCookie, 404)
	if err := db.Model(&entity.TeamMembership{}).Where("id = ?", "tmm_csv_rejoined").Update("status", entity.ResourceActive).Error; err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{entity.ResourceDisabled, entity.ResourceArchived} {
		if err := db.Model(&entity.Team{}).Where("id = ?", teamID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		fail(teamPath+"/export.csv?"+query.Encode(), actorCookie, 404)
	}
	fail("/api/v1/usage/export.csv?from=2024-01-01T00:00:00Z&to=2024-07-01T00:00:00Z&granularity=hour", actorCookie, 422)
	groups := make([]entity.CallRecord, 501)
	for index := range groups {
		groups[index] = entity.CallRecord{RequestID: fmt.Sprintf("req_csv_group_%d", index), UserID: users[0].ID, KeyID: "key_csv_groups", ModelID: fmt.Sprintf("mdl_csv_group_%d", index), ModelName: "Bounded group", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: from, CompletedAt: from.Add(time.Millisecond), InputTokens: &zero, OutputTokens: &zero, CallPricingFields: entity.CallPricingFields{PricingStatus: "no_work"}}
	}
	if err := db.CreateInBatches(&groups, 100).Error; err != nil {
		t.Fatal(err)
	}
	fail(personalExport+"&key_id=key_csv_groups", actorCookie, 422)
}

var usageExportFixtureColumns = strings.Split("row_type,schema_version,text_encoding,scope,scope_id,queried_at,source,may_lag,latest_selected_completed_at,timezone,granularity,available_dimensions,filter_period,filter_from,filter_to,filter_granularity,filter_timezone,filter_compare,filter_model_id,filter_key_id,filter_status,filter_protocol,filter_stream,filter_user_id,filter_project_id,filter_team_id,filter_provider_id,filter_provider_model_id,filter_connection_id,period,period_from,period_to,section,dimension,entity_id,entity_name,unknown_identity,bucket_start,bucket_end,requests,successes,errors,canceled,success_rate,average_duration_ms,input_value,input_known,input_unknown_calls,output_value,output_known,output_unknown_calls,total_value,total_known,total_unknown_calls,unknown_amount_calls,currency,amount,amount_calls,pricing_status,pricing_status_calls", ",")

func assertUsageExportCSV(t *testing.T, response *httptest.ResponseRecorder, scope, target string, filters url.Values, report service.UsageReport, before, after time.Time) {
	t.Helper()
	if response.Code != 200 {
		t.Fatalf("%s CSV status %d: %s", scope, response.Code, response.Body.String())
	}
	kind, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
	if err != nil || kind != "text/csv" || parameters["charset"] != "utf-8" {
		t.Fatalf("CSV content type: %v %v", response.Header(), err)
	}
	kind, parameters, err = mime.ParseMediaType(response.Header().Get("Content-Disposition"))
	if err != nil || kind != "attachment" || parameters["filename"] != "routex-"+scope+"-usage.csv" || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("CSV private download headers: %v %v", response.Header(), err)
	}
	records, err := csv.NewReader(strings.NewReader(response.Body.String())).ReadAll()
	if err != nil || len(records) < 3 || !slices.Equal(records[0], usageExportFixtureColumns) {
		t.Fatalf("CSV rectangular schema: %v", err)
	}
	textColumns := strings.Split("scope_id,queried_at,source,latest_selected_completed_at,timezone,granularity,available_dimensions,filter_period,filter_from,filter_to,filter_granularity,filter_timezone,filter_model_id,filter_key_id,filter_status,filter_protocol,filter_user_id,filter_project_id,filter_team_id,filter_provider_id,filter_provider_model_id,filter_connection_id,period_from,period_to,entity_id,entity_name,bucket_start,bucket_end,input_value,input_known,output_value,output_known,total_value,total_known,currency,amount,pricing_status", ",")
	rows := make([]map[string]string, 0, len(records)-1)
	for index, record := range records[1:] {
		if len(record) != len(usageExportFixtureColumns) {
			t.Fatalf("CSV row %d width %d", index, len(record))
		}
		row := make(map[string]string, len(record))
		for columnIndex, value := range record {
			column := usageExportFixtureColumns[columnIndex]
			if value != "" && slices.Contains(textColumns, column) {
				if !strings.HasPrefix(value, "'") {
					t.Fatalf("unencoded CSV row %d %s=%q", index, column, value)
				}
				value = value[1:] // Remove one prefix, preserving an original apostrophe.
			}
			row[column] = value
		}
		if row["section"] == "group" {
			for _, column := range []string{"entity_id", "entity_name"} {
				if record[slices.Index(usageExportFixtureColumns, column)] == "" {
					t.Fatalf("group present-empty %s collapsed into absent text", column)
				}
			}
		}
		rows = append(rows, row)
	}
	metadata := usageExportBlankRow()
	for key, value := range map[string]string{"row_type": "metadata", "schema_version": "routex_usage_v1", "text_encoding": "apostrophe_text_v1", "scope": scope, "scope_id": target, "source": report.Source, "may_lag": strconv.FormatBool(report.MayLag), "timezone": report.Timezone, "granularity": report.Granularity, "available_dimensions": strings.Join(report.AvailableDimensions, ","), "filter_compare": "false"} {
		metadata[key] = value
	}
	if report.LatestCompletedAt != nil {
		metadata["latest_selected_completed_at"] = report.LatestCompletedAt.UTC().Format(time.RFC3339Nano)
	}
	for key, values := range filters {
		metadata["filter_"+key] = values[0]
	}
	queriedAt, err := time.Parse(time.RFC3339Nano, rows[0]["queried_at"])
	if err != nil || queriedAt.Before(before) || queriedAt.After(after) {
		t.Fatalf("CSV queried_at not its fresh server capture: %q %v", rows[0]["queried_at"], err)
	}
	metadata["queried_at"] = rows[0]["queried_at"]
	if !reflect.DeepEqual(metadata, rows[0]) {
		t.Fatalf("CSV metadata mismatch\ngot: %v\nwant: %v", rows[0], metadata)
	}
	want := usageExportExpectedPeriod("current", report.Current, report.AvailableDimensions)
	if report.Previous != nil {
		want = append(want, usageExportExpectedPeriod("previous", *report.Previous, report.AvailableDimensions)...)
	}
	if len(rows)-1 != len(want) {
		t.Fatalf("CSV projection row count %d want %d", len(rows)-1, len(want))
	}
	for index, expected := range want {
		if !reflect.DeepEqual(rows[index+1], expected) {
			t.Fatalf("CSV %s row %d differs from report\ngot: %v\nwant: %v", scope, index+1, rows[index+1], expected)
		}
	}
	private := []string{"not_exportable", "private_snapshot", "Live renamed provider", "Live renamed model"}
	if scope == "personal" || scope == "project" || scope == "team" {
		private = append(private, "prv_csv_history", "pmd_csv_history", "con_csv_history", "@historical provider", "Historical upstream", "=historical connection", "usr_csv_peer")
	}
	if scope == "team" {
		private = append(private, "usr_csv_actor", "tmm_csv_original", "tmm_csv_owner", "tmm_csv_rejoined", "key_csv_history", "key_csv_project")
	}
	for _, secret := range private {
		if strings.Contains(response.Body.String(), secret) {
			t.Fatalf("%s CSV exposed %q", scope, secret)
		}
	}
}

func usageExportBlankRow() map[string]string {
	row := make(map[string]string, len(usageExportFixtureColumns))
	for _, column := range usageExportFixtureColumns {
		row[column] = ""
	}
	return row
}

// This oracle consumes the public report DTO, never the production encoder.
func usageExportExpectedPeriod(name string, period service.UsagePeriod, dimensions []string) []map[string]string {
	var rows []map[string]string
	emit := func(locator map[string]string, stats service.UsageStats) {
		row := usageExportBlankRow()
		for key, value := range locator {
			row[key] = value
		}
		row["row_type"] = "stats"
		for key, value := range map[string]int64{"requests": stats.Requests, "successes": stats.Successes, "errors": stats.Errors, "canceled": stats.Canceled, "unknown_amount_calls": stats.UnknownAmountCalls} {
			row[key] = strconv.FormatInt(value, 10)
		}
		for key, value := range map[string]*float64{"success_rate": stats.SuccessRate, "average_duration_ms": stats.AverageDurationMS} {
			if value != nil {
				row[key] = strconv.FormatFloat(*value, 'g', -1, 64)
			}
		}
		for key, count := range map[string]service.UsageCount{"input": stats.Tokens.Input, "output": stats.Tokens.Output, "total": stats.Tokens.Total} {
			row[key+"_known"], row[key+"_unknown_calls"] = count.Known, strconv.FormatInt(count.UnknownCalls, 10)
			if count.Value != nil {
				row[key+"_value"] = *count.Value
			}
		}
		rows = append(rows, row)
		amounts := slices.Clone(stats.Amounts)
		slices.SortFunc(amounts, func(a, b service.UsageAmount) int { return strings.Compare(a.Currency, b.Currency) })
		for _, amount := range amounts {
			child := usageExportBlankRow()
			for key, value := range locator {
				child[key] = value
			}
			child["row_type"], child["currency"], child["amount"], child["amount_calls"] = "amount", amount.Currency, amount.Amount, strconv.FormatInt(amount.Calls, 10)
			rows = append(rows, child)
		}
		statuses := make([]string, 0, len(stats.PricingStatuses))
		for status := range stats.PricingStatuses {
			statuses = append(statuses, status)
		}
		slices.Sort(statuses)
		for _, status := range statuses {
			child := usageExportBlankRow()
			for key, value := range locator {
				child[key] = value
			}
			child["row_type"], child["pricing_status"], child["pricing_status_calls"] = "pricing_status", status, strconv.FormatInt(stats.PricingStatuses[status], 10)
			rows = append(rows, child)
		}
	}
	locator := func(section string) map[string]string {
		return map[string]string{"period": name, "period_from": period.From.UTC().Format(time.RFC3339Nano), "period_to": period.To.UTC().Format(time.RFC3339Nano), "section": section}
	}
	emit(locator("summary"), period.Summary)
	for _, bucket := range period.Trend {
		row := locator("trend")
		row["bucket_start"], row["bucket_end"] = bucket.Start.UTC().Format(time.RFC3339Nano), bucket.End.UTC().Format(time.RFC3339Nano)
		emit(row, bucket.Stats)
	}
	groups := map[string][]service.UsageGroup{"model": period.Models, "key": period.Keys, "provider": period.Providers, "provider_model": period.ProviderModels, "connection": period.Connections}
	for _, dimension := range dimensions {
		for _, group := range groups[dimension] {
			row := locator("group")
			row["dimension"], row["entity_id"], row["entity_name"], row["unknown_identity"] = dimension, group.ID, group.Name, strconv.FormatBool(group.Unknown)
			emit(row, group.Stats)
		}
	}
	return rows
}
