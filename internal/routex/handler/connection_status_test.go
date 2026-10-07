package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/service"
)

func TestConnectionStatusHandlerStrongReviewAndStrictBoolean(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/connections/:connection_id/status", ctrl.WriteConnectionStatus)
	token := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	for _, bad := range []struct{ body, header, path string }{
		{`{"enabled":false,"reason":"R"}`, "", "/connections/con_target/status"},
		{`{"enabled":false,"reason":"R"}`, "W/" + token, "/connections/con_target/status"},
		{`{"enabled":false,"reason":"R"}`, token + ", " + token, "/connections/con_target/status"},
		{`{"enabled":null,"reason":"R"}`, token, "/connections/con_target/status"},
		{`{"enabled":"false","reason":"R"}`, token, "/connections/con_target/status"},
		{`{"enabled":false,"reason":"R","enabled":true}`, token, "/connections/con_target/status"},
		{`{"enabled":false,"reason":"R","name":"N"}`, token, "/connections/con_target/status"},
		{strings.Repeat(" ", 64*1024+1), token, "/connections/con_target/status"},
		{`{"enabled":false,"reason":"R"}`, token, "/connections/con_bad%20/status"},
	} {
		req := httptest.NewRequest("PUT", bad.path, strings.NewReader(bad.body))
		req.Header.Set("If-Match", bad.header)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(out.Code, out.Body.String())
		}
	}
	router = fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/status", memberMetadataResponseHeaders, func(c *fox.Context) error {
		var input service.ConnectionStatusInput
		if err := decodeStrictRequest(c, &input); err != nil {
			return err
		}
		if input.Enabled {
			t.Fatal("false dropped")
		}
		c.Status(204)
		return nil
	})
	out := httptest.NewRecorder()
	router.ServeHTTP(out, httptest.NewRequest("PUT", "/status", strings.NewReader(`{"enabled":false,"reason":"Reviewed"}`)))
	if out.Code != 204 {
		t.Fatal(out.Code, out.Body.String())
	}
}
