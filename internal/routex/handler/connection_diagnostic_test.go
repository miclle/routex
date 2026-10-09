package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestConnectionDiagnosticHandlerRejectsUnsafeRequestBeforeService(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/connections/:connection_id/test", New(nil).TestConnection)
	valid := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	for _, test := range []struct{ path, body, etag string }{
		{"/connections/con_target/test?credential_id=crd_other", `{"credential_id":"crd_target"}`, valid},
		{"/connections/CON_TARGET/test", `{"credential_id":"crd_target"}`, valid},
		{"/connections/con_target%20/test", `{"credential_id":"crd_target"}`, valid},
		{"/connections/con_target/test", `{"credential_id":"crd_target"}`, ""},
		{"/connections/con_target/test", `{"credential_id":"crd_target"}`, "W/" + valid},
		{"/connections/con_target/test", `{"credential_id":"crd_target"}`, strings.ToUpper(valid)},
		{"/connections/con_target/test", `{"credential_id":null}`, valid},
		{"/connections/con_target/test", `{"credential_id":"crd_target","secret":"forbidden"}`, valid},
		{"/connections/con_target/test", `{"credential_id":"crd_target","credential_id":"crd_other"}`, valid},
		{"/connections/con_target/test", `{"credential_id":"crd_\ud800"}`, valid},
		{"/connections/con_target/test", strings.Repeat(" ", 4*1024+1), valid},
	} {
		req := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		req.Header.Set("If-Match", test.etag)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != 400 || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(test.path, out.Code, out.Body.String())
		}
	}
}

func TestConnectionDiagnosticHandlerAcceptsOnlyOneStrongReview(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.POST("/connections/:connection_id/test", func(c *fox.Context) error {
		if err := connectionMetadataRequest(c); err != nil {
			return err
		}
		if _, err := connectionMetadataETag(c); err != nil {
			return err
		}
		var input service.ConnectionDiagnosticInput
		if err := decodeStrictRequest(c, &input); err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	valid := `"` + strings.Repeat("a", 64) + "." + strings.Repeat("b", 64) + `"`
	for _, headers := range [][]string{{valid}, {valid, valid}, {strings.Trim(valid, `"`)}, {valid + ", " + valid}} {
		req := httptest.NewRequest("POST", "/connections/con_target/test", strings.NewReader(`{"credential_id":"crd_target"}`))
		for _, value := range headers {
			req.Header.Add("If-Match", value)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		want := 400
		if len(headers) == 1 && headers[0] == valid {
			want = 204
		}
		if out.Code != want || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(headers, out.Code, out.Header())
		}
	}
}
