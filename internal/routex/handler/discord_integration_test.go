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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

const discordFixturePassword = githubFixturePassword
const discordFixtureProfile = "discord.oauth2.v1"
const discordAdminSubject = "101"
const discordMemberSubject = "202"

const discordFixtureNamespace = "https://discord.com"

type discordTLSCode struct{ subject, challenge, redirect string }
type discordTokenHold struct {
	entered, resume chan struct{}
	once            sync.Once
}

func (h *discordTokenHold) release() { h.once.Do(func() { close(h.resume) }) }

type discordTLSFixture struct {
	server                           *httptest.Server
	roots                            *x509.CertPool
	mu                               sync.Mutex
	codes                            map[string]discordTLSCode
	tokens                           map[string]string
	tokenCalls, profileCalls, closes int
	hold                             *discordTokenHold
	profileID                        any
	duplicateID                      bool
	unexpected                       int
	github                           *githubTLSFixture
}

// The service still sees the literal public HTTPS URLs. Only this per-Service
// test transport maps the one approved Discord host to a local SAN-verified TLS peer.
func newDiscordTLSFixture(t *testing.T) *discordTLSFixture {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate controlled TLS key")
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Controlled Discord fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create controlled CA")
	}
	parsed, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal("parse controlled CA")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "discord.com"}, DNSNames: []string{"discord.com"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, parsed, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create exact-host TLS certificate")
	}
	p := &discordTLSFixture{roots: x509.NewCertPool(), codes: map[string]discordTLSCode{}, tokens: map[string]string{}}
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
func (p *discordTLSFixture) transport() *http.Transport {
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: p.roots}, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "discord.com:443" {
			return nil, errors.New("fixture destination rejected")
		}
		return (&net.Dialer{}).DialContext(ctx, network, p.server.Listener.Addr().String())
	}}
}

type discordCombinedTransport struct{ discord, github *http.Transport }

