package handler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	ber "github.com/go-asn1-ber/asn1-ber"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testLDAPLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("LDAP lifecycle requires the selected matrix database")
	}
	dir := t.TempDir()
	oidcWriteTLSMaterial(t, dir)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate LDAP child")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, "-test.run=^TestLDAPLifecycleTLSChild$", "-test.timeout=120s", "-test.count=1")
	command.WaitDelay = 5 * time.Second
	// Retain normal runtime/driver settings, but replace every trust/child variable
	// explicitly. Go 1.27 honors these paths on Darwin as well as Unix.
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_LDAP_TLS_CHILD": true, "ROUTEX_LDAP_TLS_DIR": true, "ROUTEX_LDAP_TLS_DRIVER": true}
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
	command.Env = append(command.Env, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(settings, ","), "ROUTEX_LDAP_TLS_CHILD=1", "ROUTEX_LDAP_TLS_DIR="+dir, "ROUTEX_LDAP_TLS_DRIVER="+driver)
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
		t.Fatal("LDAP child did not produce its terminal report")
	}
	if runErr != nil || closeErr != nil || report.Failed {
		t.Fatalf("LDAP child failed at bounded stage %q", report.Stage)
	}
	if report.Stage != "complete" {
		t.Fatal("LDAP child did not complete all stages")
	}
}

