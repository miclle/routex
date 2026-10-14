package handler

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// Historical fixtures may remove V107 only when no recipient notice survives.
// This is an exclusively owned test downgrade, never a production operation.
func legacyOffboardingNotificationsBeforeV107(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := offboardingNotificationsPeelV107(db, personalKeyBehaviorLedger(t, db)); err != nil {
		t.Fatal(err)
	}
}
func offboardingNotificationsPeelV107(db *gorm.DB, expected []personalKeyBehaviorMigrationEntry) error {
	if len(expected) < 1 || len(expected) > 116 {
		return errors.New("unsupported offboarding notification fixture head")
	}
	for i, row := range expected {
		if row.Version != i+1 || row.AppliedAt == "" {
			return errors.New("invalid offboarding notification fixture receipts")
		}
	}
	var actual []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected) {
		return errors.New("offboarding notification receipt mismatch")
	}
	if db.Migrator().HasTable(&entity.OffboardingNotificationRecipient{}) {
		var count int64
		if err := db.Model(&entity.OffboardingNotificationRecipient{}).Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained offboarding recipients prevent historical peel")
		}
	}
	for _, target := range []struct{ table, column string }{{"operational_alerts", "kind"}, {"operational_alert_occurrences", "source_type"}, {"notifications", "kind"}, {"notification_delivery_intents", "kind"}} {
		if !db.Migrator().HasTable(target.table) {
			continue
		}
		var count int64
		if err := db.Table(target.table).Where(target.column+" = ?", "offboarding_completed").Count(&count).Error; err != nil || count != 0 {
			return errors.New("retained offboarding notification prevents historical peel")
		}
	}
	// Validate retained V107 evidence at the reviewed current head before
	// removing any newer schema or receipt. A refusal must change neither.
	if len(expected) > 107 {
		if err := modelDiscoveryPeelV108(db, expected); err != nil {
			return err
		}
		expected = expected[:107]
	}
	if len(expected) < 107 {
		return nil
	}
	// V30's four operational kinds/sources remain the exact historical boundary.
	for _, item := range []struct {
		model any
		name  string
	}{{&offboardingHistoricalAlertV106{}, "ck_operational_alerts_kind"}, {&offboardingHistoricalOccurrenceV106{}, "ck_alert_occurrences_source"}} {
		if db.Migrator().HasConstraint(item.model, item.name) {
			if err := db.Migrator().DropConstraint(item.model, item.name); err != nil {
				return err
			}
		}
		if err := db.Migrator().CreateConstraint(item.model, item.name); err != nil {
			return err
		}
	}
	if err := db.Migrator().DropTable(&entity.OffboardingNotificationRecipient{}); err != nil {
		return err
	}
	result := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 107, expected[106].AppliedAt).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		return errors.New("offboarding notification exact receipt removal failed")
	}
	actual = nil
	if err := db.Table("schema_migrations").Order("version").Find(&actual).Error; err != nil || !reflect.DeepEqual(actual, expected[:106]) {
		return errors.New("offboarding notification peel changed historical receipts")
	}
	return nil
}

type offboardingHistoricalAlertV106 struct {
	Kind string `gorm:"check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure','provider_quality_degraded','route_unavailable')"`
}

func (offboardingHistoricalAlertV106) TableName() string { return "operational_alerts" }

type offboardingHistoricalOccurrenceV106 struct {
	SourceType string `gorm:"check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification','provider_quality_window','gateway_call')"`
}

func (offboardingHistoricalOccurrenceV106) TableName() string { return "operational_alert_occurrences" }

