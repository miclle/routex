package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Frozen version 17 adds presentation settings and retained announcements.
type siteSettingV17 struct {
	ID              int    `gorm:"primaryKey;autoIncrement:false"`
	Name            string `gorm:"size:100;not null"`
	ServiceURL      string `gorm:"size:2048;not null"`
	LogoURL         string `gorm:"size:2048;not null"`
	Footer          string `gorm:"size:500;not null"`
	DefaultLanguage string `gorm:"size:8;not null"`
	ETag            string `gorm:"size:30;not null"`
	UpdatedAt       time.Time
}

func (siteSettingV17) TableName() string { return "site_settings" }

type announcementV17 struct {
	ID        string `gorm:"primaryKey;size:30"`
	Content   string `gorm:"type:text;not null"`
	Status    string `gorm:"size:12;not null;index:idx_announcements_status"`
	ETag      string `gorm:"size:30;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	ClosedAt  *time.Time
}

func (announcementV17) TableName() string { return "announcements" }

type sitePermissionV17 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (sitePermissionV17) TableName() string { return "role_permissions" }
func siteMigration(db *gorm.DB) error {
	if err := migrateTables(db, &siteSettingV17{}, &announcementV17{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&siteSettingV17{ID: 1, Name: "RouteX", DefaultLanguage: "en", ETag: "0", UpdatedAt: time.Now().UTC()}).Error; err != nil {
		return err
	}
	for _, permission := range []string{"site.write", "announcements.write"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&sitePermissionV17{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
