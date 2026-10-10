package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGitHubInputCaptureExactCorrelationAndPrivacy(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	valid := githubCallbackPath + "?state=" + proof + "&code=private-code"
	for _, test := range []struct {
		name, target              string
		cookies                   []string
		invalid                   bool
		cookie, code, remoteError string
		retainedQuery             string
	}{
		{"one_exact_cookie", valid, []string{githubCookie + "=" + proof + "; routex_session=other-session"}, false, proof, "private-code", "", ""},
		{"identical_duplicate_headers", valid, []string{githubCookie + "=" + proof, githubCookie + "=" + proof}, true, proof, "private-code", "", ""},
		{"identical_duplicate_parts", valid, []string{githubCookie + "=" + proof + "; " + githubCookie + "=" + proof}, true, proof, "private-code", "", ""},
		{"malformed_cookie_is_removed", valid, []string{githubCookie}, true, "", "private-code", "", ""},
		{"quoted_cookie_is_rejected", valid, []string{githubCookie + "=\"" + proof + "\""}, true, "\"" + proof + "\"", "private-code", "", ""},
		{"alias_never_supplies_proof", valid, []string{"routex_github=" + proof}, false, "", "private-code", "", ""},
		{"lowercase_name_never_supplies_proof", valid, []string{"__host-routex_github=" + proof}, false, "", "private-code", "", ""},
		{"noncanonical_cookie", valid, []string{githubCookie + "=" + strings.Repeat("A", 42) + "B"}, true, strings.Repeat("A", 42) + "B", "private-code", "", ""},
		{"provider_rejection", githubCallbackPath + "?state=" + proof + "&error=access_denied&error_description=private-decoration", []string{githubCookie + "=" + proof}, false, proof, "", "access_denied", ""},
		{"code_and_error", valid + "&error=access_denied", []string{githubCookie + "=" + proof}, true, proof, "private-code", "access_denied", ""},
		{"duplicate_state", valid + "&state=" + proof, []string{githubCookie + "=" + proof}, true, proof, "private-code", "", ""},
		{"unknown_callback_key", valid + "&redirect=https%3A%2F%2Fexample.invalid", []string{githubCookie + "=" + proof}, true, proof, "private-code", "", ""},
		{"malformed_escape", valid + "&error_description=%zz", []string{githubCookie + "=" + proof}, true, proof, "", "", ""},
		{"query_on_start", githubAuthPath + "/start?code=private-code", []string{githubCookie + "=" + proof}, true, proof, "", "", ""},
		{"query_on_unknown_auth_path", githubAuthPath + "/missing?code=private-code", []string{githubCookie + "=" + proof}, true, proof, "", "", ""},
		{"unrelated_query_is_preserved", "/api/v1/calls?cursor=opaque", []string{githubCookie + "=" + proof + "; routex_session=other-session"}, false, proof, "", "", "cursor=opaque"},
		{"bounded_callback_query", githubCallbackPath + "?" + strings.Repeat("a", 8193), []string{githubCookie + "=" + proof}, true, proof, "", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.target, nil)
			for _, cookie := range test.cookies {
				r.Header.Add("Cookie", cookie)
			}
			got := captureGitHubInputs(r)
			if got.invalid != test.invalid || got.cookie != test.cookie || got.code != test.code || got.remoteError != test.remoteError {
				t.Fatal("captured fields or rejection differ from the declared case")
			}
			if r.URL.RawQuery != test.retainedQuery || strings.Contains(r.RequestURI, "private-code") || strings.Contains(r.RequestURI, "private-decoration") {
				t.Fatal("query privacy or unrelated query preservation failed")
			}
			for _, h := range r.Header.Values("Cookie") {
				for _, part := range strings.Split(h, ";") {
					key, _, _ := strings.Cut(strings.TrimSpace(part), "=")
					if key == githubCookie {
						t.Fatal("exact correlation cookie reached ordinary headers")
					}
				}
			}
			if again := captureGitHubInputs(r); !reflect.DeepEqual(again, got) {
				t.Fatal("capture lost original proof after redaction")
			}
			if strings.Contains(strings.Join(test.cookies, ";"), "routex_session=other-session") && !strings.Contains(r.Header.Get("Cookie"), "routex_session=other-session") {
				t.Fatal("unrelated Session cookie was removed")
			}
		})
	}
}

func TestGitHubInputMiddlewareRedactsBeforeDownstreamObservation(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, test := range []struct{ name, target string }{
		{"callback", githubCallbackPath + "?state=" + proof + "&code=private-code"},
		{"unknown_auth_path", githubAuthPath + "/unknown?code=private-code"},
		{"unrelated_path", "/unrelated?cursor=opaque"},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine := gin.New()
			observed := false
			engine.Use(GitHubInputs, func(c *gin.Context) {
				observed = true
				if strings.Contains(c.Request.Header.Get("Cookie"), githubCookie) || strings.Contains(c.Request.RequestURI, "private-code") || strings.Contains(c.Request.URL.RawQuery, "private-code") {
					t.Error("sensitive correlation input reached downstream observation")
				}
				c.Next()
			})
			engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })
			r := httptest.NewRequest(http.MethodGet, test.target, nil)
			r.Header.Set("Cookie", githubCookie+"="+proof)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			if !observed || w.Code != http.StatusNotFound {
				t.Fatal("unknown path did not traverse real middleware")
			}
		})
	}
}

func TestGitHubBrowserProofRequiresCanonicalIndependentBytes(t *testing.T) {
	for _, input := range []string{"", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("A", 42) + "B", strings.Repeat("+", 43)} {
		if githubBrowserProof(input) {
			t.Fatal("invalid proof admitted")
		}
	}
	if !githubBrowserProof(base64.RawURLEncoding.EncodeToString(make([]byte, 32))) {
		t.Fatal("canonical proof rejected")
	}
}
