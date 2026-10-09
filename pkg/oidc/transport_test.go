package oidc

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

func TestNoPermissiveTransportOrConfigurationDefault(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing_transport", func(c *Config) { c.Transport = nil }},
		{"missing_policy", func(c *Config) { c.EndpointPolicy = nil }},
		{"http_issuer", func(c *Config) { c.Issuer = "http://identity.example" }},
		{"http_callback", func(c *Config) { c.RedirectURL = "http://routex.example/callback" }},
		{"callback_query", func(c *Config) { c.RedirectURL = testRedirect + "?state=private" }},
		{"callback_fragment", func(c *Config) { c.RedirectURL = testRedirect + "#" }},
		{"issuer_query", func(c *Config) { c.Issuer = testIssuer + "?" }},
		{"issuer_userinfo", func(c *Config) { c.Issuer = "https://secret@identity.example" }},
		{"issuer_opaque", func(c *Config) { c.Issuer = "https:identity.example" }},
		{"issuer_backslash", func(c *Config) { c.Issuer = "https://identity.example/\\path" }},
		{"empty_client", func(c *Config) { c.ClientID = "" }},
		{"oversized_client", func(c *Config) { c.ClientID = strings.Repeat("x", 257) }},
		{"empty_secret", func(c *Config) { c.ClientSecret = "" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			config := fixture.config(t)
			test.mutate(&config)
			if _, err := Discover(context.Background(), config); !errors.Is(err, ErrConfig) || len(fixture.requests) != 0 {
				t.Fatal("invalid config reached network")
			}
		})
	}
}

func TestBoundedResponsesAndRedirectsNeverFallback(t *testing.T) {
	stages := []struct {
		name, target string
		limit        int
	}{
		{"discovery", testIssuer + "/.well-known/openid-configuration", maxDiscoveryBytes},
		{"token", testIssuer + "/token", maxTokenBytes},
		{"keys", testIssuer + "/keys", maxJWKSBytes},
	}
	for _, stage := range stages {
		for _, mode := range []string{"oversize", "redirect", "server_error", "wrong_media", "malformed_json", "close_error"} {
			t.Run(stage.name+"/"+mode, func(t *testing.T) {
				fixture := newProtocolFixture(t)
				fixture.override = func(request *http.Request) *http.Response {
					if request.URL.String() != stage.target {
						return nil
					}
					result := responseJSON(map[string]any{})
					switch mode {
					case "oversize":
						result.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", stage.limit+1)))
					case "redirect":
						result.StatusCode = http.StatusTemporaryRedirect
						result.Header.Set("Location", "https://attacker.example")
					case "server_error":
						result.StatusCode = http.StatusServiceUnavailable
						result.Body = io.NopCloser(strings.NewReader("private-provider-body"))
					case "wrong_media":
						result.Header.Set("Content-Type", "text/html")
					case "malformed_json":
						result.Body = io.NopCloser(strings.NewReader("{"))
					case "close_error":
						result.Body = &closeFailure{Reader: strings.NewReader("{}")}
					}
					return result
				}
				client, err := Discover(context.Background(), fixture.config(t))
				if stage.name != "discovery" {
					if err != nil {
						t.Fatal(err)
					}
					_, err = client.Exchange(context.Background(), testCallback())
				}
				if err == nil || strings.Contains(err.Error(), "private-provider-body") || strings.Contains(err.Error(), testSecret) {
					t.Fatal("invalid response accepted or sensitive error exposed")
				}
				expected := 1
				if stage.name == "token" {
					expected = 2
				}
				if stage.name == "keys" {
					expected = 3
				}
				if len(fixture.requests) != expected {
					t.Fatalf("requests=%d want%d; retry/fallback detected", len(fixture.requests), expected)
				}
			})
		}
	}
}

type closeFailure struct{ io.Reader }

func (*closeFailure) Close() error { return errors.New("private close failure") }

