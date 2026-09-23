package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type resourceLimitV14 struct {
	ScopeKind    string `gorm:"primaryKey;size:20"`
	ScopeID      string `gorm:"primaryKey;size:30"`
	ETag         string `gorm:"size:64;not null"`
	PreviousETag string `gorm:"size:64;not null"`
	ActorID      string `gorm:"size:30;not null"`
	Reason       string `gorm:"size:2000;not null"`
	RPM          *int64
	Concurrency  *int64
	IPMode       string `gorm:"size:20;not null"`
	IPRangesJSON string `gorm:"type:text;not null"`
	UpdatedAt    time.Time
}

func (resourceLimitV14) TableName() string { return "resource_limits" }

type limitInstallationV14 struct {
	ID          int    `gorm:"primaryKey;autoIncrement:false"`
	JournalID   string `gorm:"size:64;not null"`
	Initialized bool   `gorm:"not null"`
}

func (limitInstallationV14) TableName() string { return "limit_installation" }
func resourceLimitMigration(db *gorm.DB) error {
	if err := migrateTables(db, &resourceLimitV14{}, &limitInstallationV14{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&limitInstallationV14{ID: 1}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"limits.users.write", "projects.limits.write"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&limitPermissionV14{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}

type limitPermissionV14 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (limitPermissionV14) TableName() string { return "role_permissions" }
