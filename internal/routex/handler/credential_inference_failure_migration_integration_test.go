package handler

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Frozen V108 envelope definitions are used only to peel a current empty test
// schema. Retained observer facts or projected Credential identities forbid it.
type credentialHistoricalModelDiscoveryAlertV108 struct {
	ID                      string `gorm:"primaryKey;size:30"`
	FirstRecordedModelCount int    `gorm:"not null;default:0"`
	Kind                    string `gorm:"size:40;not null;check:ck_operational_alerts_kind,((kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable') OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 111 AND ASCII(SUBSTRING(kind,2,1)) = 102 AND ASCII(SUBSTRING(kind,3,1)) = 102 AND ASCII(SUBSTRING(kind,4,1)) = 98 AND ASCII(SUBSTRING(kind,5,1)) = 111 AND ASCII(SUBSTRING(kind,6,1)) = 97 AND ASCII(SUBSTRING(kind,7,1)) = 114 AND ASCII(SUBSTRING(kind,8,1)) = 100 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 110 AND ASCII(SUBSTRING(kind,11,1)) = 103 AND ASCII(SUBSTRING(kind,12,1)) = 95 AND ASCII(SUBSTRING(kind,13,1)) = 99 AND ASCII(SUBSTRING(kind,14,1)) = 111 AND ASCII(SUBSTRING(kind,15,1)) = 109 AND ASCII(SUBSTRING(kind,16,1)) = 112 AND ASCII(SUBSTRING(kind,17,1)) = 108 AND ASCII(SUBSTRING(kind,18,1)) = 101 AND ASCII(SUBSTRING(kind,19,1)) = 116 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 6 AND ASCII(SUBSTRING(severity,1,1)) = 109 AND ASCII(SUBSTRING(severity,2,1)) = 101 AND ASCII(SUBSTRING(severity,3,1)) = 100 AND ASCII(SUBSTRING(severity,4,1)) = 105 AND ASCII(SUBSTRING(severity,5,1)) = 117 AND ASCII(SUBSTRING(severity,6,1)) = 109) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND (OCTET_LENGTH(state) = 8 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 115 AND ASCII(SUBSTRING(state,4,1)) = 111 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 118 AND ASCII(SUBSTRING(state,7,1)) = 101 AND ASCII(SUBSTRING(state,8,1)) = 100) AND occurrence_count = 1 AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND first_seen_at = last_seen_at)) AND first_recorded_model_count = 0 AND severity IN ('high','medium')) OR ((OCTET_LENGTH(kind) = 21 AND ASCII(SUBSTRING(kind,1,1)) = 109 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 100 AND ASCII(SUBSTRING(kind,4,1)) = 101 AND ASCII(SUBSTRING(kind,5,1)) = 108 AND ASCII(SUBSTRING(kind,6,1)) = 115 AND ASCII(SUBSTRING(kind,7,1)) = 95 AND ASCII(SUBSTRING(kind,8,1)) = 102 AND ASCII(SUBSTRING(kind,9,1)) = 105 AND ASCII(SUBSTRING(kind,10,1)) = 114 AND ASCII(SUBSTRING(kind,11,1)) = 115 AND ASCII(SUBSTRING(kind,12,1)) = 116 AND ASCII(SUBSTRING(kind,13,1)) = 95 AND ASCII(SUBSTRING(kind,14,1)) = 114 AND ASCII(SUBSTRING(kind,15,1)) = 101 AND ASCII(SUBSTRING(kind,16,1)) = 99 AND ASCII(SUBSTRING(kind,17,1)) = 111 AND ASCII(SUBSTRING(kind,18,1)) = 114 AND ASCII(SUBSTRING(kind,19,1)) = 100 AND ASCII(SUBSTRING(kind,20,1)) = 101 AND ASCII(SUBSTRING(kind,21,1)) = 100) AND (OCTET_LENGTH(severity) = 3 AND ASCII(SUBSTRING(severity,1,1)) = 108 AND ASCII(SUBSTRING(severity,2,1)) = 111 AND ASCII(SUBSTRING(severity,3,1)) = 119) AND (OCTET_LENGTH(detail_code) = 14 AND ASCII(SUBSTRING(detail_code,1,1)) = 102 AND ASCII(SUBSTRING(detail_code,2,1)) = 105 AND ASCII(SUBSTRING(detail_code,3,1)) = 114 AND ASCII(SUBSTRING(detail_code,4,1)) = 115 AND ASCII(SUBSTRING(detail_code,5,1)) = 116 AND ASCII(SUBSTRING(detail_code,6,1)) = 95 AND ASCII(SUBSTRING(detail_code,7,1)) = 114 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 99 AND ASCII(SUBSTRING(detail_code,10,1)) = 111 AND ASCII(SUBSTRING(detail_code,11,1)) = 114 AND ASCII(SUBSTRING(detail_code,12,1)) = 100 AND ASCII(SUBSTRING(detail_code,13,1)) = 101 AND ASCII(SUBSTRING(detail_code,14,1)) = 100) AND first_recorded_model_count BETWEEN 1 AND 2000 AND (OCTET_LENGTH(subject_type) = 8 AND ASCII(SUBSTRING(subject_type,1,1)) = 112 AND ASCII(SUBSTRING(subject_type,2,1)) = 114 AND ASCII(SUBSTRING(subject_type,3,1)) = 111 AND ASCII(SUBSTRING(subject_type,4,1)) = 118 AND ASCII(SUBSTRING(subject_type,5,1)) = 105 AND ASCII(SUBSTRING(subject_type,6,1)) = 100 AND ASCII(SUBSTRING(subject_type,7,1)) = 101 AND ASCII(SUBSTRING(subject_type,8,1)) = 114) AND OCTET_LENGTH(subject_id) BETWEEN 1 AND 30 AND OCTET_LENGTH(subject_name) BETWEEN 1 AND 400)"`
}

