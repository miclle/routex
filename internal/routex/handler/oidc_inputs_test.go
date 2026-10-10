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

func TestOIDCInputCaptureIsBoundedAndRemovesSensitiveMaterial(t *testing.T) {
	state := strings.Repeat("s", 43)
	for _, tc := range []struct {
		name, path, query, cookie string
		valid                     bool
	}{
		{"valid", oidcCallbackPath, "state=" + state + "&code=private-code&iss=https%3A%2F%2Fissuer.example", strings.Repeat("c", 43), true},
		{"provider rejection", oidcCallbackPath, "state=" + state + "&error=access_denied&error_description=private-description", strings.Repeat("c", 43), true},
		{"duplicate state", oidcCallbackPath, "state=" + state + "&state=" + state + "&code=private-code", strings.Repeat("c", 43), false},
		{"ambiguous result", oidcCallbackPath, "state=" + state + "&code=private-code&error=private-error", strings.Repeat("c", 43), false},
		{"malformed query", oidcCallbackPath, "state=" + state + "&code=private-code&bad=%zz", strings.Repeat("c", 43), false},
		{"unknown decoration", oidcCallbackPath, "state=" + state + "&code=private-code&redirect=private-target", strings.Repeat("c", 43), false},
		{"oversized", oidcCallbackPath, "state=" + state + "&code=" + strings.Repeat("x", 8192), strings.Repeat("c", 43), false},
		{"noncallback", oidcCookiePath + "/unknown", "code=private-code", strings.Repeat("c", 43), false},
		{"short cookie", oidcCallbackPath, "state=" + state + "&code=private-code", "private-short", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, tc.path, nil)
			request.URL.RawQuery = tc.query
			request.RequestURI += "?" + tc.query
			request.AddCookie(&http.Cookie{Name: sessionCookie, Value: "retained-local-session"})
			request.AddCookie(&http.Cookie{Name: oidcCookie, Value: tc.cookie})
			actual := captureOIDCInputs(request)
			if actual.invalid == tc.valid {
				t.Fatal("unexpected protocol admission")
			}
			if request.URL.RawQuery != "" || request.URL.ForceQuery || strings.Contains(request.RequestURI, "?") || strings.Contains(request.Header.Get("Cookie"), oidcCookie) {
				t.Fatal("sensitive material remains on ordinary request")
			}
			retained, err := request.Cookie(sessionCookie)
			if err != nil || retained.Value != "retained-local-session" {
				t.Fatal("independent Session cookie lost")
			}
			if again := captureOIDCInputs(request); again != actual {
				t.Fatal("capture is not idempotent")
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, oidcCallbackPath+"?state="+state+"&code=private-code", nil)
	request.AddCookie(&http.Cookie{Name: oidcCookie, Value: strings.Repeat("c", 43)})
	request.AddCookie(&http.Cookie{Name: oidcCookie, Value: strings.Repeat("d", 43)})
	if !captureOIDCInputs(request).invalid {
		t.Fatal("duplicate correlation cookies accepted")
	}
}

func TestOIDCReviewHeaderAcceptsOnlyOneStrongExactDigest(t *testing.T) {
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/review", func(c *fox.Context) error {
		_, err := oidcReviewETag(c)
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

func TestOIDCCorrelationCookieAndInvalidCallbackHaveFixedDestinations(t *testing.T) {
	router := fox.New()
	ctrl := New(nil)
	router.GET("/cookie", func(c *fox.Context) {
		oidcSetCookie(c, &service.OIDCStart{Cookie: strings.Repeat("c", 43), ExpiresAt: time.Now().Add(5 * time.Minute)})
		c.Status(204)
	})
	router.GET(oidcCallbackPath, ctrl.OIDCCallback)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/cookie", nil))
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("expected one correlation cookie")
	}
	cookie := cookies[0]
	if cookie.Name != oidcCookie || cookie.Path != oidcCookiePath || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 300 {
		t.Fatal("unsafe cross-site cookie contract")
	}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, oidcCallbackPath+"?code=private-code&redirect=https://untrusted.example", nil))
	if response.Code != 303 || response.Header().Get("Location") != "/auth/oidc/complete" || response.Header().Get("Referrer-Policy") != "no-referrer" || strings.Contains(response.Body.String(), "private-code") {
		t.Fatal("callback reflected credentials or an untrusted destination")
	}
	if cleared := response.Result().Cookies(); len(cleared) != 1 || cleared[0].MaxAge != -1 {
		t.Fatal("invalid callback retained correlation cookie")
	}
}

func TestOIDCDefaultLoggerAndRecoveryNeverExposeCallbackMaterial(t *testing.T) {
	if os.Getenv("ROUTEX_OIDC_LOG_CHILD") == "1" {
		engine := fox.Default()
		engine.RedirectTrailingSlash = false
		engine.RedirectFixedPath = false
		engine.Engine.Use(OIDCInputs)
		engine.GET(oidcCookiePath+"/panic", func(c *fox.Context) { panic("fixed test panic") })
		for _, path := range []string{oidcCookiePath + "/missing", oidcCookiePath + "/panic/", oidcCookiePath + "/panic"} {
			request := httptest.NewRequest(http.MethodGet, path+"?code=private-callback-code&state=private-callback-state&bad=%zz", nil)
			request.AddCookie(&http.Cookie{Name: oidcCookie, Value: strings.Repeat("private-cookie", 4)})
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if strings.Contains(response.Body.String(), "private-callback") {
				t.Fatal("callback material reflected")
			}
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestOIDCDefaultLoggerAndRecoveryNeverExposeCallbackMaterial$", "-test.v")
	command.Env = append(os.Environ(), "ROUTEX_OIDC_LOG_CHILD=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("logger child failed: %v %s", err, output)
	}
	for _, sensitive := range []string{"private-callback-code", "private-callback-state", "private-cookie", url.QueryEscape("private-callback-code")} {
		if strings.Contains(string(output), sensitive) {
			t.Fatal("default logger or recovery exposed callback material")
		}
	}
	if !strings.Contains(string(output), oidcCookiePath+"/missing") || !strings.Contains(string(output), "panic recovered") {
		t.Fatal("missing real logger/recovery witness")
	}
}
