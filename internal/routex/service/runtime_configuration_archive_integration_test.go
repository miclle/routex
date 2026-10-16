package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// The maintained runner dispatches this exclusive worker before the handler
// matrix. Its explicit gate prevents destructive archive/schema qualification
// from sharing the nonmatrix package worker's databases or silently replacing
// the official migration/identity matrix. Both disposable drivers are required.
func TestRuntimeConfigurationArchiveIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_RUNTIME_ARCHIVE") != "1" {
		t.Skip("the maintained integration runner owns the dedicated archive worker")
	}
	if driver := os.Getenv("ROUTEX_TEST_RUNTIME_ARCHIVE_CHILD_DRIVER"); driver != "" {
		db := runtimeArchiveIntegrationDB(t, driver)
		store := runtimeArchiveIntegrationStore(t, "first")
		svc := &Service{db: db, secrets: store}
		facts, err := svc.readRuntimeConfigurationArchiveV1(t.Context(), os.Getenv("ROUTEX_TEST_RUNTIME_ARCHIVE_CHILD_ID"))
		if err != nil || facts == nil || facts.Document.Source.Providers[0].Name != strings.Repeat("界", 14000) {
			t.Fatal("cold process source unavailable", err)
		}
		if facts.Document.SourceDigest != strings.Repeat("d", 64) {
			t.Fatal("cold process backfilled current source")
		}
		return
	}
	for _, driver := range []string{"postgres", "mysql"} {
		t.Run(driver, func(t *testing.T) { testRuntimeArchiveDatabase(t, driver) })
	}
}

func runtimeArchiveIntegrationDB(t *testing.T, driver string) *gorm.DB {
	t.Helper()
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("unknown archive database driver")
	}
	dsn := os.Getenv("ROUTEX_TEST_" + strings.ToUpper(driver) + "_DSN")
	if dsn == "" {
		t.Fatal("dedicated archive worker requires both disposable driver DSNs")
	}
	db, err := database.Open(t.Context(), driver, dsn)
	if err != nil {
		t.Fatal("archive database open", err)
	}
	db = db.Session(&gorm.Session{Logger: logger.Discard})
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	// This test-only identity check is deliberately confined to the disposable
	// database boundary; production archive persistence remains dialect-neutral.
	query := "SELECT current_database()"
	if driver == "mysql" {
		query = "SELECT DATABASE()"
	}
	var name string
	if err := db.Raw(query).Scan(&name).Error; err != nil || name != "routex_test" {
		t.Fatal("archive worker requires isolated routex_test database")
	}
	return db
}

