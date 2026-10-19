//go:build integration && routex_cleanup_evidence && routex_cleanup_evidence_cli && linux

package cleanupevidence

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
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type cleanupCLIConfig struct {
	Source         string `json:"source_commit"`
	Run            string `json:"run_id"`
	UID            int    `json:"host_uid"`
	NormalPassword string `json:"normal_password"`
	F02Password    string `json:"f02_password"`
	NormalBinary   string `json:"normal_binary"`
	NormalSHA256   string `json:"normal_sha256"`
	F02Binary      string `json:"f02_binary"`
	F02SHA256      string `json:"f02_sha256"`
}

// This lane invokes real, separately built Linux commands. Its rows are the
// existing synthetic released-schema fixture, not an API-produced cleanup.
// Explicit selection with missing prerequisites fails; it never skips.
func TestCleanupEvidenceCLIPostgresIntegration(t *testing.T) {
	config := cleanupCLIInputs(t)
	defer func() { config.NormalPassword, config.F02Password = "", "" }()
	runner := cleanupCLIRunner{}
	for _, phase := range []string{"normal", "f02"} {
		t.Run(phase, func(t *testing.T) {
			if runner.unknown {
				t.Fatal("earlier original CLI closure remains unknown")
			}
			password, binary, pin, state := config.NormalPassword, config.NormalBinary, config.NormalSHA256, "acknowledged"
			if phase == "f02" {
				password, binary, pin, state = config.F02Password, config.F02Binary, config.F02SHA256, "pending"
			}
			db := cleanupCLIDatabase(t, config, phase, password)
			e, seeded := fixture(state)
			e.SourceCommit, e.RunID = config.Source, config.Run
			e.AsOf = e.RunStarted.Add(20 * time.Second)
			seedCleanupProjectionSQL(t, db, e, seeded)
			if state == "acknowledged" {
				// The common seed deliberately creates only the original claim audit.
				cleanupSQLCreate(t, db, cleanupSQLAudit(seeded.audits[1]))
			}
			invoke := func(t *testing.T, expected Expectation, suppliedPassword string, accept bool) {
				t.Helper()
				if runner.unknown {
					t.Fatal("earlier original CLI closure remains unknown")
				}
				before := cleanupSQLSnapshot(t, db, e)
				raw := cleanupCLIInput(t, expected, suppliedPassword)
				defer clear(raw)
				var want []byte
				if accept {
					want = cleanupCLIExpected(t, expected, seeded)
				} else {
					want = []byte("{\"result\":\"cleanup_evidence_rejected\",\"runtime_acceptance\":false}\n")
				}
				runner.invoke(t, phase, binary, pin, raw, want, accept)
				if !reflect.DeepEqual(before, cleanupSQLSnapshot(t, db, e)) {
					t.Fatal("actual CLI invocation changed the exact SQL fixture")
				}
			}
			t.Run("exact_original_tuple", func(t *testing.T) { invoke(t, e, password, true) })
			for _, name := range []string{"foreign_source", "foreign_run", "wrong_password", "microsecond_birth"} {
				t.Run(name, func(t *testing.T) {
					foreign, suppliedPassword := e, password
					switch name {
					case "foreign_source":
						foreign.SourceCommit = cleanupCLIForeign(e.SourceCommit)
					case "foreign_run":
						foreign.RunID = cleanupCLIForeign(e.RunID)
					case "wrong_password":
						suppliedPassword = config.F02Password
						if phase == "f02" {
							suppliedPassword = config.NormalPassword
						}
					case "microsecond_birth":
						foreign.ActorBirth = foreign.ActorBirth.Add(time.Microsecond)
					}
					invoke(t, foreign, suppliedPassword, false)
				})
			}
			t.Run("missing_exact_claim_audit", func(t *testing.T) {
				t.Cleanup(func() {
					if runner.unknown {
						t.Error("unknown original CLI closure preserves altered fixture")
						return
					}
					cleanupSQLRetained(t, db, func(tx *gorm.DB) *gorm.DB { return tx.Create(cleanupSQLAudit(seeded.audits[0])) })
				})
				cleanupSQLDelete(t, db, &entity.AuditEvent{}, "id", seeded.audits[0].ID)
				invoke(t, e, password, false)
			})
			t.Run("other_phase_history_refused", func(t *testing.T) {
				otherState := "pending"
				if state == "pending" {
					otherState = "acknowledged"
				}
				_, other := fixture(otherState)
				t.Cleanup(func() {
					if runner.unknown {
						t.Error("unknown original CLI closure preserves altered fixture")
						return
					}
					cleanupSQLRetained(t, db, func(tx *gorm.DB) *gorm.DB {
						return tx.Model(&entity.CredentialPublishedCleanup{}).Where("request_id = ?", e.RequestID).Updates(cleanupSQLCommandState(seeded.published[0].Command))
					})
					if state == "pending" {
						cleanupSQLRetained(t, db, func(tx *gorm.DB) *gorm.DB { return tx.Where("id = ?", other.audits[1].ID).Delete(&entity.AuditEvent{}) })
					} else {
						cleanupSQLRetained(t, db, func(tx *gorm.DB) *gorm.DB { return tx.Create(cleanupSQLAudit(seeded.audits[1])) })
					}
				})
				cleanupSQLUpdate(t, db, &entity.CredentialPublishedCleanup{}, "request_id", e.RequestID, cleanupSQLCommandState(other.published[0].Command))
				if state == "pending" {
					cleanupSQLCreate(t, db, cleanupSQLAudit(other.audits[1]))
				} else {
					cleanupSQLDelete(t, db, &entity.AuditEvent{}, "id", seeded.audits[1].ID)
				}
				invoke(t, e, password, false)
			})
			if state == "pending" {
				t.Run("expired_pending_keeps_original_claim", func(t *testing.T) {
					expired := e
					expired.AsOf = e.RunStarted.Add(50 * time.Second)
					invoke(t, expired, password, true)
				})
			}
		})
	}
}

