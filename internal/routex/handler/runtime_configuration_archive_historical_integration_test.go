package handler

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Literal V111 projections for test-only historical schema reconstruction.
// They never downgrade an application database or consume retained archive facts.
type runtimeArchiveHistoricalRootJobV111 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9 OR inventory_version = 10"`
	Domain           int `gorm:"not null;check:ck_secret_rotation_domain,(inventory_version = 1 AND domain >= 0 AND domain <= 5) OR (inventory_version = 2 AND domain >= 0 AND domain <= 7) OR (inventory_version = 3 AND domain >= 0 AND domain <= 8) OR (inventory_version = 4 AND domain >= 0 AND domain <= 9) OR (inventory_version = 5 AND domain >= 0 AND domain <= 10) OR (inventory_version = 6 AND domain >= 0 AND domain <= 11) OR (inventory_version = 7 AND domain >= 0 AND domain <= 11) OR (inventory_version = 8 AND domain >= 0 AND domain <= 11) OR (inventory_version = 9 AND domain >= 0 AND domain <= 12) OR (inventory_version = 10 AND domain >= 0 AND domain <= 12)"`
}

func (runtimeArchiveHistoricalRootJobV111) TableName() string { return "secret_rotation_jobs" }

type runtimeArchiveHistoricalRootProcessV111 struct {
	InventoryVersion int `gorm:"not null;default:1;check:ck_secret_process_inventory_version,inventory_version = 1 OR inventory_version = 2 OR inventory_version = 3 OR inventory_version = 4 OR inventory_version = 5 OR inventory_version = 6 OR inventory_version = 7 OR inventory_version = 8 OR inventory_version = 9 OR inventory_version = 10"`
}

func (runtimeArchiveHistoricalRootProcessV111) TableName() string {
	return "secret_process_verifications"
}

