package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The registry owns reset/migration. Root supplies a same-source production
// artifact. Explicit LDAP rows below are persistence seeds, not directory authentication acceptance.
func testLDAPProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("LDAP restart requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("LDAP restart requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("LDAP restart requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	if !filepath.IsAbs(binary) || len(expectedHash) != 64 {
		t.Fatal("root-built LDAP artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			t.Fatal("LDAP artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read LDAP artifact")
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("LDAP artifact differs from root binding")
		}
	}
	verifyBinary()
	root := make([]byte, 32)
	if _, err := rand.Read(root); err != nil {
		t.Fatal("create restart root")
	}
	defer clear(root)
	store, err := secretstore.New(root)
	if err != nil {
		t.Fatal("create restart store")
	}
	seedService, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal("create seed service")
	}
	seedRouter := fox.New()
	New(seedService).RegisterRoutes(seedRouter)
	setup := identityRequest(seedRouter, "POST", "/api/v1/setup", mfaFixtureBody(SetupRequest{Email: "ldap-restart@example.invalid", Password: "test-only-ldap-restart-password", Name: "LDAP restart administrator"}), nil, "")
	if setup.Code != 201 {
		t.Fatal("restart seed setup failed")
	}
	local, localCookie := readIdentity(t, setup)
	var administrator entity.User
	if db.Where("id = ?", local.User.ID).Take(&administrator).Error != nil {
		t.Fatal("read persisted administrator")
	}
	member := entity.User{ID: "usr_ldap_restart_member", Email: "ldap-restart-member@example.invalid", Name: "LDAP restart member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if db.Create(&member).Error != nil || db.Where("id = ?", member.ID).Take(&member).Error != nil {
		t.Fatal("seed existing admitted member")
	}
	const endpoint = "ldaps://ldap-restart.example.invalid:636"
	configRevision := strings.Repeat("a", 64)
	policyRevision := strings.Repeat("b", 64)
	generation := strings.Repeat("c", 64)
	newBinding := func(user entity.User, raw string) entity.LDAPBinding {
		t.Helper()
		bid, e := id.NewPrefixed("ldb")
		if e != nil {
			t.Fatal("binding ID")
		}
		subject := base64.StdEncoding.EncodeToString([]byte(raw))
		digest, _ := json.Marshal([]string{"ldap.subject.v1", "ldap", "entryUUID", subject})
		row := entity.LDAPBinding{ID: bid, ProviderID: "ldap", IdentityAttribute: "entryUUID", UserID: user.ID, UserCreatedAt: user.CreatedAt, ConfigRevision: configRevision, Subject: subject, SubjectDigest: secret.SHA256Hex(string(digest)), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if db.Create(&row).Error != nil {
			t.Fatal("persist binding")
		}
		return row
	}
	adminBinding := newBinding(administrator, "12345678-1234-4321-abcd-123456780001")
	memberBinding := newBinding(member, "12345678-1234-4321-abcd-123456780002")
	var provider entity.LDAPProvider
	if db.Where("id = ?", "ldap").Take(&provider).Error != nil {
		t.Fatal("missing migrated LDAP singleton")
	}
	ciphertext, err := store.Seal("ldap:ldap:"+generation, "test-only-restart-client-secret")
	if err != nil {
		t.Fatal("seal restart client secret")
	}
	provider.Name = "Restart identity"
	provider.Endpoint = endpoint
	provider.BindDN = "cn=service,dc=example"
	provider.BaseDN = "dc=example"
	provider.UserFilter = "(uid={username})"
	provider.IdentityAttribute = "entryUUID"
	provider.SecretGeneration = generation
	provider.AuthCiphertext = ciphertext
	provider.Enabled = true
	provider.ConfigRevision = configRevision
	provider.PolicyRevision = policyRevision
	provider.ReviewRevision = strings.Repeat("d", 64)
	provider.VerifiedConfigRevision = configRevision
	provider.VerifiedBy = administrator.ID
	provider.VerifiedUserCreatedAt = &administrator.CreatedAt
	provider.VerifiedBindingID = adminBinding.ID
	provider.VerifiedBindingCreatedAt = &adminBinding.CreatedAt
	if db.Save(&provider).Error != nil {
		t.Fatal("seed verified enabled LDAP configuration")
	}
	newLDAPSession := func(user entity.User, binding entity.LDAPBinding) (entity.Session, *http.Cookie) {
		t.Helper()
		token, err := secret.RandomURLSafe(32)
		if err != nil {
			t.Fatal("create transient bearer")
		}
		sessionID, err := id.NewPrefixed("ses")
		if err != nil {
			t.Fatal("create Session identity")
		}
		row := entity.Session{ID: sessionID, UserID: user.ID, TokenHash: secret.SHA256Hex(token), ExpiresAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), PrimaryMethod: "ldap", LDAPBindingID: binding.ID, LDAPBindingCreatedAt: &binding.CreatedAt, LDAPConfigRevision: configRevision, LDAPPolicyRevision: policyRevision, LDAPUserCreatedAt: &user.CreatedAt}
		if db.Create(&row).Error != nil {
			t.Fatal("seed exact LDAP Session")
		}
		return row, &http.Cookie{Name: sessionCookie, Value: token}
	}
	adminLDAP, adminLDAPCookie := newLDAPSession(administrator, adminBinding)
	memberLDAP, memberLDAPCookie := newLDAPSession(member, memberBinding)
	var localRow entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(localCookie.Value)).Take(&localRow).Error != nil || localRow.PrimaryMethod != "" {
		t.Fatal("local seed has unexpected provenance")
	}
	before := []entity.Session{localRow, adminLDAP, memberLDAP}
	for i, original := range before {
		var persisted entity.Session
		if db.Where("id = ?", original.ID).Take(&persisted).Error != nil {
			t.Fatal("capture persisted Session baseline")
		}
		before[i] = persisted
	}
	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-ldap-process-restart-")
	if err != nil {
		t.Fatal("create private restart directory")
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("restrict restart directory")
	}
	var current *oidcRestartProcess
	cleanupKnown := true
	defer func() {
		if current != nil {
			if err := current.stop(); err != nil {
				cleanupKnown = false
				t.Error("owned LDAP closure failed")
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
		if key != "ROUTEX_OAUTH_RESTART_DSN" && key != "ROUTEX_OAUTH_RESTART_ROOT" {
			environment = append(environment, value)
		}
	}
	environment = append(environment, "ROUTEX_OAUTH_RESTART_DSN="+os.Getenv(envName), "ROUTEX_OAUTH_RESTART_ROOT="+base64.StdEncoding.EncodeToString(root))
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
			t.Logf("owned LDAP production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
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
	check := func(cookie *http.Cookie, status int, userID string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "GET", origin+"/api/v1/auth/session", nil)
		if err != nil {
			t.Fatal("create real Session read")
		}
		req.AddCookie(cookie)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("real Session read failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > 4096 || response.StatusCode != status || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("real Session status=%d want=%d", response.StatusCode, status)
		}
		if status == 200 {
			var value SessionResponse
			if json.Unmarshal(body, &value) != nil || value.User.ID != userID || value.CSRFToken != secret.SHA256Hex("routex-csrf:"+cookie.Value) {
				t.Fatal("real Session changed identity or CSRF derivation")
			}
		}
		if bytes.Contains(body, []byte(cookie.Value)) {
			t.Fatal("Session response exposed bearer")
		}
	}
	launch()
	firstPID := current.pid
	check(localCookie, 200, administrator.ID)
	check(adminLDAPCookie, 200, administrator.ID)
	check(memberLDAPCookie, 200, member.ID)
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("first production process did not join and close group")
	}
	current = nil
	launch()
	if current.pid == firstPID {
		t.Fatal("restart did not obtain separate process identity")
	}
	check(localCookie, 200, administrator.ID)
	check(adminLDAPCookie, 200, administrator.ID)
	check(memberLDAPCookie, 200, member.ID)
	for _, original := range before {
		var persisted entity.Session
		if db.Where("id = ?", original.ID).Take(&persisted).Error != nil || !ldapRestartSameSession(persisted, original) {
			t.Fatal("restart changed retained Session/provenance")
		}
	}
	// Exact test-only tampering isolates defensive fresh-read guards. No restore,
	// configuration mutation claim, historical revocation or directory authentication acceptance.
	changedBirth := memberBinding.CreatedAt.Add(time.Microsecond)
	changed := db.Model(&entity.LDAPBinding{}).Where("id = ? AND created_at = ?", memberBinding.ID, memberBinding.CreatedAt).Update("created_at", changedBirth)
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact member binding birth")
	}
	check(memberLDAPCookie, 401, "")
	check(adminLDAPCookie, 200, administrator.ID)
	check(localCookie, 200, administrator.ID)
	changed = db.Model(&entity.LDAPProvider{}).Where("id = ? AND policy_revision = ?", "ldap", policyRevision).Update("policy_revision", strings.Repeat("e", 64))
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact policy revision")
	}
	check(adminLDAPCookie, 401, "")
	check(localCookie, 200, administrator.ID)
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("second production process did not join and close group")
	}
	current = nil
	closed, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("owned listener not released")
	}
	if closed.Close() != nil {
		t.Fatal("close final port proof")
	}
}

func ldapRestartSameSession(a, b entity.Session) bool {
	sameTime := func(x, y *time.Time) bool { return x == nil && y == nil || x != nil && y != nil && x.Equal(*y) }
	return a.ID == b.ID && a.UserID == b.UserID && a.TokenHash == b.TokenHash && a.ExpiresAt.Equal(b.ExpiresAt) && a.CreatedAt.Equal(b.CreatedAt) && a.PrimaryMethod == b.PrimaryMethod && a.LDAPBindingID == b.LDAPBindingID && sameTime(a.LDAPBindingCreatedAt, b.LDAPBindingCreatedAt) && a.LDAPConfigRevision == b.LDAPConfigRevision && a.LDAPPolicyRevision == b.LDAPPolicyRevision && sameTime(a.LDAPUserCreatedAt, b.LDAPUserCreatedAt)
}
