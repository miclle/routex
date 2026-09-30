package entity

import "time"

// OperationalAlert is a secret-free aggregation of allowlisted operational
// failures. Source occurrences are stored separately so retries cannot inflate
// the count or create duplicate recipient notifications.
type OperationalAlert struct {
	ID              string    `gorm:"primaryKey;size:30"`
	GroupKey        string    `gorm:"size:120;not null;uniqueIndex"`
	Kind            string    `gorm:"size:40;not null"`
	Severity        string    `gorm:"size:10;not null;index:idx_operational_alerts_severity_state,priority:1"`
	DetailCode      string    `gorm:"size:40;not null"`
	State           string    `gorm:"size:12;not null;index:idx_operational_alerts_severity_state,priority:2"`
	OccurrenceCount int       `gorm:"not null"`
	FirstSeenAt     time.Time `gorm:"precision:6;not null"`
	LastSeenAt      time.Time `gorm:"precision:6;not null;index:idx_operational_alerts_last_seen"`
	ETag            string    `gorm:"size:30;not null"`
	UpdatedAt       time.Time `gorm:"precision:6;not null"`
}

type OperationalAlertOccurrence struct {
	ID         string    `gorm:"primaryKey;size:30"`
	AlertID    string    `gorm:"size:30;not null;index"`
	SourceType string    `gorm:"size:32;not null;uniqueIndex:idx_alert_occurrence_source,priority:1"`
	SourceID   string    `gorm:"size:80;not null;uniqueIndex:idx_alert_occurrence_source,priority:2"`
	DetailCode string    `gorm:"size:40;not null"`
	OccurredAt time.Time `gorm:"precision:6;not null"`
}

type Notification struct {
	ID                 string     `gorm:"primaryKey;size:30"`
	RecipientID        string     `gorm:"size:30;not null;uniqueIndex:idx_notifications_recipient_alert,priority:1;index:idx_notifications_recipient_last_seen,priority:1"`
	AlertID            string     `gorm:"size:30;not null;uniqueIndex:idx_notifications_recipient_alert,priority:2"`
	LatestOccurrenceID string     `gorm:"size:30;not null"`
	Kind               string     `gorm:"size:40;not null"`
	Severity           string     `gorm:"size:10;not null"`
	DetailCode         string     `gorm:"size:40;not null"`
	OccurrenceCount    int        `gorm:"not null"`
	Read               bool       `gorm:"not null;index:idx_notifications_recipient_read,priority:2"`
	FirstSeenAt        time.Time  `gorm:"precision:6;not null"`
	LastSeenAt         time.Time  `gorm:"precision:6;not null;index:idx_notifications_recipient_last_seen,priority:2"`
	ReadAt             *time.Time `gorm:"precision:6"`
}

type NotificationSetting struct {
	UserID        string    `gorm:"primaryKey;size:30"`
	ExternalEmail string    `gorm:"size:254;not null"`
	EmailHigh     bool      `gorm:"not null"`
	EmailMedium   bool      `gorm:"not null"`
	ETag          string    `gorm:"size:30;not null"`
	UpdatedAt     time.Time `gorm:"precision:6;not null"`
}

type NotificationDeliveryIntent struct {
	ID             string     `gorm:"primaryKey;size:30"`
	OccurrenceID   string     `gorm:"size:30;not null;uniqueIndex:idx_notification_delivery_occurrence_recipient,priority:1"`
	RecipientID    string     `gorm:"size:30;not null;uniqueIndex:idx_notification_delivery_occurrence_recipient,priority:2"`
	RecipientEmail string     `gorm:"size:254;not null"`
	Kind           string     `gorm:"size:40;not null"`
	Severity       string     `gorm:"size:10;not null"`
	DetailCode     string     `gorm:"size:40;not null"`
	SMTPETag       string     `gorm:"size:30;not null"`
	Status         string     `gorm:"size:12;not null;index:idx_notification_delivery_due,priority:1"`
	Attempts       int        `gorm:"not null"`
	NextAttemptAt  time.Time  `gorm:"precision:6;not null;index:idx_notification_delivery_due,priority:2"`
	LeaseToken     string     `gorm:"size:30;not null"`
	LeaseUntil     *time.Time `gorm:"precision:6"`
	ResultCode     string     `gorm:"size:40;not null"`
	CreatedAt      time.Time  `gorm:"precision:6;not null"`
	UpdatedAt      time.Time  `gorm:"precision:6;not null"`
	CompletedAt    *time.Time `gorm:"precision:6"`
}

func (OperationalAlert) TableName() string           { return "operational_alerts" }
func (OperationalAlertOccurrence) TableName() string { return "operational_alert_occurrences" }
func (Notification) TableName() string               { return "notifications" }
func (NotificationSetting) TableName() string        { return "notification_settings" }
func (NotificationDeliveryIntent) TableName() string { return "notification_delivery_intents" }
