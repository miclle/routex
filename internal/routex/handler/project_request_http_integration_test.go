package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func testProjectRequestHTTP(t *testing.T, db *gorm.DB) {
	t.Helper()
	router := identityRouter(t, db)
	admin, adminCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"request-admin@example.invalid","password":"test-only-request-password"}`, nil, ""))
	send := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, csrf)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		return send(adminCookie, admin.CSRFToken, method, path, body)
	}
	expectStatus(t, request("POST", "/api/v1/admin/members", map[string]any{"name": "Request manager", "email": "request-http@example.invalid", "password": "request-http-password"}), 201)
	manager, managerCookie := readIdentity(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"request-http@example.invalid","password":"request-http-password"}`, nil, ""))
	asManager := func(method, path string, body any) *httptest.ResponseRecorder {
		return send(managerCookie, manager.CSRFToken, method, path, body)
	}
	project := decodeCatalogResponse[ProjectResponse](t, asManager("POST", "/api/v1/projects", map[string]any{"name": "HTTP requests"}), 201)
	path := "/api/v1/projects/" + project.ID + "/requests"
	candidatePath := "/api/v1/projects/" + project.ID + "/request-model-candidates"
	expectStatus(t, identityRequest(router, "GET", path, "", nil, ""), 401)
	expectStatus(t, request("GET", candidatePath, nil), 403)
	candidates := decodeCatalogResponse[ResourceCandidatesResponse](t, asManager("GET", candidatePath+"?q=mdl_request_a", nil), 200)
	if len(candidates.Items) != 1 || candidates.Items[0].ID != "mdl_request_a" || candidates.Items[0].Email != "" {
		t.Fatalf("scoped request candidates: %+v", candidates.Items)
	}
	literal := decodeCatalogResponse[ResourceCandidatesResponse](t, asManager("GET", candidatePath+"?q=%25", nil), 200)
	if len(literal.Items) != 0 {
		t.Fatal("candidate search treated a literal percent as wildcard")
	}
	body := map[string]any{"request_id": "req_http_first", "model_ids": []string{"mdl_request_a"}, "reason": "Application workload"}
	expectStatus(t, send(managerCookie, "", "POST", path, body), 403)
	bad := map[string]any{"request_id": "req_http_bad", "kind": "QUOTA", "model_ids": []string{"mdl_request_a"}, "reason": "Invalid kind"}
	expectStatus(t, asManager("POST", path, bad), 400)
	bad["kind"] = "MODEL_ACCESS"
	bad["approved"] = true
	expectStatus(t, asManager("POST", path, bad), 400)
	first := decodeCatalogResponse[service.ProjectRequestRecord](t, asManager("POST", path, body), 201)
	expectStatus(t, asManager("POST", path+"/"+first.ID+"/decision", map[string]any{"action": "approve"}), 403)
	expectStatus(t, request("POST", path+"/"+first.ID+"/decision", map[string]any{"action": "reject"}), 400)
	expectStatus(t, request("POST", path+"/"+first.ID+"/decision", map[string]any{"action": "approve"}), 200)
	filtered := decodeCatalogResponse[ResourceCandidatesResponse](t, asManager("GET", candidatePath+"?q=mdl_request_a", nil), 200)
	if len(filtered.Items) != 0 {
		t.Fatal("already granted model remained a request candidate")
	}
	body["request_id"] = "req_http_second"
	body["model_ids"] = []string{"mdl_request_c"}
	second := decodeCatalogResponse[service.ProjectRequestRecord](t, asManager("POST", path, body), 201)
	page := decodeCatalogResponse[service.ProjectRequestPage](t, asManager("GET", path+"?status=approved&limit=1", nil), 200)
	if len(page.Items) != 1 || page.Items[0].ID != first.ID {
		t.Fatal("HTTP status filter was not bound")
	}
	page = decodeCatalogResponse[service.ProjectRequestPage](t, asManager("GET", path+"?limit=1", nil), 200)
	if len(page.Items) != 1 || page.Items[0].ID != second.ID || page.NextCursor == "" {
		t.Fatal("HTTP page limit was not bound")
	}
	next := decodeCatalogResponse[service.ProjectRequestPage](t, asManager("GET", path+"?cursor="+page.NextCursor, nil), 200)
	if len(next.Items) != 1 || next.Items[0].ID != first.ID {
		t.Fatal("HTTP cursor was not bound")
	}
	expectStatus(t, asManager("GET", path+"?limit=101", nil), 400)
	expectStatus(t, asManager("POST", path+"/"+second.ID+"/decision", map[string]any{"action": "withdraw"}), 200)
	if err := db.Model(&entity.Project{}).Where("id = ?", project.ID).Update("status", entity.ResourceDisabled).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, asManager("GET", candidatePath, nil), 409)
}