type runtimeArchiveHistoricalRootItemV111 struct {
	Domain string `gorm:"primaryKey;size:32;check:ck_secret_item_domain,(((((CHAR_LENGTH(domain) = 20 AND ASCII(SUBSTRING(domain,1,1)) = 112 AND ASCII(SUBSTRING(domain,2,1)) = 114 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 118 AND ASCII(SUBSTRING(domain,5,1)) = 105 AND ASCII(SUBSTRING(domain,6,1)) = 100 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 95 AND ASCII(SUBSTRING(domain,10,1)) = 99 AND ASCII(SUBSTRING(domain,11,1)) = 114 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 100 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 110 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 105 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 108 AND ASCII(SUBSTRING(domain,20,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 101 AND ASCII(SUBSTRING(domain,2,1)) = 103 AND ASCII(SUBSTRING(domain,3,1)) = 114 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 115 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 115) OR (CHAR_LENGTH(domain) = 13 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 109 AND ASCII(SUBSTRING(domain,3,1)) = 116 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 115 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 116 AND ASCII(SUBSTRING(domain,9,1)) = 116 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 110 AND ASCII(SUBSTRING(domain,12,1)) = 103 AND ASCII(SUBSTRING(domain,13,1)) = 115) OR (CHAR_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 115 AND ASCII(SUBSTRING(domain,2,1)) = 116 AND ASCII(SUBSTRING(domain,3,1)) = 111 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 97 AND ASCII(SUBSTRING(domain,6,1)) = 103 AND ASCII(SUBSTRING(domain,7,1)) = 101 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 114 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 118 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 115 AND ASCII(SUBSTRING(domain,14,1)) = 105 AND ASCII(SUBSTRING(domain,15,1)) = 111 AND ASCII(SUBSTRING(domain,16,1)) = 110 AND ASCII(SUBSTRING(domain,17,1)) = 115) OR (CHAR_LENGTH(domain) = 8 AND ASCII(SUBSTRING(domain,1,1)) = 117 AND ASCII(SUBSTRING(domain,2,1)) = 115 AND ASCII(SUBSTRING(domain,3,1)) = 101 AND ASCII(SUBSTRING(domain,4,1)) = 114 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 109 AND ASCII(SUBSTRING(domain,7,1)) = 102 AND ASCII(SUBSTRING(domain,8,1)) = 97)) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 119 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 105 AND ASCII(SUBSTRING(domain,10,1)) = 116 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104) OR (OCTET_LENGTH(domain) = 17 AND ASCII(SUBSTRING(domain,1,1)) = 118 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 108 AND ASCII(SUBSTRING(domain,5,1)) = 116 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 101 AND ASCII(SUBSTRING(domain,9,1)) = 97 AND ASCII(SUBSTRING(domain,10,1)) = 100 AND ASCII(SUBSTRING(domain,11,1)) = 101 AND ASCII(SUBSTRING(domain,12,1)) = 114 AND ASCII(SUBSTRING(domain,13,1)) = 95 AND ASCII(SUBSTRING(domain,14,1)) = 97 AND ASCII(SUBSTRING(domain,15,1)) = 117 AND ASCII(SUBSTRING(domain,16,1)) = 116 AND ASCII(SUBSTRING(domain,17,1)) = 104))) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 105 AND ASCII(SUBSTRING(domain,3,1)) = 100 AND ASCII(SUBSTRING(domain,4,1)) = 99 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115) OR (OCTET_LENGTH(domain) = 15 AND ASCII(SUBSTRING(domain,1,1)) = 111 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 117 AND ASCII(SUBSTRING(domain,4,1)) = 116 AND ASCII(SUBSTRING(domain,5,1)) = 104 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 112 AND ASCII(SUBSTRING(domain,8,1)) = 114 AND ASCII(SUBSTRING(domain,9,1)) = 111 AND ASCII(SUBSTRING(domain,10,1)) = 118 AND ASCII(SUBSTRING(domain,11,1)) = 105 AND ASCII(SUBSTRING(domain,12,1)) = 100 AND ASCII(SUBSTRING(domain,13,1)) = 101 AND ASCII(SUBSTRING(domain,14,1)) = 114 AND ASCII(SUBSTRING(domain,15,1)) = 115) OR (OCTET_LENGTH(domain) = 14 AND ASCII(SUBSTRING(domain,1,1)) = 108 AND ASCII(SUBSTRING(domain,2,1)) = 100 AND ASCII(SUBSTRING(domain,3,1)) = 97 AND ASCII(SUBSTRING(domain,4,1)) = 112 AND ASCII(SUBSTRING(domain,5,1)) = 95 AND ASCII(SUBSTRING(domain,6,1)) = 112 AND ASCII(SUBSTRING(domain,7,1)) = 114 AND ASCII(SUBSTRING(domain,8,1)) = 111 AND ASCII(SUBSTRING(domain,9,1)) = 118 AND ASCII(SUBSTRING(domain,10,1)) = 105 AND ASCII(SUBSTRING(domain,11,1)) = 100 AND ASCII(SUBSTRING(domain,12,1)) = 101 AND ASCII(SUBSTRING(domain,13,1)) = 114 AND ASCII(SUBSTRING(domain,14,1)) = 115)) OR (OCTET_LENGTH(domain) = 24 AND ASCII(SUBSTRING(domain,1,1)) = 110 AND ASCII(SUBSTRING(domain,2,1)) = 97 AND ASCII(SUBSTRING(domain,3,1)) = 109 AND ASCII(SUBSTRING(domain,4,1)) = 101 AND ASCII(SUBSTRING(domain,5,1)) = 100 AND ASCII(SUBSTRING(domain,6,1)) = 95 AND ASCII(SUBSTRING(domain,7,1)) = 105 AND ASCII(SUBSTRING(domain,8,1)) = 100 AND ASCII(SUBSTRING(domain,9,1)) = 101 AND ASCII(SUBSTRING(domain,10,1)) = 110 AND ASCII(SUBSTRING(domain,11,1)) = 116 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 116 AND ASCII(SUBSTRING(domain,14,1)) = 121 AND ASCII(SUBSTRING(domain,15,1)) = 95 AND ASCII(SUBSTRING(domain,16,1)) = 112 AND ASCII(SUBSTRING(domain,17,1)) = 114 AND ASCII(SUBSTRING(domain,18,1)) = 111 AND ASCII(SUBSTRING(domain,19,1)) = 118 AND ASCII(SUBSTRING(domain,20,1)) = 105 AND ASCII(SUBSTRING(domain,21,1)) = 100 AND ASCII(SUBSTRING(domain,22,1)) = 101 AND ASCII(SUBSTRING(domain,23,1)) = 114 AND ASCII(SUBSTRING(domain,24,1)) = 115) OR (OCTET_LENGTH(domain) = 21 AND ASCII(SUBSTRING(domain,1,1)) = 97 AND ASCII(SUBSTRING(domain,2,1)) = 112 AND ASCII(SUBSTRING(domain,3,1)) = 105 AND ASCII(SUBSTRING(domain,4,1)) = 95 AND ASCII(SUBSTRING(domain,5,1)) = 107 AND ASCII(SUBSTRING(domain,6,1)) = 101 AND ASCII(SUBSTRING(domain,7,1)) = 121 AND ASCII(SUBSTRING(domain,8,1)) = 95 AND ASCII(SUBSTRING(domain,9,1)) = 100 AND ASCII(SUBSTRING(domain,10,1)) = 101 AND ASCII(SUBSTRING(domain,11,1)) = 108 AND ASCII(SUBSTRING(domain,12,1)) = 105 AND ASCII(SUBSTRING(domain,13,1)) = 118 AND ASCII(SUBSTRING(domain,14,1)) = 101 AND ASCII(SUBSTRING(domain,15,1)) = 114 AND ASCII(SUBSTRING(domain,16,1)) = 121 AND ASCII(SUBSTRING(domain,17,1)) = 95 AND ASCII(SUBSTRING(domain,18,1)) = 97 AND ASCII(SUBSTRING(domain,19,1)) = 117 AND ASCII(SUBSTRING(domain,20,1)) = 116 AND ASCII(SUBSTRING(domain,21,1)) = 104)"`
}

