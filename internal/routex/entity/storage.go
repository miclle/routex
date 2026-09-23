package entity

import "time"

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
	ID              string    `gorm:"primaryKey;size:30"`
	OwnerID         string    `gorm:"size:30;not null;index:idx_storage_objects_owner"`
	RevisionID      string    `gorm:"size:30;not null"`
	Purpose         string    `gorm:"size:16;not null"`
	State           string    `gorm:"size:20;not null;index:idx_storage_objects_state"`
	Name            string    `gorm:"size:200;not null"`
	MIME            string    `gorm:"size:100;not null"`
	Size            int64     `gorm:"not null"`
	SHA256          string    `gorm:"size:64;not null"`
	UploadConfirmed bool      `gorm:"not null"`
	VersionID       string    `gorm:"size:1024;not null" json:"-"`
	CleanupAttempts int       `gorm:"not null"`
	CleanupCode     string    `gorm:"size:40;not null"`
	NextCleanupAt   time.Time `gorm:"not null;index:idx_storage_objects_cleanup"`
	LeaseToken      string    `gorm:"size:30;not null"`
	LeaseUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (StorageObject) TableName() string { return "storage_objects" }
