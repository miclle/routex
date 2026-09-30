package handler

import (
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

func testNotificationMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []any{
		&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{},
		&entity.NotificationSetting{}, &entity.NotificationDeliveryIntent{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("notification migration omitted table for %T", table)
		}
	}
	for _, item := range []struct {
		model any
		index string
	}{
		{&entity.OperationalAlert{}, "idx_operational_alerts_severity_state"},
		{&entity.OperationalAlertOccurrence{}, "idx_alert_occurrence_source"},
		{&entity.Notification{}, "idx_notifications_recipient_alert"},
		{&entity.Notification{}, "idx_notifications_recipient_last_seen"},
		{&entity.NotificationDeliveryIntent{}, "idx_notification_delivery_occurrence_recipient"},
		{&entity.NotificationDeliveryIntent{}, "idx_notification_delivery_due"},
	} {
		if !db.Migrator().HasIndex(item.model, item.index) {
			t.Fatalf("notification migration omitted index %s", item.index)
		}
	}
	for _, model := range []any{&entity.OperationalAlert{}, &entity.OperationalAlertOccurrence{}, &entity.Notification{}, &entity.NotificationSetting{}, &entity.NotificationDeliveryIntent{}} {
		var count int64
		if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("fresh notification migration seeded %T rows: count=%d err=%v", model, count, err)
		}
	}

	now := time.Now().UTC()
	orphanOccurrence := entity.OperationalAlertOccurrence{ID: "occ_orphan", AlertID: "alr_missing", SourceType: "system_job", SourceID: "job_missing", DetailCode: "publication_failed", OccurredAt: now}
	if err := db.Create(&orphanOccurrence).Error; err == nil {
		t.Fatal("notification migration accepted an occurrence without an alert")
	}
	orphanNotification := entity.Notification{ID: "ntf_orphan", RecipientID: "usr_missing", AlertID: "alr_missing", Kind: "system_job_failure", Severity: "high", DetailCode: "publication_failed", OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now}
	if err := db.Create(&orphanNotification).Error; err == nil {
		t.Fatal("notification migration accepted a notification without recipient and alert")
	}
	orphanSetting := entity.NotificationSetting{UserID: "usr_missing", ExternalEmail: "missing@example.invalid", ETag: "rev_orphan", UpdatedAt: now}
	if err := db.Create(&orphanSetting).Error; err == nil {
		t.Fatal("notification migration accepted settings without a user")
	}
	orphanDelivery := entity.NotificationDeliveryIntent{
		ID: "ndl_orphan", OccurrenceID: "occ_missing", RecipientID: "usr_missing", RecipientEmail: "missing@example.invalid",
		Kind: "system_job_failure", Severity: "high", DetailCode: "publication_failed", SMTPETag: "rev_missing",
		Status: "pending", NextAttemptAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&orphanDelivery).Error; err == nil {
		t.Fatal("notification migration accepted a delivery without occurrence and recipient")
	}
}
