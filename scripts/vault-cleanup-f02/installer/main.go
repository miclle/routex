//go:build linux

// Private fixture helper. Never linked into the RouteX production executable.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"golang.org/x/sys/unix"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const constraintName = "routex_fixture_f28_postack_refusal"
const inputPath = "/fixture-f28-input/config.json"

var BuildSource string
var BuildRunID string
var BuildNormalSchemaReceiptSHA256 string
var BuildTrustedHostUID string

// These frozen fixture definitions describe ONLY the additional check. They
// never AutoMigrate, create a business table, or replace released constraints.
type orphanRefusal struct {
	State string `gorm:"column:state;check:routex_fixture_f28_postack_refusal,state <> 'acknowledged'"`
}

func (orphanRefusal) TableName() string { return "provider_credential_cleanups" }

type publishedRefusal struct {
	State string `gorm:"column:state;check:routex_fixture_f28_postack_refusal,state <> 'acknowledged'"`
}

func (publishedRefusal) TableName() string { return "credential_published_cleanups" }
func fixture(kind string) (any, string, error) {
	switch kind {
	case "orphan":
		return &orphanRefusal{}, "orphan", nil
	case "published":
		return &publishedRefusal{}, "committed", nil
	}
	return nil, "", refusal("argument")
}

type refusal string

func (r refusal) Error() string { return "F28 fixture refused" }
func safeFailure(err error) map[string]string {
	var stage refusal
	if !errors.As(err, &stage) || !map[refusal]bool{"argument": true, "protectedInput": true, "config": true, "connect": true, "freshScope": true, "constraint": true, "transaction": true, "encode": true}[stage] {
		stage = "transaction"
	}
	return map[string]string{"result": "f28_fault_fixture_refused", "stage": string(stage)}
}

type config struct {
	RunID                     string    `json:"run_id"`
	SourceCommit              string    `json:"source_commit"`
	Namespace                 string    `json:"namespace"`
	DatabaseContainerID       string    `json:"database_container_id"`
	NormalSchemaReceiptSHA256 string    `json:"normal_schema_receipt_sha256"`
	DatabaseCreatedForRun     bool      `json:"database_created_for_run"`
	FaultOnly                 bool      `json:"fault_only"`
	DatabasePassword          string    `json:"database_password"`
	Kind                      string    `json:"kind"`
	CreationID                string    `json:"creation_id"`
	CleanupID                 string    `json:"cleanup_id"`
	ActorID                   string    `json:"actor_id"`
	ActorBirth                time.Time `json:"actor_birth"`
	IntegrationID             string    `json:"integration_id"`
	IntegrationBirth          time.Time `json:"integration_birth"`
	CredentialID              string    `json:"credential_id"`
	CredentialBirth           time.Time `json:"credential_birth"`
	CreationBirth             time.Time `json:"creation_birth"`
	RunStarted                time.Time `json:"run_started"`
}

type heldDirectory struct {
	fd   int
	name string
	stat unix.Stat_t
}

func sameIdentity(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid
}

// Reads may advance atime. No other retained input metadata may change; Linux
// nanosecond ctime also detects a same-size rewrite with restored mtime.
func stableMetadata(a, b unix.Stat_t) bool {
	return sameIdentity(a, b) && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func protectedLeaf(st unix.Stat_t, uid uint32) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFREG && st.Mode&07777 == 0600 && st.Uid == uid && st.Nlink == 1 && st.Size >= 1 && st.Size <= 16384
}

func privateRead(path string, uid uint32) ([]byte, error) {
	return privateReadAfterRead(path, uid, nil)
}

