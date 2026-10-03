package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestTeamModelRequestCandidateQueryCannotBecomeDirectory(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/teams", ctrl.ListTeamModelRequestTeams)
	router.GET("/candidates", ctrl.ListTeamModelRequestCandidates)
	router.GET("/candidate", ctrl.GetTeamModelRequestCandidate)
	router.GET("/workspace", ctrl.TeamModelRequestWorkspace)
	for _, path := range []string{"/teams", "/candidates"} {
		for _, query := range []string{"q=", "q=one&q=two", "limit=0", "limit=51", "limit=01", "limit=%2B1", "limit=1&limit=2", "user_id=private", "provider_id=private", "team_id=unreviewed", "price=true"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatal("minimal selector expanded/ambiguous query reached identity", path, query, response.Code)
			}
		}
	}
	for _, path := range []string{"/candidate", "/workspace"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?user_id=private", nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("detail/workspace queried unrelated directory", path, response.Code)
		}
	}
}
