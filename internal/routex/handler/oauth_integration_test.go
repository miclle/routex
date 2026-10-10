package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"github.com/miclle/routex/pkg/upstream"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"log"
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
)

// testOAuthLifecycle is registered by the real-driver matrix after its normal
// database reset and migration. Trust configuration belongs to a fresh child,
// never the shared matrix process: x509 caches system roots on first use.
func testOAuthLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("OAuth lifecycle requires the selected matrix database")
	}
	dir := t.TempDir()
	oidcWriteTLSMaterial(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate OAuth child")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestOAuthLifecycleTLSChild$", "-test.timeout=120s", "-test.count=1")
	command.WaitDelay = 5 * time.Second
	// Retain normal runtime/driver settings, but replace every trust/child variable
	// explicitly. Go 1.27 honors these paths on Darwin as well as Unix.
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_OAUTH_TLS_CHILD": true, "ROUTEX_OAUTH_TLS_DIR": true, "ROUTEX_OAUTH_TLS_DRIVER": true}
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
	command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(settings, ","), "ROUTEX_OAUTH_TLS_CHILD=1", "ROUTEX_OAUTH_TLS_DIR="+dir, "ROUTEX_OAUTH_TLS_DRIVER="+driver)
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
		t.Fatal("OAuth child did not produce its terminal report")
	}
	if runErr != nil || closeErr != nil || report.Failed {
		t.Fatalf("OAuth child failed at bounded stage %q", report.Stage)
	}
	if report.Stage != "complete" {
		t.Fatal("OAuth child did not complete all stages")
	}
}