func (runtimeArchiveHistoricalRootItemV111) TableName() string { return "secret_rotation_items" }

// Read-only successor preflight; incomplete or retained intent blocks every DDL.
func runtimeArchiveCleanupFixtureEmpty(db *gorm.DB) error {
	model := &entity.RuntimeArchiveCleanup{}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	if !db.Migrator().HasTable(model) {
		return errors.New("incomplete cleanup fixture overlay")
	}
	for _, name := range statement.Schema.DBNames {
		if !db.Migrator().HasColumn(model, name) {
			return errors.New("incomplete cleanup fixture column")
		}
	}
	for name := range statement.Schema.ParseCheckConstraints() {
		if !db.Migrator().HasConstraint(model, name) {
			return errors.New("incomplete cleanup fixture guard")
		}
	}
	for _, index := range statement.Schema.ParseIndexes() {
		if !db.Migrator().HasIndex(model, index.Name) {
			return errors.New("incomplete cleanup fixture index")
		}
	}
	var count int64
	if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained cleanup intent prevents historical peel")
	}
	return nil
}

// V116 must be complete and empty before any predecessor fixture may mutate.
func runtimeArchiveRollbackFixtureEmpty(db *gorm.DB) error {
	model := &entity.RuntimeArchiveRollback{}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(model); err != nil {
		return err
	}
	if !db.Migrator().HasTable(model) {
		return errors.New("incomplete rollback fixture overlay")
	}
	for _, name := range statement.Schema.DBNames {
		if !db.Migrator().HasColumn(model, name) {
			return errors.New("incomplete rollback fixture column")
		}
	}
	for name := range statement.Schema.ParseCheckConstraints() {
		if !db.Migrator().HasConstraint(model, name) {
			return errors.New("incomplete rollback fixture guard")
		}
	}
	for _, name := range []string{"fk_archive_rollback_archive", "fk_archive_rollback_publication"} {
		if !db.Migrator().HasConstraint(model, name) {
			return errors.New("incomplete rollback fixture reference")
		}
	}
	for _, index := range statement.Schema.ParseIndexes() {
		if !db.Migrator().HasIndex(model, index.Name) {
			return errors.New("incomplete rollback fixture index")
		}
	}
	var count int64
	if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained rollback intent prevents historical peel")
	}
	return nil
}

