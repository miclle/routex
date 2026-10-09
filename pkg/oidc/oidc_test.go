package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	testIssuer   = "https://identity.example/tenant"
	testClientID = "routex-client"
	testSecret   = "private-client-secret"
	testRedirect = "https://routex.example/api/identity/callback"
	testState    = "sssssssssssssssssssssssssssssssssssssssssss"
	testNonce    = "nnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnnn"
	testVerifier = "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"
)

type recordedRequest struct {
	method, target, body, username, password string
	deadline                                 time.Time
}
type protocolFixture struct {
	mu       sync.Mutex
	key      *ecdsa.PrivateKey
	metadata map[string]any
	claims   map[string]any
	requests []recordedRequest
	policies []string
	override func(*http.Request) *http.Response
	deny     string
	rawToken string
}

func newProtocolFixture(t *testing.T) *protocolFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	return &protocolFixture{key: key,
		metadata: map[string]any{"issuer": testIssuer, "authorization_endpoint": testIssuer + "/authorize",
			"token_endpoint": testIssuer + "/token", "jwks_uri": testIssuer + "/keys",
			"response_types_supported": []string{"code"}, "id_token_signing_alg_values_supported": []string{"ES256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_basic"}},
		claims: map[string]any{"iss": testIssuer, "sub": "exact-subject", "aud": testClientID,
			"nonce": testNonce, "iat": now - 1, "exp": now + 300}}
}

func responseJSON(value any) *http.Response {
	body, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(body)))}
}

func (f *protocolFixture) sign(t *testing.T, claims map[string]any, key *ecdsa.PrivateKey) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "current"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func (f *protocolFixture) config(t *testing.T) Config {
	t.Helper()
	return Config{Issuer: testIssuer, ClientID: testClientID, ClientSecret: testSecret, RedirectURL: testRedirect,
		EndpointPolicy: func(ctx context.Context, target *url.URL) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.policies = append(f.policies, target.String())
			if target.String() == f.deny {
				return errors.New("private policy internals")
			}
			return ctx.Err()
		},
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body := ""
			if request.Body != nil {
				bytes, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				body = string(bytes)
				if err := request.Body.Close(); err != nil {
					return nil, err
				}
			}
			username, password, _ := request.BasicAuth()
			deadline, _ := request.Context().Deadline()
			f.mu.Lock()
			f.requests = append(f.requests, recordedRequest{request.Method, request.URL.String(), body, username, password, deadline})
			f.mu.Unlock()
			if f.override != nil {
				if result := f.override(request); result != nil {
					return result, nil
				}
			}
			switch request.URL.String() {
			case testIssuer + "/.well-known/openid-configuration":
				return responseJSON(f.metadata), nil
			case testIssuer + "/token":
				raw := f.rawToken
				if raw == "" {
					raw = f.sign(t, f.claims, f.key)
				}
				return responseJSON(map[string]any{"access_token": "private-access-token", "token_type": "Bearer", "id_token": raw}), nil
			case testIssuer + "/keys":
				return responseJSON(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "current", Use: "sig", Algorithm: "ES256"}}}), nil
			default:
				t.Errorf("unexpected endpoint: %s", request.URL)
				return nil, errors.New("unexpected endpoint")
			}
		})}
}

func testCallback() Callback {
	return Callback{Code: "single-code", State: testState, ExpectedState: testState,
		ExpectedNonce: testNonce, PKCEVerifier: testVerifier}
}

func discoverFixture(t *testing.T, fixture *protocolFixture) *Client {
	t.Helper()
	client, err := Discover(context.Background(), fixture.config(t))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestAuthorizationCodeUsesExactIdentityS256AndBasicWithoutRetry(t *testing.T) {
	fixture := newProtocolFixture(t)
	client := discoverFixture(t, fixture)
	authorization, err := client.AuthorizationURL(context.Background(), Authorization{testState, testNonce, testVerifier})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorization)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	expected := url.Values{"client_id": {testClientID}, "redirect_uri": {testRedirect}, "response_type": {"code"},
		"scope": {"openid"}, "state": {testState}, "nonce": {testNonce}, "code_challenge_method": {"S256"},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(testVerifier)}}
	if query.Encode() != expected.Encode() {
		t.Fatalf("authorization parameters differ: %v", query)
	}
	identity, err := client.Exchange(context.Background(), testCallback())
	if err != nil {
		t.Fatal(err)
	}
	if identity != (Identity{Issuer: testIssuer, Subject: "exact-subject"}) {
		t.Fatal("identity was substituted")
	}
	if len(fixture.requests) != 3 {
		t.Fatalf("requests=%d, want discovery/token/JWKS once", len(fixture.requests))
	}
	token := fixture.requests[1]
	if token.method != http.MethodPost || token.target != testIssuer+"/token" ||
		token.username != testClientID || token.password != testSecret {
		t.Fatal("explicit Basic exchange changed")
	}
	form, err := url.ParseQuery(token.body)
	if err != nil {
		t.Fatal(err)
	}
	expectedForm := url.Values{"grant_type": {"authorization_code"}, "code": {"single-code"},
		"redirect_uri": {testRedirect}, "code_verifier": {testVerifier}}
	if form.Encode() != expectedForm.Encode() {
		t.Fatal("token exchange body changed")
	}
	for _, request := range fixture.requests {
		remaining := time.Until(request.deadline)
		if remaining <= 0 || remaining > OperationTimeout {
			t.Fatal("uncapped request context")
		}
	}
	for _, target := range []string{testRedirect, testIssuer + "/.well-known/openid-configuration", testIssuer + "/authorize",
		authorization, testIssuer + "/token", testIssuer + "/keys"} {
		if !contains(fixture.policies, target) {
			t.Fatalf("missing policy admission for %s", target)
		}
	}
}

