package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type systemInstanceV27 struct {
	ID                string `gorm:"primaryKey;size:30"`
	LeaseToken        string `gorm:"size:30;not null"`
	HeartbeatRevision uint64 `gorm:"not null;check:chk_system_instances_revision,heartbeat_revision > 0"`
	Name              string `gorm:"size:100;not null"`
	Hostname          string `gorm:"size:253;not null"`
	Role              string `gorm:"size:20;not null;check:chk_system_instances_role,role = 'combined'"`
	Version           string `gorm:"size:100;not null"`
	Commit            string `gorm:"size:64;not null"`
	BuildTime         string `gorm:"size:64;not null"`
	GoVersion         string `gorm:"size:40;not null"`
	OS                string `gorm:"size:32;not null"`
	Arch              string `gorm:"size:32;not null"`
	CPUUsed           *int64
	CPUTotal          *int64
	CPUScope          string `gorm:"size:40;not null"`
	MemoryUsed        *int64
	MemoryTotal       *int64
	MemoryScope       string `gorm:"size:40;not null"`
	StorageUsed       *int64
	StorageTotal      *int64
	StorageScope      string     `gorm:"size:40;not null"`
	StartedAt         time.Time  `gorm:"precision:6;not null"`
	LastHeartbeatAt   time.Time  `gorm:"precision:6;not null;index:idx_system_instances_cleanup,priority:2"`
	LeaseExpiresAt    time.Time  `gorm:"precision:6;not null;index:idx_system_instances_lease"`
	StoppedAt         *time.Time `gorm:"precision:6"`
	RetiredAt         *time.Time `gorm:"precision:6;index:idx_system_instances_cleanup,priority:1"`
	RetiredBy         string     `gorm:"size:30;not null"`
}

func (systemInstanceV27) TableName() string { return "system_instances" }

type systemInstancePermissionV27 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (systemInstancePermissionV27) TableName() string { return "role_permissions" }

// Version 27 adds authoritative process-generation leases. The frozen model
// keeps interrupted MySQL DDL retryable through the shared migration helper.
func systemInstanceMigration(db *gorm.DB) error {
	if err := migrateTables(db, &systemInstanceV27{}); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&systemInstancePermissionV27{
		RoleID: "rol_admin", Permission: "system.write",
	}).Error
}