// The executable supplies only inputPath and a compile-bound owner. The internal
// callback permits deterministic FD-race controls; runtime always supplies nil.
func privateReadAfterRead(path string, uid uint32, afterRead func()) (raw []byte, runErr error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || filepath.Dir(path) == "/" {
		return nil, refusal("protectedInput")
	}
	flags := unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	rootFD, err := unix.Open("/", flags, 0)
	if err != nil {
		return nil, refusal("protectedInput")
	}
	dirs := []heldDirectory{{fd: rootFD, name: "/"}}
	defer func() {
		for i := len(dirs) - 1; i >= 0; i-- {
			if unix.Close(dirs[i].fd) != nil && runErr == nil {
				runErr = refusal("protectedInput")
			}
		}
		if runErr != nil {
			clear(raw)
			raw = nil
		}
	}()
	if unix.Fstat(rootFD, &dirs[0].stat) != nil || dirs[0].stat.Uid != 0 || dirs[0].stat.Mode&07777 != 0755 {
		return nil, refusal("protectedInput")
	}
	for _, part := range strings.Split(strings.TrimPrefix(filepath.Dir(path), "/"), "/") {
		fd, err := unix.Openat(dirs[len(dirs)-1].fd, part, flags, 0)
		if err != nil {
			return nil, refusal("protectedInput")
		}
		dirs = append(dirs, heldDirectory{fd: fd, name: part})
		if unix.Fstat(fd, &dirs[len(dirs)-1].stat) != nil {
			return nil, refusal("protectedInput")
		}
	}
	parent := dirs[len(dirs)-1]
	if parent.stat.Uid != uid || parent.stat.Mode&07777 != 0700 {
		return nil, refusal("protectedInput")
	}
	fd, err := unix.Openat(parent.fd, filepath.Base(path), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, refusal("protectedInput")
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() {
		if f.Close() != nil && runErr == nil {
			runErr = refusal("protectedInput")
		}
	}()
	var before, after, binding unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !protectedLeaf(before, uid) {
		return nil, refusal("protectedInput")
	}
	raw, err = io.ReadAll(io.LimitReader(f, 16385))
	readBytes := raw
	defer func() {
		if runErr != nil {
			clear(readBytes)
		}
	}()
	if afterRead != nil {
		afterRead()
	}
	if err != nil || len(raw) != int(before.Size) || unix.Fstat(fd, &after) != nil || !protectedLeaf(after, uid) || !stableMetadata(before, after) {
		return nil, refusal("protectedInput")
	}
	// Revalidate the held directories AND their current no-follow path bindings.
	// A renamed/replaced native directory or leaf cannot return stale bytes.
	for i, dir := range dirs {
		var current, linked unix.Stat_t
		parentFD := unix.AT_FDCWD
		if i > 0 {
			parentFD = dirs[i-1].fd
		}
		if unix.Fstat(dir.fd, &current) != nil || unix.Fstatat(parentFD, dir.name, &linked, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameIdentity(dir.stat, current) || !sameIdentity(dir.stat, linked) {
			return nil, refusal("protectedInput")
		}
		if i == len(dirs)-1 && (!stableMetadata(dir.stat, current) || !stableMetadata(dir.stat, linked) || current.Uid != uid || current.Mode&07777 != 0700) {
			return nil, refusal("protectedInput")
		}
	}
	if unix.Fstatat(parent.fd, filepath.Base(path), &binding, unix.AT_SYMLINK_NOFOLLOW) != nil || !protectedLeaf(binding, uid) || !stableMetadata(after, binding) {
		return nil, refusal("protectedInput")
	}
	return raw, nil
}
func decode(raw []byte, now time.Time) (config, error) {
	var c config
	allowed := map[string]bool{}
	for _, key := range []string{"run_id", "source_commit", "namespace", "database_container_id", "normal_schema_receipt_sha256", "database_created_for_run", "fault_only", "database_password", "kind", "creation_id", "cleanup_id", "actor_id", "actor_birth", "integration_id", "integration_birth", "credential_id", "credential_birth", "creation_birth", "run_started"} {
		allowed[key] = false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return c, refusal("config")
	}
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		seen, present := allowed[key]
		if e != nil || !ok || !present || seen {
			return c, refusal("config")
		}
		allowed[key] = true
		var item json.RawMessage
		if d.Decode(&item) != nil || bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return c, refusal("config")
		}
	}
	t, e = d.Token()
	var tail any
	if e != nil || t != json.Delim('}') || d.Decode(&tail) != io.EOF || json.Unmarshal(raw, &c) != nil {
		return c, refusal("config")
	}
	for _, seen := range allowed {
		if !seen {
			return c, refusal("config")
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(BuildRunID) || c.RunID != BuildRunID || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(BuildSource) || c.SourceCommit != BuildSource || BuildSource == "777048e6a685d56c78565545d1fe94099486668f" || c.Namespace != "routex-f28-f02-"+BuildRunID[:16] || !c.DatabaseCreatedForRun || !c.FaultOnly {
		return c, refusal("config")
	}
	for _, value := range []string{c.DatabaseContainerID, c.NormalSchemaReceiptSHA256} {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(value) {
			return c, refusal("config")
		}
	}
	if c.NormalSchemaReceiptSHA256 != BuildNormalSchemaReceiptSHA256 {
		return c, refusal("config")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`).MatchString(c.DatabasePassword) {
		return c, refusal("config")
	}
	uuid := regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)
	if !uuid.MatchString(c.CreationID) || !uuid.MatchString(c.CleanupID) || c.CreationID == c.CleanupID {
		return c, refusal("config")
	}
	for _, item := range []struct{ v, p string }{{c.ActorID, "usr"}, {c.IntegrationID, "vlt"}, {c.CredentialID, "crd"}} {
		if !regexp.MustCompile(`^` + item.p + `_[0-7][0-9a-hjkmnp-tv-z]{25}$`).MatchString(item.v) {
			return c, refusal("config")
		}
	}
	if _, _, e := fixture(c.Kind); e != nil {
		return c, e
	}
	if c.RunStarted.IsZero() || now.Before(c.RunStarted) || !now.Before(c.RunStarted.Add(900*time.Second)) {
		return c, refusal("config")
	}
	for _, birth := range []time.Time{c.ActorBirth, c.IntegrationBirth, c.CredentialBirth, c.CreationBirth} {
		if birth.Before(c.RunStarted) || birth.After(now) {
			return c, refusal("config")
		}
	}
	return c, nil
}

type baseline struct {
	OrphanCount         int64
	PublishedCount      int64
	Operations          []entity.CredentialStorageOperation
	MigrationDigest     string
	ExistingFixture     bool
	OtherFixture        bool
	ReleasedConstraints bool
	DatabaseName        string
	DatabaseUser        string
}

func guard(c config, b baseline) error {
	_, state, e := fixture(c.Kind)
	if e != nil {
		return e
	}
	if b.OrphanCount != 0 || b.PublishedCount != 0 || b.ExistingFixture || b.OtherFixture || !b.ReleasedConstraints || len(b.Operations) != 1 || b.MigrationDigest == "" || b.DatabaseName != "routex_f28_f02_"+c.RunID || b.DatabaseUser != "routex_f28_f02" {
		return refusal("freshScope")
	}
	o := b.Operations[0]
	if o.RequestID != c.CreationID || o.StorageSource != "vault" || o.State != state || o.ActorID != c.ActorID || !o.ActorBirth.Equal(c.ActorBirth) || o.IntegrationID != c.IntegrationID || o.IntegrationBirth == nil || !o.IntegrationBirth.Equal(c.IntegrationBirth) || o.CredentialID != c.CredentialID || !o.CredentialBirth.Equal(c.CredentialBirth) || !o.CreatedAt.Equal(c.CreationBirth) {
		return refusal("freshScope")
	}
	return nil
}

type installationStore interface {
	before(config) (baseline, error)
	create(string) error
	after(config) (baseline, error)
}

func install(c config, s installationStore) error {
	before, e := s.before(c)
	if e != nil {
		return e
	}
	if e = guard(c, before); e != nil {
		return e
	}
	if e = s.create(c.Kind); e != nil {
		return refusal("constraint")
	}
	after, e := s.after(c)
	if e != nil {
		return e
	}
	if !after.ExistingFixture || after.OtherFixture || !after.ReleasedConstraints || after.MigrationDigest != before.MigrationDigest || after.OrphanCount != 0 || after.PublishedCount != 0 {
		return refusal("constraint")
	}
	after.ExistingFixture = false
	return guard(c, after)
}

type gormStore struct{ tx *gorm.DB }

func (s gormStore) before(c config) (baseline, error) { return s.snapshot(c) }
func (s gormStore) after(c config) (baseline, error)  { return s.snapshot(c) }
func (s gormStore) create(kind string) error {
	model, _, e := fixture(kind)
	if e != nil {
		return e
	}
	return s.tx.Migrator().CreateConstraint(model, constraintName)
}
func (s gormStore) snapshot(c config) (baseline, error) {
	var b baseline
	if s.tx.Model(&entity.ProviderCredentialCleanup{}).Count(&b.OrphanCount).Error != nil || s.tx.Model(&entity.CredentialPublishedCleanup{}).Count(&b.PublishedCount).Error != nil {
		return b, refusal("freshScope")
	}
	columns := "request_id,storage_source,state,actor_id,actor_birth,integration_id,integration_birth,credential_id,credential_birth,created_at"
	if s.tx.Select(columns).Order("request_id").Limit(2).Find(&b.Operations).Error != nil {
		return b, refusal("freshScope")
	}
	var migrations []struct {
		Version   int
		AppliedAt string
	}
	if s.tx.Table("schema_migrations").Order("version").Limit(129).Find(&migrations).Error != nil || len(migrations) < 1 || len(migrations) > 128 {
		return b, refusal("freshScope")
	}
	raw, e := json.Marshal(migrations)
	if e != nil {
		return b, refusal("freshScope")
	}
	sum := sha256.Sum256(raw)
	b.MigrationDigest = hex.EncodeToString(sum[:])
	model, _, e := fixture(c.Kind)
	if e != nil {
		return b, e
	}
	other := any(&publishedRefusal{})
	if c.Kind == "published" {
		other = &orphanRefusal{}
	}
	b.ExistingFixture = s.tx.Migrator().HasConstraint(model, constraintName)
	b.OtherFixture = s.tx.Migrator().HasConstraint(other, constraintName)
	b.ReleasedConstraints = s.tx.Migrator().HasConstraint(&entity.ProviderCredentialCleanup{}, "ck_provider_cleanup_state_v80") && s.tx.Migrator().HasConstraint(&entity.CredentialPublishedCleanup{}, "ck_credential_published_cleanup_state_v88")
	var identity struct {
		DatabaseName string
		DatabaseUser string
	}
	// Fixed SELECT of the already selected connection identity; no catalog scan,
	// dynamic SQL, business data seeding or database selector is accepted.
	if s.tx.Raw("SELECT current_database() AS database_name, current_user AS database_user").Scan(&identity).Error != nil {
		return b, refusal("freshScope")
	}
	b.DatabaseName, b.DatabaseUser = identity.DatabaseName, identity.DatabaseUser
	var actor entity.User
	var integration entity.VaultIntegration
	if s.tx.Select("id,created_at,role,disabled,offboarded_at").Where("id = ?", c.ActorID).Take(&actor).Error != nil || actor.ID != c.ActorID || !actor.CreatedAt.Equal(c.ActorBirth) || actor.Role != "admin" || actor.Disabled || actor.OffboardedAt != nil {
		return b, refusal("freshScope")
	}
	if s.tx.Select("id,created_at").Where("id = ?", c.IntegrationID).Take(&integration).Error != nil || integration.ID != c.IntegrationID || !integration.CreatedAt.Equal(c.IntegrationBirth) {
		return b, refusal("freshScope")
	}
	var live int64
	if s.tx.Model(&entity.ProviderCredential{}).Where("id = ?", c.CredentialID).Count(&live).Error != nil || live != 0 {
		return b, refusal("freshScope")
	}
	return b, nil
}
func runIdentity(args []string, actualUID int) (string, uint32, error) {
	if len(args) != 2 || !regexp.MustCompile(`^[1-9][0-9]{0,9}$`).MatchString(BuildTrustedHostUID) {
		return "", 0, refusal("argument")
	}
	bound, err := strconv.ParseUint(BuildTrustedHostUID, 10, 32)
	if err != nil || bound == 0 || strconv.FormatUint(bound, 10) != BuildTrustedHostUID || actualUID <= 0 || uint64(actualUID) != bound {
		return "", 0, refusal("argument")
	}
	kind := ""
	switch args[1] {
	case "install-orphan":
		kind = "orphan"
	case "install-published":
		kind = "published"
	default:
		return "", 0, refusal("argument")
	}
	return kind, uint32(bound), nil
}

func privateGORMConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Discard, DisableAutomaticPing: true, TranslateError: true}
}

func run() error {
	if os.Getuid() != os.Geteuid() {
		return refusal("argument")
	}
	kind, trustedUID, e := runIdentity(os.Args, os.Geteuid())
	if e != nil {
		return e
	}
	raw, e := privateRead(inputPath, trustedUID)
	if e != nil {
		return e
	}
	defer clear(raw)
	c, e := decode(raw, time.Now().UTC())
	if e != nil {
		return e
	}
	if c.Kind != kind {
		return refusal("argument")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dsn := fmt.Sprintf("host=postgres port=5432 user=routex_f28_f02 password=%s dbname=routex_f28_f02_%s sslmode=disable connect_timeout=2 application_name=routex-f28-helper-%s", c.DatabasePassword, c.RunID, c.RunID[:16])
	db, e := gorm.Open(postgres.Open(dsn), privateGORMConfig())
	if e != nil {
		return refusal("connect")
	}
	pool, e := db.DB()
	if e != nil {
		return refusal("connect")
	}
	closed := false
	defer func() {
		if !closed {
			_ = pool.Close()
		}
	}()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	e = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return install(c, gormStore{tx}) })
	if e != nil {
		return e
	}
	if ctx.Err() != nil {
		return refusal("transaction")
	}
	closed = true
	if pool.Close() != nil || ctx.Err() != nil {
		return refusal("transaction")
	}
	ack := map[string]any{"result": "f28_fixture_constraint_installed", "scope": "fresh_owned_f02_fault_database_only", "kind": kind, "constraint": constraintName, "released_migration_ledger_preserved": true, "runtime_acceptance": false}
	if json.NewEncoder(os.Stdout).Encode(ack) != nil || ctx.Err() != nil {
		return refusal("encode")
	}
	return nil
}
func main() {
	if e := run(); e != nil {
		_ = json.NewEncoder(os.Stdout).Encode(safeFailure(e))
		os.Exit(1)
	}
}
