package entity

import "time"

// QuotaNotificationObservation is an immutable observation of current monthly
// settled exhaustion. It does not claim a historical threshold crossing.
type QuotaNotificationObservation struct {
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

// QuotaNotificationInbox freezes eligible recipients when the observation is
// saved. Historical replay neither adds new managers nor resets read state.
type QuotaNotificationInbox struct {
	ID            string     `gorm:"primaryKey;size:30;index:idx_quota_inbox_recipient_created,priority:3"`
	ObservationID string     `gorm:"size:30;not null;uniqueIndex:uq_quota_inbox_observation_recipient,priority:1"`
	RecipientID   string     `gorm:"size:30;not null;uniqueIndex:uq_quota_inbox_observation_recipient,priority:2;index:idx_quota_inbox_recipient_created,priority:1"`
	ReadAt        *time.Time `gorm:"precision:6"`
	CreatedAt     time.Time  `gorm:"precision:6;not null;index:idx_quota_inbox_recipient_created,priority:2"`
}

func (QuotaNotificationObservation) TableName() string { return "quota_notification_observations" }
func (QuotaNotificationInbox) TableName() string       { return "quota_notification_inboxes" }
