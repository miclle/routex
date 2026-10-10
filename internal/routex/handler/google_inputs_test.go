package handler

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGoogleInputCaptureCorrelationBoundsAndPrivacy(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	authority := "state=" + proof + "&iss=" + url.QueryEscape(googleResponseIssuer)
	valid := googleCallbackPath + "?" + authority + "&code=private-code"
	cookie := googleCookie + "=" + proof
	for _, test := range []struct {
		name, target   string
		cookies        []string
		invalid        bool
		expectedCookie string
		retainedQuery  string
	}{
		{"one_exact_cookie", valid, []string{cookie}, false, proof, ""},
		{"duplicate_cookie_headers", valid, []string{cookie, cookie}, true, proof, ""},
		{"duplicate_cookie_parts", valid, []string{cookie + "; " + cookie}, true, proof, ""},
		{"missing_cookie_value", valid, []string{googleCookie}, true, "", ""},
		{"quoted_cookie", valid, []string{googleCookie + "=\"" + proof + "\""}, true, "\"" + proof + "\"", ""},
		{"cookie_alias_has_no_authority", valid, []string{"routex_google=" + proof}, false, "", ""},
		{"cookie_case_alias_has_no_authority", valid, []string{"__host-routex_google=" + proof}, false, "", ""},
		{"foreign_profile_cookie_has_no_authority", valid, []string{"__Host-routex_github=" + proof}, false, "", ""},
		{"noncanonical_cookie", valid, []string{googleCookie + "=" + strings.Repeat("A", 42) + "B"}, true, strings.Repeat("A", 42) + "B", ""},
		{"missing_state", googleCallbackPath + "?iss=" + url.QueryEscape(googleResponseIssuer) + "&code=private-code", []string{cookie}, true, proof, ""},
		{"state_case_alias", googleCallbackPath + "?State=" + proof + "&iss=" + url.QueryEscape(googleResponseIssuer) + "&code=private-code", []string{cookie}, true, proof, ""},
		{"duplicate_state", valid + "&state=" + proof, []string{cookie}, true, proof, ""},
		{"escaped_duplicate_state", valid + "&%73tate=" + proof, []string{cookie}, true, proof, ""},
		{"duplicate_code", valid + "&code=private-code", []string{cookie}, true, proof, ""},
		{"duplicate_issuer", valid + "&iss=" + url.QueryEscape(googleResponseIssuer), []string{cookie}, true, proof, ""},
		{"missing_issuer", googleCallbackPath + "?state=" + proof + "&code=private-code", []string{cookie}, true, proof, ""},
		{"jwt_short_issuer_not_callback_issuer", googleCallbackPath + "?state=" + proof + "&code=private-code&iss=accounts.google.com", []string{cookie}, true, proof, ""},
		{"issuer_case_alias", googleCallbackPath + "?state=" + proof + "&code=private-code&iss=https%3A%2F%2FAccounts.google.com", []string{cookie}, true, proof, ""},
		{"issuer_trailing_slash", googleCallbackPath + "?state=" + proof + "&code=private-code&iss=" + url.QueryEscape(googleResponseIssuer+"/"), []string{cookie}, true, proof, ""},
		{"foreign_issuer", googleCallbackPath + "?state=" + proof + "&code=private-code&iss=https%3A%2F%2Fidentity.example.invalid", []string{cookie}, true, proof, ""},
		{"native_rejection", googleCallbackPath + "?" + authority + "&error=access_denied&error_description=private-decoration", []string{cookie}, false, proof, ""},
		{"duplicate_error", googleCallbackPath + "?" + authority + "&error=access_denied&error=access_denied", []string{cookie}, true, proof, ""},
		{"code_and_error", valid + "&error=access_denied", []string{cookie}, true, proof, ""},
		{"code_and_empty_error", valid + "&error=", []string{cookie}, true, proof, ""},
		{"empty_code", googleCallbackPath + "?" + authority + "&code=", []string{cookie}, true, proof, ""},
		{"ignored_scope", valid + "&scope=openid+profile", []string{cookie}, false, proof, ""},
		{"ignored_unknown_redirect", valid + "&redirect=https%3A%2F%2Fexample.invalid", []string{cookie}, false, proof, ""},
		{"ignored_repeated_decoration", valid + "&future=one&future=two", []string{cookie}, false, proof, ""},
		{"ignored_authority_case_decoration", valid + "&State=other&Code=other&ISS=other", []string{cookie}, false, proof, ""},
		{"malformed_escape", valid + "&future=%zz", []string{cookie}, true, proof, ""},
		{"malformed_semicolon", valid + "&future=a;b", []string{cookie}, true, proof, ""},
		{"query_on_start", googleAuthPath + "/start?code=private-code", []string{cookie}, true, proof, ""},
		{"forced_empty_query", googleAuthPath + "/start?", []string{cookie}, true, proof, ""},
		{"query_on_unknown_auth_path", googleAuthPath + "/missing?code=private-code", []string{cookie}, true, proof, ""},
		{"escaped_auth_path", strings.Replace(valid, "/google/", "/%67oogle/", 1), []string{cookie}, true, proof, ""},
		{"unrelated_query_preserved", "/api/v1/calls?cursor=opaque", []string{cookie}, false, proof, "cursor=opaque"},
		{"raw_query_bound", googleCallbackPath + "?" + strings.Repeat("a", 8193), []string{cookie}, true, proof, ""},
		{"code_at_bound", googleCallbackPath + "?" + authority + "&code=" + strings.Repeat("x", 4096), []string{cookie}, false, proof, ""},
		{"code_over_bound", googleCallbackPath + "?" + authority + "&code=" + strings.Repeat("x", 4097), []string{cookie}, true, proof, ""},
		{"decoration_at_bound", valid + "&future=" + strings.Repeat("x", 2048), []string{cookie}, false, proof, ""},
		{"decoration_over_bound", valid + "&future=" + strings.Repeat("x", 2049), []string{cookie}, true, proof, ""},
		{"key_at_bound", valid + "&" + strings.Repeat("x", 128) + "=one", []string{cookie}, false, proof, ""},
		{"key_over_bound", valid + "&" + strings.Repeat("x", 129) + "=one", []string{cookie}, true, proof, ""},
		{"occurrences_at_bound", valid + strings.Repeat("&future=x", 125), []string{cookie}, false, proof, ""},
		{"occurrences_over_bound", valid + strings.Repeat("&future=x", 126), []string{cookie}, true, proof, ""},
		{"error_over_bound", googleCallbackPath + "?" + authority + "&error=" + strings.Repeat("x", 257), []string{cookie}, true, proof, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.target, nil)
			for _, value := range test.cookies {
				r.Header.Add("Cookie", value)
			}
			r.Header.Add("Cookie", "routex_session=retained-session; __Host-routex_github=retained-github")
			got := captureGoogleInputs(r)
			if got.invalid != test.invalid || got.cookie != test.expectedCookie {
				t.Fatal("correlation or rejection differs from the declared case")
			}
			if r.URL.RawQuery != test.retainedQuery || r.URL.ForceQuery || strings.Contains(r.RequestURI, "private-code") || strings.Contains(r.RequestURI, "private-decoration") {
				t.Fatal("callback privacy or unrelated query preservation failed")
			}
			if strings.Contains(r.Header.Get("Cookie"), googleCookie) || !strings.Contains(r.Header.Get("Cookie"), "routex_session=retained-session") || !strings.Contains(r.Header.Get("Cookie"), "__Host-routex_github=retained-github") {
				t.Fatal("cookie isolation or redaction failed")
			}
			if again := captureGoogleInputs(r); !reflect.DeepEqual(again, got) {
				t.Fatal("redacted capture lost original authority")
			}
			if !got.invalid && r.URL.Path == googleCallbackPath {
				if got.state != proof || got.responseIssuer != googleResponseIssuer || (got.code == "") == (got.remoteError == "") {
					t.Fatal("provider decorations replaced callback authority")
				}
			}
		})
	}
}

