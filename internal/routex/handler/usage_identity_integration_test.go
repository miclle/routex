package handler

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

// The lifecycle harness supplies each real driver. Seeded immutable facts prove
// selection/export identity, without claiming native dispatch or completion.
func testUsageIdentityLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"usage-identity-admin@example.invalid","password":"test-only-usage-identity","name":"Identity administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{
		{ID: "usr_usage_exact", Name: "Exact actor", Email: "usage-exact@example.invalid", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_usage_foreign", Name: "Foreign actor", Email: "usage-foreign@example.invalid", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(email string) *http.Cookie {
		t.Helper()
		response := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-usage-identity"}`, email), nil, "")
		expectStatus(t, response, 200)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	actorCookie, peerCookie := login(users[0].Email), login(users[1].Email)
	const projectID, teamID = "prj_usage_exact", "tea_usage_exact"
	if err := db.Create(&entity.Project{ID: projectID, Name: "Archived exact history", Status: entity.ResourceArchived, CreatorID: users[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pjm_usage_exact", ProjectID: projectID, UserID: users[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Team{ID: teamID, Name: "Exact Team", Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.TeamMembership{ID: "tmm_usage_exact", TeamID: teamID, UserID: users[0].ID, Role: entity.TeamMember, Status: entity.ResourceActive}).Error; err != nil {
		t.Fatal(err)
	}
	from := time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	input, output := int64(9007199254740993), int64(2)
	amount, currency, snapshot := "0.123456789012345678", "USD", `{"private_basis":"never_expose"}`
	fact := service.CallFact{RequestID: "req_usage_exact_known", UserID: users[0].ID, KeyID: "key_usage_exact", ModelID: "mdl_usage_exact", ModelName: "Immutable exact label", ProviderID: "prv_usage_exact", ProviderName: "Private provider", ProviderModelID: "pmd_usage_exact", UpstreamModelName: "Private upstream", ConnectionID: "con_usage_exact", ConnectionName: "Private connection", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: from, CompletedAt: from.Add(time.Second), InputTokens: &input, OutputTokens: &output, Pricing: &service.CallPricing{Status: "priced", Amount: &amount, Currency: &currency, SnapshotJSON: &snapshot}}
	unknown := fact
	unknown.RequestID = "req_usage_exact_unknown"
	unknown.InputTokens = nil
	unknown.Pricing = nil
	unknown.Status = "error"
	project := fact
	project.RequestID = "req_usage_exact_project"
	project.UserID = ""
	project.ProjectID = projectID
	project.KeyID = "key_usage_project"
	team := fact
	team.RequestID = "req_usage_exact_team"
	team.TeamID = teamID
	team.TeamMembershipID = "tmm_usage_exact"
	team.KeyID = ""
	peer := fact
	peer.RequestID = "req_usage_exact_foreign"
	peer.UserID = users[1].ID
	peer.KeyID = "key_usage_foreign"
	peer.ModelID = "mdl_usage_foreign"
	peer.ProviderID = "prv_usage_foreign"
	peer.ProviderModelID = "pmd_usage_foreign"
	peer.ConnectionID = "con_usage_foreign"
	for _, row := range []service.CallFact{fact, unknown, project, team, peer, fact} {
		if err := svc.RecordCall(context.Background(), row); err != nil {
			t.Fatal(err)
		}
	}
	// Historical blank attribution is exact too: a legacy space is not a
	// Personal scope, even on a database that pads text comparisons.
	legacy := entity.CallRecord{RequestID: "req_usage_space", UserID: users[0].ID, ProjectID: " ", KeyID: "key_scope_guard", ModelID: "mdl_scope_guard", ProviderID: "prv_scope_guard", ProviderModelID: "pmd_scope_guard", ConnectionID: "con_scope_guard", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: from, CompletedAt: from.Add(time.Second), CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	var before []entity.CallRecord
	if err := db.Order("request_id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	if len(before) != 6 {
		t.Fatal("seed replay changed immutable request count", len(before))
	}
	query := url.Values{"from": {from.Format(time.RFC3339Nano)}, "to": {to.Format(time.RFC3339Nano)}, "granularity": {"hour"}}
	read := func(path string, cookie *http.Cookie, filter, urlValue string) *httptest.ResponseRecorder {
		t.Helper()
		values := query.Clone()
		if filter != "" {
			values.Set(filter, urlValue)
		}
		return identityRequest(router, "GET", path+"?"+values.Encode(), "", cookie, "")
	}
	assertParity := func(path string, cookie *http.Cookie, filter, value string, want int64) service.UsageReport {
		t.Helper()
		jsonResponse := read(path, cookie, filter, value)
		report := decodeCatalogResponse[service.UsageReport](t, jsonResponse, 200)
		if report.Current.Summary.Requests != want {
			t.Fatalf("%s %s=%q returned %d requests, want %d: %s", path, filter, value, report.Current.Summary.Requests, want, jsonResponse.Body.String())
		}
		csvResponse := read(path+"/export.csv", cookie, filter, value)
		expectStatus(t, csvResponse, 200)
		records, err := csv.NewReader(strings.NewReader(csvResponse.Body.String())).ReadAll()
		if err != nil || len(records) < 2 {
			t.Fatal("CSV decode", err)
		}
		columns := map[string]int{}
		for index, column := range records[0] {
			columns[column] = index
		}
		summaries := 0
		amounts := map[string]string{}
		for _, row := range records[1:] {
			get := func(column string) string { return row[columns[column]] }
			if get("period") != "current" || get("section") != "summary" {
				continue
			}
			switch get("row_type") {
			case "stats":
				summaries++
				if get("requests") != strconv.FormatInt(want, 10) || get("input_known") != "'"+report.Current.Summary.Tokens.Input.Known || get("total_known") != "'"+report.Current.Summary.Tokens.Total.Known || get("total_unknown_calls") != strconv.FormatInt(report.Current.Summary.Tokens.Total.UnknownCalls, 10) {
					t.Fatal("CSV changed exact report selection or known subtotal", row)
				}
				expected := ""
				if report.Current.Summary.Tokens.Total.Value != nil {
					expected = "'" + *report.Current.Summary.Tokens.Total.Value
				}
				if get("total_value") != expected {
					t.Fatal("CSV changed null versus exact total", row)
				}
			case "amount":
				amounts[strings.TrimPrefix(get("currency"), "'")] = strings.TrimPrefix(get("amount"), "'")
			}
		}
		if summaries != 1 || len(amounts) != len(report.Current.Summary.Amounts) {
			t.Fatal("CSV summary/amount parent mismatch", summaries, amounts)
		}
		for _, item := range report.Current.Summary.Amounts {
			if amounts[item.Currency] != item.Amount {
				t.Fatal("CSV altered historical decimal", item, amounts)
			}
		}
		for _, secret := range []string{"private_basis", "never_expose", "snapshot_json"} {
			if strings.Contains(jsonResponse.Body.String(), secret) || strings.Contains(csvResponse.Body.String(), secret) {
				t.Fatal("private pricing basis escaped", secret)
			}
		}
		if !strings.Contains(path, "/admin/") {
			for _, private := range []string{"prv_usage_exact", "pmd_usage_exact", "con_usage_exact", "Private provider", "Private upstream", "Private connection"} {
				if strings.Contains(jsonResponse.Body.String(), private) || strings.Contains(csvResponse.Body.String(), private) {
					t.Fatal("scoped report borrowed route diagnostics", private)
				}
			}
		}
		return report
	}
	personal := assertParity("/api/v1/usage", actorCookie, "", "", 2)
	if personal.Current.Summary.Tokens.Total.Value != nil || personal.Current.Summary.Tokens.Total.Known != "9007199254740997" || personal.Current.Summary.Tokens.Total.UnknownCalls != 1 || personal.Current.Summary.UnknownAmountCalls != 1 {
		t.Fatal("partial precision or foreign ownership changed", personal.Current.Summary)
	}
	assertParity("/api/v1/usage", peerCookie, "", "", 1)
	for _, test := range []struct {
		path          string
		cookie        *http.Cookie
		filter, value string
		want          int64
	}{
		{"/api/v1/usage", actorCookie, "model_id", fact.ModelID, 2},
		{"/api/v1/usage", actorCookie, "key_id", fact.KeyID, 2},
		{"/api/v1/projects/" + projectID + "/usage", actorCookie, "model_id", fact.ModelID, 1},
		{"/api/v1/projects/" + projectID + "/usage", actorCookie, "key_id", project.KeyID, 1},
		{"/api/v1/teams/" + teamID + "/usage", actorCookie, "model_id", fact.ModelID, 1},
		{"/api/v1/admin/usage", adminCookie, "model_id", fact.ModelID, 4},
		{"/api/v1/admin/usage", adminCookie, "key_id", fact.KeyID, 2},
		{"/api/v1/admin/usage", adminCookie, "user_id", users[0].ID, 2},
		{"/api/v1/admin/usage", adminCookie, "project_id", projectID, 1},
		{"/api/v1/admin/usage", adminCookie, "team_id", teamID, 1},
		{"/api/v1/admin/usage", adminCookie, "provider_id", fact.ProviderID, 4},
		{"/api/v1/admin/usage", adminCookie, "provider_model_id", fact.ProviderModelID, 4},
		{"/api/v1/admin/usage", adminCookie, "connection_id", fact.ConnectionID, 4},
	} {
		t.Run(test.filter+"/"+test.path, func(t *testing.T) {
			assertParity(test.path, test.cookie, test.filter, test.value, test.want)
			assertParity(test.path, test.cookie, test.filter, strings.ToUpper(test.value), 0)
			for _, suffix := range []string{"", "/export.csv"} {
				expectStatus(t, read(test.path+suffix, test.cookie, test.filter, test.value+" "), 400)
			}
		})
	}
	assertParity("/api/v1/usage", actorCookie, "model_id", peer.ModelID, 0)
	assertParity("/api/v1/usage", actorCookie, "key_id", peer.KeyID, 0)
	for _, cookie := range []*http.Cookie{actorCookie, adminCookie} {
		for _, suffix := range []string{"", "/export.csv"} {
			expectStatus(t, read("/api/v1/projects/"+strings.ToUpper(projectID)+"/usage"+suffix, cookie, "", ""), 404)
			expectStatus(t, read("/api/v1/projects/"+url.PathEscape(projectID+" ")+"/usage"+suffix, cookie, "", ""), 400)
		}
	}
	for _, suffix := range []string{"", "/export.csv"} {
		expectStatus(t, read("/api/v1/projects/"+projectID+"/usage"+suffix, peerCookie, "", ""), 404)
		expectStatus(t, read("/api/v1/admin/usage"+suffix, actorCookie, "", ""), 403)
	}
	for _, alias := range []string{strings.ToUpper(teamID), teamID + " "} {
		for _, suffix := range []string{"", "/export.csv"} {
			expectStatus(t, read("/api/v1/teams/"+url.PathEscape(alias)+"/usage"+suffix, actorCookie, "", ""), 404)
		}
	}
	filter := service.UsageFilter{From: &from, To: &to, Granularity: "hour"}
	for _, actor := range []string{strings.ToUpper(users[0].ID), users[0].ID + " "} {
		if _, err := svc.PersonalUsage(context.Background(), actor, filter); err != apperrors.ErrUnauthorized {
			t.Fatal("aliased actor read Personal facts", actor)
		}
		if _, err := svc.ProjectUsage(context.Background(), actor, projectID, filter); err != apperrors.ErrUnauthorized {
			t.Fatal("aliased manager read Project facts", actor)
		}
	}
	// Neither rendering nor export changes captured facts or creates attempts.
	var attempts int64
	if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil || attempts != 0 {
		t.Fatal("report/export created a call attempt", attempts, err)
	}
	var after []entity.CallRecord
	if err := db.Order("request_id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("report/export mutated immutable call facts")
	}
	if err := db.Where("id = ?", "pjm_usage_exact").Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/export.csv"} {
		expectStatus(t, read("/api/v1/projects/"+projectID+"/usage"+suffix, actorCookie, "", ""), 404)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", users[0].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PersonalUsage(context.Background(), users[0].ID, filter); err != apperrors.ErrUnauthorized {
		t.Fatal("disabled actor retained direct Personal service access")
	}
}
