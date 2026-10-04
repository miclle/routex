package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberTeamsParserRejectsDirectoryExpansionAndAmbiguousPages(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	var got service.MemberTeamsFilter
	router.GET("/teams", func(c *fox.Context) error {
		var err error
		got, err = memberTeamsListFilter(c)
		if err == nil {
			c.Status(http.StatusNoContent)
		}
		return err
	})
	for _, query := range []string{"limit=0", "limit=-1", "limit=51", "limit=01", "limit=1&limit=2", "limit=", "cursor=", "cursor=one&cursor=two", "user_id=foreign", "team_id=foreign", "q=name", "role=owner", "status=active", "limit=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/teams?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("scope expansion or ambiguous page accepted", query, response.Code)
		}
	}
	for _, test := range []struct {
		query  string
		limit  int
		cursor string
	}{{"", 20, ""}, {"limit=1", 1, ""}, {"limit=50&cursor=canonical", 50, "canonical"}} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/teams?"+test.query, nil))
		if response.Code != http.StatusNoContent || got.Limit != test.limit || got.Cursor != test.cursor {
			t.Fatal(test, got, response.Code)
		}
	}
}
func TestMemberTeamsPrivateHandlerRejectsExpansionWithoutIdentityOrService(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/teams", New(nil).ListMemberTeams)
	for _, query := range []string{"user_id=foreign", "team_id=foreign", "limit=51", "limit=", "cursor="} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/members/subject/teams?"+query, nil))
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(query, response.Code, response.Header())
		}
	}
}