// Runs within the existing offboarding scenario: no registry or named census change.
func offboardingNotificationMigrationControls(t *testing.T, db *gorm.DB) {
	t.Helper()
	legacyModelDiscoveryBeforeV108(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	original := personalKeyBehaviorLedger(t, db)
	if len(original) != 107 || original[106].Version != 107 {
		t.Fatal("exact V107 startup required")
	}
	// Wrong/blank/gapped receipt claims cannot authorize a destructive peel.
	for _, mutation := range []func([]personalKeyBehaviorMigrationEntry){func(v []personalKeyBehaviorMigrationEntry) { v[106].AppliedAt = "" }, func(v []personalKeyBehaviorMigrationEntry) { v[106].Version = 108 }, func(v []personalKeyBehaviorMigrationEntry) { v[106].AppliedAt += "changed" }} {
		bad := append([]personalKeyBehaviorMigrationEntry(nil), original...)
		mutation(bad)
		if offboardingNotificationsPeelV107(db, bad) == nil || !reflect.DeepEqual(original, personalKeyBehaviorLedger(t, db)) {
			t.Fatal("invalid V107 historical peel changed receipts")
		}
	}
	legacyOffboardingNotificationsBeforeV107(t, db)
	prefix := personalKeyBehaviorLedger(t, db)
	if len(prefix) != 106 {
		t.Fatal("V107 peel lost V106 prefix")
	}
	for range 2 {
		if err := database.MigrateThrough(ctx, db, 107); err != nil {
			t.Fatal(err)
		}
	}
	after := personalKeyBehaviorLedger(t, db)
	if len(after) != 107 || !reflect.DeepEqual(prefix, after[:106]) {
		t.Fatal("V107 upgrade/repeat changed historical receipts")
	}
	// Resume independently committed MySQL DDL and recreate a missing closed CHECK.
	if err := db.Migrator().DropConstraint(&entity.OffboardingNotificationRecipient{}, "ck_offboarding_notice_birth"); err != nil {
		t.Fatal(err)
	}
	result := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 107, after[106].AppliedAt).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("remove exact V107 partial receipt")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Go(func() { outcomes <- database.MigrateThrough(ctx, db, 107) })
	}
	wg.Wait()
	close(outcomes)
	for err := range outcomes {
		if err != nil {
			t.Fatal("concurrent partial V107 startup", err)
		}
	}
	if !db.Migrator().HasConstraint(&entity.OffboardingNotificationRecipient{}, "ck_offboarding_notice_birth") || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)[:106]) {
		t.Fatal("V107 partial DDL replay lost frozen constraints/prefix")
	}
	// A retained incompatible partial column must be refused before replacing
	// either released source CHECK, and the exact same prefix survives failure.
	partial := personalKeyBehaviorLedger(t, db)
	removed := db.Table("schema_migrations").Where("version = ? AND applied_at = ?", 107, partial[106].AppliedAt).Delete(&struct{}{})
	if removed.Error != nil || removed.RowsAffected != 1 {
		t.Fatal("remove V107 metadata-control receipt")
	}
	// MySQL requires removing the exact reference before changing its column.
	// The successful frozen V107 replay below must restore it on both drivers.
	if err := db.Migrator().DropConstraint(&entity.OffboardingNotificationRecipient{}, "fk_offboarding_notice_notification"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().AlterColumn(&offboardingReceiptWideV107{}, "NotificationID"); err != nil {
		t.Fatal(err)
	}
	if database.MigrateThrough(ctx, db, 107) == nil || !reflect.DeepEqual(prefix, personalKeyBehaviorLedger(t, db)) {
		t.Fatal("V107 admitted incompatible retained identity metadata")
	}
	if err := db.Migrator().AlterColumn(&entity.OffboardingNotificationRecipient{}, "NotificationID"); err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateThrough(ctx, db, 107); err != nil {
		t.Fatal("restore compatible V107 partial DDL", err)
	}
	if !db.Migrator().HasConstraint(&entity.OffboardingNotificationRecipient{}, "fk_offboarding_notice_notification") {
		t.Fatal("V107 partial DDL replay did not restore notification reference")
	}
	// New-source collation aliases and non-private metadata must fail on both DBs.
	errRejected := errors.New("expected frozen constraint refusal")
	for _, change := range []map[string]any{nil, {"kind": "OFFBOARDING_COMPLETED"}, {"kind": "offboarding_completed "}, {"subject_name": "private"}, {"detail_code": "Completed"}, {"severity": "high"}, {"state": "open"}, {"occurrence_count": 2}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			row := entity.OperationalAlert{ID: "alr_offboarding_invalid", GroupKey: "offboarding-invalid", Kind: "offboarding_completed", Severity: "medium", DetailCode: "completed", State: "resolved", OccurrenceCount: 1, FirstSeenAt: time.Now().UTC(), ETag: "rev_test", UpdatedAt: time.Now().UTC()}
			row.LastSeenAt = row.FirstSeenAt
			values := map[string]any{"id": row.ID, "group_key": row.GroupKey, "kind": row.Kind, "severity": row.Severity, "detail_code": row.DetailCode, "state": row.State, "occurrence_count": row.OccurrenceCount, "first_seen_at": row.FirstSeenAt, "last_seen_at": row.LastSeenAt, "e_tag": row.ETag, "updated_at": row.UpdatedAt, "subject_type": "", "subject_id": "", "subject_name": ""}
			for k, v := range change {
				values[k] = v
			}
			insertErr := tx.Model(&entity.OperationalAlert{}).Omit("FirstRecordedModelCount").Create(values).Error
			if change == nil && insertErr != nil {
				return errors.New("valid V107 alert control refused")
			}
			if change != nil && insertErr == nil {
				return errors.New("invalid V107 alert accepted")
			}
			return errRejected
		})
		if !errors.Is(err, errRejected) {
			t.Fatal(err)
		}
	}
	for _, change := range []map[string]any{nil, {"source_type": "OFFBOARDING_COMPLETED"}, {"source_type": "offboarding_completed "}, {"source_type": "unknown"}, {"source_id": ""}, {"detail_code": "Completed"}, {"subject_type": "user"}, {"subject_id": "usr_private"}, {"subject_name": "private"}} {
		err := db.Transaction(func(tx *gorm.DB) error {
			at := time.Now().UTC()
			alert := entity.OperationalAlert{ID: "alr_offboarding_check", GroupKey: "offboarding-check", Kind: "offboarding_completed", Severity: "medium", DetailCode: "completed", State: "resolved", OccurrenceCount: 1, FirstSeenAt: at, LastSeenAt: at, ETag: "rev_test", UpdatedAt: at}
			if err := tx.Omit("FirstRecordedModelCount", "CredentialID").Create(&alert).Error; err != nil {
				return err
			}
			values := map[string]any{"id": "occ_offboarding_check", "alert_id": alert.ID, "source_type": "offboarding_completed", "source_id": "off_check", "detail_code": "completed", "occurred_at": at, "subject_type": "", "subject_id": "", "subject_name": ""}
			for k, v := range change {
				values[k] = v
			}
			insertErr := tx.Model(&entity.OperationalAlertOccurrence{}).Omit("FirstRecordedModelCount").Create(values).Error
			if change == nil && insertErr != nil {
				return errors.New("valid V107 occurrence control refused")
			}
			if change != nil && insertErr == nil {
				return errors.New("invalid V107 occurrence accepted")
			}
			return errRejected
		})
		if !errors.Is(err, errRejected) {
			t.Fatal(err)
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("restore current schema after V107 controls", err)
	}
}

