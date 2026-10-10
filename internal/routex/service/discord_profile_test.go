package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secretstore"
)

const discordTestSecret = "private +/:&=% credential"
const discordTestState = "sssssssssssssssssssssssssssssssssssssssssss"
const discordTestVerifier = "vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv"

func discordTestService(t *testing.T) (*Service, entity.NamedIdentityProvider) {
	t.Helper()
	store, err := secretstore.New(bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	p := entity.NamedIdentityProvider{ID: discordProviderID, ProfileID: discordProfileID, IdentityIssuer: discordIdentityIssuer, ClientID: "9007199254740993", CallbackURL: "https://routex.example/api/v1/auth/discord/callback", SecretGeneration: strings.Repeat("a", 64)}
	p.AuthCiphertext, err = store.Seal(rootReference("named_identity_providers", p.ID, p.SecretGeneration), discordTestSecret)
	if err != nil {
		t.Fatal(err)
	}
	return &Service{secrets: store}, p
}

type discordTestRoundTrip func(*http.Request) (*http.Response, error)

func (f discordTestRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type discordTestBody struct {
	io.ReadCloser
	closed *atomic.Int32
	fail   bool
	readEntered chan<- struct{}
	readOnce sync.Once
}

func (b *discordTestBody) Read(p []byte) (int, error) {
	if b.readEntered != nil { b.readOnce.Do(func() { select { case b.readEntered <- struct{}{}: default: } }) }
	return b.ReadCloser.Read(p)
}

func (b *discordTestBody) Close() error {
	b.closed.Add(1)
	err := b.ReadCloser.Close()
	if b.fail {
		return errors.New("private close diagnostic")
	}
	return err
}

type discordTLSFixture struct {
	svc                                               *Service
	provider                                          entity.NamedIdentityProvider
	server                                            *httptest.Server
	roots                                             *x509.CertPool
	factories, closes, bodies, requests, exposed      atomic.Int32
	mu                                                sync.Mutex
	deadlines                                         []time.Time
	badTrust, bodyCloseFailure, transportCloseFailure bool
	responseReadPath string
	responseReadEntered chan<- struct{}
}

// Only the socket is redirected locally; the real TLS handshake still verifies
// the fixed discord.com SAN and the private test trust anchor.
func newDiscordTLSFixture(t *testing.T, serverName string, handler http.HandlerFunc) *discordTLSFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: serverName}, DNSNames: []string{serverName}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	f := &discordTLSFixture{roots: x509.NewCertPool()}
	f.roots.AddCert(parsed)
	f.svc, f.provider = discordTestService(t)
	f.server = httptest.NewUnstartedServer(handler)
	f.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	f.server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	f.server.StartTLS()
	t.Cleanup(f.server.Close)
	WithNamedIdentityTransportFactory(func(context.Context) (NamedIdentityTransport, error) {
		f.factories.Add(1)
		roots := f.roots
		if f.badTrust {
			roots = x509.NewCertPool()
		}
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}, DisableKeepAlives: true, MaxConnsPerHost: 1, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "discord.com:443" {
				return nil, errors.New("unexpected test destination")
			}
			return (&net.Dialer{Timeout: time.Second}).DialContext(ctx, network, f.server.Listener.Addr().String())
		}}
		return NamedIdentityTransport{RoundTripper: discordTestRoundTrip(func(r *http.Request) (*http.Response, error) {
			f.requests.Add(1)
			deadline, ok := r.Context().Deadline()
			if !ok {
				return nil, errors.New("missing bounded context")
			}
			f.mu.Lock()
			f.deadlines = append(f.deadlines, deadline)
			f.mu.Unlock()
			response, err := transport.RoundTrip(r)
			if response != nil && response.Body != nil {
				f.exposed.Add(1)
				var readEntered chan<- struct{}
				if r.URL.Path == f.responseReadPath { readEntered = f.responseReadEntered }
				response.Body = &discordTestBody{ReadCloser: response.Body, closed: &f.bodies, fail: f.bodyCloseFailure, readEntered: readEntered}
			}
			return response, err
		}), Close: func() error {
			transport.CloseIdleConnections()
			f.closes.Add(1)
			if f.transportCloseFailure {
				return errors.New("private transport close diagnostic")
			}
			return nil
		}}, nil
	})(f.svc)
	return f
}

