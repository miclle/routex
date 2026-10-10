package handler

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
)

// testOIDCLifecycle is registered by the real-driver matrix after its normal
// database reset and migration. Trust configuration belongs to a fresh child,
// never the shared matrix process: x509 caches system roots on first use.
func testOIDCLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("OIDC lifecycle requires the selected matrix database")
	}
	dir := t.TempDir()
	oidcWriteTLSMaterial(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate OIDC child")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestOIDCLifecycleTLSChild$", "-test.timeout=120s", "-test.count=1")
	command.WaitDelay = 5 * time.Second
	// Retain normal runtime/driver settings, but replace every trust/child variable
	// explicitly. Go 1.27 honors these paths on Darwin as well as Unix.
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_OIDC_TLS_CHILD": true, "ROUTEX_OIDC_TLS_DIR": true, "ROUTEX_OIDC_TLS_DRIVER": true}
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
	command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(settings, ","), "ROUTEX_OIDC_TLS_CHILD=1", "ROUTEX_OIDC_TLS_DIR="+dir, "ROUTEX_OIDC_TLS_DRIVER="+driver)
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
		t.Fatal("OIDC child did not produce its terminal report")
	}
	if runErr != nil || closeErr != nil || report.Failed {
		t.Fatalf("OIDC child failed at bounded stage %q", report.Stage)
	}
	if report.Stage != "complete" {
		t.Fatal("OIDC child did not complete all stages")
	}
}

func oidcWriteTLSMaterial(t *testing.T, dir string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate test CA")
	}
	now := time.Now().UTC()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "RouteX isolated OIDC test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create test CA")
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate test TLS key")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal("create test TLS certificate")
	}
	for name, value := range map[string][]byte{"ca.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), "server.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), "server-key.pem": pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(leafKey)})} {
		if err = os.WriteFile(filepath.Join(dir, name), value, 0600); err != nil {
			t.Fatal("write private TLS fixture")
		}
	}
	if err = os.Mkdir(filepath.Join(dir, "roots"), 0700); err != nil {
		t.Fatal("create isolated roots directory")
	}
}