func TestGoogleInputMiddlewareRedactsBeforeDownstreamObservation(t *testing.T) {
	proof := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for _, target := range []string{googleCallbackPath + "?state=" + proof + "&code=private-code&iss=" + url.QueryEscape(googleResponseIssuer), googleAuthPath + "/unknown?code=private-code", "/unrelated?cursor=opaque"} {
		t.Run(target[:strings.Index(target, "?")], func(t *testing.T) {
			engine := gin.New()
			observed := false
			engine.Use(GoogleInputs, func(c *gin.Context) {
				observed = true
				if strings.Contains(c.Request.Header.Get("Cookie"), googleCookie) || strings.Contains(c.Request.RequestURI, "private-code") || strings.Contains(c.Request.URL.RawQuery, "private-code") {
					t.Error("sensitive inputs reached downstream observation")
				}
				c.Next()
			})
			engine.NoRoute(func(c *gin.Context) { c.Status(http.StatusNotFound) })
			r := httptest.NewRequest(http.MethodGet, target, nil)
			r.Header.Set("Cookie", googleCookie+"="+proof)
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, r)
			if !observed || w.Code != http.StatusNotFound {
				t.Fatal("unknown route did not traverse the real middleware")
			}
		})
	}
}

func TestGoogleBrowserProofRequiresCanonicalIndependentBytes(t *testing.T) {
	for _, input := range []string{"", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("A", 42) + "B", strings.Repeat("+", 43)} {
		if googleBrowserProof(input) {
			t.Fatal("invalid proof admitted")
		}
	}
	if !googleBrowserProof(base64.RawURLEncoding.EncodeToString(make([]byte, 32))) {
		t.Fatal("canonical proof rejected")
	}
}
