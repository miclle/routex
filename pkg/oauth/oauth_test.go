package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type observedBody struct {
	reader   io.Reader
	closed   bool
	closeErr error
}

func (b *observedBody) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *observedBody) Close() error               { b.closed = true; return b.closeErr }

func testConfig(transport http.RoundTripper) Config {
	return Config{AuthorizationURL: "https://identity.example/authorize", TokenURL: "https://identity.example/token",
		UserInfoURL: "https://identity.example/profile", RedirectURL: "https://routex.example/auth/oauth/callback",
		ClientID: "client :+&", ClientSecret: "secret :+&", ClientAuthMethod: ClientSecretBasic,
		Scopes: []string{"profile", "email"}, SubjectPath: []string{"account", "id"}, Transport: transport,
		EndpointPolicy: func(context.Context, *url.URL) error { return nil }}
}
func testCallback() Callback {
	return Callback{Code: "opaque code+&", State: strings.Repeat("s", 32), ExpectedState: strings.Repeat("s", 32), PKCEVerifier: strings.Repeat("v", 43)}
}
func jsonReply(body io.ReadCloser, status int) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: body}
}

func TestAuthorizationURLCopiesConfigurationAndUsesS256(t *testing.T) {
	calls := 0
	config := testConfig(testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not dispatch") }))
	client, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	config.Scopes[0], config.SubjectPath[0] = "changed", "changed"
	input := Authorization{State: strings.Repeat("s", 32), PKCEVerifier: strings.Repeat("v", 43)}
	raw, err := client.AuthorizationURL(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(input.PKCEVerifier))
	expected := url.Values{"response_type": {"code"}, "client_id": {config.ClientID}, "redirect_uri": {config.RedirectURL},
		"scope": {"profile email"}, "state": {input.State}, "code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
	if !reflect.DeepEqual(parsed.Query(), expected) || parsed.Scheme != "https" || parsed.Host != "identity.example" || parsed.Path != "/authorize" || calls != 0 {
		t.Fatalf("unexpected authorization request or dispatch: calls=%d", calls)
	}
}

func TestExchangeExactClientAuthenticationAndTypedSubject(t *testing.T) {
	for _, method := range []ClientAuthMethod{ClientSecretBasic, ClientSecretPost} {
		t.Run(string(method), func(t *testing.T) {
			calls := 0
			tokenBody := &observedBody{reader: strings.NewReader(`{"access_token":"a.b+/~_-=","token_type":"bEaReR","refresh_token":"not returned"}`)}
			profileBody := &observedBody{reader: strings.NewReader(`{"account":{"id":900719925474099312345},"email":"not identity"}`)}
			var deadline time.Time
			config := testConfig(nil)
			config.ClientAuthMethod = method
			config.Transport = testTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				current, ok := request.Context().Deadline()
				if !ok || time.Until(current) <= 0 || time.Until(current) > OperationTimeout {
					t.Fatal("missing bounded operation deadline")
				}
				if calls == 1 {
					deadline = current
				} else if !deadline.Equal(current) {
					t.Fatal("token/profile deadline renewed")
				}
				if request.URL.RawQuery != "" || request.Header.Get("Accept") != "application/json" {
					t.Fatal("query token or missing JSON negotiation")
				}
				if calls == 1 {
					if request.Method != http.MethodPost || request.URL.String() != config.TokenURL || request.GetBody != nil || request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Fatal("token request shape")
					}
					body, err := io.ReadAll(request.Body)
					if err != nil {
						t.Fatal(err)
					}
					fields, err := url.ParseQuery(string(body))
					if err != nil {
						t.Fatal(err)
					}
					expected := url.Values{"grant_type": {"authorization_code"}, "code": {testCallback().Code}, "redirect_uri": {config.RedirectURL}, "code_verifier": {testCallback().PKCEVerifier}}
					if method == ClientSecretBasic {
						user, password, ok := request.BasicAuth()
						if !ok || user != url.QueryEscape(config.ClientID) || password != url.QueryEscape(config.ClientSecret) {
							t.Fatal("credentials not form-encoded before Basic")
						}
					} else {
						if request.Header.Get("Authorization") != "" {
							t.Fatal("post method sent Basic")
						}
						expected.Set("client_id", config.ClientID)
						expected.Set("client_secret", config.ClientSecret)
					}
					if !reflect.DeepEqual(fields, expected) {
						t.Fatal("unexpected token form fields")
					}
					return jsonReply(tokenBody, http.StatusOK), nil
				}
				if calls != 2 || !tokenBody.closed || request.Method != http.MethodGet || request.URL.String() != config.UserInfoURL || request.Body != nil || request.Header.Get("Authorization") != "Bearer a.b+/~_-=" {
					t.Fatal("profile request or token closure")
				}
				return jsonReply(profileBody, http.StatusOK), nil
			})
			client, err := New(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			config.SubjectPath[0] = "mutated"
			identity, err := client.Exchange(context.Background(), testCallback())
			if err != nil || identity != (Identity{Kind: SubjectInteger, Subject: "900719925474099312345"}) || calls != 2 || !profileBody.closed {
				t.Fatalf("typed identity or closure: %v %v calls=%d", identity, err, calls)
			}
		})
	}
}

