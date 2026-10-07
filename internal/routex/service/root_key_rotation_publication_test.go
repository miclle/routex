package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// The callbacks provide an in-memory persistence boundary, while the worker,
// ciphertext CAS, runtime loader/publisher and process proof remain production
// code. No database server, process, clock advance or proof override is used.
type rootPublicationFixture struct {
	policy          entity.SecretWritePolicy
	job             entity.SecretRotationJob
	egress          entity.Egress
	keys            []entity.SecretRootKey
	instance        entity.SystemInstance
	items           []entity.SecretRotationItem
	transactions    int
	failPublication bool
	failAfterCAS    bool
	committedCAS    bool
}
type rootPublicationConnector struct{ fixture *rootPublicationFixture }

func (c rootPublicationConnector) Connect(context.Context) (driver.Conn, error) {
	return rootPublicationConnection(c), nil
}
func (c rootPublicationConnector) Driver() driver.Driver { return rootPublicationDriver(c) }

type rootPublicationDriver rootPublicationConnector

func (d rootPublicationDriver) Open(string) (driver.Conn, error) {
	return rootPublicationConnection(d), nil
}

type rootPublicationConnection rootPublicationConnector

func (rootPublicationConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected SQL")
}
func (rootPublicationConnection) Close() error { return nil }
func (c rootPublicationConnection) Begin() (driver.Tx, error) {
	c.fixture.transactions++
	return rootPublicationTransaction(c), nil
}
func (c rootPublicationConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return c.Begin()
}

type rootPublicationTransaction struct{ fixture *rootPublicationFixture }

func (t rootPublicationTransaction) Commit() error   { t.fixture.transactions--; return nil }
func (t rootPublicationTransaction) Rollback() error { t.fixture.transactions--; return nil }

