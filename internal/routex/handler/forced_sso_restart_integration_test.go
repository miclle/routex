package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Two same-source production executables share only persisted authority. The
// controlled IdP/directory remain outside those processes; no Session is seeded.
func testForcedSSOProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("forced SSO restart requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("forced SSO restart requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("forced SSO restart requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	if !filepath.IsAbs(binary) || len(expectedHash) != 64 {
		t.Fatal("root-built forced SSO artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			t.Fatal("forced SSO artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read forced SSO artifact")
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("forced SSO artifact differs from root binding")
		}
	}
	verifyBinary()

	root := bytes.Repeat([]byte{37}, 32)
	defer clear(root)
	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-forced-sso-process-restart-")
	if err != nil {
		t.Fatal("create private restart directory")
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("restrict restart directory")
	}
	oidcWriteTLSMaterial(t, dir)
	var current *oidcRestartProcess
	cleanupKnown := true
	defer func() {
		if current != nil {
			if err := current.stop(); err != nil {
				cleanupKnown = false
				t.Error("owned forced SSO closure failed")
			}
		}
		if cleanupKnown {
			if err := os.RemoveAll(dir); err != nil {
				t.Error("remove closed fixture")
			}
		} else {
			t.Logf("retained private restart fixture for root cleanup: %s", dir)
		}
	}()
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve private port")
	}
	address := reservation.Addr().String()
	if reservation.Close() != nil {
		t.Fatal("release private reservation")
	}
	origin := "http://" + address
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf("addr: %q\ndriver: %s\ndsn: %q\nencryption_key: %q\nallow_private_upstreams: true\nevent_queue_path: \"calls.db\"\n", address, driver, "$"+"{ROUTEX_OAUTH_RESTART_DSN}", "$"+"{ROUTEX_OAUTH_RESTART_ROOT}")
	if os.WriteFile(configPath, []byte(config), 0600) != nil {
		t.Fatal("write private configuration")
	}
	environment := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "ROUTEX_OAUTH_RESTART_DSN" && key != "ROUTEX_OAUTH_RESTART_ROOT" && key != "SSL_CERT_FILE" && key != "SSL_CERT_DIR" && key != "GODEBUG" {
			environment = append(environment, value)
		}
	}
	environment = append(environment, "ROUTEX_OAUTH_RESTART_DSN="+os.Getenv(envName), "ROUTEX_OAUTH_RESTART_ROOT="+base64.StdEncoding.EncodeToString(root))
	var debug []string
	for _, v := range strings.Split(os.Getenv("GODEBUG"), ",") {
		if v != "" && !strings.HasPrefix(v, "x509sslcertoverrideplatform=") {
			debug = append(debug, v)
		}
	}
	debug = append(debug, "x509sslcertoverrideplatform=1")
	environment = append(environment, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(debug, ","))
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	launch := func() {
		t.Helper()
		verifyBinary()
		probe, err := net.Listen("tcp4", address)
		if err != nil {
			t.Fatal("restart port unavailable; refusing to terminate another listener")
		}
		if probe.Close() != nil {
			t.Fatal("close prelaunch probe")
		}
		process, startErr := oidcStartRestartProcess(binary, configPath, dir, guardPath, environment)
		current = process
		if process != nil {
			t.Logf("owned forced SSO production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
		}
		if startErr != nil {
			t.Fatal("start owned production process")
		}
		readyCtx, readyCancel := context.WithTimeout(ctx, 30*time.Second)
		defer readyCancel()
		for {
			if process.hasExited() {
				t.Fatal("production process exited before readiness")
			}
			req, err := http.NewRequestWithContext(readyCtx, "GET", origin+"/health", nil)
			if err != nil {
				t.Fatal("create readiness request")
			}
			response, err := client.Do(req)
			if err == nil {
				body, readErr := io.ReadAll(io.LimitReader(response.Body, 16))
				closeErr := response.Body.Close()
				if response.StatusCode == 200 && readErr == nil && closeErr == nil && string(body) == "ok" && !process.hasExited() {
					return
				}
			}
			select {
			case <-readyCtx.Done():
				t.Fatal("readiness exceeded bound")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	t.Setenv("ROUTEX_OIDC_TLS_DIR", dir)
	provider := oidcNewTLSProvider(t)
	defer provider.server.Close()
	directory := forcedSSONewLDAPPeer(t, ctx, dir)
	defer directory.close(t)
	request := func(callCtx context.Context, method, path string, body any, cookie *http.Cookie, csrf, tag string, ceremony *http.Cookie) *httptest.ResponseRecorder {
		t.Helper()
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal("marshal private fixture body")
		}
		if body == nil {
			raw = nil
		}
		req, e := http.NewRequestWithContext(callCtx, method, origin+path, bytes.NewReader(raw))
		if e != nil {
			t.Fatal("create real HTTP request")
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		if ceremony != nil {
			req.AddCookie(ceremony)
		}
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		if tag != "" {
			req.Header.Set("If-Match", tag)
		}
		response, e := client.Do(req)
		if e != nil {
			t.Fatal("real production HTTP request failed")
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(payload) > 64*1024 {
			t.Fatal("bounded production HTTP response")
		}
		out := httptest.NewRecorder()
		for key, values := range response.Header {
			out.Header()[key] = append([]string(nil), values...)
		}
		out.WriteHeader(response.StatusCode)
		_, _ = out.Write(payload)
		return out
	}
	f := &forcedSSOAcceptance{t: t, ctx: ctx, db: db, request: request, provider: provider, directory: directory}
	launch()
	firstPID := current.pid
	f.configure()
	pending := f.call("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: forcedSSOPassword}, nil, "", "", nil)
	f.status(pending, 202)
	var pendingLocal service.MFALoginChallenge
	if json.Unmarshal(pending.Body.Bytes(), &pendingLocal) != nil {
		t.Fatal("pending local factor")
	}
	saved := f.save(f.localCookie, f.admin.CSRFToken, true)
	if !saved.ReauthenticationRequired || !saved.RuntimeApplied {
		t.Fatal("first executable policy not applied or caller not cut off")
	}
	f.check(f.localCookie, 401)
	f.check(f.ldapCookie, 200)
	f.check(f.oidcCookie, 200)
	emergency, emergencyCookie := f.emergency(2)
	c, callback := f.oidcStart("/api/v1/auth/oidc/start", nil, "", "", map[string]any{})
	f.callback(c, callback)
	pending = f.oidcComplete(c, nil, "")
	f.status(pending, 202)
	var pendingOIDC service.MFALoginChallenge
	if json.Unmarshal(pending.Body.Bytes(), &pendingOIDC) != nil {
		t.Fatal("pending allowed factor")
	}
	var baseline []entity.Session
	for _, cookie := range []*http.Cookie{f.ldapCookie, f.oidcCookie, emergencyCookie} {
		var row entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil {
			t.Fatal("capture durable genuine Session")
		}
		baseline = append(baseline, row)
	}
	var before entity.GovernanceSetting
	if db.Take(&before, 1).Error != nil || !before.ForcedSSOEnabled {
		t.Fatal("persisted enforced policy absent")
	}
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("first forced SSO executable closure unknown")
	}
	current = nil
	launch()
	if current.pid == firstPID {
		t.Fatal("restart reused process identity")
	}
	f.check(f.localCookie, 401)
	f.check(f.ldapCookie, 200)
	f.check(f.oidcCookie, 200)
	f.check(emergencyCookie, 200)
	f.status(f.call("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: forcedSSOPassword}, nil, "", "", nil), 401)
	f.status(f.call("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pendingLocal.ChallengeToken, RecoveryCode: f.recovery[4]}, nil, "", "", nil), 401)
	for _, original := range baseline {
		var retained entity.Session
		if db.Where("id = ?", original.ID).Take(&retained).Error != nil || len(samlMigrationDifferentFields(original, retained)) != 0 {
			t.Fatal("restart changed exact retained Session proof")
		}
	}
	var after entity.GovernanceSetting
	if db.Take(&after, 1).Error != nil || after.AuthenticationPolicyRevision != before.AuthenticationPolicyRevision || after.EmergencyAdminID != before.EmergencyAdminID || after.EmergencyAdminCreatedAt == nil || !after.EmergencyAdminCreatedAt.Equal(*before.EmergencyAdminCreatedAt) || !after.ForcedSSOEnabled {
		t.Fatal("restart lost current policy authority")
	}
	r := f.call("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pendingOIDC.ChallengeToken, RecoveryCode: f.recovery[3]}, nil, "", "", nil)
	f.status(r, 200)
	allowed, allowedCookie := f.session(r)
	f.check(allowedCookie, 200)
	var allowedRow entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(allowedCookie.Value)).Take(&allowedRow).Error != nil || allowedRow.PrimaryMethod != "oidc" {
		t.Fatal("restart replaced allowed factor proof")
	}
	off := f.save(f.ldapCookie, f.ldap.CSRFToken, false)
	if off.ReauthenticationRequired || !off.RuntimeApplied {
		t.Fatal("allowed caller should survive disable")
	}
	f.check(emergencyCookie, 401)
	var disabled entity.GovernanceSetting
	if db.Take(&disabled, 1).Error != nil || disabled.RegistrationEnabled || disabled.AuthenticationPolicyRevision == before.AuthenticationPolicyRevision {
		t.Fatal("disable reopened registration or reused generation")
	}
	// An old local factor cannot revive after off; a newly admitted local Session
	// is deliberately cut off again by the next distinct on generation.
	f.status(f.call("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pendingLocal.ChallengeToken, RecoveryCode: f.recovery[4]}, nil, "", "", nil), 401)
	local, localCookie := f.factor(f.call("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: forcedSSOPassword}, nil, "", "", nil), 4)
	if local.User.ID != f.admin.User.ID {
		t.Fatal("optional local login changed member")
	}
	on := f.save(f.ldapCookie, f.ldap.CSRFToken, true)
	if on.ReauthenticationRequired || !on.RuntimeApplied {
		t.Fatal("allowed policy caller was revoked")
	}
	f.check(localCookie, 401)
	f.check(emergencyCookie, 401)
	f.check(f.ldapCookie, 200)
	f.check(f.oidcCookie, 200)
	f.check(allowedCookie, 200)
	fresh, freshCookie := f.emergency(5)
	if fresh.User.ID != emergency.User.ID || allowed.User.ID != fresh.User.ID {
		t.Fatal("emergency/member namespace changed")
	}
	f.check(freshCookie, 200)
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("second forced SSO executable closure unknown")
	}
	current = nil
	probe, probeErr := net.DialTimeout("tcp4", address, 100*time.Millisecond)
	if probe != nil {
		if probe.Close() != nil {
			t.Error("close final probe")
		}
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) {
		cleanupKnown = false
		t.Fatal("owned production port not refused")
	}
	free, e := net.Listen("tcp4", address)
	if e != nil {
		cleanupKnown = false
		t.Fatal("owned production port not bindable")
	}
	if free.Close() != nil {
		cleanupKnown = false
		t.Fatal("close final bind proof")
	}
}
