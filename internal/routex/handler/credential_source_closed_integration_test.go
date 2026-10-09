package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

func testCredentialSourceClosedMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 89 || before[88].Version != 89 {
		t.Fatal("exact V89 full ledger required")
	}
	var original []entity.CredentialSourceProcess
	if err := db.Order("process_id").Find(&original).Error; err != nil {
		t.Fatal(err)
	}
	// Reconstruct only the new additive field; restore retained known proofs from
	// this fixture's exact captured facts afterward, never through migration.
	if err := db.Migrator().DropColumn(&entity.CredentialSourceProcess{}, "ClosedAt"); err != nil {
		t.Fatal(err)
	}
	result := db.Table("schema_migrations").Where("version = ?", 89).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("remove only V89", result.Error)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 89 || after[88].Version != 89 || !reflect.DeepEqual(before[:88], after[:88]) {
		t.Fatal("released 1..88 ledger changed")
	}
	var saved []entity.CredentialSourceProcess
	if err := db.Order("process_id").Find(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if len(saved) != len(original) {
		t.Fatal("retained process census changed")
	}
	for i := range original {
		if saved[i].ClosedAt != nil {
			t.Fatal("migration fabricated shutdown proof")
		}
		known := original[i].ClosedAt
		projection := original[i]
		projection.ClosedAt = nil
		if !reflect.DeepEqual(projection, saved[i]) {
			t.Fatal("retained registration changed")
		}
		if known != nil {
			if err := db.Model(&entity.CredentialSourceProcess{}).Where("process_id = ?", original[i].ProcessID).UpdateColumn("closed_at", known).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var retained []entity.CredentialSourceProcess
	if err := db.Order("process_id").Find(&retained).Error; err != nil || !reflect.DeepEqual(original, retained) {
		t.Fatal("fixture did not restore exact original known proof", err)
	}
}

func testCredentialSourceClosedLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	owner, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	metadata := service.SystemInstanceMetadata{Name: "Graceful source owner", Hostname: "graceful.invalid", Version: "fixture", GoVersion: "test", OS: "test", Arch: "test"}
	if err := owner.StartSystemInstance(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	originalID := owner.CurrentSystemInstanceID()
	var birth entity.CredentialSourceProcess
	if err := db.Where("process_id = ?", originalID).Take(&birth).Error; err != nil || birth.ClosedAt != nil {
		t.Fatal("registration must begin unproven", err)
	}
	if err := owner.StopSystemInstance(ctx); err != nil {
		t.Fatal(err)
	}
	var closed entity.CredentialSourceProcess
	var instance entity.SystemInstance
	if err := db.Where("process_id = ?", originalID).Take(&closed).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("id = ?", originalID).Take(&instance).Error; err != nil {
		t.Fatal(err)
	}
	if closed.ClosedAt == nil || instance.StoppedAt == nil || !closed.ClosedAt.Equal(*instance.StoppedAt) {
		t.Fatal("atomic generation closure missing")
	}
	projection := closed
	projection.ClosedAt = nil
	if !reflect.DeepEqual(projection, birth) {
		t.Fatal("shutdown changed original registration")
	}
	if err := owner.StopSystemInstance(ctx); err != nil {
		t.Fatal("repeat stop", err)
	}
	if err := owner.StartSystemInstance(ctx, metadata); err == nil {
		t.Fatal("closed source registry reused for another generation")
	}
	restart, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if err := restart.StartSystemInstance(ctx, metadata); err != nil {
		t.Fatal(err)
	}
	if restart.CurrentSystemInstanceID() == originalID {
		t.Fatal("restart reused process generation")
	}
	if err := restart.StopSystemInstance(ctx); err != nil {
		t.Fatal(err)
	}
	var retained entity.CredentialSourceProcess
	if err := db.Where("process_id = ?", originalID).Take(&retained).Error; err != nil || !reflect.DeepEqual(closed, retained) {
		t.Fatal("restart erased original closure", err)
	}
	// Historical stopped/expired rows never receive synthetic registration/closure.
	unknownID, err := id.NewPrefixed("ins")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	unknown := entity.CredentialSourceProcess{ProcessID: unknownID, Generation: birth.Generation, Birth: now, RegisteredAt: now}
	if err := db.Create(&unknown).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	retained = entity.CredentialSourceProcess{}
	if err := db.Where("process_id = ?", unknownID).Take(&retained).Error; err != nil || retained.ClosedAt != nil {
		t.Fatal("unproven history backfilled", err)
	}
	if err := db.Delete(&unknown).Error; err != nil {
		t.Fatal(err)
	}
}
