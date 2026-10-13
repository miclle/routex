package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const publicationInterruptionRequestID = "d5a1aa9a-7137-4bc6-a3df-01503c3aa125"
const publicationInterruptionReason = "Preserve interrupted original publication without resuming it"
const publicationInterruptionSentinel = "ROUTEX_PUBLICATION_INTERRUPTION_CHILD"

type publicationInterruptionState struct {
	PID               int       `json:"pid"`
	TestBinarySHA256  string    `json:"test_binary_sha256"`
	InstanceID        string    `json:"instance_id"`
	InstanceStartedAt time.Time `json:"instance_started_at"`
	Loads             int64     `json:"loads"`
}
type publicationInterruptionBarrier struct {
	RequestID string    `json:"request_id"`
	Deadline  time.Time `json:"deadline"`
}

// Atomic private files carry only stage/identity facts, never authentication proof.
func publicationInterruptionWrite(path string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".publication-stage-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, path)
}

func publicationInterruptionDatabase(ctx context.Context, db *gorm.DB, driver string) bool {
	if driver != "postgres" && driver != "mysql" {
		return false
	}
	// GORM has no portable current-database accessor. This test-only guard checks
	// the disposable matrix database before any child Service or mutation starts.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	var name string
	return db.WithContext(ctx).Raw(query).Scan(&name).Error == nil && name == "routex_test"
}