func TestConstructorAndStateRejectBeforeHTTP(t *testing.T) {
	mutations := map[string]func(*Config){
		"missing_transport":   func(c *Config) { c.Transport = nil },
		"missing_policy":      func(c *Config) { c.EndpointPolicy = nil },
		"guessed_auth":        func(c *Config) { c.ClientAuthMethod = "auto" },
		"empty_path":          func(c *Config) { c.SubjectPath = nil },
		"invalid_path_utf8":   func(c *Config) { c.SubjectPath = []string{string([]byte{0xff})} },
		"long_path":           func(c *Config) { c.SubjectPath = make([]string, 17) },
		"duplicate_scope":     func(c *Config) { c.Scopes = []string{"profile", "profile"} },
		"scope_space":         func(c *Config) { c.Scopes = []string{"profile email"} },
		"scope_quote":         func(c *Config) { c.Scopes = []string{"bad\"scope"} },
		"invalid_client_utf8": func(c *Config) { c.ClientID = string([]byte{0xff}) },
	}
	for _, role := range []string{"authorization", "token", "userinfo", "callback"} {
		for _, raw := range []string{"http://identity.example/path", "https://u:p@identity.example/path", "https://identity.example/path?x=1", "https://identity.example/path?", "https://identity.example/path#part", "https://identity.example/\\path"} {
			t.Run(role+"/"+raw, func(t *testing.T) {
				calls := 0
				c := testConfig(testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
				switch role {
				case "authorization":
					c.AuthorizationURL = raw
				case "token":
					c.TokenURL = raw
				case "userinfo":
					c.UserInfoURL = raw
				case "callback":
					c.RedirectURL = raw
				}
				if _, err := New(context.Background(), c); err != ErrConfig || calls != 0 {
					t.Fatalf("unsafe endpoint: %v calls=%d", err, calls)
				}
			})
		}
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := testConfig(testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
			mutate(&c)
			if _, err := New(context.Background(), c); err != ErrConfig || calls != 0 {
				t.Fatalf("config admitted: %v calls=%d", err, calls)
			}
		})
	}
	calls := 0
	client, err := New(context.Background(), testConfig(testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, nil })))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Callback){
		func(c *Callback) { c.State = strings.Repeat("x", 32) }, func(c *Callback) { c.PKCEVerifier = "short" },
		func(c *Callback) { c.Code = "" }, func(c *Callback) { c.Code = "code\nsecret" },
	} {
		input := testCallback()
		mutate(&input)
		if _, err := client.Exchange(context.Background(), input); err != ErrState {
			t.Fatalf("callback admitted: %v", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid callback dispatched")
	}
}

func TestPolicyFreshAllEndpointsAndCannotRewriteTargets(t *testing.T) {
	for _, role := range []string{"/authorize", "/token", "/profile", "/auth/oauth/callback"} {
		t.Run(role, func(t *testing.T) {
			denied, calls := false, 0
			config := testConfig(testTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
			config.EndpointPolicy = func(_ context.Context, target *url.URL) error {
				if denied && target.Path == role {
					return errors.New("private policy details")
				}
				return nil
			}
			client, err := New(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			denied = true
			if _, err := client.Exchange(context.Background(), testCallback()); err != ErrPolicy || calls != 0 {
				t.Fatalf("stale policy: %v calls=%d", err, calls)
			}
		})
	}
	calls := 0
	config := testConfig(testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "identity.example" {
			t.Fatal("policy rewrote dispatch")
		}
		if calls == 1 {
			return jsonReply(io.NopCloser(strings.NewReader(`{"access_token":"safe","token_type":"Bearer"}`)), 200), nil
		}
		return jsonReply(io.NopCloser(strings.NewReader(`{"account":{"id":"Exact Case"}}`)), 200), nil
	}))
	config.EndpointPolicy = func(_ context.Context, target *url.URL) error { target.Host = "rewrite.invalid"; return nil }
	client, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := client.AuthorizationURL(context.Background(), Authorization{State: testCallback().State, PKCEVerifier: testCallback().PKCEVerifier})
	if err != nil || !strings.HasPrefix(auth, config.AuthorizationURL+"?") {
		t.Fatalf("policy rewrote browser target: %v", err)
	}
	identity, err := client.Exchange(context.Background(), testCallback())
	if err != nil || identity.Subject != "Exact Case" || calls != 2 {
		t.Fatalf("fresh policy flow: %v", err)
	}
}

func TestNoRedirectFallbackOrProfileAfterTokenRejection(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308, 401, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			body := &observedBody{reader: strings.NewReader(`{"access_token":"secret","token_type":"Bearer"}`)}
			config := testConfig(testTransport(func(*http.Request) (*http.Response, error) {
				calls++
				reply := jsonReply(body, status)
				reply.Header.Set("Location", "https://other.example/stolen")
				return reply, nil
			}))
			client, err := New(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			if identity, err := client.Exchange(context.Background(), testCallback()); err != ErrProtocol || identity != (Identity{}) || calls != 1 || !body.closed {
				t.Fatalf("redirect/fallback/closure: %v calls=%d", err, calls)
			}
		})
	}
	for _, raw := range []string{`{"access_token":"secret","token_type":"Bearer","access_token":"other"}`, `{"access_token":"secret","token_type":"MAC"}`, `{"access_token":"bad token","token_type":"Bearer"}`, `{"access_token":"secret","token_type":"Bearer","error":"private"}`} {
		calls := 0
		config := testConfig(testTransport(func(*http.Request) (*http.Response, error) {
			calls++
			return jsonReply(io.NopCloser(strings.NewReader(raw)), 200), nil
		}))
		client, err := New(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Exchange(context.Background(), testCallback()); err != ErrProtocol || calls != 1 {
			t.Fatalf("invalid token dispatched profile: %v calls=%d", err, calls)
		}
	}
}

func TestSharedCancellationAndErrorPrivacy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	closed := &observedBody{reader: strings.NewReader(`{"access_token":"private-token","token_type":"Bearer"}`)}
	config := testConfig(testTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		cancel()
		return jsonReply(closed, 200), nil
	}))
	client, err := New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if identity, err := client.Exchange(ctx, testCallback()); err != ErrUnavailable || identity != (Identity{}) || calls != 1 || !closed.closed {
		t.Fatalf("cancellation: %v calls=%d", err, calls)
	}
	before := calls
	if _, err := client.Exchange(ctx, testCallback()); err != ErrUnavailable || calls != before {
		t.Fatal("canceled admission dispatched")
	}
	config.Transport = testTransport(func(*http.Request) (*http.Response, error) {
		return jsonReply(&observedBody{reader: strings.NewReader("private profile")}, 200), errors.New("private-token private-code private-url")
	})
	client, err = New(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Exchange(context.Background(), testCallback()); err != ErrUnavailable || strings.Contains(err.Error(), "private") {
		t.Fatalf("transport contents leaked: %v", err)
	}
}
