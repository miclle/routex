package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type googleTrackedBody struct {
	io.Reader
	closed *int
}

func (b googleTrackedBody) Close() error { (*b.closed)++; return nil }
func googleSigned(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	raw, e := json.Marshal(claims)
	if e != nil {
		t.Fatal(e)
	}
	return googleSignedRaw(t, key, raw)
}
func googleSignedRaw(t *testing.T, key *rsa.PrivateKey, raw []byte) string {
	t.Helper()
	signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "google-key"))
	if e != nil {
		t.Fatal(e)
	}
	signed, e := signer.Sign(raw)
	if e != nil {
		t.Fatal(e)
	}
	token, e := signed.CompactSerialize()
	if e != nil {
		t.Fatal(e)
	}
	return token
}
func TestGoogleFixedCodeProfileVerifiedIssuerAliasesAndNoUserInfo(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	for _, issuer := range []string{googleIssuer, "accounts.google.com"} {
		t.Run(issuer, func(t *testing.T) {
			now := time.Now().Unix()
			token := googleSigned(t, key, map[string]any{"iss": issuer, "sub": "Case-Sensitive-001", "aud": testClientID, "nonce": testNonce, "iat": now - 1, "exp": now + 120})
			calls, closed := 0, 0
			var deadlines []time.Time
			var policy []string
			cfg := GoogleConfig{ClientID: testClientID, ClientSecret: testSecret, RedirectURL: testRedirect, EndpointPolicy: func(ctx context.Context, u *url.URL) error { policy = append(policy, u.String()); return ctx.Err() }, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("missing bounded deadline")
				}
				deadlines = append(deadlines, deadline)
				var result *http.Response
				switch calls {
				case 1:
					user, pass, ok := r.BasicAuth()
					if r.URL.String() != googleTokenURL || r.Method != "POST" || !ok || user != url.QueryEscape(testClientID) || pass != url.QueryEscape(testSecret) {
						t.Fatal("fixed Basic token contract")
					}
					raw, e := io.ReadAll(r.Body)
					if e != nil {
						t.Fatal(e)
					}
					_ = r.Body.Close()
					form, e := url.ParseQuery(string(raw))
					want := url.Values{"grant_type": {"authorization_code"}, "code": {"single-code"}, "redirect_uri": {testRedirect}, "code_verifier": {testVerifier}}
					if e != nil || !reflect.DeepEqual(form, want) {
						t.Fatal("token form mismatch")
					}
					result = responseJSON(map[string]any{"access_token": "transient-only", "token_type": "Bearer", "id_token": token})
				case 2:
					if r.URL.String() != googleJWKSURL || r.Method != "GET" || r.Header.Get("Authorization") != "" {
						t.Fatal("fixed public JWKS contract")
					}
					result = responseJSON(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "google-key", Use: "sig", Algorithm: "RS256"}}})
				default:
					t.Fatal("discovery/UserInfo/retry occurred")
				}
				result.Body = googleTrackedBody{result.Body, &closed}
				return result, nil
			})}
			client, e := NewGoogle(context.Background(), cfg)
			if e != nil || calls != 0 {
				t.Fatal("constructor performed I/O", e)
			}
			raw, e := client.AuthorizationURL(context.Background(), Authorization{State: testState, Nonce: testNonce, PKCEVerifier: testVerifier})
			if e != nil {
				t.Fatal(e)
			}
			u, e := url.Parse(raw)
			if e != nil {
				t.Fatal(e)
			}
			want := url.Values{"client_id": {testClientID}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid profile"}, "state": {testState}, "nonce": {testNonce}, "code_challenge": {oauth2.S256ChallengeFromVerifier(testVerifier)}, "code_challenge_method": {"S256"}}
			if u.Scheme+"://"+u.Host+u.Path != googleAuthorizationURL || !reflect.DeepEqual(u.Query(), want) || len(raw) > 8192 {
				t.Fatal("fixed authorization contract")
			}
			identity, e := client.Exchange(context.Background(), testCallback())
			if e != nil || identity != (Identity{Issuer: googleIssuer, Subject: "Case-Sensitive-001"}) {
				t.Fatal("verified stable identity", e)
			}
			if calls != 2 || closed != 2 || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) || len(policy) < 7 {
				t.Fatal("request/closure/shared-deadline contract")
			}
		})
	}
}
func TestGoogleSignedInvalidClaimsAndDuplicatePayloadFailClosed(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"issuer", "issuer_case", "sub_numeric", "sub_control", "sub_unicode", "sub_long", "nonce", "audience", "azp", "expired", "future_iat", "future_nbf", "missing_iat", "at_hash", "duplicate_sub", "escaped_duplicate", "malformed_surrogate"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().Unix()
			claims := map[string]any{"iss": googleIssuer, "sub": "exact", "aud": testClientID, "nonce": testNonce, "iat": now - 1, "exp": now + 120}
			switch mode {
			case "issuer":
				claims["iss"] = "https://other.example"
			case "issuer_case":
				claims["iss"] = "https://ACCOUNTS.google.com"
			case "sub_numeric":
				claims["sub"] = 1
			case "sub_control":
				claims["sub"] = "x\n"
			case "sub_unicode":
				claims["sub"] = "é"
			case "sub_long":
				claims["sub"] = strings.Repeat("x", 256)
			case "nonce":
				claims["nonce"] = "other"
			case "audience":
				claims["aud"] = "other"
			case "azp":
				claims["azp"] = "other"
			case "expired":
				claims["exp"] = now - 1
			case "future_iat":
				claims["iat"] = now + 60
			case "future_nbf":
				claims["nbf"] = now + 60
			case "missing_iat":
				delete(claims, "iat")
			case "at_hash":
				claims["at_hash"] = "incorrect"
			}
			raw, e := json.Marshal(claims)
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "duplicate_sub":
				raw = append(raw[:len(raw)-1], []byte(`,"sub":"alias"}`)...)
			case "escaped_duplicate":
				raw = append(raw[:len(raw)-1], []byte(`,"s\u0075b":"alias"}`)...)
			case "malformed_surrogate":
				raw = append(raw[:len(raw)-1], []byte(`,"ignored":"\ud800"}`)...)
			}
			signed := googleSignedRaw(t, key, raw)
			c := &Client{config: Config{Issuer: googleIssuer, ClientID: testClientID, google: true}, algorithms: []string{"RS256"}}
			got, e := c.verify(context.Background(), signed, testNonce, []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "google-key", Algorithm: "RS256"}}, "transient")
			if !errors.Is(e, ErrProtocol) || got != (Identity{}) {
				t.Fatal("invalid signed claim admitted", mode, e)
			}
		})
	}
}
func TestGoogleStrictJSONAdmissionEveryDepthAndUnicode(t *testing.T) {
	for _, raw := range []string{`{"a":1,"a":2}`, `{"a":{"x":1,"\u0078":2}}`, `{"ignored":"\ud800"}`, `{"\udfff":1}`, `{"a":1} {}`, `{"a":"` + string([]byte{0xff}) + `"}`, strings.Repeat(`{"a":`, 34) + `0` + strings.Repeat(`}`, 34)} {
		if _, e := googleJSONObject([]byte(raw)); !errors.Is(e, ErrProtocol) {
			t.Fatal("ambiguous JSON admitted")
		}
	}
	for _, raw := range []string{`{"a":"\ud83d\ude00"}`, `{"a":"�"}`, `{"a":{"x":1},"b":[true,null]}`} {
		if _, e := googleJSONObject([]byte(raw)); e != nil {
			t.Fatal("valid Unicode/JSON rejected", e)
		}
	}
}
func TestGoogleTransportPolicyRedirectAndCancellationNeverRetry(t *testing.T) {
	for _, mode := range []string{"redirect", "duplicate_token", "duplicate_jwks", "oversized", "cancel_token", "cancel_jwks", "deny_policy", "missing_transport", "missing_policy"} {
		t.Run(mode, func(t *testing.T) {
			calls, closed := 0, 0
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cfg := GoogleConfig{ClientID: testClientID, ClientSecret: testSecret, RedirectURL: testRedirect, EndpointPolicy: func(ctx context.Context, u *url.URL) error {
				if mode == "deny_policy" && u.String() == googleTokenURL {
					return errors.New("private policy")
				}
				return ctx.Err()
			}, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				payload := `{"access_token":"transient","token_type":"Bearer","id_token":"a.b.c"}`
				if mode == "cancel_token" || mode == "cancel_jwks" && calls == 2 {
					cancel()
				}
				if calls == 2 {
					payload = `{"keys":[]}`
				}
				if mode == "duplicate_token" {
					payload = `{"id_token":"a","id_token":"b"}`
				}
				if mode == "duplicate_jwks" && calls == 2 {
					payload = `{"keys":[],"keys":[]}`
				}
				if mode == "oversized" {
					payload = strings.Repeat("x", maxTokenBytes+1)
				}
				result := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: googleTrackedBody{strings.NewReader(payload), &closed}}
				if mode == "redirect" {
					result.StatusCode = 307
					result.Header.Set("Location", "https://evil.example")
				}
				return result, nil
			})}
			if mode == "missing_transport" {
				cfg.Transport = nil
			}
			if mode == "missing_policy" {
				cfg.EndpointPolicy = nil
			}
			c, e := NewGoogle(ctx, cfg)
			if e == nil {
				_, e = c.Exchange(ctx, testCallback())
			}
			if e == nil {
				t.Fatal("negative became identity")
			}
			if calls > 2 || closed != calls {
				t.Fatal("retry or unclosed real body")
			}
			if mode == "redirect" || mode == "duplicate_token" || mode == "oversized" || mode == "cancel_token" {
				if calls != 1 {
					t.Fatal("continued after token failure")
				}
			}
			if mode == "duplicate_jwks" || mode == "cancel_jwks" {
				if calls != 2 {
					t.Fatal("did not exercise JWKS stage")
				}
			}
			if mode == "deny_policy" || mode == "missing_transport" || mode == "missing_policy" {
				if calls != 0 {
					t.Fatal("I/O before admission")
				}
			}
		})
	}
}
