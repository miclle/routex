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

// Peel only the complete, empty V112 overlay in an exclusively owned fixture.
// Prove every ledger, data and frozen-root guard before the first DDL write.
func runtimeArchivePeelV112(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 112 {
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
	if len(before) != 112 {
		t.Fatal("archive historical guard requires V112")
	}
	for _, mutate := range []func([]personalKeyBehaviorMigrationEntry){
		func(rows []personalKeyBehaviorMigrationEntry) { rows[111].AppliedAt = "changed" },
		func(rows []personalKeyBehaviorMigrationEntry) { rows[111].Version = 113 },
	} {
		bad := append([]personalKeyBehaviorMigrationEntry(nil), before...)
		mutate(bad)
		if runtimeArchivePeelV112(db, bad) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("malformed archive fixture receipts changed history")
		}
	}
	stamp := time.Now().UTC().Truncate(time.Microsecond)
	rollback := errors.New("rollback retained archive fixture control")
	retained := []any{
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
				if !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, tx)) || !tx.Migrator().HasTable("runtime_configuration_archives") || !tx.Migrator().HasTable("runtime_configuration_archive_chunks") || !tx.Migrator().HasConstraint(&entity.InsightsReport{}, "ck_insights_report_definition_v111") {
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
