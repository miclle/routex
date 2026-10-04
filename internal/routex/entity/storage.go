package entity

import "time"

const (
	StorageOwnerUser    = "user"
	StorageOwnerProject = "project"
	StorageOwnerTeam    = "team"
)

type StorageSetting struct {
	ID               int     `gorm:"primaryKey;autoIncrement:false"`
	Enabled          bool    `gorm:"not null"`
	ActiveRevisionID *string `gorm:"size:30"`
	ETag             string  `gorm:"size:30;not null"`
	UpdatedAt        time.Time
}

func (StorageSetting) TableName() string { return "storage_settings" }

type StorageRevision struct {
	ID               string `gorm:"primaryKey;size:30"`
	Endpoint         string `gorm:"size:2048;not null"`
	Region           string `gorm:"size:64;not null"`
	Bucket           string `gorm:"size:63;not null"`
	Prefix           string `gorm:"size:512;not null"`
	SecretGeneration string `gorm:"size:30;not null" json:"-"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
	VerifiedAt       *time.Time
	CreatedBy        string `gorm:"size:30;not null"`
	CreatedAt        time.Time
}

func (StorageRevision) TableName() string { return "storage_revisions" }

type StorageObject struct {
	ID                  string     `gorm:"primaryKey;size:30"`
	OwnerKind           string     `gorm:"size:16;not null;default:user;index:idx_storage_objects_scope,priority:1;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project','team')"`
	OwnerID             string     `gorm:"size:30;not null;index:idx_storage_objects_owner;index:idx_storage_objects_scope,priority:2"`
	CreatorUserID       *string    `gorm:"size:30;check:ck_storage_objects_team_creator,(owner_kind = 'team' AND creator_user_id IS NOT NULL AND CHAR_LENGTH(creator_user_id) > 0 AND creator_membership_id IS NOT NULL AND CHAR_LENGTH(creator_membership_id) > 0 AND expires_at IS NOT NULL) OR (owner_kind IN ('user','project') AND creator_user_id IS NULL AND creator_membership_id IS NULL AND expires_at IS NULL)"`
	CreatorMembershipID *string    `gorm:"size:30"`
	ExpiresAt           *time.Time `gorm:"precision:6"`
	RevisionID          string     `gorm:"size:30;not null"`
	Purpose             string     `gorm:"size:16;not null"`
	State               string     `gorm:"size:20;not null;index:idx_storage_objects_state"`
	Name                string     `gorm:"size:200;not null"`
	MIME                string     `gorm:"size:100;not null"`
	Size                int64      `gorm:"not null"`
	SHA256              string     `gorm:"size:64;not null"`
	UploadConfirmed     bool       `gorm:"not null"`
	VersionID           string     `gorm:"size:1024;not null" json:"-"`
	CleanupAttempts     int        `gorm:"not null"`
	CleanupCode         string     `gorm:"size:40;not null"`
	NextCleanupAt       time.Time  `gorm:"not null;index:idx_storage_objects_cleanup"`
	LeaseToken          string     `gorm:"size:30;not null"`
	LeaseUntil          *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (StorageObject) TableName() string { return "storage_objects" }
