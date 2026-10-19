//go:build linux

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"golang.org/x/sys/unix"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func sample(t *testing.T) (config, baseline) {
	t.Helper()
	at := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	c := config{RunID: strings.Repeat("a", 32), Kind: "orphan", CreationID: "11111111-1111-4111-8111-111111111111", CleanupID: "22222222-2222-4222-8222-222222222222", ActorID: "usr_01j00000000000000000000000", ActorBirth: at, IntegrationID: "vlt_01j00000000000000000000000", IntegrationBirth: at, CredentialID: "crd_01j00000000000000000000000", CredentialBirth: at, CreationBirth: at, RunStarted: at}
	o := entity.CredentialStorageOperation{RequestID: c.CreationID, StorageSource: "vault", State: "orphan", ActorID: c.ActorID, ActorBirth: at, IntegrationID: c.IntegrationID, IntegrationBirth: &at, CredentialID: c.CredentialID, CredentialBirth: at, CreatedAt: at}
	b := baseline{Operations: []entity.CredentialStorageOperation{o}, MigrationDigest: strings.Repeat("a", 64), ReleasedConstraints: true, DatabaseName: "routex_f28_f02_" + c.RunID, DatabaseUser: "routex_f28_f02"}
	return c, b
}

type mockStore struct {
	state    baseline
	created  int
	badAfter bool
}

func (m *mockStore) before(config) (baseline, error) { return m.state, nil }
func (m *mockStore) create(kind string) error {
	if _, _, e := fixture(kind); e != nil {
		return e
	}
	m.created++
	return nil
}
func (m *mockStore) after(config) (baseline, error) {
	b := m.state
	b.ExistingFixture = true
	if m.badAfter {
		b.MigrationDigest = "changed"
	}
	return b, nil
}

