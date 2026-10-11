package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fox-gonic/fox"
)

func TestModelLifecycleStrictBodyBoundary(t *testing.T) {
	valid := `{"status":"disabled","reason":"maintenance"}`
	if in, err := decodeModelLifecycleInput(strings.NewReader(valid)); err != nil || in.Status != "disabled" {
		t.Fatal(in, err)
	}
	if in, err := decodeModelLifecycleInput(strings.NewReader(`{"status":"archived","reason":"retire"}`)); err != nil || in.Status != "archived" {
		t.Fatal(in, err)
	}
	for _, raw := range []string{"", valid + ` {}`, `{"status":"disabled","reason":null}`, `{"status":"unknown","reason":"maintenance"}`, `{"status":"active","reason":"x","reason":"y"}`, `{"status":"active","reason":"x","extra":true}`, strings.Repeat(" ", modelLifecycleBodyLimit) + valid} {
		if _, err := decodeModelLifecycleInput(strings.NewReader(raw)); err == nil {
			t.Fatal("hostile body accepted", raw)
		}
	}
	for _, r := range []io.Reader{aliasFailingReader{}, strings.NewReader("")} {
		if _, err := decodeModelLifecycleInput(r); err == nil {
			t.Fatal("invalid reader accepted")
		}
	}
}
func TestModelLifecycleStrongHeaderBoundary(t *testing.T) {
	token := strings.Repeat("a", 64) + "." + strings.Repeat("b", 64)
	for _, tc := range []struct {
		name    string
		headers []string
		ok      bool
	}{
		{"strong", []string{`"` + token + `"`}, true},
		{"missing", nil, false}, {"bare", []string{token}, false}, {"weak", []string{`W/"` + token + `"`}, false}, {"duplicate", []string{`"` + token + `"`, `"` + token + `"`}, false}, {"uppercase", []string{`"` + strings.ToUpper(token) + `"`}, false}, {"list", []string{`"` + token + `", "other"`}, false}, {"old64", []string{`"` + strings.Repeat("a", 64) + `"`}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := fox.New()
			router.GET("/", func(c *fox.Context) error {
				got, err := modelLifecycleHeader(c)
				if (err == nil) != tc.ok || tc.ok && got != token {
					t.Fatal(got, err)
				}
				return nil
			})
			req := httptest.NewRequest("GET", "/", nil)
			for _, v := range tc.headers {
				req.Header.Add("If-Match", v)
			}
			router.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
