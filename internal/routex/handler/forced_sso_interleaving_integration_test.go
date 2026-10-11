package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The fixture dispatches through either the real HTTP process or the normal
// handler stack. Both paths use the same genuine TLS identity/factor workflow.
type forcedSSOAcceptance struct {
	t           *testing.T
	ctx         context.Context
	db          *gorm.DB
	request     func(context.Context, string, string, any, *http.Cookie, string, string, *http.Cookie) *httptest.ResponseRecorder
	provider    *oidcTLSProvider
	directory   *ldapApplicationPeer
	admin       SessionResponse
	localCookie *http.Cookie
	ldap        SessionResponse
	ldapCookie  *http.Cookie
	oidc        SessionResponse
	oidcCookie  *http.Cookie
	recovery    []string
}

const forcedSSOPassword = "forced-sso-process-test-password"

func (f *forcedSSOAcceptance) call(method, path string, body any, cookie *http.Cookie, csrf, tag string, ceremony *http.Cookie) *httptest.ResponseRecorder {
	return f.request(f.ctx, method, path, body, cookie, csrf, tag, ceremony)
}
func (f *forcedSSOAcceptance) status(r *httptest.ResponseRecorder, want int) {
	f.t.Helper()
	if r.Code != want {
		f.t.Fatalf("forced SSO HTTP status=%d want=%d", r.Code, want)
	}
	if !strings.Contains(r.Header().Get("Cache-Control"), "no-store") || r.Header().Get("X-Content-Type-Options") != "nosniff" {
		f.t.Fatal("forced SSO private headers absent")
	}
}
func (f *forcedSSOAcceptance) session(r *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
	f.t.Helper()
	var v SessionResponse
	if json.Unmarshal(r.Body.Bytes(), &v) != nil || v.User.ID == "" || v.CSRFToken == "" {
		f.t.Fatal("safe Session absent")
	}
	for _, c := range r.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge > 0 {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				f.t.Fatal("Session cookie authority")
			}
			return v, c
		}
	}
	f.t.Fatal("Session cookie missing")
	return v, nil
}
func (f *forcedSSOAcceptance) policy(cookie *http.Cookie, candidate string) string {
	f.t.Helper()
	path := "/api/v1/admin/auth/policy"
	if candidate != "" {
		path += "?emergency_admin_id=" + candidate
	}
	r := f.call("GET", path, nil, cookie, "", "", nil)
	f.status(r, 200)
	var v service.AuthenticationPolicyView
	if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ReviewETag) || candidate == "" && v.ReviewEmergencyAdmin != nil || candidate != "" && (v.ReviewEmergencyAdmin == nil || v.ReviewEmergencyAdmin.ID != candidate) {
		f.t.Fatal("exact policy review absent")
	}
	return r.Header().Get("ETag")
}
func (f *forcedSSOAcceptance) save(cookie *http.Cookie, csrf string, on bool) service.AuthenticationPolicyResult {
	f.t.Helper()
	candidate := ""
	if on {
		candidate = f.admin.User.ID
	}
	tag := f.policy(cookie, candidate)
	r := f.call("PUT", "/api/v1/admin/auth/policy", service.AuthenticationPolicyInput{Enforced: on, EmergencyAdminID: candidate, Reason: "Review exact policy generation"}, cookie, csrf, tag, nil)
	f.status(r, 200)
	var out service.AuthenticationPolicyResult
	if json.Unmarshal(r.Body.Bytes(), &out) != nil || out.Confirmation != "saved" || out.Enforced != on || r.Header().Get("ETag") != fmt.Sprintf("%q", out.ReviewETag) {
		f.t.Fatal("saved policy receipt absent")
	}
	return out
}
func (f *forcedSSOAcceptance) check(cookie *http.Cookie, status int) {
	f.t.Helper()
	f.status(f.call("GET", "/api/v1/auth/session", nil, cookie, "", "", nil), status)
}
func (f *forcedSSOAcceptance) oidcStart(route string, cookie *http.Cookie, csrf, tag string, input any) (*http.Cookie, string) {
	f.t.Helper()
	r := f.call("POST", route, input, cookie, csrf, tag, nil)
	f.status(r, 200)
	var out service.OIDCStart
	if json.Unmarshal(r.Body.Bytes(), &out) != nil {
		f.t.Fatal("OIDC authorization")
	}
	var correlation *http.Cookie
	for _, c := range r.Result().Cookies() {
		if c.Name == "routex_oidc" {
			correlation = c
		}
	}
	if correlation == nil || !correlation.HttpOnly || correlation.SameSite != http.SameSiteLaxMode || correlation.Path != "/api/v1/auth/oidc" {
		f.t.Fatal("OIDC correlation")
	}
	u, e := url.Parse(out.AuthorizationURL)
	if e != nil || u.Scheme != "https" || u.Host != strings.TrimPrefix(f.provider.server.URL, "https://") {
		f.t.Fatal("controlled authorization origin")
	}
	req, e := http.NewRequestWithContext(f.ctx, "GET", out.AuthorizationURL, nil)
	if e != nil {
		f.t.Fatal(e)
	}
	req.Header.Set("X-Test-Subject", "admin-subject")
	browser := f.provider.server.Client()
	browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := browser.Do(req)
	if e != nil {
		f.t.Fatal("controlled authorization TLS")
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4097))
	closeErr := response.Body.Close()
	callback, e := url.Parse(response.Header.Get("Location"))
	if readErr != nil || closeErr != nil || response.StatusCode != 302 || e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/oidc/callback" {
		f.t.Fatal("controlled callback")
	}
	return correlation, callback.RequestURI()
}
func (f *forcedSSOAcceptance) callback(c *http.Cookie, path string) {
	f.t.Helper()
	before := f.provider.tokenCount()
	r := f.call("GET", path, nil, nil, "", "", c)
	f.status(r, 303)
	if r.Header().Get("Location") != "/auth/oidc/complete" || r.Header().Get("Referrer-Policy") != "no-referrer" || f.provider.tokenCount() != before+1 {
		f.t.Fatal("OIDC callback proof/clean boundary")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie {
			f.t.Fatal("callback minted Session")
		}
	}
}
func (f *forcedSSOAcceptance) oidcComplete(c *http.Cookie, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	return f.call("POST", "/api/v1/auth/oidc/complete", map[string]any{}, cookie, csrf, "", c)
}
func (f *forcedSSOAcceptance) factor(r *httptest.ResponseRecorder, index int) (SessionResponse, *http.Cookie) {
	f.t.Helper()
	f.status(r, 202)
	var c service.MFALoginChallenge
	if json.Unmarshal(r.Body.Bytes(), &c) != nil || !c.MFARequired || c.ChallengeToken == "" {
		f.t.Fatal("native pending factor")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie {
			f.t.Fatal("factor202 Session")
		}
	}
	r = f.call("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: c.ChallengeToken, RecoveryCode: f.recovery[index]}, nil, "", "", nil)
	f.status(r, 200)
	return f.session(r)
}
func (f *forcedSSOAcceptance) emergency(index int) (SessionResponse, *http.Cookie) {
	f.t.Helper()
	r := f.call("POST", "/api/v1/auth/emergency/login", service.EmergencyLoginInput{Email: f.admin.User.Email, Password: forcedSSOPassword}, nil, "", "", nil)
	f.status(r, 202)
	var c service.MFALoginChallenge
	if json.Unmarshal(r.Body.Bytes(), &c) != nil || !c.MFARequired {
		f.t.Fatal("emergency native challenge")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie {
			f.t.Fatal("password-only emergency Session")
		}
	}
	r = f.call("POST", "/api/v1/auth/emergency/mfa/verify", map[string]string{"challenge_token": c.ChallengeToken, "recovery_code": f.recovery[index]}, nil, "", "", nil)
	f.status(r, 200)
	return f.session(r)
}
func (f *forcedSSOAcceptance) configure() {
	f.t.Helper()
	r := f.call("POST", "/api/v1/setup", SetupRequest{Email: "forced-process@example.invalid", Name: "Forced policy administrator", Password: forcedSSOPassword}, nil, "", "", nil)
	f.status(r, 201)
	f.admin, f.localCookie = f.session(r)
	review := func(method string) string {
		r := f.call("GET", "/api/v1/admin/auth/"+method, nil, f.localCookie, "", "", nil)
		f.status(r, 200)
		return r.Header().Get("ETag")
	}
	r = f.call("PUT", "/api/v1/admin/auth/ldap", service.LDAPProviderInput{Name: "Controlled directory", Endpoint: f.directory.endpoint, BindDN: "cn=service,dc=example", BaseDN: "dc=example", UserFilter: "(uid={username})", IdentityAttribute: "entryUUID", SecretAction: "replace", BindPassword: "service-password", Reason: "Configure exact directory"}, f.localCookie, f.admin.CSRFToken, review("ldap"), nil)
	f.status(r, 200)
	r = f.call("POST", "/api/v1/admin/auth/ldap/verify", service.LDAPBindingInput{Password: forcedSSOPassword, Proof: service.LDAPProof{}, Reason: "Explicit existing member link", Username: "admin", DirectoryPassword: "directory-password"}, f.localCookie, f.admin.CSRFToken, review("ldap"), nil)
	f.status(r, 200)
	f.status(f.call("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: true, Reason: "Enable verified directory"}, f.localCookie, f.admin.CSRFToken, review("ldap"), nil), 200)
	f.status(f.call("PUT", "/api/v1/admin/auth/oidc", service.OIDCProviderInput{Name: "Controlled identity", Issuer: f.provider.server.URL, ClientID: "routex-test-client", CallbackURL: "https://routex.test/api/v1/auth/oidc/callback", SecretAction: "replace", ClientSecret: "test-only-client-secret", Reason: "Configure exact identity"}, f.localCookie, f.admin.CSRFToken, review("oidc"), nil), 200)
	c, callback := f.oidcStart("/api/v1/admin/auth/oidc/verify", f.localCookie, f.admin.CSRFToken, review("oidc"), service.OIDCIdentityInput{Password: forcedSSOPassword, Proof: service.OIDCProof{}, Reason: "Explicit existing administrator identity"})
	f.callback(c, callback)
	f.status(f.oidcComplete(c, f.localCookie, f.admin.CSRFToken), 200)
	f.status(f.call("PUT", "/api/v1/admin/auth/oidc/status", service.OIDCStatusInput{Enabled: true, Reason: "Enable verified identity"}, f.localCookie, f.admin.CSRFToken, review("oidc"), nil), 200)
	r = f.call("POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: forcedSSOPassword}, f.localCookie, f.admin.CSRFToken, "", nil)
	f.status(r, 200)
	var e service.MFAEnrollment
	if json.Unmarshal(r.Body.Bytes(), &e) != nil {
		f.t.Fatal("native enrollment")
	}
	r = f.call("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: forcedSSOPassword, EnrollmentToken: e.EnrollmentToken, Code: mfaFixtureCode(f.t, e.Secret, 0)}, f.localCookie, f.admin.CSRFToken, "", nil)
	f.status(r, 200)
	var recovery MFARecoveryResponse
	if json.Unmarshal(r.Body.Bytes(), &recovery) != nil || recovery.Session == nil || len(recovery.RecoveryCodes) != 10 {
		f.t.Fatal("native MFA recovery")
	}
	f.recovery = recovery.RecoveryCodes
	f.admin = *recovery.Session
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.MaxAge > 0 {
			f.localCookie = cookie
		}
	}
	f.ldap, f.ldapCookie = f.factor(f.call("POST", "/api/v1/auth/ldap/login", service.LDAPLoginInput{Username: "admin", Password: "directory-password"}, nil, "", "", nil), 0)
	c, callback = f.oidcStart("/api/v1/auth/oidc/start", nil, "", "", map[string]any{})
	f.callback(c, callback)
	f.oidc, f.oidcCookie = f.factor(f.oidcComplete(c, nil, ""), 1)
	for _, check := range []struct {
		cookie *http.Cookie
		method string
	}{{f.ldapCookie, "ldap"}, {f.oidcCookie, "oidc"}} {
		var row entity.Session
		if f.db.Where("token_hash = ?", secret.SHA256Hex(check.cookie.Value)).Take(&row).Error != nil || row.PrimaryMethod != check.method {
			f.t.Fatal("genuine allowed primary absent")
		}
	}
}

func forcedSSOHandlerRequests(router http.Handler) func(context.Context, string, string, any, *http.Cookie, string, string, *http.Cookie) *httptest.ResponseRecorder {
	return func(ctx context.Context, method, path string, body any, cookie *http.Cookie, csrf, tag string, ceremony *http.Cookie) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		if body == nil {
			raw = nil
		}
		r := httptest.NewRequest(method, "https://routex.test"+path, bytes.NewReader(raw)).WithContext(ctx)
		r.Header.Set("Origin", "https://routex.test")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if ceremony != nil {
			r.AddCookie(ceremony)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if tag != "" {
			r.Header.Set("If-Match", tag)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, r)
		return out
	}
}

func forcedSSONewLDAPPeer(t *testing.T, ctx context.Context, dir string) *ldapApplicationPeer {
	t.Helper()
	cert, e := tls.LoadX509KeyPair(filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem"))
	if e != nil {
		t.Fatal("TLS material")
	}
	l, e := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if e != nil {
		t.Fatal("controlled listener")
	}
	p := &ldapApplicationPeer{listener: l, endpoint: "ldaps://" + l.Addr().String(), ctx: ctx, connections: map[net.Conn]bool{}}
	p.wg.Go(func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			p.connections[c] = true
			p.mu.Unlock()
			p.wg.Go(func() {
				defer func() { _ = c.Close(); p.mu.Lock(); delete(p.connections, c); p.mu.Unlock() }()
				_ = c.SetDeadline(time.Now().Add(15 * time.Second))
				p.serve(c)
			})
		}
	})
	return p
}

func testForcedSSOInterleavings(t *testing.T, db *gorm.DB) {
	t.Helper()
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("forced SSO lifecycle requires the selected matrix database")
	}
	dir := t.TempDir()
	oidcWriteTLSMaterial(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate forced SSO child")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestForcedSSOInterleavingTLSChild$", "-test.timeout=120s", "-test.count=1")
	command.WaitDelay = 5 * time.Second
	// Retain normal runtime/driver settings, but replace every trust/child variable
	// explicitly. Go 1.27 honors these paths on Darwin as well as Unix.
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_FORCED_SSO_INTERLEAVING_CHILD": true, "ROUTEX_LDAP_TLS_DIR": true, "ROUTEX_LDAP_TLS_DRIVER": true}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !blocked[key] {
			command.Env = append(command.Env, value)
		}
	}
	debug := os.Getenv("GODEBUG")
	var settings []string
	for _, value := range strings.Split(debug, ",") {
		if value != "" && !strings.HasPrefix(value, "x509sslcertoverrideplatform=") {
			settings = append(settings, value)
		}
	}
	settings = append(settings, "x509sslcertoverrideplatform=1")
	command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(settings, ","), "ROUTEX_FORCED_SSO_INTERLEAVING_CHILD=1", "ROUTEX_LDAP_TLS_DIR="+dir, "ROUTEX_LDAP_TLS_DRIVER="+driver)
	// Child output can include framework diagnostics. Keep it private, never
	// forward authentication URLs, cookies or proof material to matrix output.
	output, err := os.OpenFile(filepath.Join(dir, "child.private.log"), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("create private child log")
	}
	command.Stdout, command.Stderr = output, output
	runErr := command.Run()
	closeErr := output.Close()
	var report struct {
		Stage  string
		Failed bool
	}
	raw, readErr := os.ReadFile(filepath.Join(dir, "result.json"))
	if readErr != nil || json.Unmarshal(raw, &report) != nil {
		t.Fatal("forced SSO child did not produce its terminal report")
	}
	if runErr != nil || closeErr != nil || report.Failed {
		t.Fatalf("forced SSO child failed at bounded stage %q", report.Stage)
	}
	if report.Stage != "complete" {
		t.Fatal("forced SSO child did not complete all stages")
	}
}

