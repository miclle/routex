package handler

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

const telegramFixturePassword = githubFixturePassword
const telegramFixtureProfile = "telegram.oidc.v1"
const telegramAdminSubject = "Telegram-Administrator-101"
const telegramMemberSubject = "Telegram-Member-202"
const telegramFixtureIssuer = "https://oauth.telegram.org"

type telegramTLSCode struct{ subject, challenge, redirect, nonce string }
type telegramTokenHold struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (h *telegramTokenHold) release() { h.once.Do(func() { close(h.resume) }) }

type telegramTLSFixture struct {
	server                                       *httptest.Server
	roots                                        *x509.CertPool
	key                                          *rsa.PrivateKey
	github                                       *githubTLSFixture
	mu                                           sync.Mutex
	codes                                        map[string]telegramTLSCode
	tokenCalls, profileCalls, closes, unexpected int
	hold                                         *telegramTokenHold
	issuer, nonce                                string
	subject                                      any
	duplicateSubject                             bool
}

// Only the operation-local test transport maps fixed public hosts. TLS still
// verifies their literal SAN names; neither global DNS nor trust is changed.
func newTelegramTLSFixture(t *testing.T) *telegramTLSFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate controlled TLS key")
	}
	signing, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate controlled RSA signing key")
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Controlled Telegram fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create controlled CA")
	}
	parsed, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal("parse controlled CA")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "oauth.telegram.org"}, DNSNames: []string{"oauth.telegram.org"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, parsed, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create exact-host TLS certificate")
	}
	p := &telegramTLSFixture{roots: x509.NewCertPool(), key: signing, codes: map[string]telegramTLSCode{}}
	p.roots.AddCert(parsed)
	p.server = httptest.NewUnstartedServer(http.HandlerFunc(p.serve))
	p.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	p.server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: key}}}
	p.server.StartTLS()
	t.Cleanup(func() {
		p.mu.Lock()
		h := p.hold
		p.hold = nil
		p.mu.Unlock()
		if h != nil {
			h.release()
		}
		p.server.Close()
	})
	return p
}
func (p *telegramTLSFixture) transport() *http.Transport {
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "oauth.telegram.org:443" {
			return nil, errors.New("fixture destination rejected")
		}
		return (&net.Dialer{}).DialContext(ctx, network, p.server.Listener.Addr().String())
	}}
}

type telegramFixtureTransport struct{ telegram, github *http.Transport }