func cleanupCLIInputs(t *testing.T) cleanupCLIConfig {
	t.Helper()
	if os.Getenv("ROUTEX_TEST_CLEANUP_EVIDENCE_CLI_OWNED") != "1" || os.Getuid() <= 0 || os.Getuid() != os.Geteuid() {
		t.Fatal("one explicitly owned nonroot native CLI lane is required")
	}
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "PG") {
			t.Fatal("PostgreSQL environment overrides are forbidden")
		}
	}
	file, raw := cleanupCLIProtected(t, os.Getenv("ROUTEX_TEST_CLEANUP_EVIDENCE_CLI_CONFIG"), false)
	defer func() {
		if file.Close() != nil {
			t.Error("protected CLI descriptor closure failed")
		}
	}()
	defer clear(raw)
	if !utf8.Valid(raw) {
		t.Fatal("protected CLI configuration is invalid")
	}
	keys := map[string]bool{"source_commit": true, "run_id": true, "host_uid": true, "normal_password": true, "f02_password": true, "normal_binary": true, "normal_sha256": true, "f02_binary": true, "f02_sha256": true}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		t.Fatal("protected CLI configuration grammar is invalid")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || !keys[key] || seen[key] {
			t.Fatal("protected CLI configuration fields are invalid")
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			clear(value)
			t.Fatal("protected CLI configuration value is invalid")
		}
		clear(value)
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(seen) != len(keys) {
		t.Fatal("protected CLI configuration is incomplete")
	}
	if _, err = decoder.Token(); err != io.EOF {
		t.Fatal("protected CLI configuration has trailing data")
	}
	var config cleanupCLIConfig
	if json.Unmarshal(raw, &config) != nil || !sourcePattern.MatchString(config.Source) || !runPattern.MatchString(config.Run) || config.UID != os.Getuid() || !hexPattern.MatchString(config.NormalSHA256) || !hexPattern.MatchString(config.F02SHA256) || config.NormalBinary == config.F02Binary || config.NormalSHA256 == config.F02SHA256 {
		t.Fatal("protected CLI bindings are invalid")
	}
	password := regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)
	if !password.MatchString(config.NormalPassword) || !password.MatchString(config.F02Password) || config.NormalPassword == config.F02Password {
		t.Fatal("distinct bounded private passwords are required")
	}
	return config
}