func discordTestCallback() namedIdentityCallback {
	return namedIdentityCallback{Code: "one-time-code", State: discordTestState, ExpectedState: discordTestState, PKCEVerifier: discordTestVerifier}
}
func discordTestJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
func discordTestClose(t *testing.T, closeClient func() error) {
	t.Helper()
	if err := closeClient(); err != nil {
		t.Errorf("operation close failed")
	}
}

func TestDiscordCanonicalIDsAndNamespaces(t *testing.T) {
	for _, value := range []string{"1", "9007199254740993", "18446744073709551615"} {
		if !discordCanonicalID(value) || !namedIdentityProfileSubject(discordProviderID, "string", value) {
			t.Fatal("valid exact string rejected")
		}
		if namedIdentityProfileSubject(discordProviderID, "integer", value) {
			t.Fatal("numeric JSON identity admitted")
		}
	}
	for _, value := range []string{"", "0", "01", "+1", "-1", "1.0", "1e2", " 1", "1 ", "١", "18446744073709551616", "999999999999999999999"} {
		if discordCanonicalID(value) || namedIdentityProfileSubject(discordProviderID, "string", value) {
			t.Fatal("noncanonical identity admitted")
		}
	}
	d, ok := namedIdentityDescriptor(discordProviderID)
	if !ok || d.profile != discordProfileID || d.issuer != discordIdentityIssuer || d.callbackPath != "/api/v1/auth/discord/callback" {
		t.Fatal("fixed descriptor")
	}
	if !namedIdentityProfileSubject(githubProviderID, "integer", "9007199254740993123456789") || !namedIdentityProfileSubject(googleProviderID, "string", "Case-Sensitive") {
		t.Fatal("prior profile behavior changed")
	}
	if namedIdentitySubjectDigest(discordProviderID, "string", "1") == namedIdentitySubjectDigest(githubProviderID, "string", "1") || rootReference("named_identity_providers", discordProviderID, "generation") != "named-identity:discord.oauth2.v1:discord:generation" {
		t.Fatal("profile namespace alias")
	}
}

func TestDiscordRejectsBadConfigurationBeforeTransport(t *testing.T) {
	for _, clientID := range []string{"", "0", "01", "18446744073709551616", "184467440737095516150", "name", "1 2"} {
		t.Run(clientID, func(t *testing.T) {
			svc, provider := discordTestService(t)
			provider.ClientID = clientID
			calls := 0
			WithNamedIdentityTransportFactory(func(context.Context) (NamedIdentityTransport, error) {
				calls++
				return NamedIdentityTransport{}, errors.New("private factory diagnostic")
			})(svc)
			client, closeClient, err := svc.namedIdentityProfileProtocol(context.Background(), provider)
			if client != nil || !errors.Is(err, namedIdentityUnavailable) || calls != 0 {
				t.Fatal("bad client reached transport")
			}
			discordTestClose(t, closeClient)
		})
	}
	for _, mode := range []string{"nil_context", "canceled_context"} {
		t.Run(mode, func(t *testing.T) {
			svc, provider := discordTestService(t)
			var ctx context.Context
			if mode == "canceled_context" {
				canceled, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = canceled
			}
			WithNamedIdentityTransportFactory(func(context.Context) (NamedIdentityTransport, error) {
				t.Error("canceled constructor reached transport")
				return NamedIdentityTransport{}, nil
			})(svc)
			client, closeClient, err := svc.namedIdentityProfileProtocol(ctx, provider)
			if client != nil || !errors.Is(err, namedIdentityUnavailable) {
				t.Fatal("canceled constructor admitted")
			}
			discordTestClose(t, closeClient)
		})
	}
	for _, mutate := range []func(*entity.NamedIdentityProvider){func(p *entity.NamedIdentityProvider) { p.ProfileID = googleProfileID }, func(p *entity.NamedIdentityProvider) { p.IdentityIssuer = "https://discord.com.evil" }, func(p *entity.NamedIdentityProvider) {
		p.CallbackURL = "http://routex.example/api/v1/auth/discord/callback"
	}, func(p *entity.NamedIdentityProvider) {
		p.CallbackURL = "https://routex.example/api/v1/auth/google/callback"
	}, func(p *entity.NamedIdentityProvider) { p.AuthCiphertext = "not-ciphertext" }} {
		svc, provider := discordTestService(t)
		mutate(&provider)
		WithNamedIdentityTransportFactory(func(context.Context) (NamedIdentityTransport, error) {
			t.Error("invalid profile reached transport")
			return NamedIdentityTransport{}, nil
		})(svc)
		client, closeClient, err := svc.namedIdentityProfileProtocol(context.Background(), provider)
		if client != nil || !errors.Is(err, namedIdentityUnavailable) {
			t.Fatal("invalid profile admitted")
		}
		discordTestClose(t, closeClient)
	}
}