// Only the registered parent starts this fresh process so system-root caching
// cannot replace real chain/hostname verification with a test-only TLS bypass.
func TestLDAPLifecycleTLSChild(t *testing.T) {
	if os.Getenv("ROUTEX_LDAP_TLS_CHILD") != "1" {
		t.Skip("launched only by the registered LDAP real-driver lifecycle")
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
			t.Fatalf("LDAP stage %s status=%d want=%d", stage, r.Code, want)
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
	const password = "test-only-ldap-password"
	stage = "local_setup"
	res := request("POST", "/api/v1/setup", SetupRequest{Email: "ldap-admin@example.invalid", Name: "Directory administrator", Password: password}, nil, "", "")
	status(res, 201)
	admin, adminCookie := session(res)
	var originalAdmin entity.User
	if db.Where("id = ?", admin.User.ID).Take(&originalAdmin).Error != nil {
		t.Fatal("read admin")
	}
	member := entity.User{ID: "usr_ldap_lifecycle_member", Email: "ldap-member@example.invalid", Name: "Existing member", Role: entity.RoleMember, PasswordHash: originalAdmin.PasswordHash}
	if db.Create(&member).Error != nil {
		t.Fatal("seed member")
	}
	res = request("POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: password}, nil, "", "")
	status(res, 200)
	memberSession, memberCookie := session(res)
	configReview := func() (service.LDAPProviderView, string) {
		t.Helper()
		r := request("GET", "/api/v1/admin/auth/ldap", nil, adminCookie, "", "")
		status(r, 200)
		var v service.LDAPProviderView
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ReviewETag) || r.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("config review")
		}
		return v, r.Header().Get("ETag")
	}
	selfReview := func() (service.LDAPAccountView, string) {
		t.Helper()
		r := request("GET", "/api/v1/account/identity/ldap", nil, memberCookie, "", "")
		status(r, 200)
		var v service.LDAPAccountView
		if json.Unmarshal(r.Body.Bytes(), &v) != nil || r.Header().Get("ETag") != fmt.Sprintf("%q", v.ReviewETag) || r.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("self review")
		}
		return v, r.Header().Get("ETag")
	}
	proof := func(username, recovery string) service.LDAPBindingInput {
		p := service.LDAPProof{}
		if recovery != "" {
			p.RecoveryCode = recovery
		}
		return service.LDAPBindingInput{Password: password, Proof: p, Reason: "Explicit existing member link", Username: username, DirectoryPassword: "directory-password"}
	}
	stage = "ldap_initial"
	initial, tag := configReview()
	if initial.IdentityAttribute != "" || initial.Enabled || initial.Verified || initial.SecretConfigured {
		t.Fatal("unconfigured defaults")
	}
	status(request("GET", "/api/v1/admin/auth/ldap", nil, memberCookie, "", ""), 403)
	config := service.LDAPProviderInput{Name: "Controlled directory", Endpoint: peer.endpoint, BindDN: "cn=service,dc=example", BaseDN: "dc=example", UserFilter: "(uid={username})", IdentityAttribute: "entryUUID", SecretAction: "replace", BindPassword: "service-password", Reason: "Configure controlled directory"}
	status(request("PUT", "/api/v1/admin/auth/ldap", config, adminCookie, admin.CSRFToken, tag), 200)
	saved, tag := configReview()
	if saved.Enabled || saved.Verified || !saved.SecretConfigured {
		t.Fatal("save enabled login")
	}
	status(request("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: true, Reason: "Must verify first"}, adminCookie, admin.CSRFToken, tag), 409)
	status(request("POST", "/api/v1/admin/auth/ldap/verify", proof("admin", ""), adminCookie, "", tag), 403)
	status(request("POST", "/api/v1/admin/auth/ldap/verify", proof("admin", ""), adminCookie, admin.CSRFToken, tag), 200)
	verified, tag := configReview()
	if !verified.Verified || verified.Enabled {
		t.Fatal("verify/link did not remain disabled")
	}
	var verifier entity.LDAPBinding
	if db.Where("user_id = ?", admin.User.ID).Take(&verifier).Error != nil || verifier.IdentityAttribute != "entryUUID" {
		t.Fatal("exact admin not linked")
	}
	status(request("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: true, Reason: "Enable verified directory"}, adminCookie, admin.CSRFToken, tag), 200)
	stage = "ldap_member_bind"
	_, selfTag := selfReview()
	status(request("POST", "/api/v1/account/identity/ldap/bind", proof("member", ""), memberCookie, memberSession.CSRFToken, selfTag), 200)
	login := func(username string) *httptest.ResponseRecorder {
		return request("POST", "/api/v1/auth/ldap/login", service.LDAPLoginInput{Username: username, Password: "directory-password"}, nil, "", "")
	}
	status(login("unbound"), 401)
	r := login("member")
	status(r, 200)
	_, ldapCookie := session(r)
	var stored entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(ldapCookie.Value)).Take(&stored).Error != nil || stored.PrimaryMethod != "ldap" || stored.LDAPBindingCreatedAt == nil {
		t.Fatal("LDAP primary provenance")
	}
	stage = "ldap_name_only"
	var before entity.LDAPProvider
	if db.Take(&before, "id = ?", "ldap").Error != nil {
		t.Fatal("capture provider")
	}
	_, tag = configReview()
	config.Name = "Renamed directory"
	config.SecretAction = "keep"
	config.BindPassword = ""
	status(request("PUT", "/api/v1/admin/auth/ldap", config, adminCookie, admin.CSRFToken, tag), 200)
	var after entity.LDAPProvider
	if db.Take(&after, "id = ?", "ldap").Error != nil || after.ConfigRevision != before.ConfigRevision || after.PolicyRevision != before.PolicyRevision || !after.Enabled || after.VerifiedBindingID != before.VerifiedBindingID {
		t.Fatal("name-only invalidated security")
	}
	status(request("GET", "/api/v1/auth/session", nil, ldapCookie, "", ""), 200)
	stage = "mfa_enrollment"
	r = request("POST", "/api/v1/account/mfa/enrollment", map[string]any{"current_password": password}, memberCookie, memberSession.CSRFToken, "")
	status(r, 200)
	var enrollment service.MFAEnrollment
	if json.Unmarshal(r.Body.Bytes(), &enrollment) != nil {
		t.Fatal("MFA enrollment")
	}
	r = request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: password, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, memberCookie, memberSession.CSRFToken, "")
	status(r, 200)
	var recovery MFARecoveryResponse
	if json.Unmarshal(r.Body.Bytes(), &recovery) != nil || recovery.Session == nil || len(recovery.RecoveryCodes) != 10 {
		t.Fatal("native MFA enable")
	}
	memberSession = *recovery.Session
	for _, c := range r.Result().Cookies() {
		if c.Name == sessionCookie {
			memberCookie = c
		}
	}

	// Revocation during real directory I/O must reject the exact admitted Session
	// without consuming its recovery proof. The subsequent local MFA completion
	// uses that same proof and gives the fixture fresh independent authority.
	stage = "ldap_held_self_bind_session"
	_, selfTag = selfReview()
	var admittedSession entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(memberCookie.Value)).Take(&admittedSession).Error != nil {
		t.Fatal("capture exact local Session")
	}
	heldReached, heldRelease := peer.hold()
	heldResult := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		heldResult <- request("POST", "/api/v1/account/identity/ldap/bind", proof("member", recovery.RecoveryCodes[8]), memberCookie, memberSession.CSRFToken, selfTag)
	}()
	select {
	case <-heldReached:
	case <-ctx.Done():
		close(heldRelease)
		t.Fatal("held self bind admission")
	}
	revokeResult := request("DELETE", "/api/v1/account/sessions/"+admittedSession.ID, nil, memberCookie, memberSession.CSRFToken, "")
	close(heldRelease)
	select {
	case held := <-heldResult:
		status(held, 401)
	case <-ctx.Done():
		t.Fatal("held self bind join")
	}
	status(revokeResult, 204)
	r = request("POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: password}, nil, "", "")
	status(r, 202)
	var preservedProofChallenge service.MFALoginChallenge
	if json.Unmarshal(r.Body.Bytes(), &preservedProofChallenge) != nil {
		t.Fatal("fresh local MFA challenge")
	}
	r = request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: preservedProofChallenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[8]}, nil, "", "")
	status(r, 200)
	memberSession, memberCookie = session(r)
	stage = "ldap_held_self_bind_cancel"
	_, selfTag = selfReview()
	var beforeCanceled entity.LDAPBinding
	if db.Where("user_id = ?", member.ID).Take(&beforeCanceled).Error != nil {
		t.Fatal("capture retained binding")
	}
	heldReached, heldRelease = peer.hold()
	cancelCtx, cancelHeld := context.WithCancel(ctx)
	heldResult = make(chan *httptest.ResponseRecorder, 1)
	go func() {
		heldResult <- requestContext(cancelCtx, "POST", "/api/v1/account/identity/ldap/bind", proof("member", recovery.RecoveryCodes[7]), memberCookie, memberSession.CSRFToken, selfTag)
	}()
	select {
	case <-heldReached:
	case <-ctx.Done():
		cancelHeld()
		close(heldRelease)
		t.Fatal("held canceled admission")
	}
	cancelHeld()
	close(heldRelease)
	select {
	case held := <-heldResult:
		status(held, 503)
	case <-ctx.Done():
		t.Fatal("canceled self bind join")
	}
	var afterCanceled entity.LDAPBinding
	if db.Where("id = ?", beforeCanceled.ID).Take(&afterCanceled).Error != nil || afterCanceled.SubjectDigest != beforeCanceled.SubjectDigest || !afterCanceled.CreatedAt.Equal(beforeCanceled.CreatedAt) {
		t.Fatal("canceled request rewrote binding")
	}
	stage = "ldap_mfa_login"
	r = login("member")
	status(r, 202)
	var challenge service.MFALoginChallenge
	if json.Unmarshal(r.Body.Bytes(), &challenge) != nil {
		t.Fatal("challenge")
	}
	r = request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", "")
	status(r, 200)
	_, ldapCookie = session(r)
	stored = entity.Session{}
	if db.Where("token_hash = ?", secret.SHA256Hex(ldapCookie.Value)).Take(&stored).Error != nil || stored.PrimaryMethod != "ldap" {
		t.Fatal("MFA downgraded LDAP")
	}
	status(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: challenge.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[0]}, nil, "", ""), 401)
	stage = "ldap_held_disable"
	r = login("member")
	status(r, 202)
	var pending service.MFALoginChallenge
	if json.Unmarshal(r.Body.Bytes(), &pending) != nil {
		t.Fatal("pending native challenge")
	}
	reached, release := peer.hold()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- login("member") }()
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatal("held directory phase")
	}
	_, tag = configReview()
	status(request("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: false, Reason: "Disable held login"}, adminCookie, admin.CSRFToken, tag), 200)
	close(release)
	select {
	case r = <-done:
		status(r, 401)
	case <-ctx.Done():
		t.Fatal("held login join")
	}
	status(request("GET", "/api/v1/auth/session", nil, ldapCookie, "", ""), 401)
	status(request("GET", "/api/v1/auth/session", nil, memberCookie, "", ""), 200)
	_, tag = configReview()
	status(request("PUT", "/api/v1/admin/auth/ldap/status", service.LDAPStatusInput{Enabled: true, Reason: "Re-enable retained binding"}, adminCookie, admin.CSRFToken, tag), 200)
	status(request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery.RecoveryCodes[9]}, nil, "", ""), 401)
	stage = "ldap_unlink"
	_, selfTag = selfReview()
	unlink := service.LDAPIdentityInput{Password: password, Proof: service.LDAPProof{RecoveryCode: recovery.RecoveryCodes[1]}, Reason: "Unlink exact identity"}
	status(request("POST", "/api/v1/account/identity/ldap/unlink", unlink, memberCookie, memberSession.CSRFToken, selfTag), 200)
	status(login("member"), 401)
	_, selfTag = selfReview()
	status(request("POST", "/api/v1/account/identity/ldap/bind", proof("member", recovery.RecoveryCodes[7]), memberCookie, memberSession.CSRFToken, selfTag), 200)
	stage = "ldap_security_change"
	_, tag = configReview()
	config.UserFilter = "(&(objectClass=person)(uid={username}))"
	status(request("PUT", "/api/v1/admin/auth/ldap", config, adminCookie, admin.CSRFToken, tag), 200)
	current, _ := configReview()
	if current.Enabled || current.Verified {
		t.Fatal("security edit retained readiness")
	}
	var count int64
	if db.Model(&entity.LDAPBinding{}).Count(&count).Error != nil || count != 0 {
		t.Fatal("security edit retained bindings")
	}
	status(request("GET", "/api/v1/auth/session", nil, adminCookie, "", ""), 200)
	status(request("GET", "/api/v1/auth/session", nil, memberCookie, "", ""), 200)
	var events []entity.AuditEvent
	if db.Where("action LIKE ?", "%ldap%").Find(&events).Error != nil {
		t.Fatal("audit")
	}
	for _, event := range events {
		if event.DetailsJSON != nil {
			for _, value := range []string{"directory-password", "service-password", peer.endpoint, "cn=service", "12345678-1234-4321"} {
				if strings.Contains(*event.DetailsJSON, value) {
					t.Fatal("private material in audit")
				}
			}
		}
	}
	stage = "complete"
}

