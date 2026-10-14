package service

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type runtimeArchiveCleanupLedger struct {
	Version   int `gorm:"primaryKey;autoIncrement:false"`
	AppliedAt string
}

func (runtimeArchiveCleanupLedger) TableName() string { return "schema_migrations" }

// The dedicated archive worker exclusively owns this table and its fixture rows.
func testRuntimeArchiveCleanupMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	peelRuntimeArchiveRollbackFixture(t, db)
	t.Cleanup(func() {
		if err := database.Migrate(context.WithoutCancel(t.Context()), db); err != nil {
			t.Error("restore V116 after historical cleanup fixture", err)
		}
	})
	ledger := func(t *testing.T) []runtimeArchiveCleanupLedger {
		t.Helper()
		var rows []runtimeArchiveCleanupLedger
		if err := db.Order("version").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	before := ledger(t)
	if len(before) != 115 {
		t.Fatal("cleanup migration requires V115 head")
	}
	removeReceipt := func(t *testing.T) {
		t.Helper()
		r := db.Where("version = ?", 115).Delete(&runtimeArchiveTestLedger{})
		if r.Error != nil || r.RowsAffected != 1 || !reflect.DeepEqual(ledger(t), before[:114]) {
			t.Fatal("cleanup fixture changed predecessor ledger", r.Error)
		}
	}
	migrate := func(t *testing.T) {
		t.Helper()
		if err := database.MigrateThrough(t.Context(), db, 115); err != nil {
			t.Fatal("cleanup migration", err)
		}
		if after := ledger(t); len(after) != 115 || !reflect.DeepEqual(after[:114], before[:114]) {
			t.Fatal("cleanup migration rewrote historical prefix")
		}
	}
	t.Run("empty_upgrade_repeat_concurrent", func(t *testing.T) {
		var count int64
		if db.Model(&entity.RuntimeArchiveCleanup{}).Count(&count).Error != nil || count != 0 {
			t.Fatal("retained cleanup table must not be peeled")
		}
		removeReceipt(t)
		if err := db.Migrator().DropTable(&entity.RuntimeArchiveCleanup{}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.MigrateThrough(t.Context(), db, 115) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal("concurrent cleanup startup", err)
			}
		}
		migrate(t)
		stable := ledger(t)
		migrate(t)
		if !reflect.DeepEqual(stable, ledger(t)) {
			t.Fatal("repeat changed immutable cleanup receipt")
		}
	})
	t.Run("empty_partial_column_repaired", func(t *testing.T) {
		removeReceipt(t)
		if err := db.Migrator().DropConstraint(&entity.RuntimeArchiveCleanup{}, "ck_runtime_archive_cleanup_payload"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(&entity.RuntimeArchiveCleanup{}, "Reason"); err != nil {
			t.Fatal(err)
		}
		migrate(t)
		if !db.Migrator().HasColumn(&entity.RuntimeArchiveCleanup{}, "Reason") {
			t.Fatal("empty cleanup column not repaired")
		}
	})
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	row := entity.RuntimeArchiveCleanup{RequestID: "00000000-0000-4000-8000-000000000001", ActorID: configurationTestIDV1("usr", 702), ActorCreatedAt: stamp, ReviewETag: strings.Repeat("a", 64), Reason: "Owned migration fixture", ArchiveIDsJSON: `["rcv_00000000000000000000000001"]`, DeletedCount: 1, DeletedBytes: 64, CompletedAt: stamp}
	t.Run("retained_receipt_repeat_and_constraints", func(t *testing.T) {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		removeReceipt(t)
		migrate(t)
		var saved entity.RuntimeArchiveCleanup
		if err := db.Take(&saved, "request_id = ?", row.RequestID).Error; err != nil {
			t.Fatal("retained receipt read", err)
		}
		// Drivers may return different location objects for the same instant.
		// Normalize representation while still comparing every persisted field.
		saved.ActorCreatedAt = saved.ActorCreatedAt.UTC()
		saved.CompletedAt = saved.CompletedAt.UTC()
		if !reflect.DeepEqual(saved, row) {
			t.Fatal("retained receipt changed")
		}
		for _, change := range []map[string]any{{"reason": ""}, {"reason": strings.Repeat("a", 4001)}, {"archive_ids_json": strings.Repeat("a", 662)}, {"deleted_count": 0}, {"deleted_count": 21}, {"deleted_bytes": 0}, {"actor_id": "bad"}, {"review_etag": "bad"}, {"completed_at": stamp.Add(-time.Microsecond)}} {
			if db.Model(&entity.RuntimeArchiveCleanup{}).Where("request_id = ?", row.RequestID).Updates(change).Error == nil {
				t.Fatal("durable cleanup constraint weakened", change)
			}
		}
	})
	t.Run("nonempty_partial_schema_refused_before_synthesis", func(t *testing.T) {
		removeReceipt(t)
		if err := db.Migrator().DropConstraint(&entity.RuntimeArchiveCleanup{}, "ck_runtime_archive_cleanup_payload"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(&entity.RuntimeArchiveCleanup{}, "Reason"); err != nil {
			t.Fatal(err)
		}
		if database.MigrateThrough(t.Context(), db, 115) == nil || db.Migrator().HasColumn(&entity.RuntimeArchiveCleanup{}, "Reason") || !reflect.DeepEqual(ledger(t), before[:114]) {
			t.Fatal("retained missing intent synthesized or receipt recorded")
		}
		var count int64
		if db.Table("runtime_archive_cleanups").Count(&count).Error != nil || count != 1 {
			t.Fatal("failed migration deleted retained receipt")
		}
		// Explicitly discard only this test's synthetic damaged table; no product
		// migration repairs historical intent by deleting or inventing it.
		if err := db.Migrator().DropTable(&entity.RuntimeArchiveCleanup{}); err != nil {
			t.Fatal(err)
		}
		migrate(t)
	})
}