// This entrypoint is inert in ordinary runs. The registered real-driver parent
// is its sole launcher, with a preexisting migrated routex_test database.
func TestOAuthLifecycleTLSChild(t *testing.T) {
	if os.Getenv("ROUTEX_OAUTH_TLS_CHILD") != "1" {
		t.Skip("launched only by the registered OAuth real-driver lifecycle")
	}
	stage := "open_database"
	defer func() {
		raw, _ := json.Marshal(struct {
			Stage  string
			Failed bool
		}{stage, t.Failed()})
		if err := os.WriteFile(filepath.Join(os.Getenv("ROUTEX_OAUTH_TLS_DIR"), "result.json"), raw, 0600); err != nil {
			t.Error("write child terminal report")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 110*time.Second)
	defer cancel()
	driver := os.Getenv("ROUTEX_OAUTH_TLS_DRIVER")
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
		t.Fatal("OAuth child requires dedicated routex_test")
	}
	stage = "tls_trust"
	provider := oauthNewTLSProvider(t)
	defer provider.server.Close()
	client := upstream.NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	client.Timeout = 5 * time.Second
	trusted, err := client.Get(provider.server.URL + "/profile")
	if err != nil {
		t.Fatal("controlled CA was not trusted by unchanged guarded transport")
	}
	if err = trusted.Body.Close(); err != nil || trusted.StatusCode != 200 {
		t.Fatal("trusted discovery probe failed")
	}
	wrongHostname, err := client.Get(strings.Replace(provider.server.URL, "127.0.0.1", "localhost", 1) + "/profile")
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
		t.Fatal("create OAuth service")
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
			t.Fatal("OAuth/auth response lacks no-store")
		}
		return response
	}
	requireStatus := func(response *httptest.ResponseRecorder, want int) {
		t.Helper()
		if response.Code != want {
			t.Fatalf("OAuth stage %s status=%d want=%d", stage, response.Code, want)
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
	const password = "test-only-oauth-password"
	stage = "local_setup"
	setup := request("POST", "/api/v1/setup", map[string]any{"email": "oauth-admin@example.invalid", "name": "OAuth administrator", "password": password}, nil, "", "", nil)
	requireStatus(setup, 201)
	admin, adminCookie := readSession(setup)
	var storedAdmin entity.User
	if db.Where("id = ?", admin.User.ID).Take(&storedAdmin).Error != nil {
		t.Fatal("read initial administrator")
	}
	member := entity.User{ID: "usr_oauth_lifecycle_member", Email: "oauth-member@example.invalid", Name: "OAuth member", Role: entity.RoleMember, PasswordHash: storedAdmin.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("create existing admitted member")
	}
	if db.Where("id = ?", member.ID).Take(&member).Error != nil {
		t.Fatal("read persisted member birth")
	}
	local := request("POST", "/api/v1/auth/login", map[string]any{"email": member.Email, "password": password}, nil, "", "", nil)
	requireStatus(local, 200)
	memberSession, memberCookie := readSession(local)
	reviewConfig := func() (service.OAuthProviderView, string) {
		res := request("GET", "/api/v1/admin/auth/oauth", nil, adminCookie, "", "", nil)
		requireStatus(res, 200)
		var value service.OAuthProviderView
		if json.Unmarshal(res.Body.Bytes(), &value) != nil || res.Header().Get("ETag") != fmt.Sprintf("%q", value.ReviewETag) {
			t.Fatal("configuration review mismatch")
		}
		return value, res.Header().Get("ETag")
	}
	reviewSelf := func(cookie *http.Cookie) (service.OAuthAccountView, string) {
		res := request("GET", "/api/v1/account/identity/oauth", nil, cookie, "", "", nil)
		requireStatus(res, 200)
		var value service.OAuthAccountView
		if json.Unmarshal(res.Body.Bytes(), &value) != nil || res.Header().Get("ETag") != fmt.Sprintf("%q", value.ReviewETag) {
			t.Fatal("account review mismatch")
		}
		return value, res.Header().Get("ETag")
	}
	requireStatus(request("GET", "/api/v1/admin/auth/oauth", nil, memberCookie, "", "", nil), 403)
	stage = "save_disabled_configuration"
	_, etag := reviewConfig()
	config := map[string]any{"name": "Controlled identity", "authorization_url": provider.server.URL + "/authorize", "token_url": provider.server.URL + "/token", "user_info_url": provider.server.URL + "/profile", "client_auth_method": "client_secret_basic", "scopes": []string{"profile"}, "subject_path": []string{"account", "id"}, "client_id": "routex-test-client", "callback_url": "https://routex.test/api/v1/auth/oauth/callback", "secret_action": "replace", "client_secret": "test-only-client-secret", "reason": "Configure controlled identity"}
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth", config, adminCookie, admin.CSRFToken, etag, nil), 200)
	initial, etag := reviewConfig()
	if initial.Enabled || initial.Verified || !initial.SecretConfigured {
		t.Fatal("save enabled unverified configuration")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": true, "reason": "Must verify first"}, adminCookie, admin.CSRFToken, etag, nil), 409)
	identityInput := func(recovery string) map[string]any {
		proof := map[string]any{}
		if recovery != "" {
			proof["recovery_code"] = recovery
		}
		return map[string]any{"password": password, "proof": proof, "reason": "Explicit existing identity ceremony"}
	}
	// Return the browser-bound callback cookie. Real authorization, code exchange,
	// PKCE and the explicitly configured profile resource all run over TLS; application routes use the real
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
			if c.Name == "routex_oauth" {
				correlation = c
			}
		}
		if correlation == nil || !correlation.Secure || !correlation.HttpOnly || correlation.SameSite != http.SameSiteLaxMode || correlation.Path != "/api/v1/auth/oauth" {
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
		if authorized.StatusCode != 302 || e != nil || callback.Scheme != "https" || callback.Host != "routex.test" || callback.Path != "/api/v1/auth/oauth/callback" {
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
		if callbackResponse.Header().Get("Location") != "/auth/oauth/complete" || callbackResponse.Header().Get("Referrer-Policy") != "no-referrer" {
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
		return request("POST", "/api/v1/auth/oauth/complete", map[string]any{}, cookie, csrf, "", correlation)
	}
	readStoredProvider := func() entity.OAuthProvider {
		t.Helper()
		var value entity.OAuthProvider
		if db.Where("id = ?", "oauth").Take(&value).Error != nil {
			t.Fatal("read exact stored provider")
		}
		return value
	}
	readStoredBindings := func() []entity.OAuthBinding {
		t.Helper()
		var values []entity.OAuthBinding
		if db.Order("id").Find(&values).Error != nil {
			t.Fatal("read retained exact bindings")
		}
		return values
	}
	pendingFactor := func() service.MFALoginChallenge {
		t.Helper()
		correlation, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
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
		if db.Where("token_hash = ?", secret.SHA256Hex(factor.ChallengeToken)).Take(&row).Error != nil || row.PrimaryMethod != "oauth" || row.OAuthBindingCreatedAt == nil || row.OAuthUserCreatedAt == nil || row.OAuthConfigRevision == "" || row.OAuthPolicyRevision == "" {
			t.Fatal("pending factor lost exact OAuth provenance")
		}
		return factor
	}
	heldExchange := func(mutate func()) {
		t.Helper()
		correlation, callback := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{}, true)
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
		var ceremony entity.OAuthCeremony
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
		if response.Header().Get("Location") != "/auth/oauth/complete" || response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("held callback lost fixed private completion route")
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == sessionCookie {
				t.Fatal("invalidated callback issued a Session")
			}
		}
		ceremony = entity.OAuthCeremony{}
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
	adminCeremony, adminCallback := start("/api/v1/admin/auth/oauth/verify", "admin-subject", adminCookie, admin.CSRFToken, etag, identityInput(""))
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
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": true, "reason": "Enable verified identity"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	public := request("GET", "/api/v1/auth/oauth", nil, nil, "", "", nil)
	requireStatus(public, 200)
	if !strings.Contains(public.Body.String(), `"available":true`) {
		t.Fatal("verified enabled provider not public")
	}
	requireStatus(request("POST", "/api/v1/auth/oauth/start", map[string]any{}, adminCookie, admin.CSRFToken, "", nil), 403)
	stage = "member_binding"
	account, selfETag := reviewSelf(memberCookie)
	if account.Bound || !account.Available {
		t.Fatal("existing member unexpectedly bound")
	}
	requireStatus(request("POST", "/api/v1/account/identity/oauth/bind", identityInput(""), memberCookie, "", selfETag, nil), 403)
	memberCeremony, _ := start("/api/v1/account/identity/oauth/bind", "member-subject", memberCookie, memberSession.CSRFToken, selfETag, identityInput(""))
	requireStatus(complete(memberCeremony, adminCookie, admin.CSRFToken), 401)
	bound := complete(memberCeremony, memberCookie, memberSession.CSRFToken)
	requireStatus(bound, 200)
	if bound.Body.String() != `{"kind":"bound"}` {
		t.Fatal("binding did not return explicit completion")
	}
	typedBindings := readStoredBindings()
	if len(typedBindings) != 2 {
		t.Fatal("typed subject bindings missing")
	}
	foundString, foundInteger := false, false
	for _, b := range typedBindings {
		if b.ProviderID != "oauth" || b.Subject != "1" {
			t.Fatal("unexpected retained typed identity")
		}
		if b.UserID == admin.User.ID && b.SubjectKind == "string" {
			foundString = true
		}
		if b.UserID == member.ID && b.SubjectKind == "integer" {
			foundInteger = true
		}
	}
	if !foundString || !foundInteger {
		t.Fatal("string and integer subjects aliased")
	}
	stage = "existing_member_login"
	loginCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	logged := complete(loginCeremony, nil, "")
	requireStatus(logged, 200)
	oauthSession, oauthCookie := readSession(logged)
	if oauthSession.User.ID != member.ID {
		t.Fatal("OAuth selected or created another member")
	}
	var storedSession entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(oauthCookie.Value)).Take(&storedSession).Error != nil || storedSession.PrimaryMethod != "oauth" || storedSession.OAuthBindingID == "" || storedSession.OAuthBindingCreatedAt == nil || storedSession.OAuthUserCreatedAt == nil || !storedSession.OAuthUserCreatedAt.Equal(member.CreatedAt) {
		t.Fatal("OAuth Session lost exact primary provenance")
	}
	requireStatus(complete(loginCeremony, nil, ""), 401)
	// An unknown verified subject is not an invitation or automatic registration.
	unknown, _ := start("/api/v1/auth/oauth/start", "unbound-subject", nil, "", "", map[string]any{})
	requireStatus(complete(unknown, nil, ""), 401)
	var userCount int64
	if db.Model(&entity.User{}).Count(&userCount).Error != nil || userCount != 2 {
		t.Fatal("OAuth auto-created a member")
	}
	stage = "name_only_preserves_established_authority"
	preservedCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	nameBaseline := readStoredProvider()
	bindingBaseline := readStoredBindings()
	_, nameETag := reviewConfig()
	rename := map[string]any{"name": "Renamed controlled identity", "authorization_url": nameBaseline.AuthorizationURL, "token_url": nameBaseline.TokenURL, "user_info_url": nameBaseline.UserInfoURL, "client_auth_method": nameBaseline.ClientAuthMethod, "scopes": []string{"profile"}, "subject_path": []string{"account", "id"}, "client_id": nameBaseline.ClientID, "callback_url": nameBaseline.CallbackURL, "secret_action": "keep", "client_secret": "", "reason": "Rename without changing authentication policy"}
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth", rename, adminCookie, admin.CSRFToken, nameETag, nil), 200)
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
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth", rename, adminCookie, admin.CSRFToken, nameETag, nil), 409)
	if !reflect.DeepEqual(readStoredProvider(), renamed) {
		t.Fatal("stale name review rewrote current configuration")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, oauthCookie, "", "", nil), 200)
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
	stage = "oauth_mfa_login"
	factorCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	challengeResponse := complete(factorCeremony, nil, "")
	requireStatus(challengeResponse, 202)
	var challenge service.MFALoginChallenge
	if json.Unmarshal(challengeResponse.Body.Bytes(), &challenge) != nil || !challenge.MFARequired || challenge.ChallengeToken == "" {
		t.Fatal("missing MFA challenge")
	}
	for _, c := range challengeResponse.Result().Cookies() {
		if c.Name == sessionCookie {
			t.Fatal("OAuth MFA challenge prematurely issued a Session")
		}
	}
	var factorRow entity.MFAChallenge
	if db.Where("token_hash = ?", secret.SHA256Hex(challenge.ChallengeToken)).Take(&factorRow).Error != nil || factorRow.PrimaryMethod != "oauth" || factorRow.OAuthBindingID == "" || factorRow.OAuthBindingCreatedAt == nil || factorRow.OAuthUserCreatedAt == nil || !factorRow.OAuthUserCreatedAt.Equal(member.CreatedAt) {
		t.Fatal("OAuth factor challenge lost exact primary provenance")
	}
	mfaLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "", nil)
	requireStatus(mfaLogin, 200)
	_, mfaCookie := readSession(mfaLogin)
	storedSession = entity.Session{}
	if db.Where("token_hash = ?", secret.SHA256Hex(mfaCookie.Value)).Take(&storedSession).Error != nil || storedSession.PrimaryMethod != "oauth" || storedSession.OAuthBindingCreatedAt == nil || storedSession.OAuthPolicyRevision == "" {
		t.Fatal("MFA completion downgraded OAuth primary")
	}
	requireStatus(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "", nil), 401)
	stage = "disable_invalidates_primary_and_pending"
	pendingCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	staleFactor := pendingFactor()
	heldExchange(func() {
		_, etag = reviewConfig()
		requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": false, "reason": "Disable and revoke OAuth authority"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	})
	requireStatus(request("GET", "/api/v1/auth/session", nil, mfaCookie, "", "", nil), 401)
	requireStatus(request("GET", "/api/v1/auth/session", nil, adminCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	requireStatus(complete(pendingCeremony, nil, ""), 401)
	stage = "reenable_then_unlink"
	_, etag = reviewConfig()
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": true, "reason": "Re-enable existing verified configuration"}, adminCookie, admin.CSRFToken, etag, nil), 200)
	staleFactorResponse := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: staleFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[9]}, nil, "", "", nil)
	requireStatus(staleFactorResponse, 401)
	for _, cookie := range staleFactorResponse.Result().Cookies() {
		if cookie.Name == sessionCookie {
			t.Fatal("reenable revived an invalidated native factor")
		}
	}
	nextCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	nextChallenge := complete(nextCeremony, nil, "")
	requireStatus(nextChallenge, 202)
	if json.Unmarshal(nextChallenge.Body.Bytes(), &challenge) != nil {
		t.Fatal("decode fresh factor challenge")
	}
	nextLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[1]}, nil, "", "", nil)
	requireStatus(nextLogin, 200)
	_, nextCookie := readSession(nextLogin)
	heldUnlinkCeremony, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	requireStatus(request("GET", "/api/v1/auth/session", nil, mfaCookie, "", "", nil), 401)
	account, selfETag = reviewSelf(memberCookie)
	if !account.Bound || !account.MFARequired {
		t.Fatal("fresh self review lost binding/MFA")
	}
	unlink := request("POST", "/api/v1/account/identity/oauth/unlink", identityInput(recovery.RecoveryCodes[2]), memberCookie, memberSession.CSRFToken, selfETag, nil)
	requireStatus(unlink, 200)
	if !strings.Contains(unlink.Body.String(), `"bound":false`) {
		t.Fatal("unlink retained bound state")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, nextCookie, "", "", nil), 401)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	requireStatus(complete(heldUnlinkCeremony, nil, ""), 401)
	unlinked, _ := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{})
	requireStatus(complete(unlinked, nil, ""), 401)
	stage = "security_save_revokes_established_and_pending_authority"
	_, selfETag = reviewSelf(memberCookie)
	rebound, _ := start("/api/v1/account/identity/oauth/bind", "member-subject", memberCookie, memberSession.CSRFToken, selfETag, identityInput(recovery.RecoveryCodes[3]))
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
	securityInput := map[string]any{"name": securityBaseline.Name, "authorization_url": securityBaseline.AuthorizationURL, "token_url": securityBaseline.TokenURL, "user_info_url": securityBaseline.UserInfoURL, "client_auth_method": securityBaseline.ClientAuthMethod, "scopes": []string{"profile"}, "subject_path": []string{"account", "id"}, "client_id": securityBaseline.ClientID, "callback_url": securityBaseline.CallbackURL, "secret_action": "replace", "client_secret": "test-only-client-secret", "reason": "Replace identity secret and invalidate old proof"}
	heldExchange(func() {
		requireStatus(request("PUT", "/api/v1/admin/auth/oauth", securityInput, adminCookie, admin.CSRFToken, securityETag, nil), 200)
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
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth", securityInput, adminCookie, admin.CSRFToken, securityETag, nil), 409)
	if !reflect.DeepEqual(readStoredProvider(), securityAfter) {
		t.Fatal("stale security review rewrote current configuration")
	}
	unverifiedConfig, freshSecurityETag := reviewConfig()
	if unverifiedConfig.Enabled || unverifiedConfig.Verified || !unverifiedConfig.SecretConfigured {
		t.Fatal("security save falsely retained verified availability")
	}
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": true, "reason": "New security configuration must verify again"}, adminCookie, admin.CSRFToken, freshSecurityETag, nil), 409)
	stage = "enterprise_offboarding"
	// Re-establish genuine authority after the original security-save assertions.
	_, offboardConfigETag := reviewConfig()
	offboardVerification, _ := start("/api/v1/admin/auth/oauth/verify", "admin-subject", adminCookie, admin.CSRFToken, offboardConfigETag, identityInput(""))
	requireStatus(complete(offboardVerification, adminCookie, admin.CSRFToken), 200)
	_, offboardEnableETag := reviewConfig()
	requireStatus(request("PUT", "/api/v1/admin/auth/oauth/status", map[string]any{"enabled": true, "reason": "Controlled offboarding authority baseline"}, adminCookie, admin.CSRFToken, offboardEnableETag, nil), 200)
	_, offboardSelfETag := reviewSelf(memberCookie)
	offboardBinding, _ := start("/api/v1/account/identity/oauth/bind", "member-subject", memberCookie, memberSession.CSRFToken, offboardSelfETag, identityInput(recovery.RecoveryCodes[5]))
	requireStatus(complete(offboardBinding, memberCookie, memberSession.CSRFToken), 200)
	offboardFactor := pendingFactor()
	offboardLogin := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: offboardFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[6]}, nil, "", "", nil)
	requireStatus(offboardLogin, 200)
	_, offboardCookie := readSession(offboardLogin)
	var offboardSession entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(offboardCookie.Value)).Take(&offboardSession).Error != nil || offboardSession.UserID != member.ID || offboardSession.PrimaryMethod != "oauth" || offboardSession.OAuthBindingID == "" || offboardSession.OAuthBindingCreatedAt == nil || offboardSession.OAuthUserCreatedAt == nil || !offboardSession.OAuthUserCreatedAt.Equal(member.CreatedAt) {
		t.Fatal("offboarding baseline lacks genuine exact enterprise MFA Session")
	}
	requireStatus(request("GET", "/api/v1/auth/session", nil, offboardCookie, "", "", nil), 200)
	requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 200)
	offboardPendingFactor := pendingFactor()
	var recoveryBefore []entity.MFARecoveryCode
	if db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryBefore).Error != nil {
		t.Fatal("read unconsumed recovery proof baseline")
	}
	offboardProvider := readStoredProvider()
	offboardBindings := readStoredBindings()
	if len(offboardBindings) != 2 || !offboardProvider.Enabled {
		t.Fatal("offboarding lacks live identity binding baseline")
	}
	if offboardSession.OAuthConfigRevision != offboardProvider.ConfigRevision || offboardSession.OAuthPolicyRevision != offboardProvider.PolicyRevision {
		t.Fatal("offboarding Session differs from current provider proof")
	}
	matchedBinding := false
	for _, binding := range offboardBindings {
		if binding.UserID == member.ID && binding.ID == offboardSession.OAuthBindingID && binding.CreatedAt.Equal(*offboardSession.OAuthBindingCreatedAt) && binding.UserCreatedAt.Equal(member.CreatedAt) {
			matchedBinding = true
		}
	}
	if !matchedBinding {
		t.Fatal("offboarding Session differs from exact retained binding birth")
	}
	// A public login callback has no admitted user until completion. Holding it
	// across departure must not grant Session or native-MFA completion authority.
	func() {
		correlation, callback := start("/api/v1/auth/oauth/start", "member-subject", nil, "", "", map[string]any{}, true)
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
					t.Error("offboarding callback did not join within original child deadline")
				}
			}
		}()
		before := provider.tokenCount()
		req := httptest.NewRequest("GET", "https://routex.test"+callback, nil).WithContext(operation)
		req.AddCookie(correlation)
		go func() { response := httptest.NewRecorder(); router.ServeHTTP(response, req); done <- response }()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("offboarding callback returned before controlled token hold")
		case <-operation.Done():
			t.Fatal("offboarding token hold exceeded original operation bound")
		}
		var ceremony entity.OAuthCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Purpose != "login" || ceremony.Status != "exchanging" || ceremony.Subject != "" || ceremony.VerifiedAt != nil {
			t.Fatal("offboarding callback lacks claimed pre-HTTP baseline")
		}
		assertRetained := exerciseEnterpriseOffboarding(t, ctx, db, svc, "oauth", member.ID, admin, adminCookie, request)
		requireStatus(request("GET", "/api/v1/auth/session", nil, offboardCookie, "", "", nil), 401)
		requireStatus(request("GET", "/api/v1/auth/session", nil, memberCookie, "", "", nil), 401)
		deniedFactor := request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: offboardPendingFactor.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[9]}, nil, "", "", nil)
		requireStatus(deniedFactor, 401)
		hold.release()
		var response *httptest.ResponseRecorder
		select {
		case response = <-done:
			joined = true
		case <-operation.Done():
			t.Fatal("offboarding callback exceeded original operation bound")
		}
		if operation.Err() != nil {
			t.Fatal("offboarding callback completed outside original operation bound")
		}
		requireStatus(response, 303)
		if response.Header().Get("Location") != "/auth/oauth/complete" || response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("offboarding callback lost fixed private completion route")
		}
		ceremony = entity.OAuthCeremony{}
		if db.Where("cookie_hash = ?", secret.SHA256Hex(correlation.Value)).Take(&ceremony).Error != nil || ceremony.Status != "verified" || ceremony.Subject != "1" || ceremony.SubjectKind != "integer" || ceremony.VerifiedAt == nil {
			t.Fatal("held offboarding callback lacks genuine verified remote identity")
		}
		deniedCompletion := complete(correlation, nil, "")
		requireStatus(deniedCompletion, 401)
		for _, result := range []*httptest.ResponseRecorder{response, deniedFactor, deniedCompletion} {
			for _, cookie := range result.Result().Cookies() {
				if cookie.Name == sessionCookie {
					t.Fatal("departure or late callback issued a Session cookie")
				}
			}
		}
		requireStatus(complete(correlation, nil, ""), 401)
		requireStatus(request("GET", callback, nil, nil, "", "", correlation), 303)
		if provider.tokenCount() != before+1 {
			t.Fatal("offboarding replay repeated remote identity exchange")
		}
		var recoveryAfter []entity.MFARecoveryCode
		if db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryAfter).Error != nil || !reflect.DeepEqual(recoveryBefore, recoveryAfter) {
			t.Fatal("revoked pending factor consumed a recovery proof")
		}
		if !reflect.DeepEqual(offboardProvider, readStoredProvider()) || !reflect.DeepEqual(offboardBindings, readStoredBindings()) {
			t.Fatal("offboarding rewrote provider configuration or explicit binding history")
		}
		assertRetained()
	}()
	stage = "complete"
}