func TestJWKSAndIDTokenBounds(t *testing.T) {
	cases := []string{"empty_keys", "too_many_keys", "private_key", "wrong_use", "wrong_algorithm", "trailing_json", "missing_id_token", "oversized_id_token"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			fixture.override = func(request *http.Request) *http.Response {
				if request.URL.String() == testIssuer+"/token" {
					switch name {
					case "missing_id_token":
						return responseJSON(map[string]any{"access_token": "private", "token_type": "Bearer"})
					case "oversized_id_token":
						return responseJSON(map[string]any{"access_token": "private", "token_type": "Bearer", "id_token": strings.Repeat("x", maxIDTokenBytes+1)})
					}
				}
				if request.URL.String() != testIssuer+"/keys" {
					return nil
				}
				key := jose.JSONWebKey{Key: &fixture.key.PublicKey, Use: "sig", Algorithm: "ES256"}
				keys := []jose.JSONWebKey{key}
				switch name {
				case "empty_keys":
					keys = nil
				case "too_many_keys":
					keys = make([]jose.JSONWebKey, maxKeys+1)
					for i := range keys {
						keys[i] = key
					}
				case "private_key":
					keys[0].Key = fixture.key
				case "wrong_use":
					keys[0].Use = "enc"
				case "wrong_algorithm":
					keys[0].Algorithm = "ES384"
				case "trailing_json":
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{\"keys\":[]}{}"))}
				}
				return responseJSON(jose.JSONWebKeySet{Keys: keys})
			}
			client := discoverFixture(t, fixture)
			if identity, err := client.Exchange(context.Background(), testCallback()); err == nil || identity != (Identity{}) {
				t.Fatal("invalid key/token set accepted")
			}
		})
	}
}

func TestCancellationAndSharedDeadlineRejectLatePositive(t *testing.T) {
	t.Run("preexpired_no_dispatch", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		client := discoverFixture(t, fixture)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.Exchange(ctx, testCallback()); err == nil || len(fixture.requests) != 1 {
			t.Fatal("cancelled exchange dispatched")
		}
		if _, err := client.AuthorizationURL(ctx, Authorization{testState, testNonce, testVerifier}); err == nil {
			t.Fatal("cancelled browser authorization accepted")
		}
	})
	for _, target := range []string{testIssuer + "/.well-known/openid-configuration", testIssuer + "/token", testIssuer + "/keys"} {
		t.Run("late_positive/"+strings.TrimPrefix(target, testIssuer+"/"), func(t *testing.T) {
			fixture := newProtocolFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.override = func(request *http.Request) *http.Response {
				if request.URL.String() == target {
					cancel()
				}
				return nil
			}
			client, err := Discover(ctx, fixture.config(t))
			if target != testIssuer+"/.well-known/openid-configuration" {
				if err != nil {
					t.Fatal(err)
				}
				_, err = client.Exchange(ctx, testCallback())
			}
			if err == nil {
				t.Fatal("positive accepted after cancellation")
			}
		})
	}
	t.Run("same_outer_deadline", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		deadline := time.Now().Add(time.Second)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		client, err := Discover(ctx, fixture.config(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Exchange(ctx, testCallback()); err != nil {
			t.Fatal(err)
		}
		for _, request := range fixture.requests {
			if !request.deadline.Equal(deadline) {
				t.Fatal("nested operation renewed caller deadline")
			}
		}
	})
}

func TestAuthorizationProofValidationAndNoHTTP(t *testing.T) {
	fixture := newProtocolFixture(t)
	client := discoverFixture(t, fixture)
	for _, request := range []Authorization{{"", testNonce, testVerifier}, {testState, "", testVerifier}, {testState, testNonce, "short"}} {
		if _, err := client.AuthorizationURL(context.Background(), request); !errors.Is(err, ErrState) {
			t.Fatal("bad authorization proof accepted")
		}
	}
	if len(fixture.requests) != 1 {
		t.Fatal("authorization builder performed network IO")
	}
}

func TestTransportClosesBodyAndRejectsUnknownEndpointBeforeDispatch(t *testing.T) {
	fixture := newProtocolFixture(t)
	closes := 0
	fixture.override = func(request *http.Request) *http.Response {
		if request.URL.String() != testIssuer+"/.well-known/openid-configuration" {
			return nil
		}
		result := responseJSON(fixture.metadata)
		result.Body = &countedBody{ReadCloser: result.Body, closes: &closes}
		return result
	}
	client := discoverFixture(t, fixture)
	if closes != 1 {
		t.Fatalf("discovery response close count=%d", closes)
	}
	transport := &boundedTransport{config: fixture.config(t), endpoints: map[string]requestBound{client.tokenURL: {method: http.MethodPost, bytes: maxTokenBytes}}}
	for _, request := range []*http.Request{
		mustRequest(t, http.MethodGet, client.tokenURL), mustRequest(t, http.MethodPost, "https://attacker.example/token"),
	} {
		if _, err := transport.RoundTrip(request); !errors.Is(err, ErrPolicy) {
			t.Fatal("unlisted request admitted")
		}
	}
	if len(fixture.requests) != 1 {
		t.Fatal("unlisted request reached transport")
	}
}

type countedBody struct {
	io.ReadCloser
	closes *int
}

func (b *countedBody) Close() error { (*b.closes)++; return b.ReadCloser.Close() }
func mustRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