func TestFrozenConstraintUsesExactExistingTablesAndPredicate(t *testing.T) {
	for _, test := range []struct{ kind, table string }{{"orphan", "provider_credential_cleanups"}, {"published", "credential_published_cleanups"}} {
		m, _, e := fixture(test.kind)
		if e != nil {
			t.Fatal("fixed mode rejected")
		}
		parsed, e := schema.Parse(m, &sync.Map{}, schema.NamingStrategy{})
		if e != nil || parsed.Table != test.table {
			t.Fatal("wrong table")
		}
		check, ok := parsed.ParseCheckConstraints()[constraintName]
		if !ok || check.Name != constraintName || check.Constraint != "state <> 'acknowledged'" || check.Field.DBName != "state" {
			t.Fatal("wrong check")
		}
	}
	for _, kind := range []string{"", "Published", "provider_credential_cleanups", "DROP", "orphan;"} {
		if _, _, e := fixture(kind); e == nil {
			t.Fatal("generic table/DDL selector accepted")
		}
	}
}
func TestInstallRefusesBothPriorCommandsExtraTargetAndForeignScopeBeforeDDL(t *testing.T) {
	c, b := sample(t)
	m := &mockStore{state: b}
	if e := install(c, m); e != nil || m.created != 1 {
		t.Fatal("fresh bounded fixture refused")
	}
	for _, mutate := range []func(*baseline){func(b *baseline) { b.OrphanCount = 1 }, func(b *baseline) { b.PublishedCount = 1 }, func(b *baseline) { b.Operations = append(b.Operations, b.Operations[0]) }, func(b *baseline) { b.ExistingFixture = true }, func(b *baseline) { b.OtherFixture = true }, func(b *baseline) { b.ReleasedConstraints = false }, func(b *baseline) { b.DatabaseName = "existing_database" }, func(b *baseline) { b.DatabaseUser = "postgres" }} {
		_, before := sample(t)
		mutate(&before)
		m := &mockStore{state: before}
		if install(c, m) == nil || m.created != 0 {
			t.Fatal("unsafe install reached DDL")
		}
	}
}
func TestTargetBirthOwnerStateAndMigrationLedgerStayExact(t *testing.T) {
	c, b := sample(t)
	for _, mutate := range []func(*baseline){func(b *baseline) { b.Operations[0].ActorID = "foreign" }, func(b *baseline) {
		b.Operations[0].CredentialBirth = b.Operations[0].CredentialBirth.Add(time.Microsecond)
	}, func(b *baseline) { b.Operations[0].State = "committed" }, func(b *baseline) { b.Operations[0].IntegrationBirth = nil }} {
		_, before := sample(t)
		mutate(&before)
		m := &mockStore{state: before}
		if install(c, m) == nil || m.created != 0 {
			t.Fatal("foreign target reached DDL")
		}
	}
	m := &mockStore{state: b, badAfter: true}
	if install(c, m) == nil || m.created != 1 {
		t.Fatal("post-DDL ledger change accepted")
	}
	c.Kind = "published"
	b.Operations[0].State = "committed"
	if guard(c, b) != nil {
		t.Fatal("fixed published target refused")
	}
}
func TestConfigIsSourceRunAndNormalSchemaReceiptBound(t *testing.T) {
	c, _ := sample(t)
	oldSource, oldRun, oldReceipt := BuildSource, BuildRunID, BuildNormalSchemaReceiptSHA256
	defer func() { BuildSource, BuildRunID, BuildNormalSchemaReceiptSHA256 = oldSource, oldRun, oldReceipt }()
	BuildSource = strings.Repeat("d", 40)
	BuildRunID = c.RunID
	BuildNormalSchemaReceiptSHA256 = strings.Repeat("c", 64)
	c.SourceCommit = BuildSource
	c.Namespace = "routex-f28-f02-" + c.RunID[:16]
	c.DatabaseContainerID = strings.Repeat("b", 64)
	c.NormalSchemaReceiptSHA256 = BuildNormalSchemaReceiptSHA256
	c.DatabaseCreatedForRun = true
	c.FaultOnly = true
	c.DatabasePassword = strings.Repeat("p", 43)
	raw, _ := json.Marshal(c)
	now := c.RunStarted.Add(time.Second)
	if _, e := decode(raw, now); e != nil {
		t.Fatal("valid protected configuration refused")
	}
	for _, value := range []string{strings.Replace(string(raw), `"kind":"orphan"`, `"Kind":"orphan"`, 1), strings.Replace(string(raw), `"kind":"orphan"`, `"kind":"orphan","kind":"published"`, 1), strings.Replace(string(raw), `"fault_only":true`, `"fault_only":false`, 1), strings.Replace(string(raw), strings.Repeat("c", 64), strings.Repeat("e", 64), 1), string(raw) + `{}`} {
		if _, e := decode([]byte(value), now); e == nil {
			t.Fatal("foreign/ambiguous configuration accepted")
		}
	}
	if _, e := decode(raw, c.RunStarted.Add(901*time.Second)); e == nil {
		t.Fatal("expired fixture accepted")
	}
}
func TestActualProtectedDescriptorGuardRejectsOwnerModeSymlinkHardlink(t *testing.T) {
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil {
		t.Fatal("chmod")
	}
	path := filepath.Join(dir, "config.json")
	if os.WriteFile(path, []byte("controlled"), 0600) != nil {
		t.Fatal("write")
	}
	uid := uint32(os.Geteuid())
	if _, e := privateRead(path, uid); e != nil {
		t.Fatal("owned file rejected")
	}
	if _, e := privateRead(path, uid+1); e == nil {
		t.Fatal("foreign owner accepted")
	}
	if os.Chmod(path, 0644) != nil {
		t.Fatal("chmod")
	}
	if _, e := privateRead(path, uid); e == nil {
		t.Fatal("public mode accepted")
	}
	if os.Chmod(path, 0600) != nil {
		t.Fatal("chmod")
	}
	alias := filepath.Join(dir, "alias")
	if os.Symlink(path, alias) != nil {
		t.Fatal("symlink")
	}
	if _, e := privateRead(alias, uid); e == nil {
		t.Fatal("symlink accepted")
	}
	if os.Link(path, filepath.Join(dir, "hard")) != nil {
		t.Fatal("hardlink")
	}
	if _, e := privateRead(path, uid); e == nil {
		t.Fatal("hardlink accepted")
	}
}
func TestClosedFailuresNeverExposeSyntheticSecrets(t *testing.T) {
	for _, e := range []error{errors.New("password=SECRET SQL PRIVATE_TOKEN"), refusal("SECRET")} {
		raw, _ := json.Marshal(safeFailure(e))
		if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "SQL") {
			t.Fatal("raw error escaped")
		}
	}
	if safeFailure(refusal("constraint"))["stage"] != "constraint" {
		t.Fatal("closed stage lost")
	}
}

