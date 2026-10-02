package handler

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

func testCallCredentialAttributionMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	call := entity.CallRecord{RequestID: "req_credential_upgrade", SnapshotID: "cfg_parent_historical", UserID: "usr_historical", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now}
	attempt := entity.CallAttempt{ID: "att_credential_upgrade", RequestID: call.RequestID, Status: "success", StartedAt: now, CompletedAt: now}
	for _, row := range []any{&call, &attempt} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	model := &entity.CallAttempt{}
	dropCallCredentialAttributionTestIndex(t, db)
	for _, field := range []string{"CredentialID", "SnapshotID"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
	}
	removeCallCredentialAttributionLedger(t, db)
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCallCredentialAttributionSchema(t, db)
	var upgraded entity.CallAttempt
	if err := db.Select("id", "credential_id", "snapshot_id", "status", "completed_at").First(&upgraded, "id = ?", attempt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.CredentialID != "" || upgraded.SnapshotID != "" || upgraded.Status != attempt.Status || !upgraded.CompletedAt.Equal(now) {
		t.Fatal("V31 upgrade invented attribution or changed immutable attempt")
	}
	// No live FK: historical IDs remain valid without catalog/publication rows.
	if err := db.Model(model).Where("id = ?", attempt.ID).Updates(map[string]any{"credential_id": "crd_deleted_historical", "snapshot_id": "cfg_deleted_historical"}).Error; err != nil {
		t.Fatal("historical attribution acquired a live FK")
	}
	for _, field := range []string{"CredentialID", "SnapshotID"} {
		if err := db.Model(model).Where("id = ?", attempt.ID).Updates(map[string]any{"credential_id": "crd_deleted_historical", "snapshot_id": "cfg_deleted_historical"}).Error; err != nil {
			t.Fatal(err)
		}
		dropCallCredentialAttributionTestIndex(t, db)
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
		removeCallCredentialAttributionLedger(t, db)
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		assertCallCredentialAttributionSchema(t, db)
		upgraded = entity.CallAttempt{}
		if err := db.Select("id", "credential_id", "snapshot_id").First(&upgraded, "id = ?", attempt.ID).Error; err != nil {
			t.Fatal(err)
		}
		if field == "CredentialID" && (upgraded.CredentialID != "" || upgraded.SnapshotID != "cfg_deleted_historical") || field == "SnapshotID" && (upgraded.CredentialID != "crd_deleted_historical" || upgraded.SnapshotID != "") {
			t.Fatal("partial column repair inferred or changed retained attribution")
		}
	}
	if err := db.Model(model).Where("id = ?", attempt.ID).Updates(map[string]any{"credential_id": "crd_retained", "snapshot_id": "cfg_retained"}).Error; err != nil {
		t.Fatal(err)
	}
	dropCallCredentialAttributionTestIndex(t, db)
	removeCallCredentialAttributionLedger(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertCallCredentialAttributionSchema(t, db)
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if err := db.Select("id", "credential_id", "snapshot_id").First(&upgraded, "id = ?", attempt.ID).Error; err != nil || upgraded.CredentialID != "crd_retained" || upgraded.SnapshotID != "cfg_retained" {
		t.Fatal("index-only repair or repeat migration changed attribution")
	}
	for _, column := range []string{"credential_id", "snapshot_id"} {
		if err := db.Model(model).Where("id = ?", attempt.ID).Update(column, nil).Error; err == nil {
			t.Fatalf("%s accepted NULL", column)
		}
	}
}

func removeCallCredentialAttributionLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 32).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("cannot reconstruct V32 migration ledger")
	}
}

func dropCallCredentialAttributionTestIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	const name = "idx_attempts_credential_snapshot_time"
	if !db.Migrator().HasIndex(&entity.CallAttempt{}, name) {
		t.Fatal("attribution fault-injection index missing before removal")
	}
	if db.Name() == "postgres" {
		// Pinned GORM DropIndex renders invalid CURRENT_SCHEMA().index SQL.
		// The fixed fixture-only identifier has no caller input; production
		// index creation and interrupted migration repair use GORM throughout.
		if err := db.Exec("DROP INDEX idx_attempts_credential_snapshot_time").Error; err != nil {
			t.Fatal(err)
		}
	} else if err := db.Migrator().DropIndex(&entity.CallAttempt{}, name); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasIndex(&entity.CallAttempt{}, name) {
		t.Fatal("attribution fault injection did not remove index")
	}
}

func assertCallCredentialAttributionSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	columns, err := db.Migrator().ColumnTypes(&entity.CallAttempt{})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, column := range columns {
		if column.Name() != "credential_id" && column.Name() != "snapshot_id" {
			continue
		}
		seen[column.Name()] = true
		if nullable, known := column.Nullable(); !known || nullable {
			t.Fatalf("%s must be nonnull", column.Name())
		}
		if size, known := column.Length(); !known || size != 30 {
			t.Fatalf("%s must be VARCHAR(30)", column.Name())
		}
		if value, known := column.DefaultValue(); !known || value != "" {
			t.Fatalf("%s default=%q known=%v must be blank unknown", column.Name(), value, known)
		}
	}
	if !seen["credential_id"] || !seen["snapshot_id"] {
		t.Fatal("attribution migration missing column")
	}
	if db.Name() == "postgres" {
		// Pinned GORM GetIndexes joins pg_attribute without index-key ordinal
		// ordering. Verify the physical key order with PostgreSQL catalog
		// ordinality; both lookup names are fixed and values parameterized.
		var ordered []string
		if err := db.Raw(`SELECT a.attname FROM pg_index i
			JOIN pg_class x ON x.oid = i.indexrelid
			JOIN pg_class t ON t.oid = i.indrelid
			JOIN pg_namespace n ON n.oid = t.relnamespace
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, position)
			JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = k.attnum
			WHERE n.nspname = current_schema() AND t.relname = ? AND x.relname = ?
			ORDER BY k.position`, "call_attempts", "idx_attempts_credential_snapshot_time").Scan(&ordered).Error; err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(ordered, []string{"credential_id", "snapshot_id", "completed_at", "id"}) {
			t.Fatalf("physical attribution index wrong order: %v", ordered)
		}
		return
	}
	indexes, err := db.Migrator().GetIndexes(&entity.CallAttempt{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range indexes {
		if index.Name() == "idx_attempts_credential_snapshot_time" {
			if !slices.Equal(index.Columns(), []string{"credential_id", "snapshot_id", "completed_at", "id"}) {
				t.Fatalf("attribution index wrong order: %v", index.Columns())
			}
			return
		}
	}
	t.Fatal("attribution index missing")
}