// This entrypoint is inert in ordinary runs. The registered real-driver parent
// is its sole launcher, with a preexisting migrated routex_test database.
func TestOIDCLifecycleTLSChild(t *testing.T) {
	if os.Getenv("ROUTEX_OIDC_TLS_CHILD") != "1" {
		t.Skip("launched only by the registered OIDC real-driver lifecycle")
	}
	stage := "open_database"
	defer func() {
		raw, _ := json.Marshal(struct {
			Stage  string
			Failed bool
		}{stage, t.Failed()})
		if err := os.WriteFile(filepath.Join(os.Getenv("ROUTEX_OIDC_TLS_DIR"), "result.json"), raw, 0600); err != nil {
			t.Error("write child terminal report")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	driver := os.Getenv("ROUTEX_OIDC_TLS_DRIVER")
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" {
		t.Fatal("unknown child driver")
	}
	db, err := database.Open(ctx, driver, os.Getenv(envName))
	if err != nil {
		t.Fatal("open selected child database")
	}
	db.Logger = logger.Discard
	pool, err := db.DB()
	if err != nil {
		t.Fatal("get child database pool")
	}
	defer func() {
		if err := pool.Close(); err != nil {
			t.Error("close child database pool")
		}
	}()
	var databaseName string
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if err = db.Raw(query).Scan(&databaseName).Error; err != nil || databaseName != "routex_test" {
		t.Fatal("OIDC child requires dedicated routex_test")
	}
	stage = "tls_trust"
	provider := oidcNewTLSProvider(t)
	defer provider.server.Close()
	client := upstream.NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	client.Timeout = 5 * time.Second
	trusted, err := client.Get(provider.server.URL + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal("controlled CA was not trusted by unchanged guarded transport")
	}
	if err = trusted.Body.Close(); err != nil || trusted.StatusCode != 200 {
		t.Fatal("trusted discovery probe failed")
	}
	wrongHostname, err := client.Get(strings.Replace(provider.server.URL, "127.0.0.1", "localhost", 1) + "/.well-known/openid-configuration")
	if wrongHostname != nil {
		_ = wrongHostname.Body.Close()
	}
	var hostnameError x509.HostnameError
	if !errors.As(err, &hostnameError) {
		t.Fatal("guarded client did not reject the trusted chain with a wrong hostname")
	}
	untrusted := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("untrusted TLS request reached HTTP") }))
	untrusted.Config.ErrorLog = log.New(io.Discard, "", 0)
	untrusted.StartTLS()
	defer untrusted.Close()
	rejected, err := client.Get(untrusted.URL)
	if rejected != nil {
		_ = rejected.Body.Close()
	}
	if err == nil {
		t.Fatal("guarded client accepted an untrusted TLS chain")
	}
	store, err := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if err != nil {
		t.Fatal("create test root store")
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal("create OIDC service")
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	request := func(method, route string, body any, cookie *http.Cookie, csrf, etag string, ceremony *http.Cookie) *httptest.ResponseRecorder {
		var payload []byte
		if body != nil {
			payload, err = json.Marshal(body)
			if err != nil {
				t.Fatal("marshal fixture input")
			}
		}
		req := httptest.NewRequest(method, "https://routex.test"+route, bytes.NewReader(payload)).WithContext(ctx)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Origin", "https://routex.test")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if ceremony != nil {
			req.AddCookie(ceremony)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			req.Header.Set("If-Match", etag)
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("OIDC/auth response lacks no-store")
		}
		return response
	}
	requireStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("OIDC stage %s status=%d want=%d", stage, response.Code, want)
		}
	}
	readSession := func(response *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
		t.Helper()
		var result SessionResponse
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.User.ID == "" || result.CSRFToken == "" {
			t.Fatal("invalid safe Session DTO")
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == sessionCookie && cookie.MaxAge > 0 {
				if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
					t.Fatal("invalid HTTPS Session cookie")
				}
				return result, cookie
			}
		}
		t.Fatal("missing Session cookie")
		return result, nil
	}
	const password = "test-only-oidc-password"
	stage = "local_setup"
	setup := request("POST", "/api/v1/setup", map[string]any{"email": "oidc-admin@example.invalid", "name": "OIDC administrator", "password": password}, nil, "", "", nil)
	requireStatus(setup, 201)
	admin, adminCookie := readSession(setup)
	var storedAdmin entity.User
	if db.Where("id = ?", admin.User.ID).Take(&storedAdmin).Error != nil {
		t.Fatal("read initial administrator")
	}
	member := entity.User{ID: "usr_oidc_lifecycle_member", Email: "oidc-member@example.invalid", Name: "OIDC member", Role: entity.RoleMember, PasswordHash: storedAdmin.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("create existing admitted member")
	}
	if db.Where("id = ?", member.ID).Take(&member).Error != nil {
		t.Fatal("read persisted member birth")
	}
	local := request("POST", "/api/v1/auth/login", map[string]any{"email": member.Email, "password": password}, nil, "", "", nil)
	requireStatus(local, 200)
	memberSession, memberCookie := readSession(local)
	reviewConfig := func() (service.OIDCProviderView, string) {
		res := request("GET", "/api/v1/admin/auth/oidc", nil, adminCookie, "", "", nil)
		requireStatus(res, 200)
		var value service.OIDCProviderView
		if json.Unmarshal(res.Body.Bytes(), &value) != nil || res.Header().Get("ETag") != fmt.Sprintf("%q", value.ReviewETag) {
			t.Fatal("configuration review mismatch")
		}
		return value, res.Header().Get("ETag")
	}
	reviewSelf := func(cookie *http.Cookie) (service.OIDCAccountView, string) {
		res := request("GET", "/api/v1/account/identity", nil, cookie, "", "", nil)
		requireStatus(res, 200)
		var value service.OIDCAccountView
		if json.Unmarshal(res.Body.Bytes(), &value) != nil || res.Header().Get("ETag") != fmt.Sprintf("%q", value.ReviewETag) {
			t.Fatal("account review mismatch")
		}
		return value, res.Header().Get("ETag")
	}
	requireStatus(request("GET", "/api/v1/admin/auth/oidc", nil, memberCookie, "", "", nil), 403)
	stage = "save_disabled_configuration"
	_, etag := reviewConfig()
	config := map[string]any{"name": "Controlled identity", "issuer": provider.server.URL, "client_id": "routex-test-client", "callback_url": "https://routex.test/api/v1/auth/oidc/callback", "secret_action": "replace", "client_secret": "test-only-client-secret", "reason": "Configure controlled identity"}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc", config, adminCookie, admin.CSRFToken, etag, nil), 200)
	initial, etag := reviewConfig()
	if initial.Enabled || initial.Verified || !initial.SecretConfigured {
		t.Fatal("save enabled unverified configuration")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc/status", map[string]any{"enabled": true, "reason": "Must verify first"}, adminCookie, admin.CSRFToken, etag, nil), 409)
	identityInput := func(recovery string) map[string]any {
		proof := map[string]any{}
		if recovery != "" {
			proof["recovery_code"] = recovery
		}
		return map[string]any{"password": password, "proof": proof, "reason": "Explicit existing identity ceremony"}
	}
	// Return the browser-bound callback cookie. Real authorization, code exchange,
	// PKCE and signed JWT/JWKS all run over TLS; application routes use the real
	// handler stack through httptest, including actual HTTPS/Origin metadata.
	start := func(route, subject string, cookie *http.Cookie, csrf, review string, input any, deferExchange ...bool) (*http.Cookie, string) {
		res := request("POST", route, input, cookie, csrf, review, nil)
		requireStatus(res, 200)
		var value struct {
			AuthorizationURL string `json:"authorization_url"`
		}
		if json.Unmarshal(res.Body.Bytes(), &value) != nil {
			t.Fatal("decode authorization start")
		}
		var correlation *http.Cookie
		for _, c := range res.Result().Cookies() {
			if c.Name == "routex_oidc" {
				correlation = c
			}
		}
		if correlation == nil || !correlation.Secure || !correlation.HttpOnly || correlation.SameSite != http.SameSiteLaxMode || correlation.Path != "/api/v1/auth/oidc" {
			t.Fatal("invalid browser correlation cookie")
		}
		u, e := url.Parse(value.AuthorizationURL)
		if e != nil || u.Scheme != "https" || u.Host != strings.TrimPrefix(provider.server.URL, "https://") {
			t.Fatal("untrusted authorization location")
		}
		authReq, e := http.NewRequestWithContext(ctx, "GET", value.AuthorizationURL, nil)
		if e != nil {
			t.Fatal("create authorization request")
		}
		authReq.Header.Set("X-Test-Subject", subject)
		browser := provider.server.Client()
		browser.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		authorized, e := browser.Do(authReq)
		if e != nil {
			t.Fatal("controlled authorization TLS failed")
		}
		_ = authorized.Body.Close()
		callback, e := url.Parse(authorized.Header.Get("Location"))
		if authorized.StatusCode != 302 || e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/oidc/callback" {
			t.Fatal("invalid controlled callback location")
		}
		if len(deferExchange) == 1 && deferExchange[0] {
			return correlation, callback.RequestURI()
		}
		before := provider.tokenCount()
		wrongState := *callback
		wrongQuery := wrongState.Query()
		wrongQuery.Set("state", strings.Repeat("x", 43))
		wrongState.RawQuery = wrongQuery.Encode()
		requireStatus(request("GET", wrongState.RequestURI(), nil, nil, "", "", correlation), 303)
		wrongCookie := *correlation
		wrongCookie.Value = strings.Repeat("z", 43)
		requireStatus(request("GET", callback.RequestURI(), nil, nil, "", "", &wrongCookie), 303)
		if provider.tokenCount() != before {
			t.Fatal("invalid browser proof reached token exchange")
		}
		callbackResponse := request("GET", callback.RequestURI(), nil, nil, "", "", correlation)
		requireStatus(callbackResponse, 303)
		if callbackResponse.Header().Get("Location") != "/auth/oidc/complete" || callbackResponse.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("callback did not clean sensitive URL")
		}
		for _, c := range callbackResponse.Result().Cookies() {
			if c.Name == sessionCookie {
				t.Fatal("GET callback issued a Session")
			}
		}
		if provider.tokenCount() != before+1 {
			t.Fatal("callback did not exchange exactly once")
		}
		return correlation, callback.RequestURI()
	}
	complete := func(correlation, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		return request("POST", "/api/v1/auth/oidc/complete", map[string]any{}, cookie, csrf, "", correlation)
	}
	readStoredProvider := func() entity.OIDCProvider {
		t.Helper()
		var value entity.OIDCProvider
		if db.Where("id = ?", "oidc").Take(&value).Error != nil {
			t.Fatal("read exact stored provider")
		}
		return value
	}
	readStoredBindings := func() []entity.OIDCBinding {
		t.Helper()
		var values []entity.OIDCBinding
		if db.Order("id").Find(&values).Error != nil {
			t.Fatal("read retained exact bindings")
		}
		return values
	}
	pendingFactor := func() service.MFALoginChallenge {
		t.Helper()
		correlation, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
		response := complete(correlation, nil, "")
		requireStatus(response, 202)
		var factor service.MFALoginChallenge
		if json.Unmarshal(response.Body.Bytes(), &factor) != nil || !factor.MFARequired || factor.ChallengeToken == "" {
			t.Fatal("pending native MFA challenge absent")
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == sessionCookie {
				t.Fatal("pending factor issued a Session")
			}
		}
		var row entity.MFAChallenge
		if db.Where("token_hash = ?", secret.SHA256Hex(factor.ChallengeToken)).Take(&row).Error != nil || row.PrimaryMethod != "oidc" || row.OIDCBindingCreatedAt == nil || row.OIDCUserCreatedAt == nil || row.OIDCConfigRevision == "" || row.OIDCPolicyRevision == "" {
			t.Fatal("pending factor lost exact OIDC provenance")
		}
		return factor
	}
	heldExchange := func(mutate func()) {
		t.Helper()
		correlation, callback := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{}, true)
		hold := provider.holdNextToken(t)
		operation, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			stop()
			if !joined {
				select {
				case <-done:
				case <-ctx.Done():
					t.Error("held callback did not join within original child deadline")
				}
			}
		}()
		before := provider.tokenCount()
		req := httptest.NewRequest("GET", "https://routex.test"+callback, nil).WithContext(operation)
		req.AddCookie(correlation)
		go func() {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			done <- response
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("callback returned before controlled token hold")
		case <-operation.Done():
			t.Fatal("controlled token hold exceeded original operation bound")
		}
		if operation.Err() != nil {
			t.Fatal("held exchange admission exceeded original operation bound")
		}
		var ceremony entity.OIDCCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Status != "exchanging" || ceremony.Subject != "" || ceremony.VerifiedAt != nil {
			t.Fatal("held callback did not claim exchanging before HTTP")
		}
		mutate()
		hold.release()
		var response *httptest.ResponseRecorder
		select {
		case response = <-done:
			joined = true
		case <-operation.Done():
			t.Fatal("invalidated callback exceeded original operation bound")
		}
		if operation.Err() != nil {
			t.Fatal("held exchange completion exceeded original operation bound")
		}
		requireStatus(response, 303)
		if response.Header().Get("Location") != "/auth/oidc/complete" || response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("held callback lost fixed private completion route")
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == sessionCookie {
				t.Fatal("invalidated callback issued a Session")
			}
		}
		ceremony = entity.OIDCCeremony{}
		if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Status != "failed" || ceremony.Subject != "" || ceremony.VerifiedAt != nil {
			t.Fatal("late exchange resurrected invalidated ceremony")
		}
		requireStatus(complete(correlation, nil, ""), 401)
		requireStatus(request("GET", callback, nil, nil, "", "", correlation), 303)
		if provider.tokenCount() != before+1 {
			t.Fatal("invalidated callback replay performed another exchange")
		}
	}
	stage = "administrator_verification"
	_, etag = reviewConfig()
	adminCeremony, adminCallback := start("/api/v1/admin/auth/oidc/verify", "admin-subject", adminCookie, admin.CSRFToken, etag, identityInput(""))
	requireStatus(complete(adminCeremony, adminCookie, ""), 403)
	verified := complete(adminCeremony, adminCookie, admin.CSRFToken)
	requireStatus(verified, 200)
	if verified.Body.String() != `{"kind":"verified"}` {
		t.Fatal("verification created a Session")
	}
	beforeReplay := provider.tokenCount()
	requireStatus(request("GET", adminCallback, nil, nil, "", "", adminCeremony), 303)
	if provider.tokenCount() != beforeReplay {
		t.Fatal("callback replay exchanged another code")
	}
	requireStatus(complete(adminCeremony, adminCookie, admin.CSRFToken), 401)
	stage = "explicit_enable"
	verifiedConfig, etag := reviewConfig()
	if !verifiedConfig.Verified || verifiedConfig.Enabled {
		t.Fatal("verification implicitly enabled provider")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc/status", map[string]any{"enabled": true, "reason": "Enable verified identity"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	public := request("GET", "/api/v1/auth/oidc", nil, nil, "", "", nil)
	requireStatus(public, 200)
	if !strings.Contains(public.Body.String(), `"available":true`) {
		t.Fatal("verified enabled provider not public")
	}
	requireStatus(request("POST", "/api/v1/auth/oidc/start", map[string]any{}, adminCookie, admin.CSRFToken, "", nil), 403)
	stage = "member_binding"
	account, selfETag := reviewSelf(memberCookie)
	if account.Bound || !account.Available {
		t.Fatal("existing member unexpectedly bound")
	}
	requireStatus(request("POST", "/api/v1/account/identity/bind", identityInput(""), memberCookie, "", selfETag, nil), 403)
	memberCeremony, _ := start("/api/v1/account/identity/bind", "member-subject", memberCookie, memberSession.CSRFToken, selfETag, identityInput(""))
	requireStatus(complete(memberCeremony, adminCookie, admin.CSRFToken), 401)
	bound := complete(memberCeremony, memberCookie, memberSession.CSRFToken)
	requireStatus(bound, 200)
	if bound.Body.String() != `{"kind":"bound"}` {
		t.Fatal("binding did not return explicit completion")
	}
	stage = "existing_member_login"
	loginCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	logged := complete(loginCeremony, nil, "")
	requireStatus(logged, 200)
	oidcSession, oidcCookie := readSession(logged)
	if oidcSession.User.ID != member.ID {
		t.Fatal("OIDC selected or created another member")
	}
	var storedSession entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(oidcCookie.Value)).Take(&storedSession).Error != nil || storedSession.PrimaryMethod != "oidc" || storedSession.OIDCBindingID == "" || storedSession.OIDCBindingCreatedAt == nil || storedSession.OIDCUserCreatedAt == nil || !storedSession.OIDCUserCreatedAt.Equal(member.CreatedAt) {
		t.Fatal("OIDC Session lost exact primary provenance")
	}
	requireStatus(complete(loginCeremony, nil, ""), 401)
	// An unknown verified subject is not an invitation or automatic registration.
	unknown, _ := start("/api/v1/auth/oidc/start", "unbound-subject", nil, "", "", map[string]any{})
	requireStatus(complete(unknown, nil, ""), 401)
	var userCount int64
	if db.Model(&entity.User{}).Count(&userCount).Error != nil || userCount != 2 {
		t.Fatal("OIDC auto-created a member")
	}
	stage = "name_only_preserves_established_authority"
	preservedCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	nameBaseline := readStoredProvider()
	bindingBaseline := readStoredBindings()
	_, nameETag := reviewConfig()
	rename := map[string]any{"name": "Renamed controlled identity", "issuer": nameBaseline.Issuer, "client_id": nameBaseline.ClientID, "callback_url": nameBaseline.CallbackURL, "secret_action": "keep", "client_secret": "", "reason": "Rename without changing authentication policy"}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc", rename, adminCookie, admin.CSRFToken, nameETag, nil), 200)
	renamed := readStoredProvider()
	if renamed.Name != "Renamed controlled identity" || renamed.ReviewRevision == nameBaseline.ReviewRevision {
		t.Fatal("name-only save did not advance display review")
	}
	normalizedRename := renamed
	normalizedRename.Name = nameBaseline.Name
	normalizedRename.ReviewRevision = nameBaseline.ReviewRevision
	normalizedRename.UpdatedAt = nameBaseline.UpdatedAt
	if !reflect.DeepEqual(normalizedRename, nameBaseline) || !reflect.DeepEqual(readStoredBindings(), bindingBaseline) {
		t.Fatal("name-only save changed security configuration or exact bindings")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc", rename, adminCookie, admin.CSRFToken, nameETag, nil), 409)
	if !reflect.DeepEqual(readStoredProvider(), renamed) {
		t.Fatal("stale name review rewrote current configuration")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, oidcCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	preservedLogin := complete(preservedCeremony, nil, "")
	requireStatus(preservedLogin, 200)
	preservedSession, _ := readSession(preservedLogin)
	if preservedSession.User.ID != member.ID {
		t.Fatal("name-only save changed admitted ceremony identity")
	}
	stage = "mfa_enrollment"
	enrolled := request("POST", "/api/v1/account/mfa/enrollment", map[string]any{"current_password": password}, memberCookie, memberSession.CSRFToken, "", nil)
	requireStatus(enrolled, 200)
	var enrollment service.MFAEnrollment
	if json.Unmarshal(enrolled.Body.Bytes(), &enrollment) != nil {
		t.Fatal("decode MFA enrollment")
	}
	enabled := request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: password, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, memberCookie, memberSession.CSRFToken, "", nil)
	requireStatus(enabled, 200)
	var recovery MFARecoveryResponse
	if json.Unmarshal(enabled.Body.Bytes(), &recovery) != nil || recovery.Session == nil || len(recovery.RecoveryCodes) != 10 {
		t.Fatal("invalid MFA enable result")
	}
	memberSession = *recovery.Session
	for _, c := range enabled.Result().Cookies() {
		if c.Name == sessionCookie {
			memberCookie = c
		}
	}
	stage = "oidc_mfa_login"
	factorCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	challengeResponse := complete(factorCeremony, nil, "")
	requireStatus(challengeResponse, 202)
	var challenge service.MFALoginChallenge
	if json.Unmarshal(challengeResponse.Body.Bytes(), &challenge) != nil || !challenge.MFARequired || challenge.ChallengeToken == "" {
		t.Fatal("missing MFA challenge")
	}
	for _, c := range challengeResponse.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Fatal("OIDC MFA challenge prematurely issued a Session")
		}
	}
	var factorRow entity.MFAChallenge
	if db.Where("token_hash = ?", secret.SHA256Hex(challenge.ChallengeToken)).Take(&factorRow).Error != nil || factorRow.PrimaryMethod != "oidc" || factorRow.OIDCBindingID == "" || factorRow.OIDCBindingCreatedAt == nil || factorRow.OIDCUserCreatedAt == nil || !factorRow.OIDCUserCreatedAt.Equal(member.CreatedAt) {
		t.Fatal("OIDC factor challenge lost exact primary provenance")
	}
	mfaLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "", nil)
	requireStatus(mfaLogin, 200)
	_, mfaCookie := readSession(mfaLogin)
	storedSession = entity.Session{}
	if db.Where("token_hash = ?", secret.SHA256Hex(mfaCookie.Value)).Take(&storedSession).Error != nil || storedSession.PrimaryMethod != "oidc" || storedSession.OIDCBindingCreatedAt == nil || storedSession.OIDCPolicyRevision == "" {
		t.Fatal("MFA completion downgraded OIDC primary")
	}
	requireStatus(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "", nil), 401)
	stage = "disable_invalidates_primary_and_pending"
	pendingCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	staleFactor := pendingFactor()
	heldExchange(func() {
		_, etag = reviewConfig()
		requireStatus(request("PUT", "/api/v1/admin/auth/oidc/status", map[string]any{"enabled": false, "reason": "Disable and revoke OIDC authority"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	})
	requireStatus(request("GET", "/api/v1/auth/session", nil, mfaCookie, "", "", nil), 401)
	requireStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	requireStatus(complete(pendingCeremony, nil, ""), 401)
	stage = "reenable_then_unlink"
	_, etag = reviewConfig()
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc/status", map[string]any{"enabled": true, "reason": "Re-enable existing verified configuration"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	staleFactorResponse := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: staleFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[9]}, nil, "", "", nil)
	requireStatus(staleFactorResponse, 401)
	for _, cookie := range staleFactorResponse.Result().Cookies() {
		if cookie.Name == sessionCookie {
			t.Fatal("reenable revived an invalidated native factor")
		}
	}
	nextCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	nextChallenge := complete(nextCeremony, nil, "")
	requireStatus(nextChallenge, 202)
	if json.Unmarshal(nextChallenge.Body.Bytes(), &challenge) != nil {
		t.Fatal("decode fresh factor challenge")
	}
	nextLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[1]}, nil, "", "", nil)
	requireStatus(nextLogin, 200)
	_, nextCookie := readSession(nextLogin)
	heldUnlinkCeremony, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	requireStatus(request("GET", "/api/v1/auth/session", nil, mfaCookie, "", "", nil), 401)
	account, selfETag = reviewSelf(memberCookie)
	if !account.Bound || !account.MFARequired {
		t.Fatal("fresh self review lost binding/MFA")
	}
	unlink := request("POST", "/api/v1/account/identity/unlink", identityInput(recovery.RecoveryCodes[2]), memberCookie, memberSession.CSRFToken, selfETag, nil)
	requireStatus(unlink, 200)
	if !strings.Contains(unlink.Body.String(), `"bound":false`) {
		t.Fatal("unlink retained bound state")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, nextCookie, "", "", nil), 401)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	requireStatus(complete(heldUnlinkCeremony, nil, ""), 401)
	unlinked, _ := start("/api/v1/auth/oidc/start", "member-subject", nil, "", "", map[string]any{})
	requireStatus(complete(unlinked, nil, ""), 401)
	stage = "security_save_revokes_established_and_pending_authority"
	_, selfETag = reviewSelf(memberCookie)
	rebound, _ := start("/api/v1/account/identity/bind", "member-subject", memberCookie, memberSession.CSRFToken, selfETag, identityInput(recovery.RecoveryCodes[3]))
	requireStatus(complete(rebound, memberCookie, memberSession.CSRFToken), 200)
	securityFactor := pendingFactor()
	securityLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: securityFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[4]}, nil, "", "", nil)
	requireStatus(securityLogin, 200)
	_, securityCookie := readSession(securityLogin)
	unconsumedSecurityFactor := pendingFactor()
	securityBaseline := readStoredProvider()
	securityBindings := readStoredBindings()
	if len(securityBindings) != 2 || !securityBaseline.Enabled || securityBaseline.VerifiedConfigRevision != securityBaseline.ConfigRevision {
		t.Fatal("security mutation lacks established authority prerequisite")
	}
	_, securityETag := reviewConfig()
	securityInput := map[string]any{"name": securityBaseline.Name, "issuer": securityBaseline.Issuer, "client_id": securityBaseline.ClientID, "callback_url": securityBaseline.CallbackURL, "secret_action": "replace", "client_secret": "test-only-client-secret", "reason": "Replace identity secret and invalidate old proof"}
	heldExchange(func() {
		requireStatus(request("PUT", "/api/v1/admin/auth/oidc", securityInput, adminCookie, admin.CSRFToken, securityETag, nil), 200)
	})
	securityAfter := readStoredProvider()
	if securityAfter.ReviewRevision == securityBaseline.ReviewRevision || securityAfter.ConfigRevision == securityBaseline.ConfigRevision || securityAfter.PolicyRevision == securityBaseline.PolicyRevision || securityAfter.SecretGeneration == securityBaseline.SecretGeneration || securityAfter.AuthCiphertext == securityBaseline.AuthCiphertext || securityAfter.Enabled || securityAfter.VerifiedConfigRevision != "" || securityAfter.VerifiedBy != "" || securityAfter.VerifiedUserCreatedAt != nil || securityAfter.VerifiedBindingID != "" || securityAfter.VerifiedBindingCreatedAt != nil || !securityAfter.CreatedAt.Equal(securityBaseline.CreatedAt) {
		t.Fatal("security save retained stale authority or replaced singleton birth")
	}
	if len(readStoredBindings()) != 0 {
		t.Fatal("security save retained old identity bindings")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, securityCookie, "", "", nil), 401)
	requireStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	invalidFactorResponse := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: unconsumedSecurityFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[9]}, nil, "", "", nil)
	requireStatus(invalidFactorResponse, 401)
	for _, cookie := range invalidFactorResponse.Result().Cookies() {
		if cookie.Name == sessionCookie {
			t.Fatal("security save retained pending native factor authority")
		}
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc", securityInput, adminCookie, admin.CSRFToken, securityETag, nil), 409)
	if !reflect.DeepEqual(readStoredProvider(), securityAfter) {
		t.Fatal("stale security review rewrote current configuration")
	}
	unverifiedConfig, freshSecurityETag := reviewConfig()
	if unverifiedConfig.Enabled || unverifiedConfig.Verified || !unverifiedConfig.SecretConfigured {
		t.Fatal("security save falsely retained verified availability")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oidc/status", map[string]any{"enabled": true, "reason": "New security configuration must verify again"}, adminCookie, admin.CSRFToken, freshSecurityETag, nil), 409)
	stage = "complete"
}

type oidcTLSCode struct{ Subject, Nonce, Challenge, Redirect string }
type oidcTLSProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	codes  map[string]oidcTLSCode
	tokens int
	key    *rsa.PrivateKey
	hold   *oidcTLSTokenHold
}