func (t telegramFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Host {
	case "github.com", "api.github.com":
		return t.github.RoundTrip(r)
	case "oauth.telegram.org":
		return t.telegram.RoundTrip(r)
	default:
		return nil, errors.New("fixture destination rejected")
	}
}
func (p *telegramTLSFixture) factory(context.Context) (service.NamedIdentityTransport, error) {
	g, h := p.transport(), p.github.transport()
	return service.NamedIdentityTransport{RoundTripper: telegramFixtureTransport{telegram: g, github: h}, Close: func() error {
		g.CloseIdleConnections()
		h.CloseIdleConnections()
		p.mu.Lock()
		p.closes++
		p.mu.Unlock()
		return nil
	}}, nil
}
func (p *telegramTLSFixture) holdToken(t *testing.T) *telegramTokenHold {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		t.Fatal("token hold already armed")
	}
	h := &telegramTokenHold{entered: make(chan struct{}), resume: make(chan struct{})}
	p.hold = h
	return h
}
func (p *telegramTLSFixture) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenCalls, p.profileCalls, p.closes
}
func (p *telegramTLSFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "TLS required", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Host + r.URL.Path {
	case "oauth.telegram.org/auth":
		q := r.URL.Query()
		subject := r.Header.Get("X-Test-Subject")
		if r.Method != "GET" || len(q) != 8 || q.Get("client_id") != "routex-telegram-test-client" || q.Get("response_type") != "code" || q.Get("scope") != "openid" || q.Get("redirect_uri") != "https://routex.test/api/v1/auth/telegram/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("code_challenge")) != 43 || len(q.Get("nonce")) != 43 || subject == "" {
			w.WriteHeader(400)
			return
		}
		for _, key := range []string{"response_type", "client_id", "redirect_uri", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
			if len(q[key]) != 1 {
				w.WriteHeader(400)
				return
			}
		}
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			w.WriteHeader(500)
			return
		}
		code := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.codes[code] = telegramTLSCode{subject: subject, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri"), nonce: q.Get("nonce")}
		p.mu.Unlock()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		w.Header().Set("Location", callback.String())
		w.WriteHeader(302)
	case "oauth.telegram.org/token":
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		user, password, basic := r.BasicAuth()
		clientID, e1 := url.QueryUnescape(user)
		clientSecret, e2 := url.QueryUnescape(password)
		if r.Method != "POST" || r.URL.RawQuery != "" || !basic || e1 != nil || e2 != nil || clientID != "routex-telegram-test-client" || clientSecret != "test-only-telegram-client-secret" || r.ParseForm() != nil || len(r.PostForm) != 5 || r.PostForm.Get("client_id") != "routex-telegram-test-client" || r.PostForm.Has("client_secret") {
			w.WriteHeader(400)
			return
		}
		p.mu.Lock()
		saved, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.tokenCalls++
		hold := p.hold
		p.hold = nil
		issuer, nonce, subject, duplicate := p.issuer, p.nonce, p.subject, p.duplicateSubject
		p.mu.Unlock()
		digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != saved.redirect || len(r.Form.Get("code_verifier")) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != saved.challenge {
			w.WriteHeader(400)
			return
		}
		if hold != nil {
			close(hold.entered)
			select {
			case <-hold.resume:
			case <-r.Context().Done():
				return
			}
		}
		if issuer == "" {
			issuer = telegramFixtureIssuer
		}
		if nonce == "" {
			nonce = saved.nonce
		}
		if subject == nil {
			subject = saved.subject
		}
		access := "test-only-telegram-access-token"
		hash := sha256.Sum256([]byte(access))
		now := time.Now()
		claims := map[string]any{"iss": issuer, "sub": subject, "aud": "routex-telegram-test-client", "azp": "routex-telegram-test-client", "nonce": nonce, "iat": now.Unix(), "exp": now.Add(time.Minute).Unix(), "at_hash": base64.RawURLEncoding.EncodeToString(hash[:16])}
		payload, e := json.Marshal(claims)
		if e != nil {
			w.WriteHeader(500)
			return
		}
		if duplicate {
			payload = append([]byte(`{"sub":"not-the-bound-sub",`), payload[1:]...)
		}
		signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: p.key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "telegram-fixture-key"))
		if e != nil {
			w.WriteHeader(500)
			return
		}
		signed, e := signer.Sign(payload)
		if e != nil {
			w.WriteHeader(500)
			return
		}
		token, e := signed.CompactSerialize()
		if e != nil {
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "Bearer", "access_token": access, "id_token": token})
	case "oauth.telegram.org/.well-known/jwks.json":
		p.mu.Lock()
		p.profileCalls++
		p.mu.Unlock()
		if r.Method != "GET" || r.URL.RawQuery != "" {
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &p.key.PublicKey, KeyID: "telegram-fixture-key", Algorithm: "RS256", Use: "sig"}}})
	default:
		p.mu.Lock()
		p.unexpected++
		p.mu.Unlock()
		w.WriteHeader(404)
	}
}

type telegramApplicationFixture struct {
	ctx                       context.Context
	router                    *fox.Engine
	service                   *service.Service
	peer                      *telegramTLSFixture
	github                    *githubApplicationFixture
	admin, member             SessionResponse
	adminCookie, memberCookie *http.Cookie
}
type telegramBrowserCeremony struct {
	cookie   *http.Cookie
	callback string
}

