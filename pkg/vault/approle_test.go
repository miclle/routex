package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const loginResponse = `{"auth":{"client_token":"minted-fixture-token","lease_duration":120,"metadata":{"secret":"not-public"},"accessor":"not-public"}}`

func loginFixture(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(Descriptor{Endpoint: server.URL + "/base", Namespace: "acme/production", Mount: "kv", Prefix: "providers", DataField: "value"}, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func TestAppRoleLoginExactTransportAndTransientLease(t *testing.T) {
	var calls atomic.Int32
	client := loginFixture(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var fields map[string]string
		if json.Unmarshal(body, &fields) != nil || len(fields) != 2 || fields["role_id"] != "custom-role" || fields["secret_id"] != "secret-fixture" {
			t.Error("wrong login body")
		}
		if r.Method != http.MethodPost || r.URL.Path != "/base/v1/auth/custom/team/login" || r.URL.RawQuery != "" || r.Header.Get("X-Vault-Token") != "" || r.Header.Get("X-Vault-Namespace") != "acme/production" || r.Header.Get("X-Vault-Request") != "true" || r.Header.Get("Content-Type") != "application/json" || !r.Close {
			t.Error("wrong login transport")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, loginResponse)
	})
	token, obs, err := client.LoginAppRole(context.Background(), "custom/team", "custom-role", "secret-fixture")
	if err != nil || token == nil || !obs.Attempted || !obs.Succeeded || obs.Failure != nil || obs.Duration <= 0 || calls.Load() != 1 {
		t.Fatalf("login observation: %+v %v", obs, err)
	}
	value, err := token.Token()
	if err != nil || value != "minted-fixture-token" {
		t.Fatal("missing exact token")
	}
	encoded, _ := json.Marshal(token)
	for _, printable := range []string{string(encoded), fmt.Sprintf("%v", token), fmt.Sprintf("%+v", token), fmt.Sprintf("%#v", token), fmt.Sprintf("%+v", obs)} {
		if strings.Contains(printable, "minted-fixture-token") || strings.Contains(printable, "not-public") {
			t.Fatal("secret in ordinary result projection")
		}
	}
	owned := token.token
	token.Close()
	token.Close()
	if value, err = token.Token(); err == nil || value != "" {
		t.Fatal("closed token reusable")
	}
	for _, b := range owned {
		if b != 0 {
			t.Fatal("owned token buffer not cleared")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("Close performed remote HTTP")
	}
}

func TestAppRoleLoginRejectsAmbiguousResponseWithoutRetry(t *testing.T) {
	cases := []struct {
		name, body, content, code string
		status                    int
	}{
		{"denied", `{"errors":["private upstream text"]}`, "application/json", "http_status", 403},
		{"html", loginResponse, "text/html", "invalid_response", 200},
		{"missing-auth", `{}`, "application/json", "invalid_response", 200},
		{"null-auth", `{"auth":null}`, "application/json", "invalid_response", 200},
		{"missing-token", `{"auth":{"lease_duration":1}}`, "application/json", "invalid_response", 200},
		{"empty-token", `{"auth":{"client_token":"","lease_duration":1}}`, "application/json", "invalid_response", 200},
		{"header-token", `{"auth":{"client_token":"a\nb","lease_duration":1}}`, "application/json", "invalid_response", 200},
		{"null-lease", `{"auth":{"client_token":"ok","lease_duration":null}}`, "application/json", "invalid_response", 200},
		{"missing-lease", `{"auth":{"client_token":"ok"}}`, "application/json", "invalid_response", 200},
		{"zero-lease", `{"auth":{"client_token":"ok","lease_duration":0}}`, "application/json", "invalid_response", 200},
		{"negative-lease", `{"auth":{"client_token":"ok","lease_duration":-1}}`, "application/json", "invalid_response", 200},
		{"fraction-lease", `{"auth":{"client_token":"ok","lease_duration":1.5}}`, "application/json", "invalid_response", 200},
		{"string-lease", `{"auth":{"client_token":"ok","lease_duration":"60"}}`, "application/json", "invalid_response", 200},
		{"overflow-lease", `{"auth":{"client_token":"ok","lease_duration":9223372037}}`, "application/json", "invalid_response", 200},
		{"duplicate", `{"auth":{"client_token":"ok","client_token":"other","lease_duration":1}}`, "application/json", "invalid_response", 200},
		{"errors", `{"auth":{"client_token":"ok","lease_duration":1},"errors":["private"]}`, "application/json", "invalid_response", 200},
		{"trailing", loginResponse + `{}`, "application/json", "invalid_response", 200},
		{"oversize", strings.Repeat("x", maxResponseBytes+1), "application/json", "response_too_large", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", tc.content)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			token, obs, err := c.LoginAppRole(context.Background(), "approle", "role", "secret")
			if token != nil || err == nil || obs.Succeeded || !obs.Attempted || obs.Failure == nil || obs.Failure.Code != tc.code || obs.Failure.HTTPStatus != tc.status || calls.Load() != 1 {
				t.Fatalf("failure: %+v %v", obs, err)
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), tc.body) {
				t.Fatal("raw response in error")
			}
		})
	}
}

