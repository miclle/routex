package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestDiagnosticAllowsRequestQueryWithoutWeakeningBaseURLs(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/openai/models" || r.URL.RawQuery != "api-version=2024-10-21" || r.Header.Get("api-key") != "test-only-key" || r.Header.Get("Authorization") != "" {
			t.Error("native adapter request changed")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()
	request, err := http.NewRequest(http.MethodGet, target.URL+"/openai/models?api-version=2024-10-21", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("api-key", "test-only-key")
	if _, err := ValidateBaseURL(request.URL.String(), true); err == nil {
		t.Fatal("stored base URL accepted query")
	}
	result, err := Diagnose(context.Background(), request, true, false, nil)
	if err != nil || !result.TransportOK || !result.APIOK || calls.Load() != 1 {
		t.Fatal("explicit native request query rejected", err, calls.Load())
	}
}

func TestDiagnosticRequestQueryRetainsUnsafeURLDenials(t *testing.T) {
	for _, raw := range []string{
		"https://user:password@example.com/models?api-version=2024-10-21",
		"https://example.com/models?api-version=2024-10-21#fragment",
		"https:opaque?api-version=2024-10-21",
		"file:///models?api-version=2024-10-21",
		"http://example.com/models?api-version=2024-10-21",
		"https://169.254.169.254/models?api-version=2024-10-21",
		"https://224.0.0.1/models?api-version=2024-10-21",
	} {
		t.Run(raw, func(t *testing.T) {
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Diagnose(context.Background(), &http.Request{Method: "GET", URL: u}, true, true, nil)
			if err == nil || result.TransportOK || result.APIOK {
				t.Fatal("unsafe request query target accepted")
			}
		})
	}
}

func TestDiagnosticQueryNeverFollowsRedirect(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/models?private=not-forwarded", http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	request, _ := http.NewRequest(http.MethodGet, origin.URL+"/openai/models?api-version=2024-10-21", nil)
	request.Header.Set("api-key", "test-only-key")
	result, err := Diagnose(context.Background(), request, true, true, nil)
	if err != nil || result.APIOK || redirected.Load() != 0 {
		t.Fatal("query-bearing diagnostic followed redirect", err, redirected.Load())
	}
}
