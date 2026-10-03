package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestModelAccessCandidateQueryRejectsDirectoryExpansionBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/candidates", ctrl.ListModelAccessCandidates)
	router.GET("/candidate", ctrl.GetModelAccessCandidate)
	for _, query := range []string{"limit=0", "limit=51", "limit=-1", "limit=01", "limit=%2B1", "limit=1&limit=2", "limit=", "q=", "q=one&q=two", "user_id=private", "provider_id=private", "connection_id=private", "price=true", "q=bad%zz"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/candidates?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatal("expanded/ambiguous candidate query reached service", query, response.Code)
		}
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/candidate?user_id=private", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatal("detail query expanded candidate scope", response.Code)
	}
}