func TestAppRoleLoginInputAndCancellationNeverRetry(t *testing.T) {
	var calls atomic.Int32
	c := loginFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, loginResponse)
	})
	for _, input := range [][3]string{{"../approle", "role", "secret"}, {"a//b", "role", "secret"}, {"/approle", "role", "secret"}, {"a%2fb", "role", "secret"}, {strings.Repeat("a", 129), "role", "secret"}, {"approle", "", "secret"}, {"approle", "role", ""}, {"approle", "role", "x y"}, {"approle", strings.Repeat("x", 4097), "secret"}} {
		token, obs, err := c.LoginAppRole(context.Background(), input[0], input[1], input[2])
		if token != nil || err == nil || obs.Attempted {
			t.Fatal("invalid auth dispatched")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	token, obs, err := c.LoginAppRole(ctx, "approle", "role", "secret")
	if token != nil || err == nil || obs.Attempted || obs.Failure.Code != "canceled" || calls.Load() != 0 {
		t.Fatal("canceled auth dispatched")
	}
}

func TestAppRoleLoginRedirectDoesNotReachTarget(t *testing.T) {
	var targetCalls, loginCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	c := loginFixture(t, func(w http.ResponseWriter, r *http.Request) {
		loginCalls.Add(1)
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	_, obs, err := c.LoginAppRole(context.Background(), "approle", "role", "secret")
	if err == nil || obs.Succeeded || targetCalls.Load() != 0 || loginCalls.Load() != 1 {
		t.Fatal("login redirect/retry")
	}
}

func TestAppRoleLoginInFlightCancellation(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int32
	c := loginFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Observation, 1)
	go func() {
		token, obs, _ := c.LoginAppRole(ctx, "approle", "role", "secret")
		if token != nil {
			token.Close()
		}
		done <- obs
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("login not dispatched")
	}
	cancel()
	select {
	case obs := <-done:
		if !obs.Attempted || obs.Succeeded || obs.Failure == nil || obs.Failure.Code != "canceled" || calls.Load() != 1 {
			t.Fatal("cancellation observation")
		}
	case <-time.After(time.Second):
		t.Fatal("login did not cancel")
	}
}

func TestLoginTokenExpiryAndConcurrentClose(t *testing.T) {
	expired := &LoginToken{token: []byte("expired-fixture"), expiresAt: time.Now().Add(-time.Second)}
	if value, err := expired.Token(); err == nil || value != "" || len(expired.token) != 0 {
		t.Fatal("expired token usable")
	}
	token := &LoginToken{token: []byte("fixture"), expiresAt: time.Now().Add(time.Minute)}
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() { _, _ = token.Token(); token.Close() })
	}
	workers.Wait()
	if value, err := token.Token(); err == nil || value != "" {
		t.Fatal("closed token usable")
	}
}

func TestAppRoleLoginDeadlineStopsInFlightRequest(t *testing.T) {
	var calls atomic.Int32
	c := loginFixture(t, func(_ http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	token, obs, err := c.LoginAppRole(ctx, "approle", "role", "secret")
	if token != nil || err == nil || !obs.Attempted || obs.Succeeded || obs.Failure.Code != "timed_out" || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatal("deadline or request bound violated")
	}
}
