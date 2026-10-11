package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testForcedSSOLifecycle(t *testing.T, db *gorm.DB) {
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
	command := exec.CommandContext(ctx, executable, "-test.run=^TestForcedSSOLifecycleTLSChild$", "-test.timeout=120s", "-test.count=1")
	command.WaitDelay = 5 * time.Second
	// Retain normal runtime/driver settings, but replace every trust/child variable
	// explicitly. Go 1.27 honors these paths on Darwin as well as Unix.
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_FORCED_SSO_TLS_CHILD": true, "ROUTEX_LDAP_TLS_DIR": true, "ROUTEX_LDAP_TLS_DRIVER": true}
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
	command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(settings, ","), "ROUTEX_FORCED_SSO_TLS_CHILD=1", "ROUTEX_LDAP_TLS_DIR="+dir, "ROUTEX_LDAP_TLS_DRIVER="+driver)
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

func TestForcedSSOLifecycleTLSChild(t *testing.T) {
	if os.Getenv("ROUTEX_FORCED_SSO_TLS_CHILD") != "1" {
		t.Skip("launched only by the registered forced-SSO real-driver lifecycle")
	}
	stage := "open_database"
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
	driver := os.Getenv("ROUTEX_LDAP_TLS_DRIVER")
	env := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if env == "" {
		t.Fatal("unknown driver")
	}
	db, e := database.Open(ctx, driver, os.Getenv(env))
	if e != nil {
		t.Fatal("open selected database")
	}
	db.Logger = logger.Discard
	pool, e := db.DB()
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		if pool.Close() != nil {
			t.Error("close database")
		}
	}()
	var dbName string
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&dbName).Error != nil || dbName != "routex_test" {
		t.Fatal("requires isolated routex_test")
	}
	peer := ldapNewApplicationPeer(t, ctx)
	defer peer.close(t)
	store, e := secretstore.New(bytes.Repeat([]byte{37}, 32))
	if e != nil {
		t.Fatal(e)
	}
	svc, e := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if e != nil {
		t.Fatal(e)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	requestContext := func(callCtx context.Context, method, path string, body any, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		if body == nil {
			raw = nil
		}
		r := httptest.NewRequest(method, "https://routex.test"+path, bytes.NewReader(raw)).WithContext(callCtx)
		r.Header.Set("Origin", "https://routex.test")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		if etag != "" {
			r.Header.Set("If-Match", etag)
		}
		out := httptest.NewRecorder()
		router.ServeHTTP(out, r)
		return out
	}
	request := func(method, path string, body any, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
		return requestContext(ctx, method, path, body, cookie, csrf, etag)
	}
	status := func(r *httptest.ResponseRecorder, want int) {
		t.Helper()
		if r.Code != want {
			t.Fatalf("forced SSO stage %s status=%d want=%d", stage, r.Code, want)
		}
		if !strings.Contains(r.Header().Get("Cache-Control"), "no-store") || r.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("authentication private headers")
		}
	}
	session := func(r *httptest.ResponseRecorder) (SessionResponse, *http.Cookie) {
		t.Helper()
		var v SessionResponse
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || v.CSRFToken == "" {
			t.Fatal("safe Session")
		}
		for _, c := range r.Result().Cookies() {
			if c.Name == sessionCookie && c.MaxAge > 0 {
				if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
					t.Fatal("Session cookie")
				}
				return v, c
			}
		}
		t.Fatal("Session cookie missing")
		return v, nil
	}
	const password = "test-only-forced-sso-password"
	stage = "setup_directory"
	res := request("POST", "/api/v1/setup", SetupRequest{Email: "forced-admin@example.invalid", Name: "Administrator", Password: password}, nil, "", "")
	status(res, 201)
	admin, adminCookie := session(res)
	ldapReview := func() (service.LDAPProviderView, string) {
		t.Helper()
		r := request("GET", "/api/v1/admin/auth/ldap", nil, adminCookie, "", "")
		status(r, 200)
		var v service.LDAPProviderView
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ReviewETag) {
			t.Fatal("directory review")
		}
		return v, r.Header().Get("ETag")
	}
	_, tag := ldapReview()
	config := service.LDAPProviderInput{Name: "Controlled directory", Endpoint: peer.endpoint, BindDN: "cn=service,dc=example", BaseDN: "dc=example", UserFilter: "(uid={username})", IdentityAttribute: "entryUUID", SecretAction: "replace", BindPassword: "service-password", Reason: "Configure approved directory"}
	status(request("PUT", "/api/v1/admin/auth/ldap", config, adminCookie, admin.CSRFToken, tag), 200)
	_, tag = ldapReview()
	link := service.LDAPBindingInput{Password: password, Proof: service.LDAPProof{}, Reason: "Explicit administrator link", Username: "admin", DirectoryPassword: "directory-password"}
	status(request("POST", "/api/v1/admin/auth/ldap/verify", link, adminCookie, admin.CSRFToken, tag), 200)
	_, tag = ldapReview()
	status(request("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: true, Reason: "Enable verified directory"}, adminCookie, admin.CSRFToken, tag), 200)
	stage = "native_mfa"
	res = request("POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: password}, adminCookie, admin.CSRFToken, "")
	status(res, 200)
	var enrollment service.MFAEnrollment
	if json.Unmarshal(res.Body.Bytes(), &enrollment) != nil {
		t.Fatal("enrollment")
	}
	res = request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: password, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, adminCookie, admin.CSRFToken, "")
	status(res, 200)
	var recovery MFARecoveryResponse
	if json.Unmarshal(res.Body.Bytes(), &recovery) != nil || recovery.Session == nil || len(recovery.RecoveryCodes) != 10 {
		t.Fatal("native factor")
	}
	admin = *recovery.Session
	for _, c := range res.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge > 0 {
			adminCookie = c
		}
	}
	ldapLogin := func() *httptest.ResponseRecorder {
		return request("POST", "/api/v1/auth/ldap/login", service.LDAPLoginInput{Username: "admin", Password: "directory-password"}, nil, "", "")
	}
	res = ldapLogin()
	status(res, 202)
	var challenge service.MFALoginChallenge
	if json.Unmarshal(res.Body.Bytes(), &challenge) != nil {
		t.Fatal("directory factor")
	}
	res = request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "")
	status(res, 200)
	directory, directoryCookie := session(res)
	var originalDirectory entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(directoryCookie.Value)).Take(&originalDirectory).Error != nil || originalDirectory.PrimaryMethod != "ldap" {
		t.Fatal("genuine directory proof")
	}
	res = request("POST", "/api/v1/auth/login", LoginRequest{Email: admin.User.Email, Password: password}, nil, "", "")
	status(res, 202)
	var pendingLocal service.MFALoginChallenge
	if json.Unmarshal(res.Body.Bytes(), &pendingLocal) != nil {
		t.Fatal("pending local challenge")
	}
	policyReview := func(cookie *http.Cookie, candidate string) (service.AuthenticationPolicyView, string) {
		t.Helper()
		path := "/api/v1/admin/auth/policy"
		if candidate != "" {
			path += "?emergency_admin_id=" + candidate
		}
		r := request("GET", path, nil, cookie, "", "")
		status(r, 200)
		var v service.AuthenticationPolicyView
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ReviewETag) {
			t.Fatal("policy review")
		}
		if candidate == "" && v.ReviewEmergencyAdmin != nil || candidate != "" && (v.ReviewEmergencyAdmin == nil || v.ReviewEmergencyAdmin.ID != candidate) {
			t.Fatal("candidate review scope")
		}
		return v, r.Header().Get("ETag")
	}
	stage = "enable_and_cutoff"
	_, noCandidateTag := policyReview(adminCookie, "")
	_, candidateTag := policyReview(adminCookie, admin.User.ID)
	in := service.AuthenticationPolicyInput{Enforced: true, EmergencyAdminID: admin.User.ID, Reason: "Require approved existing-member SSO"}
	status(request("PUT", "/api/v1/admin/auth/policy", in, adminCookie, admin.CSRFToken, noCandidateTag), 409)
	res = request("PUT", "/api/v1/admin/auth/policy", in, adminCookie, admin.CSRFToken, candidateTag)
	status(res, 200)
	var saved service.AuthenticationPolicyResult
	if json.Unmarshal(res.Body.Bytes(), &saved) != nil || saved.Confirmation != "saved" || !saved.ReauthenticationRequired || !saved.Enforced || saved.ReviewEmergencyAdmin == nil || saved.ReviewEmergencyAdmin.ID != admin.User.ID {
		t.Fatal("committed caller-cutoff receipt")
	}
	expired := false
	for _, c := range res.Result().Cookies() {
		if c.Name == sessionCookie && c.MaxAge < 0 {
			expired = true
		}
	}
	if !expired {
		t.Fatal("invalidated caller cookie retained")
	}
	status(request("GET", "/api/v1/auth/session", nil, adminCookie, "", ""), 401)
	status(request("GET", "/api/v1/auth/session", nil, directoryCookie, "", ""), 200)
	status(request("POST", "/api/v1/auth/login", LoginRequest{Email: admin.User.Email, Password: password}, nil, "", ""), 401)
	status(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pendingLocal.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[1]}, nil, "", ""), 401)
	res = request("GET", "/api/v1/auth/policy", nil, nil, "", "")
	status(res, 200)
	var public service.AuthenticationPolicyPublic
	if json.Unmarshal(res.Body.Bytes(), &public) != nil || !public.Enforced || public.PasswordLoginEnabled || !public.EmergencyEntryEnabled {
		t.Fatal("public truth table")
	}
	stage = "emergency_real_factor"
	status(request("POST", "/api/v1/account/mfa/disable", MFAChangeRequest{CurrentPassword: password, RecoveryCode: recovery.RecoveryCodes[1]}, directoryCookie, directory.CSRFToken, ""), 409)
	beginEmergency := func() service.MFALoginChallenge {
		t.Helper()
		r := request("POST", "/api/v1/auth/emergency/login", service.EmergencyLoginInput{Email: admin.User.Email, Password: password}, nil, "", "")
		status(r, 202)
		if len(r.Result().Cookies()) != 0 {
			t.Fatal("password-only emergency Session")
		}
		var c service.MFALoginChallenge
		if json.Unmarshal(r.Body.Bytes(), &c) != nil || !c.MFARequired {
			t.Fatal("emergency challenge")
		}
		return c
	}
	emergency := beginEmergency()
	status(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: emergency.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[1]}, nil, "", ""), 401)
	res = request("POST", "/api/v1/auth/emergency/mfa/verify", map[string]string{"challenge_token": emergency.ChallengeToken, "recovery_code": recovery.RecoveryCodes[1]}, nil, "", "")
	status(res, 200)
	emergencySession, emergencyCookie := session(res)
	status(request("POST", "/api/v1/auth/emergency/mfa/verify", map[string]string{"challenge_token": emergency.ChallengeToken, "recovery_code": recovery.RecoveryCodes[1]}, nil, "", ""), 401)
	var persisted entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(emergencyCookie.Value)).Take(&persisted).Error != nil || persisted.PrimaryMethod != "emergency" || persisted.EmergencyFactorCompletedAt == nil || persisted.EmergencyFactorCompletedAt.After(persisted.CreatedAt) || persisted.EmergencyMFAGeneration == "" {
		t.Fatal("actual completed emergency proof")
	}
	stage = "security_reauthentication"
	res = request("POST", "/api/v1/account/mfa/recovery-codes", MFAChangeRequest{CurrentPassword: password, RecoveryCode: recovery.RecoveryCodes[2]}, emergencyCookie, emergencySession.CSRFToken, "")
	status(res, 200)
	var renewed MFARecoveryResponse
	if json.Unmarshal(res.Body.Bytes(), &renewed) != nil || renewed.Session != nil || !renewed.ReauthenticationRequired || len(renewed.RecoveryCodes) != 10 {
		t.Fatal("committed recovery receipt without Session")
	}
	status(request("GET", "/api/v1/auth/session", nil, emergencyCookie, "", ""), 401)
	emergency = beginEmergency()
	res = request("POST", "/api/v1/auth/emergency/mfa/verify", map[string]string{"challenge_token": emergency.ChallengeToken, "recovery_code": renewed.RecoveryCodes[0]}, nil, "", "")
	status(res, 200)
	emergencySession, emergencyCookie = session(res)
	stage = "disable_never_reopens_registration"
	_, tag = policyReview(emergencyCookie, "")
	res = request("PUT", "/api/v1/admin/auth/policy", service.AuthenticationPolicyInput{Enforced: false, EmergencyAdminID: "", Reason: "Return to optional authentication"}, emergencyCookie, emergencySession.CSRFToken, tag)
	status(res, 200)
	if json.Unmarshal(res.Body.Bytes(), &saved) != nil || saved.Enforced || !saved.ReauthenticationRequired || saved.ReviewEmergencyAdmin != nil {
		t.Fatal("disable receipt")
	}
	var policy entity.GovernanceSetting
	if db.Take(&policy, 1).Error != nil || policy.RegistrationEnabled || policy.ForcedSSOEnabled || policy.AuthenticationPolicyRevision == strings.Repeat("0", 64) {
		t.Fatal("disable reopened registration or lost generation")
	}
	status(request("GET", "/api/v1/auth/session", nil, emergencyCookie, "", ""), 401)
	status(request("POST", "/api/v1/auth/emergency/login", service.EmergencyLoginInput{Email: admin.User.Email, Password: password}, nil, "", ""), 401)
	// An allowed nonlocal primary retains its exact proof and original expiry
	// across password replacement even after enforcement is disabled.
	stage = "nonlocal_security_replacement"
	res = ldapLogin()
	status(res, 202)
	if json.Unmarshal(res.Body.Bytes(), &challenge) != nil {
		t.Fatal("renewed directory challenge")
	}
	res = request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: renewed.RecoveryCodes[1]}, nil, "", "")
	status(res, 200)
	directory, directoryCookie = session(res)
	// GORM adds a populated destination primary key to the token-hash predicate.
	originalDirectory = entity.Session{}
	if db.Where("token_hash = ?", secret.SHA256Hex(directoryCookie.Value)).Take(&originalDirectory).Error != nil {
		t.Fatal("capture exact replacement authority")
	}
	res = request("POST", "/api/v1/account/password", ChangePasswordRequest{CurrentPassword: password, NewPassword: "changed-test-only-password"}, directoryCookie, directory.CSRFToken, "")
	status(res, 200)
	_, replacementCookie := session(res)
	// Read the replacement independently from the earlier emergency Session.
	persisted = entity.Session{}
	if db.Where("token_hash = ?", secret.SHA256Hex(replacementCookie.Value)).Take(&persisted).Error != nil || persisted.PrimaryMethod != "ldap" || persisted.LDAPBindingID != originalDirectory.LDAPBindingID || persisted.LDAPBindingCreatedAt == nil || !persisted.LDAPBindingCreatedAt.Equal(*originalDirectory.LDAPBindingCreatedAt) || persisted.LDAPConfigRevision != originalDirectory.LDAPConfigRevision || persisted.LDAPPolicyRevision != originalDirectory.LDAPPolicyRevision || persisted.LDAPUserCreatedAt == nil || !persisted.LDAPUserCreatedAt.Equal(*originalDirectory.LDAPUserCreatedAt) || !persisted.ExpiresAt.Equal(originalDirectory.ExpiresAt) {
		t.Fatal("security replacement lost primary proof or extended expiry")
	}
	status(request("GET", "/api/v1/auth/session", nil, directoryCookie, "", ""), 401)
	status(request("GET", "/api/v1/auth/session", nil, replacementCookie, "", ""), 200)
	stage = "complete"
}
