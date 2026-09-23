//go:build development

package website

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDevelopmentProxyPreservesRequestAndSetsForwardingHeaders(t *testing.T) {
	requestDetails := make(chan string, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		requestDetails <- fmt.Sprintf(
			"method=%s path=%s query=%s host=%s forwarded-host=%s origin-host=%s",
			req.Method,
			req.URL.Path,
			req.URL.RawQuery,
			req.Host,
			req.Header.Get("X-Forwarded-Host"),
			req.Header.Get("X-Origin-Host"),
		)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(backend.Close)

	target, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatalf("parse backend URL: %v", err)
	}

	proxy := newDevelopmentProxy(target)
	req := httptest.NewRequest(http.MethodGet, "http://app.example/assets/app.js?mode=dev", nil)
	recorder := httptest.NewRecorder()
	proxy.ServeHTTP(recorder, req)

	response := recorder.Result()
	t.Cleanup(func() { _ = response.Body.Close() })
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("read proxy response: %v", err)
	}
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("proxy status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}

	want := "method=GET path=/assets/app.js query=mode=dev host=app.example forwarded-host=app.example origin-host=" + target.Host
	if got := <-requestDetails; got != want {
		t.Fatalf("proxied request = %q, want %q", got, want)
	}
}

func TestDevServerURLFromEnvironment(t *testing.T) {
	t.Setenv("ROUTEX_VITE_DEV_SERVER_URL", "http://127.0.0.1:3100")
	t.Setenv("ROUTEX_VITE_PORT", "3101")

	got := devServerURLFromEnvironment()

	if got != "http://127.0.0.1:3100" {
		t.Fatalf("dev server URL = %q, want explicit URL", got)
	}
}

func TestDevServerURLFallsBackToConfiguredPort(t *testing.T) {
	t.Setenv("ROUTEX_VITE_PORT", "3101")

	got := devServerURLFromEnvironment()

	if got != "http://localhost:3101" {
		t.Fatalf("dev server URL = %q, want URL from configured port", got)
	}
}

func TestDevServerURLDefaultsToVitePort(t *testing.T) {
	got := devServerURLFromEnvironment()

	if got != "http://localhost:5173" {
		t.Fatalf("dev server URL = %q, want default Vite URL", got)
	}
}
