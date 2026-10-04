package handler

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

type memberKeyRevisionV52Fixture struct {
	ID                string `gorm:"primaryKey;size:30"`
	LifecycleRevision string `gorm:"size:30;not null;default:''"`
}

func (memberKeyRevisionV52Fixture) TableName() string { return "api_keys" }

// The shared harness owns all real database writes. Reconstruct V51 and a
// partially committed V52 without editing a released migration definition.
func testMemberKeyMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	column := &memberKeyRevisionV52Fixture{}
	assertSeed := func() {
		t.Helper()
		if !db.Migrator().HasColumn(column, "LifecycleRevision") {
			t.Fatal("fresh V52 omitted lifecycle revision")
		}
		var rows []entity.RolePermission
		if err := db.Where("permission = ?", "members.keys.disable").Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].RoleID != "rol_admin" {
			t.Fatal("Key disabling authority was not independently seeded to administrator only", rows, err)
		}
	}
	assertSeed()
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	userID, err := id.NewPrefixed("usr")
	if err != nil {
		t.Fatal(err)
	}
	user := entity.User{ID: userID, Email: "member-key-upgrade@example.invalid", Name: "Historical member", PasswordHash: "historical-hash", Role: entity.RoleMember, CreatedAt: stamp, UpdatedAt: stamp}
	if err := db.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	keys := []entity.APIKey{}
	for _, status := range []string{entity.KeyPending, entity.KeyActive, entity.KeyDisabled, entity.KeyRevoked} {
		keyID, e := id.NewPrefixed("key")
		if e != nil {
			t.Fatal(e)
		}
		key := entity.APIKey{ID: keyID, UserID: userID, Name: "Historical " + status, Prefix: "rtx_test_only", TokenHash: secret.SHA256Hex(keyID), Status: status, CreatedAt: stamp, UpdatedAt: stamp}
		if err := db.Create(&key).Error; err != nil {
			t.Fatal(err)
		}
		keys = append(keys, key)
	}
	read := func() []entity.APIKey {
		t.Helper()
		var rows []entity.APIKey
		if err := db.Where("user_id = ?", userID).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	baseline := read()
	removeLedger := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 52).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("cannot reconstruct V52", result.RowsAffected, result.Error)
		}
	}
	runConcurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Go(func() { results <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("concurrent V52 failed", err)
			}
		}
	}
	assertHistory := func() {
		t.Helper()
		current := read()
		if len(current) != len(baseline) {
			t.Fatal("revision backfill lost retained Key")
		}
		seen := map[string]bool{}
		for index, key := range current {
			revision := key.LifecycleRevision
			if len(revision) != 30 || !strings.HasPrefix(revision, "kvr_") || seen[revision] {
				t.Fatal("missing or reused backfill generation")
			}
			seen[revision] = true
			key.LifecycleRevision = ""
			before := baseline[index]
			before.LifecycleRevision = ""
			if !reflect.DeepEqual(key, before) {
				t.Fatal("backfill changed credential, metadata, owner or timestamps")
			}
		}
	}
	removeLedger()
	if err := db.Migrator().DropColumn(column, "LifecycleRevision"); err != nil {
		t.Fatal(err)
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "members.keys.disable").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	runConcurrent()
	assertSeed()
	assertHistory()
	first := read()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(), first) {
		t.Fatal("repeat startup rewrote persistent review identities")
	}
	// Partial MySQL DDL/backfill reconciliation retains already assigned values.
	removeLedger()
	// Use the frozen column-only fixture so GORM does not update historical
	// timestamps while reconstructing a partially committed backfill.
	if err := db.Model(column).Where("id = ?", first[0].ID).Update("lifecycle_revision", "").Error; err != nil {
		t.Fatal(err)
	}
	partial := append([]entity.APIKey(nil), first...)
	partial[0].LifecycleRevision = ""
	if !reflect.DeepEqual(read(), partial) {
		t.Fatal("partial V52 reconstruction changed historical fields before migration")
	}
	if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "members.keys.disable").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	runConcurrent()
	assertSeed()
	assertHistory()
	after := read()
	if after[0].LifecycleRevision == "" {
		t.Fatal("partial backfill was acknowledged before repair")
	}
	for index := 1; index < len(after); index++ {
		if after[index].LifecycleRevision != first[index].LifecycleRevision {
			t.Fatal("partial repair changed an already assigned revision")
		}
	}
	if err := db.Model(column).Where("id = ?", first[0].ID).Update("lifecycle_revision", nil).Error; err == nil {
		t.Fatal("revision column accepted null")
	}
	if err := db.Model(column).Where("id = ?", first[0].ID).Update("lifecycle_revision", strings.Repeat("x", 31)).Error; err == nil {
		t.Fatal("revision column accepted unbounded identity")
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(read(), after) {
		t.Fatal("repeat partial repair changed current identities")
	}
	for _, key := range keys {
		if err := db.Delete(&entity.APIKey{}, "id = ?", key.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Delete(&user).Error; err != nil {
		t.Fatal(err)
	}
}

func TestMemberKeyMigrationFixtureIsHarnessOwned(t *testing.T) {
	if reflect.ValueOf(testMemberKeyMigration).IsNil() {
		t.Fatal("missing shared migration fixture")
	}
}
