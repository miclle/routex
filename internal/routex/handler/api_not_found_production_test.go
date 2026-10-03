//go:build !development

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestCombinedProductionSPAFallbackRemainsHTML(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, path := range []string{"/", "/projects/prj_missing/settings"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+path, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(method, path, nil))
				if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") || response.Header().Get("Cache-Control") != "no-cache" {
					t.Fatalf("SPA status=%d content_type=%q cache_control=%q", response.Code, response.Header().Get("Content-Type"), response.Header().Get("Cache-Control"))
				}
				if method == http.MethodGet && !strings.Contains(strings.ToLower(response.Body.String()), "<html") {
					t.Fatal("SPA fallback did not serve the embedded index")
				}
			})
		}
	}
}
