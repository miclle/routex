package handler

import (
	"github.com/fox-gonic/fox"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicNameSuggestionsRejectNamespaceExpansion(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.GET("/names", ctrl.ListModelCreationPublicNames)
	for _, query := range []string{"q=x&q=y", "names=gpt-5.2", "actor=usr_other", "limit=50", "cursor=x", "q=%zz", "q=%ff", "q=x%0A", "q=" + strings.Repeat("x", 129)} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/names?"+query, nil))
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal(query, response.Code, response.Header())
		}
	}
}
