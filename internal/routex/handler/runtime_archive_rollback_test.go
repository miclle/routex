package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestRuntimeArchiveRollbackAnonymousRoutesStayPrivate(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, pair := range [][2]string{{"GET", "/api/v1/admin/runtime/archives/rcv_00000000000000000000000001/rollback"}, {"POST", "/api/v1/admin/runtime/archives/rcv_00000000000000000000000001/rollback"}, {"GET", "/api/v1/admin/runtime/archive-rollbacks/605670ca-5a6a-4f98-8021-000000000001"}} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(pair[0], pair[1], nil))
		if rec.Code != 401 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
			t.Fatal("anonymous routing rollback admitted or cached", rec.Code)
		}
	}
}
func TestRuntimeArchiveRollbackStrictIntentAndReviewBoundary(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/archive/:archive_id", runtimePublicationResponseHeaders, New(nil).PreviewRuntimeArchiveRollback)
	router.POST("/archive/:archive_id", runtimePublicationResponseHeaders, jsonManagementRequest, New(nil).RollbackRuntimeArchive)
	for _, suffix := range []string{"?", "?plaintext=secret-sentinel", "?readiness=true"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/archive/rcv_00000000000000000000000001"+suffix, nil))
		if rec.Code != 400 || strings.Contains(rec.Body.String(), "secret-sentinel") {
			t.Fatal("non-allowlisted preview query admitted", rec.Code)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"request_id":"x","request_id":"y","reason":"review"}`, `{"request_id":"x","reason":null}`, `{"request_id":"x","reason":"review","transport":"private"}`, `{"request_id":"x","reason":"review"} {}`, `{"request_id":["x"],"reason":"review"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/archive/rcv_00000000000000000000000001", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", `"`+strings.Repeat("a", 64)+`"`)
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatal("malformed original intent crossed service boundary", rec.Code)
		}
	}
	for _, etag := range []string{"", strings.Repeat("a", 64), `W/"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("a", 64) + `", "` + strings.Repeat("b", 64) + `"`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/archive/rcv_00000000000000000000000001", strings.NewReader(`{"request_id":"605670ca-5a6a-4f98-8021-000000000001","reason":"Reviewed exact routing"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("If-Match", etag)
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatal("unreviewed routing mutation crossed boundary", rec.Code)
		}
	}
}
