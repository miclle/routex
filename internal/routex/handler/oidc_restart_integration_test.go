package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
)

// The registry owns reset/migration. Root supplies a same-source production
// artifact. Explicit OIDC rows below are persistence seeds, not IdP acceptance.
func testOIDCProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("OIDC restart requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("OIDC restart requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("OIDC restart requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	if !filepath.IsAbs(binary) || len(expectedHash) != 64 {
		t.Fatal("root-built OIDC artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			t.Fatal("OIDC artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read OIDC artifact")
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("OIDC artifact differs from root binding")
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
	setup := identityRequest(seedRouter, "POST", "/api/v1/setup", mfaFixtureBody(SetupRequest{Email: "oidc-restart@example.invalid", Password: "test-only-oidc-restart-password", Name: "OIDC restart administrator"}), nil, "")
	if setup.Code != 201 {
		t.Fatal("restart seed setup failed")
	}
	local, localCookie := readIdentity(t, setup)
	var administrator entity.User
	if db.Where("id = ?", local.User.ID).Take(&administrator).Error != nil {
		t.Fatal("read persisted administrator")
	}
	member := entity.User{ID: "usr_oidc_restart_member", Email: "oidc-restart-member@example.invalid", Name: "OIDC restart member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if db.Create(&member).Error != nil || db.Where("id = ?", member.ID).Take(&member).Error != nil {
		t.Fatal("seed existing admitted member")
	}
	const issuer = "https://oidc-restart.example.invalid"
	configRevision := strings.Repeat("a", 64)
	policyRevision := strings.Repeat("b", 64)
	generation := strings.Repeat("c", 64)
	newBinding := func(user entity.User, subject string) entity.OIDCBinding {
		t.Helper()
		bindingID, err := id.NewPrefixed("oib")
		if err != nil {
			t.Fatal("create binding identity")
		}
		digest, _ := json.Marshal([]string{"oidc.subject.v1", issuer, subject})
		binding := entity.OIDCBinding{ID: bindingID, UserID: user.ID, UserCreatedAt: user.CreatedAt, ConfigRevision: configRevision, Subject: subject, SubjectDigest: secret.SHA256Hex(string(digest)), CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if db.Create(&binding).Error != nil {
			t.Fatal("seed exact binding")
		}
		return binding
	}
	adminBinding := newBinding(administrator, "restart-admin-subject")
	memberBinding := newBinding(member, "restart-member-subject")
	var provider entity.OIDCProvider
	if db.Where("id = ?", "oidc").Take(&provider).Error != nil {
		t.Fatal("missing migrated OIDC singleton")
	}
	ciphertext, err := store.Seal("oidc:oidc:"+generation, "test-only-restart-client-secret")
	if err != nil {
		t.Fatal("seal restart client secret")
	}
	provider.Name = "Restart identity"
	provider.Issuer = issuer
	provider.ClientID = "restart-client"
	provider.CallbackURL = "https://routex.test/api/v1/auth/oidc/callback"
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
		t.Fatal("seed verified enabled OIDC configuration")
	}
	newOIDCSession := func(user entity.User, binding entity.OIDCBinding) (entity.Session, *http.Cookie) {
		t.Helper()
		token, err := secret.RandomURLSafe(32)
		if err != nil {
			t.Fatal("create transient bearer")
		}
		sessionID, err := id.NewPrefixed("ses")
		if err != nil {
			t.Fatal("create Session identity")
		}
		row := entity.Session{ID: sessionID, UserID: user.ID, TokenHash: secret.SHA256Hex(token), ExpiresAt: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour), CreatedAt: time.Now().UTC().Truncate(time.Microsecond), PrimaryMethod: "oidc", OIDCBindingID: binding.ID, OIDCBindingCreatedAt: &binding.CreatedAt, OIDCConfigRevision: configRevision, OIDCPolicyRevision: policyRevision, OIDCUserCreatedAt: &user.CreatedAt}
		if db.Create(&row).Error != nil {
			t.Fatal("seed exact OIDC Session")
		}
		return row, &http.Cookie{Name: sessionCookie, Value: token}
	}
	adminOIDC, adminOIDCCookie := newOIDCSession(administrator, adminBinding)
	memberOIDC, memberOIDCCookie := newOIDCSession(member, memberBinding)
	var localRow entity.Session
	if db.Where("token_hash = ?", secret.SHA256Hex(localCookie.Value)).Take(&localRow).Error != nil || localRow.PrimaryMethod != "" {
		t.Fatal("local seed has unexpected provenance")
	}
	before := []entity.Session{localRow, adminOIDC, memberOIDC}
	for i, original := range before {
		var persisted entity.Session
		if db.Where("id = ?", original.ID).Take(&persisted).Error != nil {
			t.Fatal("capture persisted Session baseline")
		}
		before[i] = persisted
	}
	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-oidc-process-restart-")
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
				t.Error("owned OIDC closure failed")
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
	config := fmt.Sprintf("addr: %q\ndriver: %s\ndsn: %q\nencryption_key: %q\nallow_private_upstreams: true\nevent_queue_path: \"calls.db\"\n", address, driver, "$"+"{ROUTEX_OIDC_RESTART_DSN}", "$"+"{ROUTEX_OIDC_RESTART_ROOT}")
	if os.WriteFile(configPath, []byte(config), 0600) != nil {
		t.Fatal("write private configuration")
	}
	environment := []string{}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if key != "ROUTEX_OIDC_RESTART_DSN" && key != "ROUTEX_OIDC_RESTART_ROOT" {
			environment = append(environment, value)
		}
	}
	environment = append(environment, "ROUTEX_OIDC_RESTART_DSN="+os.Getenv(envName), "ROUTEX_OIDC_RESTART_ROOT="+base64.StdEncoding.EncodeToString(root))
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
			t.Logf("owned OIDC production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
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
	check(adminOIDCCookie, 200, administrator.ID)
	check(memberOIDCCookie, 200, member.ID)
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
	check(adminOIDCCookie, 200, administrator.ID)
	check(memberOIDCCookie, 200, member.ID)
	for _, original := range before {
		var persisted entity.Session
		if db.Where("id = ?", original.ID).Take(&persisted).Error != nil || !oidcRestartSameSession(persisted, original) {
			t.Fatal("restart changed retained Session/provenance")
		}
	}
	// Exact test-only tampering isolates defensive fresh-read guards. No restore,
	// configuration mutation claim, historical revocation or IdP acceptance.
	changedBirth := memberBinding.CreatedAt.Add(time.Microsecond)
	changed := db.Model(&entity.OIDCBinding{}).Where("id = ? AND created_at = ?", memberBinding.ID, memberBinding.CreatedAt).Update("created_at", changedBirth)
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact member binding birth")
	}
	check(memberOIDCCookie, 401, "")
	check(adminOIDCCookie, 200, administrator.ID)
	check(localCookie, 200, administrator.ID)
	changed = db.Model(&entity.OIDCProvider{}).Where("id = ? AND policy_revision = ?", "oidc", policyRevision).Update("policy_revision", strings.Repeat("e", 64))
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact policy revision")
	}
	check(adminOIDCCookie, 401, "")
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

