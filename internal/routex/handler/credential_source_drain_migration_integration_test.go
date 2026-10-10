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
	"gorm.io/gorm"
)

// This real-driver source fixture reconstructs only V88. It does not backfill
// process acknowledgments, infer remote ownership, or dispatch any SDK request.
func testCredentialSourceDrainMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 95 || before[94].Version != 95 || before[93].Version != 94 || before[92].Version != 93 || before[91].Version != 92 || before[90].Version != 91 || before[89].Version != 90 || before[88].Version != 89 || before[87].Version != 88 {
		t.Fatal("exact V88 ledger required")
	}
	for i, row := range before {
		if row.Version != i+1 {
			t.Fatal("historical ledger order")
		}
	}
	var processes []entity.CredentialSourceProcess
	var denials []entity.CredentialSourceDenial
	var uses []entity.CredentialSourceUse
	var commands []entity.CredentialPublishedCleanup
	for _, target := range []any{&processes, &denials, &uses, &commands} {
		if err := db.Find(target).Error; err != nil {
			t.Fatal(err)
		}
	}
	originalInstances := []entity.SystemInstance{}
	if err := db.Order("id").Find(&originalInstances).Error; err != nil {
		t.Fatal(err)
	}
	models := []any{&entity.CredentialPublishedCleanup{}, &entity.CredentialSourceUse{}, &entity.CredentialSourceDenial{}, &entity.CredentialSourceProcess{}}
	if err := db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	remove := func() {
		t.Helper()
		result := db.Table("schema_migrations").Where("version = ?", 88).Delete(&struct{}{})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("remove only V88", result.Error)
		}
	}
	remove()
	repeat := func() {
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
				t.Fatal(err)
			}
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		// Historical V88 table reconstruction retains the V89 ledger, so restore
		// its additive nullable shape explicitly without inferring any proof.
		if !db.Migrator().HasColumn(&entity.CredentialSourceProcess{}, "ClosedAt") {
			if err := db.Migrator().AddColumn(&entity.CredentialSourceProcess{}, "ClosedAt"); err != nil {
				t.Fatal(err)
			}
		}
		after := personalKeyBehaviorLedger(t, db)
		if len(after) != 95 || after[94].Version != 95 || after[93].Version != 94 || after[92].Version != 93 || after[91].Version != 92 || after[90].Version != 91 || after[89].Version != 90 || after[88].Version != 89 || after[87].Version != 88 || !reflect.DeepEqual(before[:87], after[:87]) {
			t.Fatal("released 1..87 changed")
		}
	}
	repeat()
	for _, model := range models {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("migration fabricated historical proof", err)
		}
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	// Empty interrupted DDL can repair a missing declared field. Nonempty proof
	// history cannot acquire invented fields, identities or successful joins.
	if err := db.Migrator().DropColumn(&entity.CredentialSourceProcess{}, "RegisteredAt"); err != nil {
		t.Fatal(err)
	}
	remove()
	repeat()
	process := entity.CredentialSourceProcess{ProcessID: "ins_00000000000000000000000000", Generation: strings.Repeat("a", 64), Birth: at, RegisteredAt: at}
	if err := db.Create(&process).Error; err != nil {
		t.Fatal(err)
	}
	var saved entity.CredentialSourceProcess
	if err := db.Select("process_id", "generation", "birth", "registered_at").Take(&saved, "process_id = ?", process.ProcessID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropColumn(&entity.CredentialSourceProcess{}, "RegisteredAt"); err != nil {
		t.Fatal(err)
	}
	remove()
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("nonempty missing proof field repaired fictionally")
	}
	// Repair only the controlled test table using its captured complete row.
	// AddColumn NOT NULL over nonempty rows would itself fabricate or fail.
	if err := db.Migrator().DropTable(&entity.CredentialSourceProcess{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateTable(&entity.CredentialSourceProcess{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&saved).Error; err != nil {
		t.Fatal(err)
	}
	repeat()
	var current entity.CredentialSourceProcess
	if err := db.Select("process_id", "generation", "birth", "registered_at").Take(&current, "process_id = ?", process.ProcessID).Error; err != nil || !reflect.DeepEqual(saved, current) {
		t.Fatal("partial DDL changed proof history", err)
	}
	if err := db.Delete(&current).Error; err != nil {
		t.Fatal(err)
	}
	denial := entity.CredentialSourceDenial{PhysicalObject: strings.Repeat("b", 64), CreationRequestID: "10000000-1111-4111-8111-000000000001", RequestID: "10000000-1111-4111-8111-000000000002", CreatedAt: at}
	if err := db.Create(&denial).Error; err != nil {
		t.Fatal(err)
	}
	duplicate := denial
	duplicate.PhysicalObject = strings.Repeat("c", 64)
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("physical ownership creation/command uniqueness absent")
	}
	if err := db.Delete(&denial).Error; err != nil {
		t.Fatal(err)
	}
	invalid := entity.CredentialPublishedCleanup{PhysicalObject: strings.Repeat("d", 64), KnownNoEffect: true, CreationRequestID: denial.CreationRequestID, RequestID: denial.RequestID, ActorID: "usr_guard", ActorBirth: at, IntegrationID: "vli_00000000000000000000000000", IntegrationBirth: at, RevisionID: "vlr_00000000000000000000000000", OperationProof: strings.Repeat("e", 64), ReviewedETag: strings.Repeat("f", 64) + "." + strings.Repeat("e", 64), Reason: "Constraint evidence", State: "pending", OwnershipJSON: "{}", CleanupJSON: "{}", StartedAt: at, Deadline: at.Add(time.Second)}
	if err := db.Create(&invalid).Error; err == nil {
		t.Fatal("known-no-effect accepted for pending/uncertain command")
	}
	// Restore the captured proof rows byte-for-byte as test reconstruction, rather
	// than asking the migration to invent them from live registrations/history.
	for _, rows := range []any{processes, denials, uses, commands} {
		if reflect.ValueOf(rows).Len() != 0 {
			if err := db.Create(rows).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var afterInstances []entity.SystemInstance
	if err := db.Order("id").Find(&afterInstances).Error; err != nil || !reflect.DeepEqual(originalInstances, afterInstances) {
		t.Fatal("migration changed retained instances", err)
	}
}