func publicationInterruptionExecutableHash() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", errors.New("controlled executable path unavailable")
	}
	info, err := os.Lstat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", errors.New("controlled executable is not a regular executable")
	}
	file, err := os.Open(executable)
	if err != nil {
		return "", errors.New("controlled executable unreadable")
	}
	digest := sha256.New()
	_, readErr := io.Copy(digest, file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return "", errors.New("controlled executable hash unavailable")
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// This is a controlled test executable, not a production binary or environment hook.
// Startup completes before the sole manual operation is allowed to enter the barrier.
func TestRuntimePublicationInterruptionChild(t *testing.T) {
	if os.Getenv(publicationInterruptionSentinel) != "1" {
		t.Skip("owned real-driver interruption child only")
	}
	dir, driver, phase := os.Getenv("ROUTEX_PUBLICATION_INTERRUPTION_DIR"), os.Getenv("ROUTEX_PUBLICATION_INTERRUPTION_DRIVER"), os.Getenv("ROUTEX_PUBLICATION_INTERRUPTION_PHASE")
	info, err := os.Lstat(dir)
	if !filepath.IsAbs(dir) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || phase != "interrupt" && phase != "replay" {
		t.Fatal("private child boundary invalid")
	}
	binaryHash, hashErr := publicationInterruptionExecutableHash()
	if hashErr != nil || binaryHash != os.Getenv("ROUTEX_PUBLICATION_INTERRUPTION_BINARY_SHA256") {
		t.Fatal("controlled executable provenance differs")
	}
	envName := map[string]string{"postgres": "ROUTEX_TEST_POSTGRES_DSN", "mysql": "ROUTEX_TEST_MYSQL_DSN"}[driver]
	if envName == "" || os.Getenv(envName) == "" {
		t.Fatal("selected real-driver database required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	ctx, deadlineCancel := context.WithTimeout(ctx, 90*time.Second)
	defer deadlineCancel()
	db, err := database.Open(ctx, driver, os.Getenv(envName))
	if err != nil {
		t.Fatal("open controlled database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("controlled pool unavailable")
	}
	defer func() {
		if pool.Close() != nil {
			t.Error("close controlled database")
		}
	}()
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	if !publicationInterruptionDatabase(ctx, db, driver) {
		t.Fatal("only dedicated routex_test permitted")
	}
	var armed atomic.Bool
	var loads atomic.Int64
	state := publicationInterruptionState{PID: os.Getpid(), TestBinarySHA256: binaryHash}
	var publishedState atomic.Pointer[publicationInterruptionState]
	if err := db.Callback().Query().Before("gorm:query").Register("test:publication-interruption-source", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Table != "providers" {
			return
		}
		current := loads.Add(1)
		if published := publishedState.Load(); published != nil {
			snapshot := *published
			snapshot.Loads = current
			if publicationInterruptionWrite(filepath.Join(dir, "loads.json"), snapshot) != nil {
				_ = tx.AddError(errors.New("private source witness unavailable"))
				return
			}
		}
		if !armed.CompareAndSwap(true, false) {
			return
		}
		deadline, bounded := tx.Statement.Context.Deadline()
		if !bounded || !time.Now().Before(deadline) || time.Until(deadline) > 3*time.Second {
			_ = tx.AddError(errors.New("original command deadline unavailable"))
			return
		}
		if publicationInterruptionWrite(filepath.Join(dir, "barrier.json"), publicationInterruptionBarrier{RequestID: publicationInterruptionRequestID, Deadline: deadline}) != nil {
			_ = tx.AddError(errors.New("private barrier witness unavailable"))
			return
		}
		<-tx.Statement.Context.Done()
		_ = publicationInterruptionWrite(filepath.Join(dir, "expired.json"), map[string]bool{"expired": true})
		_ = tx.AddError(tx.Statement.Context.Err())
	}); err != nil {
		t.Fatal("install controlled source observer")
	}
	svc, err := service.New(ctx, db, service.WithRuntimeRefreshInterval(time.Hour))
	if err != nil {
		t.Fatal("create controlled Service")
	}
	if svc.StartSystemInstance(ctx, service.SystemInstanceMetadata{Name: "Publication interruption", Hostname: "publication-interruption.invalid", Version: "test", GoVersion: "test", OS: "test", Arch: "test", StoragePath: dir}) != nil {
		t.Fatal("register controlled process birth")
	}
	defer func() {
		svc.StopRuntime()
		shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if svc.StopSystemInstance(shutdown) != nil {
			t.Error("close controlled instance")
		}
	}()
	if svc.StartRuntime(ctx) != nil {
		t.Fatal("start controlled runtime")
	}
	var instance entity.SystemInstance
	if db.Take(&instance, "id = ?", svc.CurrentSystemInstanceID()).Error != nil {
		t.Fatal("read controlled process birth")
	}
	state.InstanceID, state.InstanceStartedAt, state.Loads = instance.ID, instance.StartedAt, loads.Load()
	publishedState.Store(&state)
	if publicationInterruptionWrite(filepath.Join(dir, "loads.json"), state) != nil {
		t.Fatal("record startup source count")
	}
	if phase == "interrupt" {
		armed.Store(true)
	}
	addressRaw, err := os.ReadFile(filepath.Join(dir, "address.txt"))
	if err != nil {
		t.Fatal("read owned listener address")
	}
	address := string(addressRaw)
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		t.Fatal("only loopback child listener permitted")
	}
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("bind owned child listener")
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	server := &http.Server{Handler: router, ReadHeaderTimeout: 3 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	if publicationInterruptionWrite(filepath.Join(dir, "ready.json"), state) != nil {
		_ = server.Close()
		<-served
		t.Fatal("record child readiness")
	}
	<-ctx.Done()
	shutdown, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	if server.Shutdown(shutdown) != nil {
		_ = server.Close()
		t.Error("close controlled HTTP server")
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		t.Error("join controlled HTTP server")
	}
}

func publicationInterruptionStart(executable, dir, guardPath, driver, phase, binaryHash string) (*oidcRestartProcess, error) {
	process, err := oidcNewRestartGuard(guardPath)
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, "-test.run=^TestRuntimePublicationInterruptionChild$", "-test.timeout=100s", "-test.count=1")
	command.Dir = dir
	blocked := map[string]bool{publicationInterruptionSentinel: true, "ROUTEX_PUBLICATION_INTERRUPTION_DIR": true, "ROUTEX_PUBLICATION_INTERRUPTION_DRIVER": true, "ROUTEX_PUBLICATION_INTERRUPTION_PHASE": true, "ROUTEX_PUBLICATION_INTERRUPTION_BINARY_SHA256": true}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if !blocked[key] {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, publicationInterruptionSentinel+"=1", "ROUTEX_PUBLICATION_INTERRUPTION_DIR="+dir, "ROUTEX_PUBLICATION_INTERRUPTION_DRIVER="+driver, "ROUTEX_PUBLICATION_INTERRUPTION_PHASE="+phase, "ROUTEX_PUBLICATION_INTERRUPTION_BINARY_SHA256="+binaryHash)
	log, err := os.OpenFile(filepath.Join(dir, phase+".private.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = process.removeGuard()
		return nil, errors.New("reserve private child output")
	}
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	startErr := command.Start()
	closeErr := log.Close()
	if startErr != nil {
		_ = process.removeGuard()
		return nil, errors.New("start controlled test process")
	}
	process.done = make(chan error, 1)
	process.process = command.Process
	process.pid = command.Process.Pid
	process.pgid = process.pid
	go func() { process.done <- command.Wait() }()
	captureErr := process.writeGuard("started")
	actual, groupErr := syscall.Getpgid(process.pid)
	process.actualPGID = actual
	process.groupConfirmed = groupErr == nil && actual == process.pgid
	confirmErr := process.writeGuard("started")
	if closeErr != nil || captureErr != nil || confirmErr != nil || !process.groupConfirmed {
		return process, errors.New("controlled process ownership unavailable")
	}
	return process, nil
}

// Abrupt termination is intentional, but successful Wait and group disappearance
// remain mandatory. No PID scan, guessed identity or unrelated signal is permitted.
func publicationInterruptionKill(process *oidcRestartProcess, deadline time.Time) error {
	if process == nil || !process.groupConfirmed || !process.guardHealthy || !process.guardMatches() {
		return errors.New("controlled process ownership missing")
	}
	if process.hasExited() {
		return errors.New("controlled process exited before interruption")
	}
	actual, err := syscall.Getpgid(process.pid)
	if err != nil || actual != process.pgid {
		return errors.New("controlled process group changed")
	}
	if syscall.Kill(-process.pgid, syscall.SIGKILL) != nil {
		return errors.New("interrupt exact controlled group")
	}
	sentWithinBudget := time.Now().Before(deadline)
	select {
	case process.exit = <-process.done:
		process.joined = true
	case <-time.After(5 * time.Second):
		return errors.New("interrupted child did not join")
	}
	var exit *exec.ExitError
	if !errors.As(process.exit, &exit) {
		return errors.New("interrupted child lacks signal exit")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		return errors.New("unexpected controlled interruption outcome")
	}
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		err = syscall.Kill(-process.pgid, 0)
		if errors.Is(err, syscall.ESRCH) {
			process.closed = true
			if process.writeGuard("closed") != nil {
				return errors.New("record interrupted group closure")
			}
			if process.removeGuard() != nil {
				return errors.New("release closed interruption guard")
			}
			if !sentWithinBudget {
				return errors.New("interruption exceeded original command budget")
			}
			return nil
		}
		if err != nil {
			return errors.New("controlled group closure unknown")
		}
		time.Sleep(5 * time.Millisecond)
	}
	return errors.New("interrupted group remains")
}

func publicationInterruptionRead(ctx context.Context, path string, process *oidcRestartProcess, target any) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			if json.Unmarshal(raw, target) != nil {
				return errors.New("invalid private child stage")
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return errors.New("read private child stage")
		}
		if process.hasExited() {
			return errors.New("controlled child exited before stage")
		}
		select {
		case <-ctx.Done():
			return errors.New("controlled stage deadline exhausted")
		case <-ticker.C:
		}
	}
}