func runtimeArchiveRollbackPeelV116(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) != 116 {
		return errors.New("rollback peel requires exact V116 ledger")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid rollback fixture ledger")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("rollback fixture ledger mismatch")
	}
	if err := runtimeArchiveRollbackFixtureEmpty(db); err != nil {
		return err
	}
	if err := db.Migrator().DropTable(&entity.RuntimeArchiveRollback{}); err != nil {
		return err
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 116, expected[115].AppliedAt).Delete(&struct{}{})
	actual = nil
	if removed.Error != nil || removed.RowsAffected != 1 || db.Table("schema_migrations").Order("version").Find(&actual).Error != nil || !reflect.DeepEqual(actual, expected[:115]) {
		return errors.New("rollback peel changed retained prefix")
	}
	return nil
}

// Peel only complete, empty V112/V113/V114/V115/V116 overlays in an exclusively owned fixture.
// Prove every ledger, data and frozen-root guard before the first DDL write.
func runtimeArchivePeelV112(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 116 {
		return errors.New("unsupported archive fixture head")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid archive fixture receipts")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("archive fixture receipt mismatch")
	}
	if len(expected) < 116 {
		if db.Migrator().HasTable(&entity.RuntimeArchiveRollback{}) {
			return errors.New("historical ledger retained rollback overlay")
		}
	} else if err := runtimeArchiveRollbackFixtureEmpty(db); err != nil {
		return err
	}
	cleanupTable := "runtime_archive_cleanups"
	if len(expected) < 115 {
		if db.Migrator().HasTable(cleanupTable) {
			return errors.New("historical ledger retained cleanup overlay")
		}
	} else {
		if err := runtimeArchiveCleanupFixtureEmpty(db); err != nil {
			return err
		}
	}
	controllerTables := []string{"api_key_delivery_controller_acknowledgements", "api_key_delivery_controller_grants"}
	for _, table := range controllerTables {
		if len(expected) < 114 {
			if db.Migrator().HasTable(table) {
				return errors.New("historical ledger retained controller overlay")
			}
			continue
		}
		if !db.Migrator().HasTable(table) {
			return errors.New("incomplete controller fixture overlay")
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained controller facts prevent historical peel")
		}
	}
	retentionTables := []string{"runtime_archive_captures", "runtime_archive_contents", "runtime_archive_retention"}
	if len(expected) < 113 {
		for _, table := range retentionTables {
			if db.Migrator().HasTable(table) {
				return errors.New("historical ledger retained archive retention overlay")
			}
		}
	} else {
		for _, table := range retentionTables {
			if !db.Migrator().HasTable(table) {
				return errors.New("incomplete archive retention fixture overlay")
			}
			var count int64
			want := int64(0)
			if table == "runtime_archive_retention" {
				want = 1
			}
			if err := db.Table(table).Count(&count).Error; err != nil || count != want {
				return errors.New("retained archive retention facts prevent historical peel")
			}
		}
		var ledger entity.RuntimeArchiveRetention
		if err := db.Take(&ledger, 1).Error; err != nil || ledger.ID != 1 || !ledger.Initialized || ledger.RetainedCount != 0 || ledger.EncryptedBytes != 0 {
			return errors.New("nonempty or unknown archive retention prevents historical peel")
		}
	}
	tables := []string{"runtime_configuration_archive_chunks", "runtime_configuration_archives"}
	if len(expected) < 112 {
		for _, table := range tables {
			if db.Migrator().HasTable(table) {
				return errors.New("historical ledger retained archive overlay")
			}
		}
		return nil
	}
	for _, table := range tables {
		if !db.Migrator().HasTable(table) {
			return errors.New("incomplete archive fixture overlay")
		}
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained archive prevents historical peel")
		}
	}
	checks := []struct {
		model any
		name  string
	}{
		{&runtimeArchiveHistoricalRootJobV111{}, "ck_secret_inventory_version"},
		{&runtimeArchiveHistoricalRootJobV111{}, "ck_secret_rotation_domain"},
		{&runtimeArchiveHistoricalRootProcessV111{}, "ck_secret_process_inventory_version"},
		{&runtimeArchiveHistoricalRootItemV111{}, "ck_secret_item_domain"},
	}
	for _, item := range checks {
		st := &gorm.Statement{DB: db}
		if err := st.Parse(item.model); err != nil {
			return err
		}
		check, ok := st.Schema.ParseCheckConstraints()[item.name]
		if !ok || !db.Migrator().HasConstraint(item.model, item.name) {
			return errors.New("incomplete archive root fixture guard")
		}
		var count int64
		if err := db.Table(st.Schema.Table).Where("NOT (" + check.Constraint + ") OR (" + check.Constraint + ") IS NULL").Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained archive root authority prevents historical peel")
		}
	}
	// All retained source, provenance, accounting and root-authority guards have
	// passed before the first DDL mutation, including on nontransactional MySQL.
	if len(expected) == 116 {
		if err := runtimeArchiveRollbackPeelV116(db, expected); err != nil {
			return err
		}
		expected = expected[:115]
	}
	if len(expected) == 115 {
		if err := db.Migrator().DropTable(cleanupTable); err != nil {
			return err
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 115, expected[114].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			return errors.New("exact cleanup receipt removal failed")
		}
		actual = nil
		if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:114]) {
			return errors.New("cleanup peel changed retained prefix")
		}
		expected = expected[:114]
	}
	if len(expected) == 114 {
		for _, table := range controllerTables {
			if err := db.Migrator().DropTable(table); err != nil {
				return err
			}
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 114, expected[113].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			return errors.New("exact controller receipt removal failed")
		}
		actual = nil
		if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:113]) {
			return errors.New("controller peel changed retained prefix")
		}
		expected = expected[:113]
	}
	if len(expected) == 113 {
		for _, table := range retentionTables {
			if err := db.Migrator().DropTable(table); err != nil {
				return err
			}
		}
		removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 113, expected[112].AppliedAt).Delete(&struct{}{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			return errors.New("exact archive retention receipt removal failed")
		}
		expected = expected[:112]
	}
	for _, item := range checks {
		if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
			return err
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	for _, table := range tables {
		if err := db.Migrator().DropTable(table); err != nil {
			return err
		}
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 112, expected[111].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		return errors.New("exact archive receipt removal failed")
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:111]) {
		return errors.New("archive peel changed retained prefix")
	}
	return nil
}