func runtimeArchiveIntegrationStore(t *testing.T, write string) *secretstore.Store {
	t.Helper()
	store, err := secretstore.NewKeyring(map[string][]byte{"first": bytes.Repeat([]byte{17}, 32), "next": bytes.Repeat([]byte{23}, 32)}, "first", write)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testRuntimeArchiveDatabase(t *testing.T, driver string) {
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx := t.Context()
	t.Run("retention_v113_upgrade_and_partial_recovery", func(t *testing.T) { testRuntimeArchiveRetentionUpgrade(t, db) })
	t.Run("released_upgrade_and_concurrent_startup", func(t *testing.T) {
		if err := database.MigrateThrough(ctx, db, 111); err != nil {
			t.Fatal("empty released V111 creation", err)
		}
		sample := entity.Example{Title: "retained V111 archive predecessor"}
		if err := db.Create(&sample).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		failures := make(chan error, 4)
		for range 4 {
			wg.Go(func() { failures <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal("concurrent V112 startup", err)
			}
		}
		var retained entity.Example
		if err := db.Take(&retained, sample.ID).Error; err != nil || retained.Title != sample.Title {
			t.Fatal("released row lost", err)
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat current startup", err)
		}
		var versions int64
		if err := db.Table("schema_migrations").Count(&versions).Error; err != nil || versions != 118 {
			t.Fatal("complete V118 ledger", err)
		}
		testRuntimePublicationAcknowledgementMigration(t, db)
	})
	t.Run("cleanup_v115_migration_upgrade_and_recovery", func(t *testing.T) { testRuntimeArchiveCleanupMigration(t, db) })
	t.Run("routing_rollback_v116_migration", func(t *testing.T) { testRuntimeArchiveRollbackMigration(t, db) })
	store := runtimeArchiveIntegrationStore(t, "first")
	svc := &Service{db: db, secrets: store}
	document := configurationFixtureV1(t)
	document.PricingObservationDigest = configurationPricingDigestV1(document.PricingObservation)
	document.Source.Providers[0].Name = strings.Repeat("界", 14000)
	document.SourceDigest = strings.Repeat("d", 64)
	identity, birth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("complete_encrypted_write_identical_retry_and_cold_process", func(t *testing.T) {
		if err := svc.storeRuntimeConfigurationArchiveV1(ctx, identity, birth, document); err != nil {
			t.Fatal("archive write", err)
		}
		var original []entity.RuntimeConfigurationArchiveChunk
		if err := personalExact(db, "archive_id", identity).Order("ordinal").Find(&original).Error; err != nil || len(original) < 2 {
			t.Fatal("complete persisted chunks", err)
		}
		for _, chunk := range original {
			if strings.Contains(chunk.AuthCiphertext, document.Source.Providers[0].Name) {
				t.Fatal("plaintext persisted")
			}
		}
		if err := svc.storeRuntimeConfigurationArchiveV1(ctx, identity, birth, document); err != nil {
			t.Fatal("identical archive retry", err)
		}
		var after []entity.RuntimeConfigurationArchiveChunk
		if err := personalExact(db, "archive_id", identity).Order("ordinal").Find(&after).Error; err != nil || !reflect.DeepEqual(original, after) {
			t.Fatal("retry changed immutable archive/envelopes", err)
		}
		changed := document
		changed.SourceDigest = strings.Repeat("f", 64)
		if err := svc.storeRuntimeConfigurationArchiveV1(ctx, identity, birth, changed); err != errRuntimeArchive {
			t.Fatal("same identity different source accepted")
		}
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeConfigurationArchiveIntegration$", "-test.count=1")
		command.Env = append(os.Environ(), "ROUTEX_TEST_RUNTIME_ARCHIVE_CHILD_DRIVER="+driver, "ROUTEX_TEST_RUNTIME_ARCHIVE_CHILD_ID="+identity)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("cold child exit: %v (%d diagnostic bytes): %s", err, len(output), strings.TrimSpace(string(output)))
		}
	})
	t.Run("atomic_insert_failure_and_cancellation", func(t *testing.T) {
		callback := "archive-controlled-chunk-failure"
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "runtime_configuration_archive_chunks" {
				_ = tx.AddError(errors.New("controlled private archive failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		failedID, failedBirth, e := newRuntimeArchiveIdentity()
		if e != nil {
			t.Fatal(e)
		}
		failure := svc.storeRuntimeConfigurationArchiveV1(ctx, failedID, failedBirth, document)
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Fatal(err)
		}
		if failure != errRuntimeArchive {
			t.Fatal("failed write returned positive receipt")
		}
		var count int64
		if err := personalExact(db.Model(&entity.RuntimeConfigurationArchive{}), "id", failedID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("partial header committed", err)
		}
		if err := personalExact(db.Model(&entity.RuntimeConfigurationArchiveChunk{}), "archive_id", failedID).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("partial chunks committed", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if err := svc.storeRuntimeConfigurationArchiveV1(cancelled, failedID, failedBirth, document); err != errRuntimeArchive {
			t.Fatal("cancelled write accepted")
		}
		if facts, err := svc.readRuntimeConfigurationArchiveV1(cancelled, identity); err != errRuntimeArchive || facts != nil {
			t.Fatal("cancelled cold read accepted")
		}
	})
	t.Run("authorized_header_metadata", func(t *testing.T) { testRuntimeArchiveMetadataDatabase(t, db, svc, identity, document) })
	// Validate the original retained fixture before capacity resets its inventory.
	// Validation itself performs no storage write.
	t.Run("authenticated_integrity_review", func(t *testing.T) { testRuntimeArchiveValidationDatabase(t, db, svc, identity) })
	t.Run("failed_and_uncertain_commit_retry", func(t *testing.T) { testRuntimeArchiveCommitOutcomes(t, db, svc, document) })
	t.Run("concurrent_identical_intent", func(t *testing.T) { testRuntimeArchiveConcurrentWrites(t, db, svc, document) })
	t.Run("constraints_and_exact_order", func(t *testing.T) { testRuntimeArchiveConstraints(t, db, svc, identity) })
	t.Run("durable_capture_dedupe_caps_and_actual_bytes", func(t *testing.T) { testRuntimeArchiveDurableCapture(t, db, svc) })
	t.Run("root_epoch_cutover_inventory_cas_and_retired_readability", func(t *testing.T) { testRuntimeArchiveRootLifecycle(t, db, svc, identity, document) })
	t.Run("partial_ddl_repeat_recovery", func(t *testing.T) { testRuntimeArchivePartialDDL(t, db, svc, identity) })
	t.Run("administrator_preview_and_confirm_cleanup", func(t *testing.T) { testRuntimeArchiveCleanupDatabase(t, db, svc) })
	t.Run("authenticated_routing_rollback", func(t *testing.T) { testRuntimeArchiveRollbackDatabase(t, db, svc) })
}

func testRuntimeArchiveConstraints(t *testing.T, db *gorm.DB, svc *Service, identity string) {
	t.Helper()
	ctx := t.Context()
	var header entity.RuntimeConfigurationArchive
	if err := personalExact(db, "id", identity).Take(&header).Error; err != nil {
		t.Fatal(err)
	}
	var chunk entity.RuntimeConfigurationArchiveChunk
	if err := personalExact(db, "archive_id", identity).Order("ordinal").Take(&chunk).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		changes map[string]any
	}{
		{"unknown_codec", map[string]any{"codec_version": 2}},
		{"zero_bytes", map[string]any{"plaintext_bytes": 0}},
		{"oversized", map[string]any{"plaintext_bytes": runtimeConfigurationMaxBytesV1 + 1}},
		{"wrong_chunk_count", map[string]any{"chunk_count": header.ChunkCount + 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := personalExact(db.Model(&header), "id", identity).Updates(tc.changes).Error; err == nil {
				t.Fatal("archive bound weakened")
			}
		})
	}
	duplicate := chunk
	duplicate.ID = header.ID + "-511"
	if err := db.Omit("Archive").Create(&duplicate).Error; !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatal("ordered chunk uniqueness", err)
	}
	orphan := chunk
	orphan.ID = "rcv_01k00000000000000000000000-000"
	orphan.ArchiveID = "rcv_01k00000000000000000000000"
	if err := db.Omit("Archive").Create(&orphan).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("orphan chunk admitted", err)
	}
	if err := personalExact(db, "id", identity).Delete(&entity.RuntimeConfigurationArchive{}).Error; !errors.Is(err, gorm.ErrForeignKeyViolated) {
		t.Fatal("header deletion erased archive", err)
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := personalExact(tx, "id", chunk.ID).Delete(&entity.RuntimeConfigurationArchiveChunk{}).Error; e != nil {
			return e
		}
		if facts, e := svc.readRuntimeArchiveTx(ctx, tx, header); e != errRuntimeArchive || facts != nil {
			t.Fatal("missing chunk reconstructed partial source")
		}
		return errors.New("rollback controlled missing chunk")
	}); err == nil {
		t.Fatal("test rollback lost")
	}
	t.Run("extra_malformed_retained_chunk", func(t *testing.T) {
		model := &entity.RuntimeConfigurationArchiveChunk{}
		if err := db.Migrator().DropConstraint(model, "ck_runtime_archive_chunk_bounds"); err != nil {
			t.Fatal(err)
		}
		extra := chunk
		extra.ID = header.ID + "-511"
		extra.Ordinal = 511
		extra.AuthCiphertext = ""
		if err := db.Omit("Archive").Create(&extra).Error; err != nil {
			t.Fatal("weakened-schema extra-row fixture", err)
		}
		if facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, identity); err != errRuntimeArchive || facts != nil {
			t.Fatal("extra excluded malformed row produced complete source")
		}
		if err := personalExact(db, "id", extra.ID).Delete(model).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateConstraint(model, "ck_runtime_archive_chunk_bounds"); err != nil {
			t.Fatal(err)
		}
	})
	if facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, identity); err != nil || facts == nil {
		t.Fatal("constraint negatives damaged archive", err)
	}
}