func TestCompileBoundRunUIDRejectsAliasesZeroForeignAndMissingBeforeAccess(t *testing.T) {
	oldUID, oldArgs := BuildTrustedHostUID, os.Args
	defer func() { BuildTrustedHostUID, os.Args = oldUID, oldArgs }()
	args := []string{"private-fixture", "install-published"}
	BuildTrustedHostUID = "501"
	kind, uid, err := runIdentity(args, 501)
	if err != nil || kind != "published" || uid != 501 {
		t.Fatal("canonical independently bound owner refused")
	}
	for _, actual := range []int{0, -1, 502} {
		if _, _, err := runIdentity(args, actual); err == nil {
			t.Fatal("foreign actual owner accepted")
		}
	}
	for _, value := range []string{"", "0", "0501", "+501", "501 ", " 501", "0x1f5", "4294967296", "-1", "501\n"} {
		BuildTrustedHostUID = value
		if _, _, err := runIdentity(args, 501); err == nil {
			t.Fatal("noncanonical build owner accepted")
		}
	}
	// This calls the actual entry point. Its fixed native path must never be read
	// for an unset/invalid or actual-OS-foreign compile binding.
	os.Args = args
	for _, value := range []string{"", "0", "0501", strconv.FormatUint(uint64(os.Geteuid())+1, 10)} {
		BuildTrustedHostUID = value
		var stage refusal
		if err := run(); !errors.As(err, &stage) || stage != "argument" {
			t.Fatal("actual run accessed protected input before owner binding")
		}
	}
	oldDriver := BuildDriver
	defer func() { BuildDriver = oldDriver }()
	for _, driver := range []string{"postgres", "mysql"} {
		BuildDriver, BuildTrustedHostUID = driver, "501"
		if _, _, err := runIdentity(args, 501); err != nil {
			t.Fatal("fixed driver refused")
		}
	}
	for _, driver := range []string{"", "MySQL", "mysql:3306"} {
		BuildDriver = driver
		if _, _, err := runIdentity(args, 501); err == nil {
			t.Fatal("unknown driver reached protected input")
		}
	}
	BuildDriver = oldDriver
	BuildTrustedHostUID = "501"
	for _, bad := range [][]string{nil, {"private-fixture"}, {"private-fixture", "install-published", "501"}, {"private-fixture", "install-Published"}} {
		if _, _, err := runIdentity(bad, 501); err == nil {
			t.Fatal("arbitrary command or UID selector accepted")
		}
	}
}

func TestPrivateGORMFactoryRequiresTranslationAndDiscardLogging(t *testing.T) {
	value := privateGORMConfig()
	if value == nil || !value.TranslateError || !value.DisableAutomaticPing || value.Logger != logger.Discard {
		t.Fatal("private database factory lost explicit guards")
	}
	// Model the initialized handle that GORM Connection supplies without
	// opening a database. Independent chains must retain the owned pool but not
	// a previous table, count projection, filters or limits.
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "fixture:fixture@tcp(mysql:3306)/fixture", SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, DryRun: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal("dry-run factory")
	}
	defer func() { pool, _ := db.DB(); _ = pool.Close() }()
	initialized := db.Session(&gorm.Session{Initialized: true})
	store := newGORMStore(initialized)
	first := store.tx.Model(&entity.ProviderCredentialCleanup{}).Where("state = ?", "pending").Limit(1).Find(&[]entity.ProviderCredentialCleanup{})
	second := store.tx.Select("request_id").Order("request_id").Limit(2).Find(&[]entity.CredentialStorageOperation{})
	if first.Error != nil || second.Error != nil || first.Statement.Table != "provider_credential_cleanups" || second.Statement.Table != "credential_storage_operations" || strings.Contains(second.Statement.SQL.String(), "state") || store.tx.Statement.Model != nil || second.Statement.ConnPool != initialized.Statement.ConnPool {
		t.Fatal("owned connection query state leaked between frozen projections")
	}
}

