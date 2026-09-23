package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type storageSettingV21 struct {
	ID               int                 `gorm:"primaryKey;autoIncrement:false"`
	Enabled          bool                `gorm:"not null"`
	ActiveRevision   *storageRevisionV21 `gorm:"foreignKey:ActiveRevisionID;references:ID;constraint:fk_storage_active_revision,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	ActiveRevisionID *string             `gorm:"size:30"`
	ETag             string              `gorm:"size:30;not null"`
	UpdatedAt        time.Time
}

func (storageSettingV21) TableName() string { return "storage_settings" }

type storageRevisionV21 struct {
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

func (storageRevisionV21) TableName() string { return "storage_revisions" }

type storageObjectV21 struct {
	ID              string              `gorm:"primaryKey;size:30"`
	OwnerID         string              `gorm:"size:30;not null;index:idx_storage_objects_owner"`
	Revision        *storageRevisionV21 `gorm:"foreignKey:RevisionID;references:ID;constraint:fk_storage_object_revision,OnUpdate:RESTRICT,OnDelete:RESTRICT"`
	RevisionID      string              `gorm:"size:30;not null"`
	Purpose         string              `gorm:"size:16;not null"`
	State           string              `gorm:"size:20;not null;index:idx_storage_objects_state"`
	Name            string              `gorm:"size:200;not null"`
	MIME            string              `gorm:"size:100;not null"`
	Size            int64               `gorm:"not null"`
	SHA256          string              `gorm:"size:64;not null"`
	UploadConfirmed bool                `gorm:"not null"`
	VersionID       string              `gorm:"size:1024;not null" json:"-"`
	CleanupAttempts int                 `gorm:"not null"`
	CleanupCode     string              `gorm:"size:40;not null"`
	NextCleanupAt   time.Time           `gorm:"not null;index:idx_storage_objects_cleanup"`
	LeaseToken      string              `gorm:"size:30;not null"`
	LeaseUntil      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (storageObjectV21) TableName() string { return "storage_objects" }

type storagePermissionV21 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (storagePermissionV21) TableName() string { return "role_permissions" }

// Frozen version 21 records storage revisions, owned objects and cleanup intent.
func storageMigration(db *gorm.DB) error {
	if err := migrateTables(db, &storageRevisionV21{}, &storageSettingV21{}, &storageObjectV21{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&storageSettingV21{ID: 1, ETag: "0", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"storage.read", "storage.write", "storage.test"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&storagePermissionV21{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
