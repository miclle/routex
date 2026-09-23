package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestPriceImportRejectsUnrecognizedJSONIntent(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.POST("/preview", ctrl.PreviewPriceImport)
	router.POST("/commit", ctrl.CommitPriceImport)
	for _, test := range []struct{ path, body string }{
		{"/preview", `{"csv":"header","source":"repository"}`},
		{"/preview", `{"csv":"header"} {"csv":"other"}`},
		{"/commit", `{"csv":"header","etag":"etag","preview_digest":"digest","items":[]}`},
		{"/commit", `{"csv":"header","etag":"etag","preview_digest":"digest","follow_repository":true}`},
	} {
		req := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 {
			t.Fatalf("unsupported intent accepted: %s returned %d", test.path, response.Code)
		}
	}
}
