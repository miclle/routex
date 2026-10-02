package database

import (
	"time"

	"gorm.io/gorm"
)

// Version 34 records retirement intent without live foreign keys. A source may
// be re-enabled and freshly reviewed later; only the request UUID is unique.
type credentialRetirementReceiptV34 struct {
	RequestID               string    `gorm:"primaryKey;size:36"`
	ActorID                 string    `gorm:"size:30;not null"`
	SourceCredentialID      string    `gorm:"size:30;not null;check:ck_credential_retirement_distinct,source_credential_id <> replacement_credential_id"`
	ReplacementCredentialID string    `gorm:"size:30;not null"`
	ConnectionID            string    `gorm:"size:30;not null"`
	RequestHash             string    `gorm:"size:64;not null"`
	ReadinessETag           string    `gorm:"size:64;not null"`
	SourceETag              string    `gorm:"size:64;not null"`
	ReplacementETag         string    `gorm:"size:64;not null"`
	PreDisableSnapshotID    string    `gorm:"size:30;not null"`
	EvidenceAttemptID       string    `gorm:"size:64;not null"`
	CommittedAt             time.Time `gorm:"precision:6;not null"`
}

func (credentialRetirementReceiptV34) TableName() string {
	return "credential_retirement_receipts"
}

func credentialRetirementMigration(db *gorm.DB) error {
	return migrateTables(db, &credentialRetirementReceiptV34{})
}
