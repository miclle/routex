package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// The actual-driver owner invokes this independently with a fresh migrated
// database. Seeded immutable facts prove report behavior, not native execution.
func testMemberOverviewUsageLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"home-usage-admin@example.invalid","password":"test-only-home-usage-password","name":"Usage administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.Take(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	actor := entity.User{ID: "usr_home_usage", Email: "home-usage-member@example.invalid", Name: "Current member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	other := entity.User{ID: "usr_home_other", Email: "home-usage-other@example.invalid", Name: "Other member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	const teamID, projectID = "tem_home_usage", "prj_home_usage"
	for _, row := range []any{
		&actor, &other,
		&entity.Team{ID: teamID, Name: "Current usage Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_home_original", TeamID: teamID, UserID: actor.ID, Role: entity.TeamMember, Status: entity.ResourceActive},
		&entity.TeamMembership{ID: "tmm_home_other", TeamID: teamID, UserID: other.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.Project{ID: projectID, Name: "Archived usage Project", Status: entity.ResourceArchived, CreatorID: other.ID},
		&entity.ProjectManager{ID: "pmg_home_original", ProjectID: projectID, UserID: actor.ID},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	login := func(email string) *http.Cookie {
		t.Helper()
		response := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-home-usage-password"}`, email), nil, "")
		expectStatus(t, response, 200)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	actorCookie, otherCookie := login(actor.Email), login(other.Email)
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := identityRequest(router, "GET", path, "", cookie, "")
		if response.Code != status {
			t.Fatalf("usage GET %s: status=%d want=%d response=%s", path, response.Code, status, response.Body.String())
		}
		return response
	}
	report := func(path string, cookie *http.Cookie) service.UsageReport {
		t.Helper()
		return decodeCatalogResponse[service.UsageReport](t, read(path, cookie, 200), 200)
	}
	anchor := time.Now().UTC().Truncate(time.Second)
	input, output, zero := int64(10), int64(2), int64(0)
	usd, eur, amount, snapshot := "USD", "EUR", "0.1", `{"private":"immutable price basis"}`
	base := service.CallFact{UserID: actor.ID, KeyID: "key_home_history", ModelID: "mdl_home_history", ModelName: "Recorded model", ProviderID: "prv_home_history", ProviderName: "Recorded provider", ProviderModelID: "pmd_home_history", UpstreamModelName: "Recorded upstream", ConnectionID: "con_home_history", ConnectionName: "Recorded connection", Protocol: entity.ProtocolOpenAIChat, Status: "success", InputTokens: &input, OutputTokens: &output, Pricing: &service.CallPricing{Status: "priced", Amount: &amount, Currency: &usd, SnapshotJSON: &snapshot}}
	record := func(id, scope string, age time.Duration, change func(*service.CallFact)) {
		t.Helper()
		fact := base
		fact.RequestID, fact.StartedAt = id, anchor.Add(-age)
		fact.CompletedAt = fact.StartedAt.Add(time.Second)
		switch scope {
		case "team":
			fact.TeamID, fact.TeamMembershipID, fact.KeyID = teamID, "tmm_home_historical", ""
		case "project":
			fact.UserID, fact.ProjectID, fact.KeyID = "", projectID, "key_home_project"
		case "other":
			fact.UserID, fact.KeyID = other.ID, "key_home_other"
		}
		if change != nil {
			change(&fact)
		}
		if err := svc.RecordCall(ctx, fact); err != nil {
			t.Fatal("persist immutable usage fact", id, err)
		}
	}
	record("req_home_success_old", "personal", 29*24*time.Hour, nil)
	record("req_home_success_old", "personal", 29*24*time.Hour, nil) // Durable replay remains one fact.
	record("req_home_success_new", "personal", 2*24*time.Hour, func(f *service.CallFact) {
		i, o, a := int64(7), int64(3), "0.2"
		f.InputTokens, f.OutputTokens, f.ModelName = &i, &o, "Latest recorded model"
		f.Pricing = &service.CallPricing{Status: "priced", Amount: &a, Currency: &eur, SnapshotJSON: &snapshot}
	})
	record("req_home_unknown", "personal", 24*time.Hour, func(f *service.CallFact) { f.Status, f.InputTokens, f.Pricing = "error", nil, nil })
	record("req_home_canceled", "personal", 3*24*time.Hour, func(f *service.CallFact) {
		i, o, a := int64(4), int64(1), "0.123456789012345678"
		f.Status, f.InputTokens, f.OutputTokens = "canceled", &i, &o
		f.Pricing = &service.CallPricing{Status: "priced", Amount: &a, Currency: &usd, SnapshotJSON: &snapshot}
	})
	record("req_home_denied", "personal", time.Hour, func(f *service.CallFact) {
		f.Status, f.NoWork, f.UsageComplete = "error", true, true
		f.InputTokens, f.OutputTokens, f.CacheReadTokens, f.CacheWriteTokens = &zero, &zero, &zero, &zero
		f.Pricing = &service.CallPricing{Status: "no_work"}
	})
	record("req_home_team_actor", "team", time.Hour, nil)
	record("req_home_team_peer", "team", 2*time.Hour, func(f *service.CallFact) { f.UserID, f.TeamMembershipID = other.ID, "tmm_home_removed_peer" })
	record("req_home_project", "project", time.Hour, nil)
	record("req_home_other", "other", time.Hour, nil)
	for _, scope := range []string{"personal", "team", "project"} {
		record("req_home_previous_"+scope, scope, 31*24*time.Hour, nil)
	}
	record("req_home_before_previous", "personal", 61*24*time.Hour, nil)
	record("req_home_future", "personal", -24*time.Hour, nil)
	for _, row := range []any{&entity.Model{ID: base.ModelID, Status: entity.ResourceActive}, &entity.ModelName{Name: "live-renamed-model", ModelID: base.ModelID, CurrentModelID: &base.ModelID}, &entity.Provider{ID: base.ProviderID, Name: "Live renamed provider"}} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	query := "?period=30d&timezone=America%2FNew_York&compare=true"
	personalPath, teamPath, projectPath := "/api/v1/usage", "/api/v1/teams/"+teamID+"/usage", "/api/v1/projects/"+projectID+"/usage"
	beforeRead := time.Now().UTC()
	personal := report(personalPath+query, actorCookie)
	afterRead := time.Now().UTC()
	if personal.QueriedAt.Before(beforeRead) || personal.QueriedAt.After(afterRead) {
		t.Fatal("30-day range was not derived from the server observation", personal.QueriedAt, beforeRead, afterRead)
	}
	checkRange := func(value service.UsageReport) {
		t.Helper()
		if value.Current.To.Sub(value.Current.From) != 720*time.Hour || !value.Current.To.Equal(value.QueriedAt) || value.Previous == nil || !value.Previous.To.Equal(value.Current.From) || value.Previous.To.Sub(value.Previous.From) != 720*time.Hour || value.Granularity != "day" || value.Timezone != "America/New_York" || value.Source != "persisted_call_records" || !value.MayLag {
			t.Fatalf("elapsed range, comparison or persistence metadata changed: %+v", value)
		}
		// A spring transition plus partial midnight edges can cover 32 local
		// dates while the selected elapsed range remains exactly 720 hours.
		if len(value.Current.Trend) < 30 || len(value.Current.Trend) > 32 || value.Current.Trend[0].Start.After(value.Current.From) || value.Current.Trend[len(value.Current.Trend)-1].End.Before(value.Current.To) {
			t.Fatal("calendar labels do not cover partial elapsed edge buckets", value.Current)
		}
		var requests int64
		for index, bucket := range value.Current.Trend {
			if !bucket.Start.Before(bucket.End) || index > 0 && !bucket.Start.Equal(value.Current.Trend[index-1].End) {
				t.Fatal("day trend has a gap or overlapping bucket", bucket)
			}
			requests += bucket.Stats.Requests
		}
		if requests != value.Current.Summary.Requests {
			t.Fatal("day trend double-counted or lost selected facts", requests, value.Current.Summary.Requests)
		}
	}
	checkRange(personal)
	stats := personal.Current.Summary
	if !reflect.DeepEqual(personal.AvailableDimensions, []string{"model", "key"}) || stats.Requests != 5 || stats.Successes != 2 || stats.Errors != 2 || stats.Canceled != 1 || stats.SuccessRate == nil || *stats.SuccessRate != .4 || stats.Tokens.Input.Value != nil || stats.Tokens.Input.Known != "21" || stats.Tokens.Output.Value == nil || *stats.Tokens.Output.Value != "8" || stats.Tokens.Total.Value != nil || stats.Tokens.Total.Known != "29" || stats.Tokens.Total.UnknownCalls != 1 || stats.UnknownAmountCalls != 1 || stats.PricingStatuses["no_work"] != 1 || personal.Previous.Summary.Requests != 1 {
		t.Fatal("cancellation, retained denial, unknown coverage or success rate changed", stats)
	}
	if !reflect.DeepEqual(stats.Amounts, []service.UsageAmount{{Currency: "EUR", Amount: "0.2", Calls: 1}, {Currency: "USD", Amount: "0.223456789012345678", Calls: 2}}) || len(personal.Current.Models) != 1 || personal.Current.Models[0].ID != base.ModelID || personal.Current.Models[0].Name != base.ModelName || len(personal.Current.Keys) != 1 || personal.Current.Keys[0].ID != base.KeyID {
		t.Fatal("history IDs, names or exact currency-separated amounts changed", personal.Current)
	}
	team, project, platform := report(teamPath+query, actorCookie), report(projectPath+query, actorCookie), report("/api/v1/admin/usage"+query, adminCookie)
	for _, value := range []service.UsageReport{team, project, platform} {
		checkRange(value)
	}
	if team.TeamID != teamID || team.Current.Summary.Requests != 2 || len(team.Current.Keys) != 0 || !reflect.DeepEqual(team.AvailableDimensions, []string{"model"}) || team.Previous.Summary.Requests != 1 || project.Current.Summary.Requests != 1 || project.Previous.Summary.Requests != 1 || platform.Current.Summary.Requests != 9 || platform.Previous.Summary.Requests != 3 || len(platform.Current.Providers) != 1 || platform.Current.Providers[0].Name != base.ProviderName {
		t.Fatal("Personal, Team, Project or administrator scope broadened", team, project, platform)
	}
	for _, path := range []string{personalPath, teamPath, projectPath} {
		body := read(path+query, actorCookie, 200).Body.String()
		for _, private := range []string{base.ProviderID, base.ProviderName, base.ProviderModelID, base.UpstreamModelName, base.ConnectionID, base.ConnectionName, "immutable price basis"} {
			if strings.Contains(body, private) {
				t.Fatal("scoped report exposed administrator route details", path, private)
			}
		}
	}
	if value := report("/api/v1/admin/usage"+query+"&user_id="+actor.ID, adminCookie); value.Current.Summary.Requests != 5 {
		t.Fatal("administrator Personal filter borrowed Team actor or Project attribution", value.Current)
	}
	if value := report("/api/v1/admin/usage"+query+"&team_id="+teamID, adminCookie); value.Current.Summary.Requests != 2 || len(value.Current.Keys) != 0 {
		t.Fatal("administrator Team filter borrowed another source", value.Current)
	}
	read(personalPath+query, nil, 401)
	read("/api/v1/admin/usage"+query, actorCookie, 403)
	read(teamPath+query, adminCookie, 404)
	read(projectPath+query, otherCookie, 404)
	read("/api/v1/teams/"+strings.ToUpper(teamID)+"/usage"+query, actorCookie, 404)
	read("/api/v1/teams/"+teamID+"%20/usage"+query, actorCookie, 404)
	if alias := report("/api/v1/admin/usage"+query+"&team_id="+strings.ToUpper(teamID), adminCookie); alias.Current.Summary.Requests != 0 {
		t.Fatal("collated Team alias broadened a historical filter", alias.Current)
	}
	for _, suffix := range []string{"&user_id=" + other.ID, "&team_id=" + teamID, "&project_id=" + projectID, "&provider_id=" + base.ProviderID, "&currency=USD", "&period=month", "&model_id=mdl_%25", "&timezone=Local", "&from=2026-01-01T00%3A00%3A00Z&to=2026-01-02T00%3A00%3A00Z"} {
		read(personalPath+query+suffix, actorCookie, 400)
	}
	read(teamPath+query+"&key_id="+base.KeyID, actorCookie, 400)
	read("/api/v1/admin/usage"+query+"&team_id="+teamID+"&user_id="+actor.ID, adminCookie, 400)
	memberOverviewUsageCalendarEdges(t, db, svc, router, actor, actorCookie, base)
	// Current authority can disappear without deleting or reattributing history.
	if err := db.Where("id = ?", "tmm_home_original").Delete(&entity.TeamMembership{}).Error; err != nil {
		t.Fatal(err)
	}
	read(teamPath+query, actorCookie, 404)
	if err := db.Create(&entity.TeamMembership{ID: "tmm_home_rejoined", TeamID: teamID, UserID: actor.ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if rejoined := report(teamPath+query, actorCookie); !reflect.DeepEqual(rejoined.Current.Summary, team.Current.Summary) {
		t.Fatal("new membership reset or reassigned historical usage", rejoined.Current)
	}
	if err := db.Where("id = ?", "pmg_home_original").Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	read(projectPath+query, actorCookie, 404)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if value, err := svc.PersonalUsage(canceled, actor.ID, service.UsageFilter{Period: "30d"}); err == nil || value != nil {
		t.Fatal("canceled read fabricated a completed report", value, err)
	}
	if after := report(personalPath+query, actorCookie); !reflect.DeepEqual(after.Current.Summary, stats) {
		t.Fatal("denied/canceled reads changed retained counts or success semantics", after.Current.Summary)
	}
	// Existing usage siblings cover the 10000-row limit. This isolated package
	// proves complete-query rejection for bucket and model-group budgets.
	hourly := report(personalPath+"?period=30d&granularity=hour&compare=true", actorCookie)
	if hourly.Current.To.Sub(hourly.Current.From) != 720*time.Hour || len(hourly.Current.Trend) < 720 || len(hourly.Current.Trend) > 721 || hourly.Previous == nil || len(hourly.Previous.Trend) < 720 || len(hourly.Previous.Trend) > 721 || !reflect.DeepEqual(hourly.Current.Summary, stats) {
		t.Fatal("valid hourly comparison was rejected, clipped or changed selected facts", hourly)
	}
	read(personalPath+"?period=90d&granularity=hour", actorCookie, 422)
	rows := make([]entity.CallRecord, 501)
	for index := range rows {
		rows[index] = entity.CallRecord{RequestID: fmt.Sprintf("req_home_group_%03d", index), UserID: actor.ID, KeyID: "key_home_groups", ModelID: fmt.Sprintf("mdl_home_group_%03d", index), ModelName: "Historical group", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: anchor.Add(-time.Hour), CompletedAt: anchor, InputTokens: &zero, OutputTokens: &zero, CallPricingFields: entity.CallPricingFields{PricingStatus: "no_work"}}
	}
	if err := db.CreateInBatches(&rows, 100).Error; err != nil {
		t.Fatal(err)
	}
	tooLarge := read(personalPath+"?period=30d&key_id=key_home_groups", actorCookie, 422)
	if strings.Contains(tooLarge.Body.String(), `"current"`) {
		t.Fatal("group overflow returned a partial Home summary")
	}
}

func memberOverviewUsageCalendarEdges(t *testing.T, db *gorm.DB, svc *service.Service, router http.Handler, actor entity.User, cookie *http.Cookie, base service.CallFact) {
	t.Helper()
	// Explicit historical ranges keep the real-driver DST proof deterministic.
	from := time.Date(2024, 3, 9, 12, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 12, 6, 0, 0, 0, time.UTC)
	for index, instant := range []time.Time{from.Add(-time.Millisecond), from, time.Date(2024, 3, 10, 6, 59, 59, 0, time.UTC), time.Date(2024, 3, 10, 7, 0, 0, 0, time.UTC), to.Add(-time.Millisecond), to} {
		fact := base
		fact.RequestID, fact.ModelID, fact.KeyID = fmt.Sprintf("req_home_dst_%d", index), "mdl_home_dst", "key_home_dst"
		fact.UserID, fact.StartedAt, fact.CompletedAt = actor.ID, instant, instant.Add(time.Second)
		if err := svc.RecordCall(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	query := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "timezone": {"America/New_York"}, "granularity": {"day"}, "model_id": {"mdl_home_dst"}}
	response := identityRequest(router, "GET", "/api/v1/usage?"+query.Encode(), "", cookie, "")
	value := decodeCatalogResponse[service.UsageReport](t, response, 200)
	if !value.Current.From.Equal(from) || !value.Current.To.Equal(to) || value.Current.Summary.Requests != 4 || len(value.Current.Trend) != 4 {
		t.Fatal("inclusive-from/exclusive-to selection or calendar edges changed", value.Current)
	}
	if spring := value.Current.Trend[1]; spring.End.Sub(spring.Start) != 23*time.Hour || spring.Stats.Requests != 2 || value.Current.Trend[0].Stats.Requests != 1 || value.Current.Trend[3].Stats.Requests != 1 {
		t.Fatal("DST day bucket or clipped edge selection changed", value.Current.Trend)
	}
	// Remove only this helper's historical facts so later scopes stay bounded.
	if err := db.Where("key_id = ?", "key_home_dst").Delete(&entity.CallRecord{}).Error; err != nil {
		t.Fatal(err)
	}
}