func testRuntimeArchiveRootLifecycle(t *testing.T, db *gorm.DB, svc *Service, identity string, document runtimeConfigurationV1) {
	t.Helper()
	ctx := t.Context()
	first, next := "first", "next"
	birth := time.Now().UTC().Truncate(time.Microsecond)
	keys := []entity.SecretRootKey{{KeyID: first, State: "write", CreatedAt: birth}, {KeyID: next, State: "decrypt_only", CreatedAt: birth}}
	for i := range keys {
		writer, err := svc.secrets.WithWriteKey(keys[i].KeyID)
		if err != nil {
			t.Fatal(err)
		}
		keys[i].ProofCiphertext, err = writer.Seal(rootProofReference(keys[i].KeyID), rootProofPlaintext)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&keys[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	policy := entity.SecretWritePolicy{ID: 1, Initialized: true, WriteKeyID: &first, Epoch: 1, ETag: strings.Repeat("a", 64), UpdatedAt: birth}
	if err := db.Save(&policy).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.installSecretPolicy(policy, keys); err != nil {
		t.Fatal(err)
	}
	// The durable singleton advances while this process still has its old view.
	// A prepared stale write must fail rather than silently reseal or partially save.
	policy.Epoch = 2
	policy.WriteKeyID = &next
	policy.ETag = strings.Repeat("b", 64)
	if err := db.Save(&policy).Error; err != nil {
		t.Fatal(err)
	}
	staleID, staleBirth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.storeRuntimeConfigurationArchiveV1(ctx, staleID, staleBirth, document); err != errRuntimeArchive {
		t.Fatal("stale root write committed")
	}
	var count int64
	if err := personalExact(db.Model(&entity.RuntimeConfigurationArchive{}), "id", staleID).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("root cutover retained partial source", err)
	}
	keys[0].State = "decrypt_only"
	keys[1].State = "write"
	for i := range keys {
		if err := db.Save(&keys[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.installSecretPolicy(policy, keys); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.rootInventoryPage(ctx, "runtime_config_chunks", "")
	if err != nil || len(rows) < 2 {
		t.Fatal("retained archive inventory", err)
	}
	for _, row := range rows {
		rewrapped, err := svc.secrets.Rewrap(row.reference, row.ciphertext, next)
		if err != nil {
			t.Fatal(err)
		}
		changed := row
		changed.generation = strings.Repeat("f", 64)
		if err := db.Transaction(func(tx *gorm.DB) error {
			outcome, e := svc.rootCAS(tx, "runtime_config_chunks", changed, rewrapped)
			if e != nil {
				return e
			}
			if outcome != "changed" {
				t.Fatal("stale archive CAS updated source")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			outcome, e := svc.rootCAS(tx, "runtime_config_chunks", row, rewrapped)
			if e != nil {
				return e
			}
			if outcome != "rewrapped" {
				t.Fatal("exact archive CAS failed")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	keys[0].State = "retired"
	keys[0].ProofCiphertext = ""
	keys[0].RetiredAt = &birth
	if err := db.Save(&keys[0]).Error; err != nil {
		t.Fatal(err)
	}
	retired, err := secretstore.NewKeyring(map[string][]byte{next: bytes.Repeat([]byte{23}, 32)}, next, next)
	if err != nil {
		t.Fatal(err)
	}
	svc.secrets = retired
	if err := svc.installSecretPolicy(policy, keys); err != nil {
		t.Fatal(err)
	}
	facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, identity)
	if err != nil || facts == nil || !reflect.DeepEqual(facts.Document, document) {
		t.Fatal("archive lost after retired root removal", err)
	}
	freshID, freshBirth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.storeRuntimeConfigurationArchiveV1(ctx, freshID, freshBirth, document); err != nil {
		t.Fatal("current-root archive write", err)
	}
	testRuntimeArchiveAutomaticLifecycle(t, db, svc)
}

type runtimeArchivePartialTable struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (runtimeArchivePartialTable) TableName() string { return "runtime_configuration_archives" }

type runtimeArchiveTestLedger struct {
	Version int `gorm:"primaryKey;autoIncrement:false"`
}

func (runtimeArchiveTestLedger) TableName() string { return "schema_migrations" }

func testRuntimeArchivePartialDDL(t *testing.T, db *gorm.DB, svc *Service, identity string) {
	t.Helper()
	ctx := t.Context()
	// This isolated scenario intentionally removes only this worker's archive
	// fixtures to reproduce empty interrupted nontransactional schema creation.
	// A successor receipt may be removed only with its complete empty overlay;
	// retained controller identities or advisory reports must never be discarded.
	var before []runtimeArchiveCleanupLedger
	if err := db.Order("version").Find(&before).Error; err != nil || len(before) != 118 {
		t.Fatal("archive reconstruction requires exact V118 ledger", err)
	}
	if err := admissionOwnerFixtureV118(db, len(before)); err != nil {
		t.Fatal(err)
	}
	for _, model := range []any{&entity.RuntimePublicationAcknowledgement{}, &entity.RuntimeArchiveRollback{}, &entity.RuntimeArchiveCleanup{}, &entity.APIKeyDeliveryControllerAcknowledgement{}, &entity.APIKeyDeliveryControllerGrant{}} {
		var count int64
		if !db.Migrator().HasTable(model) || db.Model(model).Count(&count).Error != nil || count != 0 {
			t.Fatal("retained successor history prevents isolated archive fixture reconstruction")
		}
	}
	if err := peelAdmissionOwnerFixtureV118(db, before); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&entity.RuntimePublicationAcknowledgement{}, &entity.RuntimeArchiveRollback{}, &entity.RuntimeArchiveCleanup{}, &entity.APIKeyDeliveryControllerAcknowledgement{}, &entity.APIKeyDeliveryControllerGrant{}, &entity.RuntimeArchiveCapture{}, &entity.RuntimeArchiveContent{}, &entity.RuntimeArchiveRetention{}, &entity.RuntimeConfigurationArchiveChunk{}, &entity.RuntimeConfigurationArchive{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("version IN ?", []int{112, 113, 114, 115, 116, 117}).Delete(&runtimeArchiveTestLedger{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&runtimeArchivePartialTable{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("partial empty archive DDL recovery", err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("partial recovery repeat", err)
	}
	// The removed fixture cannot be mistaken for successful recovered content.
	if facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, identity); err != errRuntimeArchive || facts != nil {
		t.Fatal("removed content falsely reconstructed")
	}
	id, birth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	doc := configurationFixtureV1(t)
	if err := svc.storeRuntimeConfigurationArchiveV1(ctx, id, birth, doc); err != nil {
		t.Fatal("write after partial schema recovery", err)
	}
	if facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, id); err != nil || facts == nil {
		t.Fatal("read after partial schema recovery", err)
	}
}

// Fault wrappers surround real driver transactions. A failed acknowledgement
// is distinguished from rollback: only an explicit identical retry may resolve
// a complete retained archive, never a successful-return inference.
type runtimeArchiveCommitPool struct {
	*sql.DB
	uncertain bool
}

func (p runtimeArchiveCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &runtimeArchiveCommitTx{Tx: tx, uncertain: p.uncertain}, nil
}

type runtimeArchiveCommitTx struct {
	*sql.Tx
	uncertain bool
}

func (tx runtimeArchiveCommitTx) Commit() error {
	if tx.uncertain {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	} else {
		if err := tx.Rollback(); err != nil {
			return err
		}
	}
	return errors.New("controlled private archive commit acknowledgement failure")
}

func testRuntimeArchiveCommitOutcomes(t *testing.T, db *gorm.DB, svc *Service, document runtimeConfigurationV1) {
	t.Helper()
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, uncertain := range []bool{false, true} {
		name := "rolled_back"
		if uncertain {
			name = "committed_unknown"
		}
		t.Run(name, func(t *testing.T) {
			faultDB := db.Session(&gorm.Session{NewDB: true, Context: t.Context()})
			faultDB.Statement.ConnPool = runtimeArchiveCommitPool{DB: pool, uncertain: uncertain}
			fault := &Service{db: faultDB, secrets: svc.secrets}
			identity, birth, err := newRuntimeArchiveIdentity()
			if err != nil {
				t.Fatal(err)
			}
			if err := fault.storeRuntimeConfigurationArchiveV1(t.Context(), identity, birth, document); err != errRuntimeArchive {
				t.Fatal("uncertain commit returned positive storage receipt")
			}
			var count int64
			if err := personalExact(db.Model(&entity.RuntimeConfigurationArchive{}), "id", identity).Count(&count).Error; err != nil || (count == 1) != uncertain {
				t.Fatal("real commit fault boundary", err)
			}
			if err := svc.storeRuntimeConfigurationArchiveV1(t.Context(), identity, birth, document); err != nil {
				t.Fatal("explicit identical retry failed", err)
			}
			if facts, err := svc.readRuntimeConfigurationArchiveV1(t.Context(), identity); err != nil || facts == nil {
				t.Fatal("identical retry retained no complete facts", err)
			}
		})
	}
}

func testRuntimeArchiveConcurrentWrites(t *testing.T, db *gorm.DB, svc *Service, document runtimeConfigurationV1) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	identity, birth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for range 4 {
		wg.Go(func() { failures <- svc.storeRuntimeConfigurationArchiveV1(ctx, identity, birth, document) })
	}
	wg.Wait()
	close(failures)
	committed := false
	for err := range failures {
		if err == nil {
			committed = true
		} else if err != errRuntimeArchive {
			t.Fatal("unsanitized concurrent archive failure")
		}
	}
	if !committed {
		t.Fatal("no identical writer completed")
	}
	if err := svc.storeRuntimeConfigurationArchiveV1(ctx, identity, birth, document); err != nil {
		t.Fatal("concurrent identical retry", err)
	}
	var count int64
	if err := personalExact(db.Model(&entity.RuntimeConfigurationArchive{}), "id", identity).Count(&count).Error; err != nil || count != 1 {
		t.Fatal("concurrent write duplicated header", err)
	}
	if facts, err := svc.readRuntimeConfigurationArchiveV1(ctx, identity); err != nil || facts == nil {
		t.Fatal("concurrent source incomplete", err)
	}
}

// Metadata reads deliberately work without an envelope opener. The retained
// headers do not prove that their encrypted source can currently reconstruct.
func testRuntimeArchiveMetadataDatabase(t *testing.T, db *gorm.DB, writer *Service, identity string, document runtimeConfigurationV1) {
	secondID, secondBirth, err := newRuntimeArchiveIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.storeRuntimeConfigurationArchiveV1(t.Context(), secondID, secondBirth, document); err != nil {
		t.Fatal("second persisted metadata fixture", err)
	}
	t.Helper()
	ctx := t.Context()
	actorID := configurationTestIDV1("usr", 101)
	readerID := configurationTestIDV1("usr", 102)
	roleID := configurationTestIDV1("rol", 101)
	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, u := range []entity.User{
		{ID: actorID, Email: "archive-admin@example.test", Name: "Archive administrator", PasswordHash: "test-only", Role: entity.RoleAdmin, CreatedAt: now},
		{ID: readerID, Email: "archive-reader@example.test", Name: "Archive metadata reader", PasswordHash: "test-only", Role: entity.RoleMember, CreatedAt: now},
	} {
		if err := db.Create(&u).Error; err != nil {
			t.Fatal("metadata actor fixture", err)
		}
	}
	if err := db.Create(&entity.Role{ID: roleID, Name: "Archive read only", NameKey: strings.Repeat("a", 64), CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: roleID, Permission: "system.read"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.UserRole{RoleID: roleID, UserID: readerID}).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{db: db}
	reads := 0
	callback := "test:archive-metadata-headers-only"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "runtime_configuration_archive_chunks" {
			_ = tx.AddError(errors.New("metadata must not read envelopes"))
		}
		if tx.Statement.Table == "runtime_configuration_archives" {
			reads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Callback().Query().Remove(callback); err != nil {
			t.Error(err)
		}
	}()
	page, err := svc.ListRuntimeArchiveMetadata(ctx, readerID, RuntimeArchiveMetadataFilter{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil || page.Scope != "archive_metadata_only" {
		t.Fatal("authorized bounded header page", page, err)
	}
	seen := map[string]bool{}
	for {
		for _, row := range page.Items {
			if seen[row.ID] {
				t.Fatal("duplicate metadata")
			}
			seen[row.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		page, err = svc.ListRuntimeArchiveMetadata(ctx, readerID, RuntimeArchiveMetadataFilter{Limit: 1, Cursor: *page.NextCursor})
		if err != nil {
			t.Fatal("metadata pagination", err)
		}
	}
	if !seen[identity] {
		t.Fatal("persisted archive not reachable")
	}
	before := reads
	if _, err := svc.ListRuntimeArchiveMetadata(ctx, strings.ToUpper(readerID), RuntimeArchiveMetadataFilter{}); err == nil || reads != before {
		t.Fatal("actor alias borrowed metadata", err)
	}
	if err := personalExact(db, "user_id", readerID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListRuntimeArchiveMetadata(ctx, readerID, RuntimeArchiveMetadataFilter{}); err == nil || reads != before {
		t.Fatal("revoked permission read metadata", err)
	}
	if err := personalExact(db.Model(&entity.User{}), "id", actorID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListRuntimeArchiveMetadata(ctx, actorID, RuntimeArchiveMetadataFilter{}); err == nil || reads != before {
		t.Fatal("disabled actor read metadata", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if page, err := svc.ListRuntimeArchiveMetadata(cancelled, readerID, RuntimeArchiveMetadataFilter{}); err == nil || page != nil {
		t.Fatal("cancelled metadata read", page, err)
	}
}
