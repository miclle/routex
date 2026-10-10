package handler

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
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

const githubFixturePassword = "test-only-github-application-password"
const githubFixtureProfile = "github.com.oauth-app.v1"
const githubAdminSubject = "101"
const githubMemberSubject = "202"

type githubTLSCode struct{ subject, challenge, redirect string }
type githubTokenHold struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (h *githubTokenHold) release() { h.once.Do(func() { close(h.resume) }) }

type githubTLSFixture struct {
	server                           *httptest.Server
	roots                            *x509.CertPool
	mu                               sync.Mutex
	codes                            map[string]githubTLSCode
	tokens                           map[string]string
	tokenCalls, profileCalls, closes int
	hold                             *githubTokenHold
	profileID                        any
}

// The service still sees the literal public HTTPS URLs. Only this per-Service
// test transport maps the two approved hosts to a local SAN-verified TLS peer.
func newGitHubTLSFixture(t *testing.T) *githubTLSFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate controlled TLS key")
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Controlled GitHub fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create controlled CA")
	}
	parsed, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal("parse controlled CA")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "github.com"}, DNSNames: []string{"github.com", "api.github.com"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, parsed, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create exact-host TLS certificate")
	}
	p := &githubTLSFixture{roots: x509.NewCertPool(), codes: map[string]githubTLSCode{}, tokens: map[string]string{}}
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
func (p *githubTLSFixture) transport() *http.Transport {
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "github.com:443" && address != "api.github.com:443" {
			return nil, errors.New("fixture destination rejected")
		}
		return (&net.Dialer{}).DialContext(ctx, network, p.server.Listener.Addr().String())
	}}
}
func (p *githubTLSFixture) factory(context.Context) (service.NamedIdentityTransport, error) {
	transport := p.transport()
	return service.NamedIdentityTransport{RoundTripper: transport, Close: func() error { transport.CloseIdleConnections(); p.mu.Lock(); p.closes++; p.mu.Unlock(); return nil }}, nil
}
func (p *githubTLSFixture) holdToken(t *testing.T) *githubTokenHold {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		t.Fatal("token hold already armed")
	}
	h := &githubTokenHold{entered: make(chan struct{}), resume: make(chan struct{})}
	p.hold = h
	return h
}
func (p *githubTLSFixture) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenCalls, p.profileCalls, p.closes
}
func (p *githubTLSFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "TLS required", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Host + r.URL.Path {
	case "github.com/login/oauth/authorize":
		q := r.URL.Query()
		subject := r.Header.Get("X-Test-Subject")
		if r.Method != "GET" || q.Get("client_id") != "routex-github-test-client" || q.Get("response_type") != "code" || q.Has("scope") || q.Get("redirect_uri") != "https://routex.test/api/v1/auth/github/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("code_challenge")) != 43 || q.Has("nonce") || subject == "" {
			w.WriteHeader(400)
			return
		}
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			w.WriteHeader(500)
			return
		}
		code := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.codes[code] = githubTLSCode{subject: subject, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		w.Header().Set("Location", callback.String())
		w.WriteHeader(302)
	case "github.com/login/oauth/access_token":
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.Method != "POST" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "application/json" || r.ParseForm() != nil {
			w.WriteHeader(400)
			return
		}
		p.mu.Lock()
		saved, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.tokenCalls++
		hold := p.hold
		p.hold = nil
		p.mu.Unlock()
		digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || r.Form.Get("client_id") != "routex-github-test-client" || r.Form.Get("client_secret") != "test-only-github-client-secret" || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != saved.redirect || len(r.Form.Get("code_verifier")) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != saved.challenge {
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
		raw := make([]byte, 32)
		if _, e := rand.Read(raw); e != nil {
			w.WriteHeader(500)
			return
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.tokens[token] = saved.subject
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "bearer", "access_token": token})
	case "api.github.com/user":
		if r.Method != "GET" || r.URL.RawQuery != "" || r.Header.Get("User-Agent") != "RouteX" {
			w.WriteHeader(400)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		p.mu.Lock()
		subject, exists := p.tokens[token]
		delete(p.tokens, token)
		p.profileCalls++
		override := p.profileID
		p.mu.Unlock()
		if !ok || !exists {
			w.WriteHeader(401)
			return
		}
		var value any = json.Number(subject)
		if override != nil {
			value = override
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": value, "login": "not-a-binding-identity", "email": "not-a-binding@example.invalid"})
	default:
		w.WriteHeader(404)
	}
}

type githubApplicationFixture struct {
	ctx                       context.Context
	router                    *fox.Engine
	service                   *service.Service
	peer                      *githubTLSFixture
	admin, member             SessionResponse
	adminCookie, memberCookie *http.Cookie
}
type githubBrowserCeremony struct {
	cookie   *http.Cookie
	callback string
}

func githubApplicationRequest(ctx context.Context, router http.Handler, method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
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
func githubApplicationStatus(t *testing.T, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("GitHub HTTP status=%d want=%d", r.Code, want)
	}
}
func githubApplicationDecode[T any](t *testing.T, r *httptest.ResponseRecorder, want int) T {
	t.Helper()
	githubApplicationStatus(t, r, want)
	var out T
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatal("decode safe GitHub DTO")
	}
	return out
}
func githubApplicationCookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
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
func githubApplicationSession(t *testing.T, r *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	t.Helper()
	v := githubApplicationDecode[SessionResponse](t, r, 200)
	if v.User.ID == "" || v.CSRFToken == "" {
		t.Fatal("missing real Session")
	}
	return v, githubApplicationCookie(t, r, sessionCookie)
}
func (f *githubApplicationFixture) request(method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return githubApplicationRequest(f.ctx, f.router, method, path, body, etag, csrf, cookies...)
}
func githubIdentityInput(recovery string) map[string]any {
	proof := map[string]any{}
	if recovery != "" {
		proof["recovery_code"] = recovery
	}
	return map[string]any{"password": githubFixturePassword, "proof": proof, "reason": "Review exact existing member identity"}
}
func (f *githubApplicationFixture) config(t *testing.T) service.GitHubProviderView {
	t.Helper()
	r := f.request("GET", "/api/v1/admin/auth/github", nil, "", "", f.adminCookie)
	v := githubApplicationDecode[service.GitHubProviderView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("config ETag mismatch")
	}
	return v
}
func (f *githubApplicationFixture) account(t *testing.T, c *http.Cookie) service.GitHubAccountView {
	t.Helper()
	r := f.request("GET", "/api/v1/account/identity/github", nil, "", "", c)
	v := githubApplicationDecode[service.GitHubAccountView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("account ETag mismatch")
	}
	return v
}
func (f *githubApplicationFixture) start(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) githubBrowserCeremony {
	t.Helper()
	var body any = struct{}{}
	if path != "/api/v1/auth/github/start" {
		body = githubIdentityInput(recovery)
	}
	r := f.request("POST", path, body, etag, csrf, c)
	v := githubApplicationDecode[struct {
		AuthorizationURL string `json:"authorization_url"`
	}](t, r, 200)
	correlation := githubApplicationCookie(t, r, "__Host-routex_github")
	if correlation.SameSite != http.SameSiteLaxMode {
		t.Fatal("correlation SameSite")
	}
	target, e := url.Parse(v.AuthorizationURL)
	if e != nil || target.Scheme != "https" || target.Host != "github.com" || target.Path != "/login/oauth/authorize" {
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
	if e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/github/callback" {
		t.Fatal("fixed callback changed")
	}
	return githubBrowserCeremony{cookie: correlation, callback: callback.RequestURI()}
}
func (f *githubApplicationFixture) stage(t *testing.T, c githubBrowserCeremony) {
	t.Helper()
	r := f.request("GET", c.callback, nil, "", "", c.cookie)
	githubApplicationStatus(t, r, 303)
	if r.Header().Get("Location") != "/auth/github/complete" || r.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback leaked proof or changed completion route")
	}
	for _, v := range r.Result().Cookies() {
		if v.Name == sessionCookie && v.Value != "" {
			t.Fatal("callback issued Session")
		}
	}
}
func (f *githubApplicationFixture) begin(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) githubBrowserCeremony {
	t.Helper()
	v := f.start(t, path, subject, c, csrf, etag, recovery)
	f.stage(t, v)
	return v
}
func (f *githubApplicationFixture) finish(c githubBrowserCeremony, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	return f.request("POST", "/api/v1/auth/github/complete", struct{}{}, "", csrf, cookie, c.cookie)
}
func (f *githubApplicationFixture) login(t *testing.T, subject string) (SessionResponse, *http.Cookie) {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/github/start", subject, nil, "", "", "")
	return githubApplicationSession(t, f.finish(c, nil, ""))
}
func newGitHubApplicationFixture(t *testing.T, db *gorm.DB) *githubApplicationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	peer := newGitHubTLSFixture(t)
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
	f := &githubApplicationFixture{ctx: ctx, router: router, service: svc, peer: peer}
	setup := f.request("POST", "/api/v1/setup", map[string]any{"email": "github-admin@example.invalid", "name": "GitHub administrator", "password": githubFixturePassword}, "", "")
	githubApplicationStatus(t, setup, 201)
	// Setup has the same Session shape but returns201.
	var admin SessionResponse
	if json.Unmarshal(setup.Body.Bytes(), &admin) != nil || admin.User.ID == "" {
		t.Fatal("setup Session")
	}
	f.admin = admin
	f.adminCookie = githubApplicationCookie(t, setup, sessionCookie)
	var stored entity.User
	if db.Take(&stored, "id = ?", admin.User.ID).Error != nil {
		t.Fatal("read admitted administrator")
	}
	member := entity.User{ID: "usr_github_member", Email: "github-member@example.invalid", Name: "GitHub member", Role: entity.RoleMember, PasswordHash: stored.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("create existing member")
	}
	f.member, f.memberCookie = githubApplicationSession(t, f.request("POST", "/api/v1/auth/login", map[string]any{"email": member.Email, "password": githubFixturePassword}, "", ""))
	v := f.config(t)
	r := f.request("PUT", "/api/v1/admin/auth/github", map[string]any{"name": "Controlled GitHub", "client_id": "routex-github-test-client", "callback_url": "https://routex.test/api/v1/auth/github/callback", "secret_action": "replace", "client_secret": "test-only-github-client-secret", "reason": "Configure controlled fixed identity"}, v.ReviewETag, admin.CSRFToken, f.adminCookie)
	v = githubApplicationDecode[service.GitHubProviderView](t, r, 200)
	if v.Enabled || v.Verified || !v.SecretConfigured {
		t.Fatal("save must remain disabled/unverified")
	}
	githubApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/github/status", map[string]any{"enabled": true, "reason": "Cannot enable before verification"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 409)
	c := f.begin(t, "/api/v1/admin/auth/github/verify", githubAdminSubject, f.adminCookie, admin.CSRFToken, v.ReviewETag, "")
	verified := githubApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(c, f.adminCookie, admin.CSRFToken), 200)
	if verified.Kind != "verified" {
		t.Fatal("verification not completed")
	}
	v = f.config(t)
	v = githubApplicationDecode[service.GitHubProviderView](t, f.request("PUT", "/api/v1/admin/auth/github/status", map[string]any{"enabled": true, "reason": "Explicit verified enable"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 200)
	if !v.Enabled || !v.Verified {
		t.Fatal("enable not explicit")
	}
	a := f.account(t, f.memberCookie)
	c = f.begin(t, "/api/v1/account/identity/github/bind", githubMemberSubject, f.memberCookie, f.member.CSRFToken, a.ReviewETag, "")
	bound := githubApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(c, f.memberCookie, f.member.CSRFToken), 200)
	if bound.Kind != "bound" {
		t.Fatal("existing member not explicitly bound")
	}
	return f
}