func telegramApplicationRequest(ctx context.Context, router http.Handler, method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "https://routex.test"+path, bytes.NewReader(raw)).WithContext(ctx)
	req.Header.Set("Origin", "https://routex.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if etag != "" {
		req.Header.Set("If-Match", `"`+etag+`"`)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	for _, c := range cookies {
		if c != nil {
			req.AddCookie(c)
		}
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
func telegramApplicationStatus(t *testing.T, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("Telegram HTTP status=%d want=%d", r.Code, want)
	}
}
func telegramApplicationDecode[T any](t *testing.T, r *httptest.ResponseRecorder, want int) T {
	t.Helper()
	telegramApplicationStatus(t, r, want)
	var out T
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatal("decode safe Telegram DTO")
	}
	return out
}
func telegramApplicationCookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	var out *http.Cookie
	for _, c := range r.Result().Cookies() {
		if c.Name == name && c.Value != "" && c.MaxAge >= 0 {
			if out != nil {
				t.Fatal("duplicate issued cookie")
			}
			out = c
		}
	}
	if out == nil {
		t.Fatal("missing issued cookie")
	}
	if !out.Secure || !out.HttpOnly || out.Path != "/" || out.Domain != "" {
		t.Fatal("cookie attributes")
	}
	return out
}
func telegramApplicationSession(t *testing.T, r *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	t.Helper()
	v := telegramApplicationDecode[SessionResponse](t, r, 200)
	if v.User.ID == "" || v.CSRFToken == "" {
		t.Fatal("missing real Session")
	}
	return v, telegramApplicationCookie(t, r, sessionCookie)
}
func (f *telegramApplicationFixture) request(method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return telegramApplicationRequest(f.ctx, f.router, method, path, body, etag, csrf, cookies...)
}
func telegramIdentityInput(recovery string) map[string]any {
	proof := map[string]any{}
	if recovery != "" {
		proof["recovery_code"] = recovery
	}
	return map[string]any{"password": telegramFixturePassword, "proof": proof, "reason": "Review exact existing member identity"}
}
func (f *telegramApplicationFixture) config(t *testing.T) service.TelegramProviderView {
	t.Helper()
	r := f.request("GET", "/api/v1/admin/auth/telegram", nil, "", "", f.adminCookie)
	v := telegramApplicationDecode[service.TelegramProviderView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("config ETag mismatch")
	}
	return v
}
func (f *telegramApplicationFixture) account(t *testing.T, c *http.Cookie) service.TelegramAccountView {
	t.Helper()
	r := f.request("GET", "/api/v1/account/identity/telegram", nil, "", "", c)
	v := telegramApplicationDecode[service.TelegramAccountView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("account ETag mismatch")
	}
	return v
}
func (f *telegramApplicationFixture) start(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) telegramBrowserCeremony {
	t.Helper()
	var body any = struct{}{}
	if path != "/api/v1/auth/telegram/start" {
		body = telegramIdentityInput(recovery)
	}
	r := f.request("POST", path, body, etag, csrf, c)
	v := telegramApplicationDecode[struct {
		AuthorizationURL string `json:"authorization_url"`
	}](t, r, 200)
	correlation := telegramApplicationCookie(t, r, "__Host-routex_telegram")
	if correlation.SameSite != http.SameSiteLaxMode {
		t.Fatal("correlation SameSite")
	}
	target, e := url.Parse(v.AuthorizationURL)
	if e != nil || target.Scheme != "https" || target.Host != "oauth.telegram.org" || target.Path != "/auth" {
		t.Fatal("fixed authorization URL changed")
	}
	transport := f.peer.transport()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, e := http.NewRequestWithContext(f.ctx, "GET", v.AuthorizationURL, nil)
	if e != nil {
		t.Fatal("authorization request")
	}
	req.Header.Set("X-Test-Subject", subject)
	response, e := client.Do(req)
	if e != nil {
		t.Fatal("controlled fixed-host authorization TLS")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1025))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1024 || response.StatusCode != 302 {
		t.Fatal("controlled authorization failed")
	}
	callback, e := url.Parse(response.Header.Get("Location"))
	if e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/telegram/callback" {
		t.Fatal("fixed callback changed")
	}
	return telegramBrowserCeremony{cookie: correlation, callback: callback.RequestURI()}
}
func (f *telegramApplicationFixture) stage(t *testing.T, c telegramBrowserCeremony) {
	t.Helper()
	r := f.request("GET", c.callback, nil, "", "", c.cookie)
	telegramApplicationStatus(t, r, 303)
	if r.Header().Get("Location") != "/auth/telegram/complete" || r.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback leaked proof or changed completion route")
	}
	for _, v := range r.Result().Cookies() {
		if v.Name == sessionCookie && v.Value != "" {
			t.Fatal("callback issued Session")
		}
	}
}
func (f *telegramApplicationFixture) begin(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) telegramBrowserCeremony {
	t.Helper()
	v := f.start(t, path, subject, c, csrf, etag, recovery)
	f.stage(t, v)
	return v
}
func (f *telegramApplicationFixture) finish(c telegramBrowserCeremony, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	return f.request("POST", "/api/v1/auth/telegram/complete", struct{}{}, "", csrf, cookie, c.cookie)
}
func (f *telegramApplicationFixture) login(t *testing.T, subject string) (SessionResponse, *http.Cookie) {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/telegram/start", subject, nil, "", "", "")
	return telegramApplicationSession(t, f.finish(c, nil, ""))
}
func newTelegramApplicationFixture(t *testing.T, db *gorm.DB) *telegramApplicationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	peer := newTelegramTLSFixture(t)
	peer.github = newGitHubTLSFixture(t)
	store, e := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if e != nil {
		t.Fatal("fixture root")
	}
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithNamedIdentityTransportFactory(peer.factory))
	if e != nil {
		t.Fatal("create named identity service")
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	f := &telegramApplicationFixture{ctx: ctx, router: router, service: svc, peer: peer}
	setup := f.request("POST", "/api/v1/setup", map[string]any{"email": "telegram-admin@example.invalid", "name": "Telegram administrator", "password": telegramFixturePassword}, "", "")
	telegramApplicationStatus(t, setup, 201)
	// Setup has the same Session shape but returns201.
	var admin SessionResponse
	if json.Unmarshal(setup.Body.Bytes(), &admin) != nil || admin.User.ID == "" {
		t.Fatal("setup Session")
	}
	f.admin = admin
	f.adminCookie = telegramApplicationCookie(t, setup, sessionCookie)
	var stored entity.User
	if db.Take(&stored, "id = ?", admin.User.ID).Error != nil {
		t.Fatal("read admitted administrator")
	}
	member := entity.User{ID: "usr_telegram_member", Email: "telegram-member@example.invalid", Name: "Telegram member", Role: entity.RoleMember, PasswordHash: stored.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("create existing member")
	}
	f.member, f.memberCookie = telegramApplicationSession(t, f.request("POST", "/api/v1/auth/login", map[string]any{"email": member.Email, "password": telegramFixturePassword}, "", ""))
	v := f.config(t)
	r := f.request("PUT", "/api/v1/admin/auth/telegram", map[string]any{"name": "Controlled Telegram", "client_id": "routex-telegram-test-client", "callback_url": "https://routex.test/api/v1/auth/telegram/callback", "secret_action": "replace", "client_secret": "test-only-telegram-client-secret", "reason": "Configure controlled fixed identity"}, v.ReviewETag, admin.CSRFToken, f.adminCookie)
	v = telegramApplicationDecode[service.TelegramProviderView](t, r, 200)
	if v.Enabled || v.Verified || !v.SecretConfigured {
		t.Fatal("save must remain disabled/unverified")
	}
	telegramApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/telegram/status", map[string]any{"enabled": true, "reason": "Cannot enable before verification"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 409)
	c := f.begin(t, "/api/v1/admin/auth/telegram/verify", telegramAdminSubject, f.adminCookie, admin.CSRFToken, v.ReviewETag, "")
	verified := telegramApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(c, f.adminCookie, admin.CSRFToken), 200)
	if verified.Kind != "verified" {
		t.Fatal("verification not completed")
	}
	v = f.config(t)
	v = telegramApplicationDecode[service.TelegramProviderView](t, f.request("PUT", "/api/v1/admin/auth/telegram/status", map[string]any{"enabled": true, "reason": "Explicit verified enable"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 200)
	if !v.Enabled || !v.Verified {
		t.Fatal("enable not explicit")
	}
	a := f.account(t, f.memberCookie)
	c = f.begin(t, "/api/v1/account/identity/telegram/bind", telegramMemberSubject, f.memberCookie, f.member.CSRFToken, a.ReviewETag, "")
	bound := telegramApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(c, f.memberCookie, f.member.CSRFToken), 200)
	if bound.Kind != "bound" {
		t.Fatal("existing member not explicitly bound")
	}
	f.github = &githubApplicationFixture{ctx: ctx, router: router, service: svc, peer: peer.github, admin: f.admin, member: f.member, adminCookie: f.adminCookie, memberCookie: f.memberCookie}
	{
		g := f.github
		v := g.config(t)
		r := g.request("PUT", "/api/v1/admin/auth/github", map[string]any{"name": "Controlled GitHub", "client_id": "routex-github-test-client", "callback_url": "https://routex.test/api/v1/auth/github/callback", "secret_action": "replace", "client_secret": "test-only-github-client-secret", "reason": "Configure controlled fixed identity"}, v.ReviewETag, admin.CSRFToken, g.adminCookie)
		v = githubApplicationDecode[service.GitHubProviderView](t, r, 200)
		if v.Enabled || v.Verified || !v.SecretConfigured {
			t.Fatal("save must remain disabled/unverified")
		}
		githubApplicationStatus(t, g.request("PUT", "/api/v1/admin/auth/github/status", map[string]any{"enabled": true, "reason": "Cannot enable before verification"}, v.ReviewETag, admin.CSRFToken, g.adminCookie), 409)
		c := g.begin(t, "/api/v1/admin/auth/github/verify", githubAdminSubject, g.adminCookie, admin.CSRFToken, v.ReviewETag, "")
		verified := githubApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, g.finish(c, g.adminCookie, admin.CSRFToken), 200)
		if verified.Kind != "verified" {
			t.Fatal("verification not completed")
		}
		v = g.config(t)
		v = githubApplicationDecode[service.GitHubProviderView](t, g.request("PUT", "/api/v1/admin/auth/github/status", map[string]any{"enabled": true, "reason": "Explicit verified enable"}, v.ReviewETag, admin.CSRFToken, g.adminCookie), 200)
		if !v.Enabled || !v.Verified {
			t.Fatal("enable not explicit")
		}
		a := g.account(t, g.memberCookie)
		c = g.begin(t, "/api/v1/account/identity/github/bind", githubMemberSubject, g.memberCookie, g.member.CSRFToken, a.ReviewETag, "")
		bound := githubApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, g.finish(c, g.memberCookie, g.member.CSRFToken), 200)
		if bound.Kind != "bound" {
			t.Fatal("existing member not explicitly bound")
		}
	}
	return f
}