func TestExchangeRejectsUntrustedCeremonyBeforeNetwork(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Callback)
	}{
		{"state_mismatch", func(c *Callback) { c.State = strings.Repeat("x", 43) }},
		{"state_short", func(c *Callback) { c.State = "short"; c.ExpectedState = "short" }},
		{"state_invalid", func(c *Callback) { c.State = strings.Repeat("/", 43); c.ExpectedState = c.State }},
		{"nonce_missing", func(c *Callback) { c.ExpectedNonce = "" }},
		{"verifier_short", func(c *Callback) { c.PKCEVerifier = "short" }},
		{"verifier_invalid", func(c *Callback) { c.PKCEVerifier = strings.Repeat("/", 43) }},
		{"code_missing", func(c *Callback) { c.Code = "" }},
		{"code_oversized", func(c *Callback) { c.Code = strings.Repeat("x", 4097) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			client := discoverFixture(t, fixture)
			callback := testCallback()
			test.mutate(&callback)
			_, err := client.Exchange(context.Background(), callback)
			if !errors.Is(err, ErrState) || len(fixture.requests) != 1 {
				t.Fatal("invalid ceremony reached network")
			}
		})
	}
}

func TestExchangeRejectsSignedButInvalidIdentityClaims(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"issuer", func(c map[string]any) { c["iss"] = "https://other.example" }},
		{"audience", func(c map[string]any) { c["aud"] = "another-client" }},
		{"nonce", func(c map[string]any) { c["nonce"] = strings.Repeat("x", 43) }},
		{"subject_missing", func(c map[string]any) { delete(c, "sub") }},
		{"subject_control", func(c map[string]any) { c["sub"] = "private\nsubject" }},
		{"subject_oversized", func(c map[string]any) { c["sub"] = strings.Repeat("x", 256) }},
		{"party_wrong", func(c map[string]any) { c["azp"] = "other" }},
		{"party_wrong_type", func(c map[string]any) { c["azp"] = []string{testClientID} }},
		{"multi_audience_without_party", func(c map[string]any) { c["aud"] = []string{testClientID, "other"} }},
		{"issued_missing", func(c map[string]any) { delete(c, "iat") }},
		{"issued_future", func(c map[string]any) { c["iat"] = time.Now().Add(time.Minute).Unix() }},
		{"issued_fractional", func(c map[string]any) { c["iat"] = float64(time.Now().Unix()) - 0.5 }},
		{"expiry_missing", func(c map[string]any) { delete(c, "exp") }},
		{"expiry_past", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }},
		{"not_before_future", func(c map[string]any) { c["nbf"] = time.Now().Add(time.Minute).Unix() }},
		{"not_before_fractional", func(c map[string]any) { c["nbf"] = float64(time.Now().Unix()) - 0.5 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			test.mutate(fixture.claims)
			client := discoverFixture(t, fixture)
			identity, err := client.Exchange(context.Background(), testCallback())
			if !errors.Is(err, ErrProtocol) || identity != (Identity{}) || len(fixture.requests) != 3 {
				t.Fatal("invalid signed identity accepted")
			}
		})
	}
}