// Use real retained parents and an otherwise valid fresh tuple so each rejected
// column/index cannot pass because of a missing parent or a different collision.
// Every control rolls back; no synthetic recipient becomes visible or deliverable.
func offboardingNotificationReceiptControls(t *testing.T, db *gorm.DB, receipts []entity.OffboardingNotificationRecipient) {
	t.Helper()
	if len(receipts) < 2 {
		t.Fatal("receipt constraints require two real completion recipients")
	}
	var completed entity.OffboardingCase
	var occurrence entity.OperationalAlertOccurrence
	if err := db.First(&completed, "id = ?", receipts[0].CaseID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&occurrence, "id = ?", receipts[0].OccurrenceID).Error; err != nil {
		t.Fatal(err)
	}
	rollback := errors.New("rollback receipt constraint control")
	for _, control := range []string{"valid", "zero_birth", "null_birth", "foreign_case", "foreign_recipient", "foreign_occurrence", "foreign_notification", "unique_case_recipient", "unique_occurrence_recipient", "unique_notification"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			otherCase := completed
			otherCase.ID, otherCase.RequestID = "off_receipt_control", "req_receipt_control"
			if err := tx.Omit("FirstRecordedModelCount").Create(&otherCase).Error; err != nil {
				return err
			}
			otherOccurrence := occurrence
			otherOccurrence.ID, otherOccurrence.SourceID = "occ_receipt_control", otherCase.ID
			if err := tx.Omit("FirstRecordedModelCount").Create(&otherOccurrence).Error; err != nil {
				return err
			}
			if err := tx.Delete(&receipts[1]).Error; err != nil {
				return err
			}
			candidate := receipts[0]
			candidate.CaseID, candidate.OccurrenceID, candidate.NotificationID = otherCase.ID, otherOccurrence.ID, receipts[1].NotificationID
			switch control {
			case "zero_birth":
				candidate.RecipientCreatedAt = time.Time{}
			case "foreign_case":
				candidate.CaseID = "off_missing"
			case "foreign_recipient":
				candidate.RecipientID = "usr_missing"
			case "foreign_occurrence":
				candidate.OccurrenceID = "occ_missing"
			case "foreign_notification":
				candidate.NotificationID = "ntf_missing"
			case "unique_case_recipient":
				candidate.CaseID = receipts[0].CaseID
			case "unique_occurrence_recipient":
				candidate.OccurrenceID = receipts[0].OccurrenceID
			case "unique_notification":
				candidate.NotificationID = receipts[0].NotificationID
			}
			insertErr := tx.Omit("FirstRecordedModelCount").Create(&candidate).Error
			if control == "null_birth" {
				if insertErr != nil {
					return insertErr
				}
				insertErr = tx.Model(&candidate).UpdateColumn("recipient_created_at", nil).Error
			}
			if control == "valid" {
				if insertErr != nil {
					return insertErr
				}
			} else if insertErr == nil || strings.HasPrefix(control, "foreign_") && !errors.Is(insertErr, gorm.ErrForeignKeyViolated) || strings.HasPrefix(control, "unique_") && !errors.Is(insertErr, gorm.ErrDuplicatedKey) {
				return errors.New("receipt constraint did not reject its exact controlled defect")
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatal("V107 receipt constraint control", control, err)
		}
	}
}

type offboardingReceiptWideV107 struct {
	NotificationID string `gorm:"size:31;not null"`
}

func (offboardingReceiptWideV107) TableName() string { return "offboarding_notification_recipients" }
