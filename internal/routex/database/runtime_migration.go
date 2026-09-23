package database

import (
	"time"

	"gorm.io/gorm"
)

// runtimePublicationV7 freezes the schema introduced by migration 7.
type runtimePublicationV7 struct {
	ID         string    `gorm:"primaryKey;size:30"`
	SnapshotID string    `gorm:"size:30;not null"`
	Status     string    `gorm:"size:20;not null"`
	ErrorCode  string    `gorm:"size:40;not null"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (runtimePublicationV7) TableName() string { return "runtime_publications" }

func runtimeMigration(db *gorm.DB) error {
	return migrateTables(db, &runtimePublicationV7{})
}
