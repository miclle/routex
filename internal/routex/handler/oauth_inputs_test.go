package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func TestOAuthInputCaptureIsBoundedAndRemovesSensitiveMaterial(t *testing.T) {
	state := strings.Repeat("s", 43)
	for _, tc := range []struct {
		name, path, query, cookie string
		valid                     bool
	}{
		{"valid", oauthCallbackPath, "state=" + state + "&code=private-code", strings.Repeat("c", 43), true},
		{"provider rejection", oauthCallbackPath, "state=" + state + "&error=access_denied&error_description=private-description", strings.Repeat("c", 43), true},
		{"duplicate state", oauthCallbackPath, "state=" + state + "&state=" + state + "&code=private-code", strings.Repeat("c", 43), false},
		{"ambiguous result", oauthCallbackPath, "state=" + state + "&code=private-code&error=private-error", strings.Repeat("c", 43), false},
		{"malformed query", oauthCallbackPath, "state=" + state + "&code=private-code&bad=%zz", strings.Repeat("c", 43), false},
		{"OIDC issuer is not generic OAuth proof", oauthCallbackPath, "state=" + state + "&code=private-code&iss=https%3A%2F%2Fissuer.example", strings.Repeat("c", 43), false},
		{"unknown decoration", oauthCallbackPath, "state=" + state + "&code=private-code&redirect=private-target", strings.Repeat("c", 43), false},
		{"oversized", oauthCallbackPath, "state=" + state + "&code=" + strings.Repeat("x", 8192), strings.Repeat("c", 43), false},
		{"noncallback", oauthCookiePath + "/unknown", "code=private-code", strings.Repeat("c", 43), false},
		{"short cookie", oauthCallbackPath, "state=" + state + "&code=private-code", "private-short", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.URL.RawQuery = tc.query
			request.RequestURI += "?" + tc.query
			request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "retained-local-session"})
			request.AddCookie(&http.Cookie{Name: oauthCookie, Value: tc.cookie})
			actual := captureOAuthInputs(request)
			if actual.invalid == tc.valid {
				t.Fatal("unexpected protocol admission")
			}
			if request.URL.RawQuery != "" || request.URL.ForceQuery || strings.Contains(request.RequestURI, "?") || strings.Contains(request.Header.Get("Cookie"), oauthCookie) {
				t.Fatal("sensitive material remains on ordinary request")
			}
			retained, err := request.Cookie(sessionCookie)
			if err != nil || retained.Value != "retained-local-session" {
				t.Fatal("independent Session cookie lost")
			}
			if again := captureOAuthInputs(request); again != actual {
				t.Fatal("capture is not idempotent")
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, oauthCallbackPath+"?state="+state+"&code=private-code", nil)
	request.AddCookie(&http.Cookie{Name: oauthCookie, Value: strings.Repeat("c", 43)})
	request.AddCookie(&http.Cookie{Name: oauthCookie, Value: strings.Repeat("d", 43)})
	if !captureOAuthInputs(request).invalid {
		t.Fatal("duplicate correlation cookies accepted")
	}
}

func TestOAuthReviewHeaderAcceptsOnlyOneStrongExactDigest(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/review", func(c *fox.Context) error {
		_, err := oauthReviewETag(c)
		if err == nil {
			c.Status(http.StatusNoContent)
		}
		return err
	})
	strong := `"` + strings.Repeat("a", 64) + `"`
	for _, tc := range []struct {
		headers []string
		status  int
	}{
		{[]string{strong}, 204}, {nil, 400}, {[]string{strong, strong}, 400},
		{[]string{"W/" + strong}, 400}, {[]string{strings.Repeat("a", 64)}, 400},
		{[]string{`"` + strings.Repeat("A", 64) + `"`}, 400}, {[]string{strong + ", " + strong}, 400},
	} {
		request := httptest.NewRequest(http.MethodGet, "/review", nil)
		for _, header := range tc.headers {
			request.Header.Add("If-Match", header)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Fatal("strong review contract", response.Code)
		}
	}
}

func TestOAuthCorrelationCookieAndInvalidCallbackHaveFixedDestinations(t *testing.T) {
	router := fox.New()
	ctrl := New(nil)
	router.GET("/cookie", func(c *fox.Context) {
		oauthSetCookie(c, &service.OAuthStart{Cookie: strings.Repeat("c", 43), ExpiresAt: time.Now().Add(5 * time.Minute)})
		c.Status(204)
	})
	router.GET(oauthCallbackPath, ctrl.OAuthCallback)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/cookie", nil))
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("expected one correlation cookie")
	}
	cookie := cookies[0]
	if cookie.Name != oauthCookie || cookie.Path != oauthCookiePath || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 300 {
		t.Fatal("unsafe cross-site cookie contract")
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, oauthCallbackPath+"?code=private-code&redirect=https://untrusted.example", nil))
	if response.Code != 303 || response.Header().Get("Location") != "/auth/oauth/complete" || response.Header().Get("Referrer-Policy") != "no-referrer" || strings.Contains(response.Body.String(), "private-code") {
		t.Fatal("callback reflected credentials or an untrusted destination")
	}
	if cleared := response.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge != -1 {
		t.Fatal("invalid callback retained correlation cookie")
	}
}

func TestOAuthDefaultLoggerAndRecoveryNeverExposeCallbackMaterial(t *testing.T) {
	if os.Getenv("ROUTEX_OAUTH_LOG_CHILD") == "1" {
		engine := fox.Default()
		engine.RedirectTrailingSlash = false
		engine.RedirectFixedPath = false
		engine.Engine.Use(OAuthInputs)
		engine.GET(oauthCookiePath+"/panic", func(c *fox.Context) { panic("fixed test panic") })
		for _, path := range []string{oauthCookiePath + "/missing", oauthCookiePath + "/panic/", oauthCookiePath + "/panic"} {
			request := httptest.NewRequest(http.MethodGet, path+"?code=private-callback-code&state=private-callback-state&bad=%zz", nil)
			request.AddCookie(&http.Cookie{Name: oauthCookie, Value: strings.Repeat("private-cookie", 4)})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if strings.Contains(response.Body.String(), "private-callback") {
				t.Fatal("callback material reflected")
			}
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestOAuthDefaultLoggerAndRecoveryNeverExposeCallbackMaterial$", "-test.v")
	command.Env = append(os.Environ(), "ROUTEX_OAUTH_LOG_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("logger child failed: %v %s", err, output)
	}
	for _, sensitive := range []string{"private-callback-code", "private-callback-state", "private-cookie", url.QueryEscape("private-callback-code")} {
		if strings.Contains(string(output), sensitive) {
			t.Fatal("default logger or recovery exposed callback material")
		}
	}
	if !strings.Contains(string(output), oauthCookiePath+"/missing") || !strings.Contains(string(output), "panic recovered") {
		t.Fatal("missing real logger/recovery witness")
	}
}
