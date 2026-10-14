package entity

import "time"

// RuntimeArchiveCleanup retains the exact committed administrator intent. Its
// receipt describes storage deletion, never configuration or fleet application.
type RuntimeArchiveCleanup struct {
	RequestID      string    `gorm:"primaryKey;size:36;check:ck_runtime_archive_cleanup_identity,OCTET_LENGTH(request_id) = 36 AND OCTET_LENGTH(actor_id) = 30 AND OCTET_LENGTH(review_etag) = 64" json:"-"`
	ActorID        string    `gorm:"size:30;not null;index" json:"-"`
	ActorCreatedAt time.Time `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	ReviewETag     string    `gorm:"column:review_etag;size:64;not null" json:"-"`
	Reason         string    `gorm:"type:text;not null;check:ck_runtime_archive_cleanup_payload,OCTET_LENGTH(reason) BETWEEN 1 AND 4000 AND OCTET_LENGTH(archive_ids_json) BETWEEN 34 AND 661" json:"-"`
	ArchiveIDsJSON string    `gorm:"type:text;not null" json:"-"`
	DeletedCount   int64     `gorm:"not null;check:ck_runtime_archive_cleanup_bounds,deleted_count BETWEEN 1 AND 20 AND deleted_bytes > 0" json:"-"`
	DeletedBytes   int64     `gorm:"not null" json:"-"`
	CompletedAt    time.Time `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_cleanup_time,actor_created_at > '0001-01-01 00:00:00' AND completed_at >= actor_created_at" json:"-"`
}

func (RuntimeArchiveCleanup) TableName() string { return "runtime_archive_cleanups" }
