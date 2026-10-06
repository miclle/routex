package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestMemberOverviewRolesStrictSelfQuery(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/overview/roles", func(c *fox.Context) error {
		_, err := memberOverviewRolesFilter(c)
		if err == nil {
			c.Status(http.StatusNoContent)
		}
		return err
	})
	for _, query := range []string{"limit=0", "limit=51", "limit=01", "limit=1&limit=2", "cursor=", "user_id=usr_other", "role_id=rol_other", "q=name", "team_id=tea_other", "limit=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/overview/roles?"+query, nil))
		if response.Code != 400 {
			t.Fatal("ambiguous/targeted query accepted", query, response.Code)
		}
	}
	for _, query := range []string{"", "limit=1", "limit=50&cursor=opaque"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/overview/roles?"+query, nil))
		if response.Code != 204 {
			t.Fatal(query, response.Code)
		}
	}
}
func TestMemberOverviewRolesRouteNoStoreBeforeAuthentication(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, path := range []string{"/api/v1/overview/roles", "/api/v1/overview/roles?user_id=usr_other", "/api/v1/overview/roles?limit=bad"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != 401 || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unauthorized response became cacheable", path, response.Code, response.Header())
		}
	}
	direct := fox.New()
	direct.RenderErrorFunc = renderAPIError
	direct.GET("/overview/roles", New(nil).MemberOverviewRoles)
	response := httptest.NewRecorder()
	direct.ServeHTTP(response, httptest.NewRequest("GET", "/overview/roles?user_id=usr_other", nil))
	if response.Code != 400 || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid self query leaked", response.Code)
	}
}
