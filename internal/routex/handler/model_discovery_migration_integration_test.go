package handler

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Test-only historical peel refuses retained discovery evidence before restoring
// the exact V107 CHECKs and removing only the reviewed V108 ledger receipt.
func legacyModelDiscoveryBeforeV108(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := modelDiscoveryPeelV108(db, personalKeyBehaviorLedger(t, db)); err != nil {
		t.Fatal(err)
	}
}

func modelDiscoveryPeelV108(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 112 {
		return errors.New("unsupported discovery fixture head")
	}
	for i, entry := range expected {
		if entry.Version != i+1 || entry.AppliedAt == "" {
			return errors.New("invalid discovery fixture receipt")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(expected, actual) {
		return errors.New("discovery receipt mismatch")
	}
	if len(expected) < 108 {
		return nil
	}
	var count int64
	if err := db.Model(&entity.ModelDiscoveryNotice{}).Count(&count).Error; err != nil || count != 0 {
		return errors.New("retained discovery source prevents peel")
	}
	for _, target := range []struct{ table, column string }{{"operational_alerts", "kind"}, {"operational_alert_occurrences", "source_type"}, {"notifications", "kind"}, {"notification_delivery_intents", "kind"}} {
		value := "models_first_recorded"
		if target.column == "source_type" {
			value = "model_discovery"
		}
		if err := db.Table(target.table).Where(target.column+" = ?", value).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained discovery projection prevents peel")
		}
	}
	// Retained discovery facts/projections reject the reviewed current head
	// without first consuming its successor schema or ledger receipt.
	if len(expected) >= 109 {
		if err := credentialInferencePeelV109(db, expected); err != nil {
			return err
		}
		expected = expected[:108]
	}
	for _, item := range []struct {
		model any
		name  string
	}{{&discoveryHistoricalAlertV107{}, "ck_operational_alerts_kind"}, {&discoveryHistoricalOccurrenceV107{}, "ck_alert_occurrences_source"}, {&discoveryHistoricalSeverityV107{}, "ck_operational_alerts_severity"}} {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	if err := db.Migrator().DropConstraint(&entity.NotificationDeliveryIntent{}, "ck_notification_delivery_no_discovery"); err != nil {
		return err
	}
	if err := db.Migrator().DropConstraint(&entity.Notification{}, "ck_notifications_discovery"); err != nil {
		return err
	}
	for _, model := range []any{&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{}} {
		if err := db.Migrator().DropColumn(model, "FirstRecordedModelCount"); err != nil {
			return err
		}
	}
	if err := db.Migrator().DropTable(&entity.ModelDiscoveryNotice{}); err != nil {
		return err
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 108, expected[107].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		return errors.New("discovery receipt removal failed")
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(expected[:107], actual) {
		return errors.New("discovery peel changed historical receipts")
	}
	return nil
}

type discoveryHistoricalAlertV107 struct {
	Kind string `gorm:"check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable') OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 111 AND ASCII(SUBSTRING(kind,2,1)) = 102 AND ASCII(SUBSTRING(kind,3,1)) = 102 AND ASCII(SUBSTRING(kind,4,1)) = 98 AND ASCII(SUBSTRING(kind,5,1)) = 111 AND ASCII(SUBSTRING(kind,6,1)) = 97 AND ASCII(SUBSTRING(kind,7,1)) = 114 AND ASCII(SUBSTRING(kind,8,1)) = 100 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 110 AND ASCII(SUBSTRING(kind,11,1)) = 103 AND ASCII(SUBSTRING(kind,12,1)) = 95 AND ASCII(SUBSTRING(kind,13,1)) = 99 AND ASCII(SUBSTRING(kind,14,1)) = 111 AND ASCII(SUBSTRING(kind,15,1)) = 109 AND ASCII(SUBSTRING(kind,16,1)) = 112 AND ASCII(SUBSTRING(kind,17,1)) = 108 AND ASCII(SUBSTRING(kind,18,1)) = 101 AND ASCII(SUBSTRING(kind,19,1)) = 116 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 6 AND ASCII(SUBSTRING(severity,1,1)) = 109 AND ASCII(SUBSTRING(severity,2,1)) = 101 AND ASCII(SUBSTRING(severity,3,1)) = 100 AND ASCII(SUBSTRING(severity,4,1)) = 105 AND ASCII(SUBSTRING(severity,5,1)) = 117 AND ASCII(SUBSTRING(severity,6,1)) = 109) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND (OCTET_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 115 AND ASCII(SUBSTRING(state,4,1)) = 111 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 118 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100) AND occurrence_count = 1 AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND first_seen_at = last_seen_at)"`
}

func (discoveryHistoricalAlertV107) TableName() string { return "operational_alerts" }

type discoveryHistoricalOccurrenceV107 struct {
	SourceType string `gorm:"check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call') OR ((OCTET_LENGTH(source_type) = 21 AND ASCII(SUBSTRING(source_type,1,1)) = 111 AND ASCII(SUBSTRING(source_type,2,1)) = 102 AND ASCII(SUBSTRING(source_type,3,1)) = 102 AND ASCII(SUBSTRING(source_type,4,1)) = 98 AND ASCII(SUBSTRING(source_type,5,1)) = 111 AND ASCII(SUBSTRING(source_type,6,1)) = 97 AND ASCII(SUBSTRING(source_type,7,1)) = 114 AND ASCII(SUBSTRING(source_type,8,1)) = 100 AND ASCII(SUBSTRING(source_type,9,1)) = 105 AND ASCII(SUBSTRING(source_type,10,1)) = 110 AND ASCII(SUBSTRING(source_type,11,1)) = 103 AND ASCII(SUBSTRING(source_type,12,1)) = 95 AND ASCII(SUBSTRING(source_type,13,1)) = 99 AND ASCII(SUBSTRING(source_type,14,1)) = 111 AND ASCII(SUBSTRING(source_type,15,1)) = 109 AND ASCII(SUBSTRING(source_type,16,1)) = 112 AND ASCII(SUBSTRING(source_type,17,1)) = 108 AND ASCII(SUBSTRING(source_type,18,1)) = 101 AND ASCII(SUBSTRING(source_type,19,1)) = 116 AND ASCII(SUBSTRING(source_type,20,1)) = 101 AND ASCII(SUBSTRING(source_type,21,1)) = 100) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND OCTET_LENGTH(source_id) > 0)"`
}

func (discoveryHistoricalOccurrenceV107) TableName() string { return "operational_alert_occurrences" }

type discoveryHistoricalSeverityV107 struct {
	Severity string `gorm:"check:ck_operational_alerts_severity,severity IN ('high','medium')"`
}

func (discoveryHistoricalSeverityV107) TableName() string { return "operational_alerts" }

// Reuses the catalog scenario's exclusive fresh database; no registry or process
// birth is added. V107 controls remain separate and immutable in meaning.
func modelDiscoveryMigrationControls(t *testing.T, db *gorm.DB) {
	t.Helper()
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 112 || before[110].Version != 111 || before[109].Version != 110 {
		t.Fatal("exact discovery startup head required")
	}
	for _, change := range []func([]personalKeyBehaviorMigrationEntry){func(v []personalKeyBehaviorMigrationEntry) { v[107].Version = 109 }, func(v []personalKeyBehaviorMigrationEntry) { v[107].AppliedAt = "" }, func(v []personalKeyBehaviorMigrationEntry) { v[107].AppliedAt += "changed" }} {
		invalid := append([]personalKeyBehaviorMigrationEntry(nil), before...)
		change(invalid)
		if modelDiscoveryPeelV108(db, invalid) == nil || !reflect.DeepEqual(before, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid discovery peel changed receipts")
		}
	}
	legacyModelDiscoveryBeforeV108(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 107 || !reflect.DeepEqual(before[:107], prefix) {
		t.Fatal("discovery peel lost V107 receipts")
	}
	modelDiscoveryPartialCountControls(t, db)
	for range 2 {
		if err := database.Migrate(t.Context(), db); err != nil {
			t.Fatal(err)
		}
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 112 || after[110].Version != 111 || after[109].Version != 110 || !reflect.DeepEqual(prefix, after[:107]) {
		t.Fatal("discovery upgrade/repeat changed historical receipts")
	}
	legacyCredentialInferenceBeforeV109(t, db)
	if err := db.Migrator().DropConstraint(&entity.ModelDiscoveryNotice{}, "ck_model_discovery_count"); err != nil {
		t.Fatal(err)
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 108, after[107].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("remove exact partial V108 receipt")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Go(func() { outcomes <- database.Migrate(t.Context(), db) })
	}
	wg.Wait()
	close(outcomes)
	for err := range outcomes {
		if err != nil {
			t.Fatal("concurrent partial discovery startup", err)
		}
	}
	if !db.Migrator().HasConstraint(&entity.ModelDiscoveryNotice{}, "ck_model_discovery_count") || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)[:107]) {
		t.Fatal("partial discovery repair lost constraint or prefix")
	}
}