func TestDiscordFixedTLSProofAndOperationClosure(t *testing.T) {
	var seen atomic.Int32
	var invalid atomic.Bool
	f := newDiscordTLSFixture(t, "discord.com", func(w http.ResponseWriter, r *http.Request) {
		seen.Add(1)
		if r.TLS == nil || r.Host != "discord.com" {
			invalid.Store(true)
		}
		switch r.URL.Path {
		case "/api/v10/oauth2/token":
			u, p, ok := r.BasicAuth()
			decoded, decodeErr := url.QueryUnescape(p)
			raw, readErr := io.ReadAll(io.LimitReader(r.Body, 32<<10))
			form, parseErr := url.ParseQuery(string(raw))
			if r.Method != "POST" || !ok || u != "9007199254740993" || decodeErr != nil || decoded != discordTestSecret || readErr != nil || parseErr != nil || len(form) != 4 || form.Get("grant_type") != "authorization_code" || form.Get("code") != "one-time-code" || form.Get("redirect_uri") != "https://routex.example/api/v1/auth/discord/callback" || form.Get("code_verifier") != discordTestVerifier || form.Has("client_id") || form.Has("client_secret") || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				invalid.Store(true)
			}
			discordTestJSON(w, 200, `{"access_token":"opaque-test-token","token_type":"Bearer","scope":"identify email","refresh_token":"discarded","expires_in":3600}`)
		case "/api/v10/users/@me":
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer opaque-test-token" || r.URL.RawQuery != "" {
				invalid.Store(true)
			}
			discordTestJSON(w, 200, `{"id":"18446744073709551615","username":"not-identity","email":"not-identity"}`)
		default:
			invalid.Store(true)
			discordTestJSON(w, 404, `{}`)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, closeClient, err := f.svc.namedIdentityProfileProtocol(ctx, f.provider)
	if err != nil {
		t.Fatal("construct fixed protocol")
	}
	t.Cleanup(func() { discordTestClose(t, closeClient) })
	authorization, err := client.AuthorizationURL(ctx, namedIdentityAuthorization{State: discordTestState, PKCEVerifier: discordTestVerifier})
	if err != nil {
		t.Fatal("authorization URL")
	}
	u, err := url.Parse(authorization)
	challenge := sha256.Sum256([]byte(discordTestVerifier))
	want := url.Values{"response_type": {"code"}, "client_id": {f.provider.ClientID}, "redirect_uri": {f.provider.CallbackURL}, "scope": {"identify"}, "state": {discordTestState}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}}
	if err != nil || u.Scheme != "https" || u.Host != "discord.com" || u.Path != "/oauth2/authorize" || !reflect.DeepEqual(u.Query(), want) || seen.Load() != 0 {
		t.Fatal("exact seven authorization parameters")
	}
	identity, err := client.Exchange(ctx, discordTestCallback())
	if err != nil || identity.Kind != "string" || identity.Subject != "18446744073709551615" || invalid.Load() || seen.Load() != 2 {
		t.Fatal("genuine TLS identity proof failed")
	}
	firstCloseErr := closeClient()
	secondCloseErr := closeClient()
	f.mu.Lock()
	deadlines := append([]time.Time(nil), f.deadlines...)
	f.mu.Unlock()
	parentDeadline, _ := ctx.Deadline()
	if firstCloseErr != nil || secondCloseErr != nil || f.factories.Load() != 1 || f.closes.Load() != 1 || f.bodies.Load() != 2 || len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) || deadlines[0].After(parentDeadline) {
		t.Fatal("owned transport/body/shared deadline")
	}
}