type oauthTLSCode struct{ Subject, Challenge, Redirect string }
type oauthTLSProvider struct {
	server *httptest.Server
	mu     sync.Mutex
	codes  map[string]oauthTLSCode
	access map[string]string
	tokens int
	hold   *oauthTLSTokenHold
}
type oauthTLSTokenHold struct {
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
}

func (h *oauthTLSTokenHold) release() { h.once.Do(func() { close(h.resume) }) }
func (p *oauthTLSProvider) holdNextToken(t *testing.T) *oauthTLSTokenHold {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.hold != nil {
		t.Fatal("controlled token hold already armed")
	}
	h := &oauthTLSTokenHold{entered: make(chan struct{}), resume: make(chan struct{})}
	p.hold = h
	return h
}
func (p *oauthTLSProvider) tokenCount() int { p.mu.Lock(); defer p.mu.Unlock(); return p.tokens }
func oauthNewTLSProvider(t *testing.T) *oauthTLSProvider {
	t.Helper()
	p := &oauthTLSProvider{codes: map[string]oauthTLSCode{}, access: map[string]string{}}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { p.serve(t, w, r) }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	certificate, err := tls.LoadX509KeyPair(filepath.Join(os.Getenv("ROUTEX_OAUTH_TLS_DIR"), "server.pem"), filepath.Join(os.Getenv("ROUTEX_OAUTH_TLS_DIR"), "server-key.pem"))
	if err != nil {
		t.Fatal("load controlled TLS identity")
	}
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	p.server = server
	server.StartTLS()
	return p
}
func (p *oauthTLSProvider) serve(t *testing.T, w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		t.Error("OAuth server received plaintext")
		w.WriteHeader(400)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/authorize":
		q := r.URL.Query()
		subject := r.Header.Get("X-Test-Subject")
		if r.Method != "GET" || q.Get("client_id") != "routex-test-client" || q.Get("response_type") != "code" || q.Get("scope") != "profile" || q.Get("redirect_uri") != "https://routex.test/api/v1/auth/oauth/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) != 43 || q.Has("nonce") || len(q.Get("code_challenge")) != 43 || subject == "" {
			t.Error("invalid configured OAuth authorization")
			w.WriteHeader(400)
			return
		}
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			t.Error("generate code")
			w.WriteHeader(500)
			return
		}
		code := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.codes[code] = oauthTLSCode{Subject: subject, Challenge: q.Get("code_challenge"), Redirect: q.Get("redirect_uri")}
		p.mu.Unlock()
		callback, _ := url.Parse(q.Get("redirect_uri"))
		callback.RawQuery = url.Values{"code": {code}, "state": {q.Get("state")}}.Encode()
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
		if !ok || client != "routex-test-client" || credential != "test-only-client-secret" || !exists || len(verifier) != 43 || base64.RawURLEncoding.EncodeToString(digest[:]) != saved.Challenge || r.Form.Get("redirect_uri") != saved.Redirect || r.Form.Get("grant_type") != "authorization_code" || r.Form.Has("client_secret") {
			t.Error("invalid configured token/PKCE contract")
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
		if _, err := rand.Read(raw); err != nil {
			t.Error("generate transient access token")
			w.WriteHeader(500)
			return
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		p.mu.Lock()
		p.access[token] = saved.Subject
		p.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "Bearer", "access_token": token})
	case "/profile":
		if r.Method != "GET" || r.URL.RawQuery != "" {
			w.WriteHeader(400)
			return
		}
		// An unauthenticated trust probe validates only TLS, never identity.
		if r.Header.Get("Authorization") == "" {
			_ = json.NewEncoder(w).Encode(map[string]bool{"tls": true})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		p.mu.Lock()
		subject, exists := p.access[token]
		delete(p.access, token)
		p.mu.Unlock()
		if !ok || !exists {
			w.WriteHeader(401)
			return
		}
		var typedSubject any = subject
		if subject == "admin-subject" {
			typedSubject = "1"
		}
		if subject == "member-subject" {
			typedSubject = json.Number("1")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"account": map[string]any{"id": typedSubject}, "email": "not-an-identity@example.invalid"})
	default:
		w.WriteHeader(404)
	}
}
