package database

import (
	"gorm.io/gorm"
	"time"
)

// Version 35 keeps quota observations and recipient inboxes separate from
// operational alerts and contains no live identity or resource foreign keys.
type quotaNotificationObservationV35 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	ScopeKind         string    `gorm:"size:20;not null;uniqueIndex:uq_quota_notification_observation,priority:1;check:ck_quota_notification_scope,scope_kind IN ('user','project')"`
	ScopeID           string    `gorm:"size:30;not null;uniqueIndex:uq_quota_notification_observation,priority:2"`
	ScopeName         string    `gorm:"size:100;not null"`
	Dimension         string    `gorm:"size:20;not null;uniqueIndex:uq_quota_notification_observation,priority:3;check:ck_quota_notification_dimension,dimension IN ('tokens','money')"`
	PolicyRevision    string    `gorm:"size:64;not null;uniqueIndex:uq_quota_notification_observation,priority:5"`
	MonthStart        time.Time `gorm:"precision:6;not null;uniqueIndex:uq_quota_notification_observation,priority:4;check:ck_quota_notification_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end"`
	MonthEnd          time.Time `gorm:"precision:6;not null"`
	TimeZone          string    `gorm:"size:100;not null"`
	AsOf              time.Time `gorm:"precision:6;not null"`
	Limit             string    `gorm:"column:limit_value;size:40;not null"`
	Settled           string    `gorm:"column:settled_value;size:80;not null"`
	Currency          string    `gorm:"size:3;not null;uniqueIndex:uq_quota_notification_observation,priority:6;check:ck_quota_notification_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND currency <> '')"`
	CoverageStart     time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt time.Time `gorm:"precision:6;not null"`
}

type quotaNotificationInboxV35 struct {
	ID            string     `gorm:"primaryKey;size:30;index:idx_quota_inbox_recipient_created,priority:3"`
	ObservationID string     `gorm:"size:30;not null;uniqueIndex:uq_quota_inbox_observation_recipient,priority:1"`
	RecipientID   string     `gorm:"size:30;not null;uniqueIndex:uq_quota_inbox_observation_recipient,priority:2;index:idx_quota_inbox_recipient_created,priority:1"`
	ReadAt        *time.Time `gorm:"precision:6"`
	CreatedAt     time.Time  `gorm:"precision:6;not null;index:idx_quota_inbox_recipient_created,priority:2"`
}

func (quotaNotificationObservationV35) TableName() string { return "quota_notification_observations" }
func (quotaNotificationInboxV35) TableName() string       { return "quota_notification_inboxes" }
func quotaNotificationMigration(db *gorm.DB) error {
	return migrateTables(db, &quotaNotificationObservationV35{}, &quotaNotificationInboxV35{})
}
