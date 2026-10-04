package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type rootFenceFixture struct {
	policy   entity.SecretWritePolicy
	queries  []string
	updates  []string
	affected int64
}
type rootFenceConnector struct{ fixture *rootFenceFixture }

func (c rootFenceConnector) Connect(context.Context) (driver.Conn, error) {
	return rootFenceConnection(c), nil
}
func (c rootFenceConnector) Driver() driver.Driver { return rootFenceDriver(c) }

type rootFenceDriver rootFenceConnector

func (d rootFenceDriver) Open(string) (driver.Conn, error) { return rootFenceConnection(d), nil }

type rootFenceConnection rootFenceConnector

func (rootFenceConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unsupported")
}
func (rootFenceConnection) Close() error              { return nil }
func (rootFenceConnection) Begin() (driver.Tx, error) { return nil, errors.New("unsupported") }
func (c rootFenceConnection) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.fixture.queries = append(c.fixture.queries, query)
	p := c.fixture.policy
	var key driver.Value
	if p.WriteKeyID != nil {
		key = *p.WriteKeyID
	}
	return &rootFenceRows{values: []driver.Value{int64(p.ID), p.Initialized, key, int64(p.Epoch), p.ETag}}, nil
}
func (c rootFenceConnection) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.fixture.updates = append(c.fixture.updates, query)
	return driver.RowsAffected(c.fixture.affected), nil
}

type rootFenceRows struct {
	values []driver.Value
	done   bool
}

func (*rootFenceRows) Columns() []string {
	return []string{"id", "initialized", "write_key_id", "epoch", "etag"}
}
func (*rootFenceRows) Close() error { return nil }
func (r *rootFenceRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(values, r.values)
	return nil
}
func rootFenceDB(t *testing.T, fixture *rootFenceFixture) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(rootFenceConnector{fixture})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func rootFenceService(t *testing.T) (*Service, *rootFenceFixture) {
	t.Helper()
	store, err := secretstore.NewKeyring(map[string][]byte{"old": bytes.Repeat([]byte{1}, 32), "new": bytes.Repeat([]byte{2}, 32)}, "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	key := "old"
	fixture := &rootFenceFixture{policy: entity.SecretWritePolicy{ID: 1, Initialized: true, WriteKeyID: &key, Epoch: 1, ETag: strings.Repeat("a", 64)}, affected: 1}
	svc := &Service{db: rootFenceDB(t, fixture), secrets: store, rootNow: time.Now}
	svc.rootPolicy.Store(&secretPolicyView{epoch: 1, writeID: key, store: store})
	return svc, fixture
}
func TestRootSecretFenceRejectsWholeStalePreparationAndWrongSelection(t *testing.T) {
	svc, f := rootFenceService(t)
	cipher, err := svc.sealSecret("credential", "private")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.guardSecretWrite(svc.db, 1, "credential", cipher); err != nil {
		t.Fatal("current preparation rejected", err)
	}
	if len(f.queries) != 1 || !strings.Contains(f.queries[0], "FOR UPDATE") {
		t.Fatal("fence is not held through caller transaction")
	}
	f.policy.Epoch = 2
	key := "new"
	f.policy.WriteKeyID = &key
	view, err := svc.secrets.WithWriteKey("new")
	if err != nil {
		t.Fatal(err)
	}
	svc.rootPolicy.Store(&secretPolicyView{epoch: 2, writeID: key, store: view})
	if err := svc.guardSecretWrite(svc.db, 1, "credential", cipher); !errors.Is(err, secretPolicyChanged) {
		t.Fatal("stale epoch repaired silently", err)
	}
	if err := svc.guardSecretWrite(svc.db, 2, "credential", cipher); !errors.Is(err, secretPolicyChanged) {
		t.Fatal("wrong wrapping ID accepted", err)
	}
	fresh, err := svc.sealSecret("credential", "private")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.guardSecretWrite(svc.db, 2, "credential", fresh); err != nil {
		t.Fatal(err)
	}
	if err := svc.guardSecretWrite(svc.db, 2, "foreign", fresh); !errors.Is(err, secretPolicyChanged) {
		t.Fatal("reference substitution accepted")
	}
	before := len(f.queries)
	if err := svc.guardSecretWrite(svc.db, 0, "", ""); err != nil || len(f.queries) != before {
		t.Fatal("authentication removal invented an envelope")
	}
	f.policy.Initialized = false
	f.policy.Epoch = 0
	f.policy.WriteKeyID = nil
	if err := svc.guardSecretWrite(svc.db, 2, "credential", fresh); !errors.Is(err, secretPolicyChanged) {
		t.Fatal("initialized epoch fell back to legacy")
	}
}
func TestRootSecretReadDrainRefusesNewReadsAndNeverMutatesMaster(t *testing.T) {
	svc, _ := rootFenceService(t)
	cipher, err := svc.sealSecret("credential", "private")
	if err != nil {
		t.Fatal(err)
	}
	view := svc.rootPolicy.Load()
	view.refs.Add(1)
	done := make(chan error, 1)
	go func() { done <- svc.drainSecretView(context.Background(), view) }()
	deadline := time.Now().Add(time.Second)
	for !view.closed.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !view.closed.Load() {
		t.Fatal("reader admission not closed")
	}
	if plain, err := svc.openSecret("credential", cipher); plain != "" || err == nil {
		t.Fatal("new operation acquired retiring view")
	}
	select {
	case <-done:
		t.Fatal("reader drain skipped in-flight reference")
	default:
	}
	view.refs.Add(-1)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// A failed final transition can reopen the exact old view; a successful one
	// installs a new restricted view instead, without mutating master material.
	view.closed.Store(false)
	svc.rootReadersClosed.Store(false)
	if plain, err := svc.openSecret("credential", cipher); err != nil || plain != "private" {
		t.Fatal("unchanged view could not recover", err)
	}
	restricted, err := svc.secrets.WithWriteKey("new")
	if err != nil {
		t.Fatal(err)
	}
	restricted, err = restricted.WithAllowedKeys([]string{"new"})
	if err != nil {
		t.Fatal(err)
	}
	svc.rootPolicy.Store(&secretPolicyView{epoch: 2, writeID: "new", store: restricted})
	if _, err := svc.openSecret("credential", cipher); err == nil {
		t.Fatal("retired envelope readable")
	}
	if plain, err := svc.secrets.Open("credential", cipher); err != nil || plain != "private" {
		t.Fatal("private recovery master mutated")
	}
}
func TestRootSecretConcurrentReadViews(t *testing.T) {
	svc, _ := rootFenceService(t)
	cipher, err := svc.sealSecret("credential", "private")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			for range 25 {
				if plain, err := svc.openSecret("credential", cipher); err != nil || plain != "private" {
					t.Error("concurrent read failed")
				}
			}
		})
	}
	group.Wait()
	if svc.rootPolicy.Load().refs.Load() != 0 {
		t.Fatal("read reference leaked")
	}
}
func TestRootSecretCASUpdatesCiphertextOnlyWithExactOriginal(t *testing.T) {
	svc, f := rootFenceService(t)
	row := rootInventoryRow{id: "crd_Case", reference: "crd_Case", ciphertext: "cipher_Case"}
	outcome, err := svc.rootCAS(svc.db, "provider_credentials", row, "new_cipher")
	if err != nil || outcome != "rewrapped" {
		t.Fatal(outcome, err)
	}
	if len(f.updates) != 1 {
		t.Fatal("missing CAS")
	}
	query := f.updates[0]
	if !strings.Contains(query, `SET "ciphertext"=`) || strings.Contains(query, "updated_at") || !strings.Contains(query, `"id" =`) || !strings.Contains(query, `"ciphertext" =`) {
		t.Fatal("CAS mutates business facts or lacks original authority", query)
	}
}

