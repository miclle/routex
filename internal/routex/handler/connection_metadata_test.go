package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func TestConnectionMetadataHandlerRejectsUnsafeBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/connections/:connection_id/metadata", ctrl.GetConnectionMetadata)
	router.PUT("/connections/:connection_id/metadata", ctrl.WriteConnectionMetadata)
	valid := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	for _, test := range []struct{ method, path, body, etag string }{
		{"GET", "/connections/con_target/metadata?provider_id=foreign", "", ""},
		{"GET", "/connections/CON_TARGET/metadata", "", ""},
		{"GET", "/connections/con_target%20/metadata", "", ""},
		{"GET", "/connections/con_" + strings.Repeat("x", 27) + "/metadata", "", ""},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R"}`, ""},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R"}`, "W/" + valid},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R"}`, strings.ToUpper(valid)},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R"}`, `"` + strings.Repeat("a", 64) + `"`},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R","name":"B"}`, valid},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":"R","egress_id":null}`, valid},
		{"PUT", "/connections/con_target/metadata", `{"name":"A","reason":null}`, valid},
		{"PUT", "/connections/con_target/metadata", `{"name":"\ud800","reason":"R"}`, valid},
		{"PUT", "/connections/con_target/metadata", strings.Repeat(" ", 64*1024+1), valid},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("If-Match", test.etag)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(test, out.Code, out.Body.String())
		}
	}
}
func TestConnectionMetadataHandlerStrongReviewAndPrivateDenials(t *testing.T) {
	valid := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/connections/:connection_id/metadata", func(c *fox.Context) error {
		if err := connectionMetadataRequest(c); err != nil {
			return err
		}
		if _, err := connectionMetadataETag(c); err != nil {
			return err
		}
		var input service.ConnectionMetadataInput
		if err := decodeStrictRequest(c, &input); err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	for _, headers := range [][]string{{valid}, {valid, valid}, {strings.Trim(valid, `"`)}, {valid + ", " + valid}, {`"` + strings.Repeat("a", 64) + "/" + strings.Repeat("b", 64) + `"`}} {
		req := httptest.NewRequest("PUT", "/connections/con_target/metadata", strings.NewReader(`{"name":"😀","reason":"Reviewed"}`))
		for _, value := range headers {
			req.Header.Add("If-Match", value)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		want := 400
		if len(headers) == 1 && headers[0] == valid {
			want = 204
		}
		if out.Code != want {
			t.Fatal(headers, out.Code)
		}
	}
	for _, denial := range []error{apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrBadRequest} {
		router := fox.New()
		router.RenderErrorFunc = renderAPIError
		router.PUT("/metadata", memberMetadataResponseHeaders, func(*fox.Context) error { return denial })
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest("PUT", "/metadata", nil))
		if out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(out.Code, out.Header())
		}
	}
}