func TestDiscordTLSRejectsUntrustedIdentityShapes(t *testing.T) {
	cases := []struct{ name, body string }{
		{"numeric", `{"id":9007199254740993}`}, {"zero", `{"id":"0"}`}, {"leading_zero", `{"id":"01"}`}, {"overflow", `{"id":"18446744073709551616"}`}, {"spaces", `{"id":" 1"}`}, {"missing", `{"username":"1"}`}, {"null", `{"id":null}`},
		{"duplicate", `{"id":"1","id":"2"}`}, {"escaped_duplicate", `{"id":"1","\u0069d":"2"}`}, {"surrogate", `{"id":"\ud800"}`}, {"raw_utf8", "{\"id\":\"\xff\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscordTLSFixture(t, "discord.com", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v10/oauth2/token" {
					discordTestJSON(w, 200, `{"access_token":"opaque-test-token","token_type":"Bearer"}`)
				} else {
					discordTestJSON(w, 200, tc.body)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, closeClient, err := f.svc.namedIdentityProfileProtocol(ctx, f.provider)
			if err != nil {
				t.Fatal("construct")
			}
			t.Cleanup(func() { discordTestClose(t, closeClient) })
			identity, err := client.Exchange(ctx, discordTestCallback())
			if !errors.Is(err, apperrors.ErrUnauthorized) || identity != (namedIdentityRemoteIdentity{}) || f.requests.Load() != 2 || f.bodies.Load() != 2 {
				t.Fatal("invalid remote identity admitted or replayed")
			}
		})
	}
}

func TestDiscordTLSResponseBoundsAndNoFallback(t *testing.T) {
	cases := []struct {
		name                   string
		tokenStatus            int
		tokenBody, profileBody string
		calls                  int32
	}{
		{"redirect307", 307, `{}`, `{"id":"1"}`, 1}, {"redirect308", 308, `{}`, `{"id":"1"}`, 1}, {"unauthorized", 401, `{"error":"private-provider-detail"}`, `{"id":"1"}`, 1},
		{"wrong_type", 200, `{"access_token":"opaque","token_type":"Basic"}`, `{"id":"1"}`, 1}, {"duplicate_token", 200, `{"access_token":"first","access_token":"second","token_type":"Bearer"}`, `{"id":"1"}`, 1},
		{"token_limit", 200, strings.Repeat("x", (64<<10)+1), `{"id":"1"}`, 1}, {"profile_limit", 200, `{"access_token":"opaque","token_type":"Bearer"}`, strings.Repeat("x", (256<<10)+1), 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDiscordTLSFixture(t, "discord.com", func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v10/oauth2/token" {
					w.Header().Set("Location", "https://discord.com/forbidden-retry")
					discordTestJSON(w, tc.tokenStatus, tc.tokenBody)
				} else {
					discordTestJSON(w, 200, tc.profileBody)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, closeClient, err := f.svc.namedIdentityProfileProtocol(ctx, f.provider)
			if err != nil {
				t.Fatal("construct")
			}
			t.Cleanup(func() { discordTestClose(t, closeClient) })
			identity, err := client.Exchange(ctx, discordTestCallback())
			if err == nil || identity != (namedIdentityRemoteIdentity{}) || f.requests.Load() != tc.calls || f.bodies.Load() != tc.calls || strings.Contains(err.Error(), "private-provider-detail") || strings.Contains(err.Error(), discordTestSecret) {
				t.Fatal("response admission/privacy/replay")
			}
		})
	}
}