func TestForcedSSOInterleavingTLSChild(t *testing.T) {
	if os.Getenv("ROUTEX_FORCED_SSO_INTERLEAVING_CHILD") != "1" {
		t.Skip("launched only by the registered forced-SSO interleaving scenario")
	}
	stage := "database"
	defer func() {
		raw, _ := json.Marshal(struct {
			Stage  string
			Failed bool
		}{stage, t.Failed()})
		if os.WriteFile(filepath.Join(os.Getenv("ROUTEX_LDAP_TLS_DIR"), "result.json"), raw, 0600) != nil {
			t.Error("write private terminal report")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	// Failure-path teardown is admitted only after the dispatched application
	// request positively joins. Cancellation alone is never closure evidence.
	applicationJoined := true
	driver := os.Getenv("ROUTEX_LDAP_TLS_DRIVER")
	env := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if env == "" {
		t.Fatal("selected driver absent")
	}
	db, e := database.Open(ctx, driver, os.Getenv(env))
	if e != nil {
		t.Fatal("open isolated database")
	}
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	pool, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if !applicationJoined {
			t.Error("database teardown withheld: application join unproven")
			return
		}
		if pool.Close() != nil {
			t.Error("close database")
		}
	}()
	var name string
	q := "SELECT current_database()"
	if driver == "mysql" {
		q = "SELECT DATABASE()"
	}
	if db.Raw(q).Scan(&name).Error != nil || name != "routex_test" {
		t.Fatal("dedicated database required")
	}
	dir := os.Getenv("ROUTEX_LDAP_TLS_DIR")
	t.Setenv("ROUTEX_OIDC_TLS_DIR", dir)
	provider := oidcNewTLSProvider(t)
	defer func() {
		if !applicationJoined {
			t.Error("identity peer teardown withheld: application join unproven")
			return
		}
		provider.server.Close()
	}()
	directory := forcedSSONewLDAPPeer(t, ctx, dir)
	defer func() {
		if !applicationJoined {
			t.Error("directory teardown withheld: application join unproven")
			return
		}
		directory.close(t)
	}()
	store, e := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if e != nil {
		t.Fatal(e)
	}
	// Install the immutable callback registry before any runtime or heartbeat starts.
	const faultName = "forced_sso_before_authorization_publication"
	var failAuthorization atomic.Bool
	var failedAuthorizationLoads atomic.Int64
	if e := db.Callback().Query().Before("gorm:query").Register(faultName, func(tx *gorm.DB) {
		// Only the runtime loader's complete binding slice is intercepted. Policy
		// review/save, Session checks and held OIDC transactions remain genuine.
		_, runtimeBindings := tx.Statement.Dest.(*[]entity.ModelProviderBinding)
		if failAuthorization.Load() && runtimeBindings && tx.Statement.Table == "model_provider_bindings" {
			failedAuthorizationLoads.Add(1)
			_ = tx.AddError(errors.New("controlled forced SSO authorization load failure"))
		}
	}); e != nil {
		t.Fatal("register exact authorization loader fault")
	}
	defer func() { failAuthorization.Store(false) }()
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true), service.WithRuntimeRefreshInterval(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	f := &forcedSSOAcceptance{t: t, ctx: ctx, db: db, request: forcedSSOHandlerRequests(router), provider: provider, directory: directory}
	stage = "genuine_identities_and_native_factor"
	f.configure()
	var nativeDispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer forced-controlled" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected native upstream contract")
			w.WriteHeader(400)
			return
		}
		nativeDispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"forced-native","object":"chat.completion","model":"forced-upstream","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer func() {
		if !applicationJoined {
			t.Error("native peer teardown withheld: application join unproven")
			return
		}
		upstream.Close()
	}()
	fresh := func(prefix string) string {
		t.Helper()
		v, e := id.NewPrefixed(prefix)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	prv, con, crd, pm, model, bnd, team := fresh("prv"), fresh("con"), fresh("crd"), fresh("pmd"), fresh("mdl"), fresh("bnd"), fresh("tem")
	birth := time.Now().UTC().Truncate(time.Microsecond)
	cipher, e := store.Seal(crd, "forced-controlled")
	if e != nil {
		t.Fatal(e)
	}
	// These are native routing/Team relationships only. Identity Sessions/bindings,
	// MFA proofs and policy state above were issued through genuine HTTP workflows.
	for _, row := range []any{
		&entity.Provider{ID: prv, Name: "Forced native", Enabled: true, ETag: "0", CreatedAt: birth},
		&entity.ProviderConnection{ID: con, ProviderID: prv, Name: "Controlled", BaseURL: upstream.URL + "/v1", Protocol: entity.ProtocolOpenAIChat, Adapter: "native", Enabled: true, TransportGeneration: "0", CreatedAt: birth},
		&entity.ProviderCredential{ID: crd, ConnectionID: con, Name: "Controlled", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified", VerifiedTransportGeneration: "0", CreatedAt: birth},
		&entity.ProviderModel{ID: pm, ConnectionID: con, UpstreamName: "forced-upstream", CapabilityTransportGeneration: "0", CreatedAt: birth},
		&entity.CredentialModelAccess{CredentialID: crd, ProviderModelID: pm},
		&entity.Model{ID: model, Status: entity.ResourceActive, CreatedAt: birth},
		&entity.ModelName{Name: "forced-model", ModelID: model, CurrentModelID: &model, CreatedAt: birth},
		&entity.ModelProviderBinding{ID: bnd, ModelID: model, ProviderModelID: pm, Weight: 100, CreatedAt: birth},
		&entity.Team{ID: team, Name: "Forced Team", Status: entity.ResourceActive},
		&entity.TeamMembership{ID: fresh("tmm"), TeamID: team, UserID: f.admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive},
		&entity.TeamModelGrant{TeamID: team, ModelID: model},
	} {
		if db.Create(row).Error != nil {
			t.Fatal("native relationship fixture")
		}
	}
	if e = svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Forced policy", Hostname: "forced.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test", StoragePath: t.TempDir()}); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if !applicationJoined {
			t.Error("runtime teardown withheld: application join unproven")
			return
		}
		svc.StopRuntime()
		if svc.StopCallRecorder() != nil {
			t.Error("stop recorder")
		}
		stop, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if svc.StopSystemInstance(stop) != nil {
			t.Error("stop owned system instance")
		}
	}()
	if svc.StartRuntime(ctx) != nil || svc.StartCallRecorder(ctx, filepath.Join(t.TempDir(), "forced-calls.db")) != nil {
		t.Fatal("start actual native runtime")
	}
	localIdentity, e := svc.RuntimeAuthenticateTeamSession(ctx, f.localCookie.Value, team)
	if e != nil || svc.ReauthorizeTeamSession(ctx, localIdentity, model) != nil {
		t.Fatal("actual local Team admission prerequisite")
	}
	oidcIdentity, e := svc.RuntimeAuthenticateTeamSession(ctx, f.oidcCookie.Value, team)
	if e != nil || svc.ReauthorizeTeamSession(ctx, oidcIdentity, model) != nil {
		t.Fatal("actual OIDC Team admission prerequisite")
	}
	ldapIdentity, e := svc.RuntimeAuthenticateTeamSession(ctx, f.ldapCookie.Value, team)
	if e != nil || svc.ReauthorizeTeamSession(ctx, ldapIdentity, model) != nil {
		t.Fatal("actual LDAP Team admission prerequisite")
	}
	native := func(cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		return f.call("POST", "/api/v1/teams/"+team+"/chat/completions", map[string]any{"model": "forced-model", "messages": []any{map[string]string{"role": "user", "content": "controlled"}}}, cookie, csrf, "", nil)
	}
	expectStatus(t, native(f.localCookie, f.admin.CSRFToken), 200)
	if nativeDispatches.Load() != 1 {
		t.Fatal("genuine native completion prerequisite absent")
	}
	pending := f.call("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: forcedSSOPassword}, nil, "", "", nil)
	f.status(pending, 202)
	var localFactor service.MFALoginChallenge
	if json.Unmarshal(pending.Body.Bytes(), &localFactor) != nil {
		t.Fatal("pending local native factor")
	}
	// OIDC is intentionally an allowed profile. A response held over the policy
	// cutoff must retain verified provenance and still require its native factor.
	stage = "held_oidc_callback_and_failed_publication"
	correlation, path := f.oidcStart("/api/v1/auth/oidc/start", nil, "", "", map[string]any{})
	hold := provider.holdNextToken(t)
	operation, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	done := make(chan *httptest.ResponseRecorder, 1)
	joined := make(chan struct{})
	defer func() {
		hold.release()
		stop()
		// Prefer already-proven closure even if the child/operation has expired.
		select {
		case <-joined:
			applicationJoined = true
			return
		default:
		}
		// This independent cleanup allowance joins cancelled work only. It does
		// not renew the original callback10s or local admission5s operation budget.
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		select {
		case <-joined:
			applicationJoined = true
		case <-cleanup.Done():
			t.Error("held application request cleanup join unproven")
		}
	}()
	beforeTokens := provider.tokenCount()
	applicationJoined = false
	go func() { defer close(joined); done <- f.request(operation, "GET", path, nil, nil, "", "", correlation) }()
	select {
	case <-hold.entered:
	case <-joined:
		t.Fatal("callback returned before token hold")
	case <-operation.Done():
		t.Fatal("token hold admission bound")
	}
	var ceremony entity.OIDCCeremony
	if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Status != "exchanging" || ceremony.Subject != "" || ceremony.VerifiedAt != nil {
		t.Fatal("atomic callback claim before token I/O absent")
	}
	// Keep the original invalid-route witness: that failure occurs AFTER auth
	// publication. A separate exact loader fault below must fail BEFORE auth.Store.
	changed := db.Model(&entity.ModelProviderBinding{}).Where("id = ? AND weight = ?", bnd, 100).Update("weight", 50)
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("controlled route-publication failure")
	}
	// Freshen the genuine old authorization lease immediately before cutoff.
	// The invalid route prevents route replacement, but refresh still installs
	// authorization. All three proofs must be live before inducing DB failure.
	if svc.RefreshRuntime(ctx) == nil {
		t.Fatal("invalid route unexpectedly published")
	}
	oldStatus := svc.RuntimeStatus()
	if !oldStatus.Ready || oldStatus.AuthorizationValidUntil == nil || !time.Now().Before(*oldStatus.AuthorizationValidUntil) || oldStatus.SnapshotID == "" || oldStatus.ErrorCode != "invalid_configuration" {
		t.Fatal("fresh old authorization lease absent")
	}
	localIdentity, e = svc.RuntimeAuthenticateTeamSession(ctx, f.localCookie.Value, team)
	if e != nil || svc.ReauthorizeTeamSession(ctx, localIdentity, model) != nil || svc.ReauthorizeTeamSession(ctx, oidcIdentity, model) != nil || svc.ReauthorizeTeamSession(ctx, ldapIdentity, model) != nil {
		t.Fatal("old leased admission prerequisite absent")
	}
	failAuthorization.Store(true)
	saved := f.save(f.localCookie, f.admin.CSRFToken, true)
	if !saved.ReauthenticationRequired || saved.RuntimeApplied {
		t.Fatal("failed publication incorrectly claimed applied/current Session")
	}
	retainedStatus := svc.RuntimeStatus()
	if failedAuthorizationLoads.Load() != 1 || !retainedStatus.Ready || retainedStatus.ErrorCode != "database_unavailable" || retainedStatus.SnapshotID != oldStatus.SnapshotID || retainedStatus.AuthorizationValidUntil == nil || !retainedStatus.AuthorizationValidUntil.Equal(*oldStatus.AuthorizationValidUntil) || !time.Now().Before(*oldStatus.AuthorizationValidUntil) {
		t.Fatal("pre-publication failure did not retain the exact live old authorization lease")
	}
	// The loader fails before runtime.auth.Store, so the old authorization still
	// contains this local Session. Its fresh lease cannot account for denial;
	// the separately committed current policy fence must reject both admission
	// and the already captured native pre-dispatch identity.
	if _, e = svc.RuntimeAuthenticateTeamSession(ctx, f.localCookie.Value, team); e == nil {
		t.Fatal("old leased local admission survived failed auth publication")
	}
	if svc.ReauthorizeTeamSession(ctx, localIdentity, model) == nil {
		t.Fatal("held local pre-dispatch authority survived policy commit")
	}
	f.check(f.localCookie, 401)
	expectStatus(t, native(f.localCookie, f.admin.CSRFToken), 401)
	if nativeDispatches.Load() != 1 {
		t.Fatal("denied native request reached upstream")
	}
	if _, e = svc.RuntimeAuthenticateTeamSession(ctx, f.oidcCookie.Value, team); e != nil {
		t.Fatal("allowed OIDC admission lost under retained authorization")
	}
	if _, e = svc.RuntimeAuthenticateTeamSession(ctx, f.ldapCookie.Value, team); e != nil {
		t.Fatal("allowed LDAP admission lost under retained authorization")
	}
	if svc.ReauthorizeTeamSession(ctx, oidcIdentity, model) != nil || svc.ReauthorizeTeamSession(ctx, ldapIdentity, model) != nil {
		t.Fatal("policy cutoff changed independently valid allowed Team proof")
	}
	if !time.Now().Before(*oldStatus.AuthorizationValidUntil) || failedAuthorizationLoads.Load() != 1 {
		t.Fatal("deny witness expired or unexpected runtime load occurred")
	}
	failAuthorization.Store(false)
	// Preserve the separate partial route-publication assertion: after removing
	// the loader fault, authorization publishes but the invalid route still fails.
	if svc.RefreshRuntime(ctx) == nil || svc.RuntimeStatus().ErrorCode != "invalid_configuration" {
		t.Fatal("retained invalid-route publication witness absent")
	}
	if svc.ReauthorizeTeamSession(ctx, localIdentity, model) == nil || svc.ReauthorizeTeamSession(ctx, oidcIdentity, model) != nil || svc.ReauthorizeTeamSession(ctx, ldapIdentity, model) != nil {
		t.Fatal("partial route publication changed exact primary authority")
	}
	hold.release()
	var returned *httptest.ResponseRecorder
	select {
	case returned = <-done:
	case <-operation.Done():
		t.Fatal("held OIDC callback did not return in original operation bound")
	}
	select {
	case <-joined:
		applicationJoined = true
	case <-operation.Done():
		t.Fatal("held application request did not positively join")
	}
	f.status(returned, 303)
	if operation.Err() != nil || returned.Header().Get("Location") != "/auth/oidc/complete" || returned.Header().Get("Referrer-Policy") != "no-referrer" || provider.tokenCount() != beforeTokens+1 {
		t.Fatal("held allowed callback proof/clean exact-once boundary")
	}
	if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Status != "verified" || ceremony.Subject != "admin-subject" || ceremony.VerifiedAt == nil {
		t.Fatal("allowed callback lost actual signed subject")
	}
	r := f.oidcComplete(correlation, nil, "")
	allowed, allowedCookie := f.factor(r, 2)
	f.check(allowedCookie, 200)
	var allowedSession entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(allowedCookie.Value)).Take(&allowedSession).Error != nil || allowedSession.PrimaryMethod != "oidc" || allowedSession.OIDCBindingID == "" {
		t.Fatal("held callback/native factor lost primary proof")
	}
	f.status(f.call("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: localFactor.ChallengeToken, RecoveryCode: f.recovery[3]}, nil, "", "", nil), 401)
	_, emergencyCookie := f.emergency(3)
	f.check(emergencyCookie, 200)
	stage = "off_on_generation_and_current_native_recovery"
	off := f.save(f.ldapCookie, f.ldap.CSRFToken, false)
	if off.RuntimeApplied || off.ReauthenticationRequired {
		t.Fatal("failed-route disable receipt")
	}
	f.check(emergencyCookie, 401)
	if svc.ReauthorizeTeamSession(ctx, localIdentity, model) == nil {
		t.Fatal("old local Team authority revived after off")
	}
	if svc.ReauthorizeTeamSession(ctx, oidcIdentity, model) != nil || svc.ReauthorizeTeamSession(ctx, ldapIdentity, model) != nil {
		t.Fatal("allowed Team authority lost after off")
	}
	on := f.save(f.ldapCookie, f.ldap.CSRFToken, true)
	if on.RuntimeApplied || on.ReauthenticationRequired {
		t.Fatal("failed-route reenable receipt")
	}
	f.check(emergencyCookie, 401)
	if svc.ReauthorizeTeamSession(ctx, localIdentity, model) == nil {
		t.Fatal("old Team authority revived after off/on")
	}
	if db.Model(&entity.ModelProviderBinding{}).Where("id = ? AND weight = ?", bnd, 50).Update("weight", 100).Error != nil || svc.RefreshRuntime(ctx) != nil {
		t.Fatal("restore exact controlled runtime")
	}
	expectStatus(t, native(f.oidcCookie, f.oidc.CSRFToken), 200)
	expectStatus(t, native(f.ldapCookie, f.ldap.CSRFToken), 200)
	if nativeDispatches.Load() != 3 {
		t.Fatal("native completions/rejected admission count changed")
	}
	if svc.FlushCallRecorder(ctx) != nil {
		t.Fatal("flush exact native attempt evidence")
	}
	var attempts []entity.CallAttempt
	if db.Where("credential_id = ?", crd).Find(&attempts).Error != nil || len(attempts) != 3 {
		t.Fatal("native attempt census changed")
	}
	for _, attempt := range attempts {
		if attempt.NativeCompletionEvidence != "completed" || attempt.ConnectionID != con || attempt.ProviderID != prv || attempt.ProviderModelID != pm || attempt.SnapshotID == "" {
			t.Fatal("successful native attempt lost parser completion or exact attribution")
		}
	}
	if allowed.User.ID != f.admin.User.ID {
		t.Fatal("held proof changed admitted member")
	}
	var policy entity.GovernanceSetting
	if db.Take(&policy, 1).Error != nil || !policy.ForcedSSOEnabled || policy.RegistrationEnabled {
		t.Fatal("policy/registration closure drift")
	}
	stage = "complete"
}
