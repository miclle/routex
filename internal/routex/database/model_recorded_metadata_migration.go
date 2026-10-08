package database

import (
	"time"

	"gorm.io/gorm"
)

// Frozen V86 adds a nullable recorded configuration timestamp. Historical
// creation dates cannot establish when configuration last changed.
type modelRecordedMetadataV86 struct {
	ID              string     `gorm:"primaryKey;size:30"`
	ConfigUpdatedAt *time.Time `gorm:"column:config_updated_at;precision:6;autoCreateTime:false;autoUpdateTime:false"`
}

func (modelRecordedMetadataV86) TableName() string { return "models" }

func modelRecordedMetadataMigration(db *gorm.DB) error {
	frozen := &modelRecordedMetadataV86{}
	if db.Migrator().HasColumn(frozen, "ConfigUpdatedAt") {
		return nil
	}
	return db.Migrator().AddColumn(frozen, "ConfigUpdatedAt")
}