func rootPublicationService(t *testing.T) (*Service, *rootPublicationFixture) {
	t.Helper()
	store, err := secretstore.NewKeyring(map[string][]byte{"next": bytes.Repeat([]byte{2}, 32), "third": bytes.Repeat([]byte{3}, 32)}, "", "next")
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := store.Seal("egress:egr_publication:sec_publication", "private fixture")
	if err != nil {
		t.Fatal(err)
	}
	key, jobID := "third", "rot_publication"
	now := time.Now().UTC()
	f := &rootPublicationFixture{
		policy:   entity.SecretWritePolicy{ID: 1, Initialized: true, WriteKeyID: &key, Epoch: 4, ActiveJobID: &jobID},
		job:      entity.SecretRotationJob{InventoryVersion: 2, ID: jobID, SourceKeyID: "next", TargetKeyID: key, CutoverEpoch: 4, ScanGeneration: 1, Status: "migrating", Phase: "migration", Domain: 1, CountsJSON: "{}"},
		egress:   entity.Egress{ID: "egr_publication", Kind: "https", Host: "127.0.0.1", Port: 9, SecretGeneration: "sec_publication", AuthCiphertext: cipher},
		instance: entity.SystemInstance{ID: "ins_publication", LeaseToken: "lease_publication", Role: "combined", LeaseExpiresAt: now.Add(time.Minute)},
	}
	f.job.ETag = rootJobETag(f.job)
	for _, id := range []string{"next", "third"} {
		writer, e := store.WithWriteKey(id)
		if e != nil {
			t.Fatal(e)
		}
		proof, e := writer.Seal(rootProofReference(id), rootProofPlaintext)
		if e != nil {
			t.Fatal(e)
		}
		state := "decrypt_only"
		if id == key {
			state = "write"
		}
		f.keys = append(f.keys, entity.SecretRootKey{KeyID: id, State: state, ProofCiphertext: proof})
	}
	var svc *Service
	pool := sql.OpenDB(rootPublicationConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, SkipDefaultTransaction: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		tx.RowsAffected = 1
		switch dst := tx.Statement.Dest.(type) {
		case *entity.SecretWritePolicy:
			*dst = f.policy
		case *entity.SecretRotationJob:
			*dst = f.job
		case *[]entity.SystemInstance:
			*dst = []entity.SystemInstance{f.instance}
		case *[]entity.SecretRootKey:
			*dst = append([]entity.SecretRootKey(nil), f.keys...)
		case *[]entity.Egress:
			if f.committedCAS {
				if f.transactions > 1 {
					t.Fatal("publication borrowed a connection inside the page transaction")
				}
				if !svc.egressMu.TryLock() {
					t.Fatal("publication retained the egress write mutex")
				}
				svc.egressMu.Unlock()
			}
			if f.failPublication {
				_ = tx.AddError(errors.New("publication unavailable"))
				return
			}
			*dst = []entity.Egress{f.egress}
		case *[]map[string]any:
			*dst = []map[string]any{{"id": f.egress.ID, "auth_ciphertext": f.egress.AuthCiphertext, "secret_generation": f.egress.SecretGeneration}}
		case *entity.QuotaSetting:
			*dst = entity.QuotaSetting{ID: 1, TimeZone: "UTC"}
		case *entity.PricingSetting:
			*dst = entity.PricingSetting{ID: 1, PlatformCurrency: "USD"}
		default:
			v := reflect.ValueOf(tx.Statement.Dest).Elem()
			v.Set(reflect.Zero(v.Type()))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		tx.RowsAffected = 1
		switch dst := tx.Statement.Dest.(type) {
		case *entity.SecretRotationJob:
			f.job = *dst
		case map[string]any:
			if tx.Statement.Table == "egresses" {
				if f.transactions != 1 {
					t.Fatal("rewrap escaped its transaction")
				}
				f.egress.AuthCiphertext = dst["auth_ciphertext"].(string)
				f.committedCAS = true
				f.failPublication = f.failAfterCAS
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		tx.RowsAffected = 1
		switch row := tx.Statement.Dest.(type) {
		case *entity.SecretRotationItem:
			f.items = append(f.items, *row)
		case *entity.SystemJob:
			_ = tx.AddError(errors.New("operational reporting unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc = &Service{db: db, secrets: store, rootNow: time.Now, instanceNow: time.Now, runtime: &gatewayRuntime{}, instance: &systemInstanceLease{id: f.instance.ID, token: f.instance.LeaseToken}}
	if err := svc.installSecretPolicy(f.policy, f.keys); err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc, f
}

func TestRootRotationPagePublicationFailurePreservesProgressAndDeniesProof(t *testing.T) {
	svc, f := rootPublicationService(t)
	ctx := context.Background()
	before := svc.runtime.routes.Load()
	f.failAfterCAS = true
	if err := svc.RunSecretRotationOnce(ctx); !errors.Is(err, secretStoreUnavailable) {
		t.Fatal("failed publication reported success", err)
	}
	if key, err := svc.secrets.KeyID(rootReference("egresses", f.egress.ID, f.egress.SecretGeneration), f.egress.AuthCiphertext); err != nil || key != "third" {
		t.Fatal("committed rewrap disappeared", key, err)
	}
	counts := rootCounts(f.job)["egresses"]
	if len(f.items) != 1 || f.job.Cursor != f.egress.ID || counts.Scanned != 1 || counts.Rewrapped != 1 || f.job.Status != "blocked" || f.job.BlockerCode != "publication_pending" {
		t.Fatal("blocking overwrote the committed checkpoint", f.job.Cursor, f.job.Status, f.job.BlockerCode, counts)
	}
	if svc.runtime.routes.Load() != before || f.policy.Epoch != 4 || *f.policy.WriteKeyID != "third" {
		t.Fatal("publication failure replaced routing or rolled back the write policy")
	}
	f.failPublication = false
	if _, err := svc.rootCurrentProofAdmission(svc.authDB(ctx), f.policy, false); !errors.Is(err, secretStoreUnavailable) {
		t.Fatal("stale runtime allowed rollback proof", err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.rootCurrentProofAdmission(svc.authDB(ctx), f.policy, false); err != nil {
		t.Fatal("real publication could not restore current proof", err)
	}
	if f.job.Status != "blocked" || f.transactions != 0 {
		t.Fatal("publication silently resumed work or retained a transaction")
	}
}

func TestRootRotationPagePublishesCommittedCiphertextBeforeRollbackProof(t *testing.T) {
	svc, f := rootPublicationService(t)
	ctx := context.Background()
	before := svc.runtime.routes.Load()
	if _, err := svc.rootCurrentProof(svc.authDB(ctx), f.policy); err != nil {
		t.Fatal("initial proof", err)
	}
	if err := svc.RunSecretRotationOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if key, err := svc.secrets.KeyID(rootReference("egresses", f.egress.ID, f.egress.SecretGeneration), f.egress.AuthCiphertext); err != nil || key != "third" {
		t.Fatal("page did not rewrap", key, err)
	}
	if len(f.items) != 1 || f.items[0].Outcome != "rewrapped" || f.job.Cursor != f.egress.ID {
		t.Fatal("durable page checkpoint missing")
	}
	if _, err := svc.rootCurrentProofAdmission(svc.authDB(ctx), f.policy, false); err != nil {
		t.Fatal("fresh rollback proof rejected committed page", err)
	}
	if after := svc.runtime.routes.Load(); after.ID == before.ID || after.Digest == before.Digest {
		t.Fatal("committed ciphertext page returned without runtime publication")
	}
	if f.transactions != 0 {
		t.Fatal("publication retained a transaction")
	}
}