type oidcTLSTokenHold struct {
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (h *oidcTLSTokenHold) release() { h.once.Do(func() { close(h.resume) }) }
func (p *oidcTLSProvider) holdNextToken(t *testing.T) *oidcTLSTokenHold {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		t.Fatal("controlled token hold already armed")
	}
	h := &oidcTLSTokenHold{entered: make(chan struct{}), resume: make(chan struct{})}
	p.hold = h
	return h
}

func (p *oidcTLSProvider) tokenCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.tokens }
func oidcNewTLSProvider(t *testing.T) *oidcTLSProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal("generate test signing key")
	}
	p := &oidcTLSProvider{codes: map[string]oidcTLSCode{}, key: key}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serve(t, w, r) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	certificate, err := tls.LoadX509KeyPair(filepath.Join(os.Getenv("ROUTEX_OIDC_TLS_DIR"), "server.pem"), filepath.Join(os.Getenv("ROUTEX_OIDC_TLS_DIR"), "server-key.pem"))
	if err != nil {
		t.Fatal("load test TLS identity")
	}
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	p.server = server
	server.StartTLS()
	return p
}
func (p *oidcTLSProvider) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		t.Error("IdP received plaintext")
		w.WriteHeader(400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": p.server.URL, "authorization_endpoint": p.server.URL + "/authorize", "token_endpoint": p.server.URL + "/token", "jwks_uri": p.server.URL + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}})
	case "/authorize":
		q := r.URL.Query()
		subject := r.Header.Get("X-Test-Subject")
		if r.Method != "GET" || q.Get("client_id") != "routex-test-client" || q.Get("response_type") != "code" || q.Get("scope") != "openid" || q.Get("redirect_uri") != "https://routex.test/api/v1/auth/oidc/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || len(q.Get("nonce")) != 43 || len(q.Get("code_challenge")) != 43 || subject == "" {
			t.Error("invalid controlled authorization contract")
			w.WriteHeader(400)
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Error("generate test code")
			w.WriteHeader(500)
			return
		}
		code := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.codes[code] = oidcTLSCode{Subject: subject, Nonce: q.Get("nonce"), Challenge: q.Get("code_challenge"), Redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {p.server.URL}}.Encode()
		w.Header().Set("Location", callback.String())
		w.WriteHeader(302)
	case "/token":
		if r.Method != "POST" {
			w.WriteHeader(405)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			w.WriteHeader(400)
			return
		}
		client, credential, ok := r.BasicAuth()
		p.mu.Lock()
		saved, exists := p.codes[r.Form.Get("code")]
		delete(p.codes, r.Form.Get("code"))
		p.tokens++
		hold := p.hold
		p.hold = nil
		p.mu.Unlock()
		verifier := r.Form.Get("code_verifier")
		digest := sha256.Sum256([]byte(verifier))
		if !ok || client != "routex-test-client" || credential != "test-only-client-secret" || !exists || len(verifier) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != saved.Challenge || r.Form.Get("redirect_uri") != saved.Redirect || r.Form.Get("grant_type") != "authorization_code" {
			t.Error("invalid controlled code/PKCE exchange")
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
		now := time.Now().Unix()
		header, _ := json.Marshal(map[string]any{"alg": "RS256", "kid": "test-signing-key", "typ": "JWT"})
		claims, _ := json.Marshal(map[string]any{"iss": p.server.URL, "sub": saved.Subject, "aud": "routex-test-client", "iat": now, "exp": now + 120, "nonce": saved.Nonce})
		signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
		hash := sha256.Sum256([]byte(signing))
		signature, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, hash[:])
		if err != nil {
			t.Error("sign fixture identity")
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id_token": signing + "." + base64.RawURLEncoding.EncodeToString(signature), "token_type": "Bearer", "access_token": "test-only-transient-access-token"})
	case "/jwks":
		if r.Method != "GET" {
			w.WriteHeader(405)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-signing-key", "n": base64.RawURLEncoding.EncodeToString(p.key.N.Bytes()), "e": "AQAB"}}})
	default:
		w.WriteHeader(404)
	}
}
