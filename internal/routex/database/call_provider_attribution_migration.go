package database

import (
	"time"

	"gorm.io/gorm"
)

// Version 26 adds immutable provider attribution captured from the published
// route. Empty values keep legacy and no-route calls in an explicit unknown
// group without joining historical facts to mutable catalog labels.
type callProviderAttributionV26 struct {
	RequestID         string    `gorm:"column:request_id;primaryKey;size:64;index:idx_calls_provider_time,priority:3"`
	ProviderID        string    `gorm:"size:30;not null;default:'';index:idx_calls_provider_time,priority:1"`
	ProviderName      string    `gorm:"size:100;not null;default:''"`
	ConnectionName    string    `gorm:"size:100;not null;default:''"`
	UpstreamModelName string    `gorm:"size:255;not null;default:''"`
	StartedAt         time.Time `gorm:"precision:6;not null;index:idx_calls_provider_time,priority:2"`
}

func (callProviderAttributionV26) TableName() string { return "call_records" }

func callProviderAttributionMigration(db *gorm.DB) error {
	model := &callProviderAttributionV26{}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName"} {
		if !db.Migrator().HasColumn(model, field) {
			if err := db.Migrator().AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	if !db.Migrator().HasIndex(model, "idx_calls_provider_time") {
		return db.Migrator().CreateIndex(model, "idx_calls_provider_time")
	}
	return nil
}
