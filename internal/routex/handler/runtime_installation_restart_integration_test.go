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
	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The registry owns reset/migration. Root supplies a same-source production
// artifact. Two real processes record their own registered installation births.
func testRuntimeInstallationProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("installation restart requires selected matrix database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, guardErr := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || guardErr != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("installation restart requires owned private guard directory")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	var databaseName string
	// Dedicated-database guard matches the existing real-driver test boundary.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("installation restart requires dedicated routex_test")
	}
	binary := os.Getenv("ROUTEX_TEST_OIDC_BINARY")
	expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	if !filepath.IsAbs(binary) || len(expectedHash) != 64 {
		t.Fatal("root-built installation artifact path and SHA256 are required")
	}
	verifyBinary := func() {
		t.Helper()
		st, err := os.Lstat(binary)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 {
			t.Fatal("installation artifact must be regular executable")
		}
		file, err := os.Open(binary)
		if err != nil {
			t.Fatal("read installation artifact")
		}
		digest := sha256.New()
		_, readErr := io.Copy(digest, file)
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("installation artifact differs from root binding")
		}
	}
	verifyBinary()

	root := bytes.Repeat([]byte{131}, 32)
	defer clear(root)
	store, err := secretstore.New(root)
	if err != nil {
		t.Fatal("create private seed store")
	}
	seed, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal("create seed service")
	}
	router := fox.New()
	New(seed).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"installation-restart@example.invalid","password":"installation-restart-password","name":"Installation restart"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)

	// Never remove descriptors/journal while an owned process or group may live.
	dir, err := os.MkdirTemp("", "routex-installation-process-restart-")
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
				t.Error("owned installation closure failed")
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
			t.Logf("owned installation production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", process.pid, process.pgid, process.actualPGID, process.groupConfirmed)
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
	read := func() service.RuntimeInstallationPage {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "GET", origin+"/api/v1/admin/runtime/installations", nil)
		if err != nil {
			t.Fatal("create installation read")
		}
		req.AddCookie(cookie)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("real installation read")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
		closeErr := response.Body.Close()
		var page service.RuntimeInstallationPage
		if readErr != nil || closeErr != nil || len(raw) > 128*1024 || response.StatusCode != 200 || response.Header.Get("Cache-Control") != "private, no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" || json.Unmarshal(raw, &page) != nil || page.Scope != "single_process_gateway_admission" || page.NextCursor != nil {
			t.Fatal("real installation receipt rejected")
		}
		if bytes.Contains(raw, []byte(cookie.Value)) || bytes.Contains(raw, []byte("source_digest")) {
			t.Fatal("installation response exposed private proof")
		}
		return page
	}
	// Fresh HTTP Session reads supply current authority and CSRF for each process.
	// This extends the existing backend/API restart witness, not artifact-build proof.
	freshPublicationSession := func() SessionResponse {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, "GET", origin+"/api/v1/auth/session", nil)
		if err != nil {
			t.Fatal("create publication Session read")
		}
		req.AddCookie(cookie)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("read real publication Session")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
		closeErr := response.Body.Close()
		var session SessionResponse
		if readErr != nil || closeErr != nil || len(raw) > 64*1024 || response.StatusCode != http.StatusOK || json.Unmarshal(raw, &session) != nil || session.User.ID != admin.User.ID || session.CSRFToken == "" {
			t.Fatal("publication requires fresh original-actor Session")
		}
		return session
	}
	publicationInput := service.RuntimePublicationCommandInput{RequestID: "4a70c292-6872-4e4c-a5ce-a8211a953777", Reason: "Retain original manual publication across process restart"}
	publicationBody, err := json.Marshal(publicationInput)
	if err != nil {
		t.Fatal("encode original publication intent")
	}
	publicationRequest := func(method, csrf string) service.RuntimePublicationCommandReceipt {
		t.Helper()
		endpoint := origin + "/api/v1/admin/runtime/publication-commands/" + publicationInput.RequestID
		var body io.Reader
		if method == http.MethodPost {
			endpoint = origin + "/api/v1/admin/runtime/publish"
			body = bytes.NewReader(publicationBody)
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
		if err != nil {
			t.Fatal("create real publication request")
		}
		req.AddCookie(cookie)
		if method == http.MethodPost {
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", csrf)
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal("perform real publication request")
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
		closeErr := response.Body.Close()
		var receipt service.RuntimePublicationCommandReceipt
		var fields map[string]json.RawMessage
		if readErr != nil || closeErr != nil || len(raw) > 16*1024 || response.StatusCode != http.StatusOK || response.Header.Get("Cache-Control") != "private, no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff" || json.Unmarshal(raw, &fields) != nil || len(fields) != 9 || json.Unmarshal(raw, &receipt) != nil {
			t.Fatal("real terminal publication receipt rejected")
		}
		for _, name := range []string{"request_id", "actor_id", "reason", "claim_instance_id", "claim_instance_started_at", "claimed_at", "finalized_at", "status", "failure_code"} {
			if _, ok := fields[name]; !ok {
				t.Fatal("publication receipt changed its exact wire contract")
			}
		}
		if receipt.RequestID != publicationInput.RequestID || receipt.ActorID != admin.User.ID || receipt.Reason != publicationInput.Reason || receipt.Status != "refresh_returned_nil" || receipt.FinalizedAt == nil || receipt.FailureCode != nil || string(fields["failure_code"]) != "null" || bytes.Contains(raw, []byte(cookie.Value)) || (csrf != "" && bytes.Contains(raw, []byte(csrf))) {
			t.Fatal("publication receipt lost original intent or exposed Session proof")
		}
		return receipt
	}
	publicationAudits := func(row entity.RuntimePublicationCommand) []entity.AuditEvent {
		t.Helper()
		runtimePublicationAuditV105(t, db, row, 1)
		var events []entity.AuditEvent
		if db.Where("action IN ?", []string{"runtime.publication.claim", "runtime.publication.finalize"}).Order("id ASC").Find(&events).Error != nil || len(events) != 2 {
			t.Fatal("read exact original publication audit pair")
		}
		return events
	}
	launch()
	firstPID := current.pid
	first := read()
	if len(first.Items) != 1 || first.Items[0].CurrentServingInstallationMatches == nil || !*first.Items[0].CurrentServingInstallationMatches || first.Items[0].InstanceStatus != "online" {
		t.Fatal("first production observation absent")
	}
	var original entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&original, "id = ?", first.Items[0].ID).Error != nil {
		t.Fatal("capture first production observation")
	}
	firstSession := freshPublicationSession()
	originalReceipt := publicationRequest(http.MethodPost, firstSession.CSRFToken)
	if originalReceipt.ClaimInstanceID != original.InstanceID || !originalReceipt.ClaimInstanceStartedAt.Equal(original.InstanceStartedAt) || originalReceipt.ClaimedAt.Before(original.InstanceStartedAt) || originalReceipt.FinalizedAt.Before(originalReceipt.ClaimedAt) {
		t.Fatal("original command lost its exact serving-process birth")
	}
	originalCommand := runtimePublicationStoredV105(t, db, publicationInput.RequestID)
	var originalActor entity.User
	if db.Take(&originalActor, "id = ?", admin.User.ID).Error != nil || originalCommand.ActorID != originalActor.ID || !originalCommand.ActorCreatedAt.Equal(originalActor.CreatedAt) || originalCommand.ClaimInstanceID != originalReceipt.ClaimInstanceID || !originalCommand.ClaimInstanceStartedAt.Equal(originalReceipt.ClaimInstanceStartedAt) || !originalCommand.ClaimedAt.Equal(originalReceipt.ClaimedAt) || originalCommand.FinalizedAt == nil || !originalCommand.FinalizedAt.Equal(*originalReceipt.FinalizedAt) || originalCommand.Outcome != originalReceipt.Status || originalCommand.FailureCode != nil {
		t.Fatal("real command receipt differs from durable original tuple")
	}
	originalAudits := publicationAudits(originalCommand)
	if !reflect.DeepEqual(originalReceipt, publicationRequest(http.MethodGet, "")) {
		t.Fatal("original HTTP read changed terminal publication receipt")
	}
	firstSession.CSRFToken = ""
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("first production process closure unknown")
	}
	current = nil
	verifyBinary()
	launch()
	if current.pid == firstPID {
		t.Fatal("production restart reused PID")
	}
	second := read()
	if len(second.Items) != 2 {
		t.Fatal("restart observations missing")
	}
	oldSeen, newSeen := false, false
	for _, row := range second.Items {
		if row.ID == first.Items[0].ID {
			oldSeen = row.InstanceID == original.InstanceID && row.InstanceStartedAt.Equal(original.InstanceStartedAt) && row.InstanceStatus == "offline" && row.CurrentServingInstallationMatches == nil && row.FirstObservedAt.Equal(original.FirstObservedAt)
		} else {
			newSeen = row.InstanceID != original.InstanceID && row.InstanceStartedAt.After(original.InstanceStartedAt) && row.InstanceStatus == "online" && row.CurrentServingInstallationMatches != nil && *row.CurrentServingInstallationMatches
		}
	}
	if !oldSeen || !newSeen {
		t.Fatal("restart borrowed another process installation")
	}
	var retained entity.RuntimeInstallationObservation
	if db.Session(&gorm.Session{QueryFields: true}).Take(&retained, "id = ?", original.ID).Error != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("restart rewrote immutable observation")
	}
	resumedSession := freshPublicationSession()
	// HTTP persistence and immutable DB/audit facts are independent of the direct
	// service load-counter test. Background polling prevents an invocation count
	// from being inferred from runtime timestamps or snapshot IDs here.
	if !reflect.DeepEqual(originalReceipt, publicationRequest(http.MethodGet, "")) || !reflect.DeepEqual(originalReceipt, publicationRequest(http.MethodPost, resumedSession.CSRFToken)) {
		t.Fatal("restart replay replaced the original terminal command receipt")
	}
	retainedCommand := runtimePublicationStoredV105(t, db, publicationInput.RequestID)
	if !reflect.DeepEqual(originalCommand, retainedCommand) || !reflect.DeepEqual(originalAudits, publicationAudits(retainedCommand)) {
		t.Fatal("restart replay rewrote original command or publication audit")
	}
	resumedSession.CSRFToken = ""
	if err := current.stop(); err != nil {
		cleanupKnown = false
		t.Fatal("second production process closure unknown")
	}
	current = nil
	probe, probeErr := net.DialTimeout("tcp4", address, 100*time.Millisecond)
	if probe != nil {
		if err := probe.Close(); err != nil {
			t.Error(err)
		}
	}
	if !errors.Is(probeErr, syscall.ECONNREFUSED) {
		cleanupKnown = false
		t.Fatal("owned app port not refused")
	}
	free, bindErr := net.Listen("tcp4", address)
	if bindErr != nil {
		cleanupKnown = false
		t.Fatal("owned app port not bindable")
	}
	if free.Close() != nil {
		cleanupKnown = false
		t.Fatal("close final port probe")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 200)
	if admin.User.ID == "" {
		t.Fatal("seed administrator absent")
	}
}