func TestRootSecretTargetOnlyStartupIgnoresRetiredProbeAndRejectsLegacy(t *testing.T) {
	target, err := secretstore.NewKeyring(map[string][]byte{"new": bytes.Repeat([]byte{2}, 32)}, "", "new")
	if err != nil {
		t.Fatal(err)
	}
	proof, err := target.Seal(rootProofReference("new"), rootProofPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	key := "new"
	stamp := time.Now().UTC()
	p := entity.SecretWritePolicy{ID: 1, Initialized: true, WriteKeyID: &key, Epoch: 3}
	keys := []entity.SecretRootKey{{KeyID: "old", State: "retired", RetiredAt: &stamp}, {KeyID: "new", State: "write", ProofCiphertext: proof}}
	svc := &Service{secrets: target}
	if err := svc.installSecretPolicy(p, keys); err != nil {
		t.Fatal("target-only restart requires retired material", err)
	}
	cipher, err := svc.sealSecret("live", "private")
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := svc.openSecret("live", cipher); err != nil || plain != "private" {
		t.Fatal("restricted target unavailable", err)
	}
	for _, store := range []*secretstore.Store{nil, func() *secretstore.Store {
		v, e := secretstore.New(bytes.Repeat([]byte{1}, 32))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}(), func() *secretstore.Store {
		v, e := secretstore.NewKeyring(map[string][]byte{"old": bytes.Repeat([]byte{1}, 32)}, "old", "old")
		if e != nil {
			t.Fatal(e)
		}
		return v
	}()} {
		if err := (&Service{secrets: store}).installSecretPolicy(p, keys); err == nil {
			t.Fatal("initialized durable policy bypassed by old bootstrap")
		}
	}
	wrong, err := secretstore.NewKeyring(map[string][]byte{"new": bytes.Repeat([]byte{3}, 32)}, "", "new")
	if err != nil {
		t.Fatal(err)
	}
	if err := (&Service{secrets: wrong}).installSecretPolicy(p, keys); err == nil {
		t.Fatal("same ID rebound to foreign material")
	}
}
func TestRootSecretDrainIncludesReadersOfEarlierView(t *testing.T) {
	svc, _ := rootFenceService(t)
	current := svc.rootPolicy.Load()
	svc.rootReaders.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := svc.drainSecretView(ctx, current); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("prior-generation reader not drained", err)
	}
	svc.rootReaders.Add(-1)
	if current.closed.Load() || svc.rootReadersClosed.Load() {
		t.Fatal("failed drain did not reopen unchanged view")
	}
}
