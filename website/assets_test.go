package website

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestUnknownAPIRoutesReturnJSON404(t *testing.T) {
	router := fox.New()
	EmbedAssets(router)
	for _, method := range []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, "/api/v1/missing", nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body = %s", response.Code, response.Body.String())
			}
			if !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("Content-Type = %q, want JSON", response.Header().Get("Content-Type"))
			}
			if method != http.MethodHead && !json.Valid(response.Body.Bytes()) {
				t.Fatalf("invalid JSON error response: %s", response.Body.String())
			}
		})
	}
}