func (credentialHistoricalModelDiscoveryAlertV108) TableName() string { return "operational_alerts" }

type credentialHistoricalModelDiscoveryOccurrenceV108 struct {
	ID                      string `gorm:"primaryKey;size:30"`
	FirstRecordedModelCount int    `gorm:"not null;default:0"`
	SourceType              string `gorm:"size:32;not null;check:ck_alert_occurrences_source,((source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call') OR ((OCTET_LENGTH(source_type) = 21 AND ASCII(SUBSTRING(source_type,1,1)) = 111 AND ASCII(SUBSTRING(source_type,2,1)) = 102 AND ASCII(SUBSTRING(source_type,3,1)) = 102 AND ASCII(SUBSTRING(source_type,4,1)) = 98 AND ASCII(SUBSTRING(source_type,5,1)) = 111 AND ASCII(SUBSTRING(source_type,6,1)) = 97 AND ASCII(SUBSTRING(source_type,7,1)) = 114 AND ASCII(SUBSTRING(source_type,8,1)) = 100 AND ASCII(SUBSTRING(source_type,9,1)) = 105 AND ASCII(SUBSTRING(source_type,10,1)) = 110 AND ASCII(SUBSTRING(source_type,11,1)) = 103 AND ASCII(SUBSTRING(source_type,12,1)) = 95 AND ASCII(SUBSTRING(source_type,13,1)) = 99 AND ASCII(SUBSTRING(source_type,14,1)) = 111 AND ASCII(SUBSTRING(source_type,15,1)) = 109 AND ASCII(SUBSTRING(source_type,16,1)) = 112 AND ASCII(SUBSTRING(source_type,17,1)) = 108 AND ASCII(SUBSTRING(source_type,18,1)) = 101 AND ASCII(SUBSTRING(source_type,19,1)) = 116 AND ASCII(SUBSTRING(source_type,20,1)) = 101 AND ASCII(SUBSTRING(source_type,21,1)) = 100) AND (OCTET_LENGTH(detail_code) = 9 AND ASCII(SUBSTRING(detail_code,1,1)) = 99 AND ASCII(SUBSTRING(detail_code,2,1)) = 111 AND ASCII(SUBSTRING(detail_code,3,1)) = 109 AND ASCII(SUBSTRING(detail_code,4,1)) = 112 AND ASCII(SUBSTRING(detail_code,5,1)) = 108 AND ASCII(SUBSTRING(detail_code,6,1)) = 101 AND ASCII(SUBSTRING(detail_code,7,1)) = 116 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 100) AND OCTET_LENGTH(subject_type) = 0 AND OCTET_LENGTH(subject_id) = 0 AND OCTET_LENGTH(subject_name) = 0 AND OCTET_LENGTH(source_id) > 0)) AND first_recorded_model_count = 0) OR ((OCTET_LENGTH(source_type) = 15 AND ASCII(SUBSTRING(source_type,1,1)) = 109 AND ASCII(SUBSTRING(source_type,2,1)) = 111 AND ASCII(SUBSTRING(source_type,3,1)) = 100 AND ASCII(SUBSTRING(source_type,4,1)) = 101 AND ASCII(SUBSTRING(source_type,5,1)) = 108 AND ASCII(SUBSTRING(source_type,6,1)) = 95 AND ASCII(SUBSTRING(source_type,7,1)) = 100 AND ASCII(SUBSTRING(source_type,8,1)) = 105 AND ASCII(SUBSTRING(source_type,9,1)) = 115 AND ASCII(SUBSTRING(source_type,10,1)) = 99 AND ASCII(SUBSTRING(source_type,11,1)) = 111 AND ASCII(SUBSTRING(source_type,12,1)) = 118 AND ASCII(SUBSTRING(source_type,13,1)) = 101 AND ASCII(SUBSTRING(source_type,14,1)) = 114 AND ASCII(SUBSTRING(source_type,15,1)) = 121) AND (OCTET_LENGTH(detail_code) = 14 AND ASCII(SUBSTRING(detail_code,1,1)) = 102 AND ASCII(SUBSTRING(detail_code,2,1)) = 105 AND ASCII(SUBSTRING(detail_code,3,1)) = 114 AND ASCII(SUBSTRING(detail_code,4,1)) = 115 AND ASCII(SUBSTRING(detail_code,5,1)) = 116 AND ASCII(SUBSTRING(detail_code,6,1)) = 95 AND ASCII(SUBSTRING(detail_code,7,1)) = 114 AND ASCII(SUBSTRING(detail_code,8,1)) = 101 AND ASCII(SUBSTRING(detail_code,9,1)) = 99 AND ASCII(SUBSTRING(detail_code,10,1)) = 111 AND ASCII(SUBSTRING(detail_code,11,1)) = 114 AND ASCII(SUBSTRING(detail_code,12,1)) = 100 AND ASCII(SUBSTRING(detail_code,13,1)) = 101 AND ASCII(SUBSTRING(detail_code,14,1)) = 100) AND first_recorded_model_count BETWEEN 1 AND 2000 AND (OCTET_LENGTH(subject_type) = 8 AND ASCII(SUBSTRING(subject_type,1,1)) = 112 AND ASCII(SUBSTRING(subject_type,2,1)) = 114 AND ASCII(SUBSTRING(subject_type,3,1)) = 111 AND ASCII(SUBSTRING(subject_type,4,1)) = 118 AND ASCII(SUBSTRING(subject_type,5,1)) = 105 AND ASCII(SUBSTRING(subject_type,6,1)) = 100 AND ASCII(SUBSTRING(subject_type,7,1)) = 101 AND ASCII(SUBSTRING(subject_type,8,1)) = 114) AND OCTET_LENGTH(subject_id) BETWEEN 1 AND 30 AND OCTET_LENGTH(subject_name) BETWEEN 1 AND 400 AND OCTET_LENGTH(source_id) = 30)"`
}

