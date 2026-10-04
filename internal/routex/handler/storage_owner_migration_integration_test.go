package handler

import (
	"context"
	"slices"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

type storageOwnerV23Integration struct {
	OwnerKind string `gorm:"size:16;not null;default:user;index:idx_storage_objects_scope,priority:1;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project')"`
	OwnerID   string `gorm:"size:30;not null;index:idx_storage_objects_scope,priority:2"`
}

func (storageOwnerV23Integration) TableName() string { return "storage_objects" }

func testStorageOwnerMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	revision := entity.StorageRevision{
		ID:               "str_owner_migration",
		Endpoint:         "https://storage.example.invalid",
		Region:           "test-region",
		Bucket:           "test-bucket",
		Prefix:           "tests/",
		SecretGeneration: "sec_owner_migration",
		AuthCiphertext:   "test-ciphertext",
		CreatedBy:        "usr_owner_migration",
	}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	legacy := entity.StorageObject{
		ID:            "obj_owner_migration_legacy",
		OwnerKind:     entity.StorageOwnerUser,
		OwnerID:       "usr_owner_migration",
		RevisionID:    revision.ID,
		Purpose:       "attachment",
		State:         "ready",
		Name:          "legacy.txt",
		MIME:          "text/plain",
		SHA256:        "legacy",
		NextCleanupAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	model := &storageOwnerV23Integration{}
	renameStorageOwnerTestIndex(t, db)
	rewindTeamStorageOwnerGuard(t, db)
	// Removing the column reproduces the V22 schema and lets each database drop
	// its dependent V23 constraint. The renamed residual index is intentionally
	// ignored so the migration must create the canonical composite index again.
	if err := db.Migrator().DropColumn(model, "OwnerKind"); err != nil {
		t.Fatal(err)
	}
	result := db.Table("schema_migrations").Where("version = ?", 23).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration 23 ledger: affected=%d, err=%v", result.RowsAffected, result.Error)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	var upgraded entity.StorageObject
	if err := db.First(&upgraded, "id = ?", legacy.ID).Error; err != nil {
		t.Fatal(err)
	}
	if upgraded.OwnerKind != entity.StorageOwnerUser || upgraded.OwnerID != legacy.OwnerID {
		t.Fatalf("legacy owner changed: kind=%q id=%q", upgraded.OwnerKind, upgraded.OwnerID)
	}
	assertStorageOwnerSchema(t, db)
	dropStorageOwnerTestIndex(t, db, "idx_storage_objects_scope_v23_reset")

	// Reproduce an interrupted MySQL migration after AddColumn but before its
	// backfill, constraint, and index DDL have all been reconciled.
	rewindTeamStorageOwnerGuard(t, db)
	if err := db.Migrator().DropConstraint(model, "ck_storage_objects_owner_kind"); err != nil {
		t.Fatal(err)
	}
	dropStorageOwnerTestIndex(t, db, "idx_storage_objects_scope")
	if err := db.Model(model).Where("id = ?", legacy.ID).Update("owner_kind", "").Error; err != nil {
		t.Fatal(err)
	}
	result = db.Table("schema_migrations").Where("version = ?", 23).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove partial migration 23 ledger: affected=%d, err=%v", result.RowsAffected, result.Error)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertStorageOwnerSchema(t, db)
	if err := db.First(&upgraded, "id = ?", legacy.ID).Error; err != nil || upgraded.OwnerKind != entity.StorageOwnerUser {
		t.Fatalf("partial migration backfill failed: kind=%q err=%v", upgraded.OwnerKind, err)
	}

	// Reapplying an unledgered completed step must preserve the same row and schema.
	result = db.Table("schema_migrations").Where("version = ?", 23).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove completed migration 23 ledger: affected=%d, err=%v", result.RowsAffected, result.Error)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertStorageOwnerSchema(t, db)

	project := entity.StorageObject{
		ID:            "obj_owner_migration_project",
		OwnerKind:     entity.StorageOwnerProject,
		OwnerID:       "prj_owner_migration",
		RevisionID:    revision.ID,
		Purpose:       "attachment",
		State:         "ready",
		Name:          "project.txt",
		MIME:          "text/plain",
		SHA256:        "project",
		NextCleanupAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&project).Error; err != nil {
		t.Fatalf("project owner must be accepted: %v", err)
	}
	invalid := project
	invalid.ID = "obj_owner_migration_invalid"
	invalid.OwnerKind = "team"
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("unsupported storage owner kind must be rejected")
	}
}

// Reconstructing V22/V23 also rewinds the later dependent Team guard. Otherwise
// MySQL rejects dropping its referenced column and PostgreSQL can silently drop
// it while the V46 ledger still claims it exists. All migrations then run in
// release order and restore the current schema after each historical fixture.
func rewindTeamStorageOwnerGuard(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &entity.StorageObject{}
	if !db.Migrator().HasConstraint(model, "ck_storage_objects_team_creator") {
		t.Fatal("current Team guard missing before historical reconstruction")
	}
	if err := db.Migrator().DropConstraint(model, "ck_storage_objects_team_creator"); err != nil {
		t.Fatal(err)
	}
	result := db.Table("schema_migrations").Where("version = ?", 46).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("rewind dependent V46 ledger: affected=%d err=%v", result.RowsAffected, result.Error)
	}
}

func renameStorageOwnerTestIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &storageOwnerV23Integration{}
	if !db.Migrator().HasIndex(model, "idx_storage_objects_scope") {
		t.Fatal("storage owner scope index is missing before upgrade reconstruction")
	}
	// GORM's PostgreSQL RenameIndex emits CURRENT_SCHEMA() as an identifier,
	// which PostgreSQL 18 rejects. This static test-only DDL reconstructs V22;
	// the production migration remains portable GORM Migrator operations.
	statement := map[string]string{
		"postgres": "ALTER INDEX idx_storage_objects_scope RENAME TO idx_storage_objects_scope_v23_reset",
		"mysql":    "ALTER TABLE storage_objects RENAME INDEX idx_storage_objects_scope TO idx_storage_objects_scope_v23_reset",
	}[db.Name()]
	if statement == "" {
		t.Fatalf("unsupported test driver %s", db.Name())
	}
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
}

func dropStorageOwnerTestIndex(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	model := &storageOwnerV23Integration{}
	if !db.Migrator().HasIndex(model, name) {
		return
	}
	// GORM's PostgreSQL DropIndex emits CURRENT_SCHEMA() as an identifier,
	// which PostgreSQL 18 rejects. Static test-only DDL keeps the production
	// migration GORM-only while allowing exact interrupted-DDL reconstruction.
	statements := map[string]map[string]string{
		"postgres": {
			"idx_storage_objects_scope":           "DROP INDEX idx_storage_objects_scope",
			"idx_storage_objects_scope_v23_reset": "DROP INDEX idx_storage_objects_scope_v23_reset",
		},
		"mysql": {
			"idx_storage_objects_scope":           "DROP INDEX idx_storage_objects_scope ON storage_objects",
			"idx_storage_objects_scope_v23_reset": "DROP INDEX idx_storage_objects_scope_v23_reset ON storage_objects",
		},
	}
	statement := statements[db.Name()][name]
	if statement == "" {
		t.Fatalf("unsupported test index %q for %s", name, db.Name())
	}
	if err := db.Exec(statement).Error; err != nil {
		t.Fatal(err)
	}
}

func assertStorageOwnerSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &storageOwnerV23Integration{}
	if !db.Migrator().HasColumn(model, "OwnerKind") {
		t.Fatal("storage owner migration did not restore owner_kind")
	}
	if !db.Migrator().HasConstraint(model, "ck_storage_objects_owner_kind") {
		t.Fatal("storage owner migration did not restore owner-kind constraint")
	}
	if !db.Migrator().HasIndex(&entity.StorageObject{}, "idx_storage_objects_owner") {
		t.Fatal("storage owner migration removed the released owner-id index")
	}
	columns := storageOwnerIndexColumns(t, db)
	if !slices.Equal(columns, []string{"owner_kind", "owner_id"}) {
		t.Fatalf("storage owner index columns = %v", columns)
	}
}

func storageOwnerIndexColumns(t *testing.T, db *gorm.DB) []string {
	t.Helper()
	var columns []string
	// GORM's PostgreSQL GetIndexes currently reverses composite column order.
	// Ordered, static catalogue reads verify the physical index produced by the
	// otherwise GORM-only migration on each supported database.
	if db.Name() == "postgres" {
		const query = `
SELECT attribute.attname
FROM pg_class AS table_relation
JOIN pg_namespace AS namespace ON namespace.oid = table_relation.relnamespace
JOIN pg_index AS index_definition ON index_definition.indrelid = table_relation.oid
JOIN pg_class AS index_relation ON index_relation.oid = index_definition.indexrelid
JOIN LATERAL unnest(index_definition.indkey) WITH ORDINALITY AS key(attnum, ordinal) ON TRUE
JOIN pg_attribute AS attribute ON attribute.attrelid = table_relation.oid AND attribute.attnum = key.attnum
WHERE namespace.nspname = CURRENT_SCHEMA()
  AND table_relation.relname = 'storage_objects'
  AND index_relation.relname = 'idx_storage_objects_scope'
ORDER BY key.ordinal`
		if err := db.Raw(query).Scan(&columns).Error; err != nil {
			t.Fatal(err)
		}
		return columns
	}
	if db.Name() == "mysql" {
		const query = `
SELECT column_name
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'storage_objects'
  AND index_name = 'idx_storage_objects_scope'
ORDER BY seq_in_index`
		if err := db.Raw(query).Scan(&columns).Error; err != nil {
			t.Fatal(err)
		}
		return columns
	}
	t.Fatalf("unsupported test driver %s", db.Name())
	return nil
}
