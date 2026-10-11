package handler

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Synthetic, exclusively owned migration fixtures only: this is not a supported
// production downgrade. Never erase a command, including an unfinished claim.
// expected binds every receipt byte captured before this peel. MySQL DDL is not
// transactional; any failure stops this fixture instead of claiming restoration.
func runtimePublicationPeelV105(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) != 104 && len(expected) != 105 {
		return errors.New("unsupported publication fixture boundary")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid publication fixture receipts")
		}
	}
	read := func() ([]personalKeyBehaviorMigrationEntry, error) {
		var rows []personalKeyBehaviorMigrationEntry
		err := db.Table("schema_migrations").Order("version").Find(&rows).Error
		return rows, err
	}
	before, err := read()
	if err != nil || !reflect.DeepEqual(before, expected) {
		return errors.New("publication fixture receipt mismatch")
	}
	model := &entity.RuntimePublicationCommand{}
	if len(before) == 104 {
		if db.Migrator().HasTable(model) {
			return errors.New("V104 fixture retained publication table")
		}
		return nil
	}
	var count int64
	if !db.Migrator().HasTable(model) || db.Model(model).Count(&count).Error != nil || count != 0 {
		return errors.New("retained publication commands prevent historical peel")
	}
	// Reject bounded startup before any DDL; all original receipts must survive.
	if database.MigrateThrough(db.Statement.Context, db, 104) == nil {
		return errors.New("V104 admitted V105 fixture")
	}
	checked, err := read()
	if err != nil || !reflect.DeepEqual(before, checked) {
		return errors.New("publication fixture changed before DDL")
	}
	if db.Model(model).Count(&count).Error != nil || count != 0 {
		return errors.New("publication fixture became nonpristine")
	}
	if db.Migrator().DropTable(model) != nil {
		return errors.New("remove empty publication fixture table")
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 105, before[104].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		return errors.New("remove exact publication fixture receipt")
	}
	after, err := read()
	if err != nil || !reflect.DeepEqual(before[:104], after) || db.Migrator().HasTable(model) {
		return errors.New("publication peel changed V104 receipts")
	}
	return nil
}

func legacyRuntimePublicationBeforeV104(t *testing.T, db *gorm.DB) {
	t.Helper()
	legacyTelegramBeforeV106(t, db)
	if err := runtimePublicationPeelV105(db, personalKeyBehaviorLedger(t, db)); err != nil {
		t.Fatal(err)
	}
}

// Called by the existing real-driver V105 migration scenario, without additional
// t.Run names. Transactional negative controls never reach destructive DDL.
func runtimePublicationHistoricalGuardControlsV105(t *testing.T, db *gorm.DB, original []personalKeyBehaviorMigrationEntry) {
	t.Helper()
	rollback := errors.New("rollback publication historical refusal")
	assertUnchanged := func(want []personalKeyBehaviorMigrationEntry) {
		t.Helper()
		if !reflect.DeepEqual(want, personalKeyBehaviorLedger(t, db)) || !db.Migrator().HasTable(&entity.RuntimePublicationCommand{}) {
			t.Fatal("refusal changed original publication fixture")
		}
	}
	birth := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	retained := entity.RuntimePublicationCommand{RequestID: "5f739b12-12ce-4a20-8da7-04bc8885dfb8", ActorID: "usr_01m36yee4gkbns18pfcqqc75a3", ActorCreatedAt: birth, Reason: "Retained migration-only command blocks historical peel", ClaimInstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", ClaimInstanceStartedAt: birth, ClaimedAt: birth, Outcome: "claimed"}
	err := db.Transaction(func(tx *gorm.DB) error {
		if tx.Create(&retained).Error != nil {
			return errors.New("store retained command control")
		}
		saved := runtimePublicationStoredV105(t, tx, retained.RequestID)
		if runtimePublicationPeelV105(tx, original) == nil {
			return errors.New("retained command admitted historical peel")
		}
		var got entity.RuntimePublicationCommand
		var count int64
		if tx.Session(&gorm.Session{QueryFields: true}).Take(&got, "request_id = ?", retained.RequestID).Error != nil || !runtimePublicationRowsEqualV105(saved, got) || tx.Model(&entity.RuntimePublicationCommand{}).Count(&count).Error != nil || count != 1 || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, tx)) {
			return errors.New("refusal changed retained command or receipt")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("retained command refusal failed")
	}
	assertUnchanged(original)
	for _, kind := range []string{"changed_receipt", "blank_receipt", "ledger_gap", "future_tail", "already104_with_table"} {
		err = db.Transaction(func(tx *gorm.DB) error {
			var q *gorm.DB
			switch kind {
			case "changed_receipt":
				q = tx.Table("schema_migrations").Where("version = ?", 105).Update("applied_at", "fixture-receipt-mismatch")
			case "blank_receipt":
				q = tx.Table("schema_migrations").Where("version = ?", 105).Update("applied_at", "")
			case "already104_with_table":
				q = tx.Table("schema_migrations").Where("version = ?", 105).Delete(&struct{}{})
			case "ledger_gap":
				q = tx.Table("schema_migrations").Where("version = ?", 104).Delete(&struct{}{})
			case "future_tail":
				q = tx.Table("schema_migrations").Where("version = ?", 105).Update("version", 106)
			}
			if q.Error != nil || q.RowsAffected != 1 {
				return errors.New("construct exact ledger refusal control")
			}
			changed := personalKeyBehaviorLedger(t, tx)
			if kind != "changed_receipt" && runtimePublicationPeelV105(tx, changed) == nil {
				return errors.New("noncontiguous ledger admitted historical peel")
			}
			if runtimePublicationPeelV105(tx, original) == nil || !reflect.DeepEqual(changed, personalKeyBehaviorLedger(t, tx)) || !tx.Migrator().HasTable(&entity.RuntimePublicationCommand{}) {
				return errors.New("mismatched ledger admitted historical peel")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("ledger refusal failed", kind)
		}
		assertUnchanged(original)
	}
	// Exercise two genuine empty peels and the already-bounded path. Only V105's
	// new receipt may be regenerated; all 104 predecessor timestamps remain exact.
	current := original
	for range 2 {
		if runtimePublicationPeelV105(db, current) != nil || db.Migrator().HasTable(&entity.RuntimePublicationCommand{}) || !reflect.DeepEqual(original[:104], personalKeyBehaviorLedger(t, db)) {
			t.Fatal("pristine V105 peel changed original V104 receipts")
		}
		legacyRuntimePublicationBeforeV104(t, db)
		if database.MigrateThrough(db.Statement.Context, db, 104) != nil || !reflect.DeepEqual(original[:104], personalKeyBehaviorLedger(t, db)) {
			t.Fatal("bounded V104 repeat changed original receipts")
		}
		if database.MigrateThrough(db.Statement.Context, db, 105) != nil {
			t.Fatal("restore complete V105 after historical control")
		}
		current = personalKeyBehaviorLedger(t, db)
		var count int64
		if len(current) != 105 || current[104].Version != 105 || !reflect.DeepEqual(original[:104], current[:104]) || db.Model(&entity.RuntimePublicationCommand{}).Count(&count).Error != nil || count != 0 {
			t.Fatal("V105 restoration changed predecessors or fabricated commands")
		}
	}
}
