package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestRuntimeArchiveValidationAnonymousIsPrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/runtime/archives/rcv_00000000000000000000000001/validation", nil))
	if rec.Code != 401 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
		t.Fatal("anonymous integrity route bypass or unsafe response", rec.Code)
	}
}

func TestRuntimeArchiveValidationRejectsQueryBeforeService(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/archive/:archive_id", runtimePublicationResponseHeaders, New(nil).ValidateRuntimeArchive)
	for _, suffix := range []string{"?", "?source=private-sentinel", "?limit=1", "?readiness=true"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/archive/rcv_00000000000000000000000001"+suffix, nil))
		if rec.Code != 400 || strings.Contains(rec.Body.String(), "private-sentinel") || rec.Header().Get("ETag") != "" {
			t.Fatal("query crossed validation boundary", rec.Code)
		}
	}
}
