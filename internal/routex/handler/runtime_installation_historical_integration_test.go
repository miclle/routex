package handler

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// installationPristineV100 admits only the exclusively owned integration
// database before a historical fixture. It never reinterprets a durable receipt.
func installationPristineV100(db *gorm.DB) error {
	var rows []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&rows).Error; err != nil || len(rows) != 100 {
		return errors.New("historical fixture requires exact current V100 ledger")
	}
	for i, row := range rows {
		if row.Version != i+1 {
			return errors.New("noncontiguous current V100 ledger")
		}
	}
	if !db.Migrator().HasTable("runtime_installation_observations") {
		return errors.New("current installation observation table missing")
	}
	var count int64
	if err := db.Table("runtime_installation_observations").Count(&count).Error; err != nil || count != 0 {
		return errors.New("historical fixture cannot discard installation history")
	}
	return nil
}

// This removes only the empty additive V100 table and its exact ledger row.
// All released schemas, current source records and the V1-V99 receipts remain.
// The caller must restore full-current startup before leaving its scenario.
func legacyInstallationBeforeV99(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := installationPristineV100(db); err != nil {
		t.Fatal(err)
	}
	before := personalKeyBehaviorLedger(t, db)
	if database.MigrateThrough(db.Statement.Context, db, 99) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("bounded99 admitted newer V100 ledger or changed history")
	}
	// The harness owns this quiescent test database; there is no live publisher.
	if err := installationPristineV100(db); err != nil {
		t.Fatal("fresh pristine installation proof", err)
	}
	if err := db.Migrator().DropTable("runtime_installation_observations"); err != nil {
		t.Fatal("drop only proven empty installation table", err)
	}
	removed := db.Table("schema_migrations").Where("version = ?", 100).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 || !reflect.DeepEqual(before[:99], personalKeyBehaviorLedger(t, db)) || db.Migrator().HasTable("runtime_installation_observations") {
		t.Fatal("remove only owned V100 overlay; preserve exact V99 ledger/version/time", removed.Error)
	}
}

// Reuse the existing Google migration scenario without new named tests. This
// row is a rollback-only schema guard fixture, never installed runtime evidence.
func googleInstallationOverlayControls(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := installationPristineV100(db); err != nil {
		t.Fatal("positive V100 pristine prerequisite", err)
	}
	before := personalKeyBehaviorLedger(t, db)
	rollback := errors.New("rollback dirty installation guard control")
	birth := time.Now().UTC().Truncate(time.Microsecond)
	row := entity.RuntimeInstallationObservation{ID: "rin_01m36yee4gkbns18pfcqqc75a3", InstanceID: "ins_01m36yee4gkbns18pfcqqc75a3", InstanceStartedAt: birth, SnapshotID: "cfg_01m36yee4gkbns18pfcqqc75a3", ProjectionVersion: 1, SourceDigest: strings.Repeat("a", 64), RoutesPublishedAt: birth, FirstObservedAt: birth}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if installationPristineV100(tx) == nil {
			return errors.New("dirty installation history admitted")
		}
		var count int64
		if err := tx.Table("runtime_installation_observations").Count(&count).Error; err != nil || count != 1 || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, tx)) {
			return errors.New("dirty guard changed receipt or ledger")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal("installation dirty-state guard control", err)
	}
	if err := installationPristineV100(db); err != nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("dirty guard did not retain original pristine state", err)
	}
}
