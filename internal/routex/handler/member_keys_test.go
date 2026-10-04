package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberKeyListRejectsScopeExpansionAndAmbiguousPagination(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	var got service.MemberKeyFilter
	router.GET("/keys", func(c *fox.Context) error {
		var err error
		got, err = memberKeyListFilter(c)
		if err == nil {
			c.Status(http.StatusNoContent)
		}
		return err
	})
	for _, query := range []string{"limit=0", "limit=-1", "limit=101", "limit=01", "limit=1&limit=2", "limit=", "cursor=", "cursor=one&cursor=two", "user_id=foreign", "project_id=foreign", "key_id=foreign", "secret=true", "q=name", "limit=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/keys?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("expanded or ambiguous list query reached service", query, response.Code)
		}
	}
	for _, test := range []struct {
		query  string
		limit  int
		cursor string
	}{{"", 40, ""}, {"limit=1", 1, ""}, {"limit=100&cursor=canonical", 100, "canonical"}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/keys?"+test.query, nil))
		if response.Code != http.StatusNoContent || got.Limit != test.limit || got.Cursor != test.cursor {
			t.Fatal("bounded list changed", test, got, response.Code)
		}
	}
}

func TestMemberKeyPrivateHandlersRejectUnsafeInputBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/keys", ctrl.ListMemberKeys)
	router.GET("/members/:user_id/keys/:key_id", ctrl.GetMemberKey)
	router.POST("/members/:user_id/keys/:key_id/disable", ctrl.DisableMemberKey)
	for _, test := range []struct{ method, path, body, etag string }{
		{"GET", "/members/subject/keys?user_id=foreign", "", ""},
		{"GET", "/members/subject/keys/key?project_id=foreign", "", ""},
		{"POST", "/members/subject/keys/key/disable?project_id=foreign", `{"reason":"test"}`, `"` + strings.Repeat("a", 64) + `"`},
		{"POST", "/members/subject/keys/key/disable", `{"reason":"test","status":"active"}`, `"` + strings.Repeat("a", 64) + `"`},
		{"POST", "/members/subject/keys/key/disable", `{"reason":"test"} {}`, `"` + strings.Repeat("a", 64) + `"`},
		{"POST", "/members/subject/keys/key/disable", `{"reason":"test"}`, ""},
		{"POST", "/members/subject/keys/key/disable", `{"reason":"test"}`, `W/"` + strings.Repeat("a", 64) + `"`},
	} {
		request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("If-Match", test.etag)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unsafe request reached identity/service or became cacheable", test, response.Code, response.Header())
		}
	}
}