// Exercise refusal inside the existing real-driver registry without extra names.
func runtimeArchiveHistoricalGuardControlsV112(t *testing.T, db *gorm.DB) {
	t.Helper()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 116 {
		t.Fatal("archive historical guard requires V116")
	}
	rollback := errors.New("rollback retained archive fixture control")
	for _, mutate := range []func([]personalKeyBehaviorMigrationEntry){
		func(rows []personalKeyBehaviorMigrationEntry) { rows[111].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[111].Version = 113 },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[112].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[112].Version = 114 },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[113].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[113].Version = 115 },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[114].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[114].Version = 116 },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[115].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[115].Version = 117 },
	} {
		bad := append([]personalKeyBehaviorMigrationEntry(nil), before...)
		mutate(bad)
		if runtimeArchivePeelV112(db, bad) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("malformed archive fixture receipts changed history")
		}
	}
	for _, values := range []map[string]any{{"initialized": false}, {"retained_count": int64(1)}, {"encrypted_bytes": int64(1)}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&entity.RuntimeArchiveRetention{}).Where("id = ?", 1).Updates(values).Error; err != nil {
				return err
			}
			for _, peel := range []func(*gorm.DB, []personalKeyBehaviorMigrationEntry) error{runtimeArchivePeelV112, insightsReportsPeelV110, insightsReportsPeelV111} {
				if peel(tx, before) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, tx)) || !tx.Migrator().HasTable("runtime_archive_retention") {
					return errors.New("retained accounting admitted or changed historical peel")
				}
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("retained archive accounting refusal", err)
		}
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	controller := deliveryControllerMigrationGrantFixture(stamp)
	retained := []any{
		&entity.RuntimeArchiveCleanup{RequestID: "00000000-0000-4000-8000-000000000001", ActorID: "usr_01arz3ndektsv4rrffq69g5fav", ActorCreatedAt: stamp, ReviewETag: strings.Repeat("a", 64), Reason: "Historical refusal fixture", ArchiveIDsJSON: `["rcv_00000000000000000000000001"]`, DeletedCount: 1, DeletedBytes: 1, CompletedAt: stamp},
		&controller,
		&entity.APIKeyDeliveryControllerAcknowledgement{ID: "kca_01arz3ndektsv4rrffq69g5fav", GrantID: "kcg_01arz3ndektsv4rrffq69g5fav", GrantBirth: stamp, RequestID: "00000000-0000-4000-8000-000000000001", IntentDigest: strings.Repeat("a", 64), DescriptorRevision: 1, ConfigurationDigest: strings.Repeat("a", 64), BindingID: "kvb_01arz3ndektsv4rrffq69g5fav", BindingBirth: stamp, KeyID: "key_01arz3ndektsv4rrffq69g5fav", KeyBirth: stamp, AgentState: "running", OutputState: "written", RecordedAt: stamp},
		&entity.RuntimeConfigurationArchive{ID: "rcv_00000000000000000000000001", CodecVersion: 1, ContentDigest: strings.Repeat("a", 64), SourceDigest: strings.Repeat("b", 64), AuthorizationDigest: strings.Repeat("c", 64), NetworkDigest: strings.Repeat("d", 64), PricingDigest: strings.Repeat("e", 64), PlaintextBytes: 1, ChunkCount: 1, CreatedAt: stamp},
		&entity.SecretProcessVerification{ProcessID: "ins_archive_peel", InventoryVersion: 11, PolicyEpoch: 1, KeyManifestDigest: strings.Repeat("a", 64), CryptoVersion: 2, LeaseToken: "lease_archive_peel", RuntimeSnapshotID: "cfg_archive_peel", RuntimeSourceDigest: strings.Repeat("b", 64), VerifiedAt: stamp},
	}
	for _, row := range retained {
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
			for _, peel := range []func(*gorm.DB, []personalKeyBehaviorMigrationEntry) error{runtimeArchivePeelV112, insightsReportsPeelV110, insightsReportsPeelV111} {
				if peel(tx, before) == nil {
					return errors.New("retained archive facts admitted historical peel")
				}
				if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, tx)) || !tx.Migrator().HasTable("runtime_archive_cleanups") || !tx.Migrator().HasTable("api_key_delivery_controller_grants") || !tx.Migrator().HasTable("api_key_delivery_controller_acknowledgements") || !tx.Migrator().HasTable("runtime_configuration_archives") || !tx.Migrator().HasTable("runtime_configuration_archive_chunks") || !tx.Migrator().HasConstraint(&entity.InsightsReport{}, "ck_insights_report_definition_v111") {
					return errors.New("archive refusal changed schema or receipts")
				}
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("retained archive historical refusal", err)
		}
	}
	if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("archive controls changed historical receipts")
	}
}
