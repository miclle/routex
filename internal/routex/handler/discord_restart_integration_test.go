package handler

import (
	"bytes"
	"context"
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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The registry owns reset/migration. Root supplies a same-source production
// artifact. Identity setup and issuance use genuine TLS OAuth token/profile, consumed Discord ceremonies.
func testDiscordProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("Discord restart requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("Discord restart requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("Discord restart requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	if !filepath.IsAbs(binary) || len(expectedHash) != 64 {
		t.Fatal("root-built Discord artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			t.Fatal("Discord artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read Discord artifact")
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("Discord artifact differs from root binding")
		}
	}
	verifyBinary()

	root := bytes.Repeat([]byte{37}, 32)
	defer clear(root)
	f := newDiscordApplicationFixture(t, db)
	githubSession, githubCookie := f.github.login(t, githubMemberSubject)
	githubBefore := githubAssertPrimary(t, db, githubCookie, githubSession.User.ID)
	var githubProviderBefore entity.NamedIdentityProvider
	if db.Session(&gorm.Session{QueryFields: true}).Take(&githubProviderBefore, "id = ?", "github").Error != nil {
		t.Fatal("capture independent GitHub provider")
	}

	var recovery []string
	f.admin, f.adminCookie, recovery = discordEnableMFA(t, f, f.admin, f.adminCookie)
	factor := discordChallenge(t, f, discordAdminSubject)
	adminDiscord, adminDiscordCookie := discordApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
	pendingFactor := discordChallenge(t, f, discordAdminSubject)
	var factorBefore entity.MFAChallenge
	if db.Session(&gorm.Session{QueryFields: true}).Where("token_hash = ?", secret.SHA256Hex(pendingFactor.ChallengeToken)).Take(&factorBefore).Error != nil || factorBefore.PrimaryMethod != "discord" || factorBefore.NamedIdentityProfileID != discordFixtureProfile {
		t.Fatal("capture genuine pending native MFA proof")
	}

	memberDiscord, memberDiscordCookie := f.login(t, discordMemberSubject)
	if adminDiscord.User.ID != f.admin.User.ID || memberDiscord.User.ID != f.member.User.ID {
		t.Fatal("genuine Discord login changed member")
	}
	cookies := []*http.Cookie{f.adminCookie, f.memberCookie, adminDiscordCookie, memberDiscordCookie, githubCookie}
	var before []entity.Session
	for i, cookie := range cookies {
		var row entity.Session
		if db.Session(&gorm.Session{QueryFields: true}).Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil {
			t.Fatal("read genuinely issued Session")
		}
		if i < 2 {
			if row.PrimaryMethod != "" {
				t.Fatal("local Session gained external provenance")
			}
		} else if i == 4 {
			if !reflect.DeepEqual(row, githubBefore) {
				t.Fatal("independent GitHub Session/proof changed before restart")
			}
		} else if row.PrimaryMethod != "discord" || row.NamedIdentityProviderID != "discord" || row.NamedIdentityProfileID != discordFixtureProfile || row.NamedIdentityBindingID == "" || row.NamedIdentityBindingCreatedAt == nil || row.NamedIdentityUserCreatedAt == nil {
			t.Fatal("genuine Discord Session missing exact provenance")
		}
		before = append(before, row)
	}
	var provider entity.NamedIdentityProvider
	var memberBinding entity.NamedIdentityBinding
	if db.Session(&gorm.Session{QueryFields: true}).Take(&provider, "id = ?", "discord").Error != nil || db.Session(&gorm.Session{QueryFields: true}).Take(&memberBinding, "user_id = ? AND provider_id = ?", f.member.User.ID, "discord").Error != nil {
		t.Fatal("capture exact trust/binding")
	}
	policyRevision := provider.PolicyRevision
	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-discord-process-restart-")
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
				t.Error("owned Discord closure failed")
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
			t.Logf("owned Discord production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
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
	check(f.adminCookie, 200, f.admin.User.ID)
	check(f.memberCookie, 200, f.member.User.ID)
	check(adminDiscordCookie, 200, f.admin.User.ID)
	check(memberDiscordCookie, 200, f.member.User.ID)
	check(githubCookie, 200, githubSession.User.ID)
	// The genuine fixed-host TLS proof is staged immediately before the first stop. Its
	// original expiry is never rewritten; completion after restart must consume it.
	pending := f.begin(t, "/api/v1/auth/discord/start", discordMemberSubject, nil, "", "", "")
	var staged entity.NamedIdentityCeremony
	if db.Session(&gorm.Session{QueryFields: true}).Take(&staged, "cookie_hash = ?", secret.SHA256Hex(pending.cookie.Value)).Error != nil || staged.Status != "verified" || staged.ConsumedAt != nil || !staged.ExpiresAt.After(time.Now()) || staged.ProfileID != discordFixtureProfile || staged.IdentityIssuer != discordFixtureNamespace {
		t.Fatal("capture genuine unconsumed staged identity")
	}
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("first production process did not join and close group")
	}
	current = nil
	launch()
	if current.pid == firstPID {
		t.Fatal("restart did not obtain separate process identity")
	}
	check(f.adminCookie, 200, f.admin.User.ID)
	check(f.memberCookie, 200, f.member.User.ID)
	check(adminDiscordCookie, 200, f.admin.User.ID)
	check(memberDiscordCookie, 200, f.member.User.ID)
	check(githubCookie, 200, githubSession.User.ID)
	for _, want := range before {
		var got entity.Session
		if db.Session(&gorm.Session{QueryFields: true}).Take(&got, "id = ?", want.ID).Error != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("restart changed genuinely issued Session/provenance")
		}
	}
	var retained entity.NamedIdentityCeremony
	if db.Session(&gorm.Session{QueryFields: true}).Take(&retained, "id = ?", staged.ID).Error != nil || !reflect.DeepEqual(retained, staged) {
		t.Fatal("restart changed verified identity proof")
	}
	complete := func(want int) *http.Cookie {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/auth/discord/complete", strings.NewReader("{}"))
		if err != nil {
			t.Fatal("create real Discord completion")
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		// Manually sending Secure cookies over this owned loopback HTTP test checks
		// server persistence only. It is explicitly not browser cookie acceptance.
		req.AddCookie(pending.cookie)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("real Discord completion failed")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > 4096 || response.StatusCode != want || response.Header.Get("Cache-Control") != "private, no-store" {
			t.Fatalf("real completion status=%d want=%d", response.StatusCode, want)
		}
		var cookie *http.Cookie
		for _, c := range response.Cookies() {
			if c.Name == sessionCookie && c.Value != "" && c.MaxAge >= 0 {
				if cookie != nil {
					t.Fatal("duplicate admitted Session cookie")
				}
				cookie = c
			}
		}
		if want == 200 {
			var v SessionResponse
			if json.Unmarshal(body, &v) != nil || v.User.ID != f.member.User.ID || cookie == nil || v.CSRFToken != secret.SHA256Hex("routex-csrf:"+cookie.Value) {
				t.Fatal("restarted completion did not admit exact member")
			}
		} else if cookie != nil {
			t.Fatal("replay issued a Session")
		}
		for _, raw := range []string{pending.cookie.Value, staged.CookieHash, staged.StateHash} {
			if bytes.Contains(body, []byte(raw)) {
				t.Fatal("completion exposed private proof")
			}
		}
		return cookie
	}
	var countBefore int64
	if db.Model(&entity.Session{}).Count(&countBefore).Error != nil {
		t.Fatal("count original Sessions")
	}
	completedCookie := complete(200)
	check(completedCookie, 200, f.member.User.ID)
	complete(401)
	var countAfter int64
	if db.Model(&entity.Session{}).Count(&countAfter).Error != nil || countAfter != countBefore+1 {
		t.Fatal("completion replay changed Session cardinality")
	}
	var consumed entity.NamedIdentityCeremony
	if db.Session(&gorm.Session{QueryFields: true}).Take(&consumed, "id = ?", staged.ID).Error != nil || consumed.Status != "consumed" || consumed.ConsumedAt == nil {
		t.Fatal("completion not durably consumed")
	}
	if consumed.BindingID != memberBinding.ID || consumed.BindingCreatedAt == nil || !consumed.BindingCreatedAt.Equal(memberBinding.CreatedAt) {
		t.Fatal("completion did not retain exact binding")
	}
	consumed.BindingID = staged.BindingID
	consumed.BindingCreatedAt = staged.BindingCreatedAt
	consumed.Status = staged.Status
	consumed.ConsumedAt = staged.ConsumedAt
	if !reflect.DeepEqual(consumed, staged) {
		t.Fatal("completion rewrote verified identity facts")
	}
	// The original genuine primary challenge survives restart without exchanging
	// another code or rewriting its expiry. Recovery proof is consumed once.
	var factorAfter entity.MFAChallenge
	if db.Session(&gorm.Session{QueryFields: true}).Where("token_hash = ?", factorBefore.TokenHash).Take(&factorAfter).Error != nil || !reflect.DeepEqual(factorBefore, factorAfter) {
		t.Fatal("restart rewrote pending native MFA proof")
	}
	factorBody, marshalErr := json.Marshal(MFALoginRequest{ChallengeToken: pendingFactor.ChallengeToken, RecoveryCode: recovery[1]})
	if marshalErr != nil {
		t.Fatal("encode original native MFA proof")
	}
	factorRequest, requestErr := http.NewRequestWithContext(ctx, "POST", origin+"/api/v1/auth/mfa/verify", bytes.NewReader(factorBody))
	if requestErr != nil {
		t.Fatal("create native MFA completion")
	}
	factorRequest.Header.Set("Origin", origin)
	factorRequest.Header.Set("Content-Type", "application/json")
	factorResponse, factorErr := client.Do(factorRequest)
	if factorErr != nil {
		t.Fatal("real native MFA completion failed")
	}
	factorRaw, factorReadErr := io.ReadAll(io.LimitReader(factorResponse.Body, 4097))
	factorCloseErr := factorResponse.Body.Close()
	var admitted SessionResponse
	if factorReadErr != nil || factorCloseErr != nil || len(factorRaw) > 4096 || factorResponse.StatusCode != 200 || json.Unmarshal(factorRaw, &admitted) != nil || admitted.User.ID != f.admin.User.ID {
		t.Fatal("restarted native MFA did not admit exact administrator")
	}
	var factorCookie *http.Cookie
	for _, cookie := range factorResponse.Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" && cookie.MaxAge >= 0 {
			if factorCookie != nil {
				t.Fatal("duplicate native MFA Session cookie")
			}
			factorCookie = cookie
		}
	}
	if factorCookie == nil {
		t.Fatal("native MFA completion lacked Session cookie")
	}
	check(factorCookie, 200, f.admin.User.ID)
	discordAssertPrimary(t, db, factorCookie, f.admin.User.ID)
	var used int64
	if db.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NOT NULL", f.admin.User.ID).Count(&used).Error != nil || used != 2 {
		t.Fatal("restart native MFA recovery consumption changed")
	}
	// Exact test-only tampering isolates fresh provenance fences. It does not
	// claim a management API operation, identity proof or historical revocation.
	changedBirth := memberBinding.CreatedAt.Add(time.Microsecond)
	changed := db.Model(&entity.NamedIdentityBinding{}).Where("id = ? AND created_at = ?", memberBinding.ID, memberBinding.CreatedAt).Update("created_at", changedBirth)
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact member binding birth")
	}
	check(memberDiscordCookie, 401, "")
	check(completedCookie, 401, "")
	check(adminDiscordCookie, 200, f.admin.User.ID)
	check(f.memberCookie, 200, f.member.User.ID)
	check(f.adminCookie, 200, f.admin.User.ID)
	changed = db.Model(&entity.NamedIdentityProvider{}).Where("id = ? AND policy_revision = ?", "discord", policyRevision).Update("policy_revision", strings.Repeat("e", 64))
	if changed.Error != nil || changed.RowsAffected != 1 {
		t.Fatal("mutate exact policy revision")
	}
	check(adminDiscordCookie, 401, "")
	check(factorCookie, 401, "")
	check(githubCookie, 200, githubSession.User.ID)
	var githubProviderAfter entity.NamedIdentityProvider
	if db.Session(&gorm.Session{QueryFields: true}).Take(&githubProviderAfter, "id = ?", "github").Error != nil || !reflect.DeepEqual(githubProviderBefore, githubProviderAfter) {
		t.Fatal("Discord policy tamper changed independent GitHub configuration")
	}

	check(f.adminCookie, 200, f.admin.User.ID)
	check(f.memberCookie, 200, f.member.User.ID)
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