func (credentialHistoricalModelDiscoveryOccurrenceV108) TableName() string {
	return "operational_alert_occurrences"
}

func credentialInferencePeelV109(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 116 {
		return errors.New("unsupported Credential fixture head")
	}
	for i, entry := range expected {
		if entry.Version != i+1 || entry.AppliedAt == "" {
			return errors.New("invalid Credential fixture receipt")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("Credential fixture receipt mismatch")
	}
	if len(expected) < 109 {
		return nil
	}
	var count int64
	for _, model := range []any{&entity.CredentialInferenceFailureEpisode{}, &entity.CredentialInferenceFailureState{}} {
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained Credential observer prevents peel")
		}
	}
	for _, table := range []string{"operational_alerts", "operational_alert_occurrences", "notifications", "notification_delivery_intents"} {
		discriminator, value := "kind", "credential_inference_failure"
		if table == "operational_alert_occurrences" {
			discriminator, value = "source_type", "credential_inference"
		}
		if err := db.Table(table).Where("OCTET_LENGTH(credential_id) <> 0 OR "+discriminator+" = ?", value).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained Credential projection prevents peel")
		}
	}
	if len(expected) >= 110 {
		if err := insightsReportsPeelV110(db, expected); err != nil {
			return err
		}
		expected = expected[:109]
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&credentialHistoricalModelDiscoveryAlertV108{}, "ck_operational_alerts_kind"},
		{&credentialHistoricalModelDiscoveryOccurrenceV108{}, "ck_alert_occurrences_source"},
	} {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		model any
		name  string
	}{
		{&entity.Notification{}, "ck_notifications_credential_inference"},
		{&entity.NotificationDeliveryIntent{}, "ck_notification_delivery_credential_inference"},
	} {
		if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	for _, model := range []any{&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{}, &entity.NotificationDeliveryIntent{}} {
		if err := db.Migrator().DropColumn(model, "CredentialID"); err != nil {
			return err
		}
	}
	if err := db.Migrator().DropTable(&entity.CredentialInferenceFailureEpisode{}, &entity.CredentialInferenceFailureState{}); err != nil {
		return err
	}
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 109, expected[108].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		return errors.New("exact Credential receipt removal failed")
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:108]) {
		return errors.New("Credential peel changed retained prefix")
	}
	return nil
}

