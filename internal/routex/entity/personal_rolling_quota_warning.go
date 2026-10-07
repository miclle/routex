package entity

import "time"

// PersonalRollingQuotaWarningState is a sampled episode, not a crossing history.
// General policy edits do not reset it. Unknown journal coverage never rearms it.
type PersonalRollingQuotaWarningState struct {
	OwnerID           string    `gorm:"primaryKey;size:30"`
	ResourceCreatedAt time.Time `gorm:"primaryKey;precision:6"`
	WindowKind        string    `gorm:"primaryKey;size:2;check:ck_prqw_state_window,(OCTET_LENGTH(window_kind) = 2 AND ASCII(SUBSTRING(window_kind,1,1)) = 53 AND ASCII(SUBSTRING(window_kind,2,1)) = 104) OR (OCTET_LENGTH(window_kind) = 2 AND ASCII(SUBSTRING(window_kind,1,1)) = 55 AND ASCII(SUBSTRING(window_kind,2,1)) = 100)"`
	Cap               int64     `gorm:"not null;check:ck_prqw_state_cap,cap >= 0"`
	LastResetReview   string    `gorm:"size:64;not null"`
	EpisodeID         string    `gorm:"size:30;not null"`
	NearSent          bool      `gorm:"not null"`
	CriticalSent      bool      `gorm:"not null"`
	LastAsOf          time.Time `gorm:"precision:6;not null"`
}

// PersonalRollingQuotaWarningObservation preserves fully covered settled facts.
// WindowEnd equals the sample time; holds are never converted into settled use.
type PersonalRollingQuotaWarningObservation struct {
	ID                  string    `gorm:"primaryKey;size:30"`
	OwnerID             string    `gorm:"size:30;not null;check:ck_prqw_owner,CHAR_LENGTH(owner_id) BETWEEN 1 AND 30"`
	ResourceCreatedAt   time.Time `gorm:"precision:6;not null"`
	WindowKind          string    `gorm:"size:2;not null;check:ck_prqw_window,(OCTET_LENGTH(window_kind) = 2 AND ASCII(SUBSTRING(window_kind,1,1)) = 53 AND ASCII(SUBSTRING(window_kind,2,1)) = 104) OR (OCTET_LENGTH(window_kind) = 2 AND ASCII(SUBSTRING(window_kind,1,1)) = 55 AND ASCII(SUBSTRING(window_kind,2,1)) = 100)"`
	EpisodeID           string    `gorm:"size:30;not null;uniqueIndex:uq_prqw_episode_level,priority:1"`
	PolicyRevision      string    `gorm:"size:64;not null"`
	WindowStart         time.Time `gorm:"precision:6;not null"`
	WindowEnd           time.Time `gorm:"precision:6;not null"`
	AsOf                time.Time `gorm:"precision:6;not null;check:ck_prqw_window_time,window_end = as_of AND window_start < window_end AND coverage_start <= as_of AND resource_created_at <= as_of"`
	CoverageStart       time.Time `gorm:"precision:6;not null"`
	TimeZone            string    `gorm:"size:100;not null"`
	Limit               int64     `gorm:"column:limit_value;not null;check:ck_prqw_amount,limit_value > 0 AND limit_value <= 9223372036854775807 AND settled_value >= 0 AND settled_value <= 9223372036854775807"`
	Settled             int64     `gorm:"column:settled_value;not null"`
	Level               string    `gorm:"size:10;not null;uniqueIndex:uq_prqw_episode_level,priority:2;check:ck_prqw_level,(OCTET_LENGTH(level) = 4 AND ASCII(SUBSTRING(level,1,1)) = 110 AND ASCII(SUBSTRING(level,2,1)) = 101 AND ASCII(SUBSTRING(level,3,1)) = 97 AND ASCII(SUBSTRING(level,4,1)) = 114 AND threshold = 80) OR (OCTET_LENGTH(level) = 8 AND ASCII(SUBSTRING(level,1,1)) = 99 AND ASCII(SUBSTRING(level,2,1)) = 114 AND ASCII(SUBSTRING(level,3,1)) = 105 AND ASCII(SUBSTRING(level,4,1)) = 116 AND ASCII(SUBSTRING(level,5,1)) = 105 AND ASCII(SUBSTRING(level,6,1)) = 99 AND ASCII(SUBSTRING(level,7,1)) = 97 AND ASCII(SUBSTRING(level,8,1)) = 108 AND threshold = 90)"`
	Threshold           int       `gorm:"not null"`
	ThresholdGeneration string    `gorm:"size:40;not null;check:ck_prqw_generation,OCTET_LENGTH(threshold_generation) = 25 AND ASCII(SUBSTRING(threshold_generation,1,1)) = 112 AND ASCII(SUBSTRING(threshold_generation,2,1)) = 101 AND ASCII(SUBSTRING(threshold_generation,3,1)) = 114 AND ASCII(SUBSTRING(threshold_generation,4,1)) = 115 AND ASCII(SUBSTRING(threshold_generation,5,1)) = 111 AND ASCII(SUBSTRING(threshold_generation,6,1)) = 110 AND ASCII(SUBSTRING(threshold_generation,7,1)) = 97 AND ASCII(SUBSTRING(threshold_generation,8,1)) = 108 AND ASCII(SUBSTRING(threshold_generation,9,1)) = 45 AND ASCII(SUBSTRING(threshold_generation,10,1)) = 114 AND ASCII(SUBSTRING(threshold_generation,11,1)) = 111 AND ASCII(SUBSTRING(threshold_generation,12,1)) = 108 AND ASCII(SUBSTRING(threshold_generation,13,1)) = 108 AND ASCII(SUBSTRING(threshold_generation,14,1)) = 105 AND ASCII(SUBSTRING(threshold_generation,15,1)) = 110 AND ASCII(SUBSTRING(threshold_generation,16,1)) = 103 AND ASCII(SUBSTRING(threshold_generation,17,1)) = 45 AND ASCII(SUBSTRING(threshold_generation,18,1)) = 56 AND ASCII(SUBSTRING(threshold_generation,19,1)) = 48 AND ASCII(SUBSTRING(threshold_generation,20,1)) = 45 AND ASCII(SUBSTRING(threshold_generation,21,1)) = 57 AND ASCII(SUBSTRING(threshold_generation,22,1)) = 48 AND ASCII(SUBSTRING(threshold_generation,23,1)) = 45 AND ASCII(SUBSTRING(threshold_generation,24,1)) = 118 AND ASCII(SUBSTRING(threshold_generation,25,1)) = 49"`
}

type PersonalRollingQuotaWarningInbox struct {
	ID            string     `gorm:"primaryKey;size:30;index:idx_prqw_recipient_created,priority:3"`
	ObservationID string     `gorm:"size:30;not null;uniqueIndex:uq_prqw_inbox,priority:1"`
	RecipientID   string     `gorm:"size:30;not null;uniqueIndex:uq_prqw_inbox,priority:2;index:idx_prqw_recipient_created,priority:1"`
	ReadAt        *time.Time `gorm:"precision:6"`
	CreatedAt     time.Time  `gorm:"precision:6;not null;index:idx_prqw_recipient_created,priority:2"`
}

func (PersonalRollingQuotaWarningState) TableName() string {
	return "personal_rolling_quota_warning_states"
}
func (PersonalRollingQuotaWarningObservation) TableName() string {
	return "personal_rolling_quota_warning_observations"
}
func (PersonalRollingQuotaWarningInbox) TableName() string {
	return "personal_rolling_quota_warning_inboxes"
}
