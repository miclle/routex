package entity

import "time"

// RuntimeArchiveRetention serializes archive admission and actual envelope-byte accounting.
// Caps pause new archives; existing legacy data and root rewrap are never deleted.
type RuntimeArchiveRetention struct {
	ID             int   `gorm:"primaryKey;autoIncrement:false;check:ck_runtime_archive_retention_id,id = 1" json:"-"`
	Initialized    bool  `gorm:"not null" json:"-"`
	RetainedCount  int64 `gorm:"not null;check:ck_runtime_archive_retention_bounds,retained_count >= 0 AND encrypted_bytes >= 0" json:"-"`
	EncryptedBytes int64 `gorm:"not null" json:"-"`
}

func (RuntimeArchiveRetention) TableName() string { return "runtime_archive_retention" }

// A digest has one canonical retained target. Older duplicate versions stay immutable.
type RuntimeArchiveContent struct {
	ContentDigest string                       `gorm:"primaryKey;size:64;check:ck_runtime_archive_content_digest,OCTET_LENGTH(content_digest) = 64" json:"-"`
	ArchiveID     string                       `gorm:"size:30;not null;index" json:"-"`
	Archive       *RuntimeConfigurationArchive `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (RuntimeArchiveContent) TableName() string { return "runtime_archive_contents" }

// Only the first retained coherent local capture is recorded, not installation
// or an actor attribution. Legacy raw archives have no inferred provenance.
type RuntimeArchiveCapture struct {
	ArchiveID         string                       `gorm:"primaryKey;size:30" json:"-"`
	SnapshotID        string                       `gorm:"size:30;not null;check:ck_runtime_archive_capture_identity,OCTET_LENGTH(snapshot_id) = 30 AND OCTET_LENGTH(archive_id) = 30" json:"-"`
	RoutesPublishedAt time.Time                    `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	CapturedAt        time.Time                    `gorm:"precision:6;not null;autoCreateTime:false;check:ck_runtime_archive_capture_time,captured_at >= routes_published_at AND routes_published_at > '0001-01-01 00:00:00'" json:"-"`
	Archive           *RuntimeConfigurationArchive `gorm:"foreignKey:ArchiveID;references:ID;constraint:OnUpdate:RESTRICT,OnDelete:RESTRICT" json:"-"`
}

func (RuntimeArchiveCapture) TableName() string { return "runtime_archive_captures" }
