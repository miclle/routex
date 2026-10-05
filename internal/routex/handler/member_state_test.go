package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestMemberStateHandlerStrictBoundary(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/state", ctrl.GetMemberState)
	router.PATCH("/members/:user_id", ctrl.SetReviewedMemberState)
	valid := `"` + strings.Repeat("a", 64) + `"`
	cases := []struct{ method, path, body, etag string }{
		{"GET", "/members/subject/state?user_id=foreign", "", ""},
		{"GET", "/members/subject%20/state", "", ""},
		{"GET", "/members/" + strings.Repeat("x", 31) + "/state", "", ""},
		{"PATCH", "/members/subject", `{"disabled":true,"reason":"R"}`, ""},
		{"PATCH", "/members/subject", `{"disabled":true,"reason":"R"}`, "W/" + valid},
		{"PATCH", "/members/subject", `{"disabled":true,"reason":"R"}`, `"` + strings.Repeat("A", 64) + `"`},
		{"PATCH", "/members/subject?role=admin", `{"disabled":true,"reason":"R"}`, valid},
		{"PATCH", "/members/subject", `{"disabled":true}`, valid},
		{"PATCH", "/members/subject", `{"disabled":true,"role":"admin","reason":"R"}`, valid},
		{"PATCH", "/members/subject", `{"disabled":null,"reason":"R"}`, valid},
		{"PATCH", "/members/subject", `{"disabled":true,"disabled":false,"reason":"R"}`, valid},
		{"PATCH", "/members/subject", `{"role":"member","reason":"\ud800"}`, valid},
		{"PATCH", "/members/subject", strings.Repeat(" ", 64*1024+1), valid},
	}
	for _, test := range cases {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("If-Match", test.etag)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, request)
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(test.method, test.path, out.Code)
		}
	}
}

// Use the product registry: a leaf-only router would miss group middleware
// rejecting the request before the private-response header is installed.
func TestMemberStateRegisteredUnauthenticatedResponsesArePrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, test := range []struct{ method, path, body string }{
		{"GET", "/api/v1/admin/members/usr_target/state", ""},
		{"PATCH", "/api/v1/admin/members/usr_target", `{"disabled":true,"reason":"Reviewed account access"}`},
	} {
		t.Run(test.method, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("If-Match", `"`+strings.Repeat("a", 64)+`"`)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assertAPIErrorResponse(t, response, test.method, ErrorResponse{Code: 401, Message: "unauthorized"})
			if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
				t.Fatalf("registered %s unauthenticated response Cache-Control = %q; want private, no-store", test.method, got)
			}
			if response.Header().Get("ETag") != "" {
				t.Fatal("unauthenticated denial returned a private review ETag")
			}
		})
	}
}
