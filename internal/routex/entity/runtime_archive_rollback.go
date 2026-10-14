package entity

import "time"

// RuntimeArchiveRollback retains original committed routing intent. Its archive
// and publication references are RESTRICT foreign keys owned by frozen V116.
type RuntimeArchiveRollback struct {
	RequestID        string    `gorm:"primaryKey;size:36;check:ck_archive_rollback_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(actor_id) = 30 AND OCTET_LENGTH(archive_id) = 30 AND OCTET_LENGTH(review_etag) = 64 AND OCTET_LENGTH(input_digest) = 64 AND OCTET_LENGTH(desired_digest) = 64" json:"-"`
	ActorID          string    `gorm:"size:30;not null;index" json:"-"`
	ActorCreatedAt   time.Time `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ArchiveID        string    `gorm:"size:30;not null;index" json:"-"`
	ArchiveCreatedAt time.Time `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ReviewETag       string    `gorm:"column:review_etag;size:64;not null" json:"-"`
	InputDigest      string    `gorm:"size:64;not null" json:"-"`
	DesiredDigest    string    `gorm:"size:64;not null" json:"-"`
	Reason           string    `gorm:"type:text;not null;check:ck_archive_rollback_reason,OCTET_LENGTH(reason) BETWEEN 1 AND 4000" json:"-"`
	Changed          bool      `gorm:"not null" json:"-"`
	AppliedAt        time.Time `gorm:"precision:6;not null;autoCreateTime:false;check:ck_archive_rollback_time,actor_created_at > '0001-01-01 00:00:00' AND archive_created_at > '0001-01-01 00:00:00' AND applied_at >= actor_created_at AND applied_at >= archive_created_at" json:"-"`
}

func (RuntimeArchiveRollback) TableName() string { return "runtime_archive_rollbacks" }
