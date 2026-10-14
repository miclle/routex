package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestRuntimeArchiveCleanupStrictReviewBody(t *testing.T) {
	ids := `["rcv_00000000000000000000000001"]`
	valid := `{"archive_ids":` + ids + `}`
	input, err := decodeRuntimeArchiveCleanupInput([]byte(valid), false)
	if err != nil || len(input.ArchiveIDs) != 1 {
		t.Fatal(input, err)
	}
	apply := `{"archive_ids":` + ids + `,"request_id":"cb59f5d1-7269-49f1-b46f-5f7b7df98649","reason":"Remove reviewed retained storage"}`
	if _, err = decodeRuntimeArchiveCleanupInput([]byte(apply), true); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		apply, `null`, `[]`, `{}`, `{"archive_ids":null}`, `{"archive_ids":[]}`,
		`{"archive_ids":[null]}`, `{"archive_ids":[1]}`, `{"archive_ids":` + ids + `,"archive_ids":` + ids + `}`,
		valid + `{}`, `{"archive_ids":` + ids + `,"automatic":true}`, strings.Repeat(" ", 8193),
	} {
		if _, err := decodeRuntimeArchiveCleanupInput([]byte(raw), false); err == nil {
			t.Fatal("unsafe preview body admitted", raw)
		}
	}
	for _, raw := range []string{valid, strings.Replace(apply, `"request_id":"cb59f5d1-7269-49f1-b46f-5f7b7df98649"`, `"request_id":null`, 1), strings.Replace(apply, `"reason":"Remove reviewed retained storage"`, `"reason":[]`, 1)} {
		if _, err := decodeRuntimeArchiveCleanupInput([]byte(raw), true); err == nil {
			t.Fatal("unsafe apply body admitted", raw)
		}
	}
	for _, escaped := range []string{`"\ud800"`, `"\udfff"`, `"\ud800X"`, `"\udc00\ud800"`} {
		malformed := strings.Replace(apply, `"Remove reviewed retained storage"`, escaped, 1)
		if _, err := decodeRuntimeArchiveCleanupInput([]byte(malformed), true); err == nil {
			t.Fatal("lossy Unicode intent admitted", escaped)
		}
	}
	paired := strings.Replace(apply, `"Remove reviewed retained storage"`, `"Reviewed \ud83d\ude00"`, 1)
	if value, err := decodeRuntimeArchiveCleanupInput([]byte(paired), true); err != nil || value.Reason != "Reviewed 😀" {
		t.Fatal("valid Unicode intent changed", value, err)
	}
	raw, _ := json.Marshal(map[string]any{"archive_ids": make([]string, 21)})
	if _, err := decodeRuntimeArchiveCleanupInput(raw, false); err == nil {
		t.Fatal("unbounded selection admitted")
	}
}

func TestRuntimeArchiveCleanupRegisteredRoutesRejectAnonymousPrivately(t *testing.T) {
	router := fox.New()
	New(nil).RegisterRoutes(router)
	for _, path := range []string{"/api/v1/admin/runtime/archives/cleanup/preview", "/api/v1/admin/runtime/archives/cleanup"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"archive_ids":["rcv_00000000000000000000000001"]}`))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 401 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("ETag") != "" {
				t.Fatal("registered cleanup route bypass or unsafe denial", rec.Code)
			}
		})
	}
}

func TestRuntimeArchiveCleanupInvalidWireStopsBeforeService(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	ctrl := New(nil)
	router.POST("/preview", runtimePublicationResponseHeaders, jsonManagementRequest, ctrl.PreviewRuntimeArchiveCleanup)
	router.POST("/cleanup", runtimePublicationResponseHeaders, jsonManagementRequest, ctrl.CleanupRuntimeArchives)
	for _, tc := range []struct{ path, body, etag string }{
		{"/preview", `{}`, ""},
		{"/preview?reason=private-intent", `{"archive_ids":["rcv_00000000000000000000000001"]}`, ""},
		{"/preview?", `{"archive_ids":["rcv_00000000000000000000000001"]}`, ""},
		{"/cleanup", `{"archive_ids":["rcv_00000000000000000000000001"],"request_id":"cb59f5d1-7269-49f1-b46f-5f7b7df98649","reason":"Reviewed storage cleanup"}`, ""},
		{"/cleanup", `{"archive_ids":["rcv_00000000000000000000000001"],"request_id":"cb59f5d1-7269-49f1-b46f-5f7b7df98649","reason":"Reviewed storage cleanup"}`, `W/"` + strings.Repeat("a", 64) + `"`},
	} {
		t.Run(tc.path+tc.etag, func(t *testing.T) {
			req := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.etag != "" {
				req.Header.Set("If-Match", tc.etag)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != 400 || rec.Header().Get("Cache-Control") != "private, no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" || strings.Contains(rec.Body.String(), "private-intent") {
				t.Fatal("invalid cleanup crossed service boundary", rec.Code)
			}
		})
	}
}
