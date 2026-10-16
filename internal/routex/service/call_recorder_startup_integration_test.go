package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

// Own a fresh database per selected driver. No extra migration or economic
// test-only table is created by this startup-isolation worker.
func TestLocalRecorderStartupSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_LOCAL_RECORDER_STARTUP") != "1" {
		t.Skip("exclusive local-recorder startup worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_LOCAL_RECORDER_STARTUP_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("startup worker requires a fresh exclusively owned schema")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var migrationPrefix []admissionOwnerTestLedger
	if err := db.Order("version").Find(&migrationPrefix).Error; err != nil {
		t.Fatal(err)
	}
	svc := &Service{db: db}
	path := filepath.Join(t.TempDir(), "local.db")
	var identity string
	t.Run("fresh_sql_identity_commit_and_file_bind_ack_retry", func(t *testing.T) {
		if err := svc.prepareLocalRecorderIdentity(ctx, path); err != nil {
			t.Fatal("persist only fresh identity", err)
		}
		var binding entity.LimitInstallation
		var modes []entity.AdmissionMode
		if err := db.Take(&binding, 1).Error; err != nil || binding.Initialized || !admissionOwnerJournal.MatchString(binding.JournalID) {
			t.Fatal("fresh identity commit changed initialization", err)
		}
		identity = binding.JournalID
		if err := db.Find(&modes).Error; err != nil || len(modes) != 0 {
			t.Fatal("V118 forbids mode before initialization", err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("SQL preparation opened local ledger", err)
		}
		if err := svc.prepareLocalRecorderIdentity(ctx, path); err != nil {
			t.Fatal("restart after identity commit", err)
		}
		var again entity.LimitInstallation
		if err := db.Take(&again, 1).Error; err != nil || again != binding {
			t.Fatal("fresh retry changed original identity", err)
		}
		// Simulate loss of the SQL acknowledgement after the exact durable file
		// bind, without retaining an original live recorder across this seam.
		queue, err := eventqueue.OpenBoundLocal(ctx, path, callQueueCapacity, callQueuePayloadLimit, identity, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.Close(); err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal("repeat V118 after unacknowledged fresh bind", err)
		}
		queue, err = svc.openLocalRecorderJournal(ctx, path)
		if err != nil {
			t.Fatal("exact fresh bind retry", err)
		}
		if err := queue.Close(); err != nil {
			t.Fatal(err)
		}
		localRecorderSQLRequireLocal(t, db, identity)
	})
	t.Run("explicit_local_restart_preserves_pending_fact", func(t *testing.T) {
		queue, err := svc.openLocalRecorderJournal(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.Reserve("retained_local_request", []byte("retained original fallback")); err != nil {
			t.Fatal(err)
		}
		if err := queue.Close(); err != nil {
			t.Fatal(err)
		}
		queue, err = svc.openLocalRecorderJournal(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = queue.Close() }()
		entries, err := queue.Read(8)
		if err != nil || len(entries) != 1 || string(entries[0].Payload) != "retained original fallback" {
			t.Fatal("explicit local restart changed ordinary recovery", err)
		}
	})
	for _, test := range []struct {
		name   string
		change func(*gorm.DB) *gorm.DB
	}{
		{"initialized_missing_mode", func(tx *gorm.DB) *gorm.DB { return tx.Delete(&entity.AdmissionMode{}, 1) }},
		{"shared_mode", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Updates(map[string]any{"mode": "shared", "revision": int64(2)})
		}},
		{"foreign_mode_installation", func(tx *gorm.DB) *gorm.DB {
			foreign := "led_01aaaaaaaaaaaaaaaaaaaaaaaa"
			return tx.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("installation_id", foreign)
		}},
		{"mode_with_uninitialized_binding", func(tx *gorm.DB) *gorm.DB {
			return tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Update("initialized", false)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer func() { _ = tx.Rollback().Error }()
			sharedEconomicHistorySQLChanged(t, test.change(tx))
			before := localRecorderSQLFile(t, path)
			candidate := &Service{db: tx}
			if err := candidate.StartCallRecorder(ctx, path); err == nil || candidate.recorder != nil || candidate.admission != nil {
				t.Fatal("contradictory mode started local recorder", err)
			}
			if !bytes.Equal(before, localRecorderSQLFile(t, path)) {
				t.Fatal("mode refusal mutated retained local bytes")
			}
		})
	}
	t.Run("exact_id_nonempty_uninitialized_cannot_reconcile_mode", func(t *testing.T) {
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		if err := tx.Delete(&entity.AdmissionMode{}, 1).Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicHistorySQLChanged(t, tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Update("initialized", false))
		before := localRecorderSQLFile(t, path)
		q, err := (&Service{db: tx}).openLocalRecorderJournal(ctx, path)
		if q != nil || !errors.Is(err, eventqueue.ErrInvalid) || !bytes.Equal(before, localRecorderSQLFile(t, path)) {
			t.Fatal("retained same-ID history became a fresh installation", err)
		}
		var modes int64
		if err := tx.Model(&entity.AdmissionMode{}).Count(&modes).Error; err != nil || modes != 0 {
			t.Fatal("rejected fresh reconciliation manufactured mode", err)
		}
	})
	t.Run("absent_established_path_creates_neither_parents_nor_file", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "not-created")
		q, err := svc.openLocalRecorderJournal(ctx, filepath.Join(parent, "local.db"))
		if q != nil || !errors.Is(err, eventqueue.ErrInvalid) {
			t.Fatal("missing established local history admitted", err)
		}
		if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("missing established startup created parents", err)
		}
		localRecorderSQLRequireLocal(t, db, identity)
	})
	t.Run("accounting_started_uninitialized_domain_cannot_seed_mode", func(t *testing.T) {
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		if err := tx.Delete(&entity.AdmissionMode{}, 1).Error; err != nil {
			t.Fatal(err)
		}
		sharedEconomicHistorySQLChanged(t, tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Update("initialized", false))
		sharedEconomicHistorySQLChanged(t, tx.Model(&entity.QuotaSetting{}).Where("id = ?", 1).Update("accounting_started", true))
		parent := filepath.Join(t.TempDir(), "not-created")
		q, err := (&Service{db: tx}).openLocalRecorderJournal(ctx, filepath.Join(parent, "local.db"))
		if q != nil || !errors.Is(err, eventqueue.ErrInvalid) {
			t.Fatal("accounting history was treated as fresh", err)
		}
		if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed accounting proof created a ledger", err)
		}
		var modes int64
		if err := tx.Model(&entity.AdmissionMode{}).Count(&modes).Error; err != nil || modes != 0 {
			t.Fatal("accounting refusal manufactured a local mode", err)
		}
	})
	t.Run("unreadable_sql_authority_preserves_file", func(t *testing.T) {
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		before := localRecorderSQLFile(t, path)
		candidate := &Service{db: tx}
		if err := candidate.StartCallRecorder(ctx, path); err == nil || candidate.recorder != nil || candidate.admission != nil ||
			!bytes.Equal(before, localRecorderSQLFile(t, path)) {
			t.Fatal("unreadable SQL authority fell back to local recovery", err)
		}
	})
	t.Run("cancelled_governance_lock_never_opens_file", func(t *testing.T) {
		held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		if err := lockGovernance(held); err != nil {
			t.Fatal(err)
		}
		blocked, stop := context.WithTimeout(ctx, 150*time.Millisecond)
		defer stop()
		before := localRecorderSQLFile(t, path)
		q, err := svc.openLocalRecorderJournal(blocked, path)
		if q != nil || err == nil || blocked.Err() != context.DeadlineExceeded || !bytes.Equal(before, localRecorderSQLFile(t, path)) {
			t.Fatal("cancelled SQL authority check opened or mutated file", err)
		}
	})
	var currentPrefix []admissionOwnerTestLedger
	if err := db.Order("version").Find(&currentPrefix).Error; err != nil || !reflect.DeepEqual(migrationPrefix, currentPrefix) {
		t.Fatal("startup isolation changed frozen migration history", err)
	}
}

func localRecorderSQLFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func localRecorderSQLRequireLocal(t *testing.T, db *gorm.DB, identity string) {
	t.Helper()
	var binding entity.LimitInstallation
	var mode entity.AdmissionMode
	if err := db.Take(&binding, 1).Error; err != nil || !binding.Initialized || binding.JournalID != identity {
		t.Fatal("local startup did not retain original established identity", err)
	}
	if err := db.Take(&mode, 1).Error; err != nil || mode.ID != 1 || mode.InstallationID != identity || mode.Mode != "local" || mode.Revision != 1 {
		t.Fatal("local startup did not acknowledge explicit coherent mode", err)
	}
}
