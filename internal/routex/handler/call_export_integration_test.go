package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

var callExportMemberColumns = []string{
	"request_id", "model_id", "model_name", "key_id", "protocol", "status", "stream",
	"started_at", "completed_at", "duration_ms", "input_tokens", "output_tokens",
	"cache_read_tokens", "cache_write_tokens", "image_inputs", "pdf_inputs",
	"pricing_status", "charge_amount", "charge_currency",
}

func readCallCSV(t *testing.T, response *httptest.ResponseRecorder, status int) [][]string {
	t.Helper()
	expectStatus(t, response, status)
	rows, err := csv.NewReader(strings.NewReader(response.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("invalid call CSV: %v", err)
	}
	return rows
}

func callCSVColumn(t *testing.T, header []string, name string) int {
	t.Helper()
	index := slices.Index(header, name)
	if index < 0 {
		t.Fatalf("missing CSV column %q in %v", name, header)
	}
	return index
}

// Invoked by the sole database lifecycle owner with a fresh migrated database.
func testCallExportLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"call-export-admin@example.invalid","password":"call-export-password","name":"Call export admin"}`, nil, "")
	expectStatus(t, setup, http.StatusCreated)
	admin, adminCookie := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	users := []entity.User{
		{ID: "usr_export_manager", Email: "call-export-manager@example.invalid", Name: "Manager", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
		{ID: "usr_export_other", Email: "call-export-other@example.invalid", Name: "Other", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	login := func(email string) *http.Cookie {
		response := identityRequest(router, "POST", "/api/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":"call-export-password"}`, email), nil, "")
		expectStatus(t, response, http.StatusOK)
		_, cookie := readIdentity(t, response)
		return cookie
	}
	managerCookie, otherCookie := login(users[0].Email), login(users[1].Email)

	projectID := "prj_call_export"
	if err := db.Create(&entity.Project{ID: projectID, Name: "Archived call export", Status: entity.ResourceArchived, CreatorID: users[1].ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ProjectManager{ID: "pjm_call_export", ProjectID: projectID, UserID: users[0].ID}).Error; err != nil {
		t.Fatal(err)
	}
	zero, input, output, cacheRead, cacheWrite, images, pdfs := int64(0), int64(11), int64(7), int64(3), int64(2), int64(1), int64(0)
	amount, currency := "0.001200", "USD"
	started := time.Date(2026, 9, 29, 9, 0, 0, 123456000, time.UTC)
	base := entity.CallRecord{
		CallPricingFields: entity.CallPricingFields{CacheReadTokens: &cacheRead, CacheWriteTokens: &cacheWrite, PricingStatus: "priced", ChargeAmount: &amount, ChargeCurrency: &currency},
		SnapshotID:        "cfg_export",
		KeyID:             "key_export",
		ModelID:           "mdl_export",
		ModelName:         " \t=HYPERLINK(\"https://invalid.example\")",
		ProviderModelID:   "pmd_export_private",
		ConnectionID:      "con_export_private",
		RouteStopReason:   "succeeded",
		Protocol:          entity.ProtocolOpenAIChat,
		Status:            "success",
		StartedAt:         started,
		CompletedAt:       started.Add(250 * time.Millisecond),
		DurationMS:        250,
		InputTokens:       &input,
		OutputTokens:      &output,
		ImageInputs:       &images,
		PDFInputs:         &pdfs,
		ErrorCode:         "",
	}
	managerPersonal := base
	managerPersonal.RequestID, managerPersonal.UserID = "req_export_manager", users[0].ID
	otherPersonal := base
	otherPersonal.RequestID, otherPersonal.UserID, otherPersonal.KeyID = "req_export_other", users[1].ID, "key_export_other"
	otherPersonal.ModelName, otherPersonal.Status = "Other model", "error"
	otherPersonal.InputTokens, otherPersonal.OutputTokens = nil, nil
	otherPersonal.CacheReadTokens, otherPersonal.CacheWriteTokens = nil, nil
	otherPersonal.ImageInputs, otherPersonal.PDFInputs = nil, nil
	otherPersonal.PricingStatus, otherPersonal.ChargeAmount, otherPersonal.ChargeCurrency = "not_captured", nil, nil
	project := base
	project.RequestID, project.UserID, project.ProjectID = "req_export_project", "", projectID
	project.ModelName, project.InputTokens, project.OutputTokens = "Project model", &zero, &zero
	adminPersonal := base
	adminPersonal.RequestID, adminPersonal.UserID, adminPersonal.ModelName = "req_export_admin", admin.User.ID, "Admin model"
	if err := db.Create([]entity.CallRecord{managerPersonal, otherPersonal, project, adminPersonal}).Error; err != nil {
		t.Fatal(err)
	}

	get := func(path string, cookie *http.Cookie) *httptest.ResponseRecorder {
		return identityRequest(router, "GET", path, "", cookie, "")
	}
	personalPath := "/api/v1/calls/export.csv"
	personalListPath := "/api/v1/calls"
	projectPath := "/api/v1/projects/" + projectID + "/calls/export.csv"
	projectListPath := "/api/v1/projects/" + projectID + "/calls"
	adminPath := "/api/v1/admin/calls/export.csv"
	adminListPath := "/api/v1/admin/calls"
	expectStatus(t, get(personalPath, nil), http.StatusUnauthorized)

	personalResponse := get(personalPath, managerCookie)
	personalRows := readCallCSV(t, personalResponse, http.StatusOK)
	if len(personalRows) != 2 || !slices.Equal(personalRows[0], callExportMemberColumns) {
		t.Fatalf("personal export shape = %v", personalRows)
	}
	requestColumn := callCSVColumn(t, personalRows[0], "request_id")
	nameColumn := callCSVColumn(t, personalRows[0], "model_name")
	if personalRows[1][requestColumn] != managerPersonal.RequestID || personalRows[1][nameColumn] != "'"+managerPersonal.ModelName {
		t.Fatalf("personal export lost isolation or formula protection: %v", personalRows[1])
	}
	for _, forbidden := range []string{"user_id", "project_id", "provider_model_id", "connection_id", "error_code", "route_stop_reason", "attempts", "price_etag", "pricing_snapshot"} {
		if slices.Contains(personalRows[0], forbidden) || strings.Contains(personalResponse.Body.String(), "export_private") {
			t.Fatalf("personal export exposed %s", forbidden)
		}
	}
	if personalResponse.Header().Get("Content-Type") != "text/csv; charset=utf-8" || personalResponse.Header().Get("Content-Disposition") != `attachment; filename="routex-personal-calls.csv"` || personalResponse.Header().Get("Cache-Control") != "private, no-store" || personalResponse.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("personal download headers = %v", personalResponse.Header())
	}

	projectResponse := get(projectPath, managerCookie)
	projectRows := readCallCSV(t, projectResponse, http.StatusOK)
	if len(projectRows) != 2 || !slices.Equal(projectRows[0], callExportMemberColumns) || projectRows[1][requestColumn] != project.RequestID {
		t.Fatalf("Project export crossed scope: %v", projectRows)
	}
	if projectResponse.Header().Get("Content-Disposition") != `attachment; filename="routex-project-calls.csv"` {
		t.Fatalf("Project filename = %q", projectResponse.Header().Get("Content-Disposition"))
	}
	expectStatus(t, get(projectPath, otherCookie), http.StatusNotFound)
	expectStatus(t, get("/api/v1/projects/prj_missing/calls/export.csv", otherCookie), http.StatusNotFound)

	expectStatus(t, get(adminPath, otherCookie), http.StatusForbidden)
	platformColumns := append(append([]string{}, callExportMemberColumns...), "user_id", "project_id")
	adminResponse := get(adminPath, adminCookie)
	adminRows := readCallCSV(t, adminResponse, http.StatusOK)
	if len(adminRows) != 5 || !slices.Equal(adminRows[0], platformColumns) {
		t.Fatalf("platform export shape = %v", adminRows)
	}
	if adminResponse.Header().Get("Content-Disposition") != `attachment; filename="routex-platform-calls.csv"` {
		t.Fatalf("platform filename = %q", adminResponse.Header().Get("Content-Disposition"))
	}
	for _, forbidden := range []string{"provider_model_id", "connection_id", "error_code", "route_stop_reason", "attempts", "price_etag", "pricing_snapshot"} {
		if slices.Contains(adminRows[0], forbidden) || strings.Contains(adminResponse.Body.String(), "export_private") {
			t.Fatalf("platform export exposed detail-only field %s", forbidden)
		}
	}

	filtered := readCallCSV(t, get(adminPath+"?user_id="+users[1].ID+"&status=error&model_id=mdl_export&key_id=key_export_other&from="+url.QueryEscape(started.Format(time.RFC3339Nano))+"&to="+url.QueryEscape(started.Format(time.RFC3339Nano)), adminCookie), http.StatusOK)
	if len(filtered) != 2 || filtered[1][requestColumn] != otherPersonal.RequestID {
		t.Fatalf("export filters diverged from call list: %v", filtered)
	}
	filteredList := decodeCatalogResponse[AdminCallsResponse](t, get(adminListPath+"?user_id="+users[1].ID, adminCookie), http.StatusOK)
	if len(filteredList.Items) != 1 || filteredList.Items[0].RequestID != otherPersonal.RequestID {
		t.Fatalf("valid platform user filter failed: %+v", filteredList.Items)
	}
	empty := readCallCSV(t, get(personalPath+"?status=canceled", managerCookie), http.StatusOK)
	if len(empty) != 1 || !slices.Equal(empty[0], callExportMemberColumns) {
		t.Fatalf("empty export must retain only its header: %v", empty)
	}
	for _, item := range []struct {
		name   string
		path   string
		cookie *http.Cookie
	}{
		{"personal list current user", personalListPath + "?user_id=" + users[0].ID, managerCookie},
		{"personal export current user", personalPath + "?user_id=" + users[0].ID, managerCookie},
		{"Project list user", projectListPath + "?user_id=" + users[0].ID, managerCookie},
		{"Project export user", projectPath + "?user_id=" + users[0].ID, managerCookie},
		{"personal list model", personalListPath + "?model_id=bad%21model", managerCookie},
		{"personal export model", personalPath + "?model_id=bad%21model", managerCookie},
		{"personal list Key", personalListPath + "?key_id=bad%20key", managerCookie},
		{"personal export Key", personalPath + "?key_id=bad%20key", managerCookie},
		{"Project list model", projectListPath + "?model_id=bad%21model", managerCookie},
		{"Project export model", projectPath + "?model_id=bad%21model", managerCookie},
		{"Project list Key", projectListPath + "?key_id=bad%20key", managerCookie},
		{"Project export Key", projectPath + "?key_id=bad%20key", managerCookie},
		{"platform list model", adminListPath + "?model_id=bad%21model", adminCookie},
		{"platform export model", adminPath + "?model_id=bad%21model", adminCookie},
		{"platform list Key", adminListPath + "?key_id=bad%20key", adminCookie},
		{"platform export Key", adminPath + "?key_id=bad%20key", adminCookie},
		{"platform list user", adminListPath + "?user_id=bad%21user", adminCookie},
		{"platform export user", adminPath + "?user_id=bad%21user", adminCookie},
		{"personal cursor", personalPath + "?cursor=opaque", managerCookie},
		{"Project limit", projectPath + "?limit=1", managerCookie},
		{"platform cursor", adminPath + "?cursor=opaque", adminCookie},
		{"platform limit", adminPath + "?limit=1", adminCookie},
		{"status", adminPath + "?status=unknown", adminCookie},
		{"timestamp", adminPath + "?from=bad", adminCookie},
		{"range", adminPath + "?from=2026-09-30T00%3A00%3A00Z&to=2026-09-29T00%3A00%3A00Z", adminCookie},
	} {
		t.Run(item.name, func(t *testing.T) {
			expectStatus(t, get(item.path, item.cookie), http.StatusBadRequest)
		})
	}

	role := entity.Role{ID: "rol_call_export", Name: "Call export reader", NameKey: "call export reader"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: role.ID, Permission: "calls.read_all"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.UserRole{UserID: users[1].ID, RoleID: role.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if rows := readCallCSV(t, get(adminPath, otherCookie), http.StatusOK); len(rows) != len(adminRows) {
		t.Fatal("delegated calls.read_all did not authorize the platform export")
	}
	if rows := readCallCSV(t, get(projectPath, otherCookie), http.StatusOK); len(rows) != 2 || rows[1][requestColumn] != project.RequestID {
		t.Fatal("delegated calls.read_all did not authorize the Project export")
	}
}