func TestDiscordTLSCancellationJoinsOwnedRequest(t *testing.T) {
	for _, phase := range []string{"token", "profile"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			readEntered := make(chan struct{}, 1)
			release := make(chan struct{})
			var releaseOnce sync.Once
			handlerDone := make(chan struct{})
			var doneOnce sync.Once
			var handlerStarted, peerInvalid atomic.Bool
			f := newDiscordTLSFixture(t, "discord.com", func(w http.ResponseWriter, r *http.Request) {
				blocked := (phase == "token" && r.URL.Path == "/api/v10/oauth2/token") || (phase == "profile" && r.URL.Path == "/api/v10/users/@me")
				if blocked {
					handlerStarted.Store(true)
					defer doneOnce.Do(func() { close(handlerDone) })
				}
				// Read to EOF before waiting: net/http can then observe a disconnected
				// POST client while the handler is blocked on its request context.
				n, readErr := io.Copy(io.Discard, io.LimitReader(r.Body, (32<<10)+1))
				closeErr := r.Body.Close()
				if readErr != nil || closeErr != nil || n > 32<<10 {
					peerInvalid.Store(true)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if blocked {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(200)
					if flusher, ok := w.(http.Flusher); ok { flusher.Flush() }
					select { case entered <- struct{}{}: default: }
					select { case <-r.Context().Done(): case <-release: }
					return
				}
				discordTestJSON(w, 200, `{"access_token":"opaque","token_type":"Bearer"}`)
			})
			f.responseReadPath = "/api/v10/oauth2/token"
			if phase == "profile" { f.responseReadPath = "/api/v10/users/@me" }
			f.responseReadEntered = readEntered
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			client, closeClient, err := f.svc.namedIdentityProfileProtocol(ctx, f.provider)
			if err != nil { cancel(); t.Fatal("construct") }
			type result struct { identity namedIdentityRemoteIdentity; err error }
			completed := make(chan result, 1)
			exchangeDone := make(chan struct{})
			joined, peerJoined := false, false
			t.Cleanup(func() {
				defer discordTestClose(t, closeClient)
				cancel()
				releaseOnce.Do(func() { close(release) })
				// One existing one-second cleanup allowance, shared by both joins.
				limit := time.NewTimer(time.Second)
				defer limit.Stop()
				if !joined { select { case <-exchangeDone: joined = true; case <-limit.C: t.Error("owned exchange did not join"); return } }
				if handlerStarted.Load() && !peerJoined { select { case <-handlerDone: peerJoined = true; case <-limit.C: t.Error("owned TLS handler did not join"); return } }
			})
			go func() {
				defer close(exchangeDone)
				identity, err := client.Exchange(ctx, discordTestCallback())
				completed <- result{identity, err}
			}()
			select { case <-entered: case <-ctx.Done(): t.Fatal("controlled phase not reached") }
			// Server Flush alone does not prove RoundTrip exposed a response. Wait
			// for the SDK to enter its owned body Read before canceling.
			select { case <-readEntered: case <-ctx.Done(): t.Fatal("owned response read not reached") }
			cancel()
			joinLimit := time.NewTimer(time.Second)
			defer joinLimit.Stop()
			select {
			case got := <-completed:
				if !errors.Is(got.err, namedIdentityUnavailable) || got.identity != (namedIdentityRemoteIdentity{}) { t.Fatal("canceled proof admitted") }
			case <-joinLimit.C: t.Fatal("owned request failed to join")
			}
			select { case <-exchangeDone: joined = true; case <-joinLimit.C: t.Fatal("owned exchange did not exit") }
			select { case <-handlerDone: peerJoined = true; case <-time.After(time.Second): t.Fatal("TLS peer did not observe cancellation") }
			want := int32(1)
			if phase == "profile" { want = 2 }
			if peerInvalid.Load() || f.requests.Load() != want || f.exposed.Load() != want || f.bodies.Load() != f.exposed.Load() {
				t.Fatal("canceled response leaked or replayed")
			}
		})
	}
}

