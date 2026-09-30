package database

import (
	"time"

	"gorm.io/gorm"
)

type operationalAlertV29 struct {
	ID              string    `gorm:"primaryKey;size:30"`
	GroupKey        string    `gorm:"size:120;not null;uniqueIndex"`
	Kind            string    `gorm:"size:40;not null;check:ck_operational_alerts_kind,kind IN ('system_job_failure','credential_verification_failure')"`
	Severity        string    `gorm:"size:10;not null;index:idx_operational_alerts_severity_state,priority:1;check:ck_operational_alerts_severity,severity IN ('high','medium')"`
	DetailCode      string    `gorm:"size:40;not null"`
	State           string    `gorm:"size:12;not null;index:idx_operational_alerts_severity_state,priority:2;check:ck_operational_alerts_state,state IN ('open','handling','resolved')"`
	OccurrenceCount int       `gorm:"not null;check:ck_operational_alerts_count,occurrence_count > 0"`
	FirstSeenAt     time.Time `gorm:"precision:6;not null"`
	LastSeenAt      time.Time `gorm:"precision:6;not null;index:idx_operational_alerts_last_seen"`
	ETag            string    `gorm:"size:30;not null"`
	UpdatedAt       time.Time `gorm:"precision:6;not null"`
}

type operationalAlertOccurrenceV29 struct {
	ID         string               `gorm:"primaryKey;size:30"`
	AlertID    string               `gorm:"size:30;not null;index"`
	SourceType string               `gorm:"size:32;not null;uniqueIndex:idx_alert_occurrence_source,priority:1;check:ck_alert_occurrences_source,source_type IN ('system_job','credential_verification')"`
	SourceID   string               `gorm:"size:80;not null;uniqueIndex:idx_alert_occurrence_source,priority:2"`
	DetailCode string               `gorm:"size:40;not null"`
	OccurredAt time.Time            `gorm:"precision:6;not null"`
	Alert      *operationalAlertV29 `gorm:"foreignKey:AlertID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

type notificationUserV29 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (notificationUserV29) TableName() string { return "users" }

type notificationV29 struct {
	ID                 string                         `gorm:"primaryKey;size:30"`
	RecipientID        string                         `gorm:"size:30;not null;uniqueIndex:idx_notifications_recipient_alert,priority:1;index:idx_notifications_recipient_last_seen,priority:1"`
	AlertID            string                         `gorm:"size:30;not null;uniqueIndex:idx_notifications_recipient_alert,priority:2"`
	LatestOccurrenceID string                         `gorm:"size:30;not null"`
	Kind               string                         `gorm:"size:40;not null"`
	Severity           string                         `gorm:"size:10;not null"`
	DetailCode         string                         `gorm:"size:40;not null"`
	OccurrenceCount    int                            `gorm:"not null;check:ck_notifications_count,occurrence_count > 0"`
	Read               bool                           `gorm:"not null;index:idx_notifications_recipient_read,priority:2"`
	FirstSeenAt        time.Time                      `gorm:"precision:6;not null"`
	LastSeenAt         time.Time                      `gorm:"precision:6;not null;index:idx_notifications_recipient_last_seen,priority:2"`
	ReadAt             *time.Time                     `gorm:"precision:6"`
	Recipient          *notificationUserV29           `gorm:"foreignKey:RecipientID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	Alert              *operationalAlertV29           `gorm:"foreignKey:AlertID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	LatestOccurrence   *operationalAlertOccurrenceV29 `gorm:"foreignKey:LatestOccurrenceID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

type notificationSettingV29 struct {
	UserID        string               `gorm:"primaryKey;size:30"`
	ExternalEmail string               `gorm:"size:254;not null"`
	EmailHigh     bool                 `gorm:"not null"`
	EmailMedium   bool                 `gorm:"not null"`
	ETag          string               `gorm:"size:30;not null"`
	UpdatedAt     time.Time            `gorm:"precision:6;not null"`
	User          *notificationUserV29 `gorm:"foreignKey:UserID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

type notificationDeliveryIntentV29 struct {
	ID             string                         `gorm:"primaryKey;size:30"`
	OccurrenceID   string                         `gorm:"size:30;not null;uniqueIndex:idx_notification_delivery_occurrence_recipient,priority:1"`
	RecipientID    string                         `gorm:"size:30;not null;uniqueIndex:idx_notification_delivery_occurrence_recipient,priority:2"`
	RecipientEmail string                         `gorm:"size:254;not null"`
	Kind           string                         `gorm:"size:40;not null"`
	Severity       string                         `gorm:"size:10;not null;check:ck_notification_delivery_severity,severity IN ('high','medium')"`
	DetailCode     string                         `gorm:"size:40;not null"`
	SMTPETag       string                         `gorm:"size:30;not null"`
	Status         string                         `gorm:"size:12;not null;index:idx_notification_delivery_due,priority:1;check:ck_notification_delivery_status,status IN ('pending','retry','sending','accepted','unknown','failed')"`
	Attempts       int                            `gorm:"not null;check:ck_notification_delivery_attempts,attempts >= 0 AND attempts <= 3"`
	NextAttemptAt  time.Time                      `gorm:"precision:6;not null;index:idx_notification_delivery_due,priority:2"`
	LeaseToken     string                         `gorm:"size:30;not null"`
	LeaseUntil     *time.Time                     `gorm:"precision:6"`
	ResultCode     string                         `gorm:"size:40;not null"`
	CreatedAt      time.Time                      `gorm:"precision:6;not null"`
	UpdatedAt      time.Time                      `gorm:"precision:6;not null"`
	CompletedAt    *time.Time                     `gorm:"precision:6"`
	Occurrence     *operationalAlertOccurrenceV29 `gorm:"foreignKey:OccurrenceID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	Recipient      *notificationUserV29           `gorm:"foreignKey:RecipientID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT"`
}

func (operationalAlertV29) TableName() string           { return "operational_alerts" }
func (operationalAlertOccurrenceV29) TableName() string { return "operational_alert_occurrences" }
func (notificationV29) TableName() string               { return "notifications" }
func (notificationSettingV29) TableName() string        { return "notification_settings" }
func (notificationDeliveryIntentV29) TableName() string { return "notification_delivery_intents" }

// Version 29 adds the durable notification inbox, alert aggregation, personal
// delivery preferences, and immutable SMTP delivery intents. It intentionally
// seeds no demonstration alerts or notifications.
func notificationMigration(db *gorm.DB) error {
	return migrateTables(db,
		&operationalAlertV29{},
		&operationalAlertOccurrenceV29{},
		&notificationV29{},
		&notificationSettingV29{},
		&notificationDeliveryIntentV29{},
	)
}
