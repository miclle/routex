package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

// Invoked by the sole database lifecycle owner with a fresh migrated database.
func testUsageLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"usage-admin@example.invalid","password":"test-only-usage-password","name":"Usage admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{{ID: "usr_usage_manager", Email: "usage-manager@example.invalid", Name: "Manager", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}, {ID: "usr_usage_other", Email: "usage-other@example.invalid", Name: "Other", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(email string) *http.Cookie {
		response := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"test-only-usage-password"}`, email), nil, "")
		expectStatus(t, response, 200)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	managerCookie, otherCookie := login(users[0].Email), login(users[1].Email)
	projectID := "prj_usage"
	if err := db.Create(&entity.Project{ID: projectID, Name: "Historical usage", Status: entity.ResourceArchived, CreatorID: users[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pjm_usage", ProjectID: projectID, UserID: users[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	input, output := int64(10), int64(2)
	amount, currency, snapshot := "0.1", "USD", "{}"
	fact := service.CallFact{RequestID: "req_usage_personal", UserID: users[0].ID, KeyID: "key_usage", ModelID: "mdl_usage", ModelName: "Historical name", ProviderID: "prv_usage", ProviderName: "Historical provider", ProviderModelID: "pmd_usage", UpstreamModelName: "Historical upstream", ConnectionID: "con_usage", ConnectionName: "Historical connection", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started.Add(150 * time.Millisecond), InputTokens: &input, OutputTokens: &output, Pricing: &service.CallPricing{Status: "priced", Amount: &amount, Currency: &currency, SnapshotJSON: &snapshot}}
	for range 2 {
		if err := svc.RecordCall(context.Background(), fact); err != nil {
			t.Fatal(err)
		}
	}
	second := fact
	second.RequestID = "req_usage_partial"
	second.InputTokens = nil
	second.Status = "error"
	second.Pricing = nil
	if err := svc.RecordCall(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	previous := fact
	previous.RequestID = "req_usage_previous"
	previous.StartedAt = started.Add(-24 * time.Hour)
	previous.CompletedAt = previous.StartedAt.Add(time.Second)
	if err := svc.RecordCall(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	project := fact
	project.RequestID = "req_usage_project"
	project.UserID = ""
	project.ProjectID = projectID
	project.Pricing = nil
	if err := svc.RecordCall(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	other := fact
	other.RequestID = "req_usage_other"
	other.UserID = users[1].ID
	other.KeyID = "key_other"
	other.ModelID = "mdl_other"
	other.ProviderID = "prv_legacy"
	other.ProviderName = "Legacy provider"
	other.ProviderModelID = "pmd_legacy"
	other.UpstreamModelName = "Legacy upstream"
	other.ConnectionID = "con_legacy"
	other.ConnectionName = "Legacy connection"
	if err := svc.RecordCall(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", other.RequestID).Updates(map[string]any{"provider_name": "", "upstream_model_name": "", "connection_name": ""}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.Provider{ID: fact.ProviderID, Name: "Live renamed provider"}).Error; err != nil {
		t.Fatal(err)
	}
	query := "?from=2026-08-02T00%3A00%3A00Z&to=2026-08-03T00%3A00%3A00Z&granularity=hour&compare=true"
	read := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		return identityRequest(router, "GET", path, "", cookie, "")
	}
	expectStatus(t, read("/api/v1/usage"+query, nil), 401)
	expectStatus(t, read("/api/v1/admin/usage"+query, managerCookie), 403)
	report := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query, managerCookie), 200)
	if report.Current.Summary.Requests != 2 || report.Current.Summary.Tokens.Input.Value != nil || report.Current.Summary.Tokens.Total.Known != "14" || report.Current.Summary.UnknownAmountCalls != 1 || len(report.Current.Summary.Amounts) != 1 || report.Current.Summary.Amounts[0].Amount != "0.1" || report.Previous == nil || report.Previous.Summary.Requests != 1 {
		t.Fatalf("canonical/partial/compare totals invalid: %+v", report)
	}
	if len(report.Current.Trend) != 24 || report.Source != "persisted_call_records" || !report.MayLag || report.LatestCompletedAt == nil {
		t.Fatal("query range/freshness metadata missing")
	}
	personalBody := read("/api/v1/usage"+query, managerCookie).Body.String()
	for _, private := range []string{"prv_usage", "Historical provider", "pmd_usage", "Historical upstream", "con_usage", "Historical connection", "providers", "provider_models", "connections", "snapshot_json"} {
		if strings.Contains(personalBody, private) {
			t.Fatalf("personal statistics exposed %s", private)
		}
	}
	guessed := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query+"&key_id=key_other", managerCookie), 200)
	if guessed.Current.Summary.Requests != 0 {
		t.Fatal("guessed key crossed personal ownership")
	}
	guessedModel := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query+"&model_id=mdl_other", managerCookie), 200)
	if guessedModel.Current.Summary.Requests != 0 {
		t.Fatal("guessed model crossed personal ownership")
	}
	streamed := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query+"&stream=true", managerCookie), 200)
	if streamed.Current.Summary.Requests != 0 {
		t.Fatal("stream filter ignored")
	}
	expectStatus(t, read("/api/v1/usage"+query+"&user_id="+users[1].ID, managerCookie), 400)
	expectStatus(t, read("/api/v1/usage"+query+"&provider_id=prv_usage", managerCookie), 400)
	expectStatus(t, read("/api/v1/usage"+query+"&team_id=team_hidden", managerCookie), 400)
	expectStatus(t, read("/api/v1/usage"+query+"&timezone=invalid", managerCookie), 400)
	projectPath := "/api/v1/projects/" + projectID + "/usage"
	projectResponse := read(projectPath+query, managerCookie)
	projectReport := decodeCatalogResponse[service.UsageReport](t, projectResponse, 200)
	if projectReport.Current.Summary.Requests != 1 {
		t.Fatal("current manager cannot read archived Project history")
	}
	for _, private := range []string{"prv_usage", "Historical provider", "pmd_usage", "Historical upstream", "con_usage", "Historical connection", "providers", "provider_models", "connections"} {
		if strings.Contains(projectResponse.Body.String(), private) {
			t.Fatalf("Project statistics exposed %s", private)
		}
	}
	expectStatus(t, read(projectPath+query+"&provider_id=prv_usage", managerCookie), 400)
	expectStatus(t, read(projectPath+query, otherCookie), 404) // The creator is not implicitly a manager.
	expectStatus(t, read("/api/v1/projects/prj_missing/usage"+query, adminCookie), 404)
	adminResponse := read("/api/v1/admin/usage"+query, adminCookie)
	adminReport := decodeCatalogResponse[service.UsageReport](t, adminResponse, 200)
	if adminReport.Current.Summary.Requests != 4 || len(adminReport.Current.Providers) != 2 || len(adminReport.Current.Connections) != 2 {
		t.Fatal("platform aggregation lost canonical route snapshots")
	}
	if !slices.Contains(adminReport.AvailableDimensions, "provider") || !strings.Contains(adminResponse.Body.String(), `"providers"`) {
		t.Fatal("platform report omitted the provider dimension")
	}
	var historicalProvider, legacyProvider *service.UsageGroup
	for index := range adminReport.Current.Providers {
		group := &adminReport.Current.Providers[index]
		switch group.ID {
		case fact.ProviderID:
			historicalProvider = group
		case other.ProviderID:
			legacyProvider = group
		}
	}
	if historicalProvider == nil || historicalProvider.Name != fact.ProviderName || historicalProvider.Name == "Live renamed provider" || historicalProvider.Stats.Requests != 3 {
		t.Fatalf("provider history was not snapshot-stable: %+v", historicalProvider)
	}
	if legacyProvider == nil || legacyProvider.Name != "" || legacyProvider.Unknown {
		t.Fatalf("legacy provider ID/name semantics changed: %+v", legacyProvider)
	}
	findGroup := func(groups []service.UsageGroup, id string) *service.UsageGroup {
		for index := range groups {
			if groups[index].ID == id {
				return &groups[index]
			}
		}
		return nil
	}
	historicalProviderModel := findGroup(adminReport.Current.ProviderModels, fact.ProviderModelID)
	historicalConnection := findGroup(adminReport.Current.Connections, fact.ConnectionID)
	if historicalProviderModel == nil || historicalProviderModel.Name != fact.UpstreamModelName || historicalConnection == nil || historicalConnection.Name != fact.ConnectionName {
		t.Fatal("historical provider-model or connection labels were omitted")
	}
	legacyProviderModel := findGroup(adminReport.Current.ProviderModels, other.ProviderModelID)
	legacyConnection := findGroup(adminReport.Current.Connections, other.ConnectionID)
	if legacyProviderModel == nil || legacyProviderModel.Name != "" || legacyProviderModel.Unknown || legacyConnection == nil || legacyConnection.Name != "" || legacyConnection.Unknown {
		t.Fatalf("legacy topology IDs/names changed: provider_model=%+v connection=%+v", legacyProviderModel, legacyConnection)
	}
	filtered := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/admin/usage"+query+"&project_id="+projectID+"&connection_id=con_usage", adminCookie), 200)
	if filtered.Current.Summary.Requests != 1 {
		t.Fatal("admin Project/connection filters lost")
	}
	providerFiltered := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/admin/usage"+query+"&provider_id="+fact.ProviderID, adminCookie), 200)
	if providerFiltered.Current.Summary.Requests != 3 || len(providerFiltered.Current.Providers) != 1 || providerFiltered.Current.Providers[0].ID != fact.ProviderID {
		t.Fatal("admin provider filter or grouping lost")
	}
	expectStatus(t, read("/api/v1/admin/usage"+query+"&provider_id=prv_%25", adminCookie), 400)
	// Legacy inconsistent creator attribution must not leak Project calls into personal totals.
	if err := db.Model(&entity.CallRecord{}).Where("request_id = ?", project.RequestID).Update("user_id", users[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	report = decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query, managerCookie), 200)
	if report.Current.Summary.Requests != 2 {
		t.Fatal("Project creator attribution leaked into personal totals")
	}
	if err := db.Where("project_id = ? AND user_id = ?", projectID, users[0].ID).Delete(&entity.ProjectManager{}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, read(projectPath+query, managerCookie), 404)
	if err := db.Model(&entity.User{}).Where("id = ?", users[0].ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PersonalUsage(context.Background(), users[0].ID, service.UsageFilter{Period: "7d"}); err == nil {
		t.Fatal("disabled actor retained service-level usage access")
	}
	// A complete result is returned or rejected; no pagination can masquerade as a total.
	rows := make([]entity.CallRecord, 10001)
	for i := range rows {
		rows[i] = entity.CallRecord{RequestID: fmt.Sprintf("req_usage_overflow_%d", i), UserID: users[1].ID, KeyID: "key_overflow", ModelID: "mdl_overflow", ModelName: "Overflow", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: started, CompletedAt: started, CallPricingFields: entity.CallPricingFields{PricingStatus: "not_captured"}}
	}
	if err := db.CreateInBatches(&rows, 250).Error; err != nil {
		t.Fatal(err)
	}
	overflow := read("/api/v1/usage"+query+"&model_id=mdl_overflow", otherCookie)
	expectStatus(t, overflow, 422)
	if strings.Contains(overflow.Body.String(), `"current"`) {
		t.Fatal("overflow returned partial statistics")
	}
	if err := db.Where("request_id = ?", rows[10000].RequestID).Delete(&entity.CallRecord{}).Error; err != nil {
		t.Fatal(err)
	}
	bounded := decodeCatalogResponse[service.UsageReport](t, read("/api/v1/usage"+query+"&model_id=mdl_overflow", otherCookie), 200)
	if bounded.Current.Summary.Requests != 10000 {
		t.Fatal("exact row limit did not return complete total")
	}
	extra := rows[10000]
	extra.RequestID = "req_usage_overflow_previous"
	extra.StartedAt = started.Add(-24 * time.Hour)
	extra.CompletedAt = extra.StartedAt
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, read("/api/v1/usage"+query+"&model_id=mdl_overflow", otherCookie), 422)
}