func oidcRestartSameSession(a, b entity.Session) bool {
	sameTime := func(x, y *time.Time) bool { return x == nil && y == nil || x != nil && y != nil && x.Equal(*y) }
	return a.ID == b.ID && a.UserID == b.UserID && a.TokenHash == b.TokenHash && a.ExpiresAt.Equal(b.ExpiresAt) && a.CreatedAt.Equal(b.CreatedAt) && a.PrimaryMethod == b.PrimaryMethod && a.OIDCBindingID == b.OIDCBindingID && sameTime(a.OIDCBindingCreatedAt, b.OIDCBindingCreatedAt) && a.OIDCConfigRevision == b.OIDCConfigRevision && a.OIDCPolicyRevision == b.OIDCPolicyRevision && sameTime(a.OIDCUserCreatedAt, b.OIDCUserCreatedAt)
}

type oidcRestartProcess struct {
	done           chan error
	process        *os.Process
	groupConfirmed bool
	stopDeadline   time.Time
	pid, pgid      int
	joined         bool
	exit           error
	closed         bool
	actualPGID     int
	guardPath      string
	guard          *os.File
	guardInfo      os.FileInfo
	guardBytes     []byte
	guardHealthy   bool
	guardState     string
}

// The descriptor precedes Start, so even abrupt worker termination blocks reset.
func oidcNewRestartGuard(path string) (*oidcRestartProcess, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, errors.New("reserve OIDC process guard")
	}
	p := &oidcRestartProcess{guardPath: path, guard: file, guardHealthy: true}
	p.guardInfo, err = file.Stat()
	if err != nil {
		p.guardHealthy = false
		_ = file.Close()
		return nil, errors.New("read OIDC process guard")
	}
	if err = p.writeGuard("never_started"); err != nil {
		_ = file.Close()
		return nil, err
	}
	return p, nil
}
func (p *oidcRestartProcess) writeGuard(state string) error {
	record := map[string]any{"state": state, "pid": p.pid, "expected_pgid": p.pgid, "actual_pgid": p.actualPGID, "group_confirmed": p.groupConfirmed, "joined": p.joined, "group_gone": p.closed}
	data, err := json.Marshal(record)
	if err != nil {
		p.guardHealthy = false
		return errors.New("encode OIDC guard")
	}
	data = append(data, '\n')
	if p.guard == nil {
		p.guardHealthy = false
		return errors.New("OIDC guard unavailable")
	}
	if err = p.guard.Truncate(0); err == nil {
		_, err = p.guard.Seek(0, io.SeekStart)
	}
	if err == nil {
		var written int
		written, err = p.guard.Write(data)
		if err == nil && written != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err == nil {
		err = p.guard.Sync()
	}
	if err != nil {
		p.guardHealthy = false
		return errors.New("persist OIDC guard")
	}
	p.guardBytes = data
	p.guardState = state
	if !p.guardMatches() {
		p.guardHealthy = false
		return errors.New("read back OIDC guard")
	}
	return nil
}
func (p *oidcRestartProcess) guardMatches() bool {
	if p.guard == nil {
		return false
	}
	current, err := os.Lstat(p.guardPath)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(p.guardInfo, current) {
		return false
	}
	actual, err := io.ReadAll(io.NewSectionReader(p.guard, 0, 4097))
	return err == nil && len(actual) <= 4096 && bytes.Equal(actual, p.guardBytes)
}
func (p *oidcRestartProcess) removeGuard() error {
	// A write/read/identity failure remains blocking even after later known exit.
	proved := p.pid == 0 && p.guardState == "never_started" || p.pid > 0 && p.closed && p.joined && p.groupConfirmed && p.guardState == "closed"
	if !proved {
		return errors.New("OIDC process closure not proved")
	}
	if !p.guardHealthy || !p.guardMatches() {
		if p.guard != nil {
			_ = p.guard.Close()
			p.guard = nil
		}
		return errors.New("retain uncertain OIDC guard")
	}
	if err := p.guard.Close(); err != nil {
		p.guard = nil
		p.guardHealthy = false
		return errors.New("close OIDC guard")
	}
	p.guard = nil
	if err := os.Remove(p.guardPath); err != nil {
		p.guardHealthy = false
		return errors.New("remove OIDC guard")
	}
	return nil
}
func oidcStartRestartProcess(binary, config, dir, guardPath string, environment []string) (*oidcRestartProcess, error) {
	process, err := oidcNewRestartGuard(guardPath)
	if err != nil {
		return nil, err
	}
	command := exec.Command(binary, "-c", config)
	command.Dir = dir
	command.Env = environment
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = command.Start(); err != nil {
		if closeErr := process.removeGuard(); closeErr != nil {
			return nil, closeErr
		}
		return nil, err
	}
	process.done = make(chan error, 1)
	process.process = command.Process
	process.pid = command.Process.Pid
	process.pgid = command.Process.Pid
	go func() { process.done <- command.Wait() }()
	capturedErr := process.writeGuard("started")
	actual, groupErr := syscall.Getpgid(process.pid)
	if groupErr == nil {
		process.actualPGID = actual
		process.groupConfirmed = actual == process.pgid
	}
	confirmedErr := process.writeGuard("started")
	if capturedErr != nil || confirmedErr != nil {
		return process, errors.New("owned process guard unavailable")
	}
	if groupErr != nil || !process.groupConfirmed {
		return process, errors.New("could not confirm owned group")
	}
	return process, nil
}
func (p *oidcRestartProcess) hasExited() bool {
	if p.joined {
		return true
	}
	select {
	case p.exit = <-p.done:
		p.joined = true
		return true
	default:
		return false
	}
}
func (p *oidcRestartProcess) stop() error {
	if p.closed {
		return nil
	}
	if p.stopDeadline.IsZero() {
		p.stopDeadline = time.Now().Add(45 * time.Second)
	}
	deadline := p.stopDeadline
	if !time.Now().Before(deadline) {
		return errors.New("owned closure deadline exceeded")
	}
	signal := func(value syscall.Signal) error {
		if p.groupConfirmed {
			return syscall.Kill(-p.pgid, value)
		}
		return p.process.Signal(value)
	}
	if !p.hasExited() {
		if err := signal(syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
			return errors.New("owned TERM failed")
		}
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return errors.New("owned closure deadline exceeded")
	}
	grace := time.NewTimer(min(30*time.Second, remaining))
	defer grace.Stop()
	if !p.joined {
		select {
		case p.exit = <-p.done:
			p.joined = true
		case <-grace.C:
			if !time.Now().Before(deadline) {
				return errors.New("owned join deadline exceeded")
			}
			if err := signal(syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && !errors.Is(err, os.ErrProcessDone) {
				return errors.New("owned KILL failed")
			}
			remaining = time.Until(deadline)
			if remaining <= 0 {
				return errors.New("join deadline exceeded")
			}
			select {
			case p.exit = <-p.done:
				p.joined = true
			case <-time.After(remaining):
				return errors.New("owned process did not join")
			}
		}
	}
	if !p.groupConfirmed {
		return errors.New("owned group identity unavailable")
	}
	for time.Now().Before(deadline) {
		err := syscall.Kill(-p.pgid, 0)
		if !time.Now().Before(deadline) {
			return errors.New("owned group closure exceeded deadline")
		}
		if errors.Is(err, syscall.ESRCH) {
			p.closed = true
			if err := p.writeGuard("closed"); err != nil {
				return err
			}
			if err := p.removeGuard(); err != nil {
				return err
			}
			if p.exit != nil {
				return errors.New("production process exited unsuccessfully")
			}
			return nil
		}
		if err != nil {
			return errors.New("owned closure unavailable")
		}
		time.Sleep(min(20*time.Millisecond, max(time.Duration(0), time.Until(deadline))))
	}
	return errors.New("owned group did not close")
}

func TestOIDCRestartGuardFileOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres.json")
	guard, err := oidcNewRestartGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	if !guard.guardMatches() {
		t.Fatal("initial never-started descriptor not read back")
	}
	if duplicate, err := oidcNewRestartGuard(path); err == nil || duplicate != nil {
		t.Fatal("existing owner guard overwritten")
	}
	if err := guard.removeGuard(); err != nil {
		t.Fatal("known never-started guard did not close")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("known never-started guard retained")
	}
	replacement, err := oidcNewRestartGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	foreign := path + ".foreign"
	if err := os.WriteFile(foreign, []byte("foreign owner"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(foreign, path); err != nil {
		t.Fatal(err)
	}
	if replacement.removeGuard() == nil {
		t.Fatal("replaced identity admitted guard removal")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign owner" {
		t.Fatal("foreign guard removed")
	}
}

func TestOIDCRestartGuardFailedWriteRemainsBlocking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mysql.json")
	guard, err := oidcNewRestartGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.guard.Close(); err != nil {
		t.Fatal(err)
	}
	guard.pid = 123
	guard.pgid = 123
	guard.actualPGID = 123
	guard.groupConfirmed = true
	if guard.writeGuard("started") == nil {
		t.Fatal("failed descriptor write admitted")
	}
	if guard.removeGuard() == nil {
		t.Fatal("failed descriptor admitted removal")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("failed descriptor guard no longer blocks reset")
	}
}