func TestDiscordTLSClosureAndTrustFailures(t *testing.T) {
	for _, mode := range []string{"body_close", "transport_close", "wrong_san", "wrong_trust"} {
		t.Run(mode, func(t *testing.T) {
			name := "discord.com"
			if mode == "wrong_san" {
				name = "wrong.example"
			}
			var received atomic.Int32
			f := newDiscordTLSFixture(t, name, func(w http.ResponseWriter, r *http.Request) {
				received.Add(1)
				if r.URL.Path == "/api/v10/oauth2/token" {
					discordTestJSON(w, 200, `{"access_token":"opaque","token_type":"Bearer"}`)
				} else {
					discordTestJSON(w, 200, `{"id":"9007199254740993"}`)
				}
			})
			f.bodyCloseFailure = mode == "body_close"
			f.transportCloseFailure = mode == "transport_close"
			f.badTrust = mode == "wrong_trust"
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, closeClient, err := f.svc.namedIdentityProfileProtocol(ctx, f.provider)
			if err != nil {
				t.Fatal("construct")
			}
			identity, exchangeErr := client.Exchange(ctx, discordTestCallback())
			first := closeClient()
			second := closeClient()
			if f.closes.Load() != 1 {
				t.Fatal("transport not closed exactly once")
			}
			if mode == "transport_close" {
				if exchangeErr != nil || identity.Subject != "9007199254740993" || first == nil || second != first {
					t.Fatal("close uncertainty not retained")
				}
				return
			}
			if !errors.Is(exchangeErr, namedIdentityUnavailable) || identity != (namedIdentityRemoteIdentity{}) || first != nil || second != nil {
				t.Fatal("failed TLS/response close admitted proof")
			}
			if mode == "body_close" {
				if received.Load() != 1 || f.bodies.Load() != 1 {
					t.Fatal("close failure continued to profile")
				}
			} else if received.Load() != 0 || f.requests.Load() != 1 {
				t.Fatal("unverified TLS reached HTTP or retried")
			}
		})
	}
}

func TestDiscordEndpointPolicyAndRequestRoles(t *testing.T) {
	calls := 0
	transport := discordRequestTransport{next: discordTestRoundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("private downstream") })}
	for _, tc := range []struct{ method, target string }{
		{"POST", "http://discord.com/api/v10/oauth2/token"}, {"POST", "https://discord.com:443/api/v10/oauth2/token"}, {"POST", "https://discord.com.evil/api/v10/oauth2/token"}, {"POST", "https://user:secret@discord.com/api/v10/oauth2/token"}, {"POST", "https://discord.com/api/v10/oauth2/%74oken"}, {"POST", discordTokenURL + "?secret=value"}, {"POST", discordTokenURL + "#fragment"}, {"GET", discordTokenURL}, {"POST", discordUserURL}, {"POST", discordAuthorizationURL},
	} {
		r, err := http.NewRequestWithContext(context.Background(), tc.method, tc.target, nil)
		if err != nil {
			t.Fatal("request fixture")
		}
		if _, err = transport.RoundTrip(r); err == nil || calls != 0 {
			t.Fatal("unsafe role/destination reached transport")
		}
	}
	if _, err := transport.RoundTrip(nil); err == nil || calls != 0 {
		t.Fatal("nil request reached transport")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(canceled, http.MethodPost, discordTokenURL, nil)
	if err != nil {
		t.Fatal("canceled request fixture")
	}
	if _, err := transport.RoundTrip(request); err == nil || calls != 0 {
		t.Fatal("canceled request reached transport")
	}
	_, provider := discordTestService(t)
	base, _ := url.Parse(discordAuthorizationURL)
	q := url.Values{"response_type": {"code"}, "client_id": {provider.ClientID}, "redirect_uri": {provider.CallbackURL}, "scope": {"identify"}, "state": {discordTestState}, "code_challenge": {discordTestVerifier}, "code_challenge_method": {"S256"}}
	base.RawQuery = q.Encode()
	if err := discordEndpointPolicy(context.Background(), base, provider.ClientID, provider.CallbackURL, false); err != nil {
		t.Fatal("valid exact policy")
	}
	for _, change := range []func(url.Values){func(q url.Values) { q.Set("scope", "identify email") }, func(q url.Values) { q.Set("code_challenge_method", "plain") }, func(q url.Values) { q.Del("state") }, func(q url.Values) { q.Add("client_id", provider.ClientID) }, func(q url.Values) { q.Set("nonce", "not-an-oidc-profile") }} {
		copyURL := *base
		copyQuery := base.Query()
		change(copyQuery)
		copyURL.RawQuery = copyQuery.Encode()
		if discordEndpointPolicy(context.Background(), &copyURL, provider.ClientID, provider.CallbackURL, false) == nil {
			t.Fatal("authorization mutation admitted")
		}
	}
}