type publicationInterruptionReply struct {
	status int
	body   []byte
}

func publicationInterruptionHTTP(ctx context.Context, client *http.Client, origin, method, path string, body []byte, cookie *http.Cookie, csrf string) (publicationInterruptionReply, error) {
	request, err := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(body))
	if err != nil {
		return publicationInterruptionReply{}, err
	}
	request.AddCookie(cookie)
	if method == http.MethodPost {
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		return publicationInterruptionReply{}, errors.New("controlled HTTP request unavailable")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 16*1024+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 16*1024 {
		return publicationInterruptionReply{}, errors.New("controlled HTTP response unavailable")
	}
	return publicationInterruptionReply{status: response.StatusCode, body: raw}, nil
}

// The existing registry supplies a dedicated database. This adds no new scenario
// registration and does not use the root's production artifact as the child.
func testRuntimePublicationInterruption(t *testing.T, db *gorm.DB, admin *service.Authentication) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Second)
	defer cancel()
	driver := db.Name()
	if !publicationInterruptionDatabase(ctx, db, driver) {
		t.Fatal("interruption requires dedicated routex_test")
	}
	guardDir := os.Getenv("ROUTEX_TEST_OIDC_GUARD_DIR")
	info, err := os.Lstat(guardDir)
	if !filepath.IsAbs(guardDir) || err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		t.Fatal("interruption requires owned root guard directory")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("locate controlled test executable")
	}
	binaryHash, hashErr := publicationInterruptionExecutableHash()
	if hashErr != nil {
		t.Fatal("pin controlled test executable provenance")
	}
	dir, err := os.MkdirTemp("", "routex-publication-interruption-")
	if err != nil {
		t.Fatal("create private interruption fixture")
	}
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("restrict private interruption fixture")
	}
	var process *oidcRestartProcess
	var dispatched chan error
	closed := true
	defer func() {
		if process != nil && !process.closed {
			if err := process.stop(); err != nil {
				closed = false
				t.Error("controlled child closure unproven")
			}
		}
		if dispatched != nil {
			select {
			case <-dispatched:
			case <-time.After(5 * time.Second):
				closed = false
				t.Error("original controlled HTTP operation did not join")
			}
		}
		if closed {
			if os.RemoveAll(dir) != nil {
				t.Error("remove closed interruption fixture")
			}
		} else {
			t.Logf("retained private interruption fixture: %s", dir)
		}
	}()
	reservation, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve private interruption port")
	}
	address := reservation.Addr().String()
	if reservation.Close() != nil {
		t.Fatal("release private interruption reservation")
	}
	if os.WriteFile(filepath.Join(dir, "address.txt"), []byte(address), 0600) != nil {
		t.Fatal("store private listener address")
	}
	origin := "http://" + address
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	cookie := &http.Cookie{Name: sessionCookie, Value: admin.Token}
	launch := func(phase string) publicationInterruptionState {
		t.Helper()
		var launchErr error
		process, launchErr = publicationInterruptionStart(executable, dir, filepath.Join(guardDir, driver+".json"), driver, phase, binaryHash)
		if launchErr != nil {
			t.Fatal("controlled child start failed")
		}
		var state publicationInterruptionState
		readyCtx, readyCancel := context.WithTimeout(ctx, 15*time.Second)
		defer readyCancel()
		if publicationInterruptionRead(readyCtx, filepath.Join(dir, "ready.json"), process, &state) != nil || state.PID != process.pid || state.TestBinarySHA256 != binaryHash || state.InstanceID == "" || state.InstanceStartedAt.IsZero() {
			t.Fatal("controlled child readiness not proved")
		}
		return state
	}
	session := func() SessionResponse {
		t.Helper()
		reply, err := publicationInterruptionHTTP(ctx, client, origin, http.MethodGet, "/api/v1/auth/session", nil, cookie, "")
		var value SessionResponse
		if err != nil || reply.status != 200 || json.Unmarshal(reply.body, &value) != nil || value.User.ID != admin.User.ID || value.CSRFToken == "" {
			t.Fatal("current original actor Session unavailable")
		}
		return value
	}
	body, _ := json.Marshal(service.RuntimePublicationCommandInput{RequestID: publicationInterruptionRequestID, Reason: publicationInterruptionReason})
	path := "/api/v1/admin/runtime/publication-commands/" + publicationInterruptionRequestID
	first := launch("interrupt")
	current := session()
	dispatched = make(chan error, 1)
	go func() {
		_, err := publicationInterruptionHTTP(ctx, client, origin, http.MethodPost, "/api/v1/admin/runtime/publish", body, cookie, current.CSRFToken)
		dispatched <- err
	}()
	var barrier publicationInterruptionBarrier
	barrierCtx, barrierCancel := context.WithTimeout(ctx, 3*time.Second)
	readErr := publicationInterruptionRead(barrierCtx, filepath.Join(dir, "barrier.json"), process, &barrier)
	barrierCancel()
	if readErr != nil || barrier.RequestID != publicationInterruptionRequestID || !time.Now().Before(barrier.Deadline) {
		t.Fatal("original bounded manual barrier not reached")
	}
	originalCtx, originalCancel := context.WithDeadline(ctx, barrier.Deadline)
	defer originalCancel()
	reply, err := publicationInterruptionHTTP(originalCtx, client, origin, http.MethodGet, path, nil, cookie, "")
	var pending service.RuntimePublicationCommandReceipt
	if err != nil || reply.status != 202 || json.Unmarshal(reply.body, &pending) != nil || pending.Status != "pending" || pending.ClaimInstanceID != first.InstanceID || !pending.ClaimInstanceStartedAt.Equal(first.InstanceStartedAt) || pending.FinalizedAt != nil || pending.FailureCode != nil {
		t.Fatal("independent pending HTTP read failed within original budget")
	}
	var original entity.RuntimePublicationCommand
	if db.WithContext(originalCtx).Take(&original, "request_id = ?", publicationInterruptionRequestID).Error != nil || original.RequestID != pending.RequestID || original.ActorID != pending.ActorID || !original.ActorCreatedAt.Equal(admin.User.CreatedAt) || original.Reason != pending.Reason || original.ClaimInstanceID != pending.ClaimInstanceID || !original.ClaimInstanceStartedAt.Equal(pending.ClaimInstanceStartedAt) || !original.ClaimedAt.Equal(pending.ClaimedAt) || original.Outcome != "claimed" || original.FinalizedAt != nil || original.FailureCode != nil {
		t.Fatal("durable original claim unavailable before interruption")
	}
	audits := func(readCtx context.Context) []entity.AuditEvent {
		t.Helper()
		var all []entity.AuditEvent
		if db.WithContext(readCtx).Where("resource_id = ? AND action IN ?", first.InstanceID, []string{"runtime.publication.claim", "runtime.publication.finalize"}).Order("id ASC").Find(&all).Error != nil {
			t.Fatal("independent claim audit unavailable")
		}
		var selected []entity.AuditEvent
		for _, row := range all {
			var details struct {
				RequestID string `json:"request_id"`
			}
			if row.DetailsJSON != nil && json.Unmarshal([]byte(*row.DetailsJSON), &details) == nil && details.RequestID == publicationInterruptionRequestID {
				selected = append(selected, row)
			}
		}
		return selected
	}
	beforeAudits := audits(originalCtx)
	if len(beforeAudits) != 1 || beforeAudits[0].Action != "runtime.publication.claim" || !time.Now().Before(barrier.Deadline) {
		t.Fatal("claim-only audit not proved in original command budget")
	}
	if _, err := os.Lstat(filepath.Join(dir, "expired.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("command expired before interruption")
	}
	if publicationInterruptionKill(process, barrier.Deadline) != nil {
		closed = false
		t.Fatal("original controlled child interruption did not join")
	}
	select {
	case err := <-dispatched:
		dispatched = nil
		if err == nil {
			t.Fatal("interrupted HTTP operation unexpectedly returned normally")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("original HTTP operation did not join")
	}
	process = nil
	if _, err := os.Lstat(filepath.Join(dir, "expired.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("original command reached context completion before interruption")
	}
	if os.Remove(filepath.Join(dir, "ready.json")) != nil {
		t.Fatal("remove closed first readiness")
	}
	second := launch("replay")
	if second.PID == first.PID || second.InstanceID == first.InstanceID || !second.InstanceStartedAt.After(first.InstanceStartedAt) {
		t.Fatal("new child borrowed original process generation")
	}
	current = session()
	checkReceipt := func(method string, raw []byte) {
		t.Helper()
		reply, err := publicationInterruptionHTTP(ctx, client, origin, method, map[bool]string{true: "/api/v1/admin/runtime/publish", false: path}[method == http.MethodPost], raw, cookie, current.CSRFToken)
		var receipt service.RuntimePublicationCommandReceipt
		if err != nil || reply.status != 202 || json.Unmarshal(reply.body, &receipt) != nil || receipt.Status != "unknown" || receipt.RequestID != pending.RequestID || receipt.ActorID != pending.ActorID || receipt.Reason != pending.Reason || receipt.ClaimInstanceID != pending.ClaimInstanceID || !receipt.ClaimInstanceStartedAt.Equal(pending.ClaimInstanceStartedAt) || !receipt.ClaimedAt.Equal(pending.ClaimedAt) || receipt.FinalizedAt != nil || receipt.FailureCode != nil {
			t.Fatal("new process resumed or rewrote interrupted command")
		}
	}
	checkReceipt(http.MethodGet, nil)
	checkReceipt(http.MethodPost, body)
	changed, _ := json.Marshal(service.RuntimePublicationCommandInput{RequestID: publicationInterruptionRequestID, Reason: "Different reviewed reason"})
	conflict, err := publicationInterruptionHTTP(ctx, client, origin, http.MethodPost, "/api/v1/admin/runtime/publish", changed, cookie, current.CSRFToken)
	if err != nil || conflict.status != 409 {
		t.Fatal("changed intent admitted after interruption")
	}
	denied, err := publicationInterruptionHTTP(ctx, client, origin, http.MethodGet, path, nil, &http.Cookie{Name: sessionCookie, Value: "invalid-current-session"}, "")
	if err != nil || denied.status != 401 {
		t.Fatal("historical interruption bypassed current Session")
	}
	var after entity.RuntimePublicationCommand
	if db.WithContext(ctx).Take(&after, "request_id = ?", publicationInterruptionRequestID).Error != nil || !reflect.DeepEqual(original, after) || !reflect.DeepEqual(beforeAudits, audits(ctx)) {
		t.Fatal("restart altered original command or claim-only audit")
	}
	var loads publicationInterruptionState
	if publicationInterruptionRead(ctx, filepath.Join(dir, "loads.json"), process, &loads) != nil || loads.PID != second.PID || loads.InstanceID != second.InstanceID || loads.Loads != second.Loads {
		t.Fatal("receipt reads or replays dispatched another refresh")
	}
	if process.stop() != nil {
		closed = false
		t.Fatal("replay child closure not proved")
	}
	process = nil
	probe, err := net.Listen("tcp4", address)
	if err != nil {
		t.Fatal("closed child listener is not reusable")
	}
	if probe.Close() != nil {
		t.Fatal("close owned port proof")
	}
}