func legacyCredentialInferenceBeforeV109(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := credentialInferencePeelV109(db, personalKeyBehaviorLedger(t, db)); err != nil {
		t.Fatal(err)
	}
}

// Wrong partial-column models deliberately omit CHECKs and ordinary entity
// callbacks. All DDL goes through GORM against the three existing table names.
type discoveryNullableCountFixture struct {
	FirstRecordedModelCount *int `gorm:"default:0"`
}
type discoveryWrongDefaultCountFixture struct {
	FirstRecordedModelCount int `gorm:"not null;default:1"`
}
type discoveryMissingDefaultCountFixture struct {
	FirstRecordedModelCount int `gorm:"not null"`
}
type discoveryWrongTypeCountFixture struct {
	FirstRecordedModelCount string `gorm:"size:30;not null;default:0"`
}
type discoveryValidCountFixture struct {
	FirstRecordedModelCount int `gorm:"not null;default:0"`
}

func modelDiscoveryPartialCountControls(t *testing.T, db *gorm.DB) {
	t.Helper()
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 107 {
		t.Fatal("V107 baseline required for partial discovery columns")
	}
	for _, table := range []string{"operational_alerts", "operational_alert_occurrences", "notifications"} {
		for _, model := range []any{&discoveryNullableCountFixture{}, &discoveryWrongDefaultCountFixture{}, &discoveryMissingDefaultCountFixture{}, &discoveryWrongTypeCountFixture{}} {
			if err := db.Table(table).Migrator().AddColumn(model, "FirstRecordedModelCount"); err != nil {
				t.Fatal("create malformed count control", table, err)
			}
			if err := database.Migrate(t.Context(), db); err == nil {
				t.Fatal("malformed empty count column accepted", table)
			}
			if !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) {
				t.Fatal("failed count validation changed migration receipts")
			}
			if err := db.Table(table).Migrator().DropColumn(model, "FirstRecordedModelCount"); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Valid partial DDL is a positive baseline: rejecting every existing column
	// would fail this upgrade and cannot satisfy the negative controls above.
	for _, table := range []string{"operational_alerts", "operational_alert_occurrences", "notifications"} {
		if !db.Table(table).Migrator().HasColumn(&discoveryValidCountFixture{}, "FirstRecordedModelCount") {
			if err := db.Table(table).Migrator().AddColumn(&discoveryValidCountFixture{}, "FirstRecordedModelCount"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := database.Migrate(t.Context(), db); err != nil {
		t.Fatal("valid empty partial count baseline rejected", err)
	}
	if got := personalKeyBehaviorLedger(t, db); len(got) != 116 || got[110].Version != 111 || got[109].Version != 110 || !reflect.DeepEqual(prefix, got[:107]) {
		t.Fatal("valid partial count upgrade changed prefix")
	}
	legacyModelDiscoveryBeforeV108(t, db)
}

// These fixed GORM maps represent a pre-observer Call/Attempt without invoking
// current business code or adding future tables to the historical baseline.
func credentialFailureHistoricalRows(tx *gorm.DB, suffix string, at time.Time) error {
	request, attempt := "req_failure_history_"+suffix, "att_failure_history_"+suffix
	if err := tx.Table("call_records").Create(map[string]any{
		"request_id": request, "snapshot_id": "cfg_history", "user_id": "usr_history", "key_id": "key_history", "model_id": "mdl_history",
		"model_name": "", "provider_model_id": "", "connection_id": "", "protocol": "openai_chat", "status": "error", "stream": false,
		"started_at": at, "completed_at": at, "duration_ms": 0, "error_code": "",
	}).Error; err != nil {
		return err
	}
	return tx.Table("call_attempts").Create(map[string]any{
		"id": attempt, "request_id": request, "credential_id": "crd_history", "snapshot_id": "cfg_history", "provider_model_id": "", "connection_id": "",
		"attempt_number": 1, "status": "error", "http_status": 0, "error_code": "", "started_at": at, "completed_at": at,
	}).Error
}

type credentialNullableEnvelopeFixture struct {
	CredentialID *string `gorm:"size:30;default:''"`
}
type credentialWrongDefaultEnvelopeFixture struct {
	CredentialID string `gorm:"size:30;not null;default:wrong"`
}
type credentialWrongTypeEnvelopeFixture struct {
	CredentialID int `gorm:"not null;default:0"`
}
type credentialValidEnvelopeFixture struct {
	CredentialID string `gorm:"size:30;not null;default:''"`
}

func testCredentialInferenceFailureMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := t.Context()
	head := personalKeyBehaviorLedger(t, db)
	if len(head) != 116 || head[110].Version != 111 || head[109].Version != 110 {
		t.Fatal("exact V110 startup head required")
	}
	for _, change := range []func([]personalKeyBehaviorMigrationEntry){func(v []personalKeyBehaviorMigrationEntry) { v[108].Version = 110 }, func(v []personalKeyBehaviorMigrationEntry) { v[108].AppliedAt = "" }, func(v []personalKeyBehaviorMigrationEntry) { v[108].AppliedAt += "changed" }} {
		invalid := append([]personalKeyBehaviorMigrationEntry(nil), head...)
		change(invalid)
		if credentialInferencePeelV109(db, invalid) == nil || !reflect.DeepEqual(head, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid Credential peel changed receipts")
		}
	}
	legacyCredentialInferenceBeforeV109(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	at := time.Now().UTC().Truncate(time.Microsecond)
	if err := db.Transaction(func(tx *gorm.DB) error { return credentialFailureHistoricalRows(tx, "retained", at) }); err != nil {
		t.Fatal(err)
	}
	var historical entity.CallAttempt
	if err := db.Take(&historical, "id = ?", "att_failure_history_retained").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	var retained entity.CallAttempt
	var count int64
	if err := db.Take(&retained, "id = ?", historical.ID).Error; err != nil || !reflect.DeepEqual(historical, retained) {
		t.Fatal("upgrade rewrote historical Attempt", err)
	}
	for _, model := range []any{&entity.CredentialInferenceFailureState{}, &entity.CredentialInferenceFailureEpisode{}} {
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("upgrade invented historical observer facts", err)
		}
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 116 || after[110].Version != 111 || after[109].Version != 110 || !reflect.DeepEqual(prefix, after[:108]) {
		t.Fatal("observer upgrade changed historical prefix")
	}
	// Missing CHECKs represent interrupted MySQL DDL, not a different version.
	if err := db.Migrator().DropConstraint(&entity.CredentialInferenceFailureState{}, "ck_credential_failure_count"); err != nil {
		t.Fatal(err)
	}
	removeReceipt := func() {
		t.Helper()
		ledger := personalKeyBehaviorLedger(t, db)
		r := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 109, ledger[108].AppliedAt).Delete(&struct{}{})
		if r.Error != nil || r.RowsAffected != 1 {
			t.Fatal("remove exact V109 receipt", r.Error)
		}
	}
	removeReceipt()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() { results <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal("concurrent partial observer startup", err)
		}
	}
	if !db.Migrator().HasConstraint(&entity.CredentialInferenceFailureState{}, "ck_credential_failure_count") {
		t.Fatal("missing repaired state CHECK")
	}
	for _, table := range []string{"operational_alerts", "operational_alert_occurrences", "notifications", "notification_delivery_intents"} {
		for _, model := range []any{&credentialNullableEnvelopeFixture{}, &credentialWrongDefaultEnvelopeFixture{}, &credentialWrongTypeEnvelopeFixture{}} {
			removeReceipt()
			constraint := map[string]string{"operational_alerts": "ck_operational_alerts_kind", "operational_alert_occurrences": "ck_alert_occurrences_source", "notifications": "ck_notifications_credential_inference", "notification_delivery_intents": "ck_notification_delivery_credential_inference"}[table]
			if err := db.Table(table).Migrator().DropConstraint(&credentialValidEnvelopeFixture{}, constraint); err != nil {
				t.Fatal(err)
			}
			if err := db.Table(table).Migrator().DropColumn(&credentialValidEnvelopeFixture{}, "CredentialID"); err != nil {
				t.Fatal(err)
			}
			if err := db.Table(table).Migrator().AddColumn(model, "CredentialID"); err != nil {
				t.Fatal("create malformed Credential envelope", table, err)
			}
			baseline := personalKeyBehaviorLedger(t, db)
			if database.Migrate(ctx, db) == nil || !reflect.DeepEqual(baseline, personalKeyBehaviorLedger(t, db)) {
				t.Fatal("malformed empty Credential column accepted", table)
			}
			if err := db.Table(table).Migrator().DropColumn(model, "CredentialID"); err != nil {
				t.Fatal(err)
			}
			if err := db.Table(table).Migrator().AddColumn(&credentialValidEnvelopeFixture{}, "CredentialID"); err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(ctx, db); err != nil {
				t.Fatal("compatible partial Credential envelope rejected", table, err)
			}
		}
	}
	// GORM uint64 becomes BIGINT UNSIGNED where the engine supports it. It
	// must never be accepted as this observer's signed int64 sequence.
	removeReceipt()
	for _, name := range []string{"ck_credential_failure_sequence", "ck_credential_failure_count", "ck_credential_failure_last_attempt"} {
		if err := db.Migrator().DropConstraint(&entity.CredentialInferenceFailureState{}, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrator().DropColumn(&entity.CredentialInferenceFailureState{}, "AcceptanceSequence"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AddColumn(&credentialUnsignedStateSequenceFixture{}, "AcceptanceSequence"); err != nil {
		t.Fatal(err)
	}
	columns, err := db.Migrator().ColumnTypes(&entity.CredentialInferenceFailureState{})
	if err != nil {
		t.Fatal(err)
	}
	unsigned := false
	found := false
	for _, column := range columns {
		if column.Name() == "acceptance_sequence" {
			full, known := column.ColumnType()
			if !known {
				t.Fatal("sequence signedness metadata unavailable")
			}
			found = true
			unsigned = strings.Contains(strings.ToLower(full), "unsigned")
		}
	}
	if !found {
		t.Fatal("unsigned control did not create sequence column")
	}
	// Assert the fixture's physical meaning in the test layer. A MySQL
	// fixture unexpectedly producing signed DDL must not silently take the
	// PostgreSQL positive branch and erase the unsigned rejection control.
	switch db.Name() {
	case "mysql":
		if !unsigned {
			t.Fatal("MySQL uint64 control did not produce unsigned metadata")
		}
	case "postgres":
		if unsigned {
			t.Fatal("PostgreSQL uint64 control unexpectedly produced unsigned metadata")
		}
	default:
		t.Fatal("unsupported observer migration control driver")
	}
	baseline := personalKeyBehaviorLedger(t, db)
	result := database.Migrate(ctx, db)
	if unsigned {
		if result == nil || !reflect.DeepEqual(baseline, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("unsigned partial observer accepted or changed receipts")
		}
	} else {
		// PostgreSQL has no unsigned integer type: this GORM field produced a
		// genuine signed baseline, and must still migrate successfully.
		if result != nil {
			t.Fatal("signed partial observer baseline rejected", result)
		}
		removeReceipt()
		for _, name := range []string{"ck_credential_failure_sequence", "ck_credential_failure_count", "ck_credential_failure_last_attempt"} {
			if err := db.Migrator().DropConstraint(&entity.CredentialInferenceFailureState{}, name); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := db.Migrator().DropColumn(&entity.CredentialInferenceFailureState{}, "AcceptanceSequence"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AddColumn(&credentialSignedStateSequenceFixture{}, "AcceptanceSequence"); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restored signed observer baseline rejected", err)
	}
	digest := strings.Repeat("a", 64)
	validState := func() entity.CredentialInferenceFailureState {
		return entity.CredentialInferenceFailureState{CredentialDigest: digest, CredentialID: "crd_history", AcceptanceSequence: 5, ConsecutiveFailures: 5, Latched: true, LastAttemptID: historical.ID, LastTriggerAttemptID: historical.ID, UpdatedAt: at}
	}
	validEpisode := func() entity.CredentialInferenceFailureEpisode {
		return entity.CredentialInferenceFailureEpisode{TriggerAttemptID: historical.ID, CredentialDigest: digest, CredentialID: "crd_history", AcceptanceSequence: 5, FailureCount: 5, RecordedAt: at}
	}
	rollback := errors.New("positive baseline rolled back")
	if err := db.Transaction(func(tx *gorm.DB) error {
		state, episode := validState(), validEpisode()
		if err := tx.Create(&state).Error; err != nil {
			return err
		}
		if err := tx.Create(&episode).Error; err != nil {
			return err
		}
		if err := credentialFailureHistoricalRows(tx, "second_episode", at); err != nil {
			return err
		}
		episode.TriggerAttemptID = "att_failure_history_second_episode"
		episode.AcceptanceSequence = 6
		if err := tx.Create(&episode).Error; err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal("valid observer CHECK/FK baseline or same-Credential episodes rejected", err)
	}
	for _, parent := range []struct {
		model any
		key   string
		value string
	}{
		{&entity.CredentialInferenceFailureState{}, "credential_digest", digest},
		{&entity.CallAttempt{}, "id", historical.ID},
	} {
		deleteAttempted := false
		err := db.Transaction(func(tx *gorm.DB) error {
			state, episode := validState(), validEpisode()
			if err := tx.Create(&state).Error; err != nil {
				return fmt.Errorf("positive state baseline: %w", err)
			}
			if err := tx.Create(&episode).Error; err != nil {
				return fmt.Errorf("positive episode baseline: %w", err)
			}
			deleteAttempted = true
			if err := tx.Where(parent.key+" = ?", parent.value).Delete(parent.model).Error; err != nil {
				return err
			}
			return rollback
		})
		if !deleteAttempted || !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatal("episode evidence deletion must be restricted", parent.key, err)
		}
	}
	for _, change := range []map[string]any{{"credential_id": ""}, {"credential_digest": "short"}, {"acceptance_sequence": -1}, {"consecutive_failures": 6}, {"latched": false}, {"last_attempt_id": ""}, {"updated_at": time.Time{}}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			state := validState()
			if err := tx.Create(&state).Error; err != nil {
				return fmt.Errorf("positive state baseline: %w", err)
			}
			if err := tx.Model(&entity.CredentialInferenceFailureState{}).Where("credential_digest = ?", digest).Updates(change).Error; err != nil {
				return err
			}
			return rollback
		})
		if err == nil || errors.Is(err, rollback) {
			t.Fatal("invalid state constraint accepted", change, err)
		}
	}
	for _, change := range []map[string]any{{"credential_id": ""}, {"acceptance_sequence": 4}, {"failure_count": 4}, {"recorded_at": time.Time{}}, {"trigger_attempt_id": "att_missing_history"}, {"credential_digest": strings.Repeat("b", 64)}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			state, episode := validState(), validEpisode()
			if err := tx.Create(&state).Error; err != nil {
				return fmt.Errorf("positive state baseline: %w", err)
			}
			if err := tx.Create(&episode).Error; err != nil {
				return fmt.Errorf("positive episode baseline: %w", err)
			}
			if err := tx.Model(&entity.CredentialInferenceFailureEpisode{}).Where("trigger_attempt_id = ?", historical.ID).Updates(change).Error; err != nil {
				return err
			}
			return rollback
		})
		if err == nil || errors.Is(err, rollback) {
			t.Fatal("invalid episode constraint/FK accepted", change, err)
		}
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		state, episode := validState(), validEpisode()
		if err := tx.Create(&state).Error; err != nil {
			return err
		}
		if err := tx.Create(&episode).Error; err != nil {
			return err
		}
		if err := credentialFailureHistoricalRows(tx, "unique", at); err != nil {
			return err
		}
		episode.TriggerAttemptID = "att_failure_history_unique"
		if err := tx.Create(&episode).Error; err != nil {
			return err
		}
		return rollback
	}); err == nil || errors.Is(err, rollback) {
		t.Fatal("duplicate Credential acceptance sequence accepted", err)
	}
	if !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)[:108]) {
		t.Fatal("constraint controls changed historical ledger")
	}
}

type credentialUnsignedStateSequenceFixture struct {
	AcceptanceSequence uint64 `gorm:"not null"`
}

func (credentialUnsignedStateSequenceFixture) TableName() string {
	return "credential_inference_failure_states"
}

type credentialSignedStateSequenceFixture struct {
	AcceptanceSequence int64 `gorm:"not null"`
}

func (credentialSignedStateSequenceFixture) TableName() string {
	return "credential_inference_failure_states"
}
