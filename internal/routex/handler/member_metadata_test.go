package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func TestMemberMetadataHandlerRejectsUnsafeBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/members/:user_id/metadata", ctrl.GetMemberMetadata)
	router.PUT("/members/:user_id/metadata", ctrl.SetMemberMetadata)
	valid := `"` + strings.Repeat("a", 64) + `"`
	for _, test := range []struct{ method, path, body, etag string }{
		{"GET", "/members/subject/metadata?email=true", "", ""},
		{"GET", "/members/target%20/metadata", "", ""},
		{"GET", "/members/" + strings.Repeat("x", 31) + "/metadata", "", ""},
		{"PUT", "/members/subject/metadata", `{"name":"A","reason":"R"}`, ""},
		{"PUT", "/members/subject/metadata", `{"name":"A","reason":"R"}`, `W/` + valid},
		{"PUT", "/members/subject/metadata", `{"name":"A","reason":"R"}`, `"` + strings.Repeat("A", 64) + `"`},
		{"PUT", "/members/subject/metadata?user_id=foreign", `{"name":"A","reason":"R"}`, valid},
		{"PUT", "/members/subject/metadata", `{"name":"A","name":"B","reason":"R"}`, valid},
		{"PUT", "/members/subject/metadata", `{"name":"A","reason":"R","role":"admin"}`, valid},
		{"PUT", "/members/subject/metadata", `{"name":"A","reason":null}`, valid},
		{"PUT", "/members/subject/metadata", `{"name":"\ud800","reason":"R"}`, valid},
		{"PUT", "/members/subject/metadata", strings.Repeat(" ", 64*1024+1), valid},
	} {
		req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
		req.Header.Set("If-Match", test.etag)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Code != 400 || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(test, response.Code, response.Body.String())
		}
	}
}
func TestMemberMetadataStrongETagAndExactBody(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.PUT("/members/:user_id/metadata", func(c *fox.Context) error {
		if err := memberMetadataRequest(c); err != nil {
			return err
		}
		_, err := memberMetadataETag(c)
		if err != nil {
			return err
		}
		var input service.MemberMetadataInput
		if err := decodeStrictRequest(c, &input); err != nil {
			return err
		}
		c.Status(204)
		return nil
	})
	for _, headers := range [][]string{{`"` + strings.Repeat("a", 64) + `"`}, {`"` + strings.Repeat("a", 64) + `"`, `"` + strings.Repeat("b", 64) + `"`}, {strings.Repeat("a", 64)}, {`"` + strings.Repeat("a", 64) + `", "` + strings.Repeat("b", 64) + `"`}} {
		req := httptest.NewRequest("PUT", "/members/subject/metadata", strings.NewReader(`{"name":"😀","reason":"Reviewed"}`))
		for _, h := range headers {
			req.Header.Add("If-Match", h)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		expected := 400
		if len(headers) == 1 && len(headers[0]) == 66 {
			expected = 204
		}
		if out.Code != expected {
			t.Fatal(headers, out.Code)
		}
	}
}

func TestMemberMetadataHeadersPrecedeAuthorityDenial(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/metadata", memberMetadataResponseHeaders, func(c *fox.Context) error { return apperrors.ErrUnauthorized })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/metadata", nil))
	if response.Code != 401 || response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal(response.Code, response.Header())
	}
}
