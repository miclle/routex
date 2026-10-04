package handler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// These private fixture schemas freeze the exact historical guard and additive
// V46 fields. They intentionally contain no evolving business associations.
type teamAttachmentV45Fixture struct {
	OwnerKind string `gorm:"size:16;not null;default:user;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project')"`
}

func (teamAttachmentV45Fixture) TableName() string { return "storage_objects" }

type teamAttachmentV46Fixture struct {
	OwnerKind           string     `gorm:"size:16;not null;default:user;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project','team')"`
	CreatorUserID       *string    `gorm:"size:30;check:ck_storage_objects_team_creator,(owner_kind = 'team' AND creator_user_id IS NOT NULL AND CHAR_LENGTH(creator_user_id) > 0 AND creator_membership_id IS NOT NULL AND CHAR_LENGTH(creator_membership_id) > 0 AND expires_at IS NOT NULL) OR (owner_kind IN ('user','project') AND creator_user_id IS NULL AND creator_membership_id IS NULL AND expires_at IS NULL)"`
	CreatorMembershipID *string    `gorm:"size:30"`
	ExpiresAt           *time.Time `gorm:"precision:6"`
}

func (teamAttachmentV46Fixture) TableName() string { return "storage_objects" }

func testTeamAttachmentMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	model := &teamAttachmentV46Fixture{}
	assertTeamAttachmentSchema(t, db)
	now := time.Now().UTC()
	revision := entity.StorageRevision{ID: "str_team_media_upgrade", Endpoint: "https://storage.example.invalid", Region: "test-region", Bucket: "test-bucket", SecretGeneration: "media-upgrade", AuthCiphertext: "opaque-fixture", CreatedBy: "usr_media_historical", CreatedAt: now}
	if err := db.Create(&revision).Error; err != nil {
		t.Fatal(err)
	}
	ids := []string{"obj_tmedia_upgrade_user", "obj_tmedia_upgrade_project", "obj_tmedia_upgrade_probe"}
	for i, id := range ids {
		kind, owner, purpose := entity.StorageOwnerUser, "usr_media_historical", "attachment"
		if i == 1 {
			kind, owner = entity.StorageOwnerProject, "prj_media_historical"
		}
		if i == 2 {
			purpose = "probe"
		}
		row := entity.StorageObject{ID: id, OwnerKind: kind, OwnerID: owner, RevisionID: revision.ID, Purpose: purpose, State: "ready", Name: "Historical metadata", MIME: "application/pdf", Size: 17, SHA256: "frozen-sha", VersionID: "frozen-version", UploadConfirmed: true, NextCleanupAt: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Compare persisted baselines, including each driver's timestamp precision.
	var before []entity.StorageObject
	if err := db.Where("id IN ?", ids).Order("id").Find(&before).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ck_storage_objects_team_creator", "ck_storage_objects_owner_kind"} {
		if err := db.Migrator().DropConstraint(model, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, field := range []string{"CreatorUserID", "CreatorMembershipID", "ExpiresAt"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().CreateConstraint(&teamAttachmentV45Fixture{}, "ck_storage_objects_owner_kind"); err != nil {
		t.Fatal(err)
	}
	removeTeamAttachmentLedger(t, db)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertTeamAttachmentSchema(t, db)
	assertTeamAttachmentHistoricalRows(t, db, ids, before)

	// Reconstruct interrupted DDL after exactly one nullable column committed;
	// released scope indexes and historical rows stay in place.
	if err := db.Migrator().DropConstraint(model, "ck_storage_objects_team_creator"); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"CreatorUserID", "ExpiresAt"} {
		if err := db.Migrator().DropColumn(model, field); err != nil {
			t.Fatal(err)
		}
	}
	if !db.Migrator().HasColumn(model, "CreatorMembershipID") {
		t.Fatal("partial fixture did not preserve its first committed column")
	}
	removeTeamAttachmentLedger(t, db)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertTeamAttachmentSchema(t, db)
	assertTeamAttachmentHistoricalRows(t, db, ids, before)

	// Concurrent retries see the same complete schema and preserve recorded data.
	removeTeamAttachmentLedger(t, db)
	var group sync.WaitGroup
	results := make(chan error, 3)
	for range 3 {
		group.Go(func() { results <- database.Migrate(ctx, db) })
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertTeamAttachmentSchema(t, db)
	assertTeamAttachmentHistoricalRows(t, db, ids, before)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	assertTeamAttachmentHistoricalRows(t, db, ids, before)

	deadline := now.Add(time.Hour)
	creator, member := "usr_deleted_creator", "tmm_deleted_creator"
	valid := entity.StorageObject{ID: "obj_tmedia_orphan", OwnerKind: "team", OwnerID: "tem_deleted_owner", CreatorUserID: &creator, CreatorMembershipID: &member, ExpiresAt: &deadline, RevisionID: revision.ID, Purpose: "attachment", State: "ready", Name: "Historical Team object", MIME: "application/pdf", SHA256: "opaque", VersionID: "version", NextCleanupAt: deadline, CreatedAt: now}
	// None of these principals exists: history and cleanup cannot depend on live FKs.
	if err := db.Create(&valid).Error; err != nil {
		t.Fatalf("historical Team proof depends on a live owner/creator/membership: %v", err)
	}
	var persisted entity.StorageObject
	if err := db.Take(&persisted, "id = ?", valid.ID).Error; err != nil {
		t.Fatal(err)
	}
	invalids := []struct {
		name string
		edit func(*entity.StorageObject)
	}{
		{"creator-null", func(row *entity.StorageObject) { row.CreatorUserID = nil }},
		{"creator-empty", func(row *entity.StorageObject) { row.CreatorUserID = stringPointer("") }},
		{"membership-null", func(row *entity.StorageObject) { row.CreatorMembershipID = nil }},
		{"membership-empty", func(row *entity.StorageObject) { row.CreatorMembershipID = stringPointer("") }},
		{"expiry-null", func(row *entity.StorageObject) { row.ExpiresAt = nil }},
		{"creator-too-long", func(row *entity.StorageObject) { row.CreatorUserID = stringPointer(strings.Repeat("x", 31)) }},
		{"membership-too-long", func(row *entity.StorageObject) { row.CreatorMembershipID = stringPointer(strings.Repeat("x", 31)) }},
		{"unknown-owner", func(row *entity.StorageObject) { row.OwnerKind = "other" }},
		{"user-with-team-proof", func(row *entity.StorageObject) { row.OwnerKind = "user" }},
		{"project-with-team-proof", func(row *entity.StorageObject) { row.OwnerKind = "project" }},
	}
	for i, test := range invalids {
		row := valid
		row.ID = fmt.Sprintf("obj_tmedia_invalid_%d", i)
		test.edit(&row)
		if err := db.Create(&row).Error; err == nil {
			t.Fatalf("V46 accepted invalid proof %s", test.name)
		}
	}
	// A completed but unledgered step preserves exact immutable Team timestamps.
	removeTeamAttachmentLedger(t, db)
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var repeated entity.StorageObject
	if err := db.Take(&repeated, "id = ?", valid.ID).Error; err != nil || !reflect.DeepEqual(persisted, repeated) {
		t.Fatalf("repeat V46 changed Team receipt metadata: before=%+v after=%+v err=%v", persisted, repeated, err)
	}
}

func removeTeamAttachmentLedger(t *testing.T, db *gorm.DB) {
	t.Helper()
	result := db.Table("schema_migrations").Where("version = ?", 46).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("remove migration46 ledger: rows=%d err=%v", result.RowsAffected, result.Error)
	}
}
func assertTeamAttachmentSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	model := &teamAttachmentV46Fixture{}
	for _, field := range []string{"CreatorUserID", "CreatorMembershipID", "ExpiresAt"} {
		if !db.Migrator().HasColumn(model, field) {
			t.Fatal("V46 field missing", field)
		}
	}
	for _, name := range []string{"ck_storage_objects_owner_kind", "ck_storage_objects_team_creator"} {
		if !db.Migrator().HasConstraint(model, name) {
			t.Fatal("V46 guard missing", name)
		}
	}
	for _, name := range []string{"idx_storage_objects_owner", "idx_storage_objects_scope", "idx_storage_objects_cleanup", "idx_storage_objects_state"} {
		if !db.Migrator().HasIndex(&entity.StorageObject{}, name) {
			t.Fatal("V46 removed historical index", name)
		}
	}
}
func assertTeamAttachmentHistoricalRows(t *testing.T, db *gorm.DB, ids []string, before []entity.StorageObject) {
	t.Helper()
	var after []entity.StorageObject
	if err := db.Where("id IN ?", ids).Order("id").Find(&after).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("V46 changed historical user/Project/probe rows: before=%+v after=%+v", before, after)
	}
}
