package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberOverviewAccountsQueryIsSelfOnlyAndBounded(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	var captured service.MemberOverviewAccountsFilter
	router.GET("/overview/accounts", func(c *fox.Context) error {
		value, err := memberOverviewAccountsFilter(c)
		if err != nil {
			return err
		}
		captured = value
		c.Status(http.StatusNoContent)
		return nil
	})
	for _, query := range []string{"limit=0", "limit=-1", "limit=51", "limit=01", "limit=1&limit=2", "limit=", "cursor=", "cursor=a&cursor=b", "user_id=usr_other", "team_id=tem_other", "project=prj_other", "key=private", "q=anything", "limit=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/overview/accounts?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("expanded or ambiguous Overview query accepted", query, response.Code)
		}
	}
	for _, test := range []struct {
		query string
		limit int
	}{{"", 10}, {"limit=1", 1}, {"limit=50&cursor=actor-bound-cursor", 50}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/overview/accounts?"+test.query, nil))
		if response.Code != http.StatusNoContent || captured.Limit != test.limit {
			t.Fatal("bounded query changed", test.query, response.Code, captured)
		}
	}
}

func TestMemberOverviewAccountsFailureIsNotCacheable(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/overview/accounts", New(nil).MemberOverviewAccounts)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/overview/accounts?user_id=usr_private", nil))
	if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("invalid scope reached Session/service or private result became cacheable", response.Code, response.Header())
	}
}
