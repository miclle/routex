package entity

import "time"

// PersonalKeyQuotaWarningObservation records a current known settled level, not a crossing
// or an admission guarantee. Personal Key shared-account warnings retain their original facts.
type PersonalKeyQuotaWarningObservation struct {
	ID                  string    `gorm:"primaryKey;size:30"`
	RootKeyID           string    `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning,priority:1;check:ck_personal_key_quota_warning_root,CHAR_LENGTH(root_key_id) BETWEEN 1 AND 30"`
	RootKeyName         string    `gorm:"size:100;not null"`
	OwnerID             string    `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning,priority:9;check:ck_personal_key_quota_warning_owner,CHAR_LENGTH(owner_id) BETWEEN 1 AND 30"`
	OwnerCreatedAt      time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:10;check:ck_personal_key_quota_warning_owner_birth,owner_created_at <= resource_created_at AND resource_created_at <= as_of"`
	Dimension           string    `gorm:"size:20;not null;uniqueIndex:uq_personal_key_quota_warning,priority:2;check:ck_personal_key_quota_warning_dimension,dimension IN ('tokens','money')"`
	MonthStart          time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:3;check:ck_personal_key_quota_warning_calendar,month_end > month_start AND as_of >= month_start AND as_of < month_end AND coverage_start <= as_of AND resource_created_at <= as_of"`
	MonthEnd            time.Time `gorm:"precision:6;not null"`
	PolicyRevision      string    `gorm:"size:64;not null;uniqueIndex:uq_personal_key_quota_warning,priority:4"`
	Currency            string    `gorm:"size:3;not null;uniqueIndex:uq_personal_key_quota_warning,priority:5;check:ck_personal_key_quota_warning_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND CHAR_LENGTH(currency) = 3)"`
	Level               string    `gorm:"size:10;not null;uniqueIndex:uq_personal_key_quota_warning,priority:6;check:ck_personal_key_quota_warning_level,(level = 'near' AND threshold = 80) OR (level = 'critical' AND threshold = 90)"`
	Threshold           int       `gorm:"not null"`
	ThresholdGeneration string    `gorm:"size:40;not null;uniqueIndex:uq_personal_key_quota_warning,priority:7;check:ck_personal_key_quota_warning_generation,threshold_generation = 'personal-key-monthly-80-90-v1'"`
	TimeZone            string    `gorm:"size:100;not null"`
	AsOf                time.Time `gorm:"precision:6;not null"`
	Limit               string    `gorm:"column:limit_value;size:40;not null"`
	Settled             string    `gorm:"column:settled_value;size:80;not null"`
	CoverageStart       time.Time `gorm:"precision:6;not null"`
	ResourceCreatedAt   time.Time `gorm:"precision:6;not null;uniqueIndex:uq_personal_key_quota_warning,priority:8"`
}

type PersonalKeyQuotaWarningInbox struct {
	ID                 string     `gorm:"primaryKey;size:30;index:idx_personal_key_quota_warning_recipient_created,priority:3"`
	ObservationID      string     `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning_inbox,priority:1"`
	RecipientID        string     `gorm:"size:30;not null;uniqueIndex:uq_personal_key_quota_warning_inbox,priority:2;index:idx_personal_key_quota_warning_recipient_created,priority:1"`
	RecipientCreatedAt time.Time  `gorm:"precision:6;not null;check:ck_personal_key_quota_warning_recipient_birth,recipient_created_at <= created_at"`
	ReadAt             *time.Time `gorm:"precision:6"`
	CreatedAt          time.Time  `gorm:"precision:6;not null;index:idx_personal_key_quota_warning_recipient_created,priority:2"`
}

func (PersonalKeyQuotaWarningObservation) TableName() string {
	return "personal_key_quota_warning_observations"
}
func (PersonalKeyQuotaWarningInbox) TableName() string { return "personal_key_quota_warning_inboxes" }