func (t discordCombinedTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	switch r.URL.Host {
	case "discord.com":
		return t.discord.RoundTrip(r)
	case "github.com", "api.github.com":
		return t.github.RoundTrip(r)
	}
	return nil, errors.New("fixture destination rejected")
}
func (p *discordTLSFixture) factory(context.Context) (service.NamedIdentityTransport, error) {
	discordTransport, githubTransport := p.transport(), p.github.transport()
	return service.NamedIdentityTransport{RoundTripper: discordCombinedTransport{discordTransport, githubTransport}, Close: func() error {
		discordTransport.CloseIdleConnections()
		githubTransport.CloseIdleConnections()
		p.mu.Lock()
		p.closes++
		p.mu.Unlock()
		return nil
	}}, nil
}
func (p *discordTLSFixture) holdToken(t *testing.T) *discordTokenHold {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		t.Fatal("token hold already armed")
	}
	h := &discordTokenHold{entered: make(chan struct{}), resume: make(chan struct{})}
	p.hold = h
	return h
}
func (p *discordTLSFixture) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokenCalls, p.profileCalls, p.closes
}
func (p *discordTLSFixture) serve(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "TLS required", 400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Host + r.URL.Path {
	case "discord.com/oauth2/authorize":
		q := r.URL.Query()
		subject := r.Header.Get("X-Test-Subject")
		if r.Method != "GET" || len(q) != 7 || q.Get("client_id") != "18446744073709551615" || q.Get("response_type") != "code" || q.Get("scope") != "identify" || q.Get("redirect_uri") != "https://routex.test/api/v1/auth/discord/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("code_challenge")) != 43 || subject == "" {
			w.WriteHeader(400)
			return
		}
		for _, values := range q {
			if len(values) != 1 {
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
		p.codes[code] = discordTLSCode{subject: subject, challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
		w.Header().Set("Location", callback.String())
		w.WriteHeader(302)
	case "discord.com/api/v10/oauth2/token":
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		clientID, clientSecret, basic := r.BasicAuth()
		clientID, idErr := url.QueryUnescape(clientID)
		clientSecret, secretErr := url.QueryUnescape(clientSecret)
		if r.Method != "POST" || r.URL.RawQuery != "" || !basic || idErr != nil || secretErr != nil || clientID != "18446744073709551615" || clientSecret != "test-only-discord-client-secret" || r.ParseForm() != nil || len(r.PostForm) != 4 {
			w.WriteHeader(400)
			return
		}
		for _, values := range r.PostForm {
			if len(values) != 1 {
				w.WriteHeader(400)
				return
			}
		}

		p.mu.Lock()
		saved, ok := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.tokenCalls++
		hold := p.hold
		p.hold = nil
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
	case "discord.com/api/v10/users/@me":
		if r.Method != "GET" || r.URL.RawQuery != "" || r.Header.Get("Accept") != "application/json" {
			w.WriteHeader(400)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		p.mu.Lock()
		subject, exists := p.tokens[token]
		delete(p.tokens, token)
		p.profileCalls++
		override := p.profileID
		duplicate := p.duplicateID
		p.mu.Unlock()
		if !ok || !exists {
			w.WriteHeader(401)
			return
		}
		if duplicate {
			_, _ = io.WriteString(w, `{"id":"202","id":"202"}`)
			return
		}
		var value any = subject
		if override != nil {
			value = override
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": value, "login": "not-a-binding-identity", "email": "not-a-binding@example.invalid"})
	default:
		p.mu.Lock()
		p.unexpected++
		p.mu.Unlock()
		w.WriteHeader(404)
	}
}

type discordApplicationFixture struct {
	ctx                       context.Context
	router                    *fox.Engine
	service                   *service.Service
	peer                      *discordTLSFixture
	github                    *githubApplicationFixture
	admin, member             SessionResponse
	adminCookie, memberCookie *http.Cookie
}
type discordBrowserCeremony struct {
	cookie   *http.Cookie
	callback string
}

func discordApplicationRequest(ctx context.Context, router http.Handler, method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
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
func discordApplicationStatus(t *testing.T, r *httptest.ResponseRecorder, want int) {
	t.Helper()
	if r.Code != want {
		t.Fatalf("Discord HTTP status=%d want=%d", r.Code, want)
	}
}
func discordApplicationDecode[T any](t *testing.T, r *httptest.ResponseRecorder, want int) T {
	t.Helper()
	discordApplicationStatus(t, r, want)
	var out T
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		t.Fatal("decode safe Discord DTO")
	}
	return out
}
func discordApplicationCookie(t *testing.T, r *httptest.ResponseRecorder, name string) *http.Cookie {
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
func discordApplicationSession(t *testing.T, r *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	t.Helper()
	v := discordApplicationDecode[SessionResponse](t, r, 200)
	if v.User.ID == "" || v.CSRFToken == "" {
		t.Fatal("missing real Session")
	}
	return v, discordApplicationCookie(t, r, sessionCookie)
}
func (f *discordApplicationFixture) request(method, path string, body any, etag, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return discordApplicationRequest(f.ctx, f.router, method, path, body, etag, csrf, cookies...)
}
func discordIdentityInput(recovery string) map[string]any {
	proof := map[string]any{}
	if recovery != "" {
		proof["recovery_code"] = recovery
	}
	return map[string]any{"password": discordFixturePassword, "proof": proof, "reason": "Review exact existing member identity"}
}
func (f *discordApplicationFixture) config(t *testing.T) service.DiscordProviderView {
	t.Helper()
	r := f.request("GET", "/api/v1/admin/auth/discord", nil, "", "", f.adminCookie)
	v := discordApplicationDecode[service.DiscordProviderView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("config ETag mismatch")
	}
	return v
}
func (f *discordApplicationFixture) account(t *testing.T, c *http.Cookie) service.DiscordAccountView {
	t.Helper()
	r := f.request("GET", "/api/v1/account/identity/discord", nil, "", "", c)
	v := discordApplicationDecode[service.DiscordAccountView](t, r, 200)
	if r.Header().Get("ETag") != `"`+v.ReviewETag+`"` {
		t.Fatal("account ETag mismatch")
	}
	return v
}
func (f *discordApplicationFixture) start(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) discordBrowserCeremony {
	t.Helper()
	var body any = struct{}{}
	if path != "/api/v1/auth/discord/start" {
		body = discordIdentityInput(recovery)
	}
	r := f.request("POST", path, body, etag, csrf, c)
	v := discordApplicationDecode[struct {
		AuthorizationURL string `json:"authorization_url"`
	}](t, r, 200)
	correlation := discordApplicationCookie(t, r, "__Host-routex_discord")
	if correlation.SameSite != http.SameSiteLaxMode {
		t.Fatal("correlation SameSite")
	}
	target, e := url.Parse(v.AuthorizationURL)
	if e != nil || target.Scheme != "https" || target.Host != "discord.com" || target.Path != "/oauth2/authorize" {
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
	if e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/discord/callback" {
		t.Fatal("fixed callback changed")
	}
	return discordBrowserCeremony{cookie: correlation, callback: callback.RequestURI()}
}
func (f *discordApplicationFixture) stage(t *testing.T, c discordBrowserCeremony) {
	t.Helper()
	r := f.request("GET", c.callback, nil, "", "", c.cookie)
	discordApplicationStatus(t, r, 303)
	if r.Header().Get("Location") != "/auth/discord/complete" || r.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("callback leaked proof or changed completion route")
	}
	for _, v := range r.Result().Cookies() {
		if v.Name == sessionCookie && v.Value != "" {
			t.Fatal("callback issued Session")
		}
	}
}
func (f *discordApplicationFixture) begin(t *testing.T, path, subject string, c *http.Cookie, csrf, etag, recovery string) discordBrowserCeremony {
	t.Helper()
	v := f.start(t, path, subject, c, csrf, etag, recovery)
	f.stage(t, v)
	return v
}
func (f *discordApplicationFixture) finish(c discordBrowserCeremony, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	return f.request("POST", "/api/v1/auth/discord/complete", struct{}{}, "", csrf, cookie, c.cookie)
}
func (f *discordApplicationFixture) login(t *testing.T, subject string) (SessionResponse, *http.Cookie) {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/discord/start", subject, nil, "", "", "")
	return discordApplicationSession(t, f.finish(c, nil, ""))
}
func newDiscordApplicationFixture(t *testing.T, db *gorm.DB) *discordApplicationFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	peer := newDiscordTLSFixture(t)
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
	f := &discordApplicationFixture{ctx: ctx, router: router, service: svc, peer: peer}
	setup := f.request("POST", "/api/v1/setup", map[string]any{"email": "discord-admin@example.invalid", "name": "Discord administrator", "password": discordFixturePassword}, "", "")
	discordApplicationStatus(t, setup, 201)
	// Setup has the same Session shape but returns201.
	var admin SessionResponse
	if json.Unmarshal(setup.Body.Bytes(), &admin) != nil || admin.User.ID == "" {
		t.Fatal("setup Session")
	}
	f.admin = admin
	f.adminCookie = discordApplicationCookie(t, setup, sessionCookie)
	var stored entity.User
	if db.Take(&stored, "id = ?", admin.User.ID).Error != nil {
		t.Fatal("read admitted administrator")
	}
	member := entity.User{ID: "usr_discord_member", Email: "discord-member@example.invalid", Name: "Discord member", Role: entity.RoleMember, PasswordHash: stored.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("create existing member")
	}
	f.member, f.memberCookie = discordApplicationSession(t, f.request("POST", "/api/v1/auth/login", map[string]any{"email": member.Email, "password": discordFixturePassword}, "", ""))
	v := f.config(t)
	r := f.request("PUT", "/api/v1/admin/auth/discord", map[string]any{"name": "Controlled Discord", "client_id": "18446744073709551615", "callback_url": "https://routex.test/api/v1/auth/discord/callback", "secret_action": "replace", "client_secret": "test-only-discord-client-secret", "reason": "Configure controlled fixed identity"}, v.ReviewETag, admin.CSRFToken, f.adminCookie)
	v = discordApplicationDecode[service.DiscordProviderView](t, r, 200)
	if v.Enabled || v.Verified || !v.SecretConfigured {
		t.Fatal("save must remain disabled/unverified")
	}
	discordApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/discord/status", map[string]any{"enabled": true, "reason": "Cannot enable before verification"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 409)
	c := f.begin(t, "/api/v1/admin/auth/discord/verify", discordAdminSubject, f.adminCookie, admin.CSRFToken, v.ReviewETag, "")
	verified := discordApplicationDecode[struct {
		Kind string `json:"kind"`
	}](t, f.finish(c, f.adminCookie, admin.CSRFToken), 200)
	if verified.Kind != "verified" {
		t.Fatal("verification not completed")
	}
	v = f.config(t)
	v = discordApplicationDecode[service.DiscordProviderView](t, f.request("PUT", "/api/v1/admin/auth/discord/status", map[string]any{"enabled": true, "reason": "Explicit verified enable"}, v.ReviewETag, admin.CSRFToken, f.adminCookie), 200)
	if !v.Enabled || !v.Verified {
		t.Fatal("enable not explicit")
	}
	a := f.account(t, f.memberCookie)
	c = f.begin(t, "/api/v1/account/identity/discord/bind", discordMemberSubject, f.memberCookie, f.member.CSRFToken, a.ReviewETag, "")
	bound := discordApplicationDecode[struct {
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

func discordAssertPrimary(t *testing.T, db *gorm.DB, cookie *http.Cookie, userID string) entity.Session {
	t.Helper()
	var row entity.Session
	var provider entity.NamedIdentityProvider
	var binding entity.NamedIdentityBinding
	if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil || row.UserID != userID || row.PrimaryMethod != "discord" || row.NamedIdentityProviderID != "discord" || row.NamedIdentityProfileID != discordFixtureProfile || row.NamedIdentityBindingID == "" || row.NamedIdentityBindingCreatedAt == nil || row.NamedIdentityUserCreatedAt == nil {
		t.Fatal("issued Session lacks exact Discord primary provenance")
	}
	if db.Take(&provider, "id = ?", "discord").Error != nil || db.Take(&binding, "id = ?", row.NamedIdentityBindingID).Error != nil || binding.ProviderID != "discord" || binding.ProfileID != discordFixtureProfile || binding.IdentityIssuer != discordFixtureNamespace || binding.SubjectKind != "string" || !binding.CreatedAt.Equal(*row.NamedIdentityBindingCreatedAt) || !binding.UserCreatedAt.Equal(*row.NamedIdentityUserCreatedAt) || binding.UserID != userID || binding.ConfigRevision != row.NamedIdentityConfigRevision || row.NamedIdentityConfigRevision != provider.ConfigRevision || row.NamedIdentityPolicyRevision != provider.PolicyRevision {
		t.Fatal("issued Session differs from current exact profile/binding/birth")
	}
	if row.OIDCBindingID != "" || row.OAuthBindingID != "" || row.LDAPBindingID != "" || row.SAMLBindingID != "" {
		t.Fatal("named identity borrowed legacy primary proof")
	}
	return row
}
func discordEnableMFA(t *testing.T, f *discordApplicationFixture, session SessionResponse, cookie *http.Cookie) (SessionResponse, *http.Cookie, []string) {
	t.Helper()
	enrollment := discordApplicationDecode[service.MFAEnrollment](t, f.request("POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: discordFixturePassword}, "", session.CSRFToken, cookie), 200)
	response := f.request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: discordFixturePassword, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, "", session.CSRFToken, cookie)
	enabled := discordApplicationDecode[MFARecoveryResponse](t, response, 200)
	if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 {
		t.Fatal("native MFA enrollment incomplete")
	}
	return *enabled.Session, discordApplicationCookie(t, response, sessionCookie), enabled.RecoveryCodes
}
func discordChallenge(t *testing.T, f *discordApplicationFixture, subject string) service.MFALoginChallenge {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/discord/start", subject, nil, "", "", "")
	r := f.finish(c, nil, "")
	v := discordApplicationDecode[service.MFALoginChallenge](t, r, 202)
	if !v.MFARequired || v.ChallengeToken == "" {
		t.Fatal("Discord primary did not require native MFA")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			t.Fatal("native MFA challenge issued Session")
		}
	}
	return v
}
func testDiscordLifecycle(t *testing.T, db *gorm.DB) {
	f := newDiscordApplicationFixture(t, db)
	var googleBefore entity.NamedIdentityProvider
	if db.Session(&gorm.Session{QueryFields: true}).Take(&googleBefore, "id = ?", "google").Error != nil {
		t.Fatal("capture independent Google singleton")
	}
	defer func() {
		var current entity.NamedIdentityProvider
		if db.Session(&gorm.Session{QueryFields: true}).Take(&current, "id = ?", "google").Error != nil || !reflect.DeepEqual(current, googleBefore) {
			t.Error("Discord lifecycle changed independent Google configuration")
		}
	}()
	t.Run("configuration_review_and_canonical_client_id", func(t *testing.T) {
		review := f.config(t)
		var before entity.NamedIdentityProvider
		if db.Session(&gorm.Session{QueryFields: true}).Take(&before, "id = ?", "discord").Error != nil {
			t.Fatal("capture current configuration")
		}
		a, b, c := f.peer.counts()
		for _, id := range []any{"0", "01", "18446744073709551616", json.Number("202")} {
			discordApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/discord", map[string]any{"name": review.Name, "client_id": id, "callback_url": review.CallbackURL, "secret_action": "keep", "reason": "Reject noncanonical client identity"}, review.ReviewETag, f.admin.CSRFToken, f.adminCookie), 400)
		}
		discordApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/discord", map[string]any{"name": review.Name, "client_id": review.ClientID, "callback_url": review.CallbackURL, "secret_action": "keep", "reason": ""}, review.ReviewETag, f.admin.CSRFToken, f.adminCookie), 400)
		stale := strings.Repeat("e", 64)
		if stale == review.ReviewETag {
			stale = strings.Repeat("f", 64)
		}
		discordApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/discord/status", map[string]any{"enabled": false, "reason": "Reject stale review"}, stale, f.admin.CSRFToken, f.adminCookie), 409)
		x, y, z := f.peer.counts()
		var after entity.NamedIdentityProvider
		if a != x || b != y || c != z || db.Session(&gorm.Session{QueryFields: true}).Take(&after, "id = ?", "discord").Error != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("invalid review changed configuration or performed remote I/O")
		}
		raw := f.request("GET", "/api/v1/admin/auth/discord", nil, "", "", f.adminCookie)
		discordApplicationStatus(t, raw, 200)
		if strings.Contains(raw.Body.String(), before.AuthCiphertext) || strings.Contains(raw.Body.String(), "test-only-discord-client-secret") {
			t.Fatal("config response exposed secret material")
		}
	})
	t.Run("fixed_profile_manual_completion_and_replay", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
		c.callback += "&scope=identify&future_decoration=ignored&iss=untrusted-decoration&nonce=not-authority"
		f.stage(t, c)
		var staged entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&staged).Error != nil || staged.ProviderID != "discord" || staged.ProfileID != discordFixtureProfile || staged.IdentityIssuer != discordFixtureNamespace || staged.Status != "verified" || staged.SubjectKind != "string" || staged.Subject != discordMemberSubject || staged.VerifiedAt == nil || staged.ConsumedAt != nil || staged.BindingID == "" || staged.BindingCreatedAt == nil {
			t.Fatal("callback did not stage exact immutable binding")
		}
		tokens, profiles, closed := f.peer.counts()
		if tokens < 3 || profiles != tokens || closed < 2*tokens {
			t.Fatal("each staged proof must read one exact profile and close its operation transports")
		}
		var before int64
		if db.Model(&entity.Session{}).Count(&before).Error != nil {
			t.Fatal("count Sessions")
		}
		session, cookie := discordApplicationSession(t, f.finish(c, nil, ""))
		if session.User.ID != f.member.User.ID {
			t.Fatal("canonical string identity changed member")
		}
		discordAssertPrimary(t, db, cookie, session.User.ID)
		discordApplicationStatus(t, f.finish(c, nil, ""), 401)
		var after int64
		if db.Model(&entity.Session{}).Count(&after).Error != nil || after != before+1 {
			t.Fatal("replay issued another Session")
		}
		f.stage(t, c)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closed {
			t.Fatal("callback replay performed remote I/O")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		for _, private := range []string{c.cookie.Value, staged.CookieHash, staged.StateHash} {
			if strings.Contains(session.CSRFToken, private) {
				t.Fatal("public Session exposed proof")
			}
		}
	})
	t.Run("state_and_duplicate_cookie_fail_before_exchange", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
		tokens, profiles, closed := f.peer.counts()
		for _, kind := range []string{"missing_state", "duplicate_code", "code_and_error", "duplicate_decoration", "escaped_duplicate_state"} {
			callback, err := url.Parse(c.callback)
			if err != nil {
				t.Fatal("controlled callback URI")
			}
			q := callback.Query()
			switch kind {
			case "missing_state":
				q.Del("state")
			case "duplicate_code":
				q.Add("code", q.Get("code"))
			case "code_and_error":
				q.Set("error", "access_denied")
			case "duplicate_decoration":
				q.Add("scope", "identify")
				q.Add("scope", "identify")
			}

			callback.RawQuery = q.Encode()
			if kind == "escaped_duplicate_state" {
				callback.RawQuery += "&%73tate=" + q.Get("state")
			}
			discordApplicationStatus(t, f.request("GET", callback.RequestURI(), nil, "", "", c.cookie), 303)
			a, b, d := f.peer.counts()
			if a != tokens || b != profiles || d != closed {
				t.Fatal("invalid callback authority reached network", kind)
			}
		}
		target, err := url.Parse(c.callback)
		if err != nil {
			t.Fatal("controlled callback URI")
		}
		q := target.Query()
		originalState := q.Get("state")
		replacement := "a"
		if originalState[0] == 'a' {
			replacement = "b"
		}
		q.Set("state", replacement+originalState[1:])
		target.RawQuery = q.Encode()
		discordApplicationStatus(t, f.request("GET", target.RequestURI(), nil, "", "", c.cookie), 303)
		discordApplicationStatus(t, f.request("GET", c.callback, nil, "", "", c.cookie, c.cookie), 303)
		wrongCookie := *c.cookie
		replacement = "a"
		if wrongCookie.Value[0] == 'a' {
			replacement = "b"
		}
		wrongCookie.Value = replacement + wrongCookie.Value[1:]
		discordApplicationStatus(t, f.finish(discordBrowserCeremony{cookie: &wrongCookie}, nil, ""), 401)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closed {
			t.Fatal("invalid state/cookie reached token or profile endpoint")
		}
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status != "pending" || proof.VerifiedAt != nil {
			t.Fatal("invalid state/cookie staged proof")
		}
		// The exact original request remains usable; failed state cannot consume it.
		f.stage(t, c)
		session, cookie := discordApplicationSession(t, f.finish(c, nil, ""))
		discordAssertPrimary(t, db, cookie, session.User.ID)
	})
	t.Run("canonical_string_identity_and_fixed_namespace", func(t *testing.T) {
		for _, bad := range []struct {
			name      string
			value     any
			duplicate bool
		}{
			{"unknown", "303", false}, {"zero", "0", false}, {"leading_zero", "0202", false}, {"overflow", "18446744073709551616", false}, {"numeric_JSON", json.Number("202"), false}, {"duplicate_id", nil, true},
		} {
			admitted, cookie := f.login(t, discordMemberSubject)
			discordAssertPrimary(t, db, cookie, admitted.User.ID)
			var usersBefore int64
			if db.Model(&entity.User{}).Count(&usersBefore).Error != nil {
				t.Fatal("count existing members")
			}
			f.peer.mu.Lock()
			f.peer.profileID = bad.value
			f.peer.duplicateID = bad.duplicate
			f.peer.mu.Unlock()
			c := f.start(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
			tokens, profiles, _ := f.peer.counts()
			f.stage(t, c)
			discordApplicationStatus(t, f.finish(c, nil, ""), 401)
			a, b, _ := f.peer.counts()
			if a != tokens+1 || b != profiles+1 {
				t.Fatal("identity denial skipped or replayed fixed exchange", bad.name)
			}
			var proof entity.NamedIdentityCeremony
			var usersAfter int64
			if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil || db.Model(&entity.User{}).Count(&usersAfter).Error != nil || usersAfter != usersBefore {
				t.Fatal("invalid/unbound identity staged proof or provisioned member", bad.name)
			}
			f.peer.mu.Lock()
			f.peer.profileID = nil
			f.peer.duplicateID = false
			f.peer.mu.Unlock()
		}
		admitted, cookie := f.login(t, discordMemberSubject)
		row := discordAssertPrimary(t, db, cookie, admitted.User.ID)
		var binding entity.NamedIdentityBinding
		if db.Take(&binding, "id = ?", row.NamedIdentityBindingID).Error != nil || binding.IdentityIssuer != discordFixtureNamespace || binding.Subject != discordMemberSubject {
			t.Fatal("fixed resource namespace or exact subject changed")
		}
		f.peer.mu.Lock()
		unexpected := f.peer.unexpected
		f.peer.mu.Unlock()
		if unexpected != 0 {
			t.Fatal("Discord exchange fetched an unapproved endpoint")
		}
	})
	t.Run("cross_profile_primary_proofs_never_mix", func(t *testing.T) {
		admitted, cookie := f.login(t, discordMemberSubject)
		before := discordAssertPrimary(t, db, cookie, admitted.User.ID)
		githubSession, githubCookie := f.github.login(t, githubMemberSubject)
		githubRow := githubAssertPrimary(t, db, githubCookie, githubSession.User.ID)
		if before.NamedIdentityBindingID == githubRow.NamedIdentityBindingID {
			t.Fatal("two profile subjects shared a binding")
		}
		for _, bad := range []map[string]any{{"primary_method": "github"}, {"named_identity_provider_id": "github"}, {"named_identity_profile_id": githubFixtureProfile}, {"oidc_binding_id": "oib_cross"}} {
			discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Model(&entity.Session{}).Where("id = ?", before.ID).Updates(bad).Error
			}) == nil {
				t.Fatal("cross-profile or old-method proof accepted")
			}
			var retained entity.Session
			if db.Session(&gorm.Session{QueryFields: true}).Take(&retained, "id = ?", before.ID).Error != nil || !reflect.DeepEqual(retained, before) {
				t.Fatal("rejected mixed proof changed original Session")
			}
			discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		}
	})
	t.Run("management_requires_intrinsic_admin_and_registration_write", func(t *testing.T) {
		discordApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/discord", nil, "", "", f.memberCookie), 403)
		view := f.config(t)
		tokens, keys, closed := f.peer.counts()
		var permission entity.RolePermission
		if db.Where("role_id = ? AND permission = ?", "rol_admin", "registration.write").Take(&permission).Error != nil {
			t.Fatal("capture independent admin permission")
		}
		removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("remove exact test permission")
		}
		defer func() {
			if db.Create(&permission).Error != nil {
				t.Error("restore exact test permission")
			}
		}()
		discordApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/discord", nil, "", "", f.adminCookie), 403)
		discordApplicationStatus(t, f.request("POST", "/api/v1/admin/auth/discord/verify", discordIdentityInput(""), view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 403)
		a, b, d := f.peer.counts()
		if a != tokens || b != keys || d != closed {
			t.Fatal("denied verification performed remote I/O")
		}

		discordApplicationStatus(t, f.request("GET", "/api/v1/account/identity/discord", nil, "", "", f.memberCookie), 200)
	})
	t.Run("name_only_review_preserves_primary_and_staged_proof", func(t *testing.T) {
		session, cookie := f.login(t, discordMemberSubject)
		before := discordAssertPrimary(t, db, cookie, session.User.ID)
		c := f.begin(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
		view := f.config(t)
		saved := discordApplicationDecode[service.DiscordProviderView](t, f.request("PUT", "/api/v1/admin/auth/discord", service.DiscordProviderInput{Name: "Renamed configured identity", ClientID: view.ClientID, CallbackURL: view.CallbackURL, SecretAction: "keep", Reason: "Change display name only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if !saved.Enabled || !saved.Verified || saved.ReviewETag == view.ReviewETag {
			t.Fatal("name-only change altered security or omitted review revision")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		after := discordAssertPrimary(t, db, cookie, session.User.ID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("name edit rewrote Session")
		}
		admitted, admittedCookie := discordApplicationSession(t, f.finish(c, nil, ""))
		discordAssertPrimary(t, db, admittedCookie, admitted.User.ID)
	})
	t.Run("cancelled_callback_never_stages_proof", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
		hold := f.peer.holdToken(t)
		bound, boundCancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer boundCancel()
		requestCtx, requestCancel := context.WithCancel(bound)
		defer requestCancel()
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			requestCancel()
			if !joined {
				select {
				case <-done:
				case <-bound.Done():
					t.Error("cancelled callback failed original join bound")
				}
			}
		}()
		go func() {
			done <- discordApplicationRequest(requestCtx, f.router, "GET", c.callback, nil, "", "", c.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("cancel control did not reach held token")
		case <-bound.Done():
			t.Fatal("cancel admission exceeded original bound")
		}
		requestCancel()
		hold.release()
		select {
		case r := <-done:
			joined = true
			discordApplicationStatus(t, r, 303)
		case <-bound.Done():
			t.Fatal("cancelled callback failed to join")
		}
		if bound.Err() != nil {
			t.Fatal("late cancelled callback join")
		}
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil {
			t.Fatal("cancelled callback staged usable proof")
		}
		discordApplicationStatus(t, f.finish(c, nil, ""), 401)
	})
	t.Run("held_verification_rechecks_original_session_before_staging", func(t *testing.T) {
		original, originalCookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: discordFixturePassword}, "", ""))
		view := discordApplicationDecode[service.DiscordProviderView](t, f.request("GET", "/api/v1/admin/auth/discord", nil, "", "", originalCookie), 200)
		c := f.start(t, "/api/v1/admin/auth/discord/verify", discordAdminSubject, originalCookie, original.CSRFToken, view.ReviewETag, "")
		hold := f.peer.holdToken(t)
		operation, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			if !joined {
				select {
				case <-done:
				case <-operation.Done():
					t.Error("held verification did not join within original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- discordApplicationRequest(operation, f.router, "GET", c.callback, nil, "", "", c.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("verification failed before real held token request")
		case <-operation.Done():
			t.Fatal("verification hold timed out")
		}
		var row entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(originalCookie.Value)).Take(&row).Error != nil {
			t.Fatal("read exact originating Session")
		}
		discordApplicationStatus(t, f.request("DELETE", "/api/v1/account/sessions/"+row.ID, nil, "", f.admin.CSRFToken, f.adminCookie), 204)
		hold.release()
		select {
		case r := <-done:
			joined = true
			discordApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held verification exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late verification completion")
		}
		discordApplicationStatus(t, f.finish(c, originalCookie, original.CSRFToken), 401)
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil {
			t.Fatal("revoked originating Session staged a usable verification")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
	})
	t.Run("held_binding_rejects_security_reconfiguration_without_cross_profile_revocation", func(t *testing.T) {
		_, githubCookie := f.github.login(t, githubMemberSubject)
		var githubBefore entity.NamedIdentityProvider
		var githubBinding entity.NamedIdentityBinding
		if db.Take(&githubBefore, "id = ?", "github").Error != nil || db.Where("provider_id = ? AND user_id = ?", "github", f.member.User.ID).Take(&githubBinding).Error != nil {
			t.Fatal("capture independent GitHub configuration")
		}
		account := f.account(t, f.memberCookie)
		c := f.start(t, "/api/v1/account/identity/discord/bind", discordMemberSubject, f.memberCookie, f.member.CSRFToken, account.ReviewETag, "")
		hold := f.peer.holdToken(t)
		operation, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			if !joined {
				select {
				case <-done:
				case <-operation.Done():
					t.Error("held binding did not join original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- discordApplicationRequest(operation, f.router, "GET", c.callback, nil, "", "", c.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("binding did not enter controlled token exchange")
		case <-operation.Done():
			t.Fatal("binding hold timed out")
		}
		before := f.config(t)
		changed := discordApplicationDecode[service.DiscordProviderView](t, f.request("PUT", "/api/v1/admin/auth/discord", map[string]any{"name": "Controlled Discord", "client_id": "18446744073709551615", "callback_url": "https://routex.test/api/v1/auth/discord/callback", "secret_action": "replace", "client_secret": "test-only-discord-client-secret", "reason": "Replace exact Discord secret generation during held binding"}, before.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if changed.Enabled || changed.Verified {
			t.Fatal("security replacement preserved enablement/verification")
		}
		hold.release()
		select {
		case r := <-done:
			joined = true
			discordApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held binding exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late held binding completion")
		}
		discordApplicationStatus(t, f.finish(c, f.memberCookie, f.member.CSRFToken), 401)
		var proof entity.NamedIdentityCeremony
		e := db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error
		if e != gorm.ErrRecordNotFound && (e != nil || proof.Status == "verified" || proof.VerifiedAt != nil) {
			t.Fatal("obsolete security generation staged usable proof")
		}
		var currentGitHub entity.NamedIdentityProvider
		var currentBinding entity.NamedIdentityBinding
		if db.Take(&currentGitHub, "id = ?", "github").Error != nil || db.Take(&currentBinding, "id = ?", githubBinding.ID).Error != nil || !reflect.DeepEqual(currentGitHub, githubBefore) || !reflect.DeepEqual(currentBinding, githubBinding) {
			t.Fatal("Discord security change rewrote independent GitHub identity")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		// Re-establish Discord only through genuine verification, explicit enable and self binding.
		verification := f.begin(t, "/api/v1/admin/auth/discord/verify", discordAdminSubject, f.adminCookie, f.admin.CSRFToken, changed.ReviewETag, "")
		discordApplicationStatus(t, f.finish(verification, f.adminCookie, f.admin.CSRFToken), 200)
		current := f.config(t)
		discordApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/discord/status", map[string]any{"enabled": true, "reason": "Explicitly restore newly verified Discord generation"}, current.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		account = f.account(t, f.memberCookie)
		bound := f.begin(t, "/api/v1/account/identity/discord/bind", discordMemberSubject, f.memberCookie, f.member.CSRFToken, account.ReviewETag, "")
		discordApplicationStatus(t, f.finish(bound, f.memberCookie, f.member.CSRFToken), 200)
	})
	t.Run("planned_departure_revokes_native_mfa_and_held_callback", func(t *testing.T) {
		var admin entity.User
		if db.Take(&admin, "id = ?", f.admin.User.ID).Error != nil {
			t.Fatal("read password baseline")
		}
		member := entity.User{ID: "usr_discord_departing_member", Email: "discord-departing@example.invalid", Name: "Departing identity member", Role: entity.RoleMember, PasswordHash: admin.PasswordHash}
		if db.Create(&member).Error != nil || db.Take(&member, "id = ?", member.ID).Error != nil {
			t.Fatal("create existing departure member")
		}
		local, localCookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: discordFixturePassword}, "", ""))
		account := f.account(t, localCookie)
		binding := f.begin(t, "/api/v1/account/identity/discord/bind", "303", localCookie, local.CSRFToken, account.ReviewETag, "")
		bound := discordApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, f.finish(binding, localCookie, local.CSRFToken), 200)
		if bound.Kind != "bound" {
			t.Fatal("departure requires genuine explicit binding")
		}
		local, localCookie, recovery := discordEnableMFA(t, f, local, localCookie)
		factor := discordChallenge(t, f, "303")
		admitted, discordCookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		discordAssertPrimary(t, db, discordCookie, admitted.User.ID)
		pending := discordChallenge(t, f, "303")
		staged := f.begin(t, "/api/v1/auth/discord/start", "303", nil, "", "", "")
		held := f.start(t, "/api/v1/auth/discord/start", "303", nil, "", "", "")
		hold := f.peer.holdToken(t)
		operation, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			if !joined {
				select {
				case <-done:
				case <-operation.Done():
					t.Error("held callback did not join within original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- discordApplicationRequest(operation, f.router, "GET", held.callback, nil, "", "", held.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("callback did not reach held real token exchange")
		case <-operation.Done():
			t.Fatal("held callback admission timed out")
		}
		var exchanging entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(held.cookie.Value)).Take(&exchanging).Error != nil || exchanging.Status != "exchanging" || exchanging.VerifiedAt != nil {
			t.Fatal("remote I/O did not follow durable exclusive claim")
		}
		var providerBefore entity.NamedIdentityProvider
		var bindingsBefore []entity.NamedIdentityBinding
		var recoveryBefore []entity.MFARecoveryCode
		if db.Take(&providerBefore, "id = ?", "discord").Error != nil || db.Order("id").Find(&bindingsBefore).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryBefore).Error != nil {
			t.Fatal("capture exact retained departure facts")
		}
		request := func(method, path string, body any, cookie *http.Cookie, csrf, etag string, extra *http.Cookie) *httptest.ResponseRecorder {
			return f.request(method, path, body, etag, csrf, cookie, extra)
		}
		assertRetained := exerciseEnterpriseOffboarding(t, f.ctx, db, f.service, "discord", member.ID, f.admin, f.adminCookie, request)
		hold.release()
		select {
		case r := <-done:
			joined = true
			discordApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held callback exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late callback cannot qualify positive closure")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", discordCookie), 401)
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", localCookie), 401)
		discordApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		for _, c := range []discordBrowserCeremony{staged, held} {
			discordApplicationStatus(t, f.finish(c, nil, ""), 401)
			discordApplicationStatus(t, f.finish(c, nil, ""), 401)
		}
		tokens, profiles, closes := f.peer.counts()
		f.stage(t, held)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closes {
			t.Fatal("departed callback replay performed I/O")
		}
		var providerAfter entity.NamedIdentityProvider
		var bindingsAfter []entity.NamedIdentityBinding
		var recoveryAfter []entity.MFARecoveryCode
		if db.Take(&providerAfter, "id = ?", "discord").Error != nil || db.Order("id").Find(&bindingsAfter).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryAfter).Error != nil || !reflect.DeepEqual(providerBefore, providerAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) || !reflect.DeepEqual(recoveryBefore, recoveryAfter) {
			t.Fatal("late proof changed retained identity/recovery facts")
		}
		assertRetained()
	})
	t.Run("native_mfa_and_disable_revalidate_exact_primary", func(t *testing.T) {
		var recovery []string
		f.member, f.memberCookie, recovery = discordEnableMFA(t, f, f.member, f.memberCookie)
		githubFactor := githubChallenge(t, f.github, githubMemberSubject)
		githubSession, githubCookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: githubFactor.ChallengeToken, RecoveryCode: recovery[3]}, "", ""))
		githubAssertPrimary(t, db, githubCookie, githubSession.User.ID)
		factor := discordChallenge(t, f, discordMemberSubject)
		session, cookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		discordAssertPrimary(t, db, cookie, session.User.ID)
		pending := discordChallenge(t, f, discordMemberSubject)
		staged := f.begin(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
		view := f.config(t)
		discordApplicationDecode[service.DiscordProviderView](t, f.request("PUT", "/api/v1/admin/auth/discord/status", service.DiscordStatusInput{Enabled: false, Reason: "Revoke this named identity only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 401)
		discordApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		discordApplicationStatus(t, f.finish(staged, nil, ""), 401)
		var count int64
		if db.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NULL", f.member.User.ID).Count(&count).Error != nil || count != 8 {
			t.Fatal("rejected primary consumed recovery proof")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		account := f.account(t, f.memberCookie)
		if !account.Bound || account.Available || !account.MFARequired {
			t.Fatal("disabled provider fabricated binding/MFA facts")
		}
		unlinked := discordApplicationDecode[service.DiscordAccountView](t, f.request("POST", "/api/v1/account/identity/discord/unlink", discordIdentityInput(recovery[2]), account.ReviewETag, f.member.CSRFToken, f.memberCookie), 200)
		if unlinked.Bound {
			t.Fatal("explicit unlink retained binding")
		}
		discordApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
	})
}