type ldapApplicationPeer struct {
	listener         net.Listener
	endpoint         string
	ctx              context.Context
	mu               sync.Mutex
	connections      map[net.Conn]bool
	wg               sync.WaitGroup
	reached, release chan struct{}
}

func ldapNewApplicationPeer(t *testing.T, ctx context.Context) *ldapApplicationPeer {
	t.Helper()
	dir := os.Getenv("ROUTEX_LDAP_TLS_DIR")
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
func (p *ldapApplicationPeer) close(t *testing.T) {
	t.Helper()
	_ = p.listener.Close()
	p.mu.Lock()
	for c := range p.connections {
		_ = c.Close()
	}
	p.mu.Unlock()
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("controlled peer did not join")
	}
}
func (p *ldapApplicationPeer) hold() (chan struct{}, chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reached = make(chan struct{})
	p.release = make(chan struct{})
	return p.reached, p.release
}
func ldapPeerEnvelope(id int64, op *ber.Packet) *ber.Packet {
	v := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
	v.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagInteger, id, ""))
	v.AppendChild(op)
	return v
}
func ldapPeerResult(id int64, tag ber.Tag, code int64) *ber.Packet {
	v := ber.Encode(ber.ClassApplication, ber.TypeConstructed, tag, nil, "")
	v.AppendChild(ber.NewInteger(ber.ClassUniversal, ber.TypePrimitive, ber.TagEnumerated, code, ""))
	v.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	v.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "", ""))
	return ldapPeerEnvelope(id, v)
}
func (p *ldapApplicationPeer) serve(c net.Conn) {
	bound := ""
	selected := ""
	for n := 0; n < 4; n++ {
		packet, e := ber.ReadPacket(c)
		if e != nil || len(packet.Children) != 2 {
			return
		}
		id, ok := packet.Children[0].Value.(int64)
		if !ok {
			return
		}
		op := packet.Children[1]
		if op.Tag == 0 {
			if len(op.Children) != 3 {
				return
			}
			dn, _ := op.Children[1].Value.(string)
			password := op.Children[2].Data.String()
			code := int64(49)
			if n == 0 && dn == "cn=service,dc=example" && password == "service-password" {
				code = 0
				bound = "service"
			}
			if n == 2 && dn == "cn="+selected+",dc=example" && password == "directory-password" {
				code = 0
				bound = selected
			}
			if _, e = c.Write(ldapPeerResult(id, 1, code).Bytes()); e != nil || code != 0 {
				return
			}
			continue
		}
		if op.Tag != 3 || len(op.Children) != 8 {
			return
		}
		base, _ := op.Children[0].Value.(string)
		scope, _ := op.Children[1].Value.(int64)
		size, _ := op.Children[3].Value.(int64)
		if size != 2 || len(op.Children[7].Children) != 1 || op.Children[7].Children[0].Data.String() != "entryUUID" {
			return
		}
		if n == 1 && bound == "service" && base == "dc=example" && scope == 2 {
			f := op.Children[6]
			if f.Tag != 3 || len(f.Children) != 2 {
				return
			}
			selected = f.Children[1].Data.String()
			p.mu.Lock()
			hit, release := p.reached, p.release
			p.reached = nil
			p.release = nil
			p.mu.Unlock()
			if hit != nil {
				close(hit)
				select {
				case <-release:
				case <-p.ctx.Done():
					return
				}
			}
		} else if n != 3 || bound != selected || base != "cn="+selected+",dc=example" || scope != 0 {
			return
		}
		suffix := map[string]string{"admin": "0001", "member": "0002", "unbound": "0003"}[selected]
		if suffix == "" {
			_, _ = c.Write(ldapPeerResult(id, 5, 0).Bytes())
			return
		}
		entry := ber.Encode(ber.ClassApplication, ber.TypeConstructed, 4, nil, "")
		entry.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "cn="+selected+",dc=example", ""))
		attrs := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		attr := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSequence, nil, "")
		attr.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "entryUUID", ""))
		values := ber.Encode(ber.ClassUniversal, ber.TypeConstructed, ber.TagSet, nil, "")
		values.AppendChild(ber.NewString(ber.ClassUniversal, ber.TypePrimitive, ber.TagOctetString, "12345678-1234-4321-abcd-12345678"+suffix, ""))
		attr.AppendChild(values)
		attrs.AppendChild(attr)
		entry.AppendChild(attrs)
		payload := append(ldapPeerEnvelope(id, entry).Bytes(), ldapPeerResult(id, 5, 0).Bytes()...)
		if _, e = c.Write(payload); e != nil {
			return
		}
	}
}
