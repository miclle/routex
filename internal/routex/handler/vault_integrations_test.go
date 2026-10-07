package handler

import (
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVaultManagementIntentHeaderBodyPrivacy(t *testing.T) {
	raw := `{"request_id":"11111111-1111-4111-8111-111111111111","reason":"Reviewed"}`
	etag := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/stage", func(c *fox.Context) error {
		if e := vaultRequest(c); e != nil {
			return e
		}
		if _, e := vaultHeader(c); e != nil {
			return e
		}
		var v service.VaultStageInput
		if e := vaultBody(c, &v); e != nil {
			return e
		}
		c.JSON(200, map[string]bool{"accepted": true})
		return nil
	})
	for _, tc := range []struct {
		name, header, body, path string
		want                     int
	}{{"valid", etag, raw, "/stage", 200}, {"missing", "", raw, "/stage", 428}, {"weak", "W/" + etag, raw, "/stage", 400}, {"unquoted", strings.Trim(etag, `"`), raw, "/stage", 400}, {"override", etag, raw[:len(raw)-1] + `,"token":"private"}`, "/stage", 400}, {"duplicate", etag, strings.Replace(raw, `"reason":"Reviewed"`, `"reason":"Reviewed","reason":"other"`, 1), "/stage", 400}, {"query", etag, raw, "/stage?token=private", 400}, {"overflow", etag, strings.Repeat(" ", 64<<10) + raw, "/stage", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
			if tc.header != "" {
				req.Header.Set("If-Match", tc.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want || rec.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(rec.Body.String(), "private") {
				t.Fatal("unsafe management boundary", rec.Code)
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, "/stage", strings.NewReader(raw))
	req.Header.Add("If-Match", etag)
	req.Header.Add("If-Match", etag)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatal("duplicate reviews accepted")
	}
}
