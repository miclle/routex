package database

import (
	"time"

	"gorm.io/gorm"
)

// Version 32 records the exact dispatched credential and runtime publication on
// each immutable attempt. Historical blanks stay unknown, with no live catalog
// foreign keys or backfill from the logical call's final route.
type callCredentialAttributionV32 struct {
	ID           string    `gorm:"primaryKey;size:64;index:idx_attempts_credential_snapshot_time,priority:4"`
	CredentialID string    `gorm:"size:30;not null;default:'';index:idx_attempts_credential_snapshot_time,priority:1"`
	SnapshotID   string    `gorm:"size:30;not null;default:'';index:idx_attempts_credential_snapshot_time,priority:2"`
	CompletedAt  time.Time `gorm:"not null;index:idx_attempts_credential_snapshot_time,priority:3"`
}

func (callCredentialAttributionV32) TableName() string { return "call_attempts" }

func callCredentialAttributionMigration(db *gorm.DB) error {
	model := &callCredentialAttributionV32{}
	for _, field := range []string{"CredentialID", "SnapshotID"} {
		if !db.Migrator().HasColumn(model, field) {
			if err := db.Migrator().AddColumn(model, field); err != nil {
				return err
			}
		}
	}
	// The leading equality columns support a bounded lookup for an exact
	// credential/publication, followed by completion time and stable ID order.
	// This index establishes attribution only, never native completion evidence.
	if !db.Migrator().HasIndex(model, "idx_attempts_credential_snapshot_time") {
		return db.Migrator().CreateIndex(model, "idx_attempts_credential_snapshot_time")
	}
	return nil
}
