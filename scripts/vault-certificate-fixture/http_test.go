package main

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestControlCannotReceiveOrReplaceApplicationSession(t *testing.T) {
	o := newFixtureOwner(context.Background(), t.TempDir(), "before")
	address, _ := url.Parse(applicationURL)
	original := strings.Repeat("a", 64)
	o.client.Jar.SetCookies(address, []*http.Cookie{{Name: "routex_session", Value: original, Path: "/"}})
	calls := 0
	o.client.Transport = fixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Port() == "8200" {
			if r.Header.Get("Cookie") != "" || r.Header.Get("X-Vault-Token") != "owned-token" {
				t.Fatal("Vault received application authority")
			}
		} else if cookie, err := r.Cookie("routex_session"); err != nil || cookie.Value != original || r.Header.Get("X-Vault-Token") != "" {
			t.Fatal("application Session was changed by Vault")
		}
		header := make(http.Header)
		header.Add("Set-Cookie", "routex_session=hostile; Path=/")
		return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
	})
	if o.control("control", "GET", "/sys/health", "owned-token", nil, 200) != nil || o.api("session", "GET", "/auth/session", "", nil, 200, nil) != nil || calls != 2 {
		t.Fatal("isolated requests failed")
	}
}
