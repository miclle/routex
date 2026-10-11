package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This helper is intentionally not registered here. The runner owns the DB,
// same-source binary and guards. Every delivery fact is obtained through the
// actual executable API/native SDK. Catalog-only rows contain no Key or proof.
func testAPIKeyDeliveryProcessRestart(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	db = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	driver := db.Name()
	dsnEnv := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if dsnEnv == "" || os.Getenv(dsnEnv) == "" {
		t.Fatal("delivery restart requires selected database")
	}
	var databaseName string
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	if db.Raw(query).Scan(&databaseName).Error != nil || databaseName != "routex_test" {
		t.Fatal("delivery restart requires dedicated database")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	guardInfo, err := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || err != nil || !guardInfo.IsDir() || guardInfo.Mode()&os.ModeSymlink != 0 || guardInfo.Mode().Perm() != 0700 {
		t.Fatal("delivery restart requires private owner guards")
	}
	guardPath := filepath.Join(guardDir, driver+".json")
	binary, expectedHash := os.Getenv("ROUTEX_TEST_OIDC_BINARY"), os.Getenv("ROUTEX_TEST_OIDC_BINARY_SHA256")
	verifyBinary := func() {
		t.Helper()
		if !filepath.IsAbs(binary) || len(expectedHash) != 64 || strings.ToLower(expectedHash) != expectedHash {
			t.Fatal("delivery restart artifact binding absent")
		}
		if _, e := hex.DecodeString(expectedHash); e != nil {
			t.Fatal("delivery restart artifact digest invalid")
		}
		info, e := os.Lstat(binary)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Size() < 1 || info.Size() > 256<<20 {
			t.Fatal("delivery restart artifact invalid")
		}
		file, e := os.Open(binary)
		if e != nil {
			t.Fatal("read delivery artifact")
		}
		digest := sha256.New()
		n, readErr := io.Copy(digest, io.LimitReader(file, (256<<20)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || n != info.Size() || hex.EncodeToString(digest.Sum(nil)) != expectedHash {
			t.Fatal("delivery artifact changed")
		}
	}
	verifyBinary()
	dir, err := os.MkdirTemp("", "routex-api-key-delivery-restart-")
	if err != nil {
		t.Fatal("create delivery restart fixture")
	}
	cleanupInstalled := false
	var earlyServer *httptest.Server
	defer func() {
		if !cleanupInstalled {
			if earlyServer != nil {
				earlyServer.Close()
			}
			if e := os.RemoveAll(dir); e != nil {
				t.Error("remove unlaunched delivery fixture")
			}
		}
	}()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("restrict delivery fixture")
	}
	oidcWriteTLSMaterial(t, dir)
	certificate, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem"))
	if err != nil {
		t.Fatal("load controlled delivery TLS")
	}
	peer := &deliveryFixturePeer{t: t, db: db, probes: map[string]map[string]string{}, values: map[string]string{}, policies: map[string]string{}, roles: map[string]map[string]any{}}
	hold := &deliveryRestartBodyHold{entered: make(chan struct{}), joined: make(chan struct{}), release: make(chan struct{})}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.Header.Get("X-Vault-Token") == deliveryFixtureWriter && strings.HasPrefix(r.URL.Path, "/v1/kv/data/applications/vap_") && hold.armed.CompareAndSwap(true, false) {
			defer close(hold.joined)
			peer.serve(&deliveryRestartBodyWriter{ResponseWriter: w, ctx: r.Context(), hold: hold, t: t}, r)
			return
		}
		peer.serve(w, r)
	}))
	earlyServer = server
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	peer.Server = server
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		server.Close()
		t.Fatal("reserve delivery application port")
	}
	address := reservation.Addr().String()
	if reservation.Close() != nil {
		server.Close()
		t.Fatal("close delivery reservation")
	}
	root := bytes.Repeat([]byte{137}, 32)
	defer clear(root)
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf("addr: %q\ndriver: %s\ndsn: %q\nencryption_key: %q\nallow_private_upstreams: true\nevent_queue_path: \"calls.db\"\n", address, driver, "${ROUTEX_DELIVERY_RESTART_DSN}", "${ROUTEX_DELIVERY_RESTART_ROOT}")
	if os.WriteFile(configPath, []byte(config), 0600) != nil {
		server.Close()
		t.Fatal("write delivery application configuration")
	}
	var environment []string
	blocked := map[string]bool{"SSL_CERT_FILE": true, "SSL_CERT_DIR": true, "GODEBUG": true, "ROUTEX_DELIVERY_RESTART_DSN": true, "ROUTEX_DELIVERY_RESTART_ROOT": true}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !blocked[key] {
			environment = append(environment, value)
		}
	}
	var debug []string
	for _, value := range strings.Split(os.Getenv("GODEBUG"), ",") {
		if value != "" && !strings.HasPrefix(value, "x509sslcertoverrideplatform=") {
			debug = append(debug, value)
		}
	}
	debug = append(debug, "x509sslcertoverrideplatform=1")
	environment = append(environment, "SSL_CERT_FILE="+filepath.Join(dir, "ca.pem"), "SSL_CERT_DIR="+filepath.Join(dir, "roots"), "GODEBUG="+strings.Join(debug, ","), "ROUTEX_DELIVERY_RESTART_DSN="+os.Getenv(dsnEnv), "ROUTEX_DELIVERY_RESTART_ROOT="+base64.StdEncoding.EncodeToString(root))
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	f := &deliveryRestartFixture{t: t, ctx: ctx, db: db, peer: peer, origin: "http://" + address, client: client}
	var current *oidcRestartProcess
	var requestJoined chan struct{}
	requestCtx, requestCancel := context.WithCancel(ctx)
	cleanupKnown := true
	defer func() {
		requestCancel()
		hold.close()
		if requestJoined != nil {
			select {
			case <-requestJoined:
			case <-time.After(10 * time.Second):
				cleanupKnown = false
				t.Error("delivery request closure unknown")
			}
		}
		if current != nil {
			if e := current.stop(); e != nil {
				cleanupKnown = false
				t.Error("delivery executable closure unknown")
			}
		}
		server.Close()
		joined := make(chan struct{})
		go func() { peer.active.Wait(); close(joined) }()
		select {
		case <-joined:
		case <-time.After(10 * time.Second):
			cleanupKnown = false
			t.Error("delivery peer closure unknown")
		}
		transport.CloseIdleConnections()
		peer.mu.Lock()
		clear(peer.values)
		clear(peer.probes)
		peer.mu.Unlock()
		if cleanupKnown {
			if e := os.RemoveAll(dir); e != nil {
				t.Error("remove closed delivery fixture")
			}
		} else {
			t.Logf("retained private delivery fixture for root cleanup: %s", dir)
		}
	}()
	cleanupInstalled = true
	launch := func() {
		t.Helper()
		verifyBinary()
		probe, e := net.Listen("tcp4", address)
		if e != nil {
			t.Fatal("delivery port occupied; refusing unrelated listener")
		}
		if probe.Close() != nil {
			t.Fatal("close delivery port probe")
		}
		p, e := oidcStartRestartProcess(binary, configPath, dir, guardPath, environment)
		current = p
		if p != nil {
			t.Logf("owned API-Key delivery production process pid=%d expected_pgid=%d actual_pgid=%d group_confirmed=%t", p.pid, p.pgid, p.actualPGID, p.groupConfirmed)
		}
		if e != nil {
			t.Fatal("start delivery production process")
		}
		ready, readyCancel := context.WithTimeout(ctx, 30*time.Second)
		defer readyCancel()
		for {
			if p.hasExited() {
				t.Fatal("delivery process exited before readiness")
			}
			req, e := http.NewRequestWithContext(ready, "GET", f.origin+"/health", nil)
			if e != nil {
				t.Fatal("create delivery readiness request")
			}
			response, e := client.Do(req)
			if e == nil {
				raw, re := io.ReadAll(io.LimitReader(response.Body, 16))
				ce := response.Body.Close()
				if response.StatusCode == 200 && re == nil && ce == nil && string(raw) == "ok" && !p.hasExited() {
					return
				}
			}
			select {
			case <-ready.Done():
				t.Fatal("delivery readiness exceeded original bound")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	launch()
	firstPID := current.pid
	setup := f.request(deliveryFixtureActor{}, "POST", "/api/v1/setup", "", map[string]string{"email": deliveryFixtureAdminEmail, "password": deliveryFixturePassword, "name": "Delivery restart administrator"})
	expectStatus(t, setup, 201)
	session, cookie := readIdentity(t, setup)
	f.admin = deliveryFixtureActor{session, cookie}
	var sessionBefore entity.Session
	if db.Session(&gorm.Session{QueryFields: true}).Take(&sessionBefore, "token_hash = ?", secret.SHA256Hex(cookie.Value)).Error != nil || sessionBefore.PrimaryMethod != "" {
		t.Fatal("capture genuine local Session")
	}
	f.configure()
	// Catalog-only admission fixture: no managed Key, secret, claim, process,
	// control/isolation/READY, ACK or publication rows are manufactured.
	for _, row := range []any{&entity.Model{ID: deliveryFixtureModel, Status: "active"}, &entity.UserModelGrant{UserID: session.User.ID, ModelID: deliveryFixtureModel}} {
		if db.Create(row).Error != nil {
			t.Fatal("create catalog-only grant fixture")
		}
	}
	base := "/api/v1/keys"
	manual := deliveryFixtureDecode[CreatedKeyResponse](t, f.request(f.admin, "POST", base, "", map[string]any{"name": "Manual persists through delivery restart", "model_ids": []string{deliveryFixtureModel}}), 201)
	if manual.Secret == "" {
		t.Fatal("manual one-time secret missing")
	}
	expectStatus(t, f.request(f.admin, "POST", base+"/"+manual.Key.ID+"/confirm", "", nil), 200)
	var manualBefore entity.APIKey
	if db.Session(&gorm.Session{QueryFields: true}).Take(&manualBefore, "id = ?", manual.Key.ID).Error != nil || manualBefore.DeliveryMode != "manual" || manualBefore.Status != entity.KeyActive || manualBefore.TokenHash != secret.SHA256Hex(manual.Secret) {
		t.Fatal("capture confirmed ordinary manual Key")
	}
	manual.Secret = ""
	target := f.ready(f.admin, base, f.resolve(f.admin, base, "restart"))
	in := f.input(target)
	etag := f.context(f.admin, base).ReviewETag
	requestJoined = make(chan struct{})
	result := make(chan *httptest.ResponseRecorder, 1)
	hold.armed.Store(true)
	go func() {
		defer close(requestJoined)
		result <- f.requestContext(requestCtx, f.admin, "POST", base+"/managed", etag, in, 15*time.Second)
	}()
	select {
	case <-hold.entered:
	case <-ctx.Done():
		t.Fatal("delivery hold exceeded scenario")
	case <-time.After(15 * time.Second):
		t.Fatal("genuine Writer headers not observed")
	}
	select {
	case response := <-result:
		expectStatus(t, response, 503)
	case <-ctx.Done():
		t.Fatal("delivery operation exceeded scenario")
	case <-time.After(15 * time.Second):
		t.Fatal("delivery original budget did not end")
	}
	select {
	case <-requestJoined:
	case <-ctx.Done():
		t.Fatal("original delivery request did not join")
	}
	select {
	case <-hold.joined:
	case <-ctx.Done():
		t.Fatal("held delivery peer did not join")
	case <-time.After(5 * time.Second):
		t.Fatal("held delivery peer closure unknown")
	}
	original := f.command(in.RequestID)
	if original.DeliveryStatus != "pending" || original.RuntimeStatus != "pending" || original.ClaimedAt == nil || original.ClaimProcessID == "" || original.ClaimProcessBirth == nil || original.AcknowledgedAt != nil || original.PublishedAt != nil || original.TerminalAt != nil || original.RevokedAt != nil {
		t.Fatal("mandatory claimed pending checkpoint absent")
	}
	var bindingBefore entity.APIKeyVaultDeliveryBinding
	var pendingBefore entity.APIKey
	var sourceBefore entity.CredentialSourceProcess
	if db.Session(&gorm.Session{QueryFields: true}).Take(&bindingBefore, "id = ?", original.BindingID).Error != nil || bindingBefore.Version != 0 || bindingBefore.AcknowledgedAt != nil {
		t.Fatal("pending binding inferred acknowledgement")
	}
	if db.Session(&gorm.Session{QueryFields: true}).Take(&pendingBefore, "id = ?", original.KeyID).Error != nil || pendingBefore.Status != entity.KeyPending || pendingBefore.DeliveryMode != "managed_vault" || pendingBefore.TokenHash != secret.SHA256Hex(peer.bearer(t, original.KeyID)) {
		t.Fatal("genuine hash-only pending Key absent")
	}
	if db.Session(&gorm.Session{QueryFields: true}).Take(&sourceBefore, "process_id = ?", original.ClaimProcessID).Error != nil || !sourceBefore.Birth.Equal(*original.ClaimProcessBirth) || sourceBefore.ClosedAt != nil {
		t.Fatal("claim lacks live original production process")
	}
	if e := current.stop(); e != nil {
		cleanupKnown = false
		t.Fatal("first delivery production process did not close")
	}
	current = nil
	var closedSource entity.CredentialSourceProcess
	var stopped entity.SystemInstance
	if db.Session(&gorm.Session{QueryFields: true}).Take(&closedSource, "process_id = ?", original.ClaimProcessID).Error != nil || closedSource.ClosedAt == nil || !closedSource.Birth.Equal(sourceBefore.Birth) || db.Take(&stopped, "id = ?", original.ClaimProcessID).Error != nil || stopped.StoppedAt == nil || !stopped.StartedAt.Equal(sourceBefore.Birth) {
		t.Fatal("graceful production closure receipt absent")
	}
	peer.mu.Lock()
	requestsBefore := append([]string(nil), peer.requests...)
	effectsBefore := []int{peer.writes, peer.reads, peer.destroys, peer.probeWrites, peer.probeReads, peer.probeDestroys}
	peer.mu.Unlock()
	if effectsBefore[0] != 1 || effectsBefore[1] != 0 || effectsBefore[2] != 0 {
		t.Fatal("original uncertainty remote effect census wrong")
	}
	launch()
	if current.pid == firstPID {
		t.Fatal("delivery executable restart reused PID")
	}
	var next []entity.SystemInstance
	if db.Where("id <> ? AND stopped_at IS NULL", original.ClaimProcessID).Find(&next).Error != nil || len(next) != 1 || next[0].ID == original.ClaimProcessID || next[0].StartedAt.Equal(sourceBefore.Birth) {
		t.Fatal("second production generation not independently registered")
	}
	var nextSource entity.CredentialSourceProcess
	if db.Take(&nextSource, "process_id = ?", next[0].ID).Error != nil || nextSource.ClosedAt != nil || !nextSource.Birth.Equal(next[0].StartedAt) {
		t.Fatal("second production source identity absent")
	}
	fresh := deliveryFixtureDecode[SessionResponse](t, f.request(f.admin, "GET", "/api/v1/auth/session", "", nil), 200)
	if fresh.User.ID != session.User.ID || fresh.CSRFToken != session.CSRFToken {
		t.Fatal("original local Session changed on process restart")
	}
	f.admin.Session = fresh
	var sessionAfter entity.Session
	var manualAfter entity.APIKey
	if db.Session(&gorm.Session{QueryFields: true}).Take(&sessionAfter, "id = ?", sessionBefore.ID).Error != nil || len(samlMigrationDifferentFields(sessionBefore, sessionAfter)) != 0 {
		t.Fatal("local durable Session altered by delivery restart")
	}
	if db.Session(&gorm.Session{QueryFields: true}).Take(&manualAfter, "id = ?", manualBefore.ID).Error != nil || len(samlMigrationDifferentFields(manualBefore, manualAfter)) != 0 {
		t.Fatal("manual Key metadata altered by delivery restart")
	}
	pending := f.receipt(f.admin, base, in.RequestID)
	if pending.KeyID != original.KeyID || pending.DeliveryStatus != "pending" || pending.KeyStatus != entity.KeyPending || pending.Descriptor != nil {
		t.Fatal("read-only receipt changed pending command")
	}
	f.assertImmutableCommand(original)
	var keyCountBefore, slotCountBefore int64
	if db.Model(&entity.APIKey{}).Where("user_id = ?", session.User.ID).Count(&keyCountBefore).Error != nil || db.Model(&entity.APIKeyDeliverySlot{}).Count(&slotCountBefore).Error != nil {
		t.Fatal("capture retained Key/slot cardinality")
	}
	replay := f.managed(f.admin, base, etag, in, 200)
	if replay.Key.ID != original.KeyID || replay.Key.Status != entity.KeyRevoked || replay.Delivery.DeliveryStatus != "unknown" || replay.Delivery.RuntimeStatus != "unknown" || replay.Delivery.Descriptor != nil {
		t.Fatal("restart replay activated uncertain managed Key")
	}
	terminal := f.command(in.RequestID)
	expected := original
	expected.DeliveryStatus = "unknown"
	expected.RuntimeStatus = "unknown"
	expected.FailureCode = "original_attempt_unrecoverable"
	expected.TerminalAt = terminal.TerminalAt
	expected.RevokedAt = terminal.RevokedAt
	expected.UpdatedAt = terminal.UpdatedAt
	if terminal.TerminalAt == nil || terminal.RevokedAt == nil || !terminal.TerminalAt.Equal(*terminal.RevokedAt) || len(samlMigrationDifferentFields(expected, terminal)) != 0 {
		t.Fatal("restart tombstone changed original claim/intent or omitted exact terminal facts")
	}
	var bindingAfter entity.APIKeyVaultDeliveryBinding
	if db.Session(&gorm.Session{QueryFields: true}).Take(&bindingAfter, "id = ?", original.BindingID).Error != nil || len(samlMigrationDifferentFields(bindingBefore, bindingAfter)) != 0 {
		t.Fatal("unknown restart invented binding proof")
	}
	f.managed(f.admin, base, etag, in, 200)
	f.assertImmutableCommand(terminal)
	changed := in
	changed.Reason = "Different original intent after restart"
	expectStatus(t, f.request(f.admin, "POST", base+"/managed", etag, changed), 409)
	expectStatus(t, f.request(f.admin, "POST", base+"/"+original.KeyID+"/confirm", "", nil), 409)
	receipt := f.receipt(f.admin, base, in.RequestID)
	expectStatus(t, f.request(f.admin, "POST", base+"/"+original.KeyID+"/delivery-reconcile", receipt.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: in.RequestID, Reason: "Unknown delivery cannot activate"}), 409)
	expectStatus(t, f.request(f.admin, "GET", base+"/"+original.KeyID+"/delivery-descriptor", "", nil), 409)
	expectStatus(t, f.native(peer.bearer(t, original.KeyID)), 401)
	var revoked entity.APIKey
	if db.Take(&revoked, "id = ?", original.KeyID).Error != nil || revoked.Status != entity.KeyRevoked || revoked.TokenHash != pendingBefore.TokenHash || !revoked.CreatedAt.Equal(pendingBefore.CreatedAt) {
		t.Fatal("original pending verifier not terminally revoked")
	}
	var commandCount, bindingCount int64
	if db.Model(&entity.APIKeyDeliveryCommand{}).Where("request_id = ?", in.RequestID).Count(&commandCount).Error != nil || commandCount != 1 || db.Model(&entity.APIKeyVaultDeliveryBinding{}).Where("key_id = ?", original.KeyID).Count(&bindingCount).Error != nil || bindingCount != 1 {
		t.Fatal("same-intent restart allocated another command/binding")
	}
	var keyCountAfter, slotCountAfter int64
	if db.Model(&entity.APIKey{}).Where("user_id = ?", session.User.ID).Count(&keyCountAfter).Error != nil || db.Model(&entity.APIKeyDeliverySlot{}).Count(&slotCountAfter).Error != nil || keyCountAfter != keyCountBefore || slotCountAfter != slotCountBefore {
		t.Fatal("restart replay allocated another Key or slot")
	}
	f.assertImmutableCommand(terminal)
	peer.mu.Lock()
	requestsAfter := append([]string(nil), peer.requests...)
	effectsAfter := []int{peer.writes, peer.reads, peer.destroys, peer.probeWrites, peer.probeReads, peer.probeDestroys}
	peer.mu.Unlock()
	if !reflect.DeepEqual(requestsBefore, requestsAfter) || !reflect.DeepEqual(effectsBefore, effectsAfter) {
		t.Fatal("restart recovery replayed machine validation/Provisioner/Writer/Reader/cleanup")
	}
	if e := current.stop(); e != nil {
		cleanupKnown = false
		t.Fatal("second delivery production process did not close")
	}
	current = nil
}

type deliveryRestartBodyHold struct {
	armed                    atomic.Bool
	entered, joined, release chan struct{}
	once                     sync.Once
}

func (h *deliveryRestartBodyHold) close() { h.once.Do(func() { close(h.release) }) }

type deliveryRestartBodyWriter struct {
	http.ResponseWriter
	ctx   context.Context
	hold  *deliveryRestartBodyHold
	t     *testing.T
	wrote bool
}

func (w *deliveryRestartBodyWriter) Write(body []byte) (int, error) {
	if w.wrote {
		w.t.Error("controlled Writer emitted duplicate reply")
		return len(body), nil
	}
	w.wrote = true
	var envelope struct {
		Data struct {
			Version int `json:"version"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data.Version != 1 {
		w.t.Error("controlled Writer did not reach genuine version response")
		w.WriteHeader(500)
		return len(body), nil
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", "64")
	w.Header().Set("Connection", "close")
	w.WriteHeader(200)
	if _, e := w.ResponseWriter.Write([]byte(`{"data":`)); e != nil {
		w.t.Error("controlled Writer body prefix failed")
		return len(body), nil
	}
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		w.t.Error("controlled Writer cannot flush headers")
		return len(body), nil
	}
	flusher.Flush()
	close(w.hold.entered)
	select {
	case <-w.ctx.Done():
	case <-w.hold.release:
	}
	// The genuine version1 envelope is never transmitted. Return success only to
	// avoid misclassifying the deliberately lost reply as a peer fixture error.
	return len(body), nil
}

type deliveryRestartFixture struct {
	t      *testing.T
	ctx    context.Context
	db     *gorm.DB
	peer   *deliveryFixturePeer
	admin  deliveryFixtureActor
	seq    atomic.Uint64
	origin string
	client *http.Client
}

func (f *deliveryRestartFixture) uuid() string {
	return fmt.Sprintf("f2910000-0000-4000-8000-%012x", f.seq.Add(1))
}
func (f *deliveryRestartFixture) request(a deliveryFixtureActor, method, path, etag string, body any) *httptest.ResponseRecorder {
	return f.requestContext(f.ctx, a, method, path, etag, body, 5*time.Second)
}
func (f *deliveryRestartFixture) requestContext(parent context.Context, a deliveryFixtureActor, method, path, etag string, body any, bound time.Duration) *httptest.ResponseRecorder {
	res := httptest.NewRecorder()
	raw := []byte{}
	if body != nil {
		var e error
		raw, e = json.Marshal(body)
		if e != nil {
			f.t.Error("encode delivery HTTP request")
			res.WriteHeader(500)
			return res
		}
	}
	defer clear(raw)
	ctx, cancel := context.WithTimeout(parent, bound)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, f.origin+path, bytes.NewReader(raw))
	if e != nil {
		f.t.Error("create delivery HTTP request")
		res.WriteHeader(500)
		return res
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if a.Cookie != nil {
		req.AddCookie(a.Cookie)
	}
	if method != "GET" {
		req.Header.Set("Origin", f.origin)
		req.Header.Set("X-CSRF-Token", a.Session.CSRFToken)
	}
	if etag != "" {
		req.Header.Set("If-Match", `"`+etag+`"`)
	}
	response, e := f.client.Do(req)
	if e != nil {
		f.t.Error("delivery executable HTTP request unavailable")
		res.WriteHeader(500)
		return res
	}
	value, re := io.ReadAll(io.LimitReader(response.Body, (128<<10)+1))
	ce := response.Body.Close()
	defer clear(value)
	if re != nil || ce != nil || len(value) > 128<<10 {
		f.t.Error("delivery executable HTTP response unavailable")
		res.WriteHeader(500)
		return res
	}
	for key, values := range response.Header {
		res.Header()[key] = append([]string(nil), values...)
	}
	res.WriteHeader(response.StatusCode)
	_, _ = res.Write(value)
	f.peer.mu.Lock()
	leaked := false
	for _, v := range f.peer.values {
		if bytes.Contains(value, []byte(v)) {
			leaked = true
		}
	}
	f.peer.mu.Unlock()
	if leaked {
		f.t.Error("managed bearer escaped process response")
	}
	for _, v := range []string{deliveryFixtureWriter, deliveryFixtureProvisioner, "fixture-infrastructure-writer", "fixture-infrastructure-reader", "private-accessor-", "token_hash", "auth_ciphertext"} {
		if bytes.Contains(value, []byte(v)) {
			f.t.Error("private delivery material escaped process response")
		}
	}
	if path == "/api/v1/auth/session" && (response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Content-Type-Options") != "nosniff") {
		f.t.Error("persisted Session response privacy absent")
	}
	if strings.Contains(path, "api-key-delivery") || strings.Contains(path, "delivery-") || strings.HasSuffix(path, "/managed") {
		if !strings.Contains(response.Header.Get("Cache-Control"), "no-store") || response.Header.Get("X-Content-Type-Options") != "nosniff" {
			f.t.Error("delivery executable response privacy absent")
		}
	}
	return res
}
func (f *deliveryRestartFixture) native(bearer string) *httptest.ResponseRecorder {
	ctx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", f.origin+"/v1/chat/completions", strings.NewReader(`{"model":"delivery-fixture","messages":[{"role":"user","content":"Revoked verifier"}]}`))
	if e != nil {
		f.t.Fatal("create revoked verifier request")
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	response, e := f.client.Do(req)
	if e != nil {
		f.t.Fatal("revoked verifier request unavailable")
	}
	body, re := io.ReadAll(io.LimitReader(response.Body, 4097))
	ce := response.Body.Close()
	defer clear(body)
	if re != nil || ce != nil || len(body) > 4096 || bytes.Contains(body, []byte(bearer)) {
		f.t.Fatal("revoked verifier response invalid")
	}
	res.WriteHeader(response.StatusCode)
	_, _ = res.Write(body)
	return res
}

func (f *deliveryRestartFixture) policy() service.APIKeyDeliveryPolicyView {
	return deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(f.admin, "GET", "/api/v1/admin/api-key-delivery/policy", "", nil), 200)
}

func (f *deliveryRestartFixture) context(a deliveryFixtureActor, base string) service.APIKeyDeliveryContextView {
	return deliveryFixtureDecode[service.APIKeyDeliveryContextView](f.t, f.request(a, "GET", base+"/delivery-context", "", nil), 200)
}

func (f *deliveryRestartFixture) configure() {
	f.t.Helper()
	a := f.admin
	base := "/api/v1/admin/api-key-delivery"
	empty := deliveryFixtureDecode[service.APIKeyCallerTemplateView](f.t, f.request(a, "GET", base+"/caller-template", "", nil), 200)
	if empty.AuthMount != "" || empty.Issuer != "" || empty.JWKSURL != "" || empty.Audience != "" || len(empty.AllowedSubjects) != 0 || empty.TokenTTLSeconds != 0 || empty.TokenMaxTTLSeconds != 0 || empty.Verified || empty.ReviewETag != f.policy().ReviewETag {
		f.t.Fatal("unconfigured template projection drift")
	}
	infraBase := "/api/v1/admin/secrets/integrations"
	page := deliveryFixtureDecode[service.VaultIntegrationPage](f.t, f.request(a, "GET", infraBase, "", nil), 200)
	input := service.VaultConfigInput{RequestID: f.uuid(), Name: "Delivery infrastructure", Descriptor: service.VaultDescriptor{Endpoint: f.peer.Server.URL, Mount: "kv", Prefix: "applications", DataField: "key"}, WriterAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-infrastructure-writer"}, ReaderAuth: service.VaultAuthInput{Action: "replace", Token: "fixture-infrastructure-reader"}, Reason: "Review infrastructure probe identities"}
	saved := deliveryFixtureDecode[service.VaultConfigResult](f.t, f.request(a, "POST", infraBase, page.ReviewETag, input), 200)
	target := infraBase + "/" + saved.IntegrationID
	current := deliveryFixtureDecode[service.VaultIntegrationView](f.t, f.request(a, "GET", target, "", nil), 200)
	probe := deliveryFixtureDecode[service.VaultProbeView](f.t, f.request(a, "POST", target+"/probes/write", current.ReviewETag, service.VaultStageInput{RequestID: f.uuid(), Reason: "Explicit infrastructure write"}), 200)
	finished := deliveryFixtureDecode[service.VaultProbeView](f.t, f.request(a, "POST", target+"/probes/"+probe.ID+"/read", probe.ReviewETag, service.VaultStageInput{RequestID: f.uuid(), Reason: "Explicit infrastructure read and cleanup"}), 200)
	if !finished.Write.Succeeded || !finished.Read.Succeeded || finished.Cleanup.State != "acknowledged" {
		f.t.Fatal("infrastructure probe not established")
	}
	var identities [2]string
	for i, purpose := range []string{"writer", "provisioner"} {
		token := deliveryFixtureWriter
		if i == 1 {
			token = deliveryFixtureProvisioner
		}
		v := deliveryFixtureDecode[service.APIKeyDeliveryIdentityView](f.t, f.request(a, "POST", base+"/identities", f.policy().ReviewETag, map[string]any{"purpose": purpose, "integration_id": saved.IntegrationID, "token": token, "reason": "Configure independent machine identity"}), 200)
		identities[i] = v.ID
	}
	tpl := service.APIKeyCallerTemplateInput{AuthMount: "workload", Issuer: deliveryFixtureIssuer, JWKSURL: deliveryFixtureJWKS, Audience: "routex-app", AllowedSubjects: []string{deliveryFixtureSubject}, TokenTTLSeconds: 120, TokenMaxTTLSeconds: 300, Reason: "Review operator workload trust"}
	deliveryFixtureDecode[service.APIKeyCallerTemplateView](f.t, f.request(a, "PUT", base+"/caller-template", f.policy().ReviewETag, tpl), 200)
	policy := service.APIKeyDeliveryPolicyInput{AllowedModes: []string{"manual"}, DefaultMode: "manual", IntegrationID: saved.IntegrationID, WriterIdentityID: identities[0], ProvisionerIdentityID: identities[1], Reason: "Select reviewed dependencies before managed admission"}
	deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "PUT", base+"/policy", f.policy().ReviewETag, policy), 200)
	verified := deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "POST", base+"/verify", f.policy().ReviewETag, map[string]string{"reason": "Validate actual dedicated machine metadata"}), 200)
	if !verified.ManagedAvailable {
		f.t.Fatal("native dependency validation unavailable")
	}
	policy.AllowedModes = []string{"manual", "managed_vault"}
	policy.Reason = "Explicitly admit managed delivery"
	enabled := deliveryFixtureDecode[service.APIKeyDeliveryPolicyView](f.t, f.request(a, "PUT", base+"/policy", verified.ReviewETag, policy), 200)
	if !enabled.ManagedAvailable {
		f.t.Fatal("managed dependency eligibility lost")
	}
	f.peer.mu.Lock()
	defer f.peer.mu.Unlock()
	if !reflect.DeepEqual(f.peer.requests, []string{"GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self", "GET /v1/auth/token/lookup-self", "POST /v1/sys/capabilities-self"}) {
		f.t.Fatal("native machine validation did not use exactly four metadata requests")
	}
	if f.peer.probeWrites != 1 || f.peer.probeReads != 1 || f.peer.probeDestroys != 1 || f.peer.writes != 0 || f.peer.reads != 0 || f.peer.destroys != 0 {
		f.t.Fatal("infrastructure and delivery roles conflated")
	}
}

func (f *deliveryRestartFixture) resolve(a deliveryFixtureActor, base, environment string) service.APIKeyDeliveryTargetView {
	f.t.Helper()
	ctx := f.context(a, base)
	in := service.APIKeyDeliveryResolveInput{RequestID: f.uuid(), ConsumerID: "", ApplicationName: "Application delivery fixture", Environment: environment, Principal: service.APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: deliveryFixtureSubject}, Reason: "Resolve explicit application environment"}
	target := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 200)
	replay := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 200)
	if target.ID != replay.ID || target.ConsumerID == "" || target.ConsumerID != replay.ConsumerID || target.Status == "ready" {
		f.t.Fatal("empty Consumer allocation or identical intent receipt drift")
	}
	in.ApplicationName = "Changed intent"
	expectStatus(f.t, f.request(a, "POST", base+"/delivery-targets/resolve", ctx.ReviewETag, in), 409)
	return target
}

func (f *deliveryRestartFixture) adminTarget(id string) service.APIKeyDeliveryTargetReviewView {
	return deliveryFixtureDecode[service.APIKeyDeliveryTargetReviewView](f.t, f.request(f.admin, "GET", "/api/v1/admin/api-key-delivery/targets/"+id, "", nil), 200)
}

func (f *deliveryRestartFixture) ready(a deliveryFixtureActor, base string, target service.APIKeyDeliveryTargetView) service.APIKeyDeliveryTargetView {
	f.t.Helper()
	adminBase := "/api/v1/admin/api-key-delivery/targets/" + target.ID
	control := f.adminTarget(target.ID)
	deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "PUT", adminBase+"/approval", control.ReviewETag, service.APIKeyDeliveryApprovalInput{Approved: true, ControlVerified: true, Reason: "Operator confirms exact Principal control"}), 200)
	current := f.adminTarget(target.ID)
	// Control and isolation are distinct, and neither alone can reconcile READY.
	expectStatus(f.t, f.request(f.admin, "POST", adminBase+"/reconcile", current.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: f.uuid(), Reason: "Control alone is insufficient"}), 503)
	deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "PUT", adminBase+"/isolation", current.ReviewETag, service.APIKeyDeliveryIsolationInput{Isolated: true, Reason: "Operator records resource-scoped isolation fact"}), 200)
	current = f.adminTarget(target.ID)
	result := deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(f.admin, "POST", adminBase+"/reconcile", current.ReviewETag, service.APIKeyDeliveryReconcileInput{RequestID: f.uuid(), Reason: "Reconcile exact desired caller grants"}), 200)
	if result.Status != "ready" || !result.ControlVerified || result.Isolation.Status != "operator_attested" {
		f.t.Fatal("genuine metadata reconciliation did not establish READY dependency proof")
	}
	return deliveryFixtureDecode[service.APIKeyDeliveryTargetView](f.t, f.request(a, "GET", base+"/delivery-targets/"+target.ID, "", nil), 200)
}

func (f *deliveryRestartFixture) input(target service.APIKeyDeliveryTargetView) service.ManagedAPIKeyInput {
	return service.ManagedAPIKeyInput{Name: "Managed application Key", ModelIDs: []string{deliveryFixtureModel}, RequestID: f.uuid(), Reason: "Explicit managed issuance", Delivery: service.APIKeyDeliverySelection{ConsumerID: target.ConsumerID, Environment: target.Environment, Principal: service.APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: deliveryFixtureSubject}, TargetReview: target.ReviewETag}}
}

func (f *deliveryRestartFixture) managed(a deliveryFixtureActor, base, etag string, in service.ManagedAPIKeyInput, status int) deliveryFixtureManaged {
	f.t.Helper()
	res := f.request(a, "POST", base+"/managed", etag, in)
	v := deliveryFixtureDecode[deliveryFixtureManaged](f.t, res, status)
	var shape map[string]json.RawMessage
	if json.Unmarshal(res.Body.Bytes(), &shape) != nil || len(shape) != 2 || shape["key"] == nil || shape["delivery"] == nil {
		f.t.Fatal("managed envelope exposed extra member")
	}
	if v.Key.ID == "" || v.Key.ID != v.Delivery.KeyID || v.Key.DeliveryMode != "managed_vault" || res.Header().Get("ETag") != `"`+v.Delivery.ReviewETag+`"` {
		f.t.Fatal("managed metadata/review mismatch")
	}
	return v
}

func (f *deliveryRestartFixture) command(request string) entity.APIKeyDeliveryCommand {
	f.t.Helper()
	var c entity.APIKeyDeliveryCommand
	if e := f.db.Where("request_id = ? AND purpose <> ?", request, "resolve").Take(&c).Error; e != nil {
		f.t.Fatal(e)
	}
	return c
}

func (f *deliveryRestartFixture) receipt(a deliveryFixtureActor, base, request string) deliveryFixtureReceipt {
	return deliveryFixtureDecode[deliveryFixtureReceipt](f.t, f.request(a, "GET", base+"/delivery-receipts/"+request, "", nil), 200)
}

func (f *deliveryRestartFixture) assertImmutableCommand(before entity.APIKeyDeliveryCommand) {
	f.t.Helper()
	after := f.command(before.RequestID)
	if fields := samlMigrationDifferentFields(before, after); len(fields) != 0 {
		f.t.Fatal("same-intent receipt mutated durable command fields", fields)
	}
}
