package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestRoleDefinitionHandlerStrictBoundary(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	ctrl := New(nil)
	router.GET("/roles/:role_id", ctrl.GetRoleDefinition)
	router.PUT("/roles/:role_id", ctrl.SetReviewedRoleDefinition)
	proof := strings.Repeat("a", 64)
	valid := `{"name":"Reviewed role","permissions":[],"identity_etag":"` + proof + `","reason":"Reviewed replacement"}`
	quoted := `"` + proof + `"`
	cases := []struct {
		name, method, path, body string
		headers                  []string
	}{
		{"get_query", "GET", "/roles/rol_recorded?limit=1", "", nil},
		{"get_empty_query_value", "GET", "/roles/rol_recorded?cursor=", "", nil},
		{"get_space", "GET", "/roles/rol_recorded%20", "", nil},
		{"get_long_id", "GET", "/roles/rol_" + strings.Repeat("x", 27), "", nil},
		{"get_foreign_prefix", "GET", "/roles/usr_recorded", "", nil},
		{"put_query", "PUT", "/roles/rol_recorded?reason=other", valid, []string{quoted}},
		{"absent_validator", "PUT", "/roles/rol_recorded", valid, nil},
		{"weak_validator", "PUT", "/roles/rol_recorded", valid, []string{"W/" + quoted}},
		{"unquoted_validator", "PUT", "/roles/rol_recorded", valid, []string{proof}},
		{"uppercase_validator", "PUT", "/roles/rol_recorded", valid, []string{`"` + strings.Repeat("A", 64) + `"`}},
		{"duplicate_validator", "PUT", "/roles/rol_recorded", valid, []string{quoted, quoted}},
		{"multiple_validator", "PUT", "/roles/rol_recorded", valid, []string{quoted + ", " + quoted}},
		{"missing_reason", "PUT", "/roles/rol_recorded", `{"name":"R","permissions":[],"identity_etag":"` + proof + `"}`, []string{quoted}},
		{"null_permissions", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"permissions":[]`, `"permissions":null`, 1), []string{quoted}},
		{"duplicate_name", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"name":"Reviewed role"`, `"name":"Reviewed role","name":"Other"`, 1), []string{quoted}},
		{"escaped_duplicate", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"name":"Reviewed role"`, `"name":"Reviewed role","\u006eame":"Other"`, 1), []string{quoted}},
		{"case_field", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"name":`, `"Name":`, 1), []string{quoted}},
		{"foreign_field", "PUT", "/roles/rol_recorded", strings.Replace(valid, `{"name":`, `{"role_id":"rol_foreign","name":`, 1), []string{quoted}},
		{"duplicate_permissions", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"permissions":[]`, `"permissions":["prices.read","prices.read"]`, 1), []string{quoted}},
		{"unsorted_permissions", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"permissions":[]`, `"permissions":["providers.read","prices.read"]`, 1), []string{quoted}},
		{"empty_name", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed role"`, `""`, 1), []string{quoted}},
		{"name_control", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed role"`, `"R\n"`, 1), []string{quoted}},
		{"name_bounds", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed role"`, `"`+strings.Repeat("界", 101)+`"`, 1), []string{quoted}},
		{"reason_bounds", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed replacement"`, `"`+strings.Repeat("界", 342)+`"`, 1), []string{quoted}},
		{"reason_trim", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed replacement"`, `" Reviewed replacement"`, 1), []string{quoted}},
		{"identity_null", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"identity_etag":"`+proof+`"`, `"identity_etag":null`, 1), []string{quoted}},
		{"identity_uppercase", "PUT", "/roles/rol_recorded", strings.Replace(valid, proof, strings.Repeat("A", 64), 1), []string{quoted}},
		{"surrogate", "PUT", "/roles/rol_recorded", strings.Replace(valid, `"Reviewed role"`, `"\ud800"`, 1), []string{quoted}},
		{"invalid_utf8", "PUT", "/roles/rol_recorded", strings.Replace(valid, "Reviewed role", string([]byte{0xff}), 1), []string{quoted}},
		{"oversized_body", "PUT", "/roles/rol_recorded", strings.Repeat(" ", 64*1024+1), []string{quoted}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			for _, header := range test.headers {
				request.Header.Add("If-Match", header)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("invalid request crossed strict boundary", response.Code)
			}
			if response.Header().Get("ETag") != "" {
				t.Fatal("rejected request returned a successful review header")
			}
		})
	}
}

func TestRoleDefinitionRegisteredDenialsArePrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, method := range []string{"GET", "PUT"} {
		t.Run(method, func(t *testing.T) {
			request := httptest.NewRequest(method, "/api/v1/admin/roles/rol_recorded", strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assertAPIErrorResponse(t, response, method, ErrorResponse{Code: 401, Message: "unauthorized"})
			if response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("ETag") != "" {
				t.Fatal("registered authentication denial exposed review metadata")
			}
		})
	}
}