func TestActualProtectedReaderRejectsSpecialModesAndAncestorAliases(t *testing.T) {
	for _, mode := range []uint32{04600, 02600, 01600} {
		t.Run(strconv.FormatUint(uint64(mode), 8), func(t *testing.T) {
			dir := t.TempDir()
			if os.Chmod(dir, 0700) != nil {
				t.Fatal("private directory")
			}
			path := filepath.Join(dir, "config.json")
			if os.WriteFile(path, []byte("controlled"), 0600) != nil || unix.Chmod(path, mode) != nil {
				t.Fatal("special-mode fixture")
			}
			var st unix.Stat_t
			if unix.Stat(path, &st) != nil || st.Mode&07777 != mode {
				t.Fatal("special-mode fixture not actually retained")
			}
			if _, err := privateRead(path, uint32(os.Geteuid())); err == nil {
				t.Fatal("special-mode leaf accepted")
			}
		})
	}
	dir := t.TempDir()
	if os.Chmod(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "config.json"), []byte("controlled"), 0600) != nil {
		t.Fatal("ancestor fixture")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if os.Symlink(dir, alias) != nil {
		t.Fatal("ancestor alias fixture")
	}
	if _, err := privateRead(filepath.Join(alias, "config.json"), uint32(os.Geteuid())); err == nil {
		t.Fatal("ancestor alias accepted")
	}
	if unix.Chmod(dir, 01700) != nil {
		t.Fatal("special-mode parent fixture")
	}
	if _, err := privateRead(filepath.Join(dir, "config.json"), uint32(os.Geteuid())); err == nil {
		t.Fatal("special-mode parent accepted")
	}
}

func TestActualReaderRejectsLeafAndParentChangesAfterRead(t *testing.T) {
	for _, kind := range []string{"rewrite_restored_mtime", "leaf_replacement", "parent_mode_restore", "parent_replacement"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			dir := filepath.Join(base, "native")
			if os.Mkdir(dir, 0700) != nil {
				t.Fatal("native directory fixture")
			}
			path := filepath.Join(dir, "config.json")
			if os.WriteFile(path, []byte("controlled"), 0600) != nil {
				t.Fatal("native leaf fixture")
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal("fixture metadata")
			}
			_, err = privateReadAfterRead(path, uint32(os.Geteuid()), func() {
				switch kind {
				case "rewrite_restored_mtime":
					if os.WriteFile(path, []byte("changed___"), 0600) != nil || os.Chtimes(path, info.ModTime(), info.ModTime()) != nil {
						t.Fatal("same-size rewrite fixture")
					}
				case "leaf_replacement":
					if os.Rename(path, path+".retained") != nil || os.WriteFile(path, []byte("controlled"), 0600) != nil {
						t.Fatal("leaf replacement fixture")
					}
				case "parent_mode_restore":
					if os.Chmod(dir, 0755) != nil || os.Chmod(dir, 0700) != nil {
						t.Fatal("parent metadata fixture")
					}
				case "parent_replacement":
					if os.Rename(dir, dir+".retained") != nil || os.Mkdir(dir, 0700) != nil || os.WriteFile(path, []byte("controlled"), 0600) != nil {
						t.Fatal("parent replacement fixture")
					}
				}
			})
			if err == nil {
				t.Fatal("actual reader accepted changed native input")
			}
		})
	}
}