// Walk directories through no-follow descriptors. Only the final private parent
// must be owned0700; / and normal system ancestors are not re-owned by this test.
func cleanupCLIProtected(t *testing.T, path string, executable bool) (*os.File, []byte) {
	t.Helper()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		t.Fatal("canonical protected CLI path is required")
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("protected CLI root descriptor unavailable")
	}
	for _, part := range parts[:len(parts)-1] {
		next, openErr := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		closeErr := unix.Close(fd)
		if openErr != nil || closeErr != nil {
			if openErr == nil {
				_ = unix.Close(next)
			}
			t.Fatal("protected CLI directory traversal refused")
		}
		fd = next
	}
	defer unix.Close(fd)
	var directory unix.Stat_t
	if unix.Fstat(fd, &directory) != nil || directory.Uid != uint32(os.Getuid()) || directory.Mode&07777 != 0700 {
		t.Fatal("protected CLI parent ownership refused")
	}
	leaf, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal("protected CLI leaf unavailable")
	}
	file := os.NewFile(uintptr(leaf), "controlled-cli-input")
	var before, after, named unix.Stat_t
	mode, bound := uint32(0600), int64(16384)
	if executable {
		mode, bound = 0500, 64<<20
	}
	if unix.Fstat(leaf, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != mode || before.Uid != uint32(os.Getuid()) || before.Nlink != 1 || before.Size <= 0 || before.Size > bound {
		_ = file.Close()
		t.Fatal("protected CLI leaf metadata refused")
	}
	// The stable descriptor is retained through exec; paths cannot substitute a
	// different executable after hash validation. No arbitrary bytes are logged.
	raw, err := io.ReadAll(io.LimitReader(file, bound+1))
	if err != nil || int64(len(raw)) != before.Size || unix.Fstat(leaf, &after) != nil || unix.Fstatat(fd, parts[len(parts)-1], &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !cleanupCLIStable(before, after) || !cleanupCLIStable(before, named) {
		clear(raw)
		_ = file.Close()
		t.Fatal("protected CLI leaf changed during read")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		clear(raw)
		_ = file.Close()
		t.Fatal("protected CLI descriptor rewind refused")
	}
	return file, raw
}

func cleanupCLIStable(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func cleanupCLIDatabase(t *testing.T, config cleanupCLIConfig, phase, password string) *gorm.DB {
	t.Helper()
	name, user := "routex_f28_"+phase+"_"+config.Run, "routex_f28_"+phase
	dsn := fmt.Sprintf("host=postgres port=5432 user=%s password=%s dbname=%s sslmode=disable connect_timeout=2 application_name=routex-f28-cli-test-%s-%s", user, password, name, phase, config.Run[:16])
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{TranslateError: true, Logger: logger.Discard, DisableAutomaticPing: true})
	dsn = ""
	if err != nil {
		if db != nil {
			if partial, poolErr := db.DB(); poolErr == nil && partial.Close() != nil {
				t.Error("partial CLI fixture pool closure failed")
			}
		}
		t.Fatal("owned fixed CLI fixture connection refused")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("owned CLI fixture pool unavailable")
	}
	t.Cleanup(func() {
		if pool.Close() != nil {
			t.Error("owned CLI fixture pool closure failed")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	var who struct{ DatabaseName, DatabaseUser string }
	err = db.WithContext(ctx).Raw("SELECT current_database() AS database_name, current_user AS database_user").Scan(&who).Error
	tables, tableErr := db.WithContext(ctx).Migrator().GetTables()
	cancel()
	if err != nil || tableErr != nil || who.DatabaseName != name || who.DatabaseUser != user || len(tables) != 0 {
		t.Fatal("exact initially empty owned CLI fixture database required")
	}
	if database.Migrate(t.Context(), db) != nil {
		t.Fatal("released CLI fixture migration refused")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	return db
}

func cleanupCLIInput(t *testing.T, e Expectation, password string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"source_commit": e.SourceCommit, "run_id": e.RunID, "database_password": password,
		"creation_request_id": e.CreationRequestID, "request_id": e.RequestID, "actor_id": e.ActorID, "actor_birth": e.ActorBirth,
		"integration_id": e.IntegrationID, "integration_birth": e.IntegrationBirth, "revision_id": e.RevisionID,
		"credential_id": e.CredentialID, "credential_birth": e.CredentialBirth, "creation_birth": e.CreationBirth,
		"creator_process_id": e.CreatorProcessID, "creator_generation": e.CreatorGeneration, "creator_birth": e.CreatorBirth,
		"physical_object": e.PhysicalObject, "operation_proof": e.OperationProof, "reviewed_etag": e.ReviewedETag,
		"reason": e.Reason, "run_started": e.RunStarted, "as_of": e.AsOf, "root_epoch": e.RootEpoch,
	})
	if err != nil || len(raw) > 16384 {
		t.Fatal("bounded CLI stdin encoding refused")
	}
	return raw
}

func cleanupCLIExpected(t *testing.T, e Expectation, seeded snapshot) []byte {
	t.Helper()
	command := seeded.published[0].Command
	presented, observation, observes := command.State, "not_attempted", 0
	if command.State == "acknowledged" {
		observation, observes = "acknowledged", 1
	} else if !e.AsOf.Before(command.Deadline) {
		presented = "unknown"
	}
	// This expected tuple is written from the fixed fixture contract, not Project
	// or derive, so the actual CLI cannot act as its own output oracle.
	value := Evidence{SourceCommit: e.SourceCommit, RunID: e.RunID, PresentedState: presented, SourceProcesses: 1, SourceUses: 1,
		Claim: Claim{RequestID: e.RequestID, CreationRequestID: e.CreationRequestID, State: command.State, FinishedAt: command.FinishedAt,
			StartedAt: command.StartedAt, Deadline: command.Deadline, ObservationState: observation, ObserveAuditCount: observes,
			ClaimAuditCount: 1, RemoteRequestID: e.RequestID, KnownNoEffect: false}}
	raw, err := json.Marshal(struct {
		Result            string   `json:"result"`
		Capability        string   `json:"database_capability"`
		RuntimeAcceptance bool     `json:"runtime_acceptance"`
		Evidence          Evidence `json:"evidence"`
	}{"verified_published_cleanup_evidence", "postgres_private_controlled", false, value})
	if err != nil {
		t.Fatal("safe expected CLI output encoding refused")
	}
	return append(raw, '\n')
}

func cleanupCLIForeign(value string) string {
	if value[0] == '0' {
		return "1" + value[1:]
	}
	return "0" + value[1:]
}

type cleanupCLIBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *cleanupCLIBuffer) Write(raw []byte) (int, error) {
	if len(raw) > b.limit-b.Len() {
		b.overflow = true
		return 0, errors.New("controlled CLI output bound exceeded")
	}
	return b.Buffer.Write(raw)
}

type cleanupCLIRunner struct{ unknown bool }

func (r *cleanupCLIRunner) invoke(t *testing.T, phase, path, pin string, input, want []byte, accept bool) {
	t.Helper()
	if r.unknown {
		t.Fatal("earlier original CLI closure remains unknown")
	}
	file, binary := cleanupCLIProtected(t, path, true)
	defer func() {
		if file.Close() != nil {
			t.Error("protected CLI descriptor closure failed")
		}
	}()
	hash := sha256.Sum256(binary)
	clear(binary)
	if hex.EncodeToString(hash[:]) != pin {
		t.Fatal("fresh native CLI binary hash refused")
	}
	// ExtraFiles pins the validated original executable through exec rather than
	// reopening its pathname. The command receives no application arguments.
	cmd := exec.Command("/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{file}
	cmd.Env = []string{"LANG=C", "TZ=UTC"}
	cmd.Stdin = bytes.NewReader(input) // os/exec owns an actual pipe and EOF.
	stdout, stderr := &cleanupCLIBuffer{limit: 16384}, &cleanupCLIBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	if cmd.Start() != nil {
		r.unknown = true
		t.Fatal("original native CLI start refused; no further dispatch")
	}
	pid := cmd.Process.Pid
	pgid, identityErr := syscall.Getpgid(pid)
	t.Logf("cleanup_cli_original_start phase=%s pid=%d pgid=%d", phase, pid, pgid)
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	var waitErr error
	interrupted := false
	if identityErr == nil && pgid == pid {
		timer := time.NewTimer(15 * time.Second)
		defer timer.Stop()
		select {
		case waitErr = <-waited:
		case <-timer.C:
			interrupted = true
		case <-t.Context().Done():
			interrupted = true
		}
	} else {
		interrupted = true
	}
	if interrupted {
		// Process.Kill targets only Go's owned original process handle. It never
		// signals a discovered/reused PID or an unenumerated process group.
		_ = cmd.Process.Kill()
		select {
		case waitErr = <-waited:
		case <-time.After(5 * time.Second):
			r.unknown = true
			t.Fatal("original native CLI join remains unknown; no further dispatch")
		}
	}
	closed := errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH)
	if !closed {
		r.unknown = true
	}
	exit := cmd.ProcessState.ExitCode()
	t.Logf("cleanup_cli_original_terminal phase=%s pid=%d pgid=%d joined=true exit=%d group_absent=%t interrupted=%t stdout_bytes=%d stderr_bytes=%d", phase, pid, pgid, exit, closed, interrupted, stdout.Len(), stderr.Len())
	var exitErr *exec.ExitError
	if interrupted || !closed || identityErr != nil || pgid != pid || stdout.overflow || stderr.overflow || stderr.Len() != 0 || (waitErr != nil && !errors.As(waitErr, &exitErr)) || (accept && exit != 0) || (!accept && exit != 1) || !bytes.Equal(stdout.Bytes(), want) {
		t.Fatal("actual original native CLI terminal or exact safe output refused")
	}
	// Retain the exact validated child stdout bytes, including terminal LF, in
	// the ordinary GoJSON transcript. Unknown output and stderr are never logged.
	t.Logf("cleanup_cli_validated_stdout_base64=%s", base64.StdEncoding.EncodeToString(stdout.Bytes()))
}