func TestExchangeRequiresVerifiedSignatureAndApprovedAlgorithm(t *testing.T) {
	t.Run("wrong_key", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		fixture.rawToken = fixture.sign(t, fixture.claims, other)
		client := discoverFixture(t, fixture)
		if _, err := client.Exchange(context.Background(), testCallback()); !errors.Is(err, ErrProtocol) {
			t.Fatal("invalid signature accepted")
		}
	})
	t.Run("symmetric_algorithm", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: []byte(strings.Repeat("x", 32))}, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(fixture.claims)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			t.Fatal(err)
		}
		fixture.rawToken, err = signed.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		client := discoverFixture(t, fixture)
		if _, err := client.Exchange(context.Background(), testCallback()); !errors.Is(err, ErrProtocol) {
			t.Fatal("symmetric signature accepted")
		}
	})
	t.Run("multi_audience_with_exact_party", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		fixture.claims["aud"] = []string{testClientID, "other"}
		fixture.claims["azp"] = testClientID
		client := discoverFixture(t, fixture)
		if _, err := client.Exchange(context.Background(), testCallback()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestDiscoveryRejectsUnsafeOrIncompatibleMetadata(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"issuer", func(m map[string]any) { m["issuer"] = testIssuer + "/" }},
		{"http_authorization", func(m map[string]any) { m["authorization_endpoint"] = "http://identity.example/auth" }},
		{"token_userinfo", func(m map[string]any) { m["token_endpoint"] = "https://user:secret@identity.example/token" }},
		{"keys_fragment", func(m map[string]any) { m["jwks_uri"] = testIssuer + "/keys#private" }},
		{"keys_empty_fragment", func(m map[string]any) { m["jwks_uri"] = testIssuer + "/keys#" }},
		{"token_query", func(m map[string]any) { m["token_endpoint"] = testIssuer + "/token?secret=x" }},
		{"keys_oversized_url", func(m map[string]any) { m["jwks_uri"] = "https://identity.example/" + strings.Repeat("x", MaxURLBytes) }},
		{"implicit_only", func(m map[string]any) { m["response_types_supported"] = []string{"id_token"} }},
		{"post_secret_only", func(m map[string]any) { m["token_endpoint_auth_methods_supported"] = []string{"client_secret_post"} }},
		{"symmetric_only", func(m map[string]any) { m["id_token_signing_alg_values_supported"] = []string{"HS256", "none"} }},
		{"same_endpoint", func(m map[string]any) { m["jwks_uri"] = m["token_endpoint"] }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProtocolFixture(t)
			test.mutate(fixture.metadata)
			if _, err := Discover(context.Background(), fixture.config(t)); err == nil || len(fixture.requests) != 1 {
				t.Fatal("unsafe discovery accepted")
			}
		})
	}
}

func TestEndpointPolicyCannotBeBypassedOrRewriteDestination(t *testing.T) {
	for _, target := range []string{testRedirect, testIssuer + "/.well-known/openid-configuration", testIssuer + "/authorize", testIssuer + "/token", testIssuer + "/keys"} {
		t.Run(strings.TrimPrefix(target, "https://"), func(t *testing.T) {
			fixture := newProtocolFixture(t)
			fixture.deny = target
			if _, err := Discover(context.Background(), fixture.config(t)); err == nil {
				t.Fatal("policy denial ignored")
			}
		})
	}
	t.Run("authorization_query", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		client := discoverFixture(t, fixture)
		raw, err := client.AuthorizationURL(context.Background(), Authorization{testState, testNonce, testVerifier})
		if err != nil {
			t.Fatal(err)
		}
		fixture.deny = raw
		if _, err := client.AuthorizationURL(context.Background(), Authorization{testState, testNonce, testVerifier}); !errors.Is(err, ErrPolicy) {
			t.Fatal("browser authorization policy bypassed")
		}
	})
	t.Run("late_token_policy", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		client := discoverFixture(t, fixture)
		fixture.deny = testIssuer + "/token"
		if _, err := client.Exchange(context.Background(), testCallback()); err == nil || len(fixture.requests) != 1 {
			t.Fatal("token policy denial dispatched")
		}
	})
	t.Run("late_keys_policy", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		client := discoverFixture(t, fixture)
		fixture.deny = testIssuer + "/keys"
		if _, err := client.Exchange(context.Background(), testCallback()); err == nil || len(fixture.requests) != 2 {
			t.Fatal("JWKS policy denial dispatched")
		}
	})
	t.Run("private_copy", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		config := fixture.config(t)
		config.EndpointPolicy = func(_ context.Context, target *url.URL) error { target.Host = "attacker.example"; return nil }
		client, err := Discover(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Exchange(context.Background(), testCallback()); err != nil {
			t.Fatal(err)
		}
		for _, request := range fixture.requests {
			if strings.Contains(request.target, "attacker.example") {
				t.Fatal("policy rewrote destination")
			}
		}
	})
}

func TestApprovedRSASignatureAndExactGoogleIssuer(t *testing.T) {
	t.Run("rsa2048", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		fixture.metadata["id_token_signing_alg_values_supported"] = []string{"RS256"}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(fixture.claims)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := signer.Sign(payload)
		if err != nil {
			t.Fatal(err)
		}
		fixture.rawToken, err = signed.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		fixture.override = func(request *http.Request) *http.Response {
			if request.URL.String() == testIssuer+"/keys" {
				return responseJSON(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Use: "sig", Algorithm: "RS256"}}})
			}
			return nil
		}
		client := discoverFixture(t, fixture)
		if _, err := client.Exchange(context.Background(), testCallback()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("no_google_alias_exception", func(t *testing.T) {
		fixture := newProtocolFixture(t)
		fixture.claims["iss"] = "accounts.google.com"
		raw := fixture.sign(t, fixture.claims, fixture.key)
		client := &Client{config: Config{Issuer: "https://accounts.google.com", ClientID: testClientID}, algorithms: []string{"ES256"}}
		identity, err := client.verify(context.Background(), raw, testNonce, []jose.JSONWebKey{{Key: &fixture.key.PublicKey, KeyID: "current", Algorithm: "ES256"}}, "private-access-token")
		if !errors.Is(err, ErrProtocol) || identity != (Identity{}) {
			t.Fatal("nonexact issuer alias accepted")
		}
	})
}