func TestStableMetadataComparesNanosecondCTimeMTimeAndLeafIdentity(t *testing.T) {
	before := unix.Stat_t{Dev: 1, Ino: 2, Mode: unix.S_IFREG | 0600, Uid: 501, Gid: 501, Nlink: 1, Size: 10, Mtim: unix.Timespec{Sec: 10, Nsec: 20}, Ctim: unix.Timespec{Sec: 10, Nsec: 30}}
	if !protectedLeaf(before, 501) || !stableMetadata(before, before) {
		t.Fatal("stable native metadata refused")
	}
	for _, mutate := range []func(*unix.Stat_t){func(s *unix.Stat_t) { s.Mtim.Nsec++ }, func(s *unix.Stat_t) { s.Ctim.Nsec++ }, func(s *unix.Stat_t) { s.Ino++ }, func(s *unix.Stat_t) { s.Dev++ }, func(s *unix.Stat_t) { s.Uid++ }, func(s *unix.Stat_t) { s.Gid++ }, func(s *unix.Stat_t) { s.Nlink++ }, func(s *unix.Stat_t) { s.Size++ }, func(s *unix.Stat_t) { s.Mode |= unix.S_ISUID }} {
		after := before
		mutate(&after)
		if stableMetadata(before, after) {
			t.Fatal("changed native metadata accepted")
		}
	}
	after := before
	after.Atim.Nsec++
	if !stableMetadata(before, after) {
		t.Fatal("legitimate read-atime change rejected")
	}
}

func TestProtectedFIFORefusalAndOwnedCancellation(t *testing.T) {
	// Both child branches are fixed unit-only modes. No runtime helper command
	// accepts a test path, environment owner, arbitrary code or cancellation mode.
	if mode := os.Getenv("ROUTEX_F28_UNIT_CHILD_MODE"); mode != "" {
		path := os.Getenv("ROUTEX_F28_UNIT_FIFO_PATH")
		switch mode {
		case "refuse":
			if _, err := privateRead(path, uint32(os.Geteuid())); err == nil {
				t.Fatal("FIFO accepted")
			}
		case "cancel":
			// Deliberately blocking dummy child verifies the parent's original
			// process/group cancellation path without blocking the test owner.
			fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
			if err == nil {
				_ = unix.Close(fd)
			}
			t.Fatal("dummy FIFO unexpectedly unblocked")
		default:
			t.Fatal("unknown child mode")
		}
		return
	}
	for _, mode := range []string{"refuse", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if os.Chmod(dir, 0700) != nil || unix.Mkfifo(path, 0600) != nil {
				t.Fatal("FIFO fixture")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProtectedFIFORefusalAndOwnedCancellation$", "-test.count=1")
			cmd.Env = append(os.Environ(), "ROUTEX_F28_UNIT_CHILD_MODE="+mode, "ROUTEX_F28_UNIT_FIFO_PATH="+path)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			cmd.SysProcAttr = &unix.SysProcAttr{Setpgid: true}
			cmd.WaitDelay = 300 * time.Millisecond
			if cmd.Start() != nil {
				t.Fatal("owned child did not start")
			}
			pid := cmd.Process.Pid
			pgid, err := unix.Getpgid(pid)
			if err != nil || pgid != pid {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("original child group identity unknown")
			}
			t.Logf("owned_fifo_child_started pid=%d pgid=%d mode=%s", pid, pgid, mode)
			if mode == "cancel" {
				cancel()
			}
			err = cmd.Wait()
			closed := unix.Kill(-pgid, 0) == unix.ESRCH
			if cmd.ProcessState == nil {
				t.Fatal("original child terminal unavailable")
			}
			exit := cmd.ProcessState.ExitCode()
			t.Logf("owned_fifo_child_terminal pid=%d pgid=%d exit=%d joined=true group_closed=%t", pid, pgid, exit, closed)
			if !closed || (mode == "refuse" && (err != nil || exit != 0 || ctx.Err() != nil)) || (mode == "cancel" && (err == nil || exit == 0 || ctx.Err() != context.Canceled)) {
				t.Fatal("original FIFO refusal/cancellation terminal unknown")
			}
		})
	}
}
