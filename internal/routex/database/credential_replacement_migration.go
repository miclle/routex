package database

import (
	"time"

	"gorm.io/gorm"
)

// Version31 keeps lineage and creation receipts as historical IDs, without
// cascading or restrictive references to deletable Credential records.
type credentialLineageV31 struct {
	ReplacesCredentialID *string `gorm:"size:30;index:idx_credentials_replaces"`
}

func (credentialLineageV31) TableName() string { return "provider_credentials" }

type credentialReplacementReceiptV31 struct {
	RequestID          string    `gorm:"primaryKey;size:36"`
	ActorID            string    `gorm:"size:30;not null"`
	SourceCredentialID string    `gorm:"size:30;not null;index:idx_credential_replacement_source"`
	ConnectionID       string    `gorm:"size:30;not null"`
	ResultCredentialID string    `gorm:"size:30;not null;uniqueIndex:idx_credential_replacement_result"`
	RequestHash        string    `gorm:"size:64;not null"`
	CreatedAt          time.Time `gorm:"precision:6;not null"`
}

func (credentialReplacementReceiptV31) TableName() string {
	return "credential_replacement_receipts"
}

func credentialReplacementMigration(db *gorm.DB) error {
	lineage := &credentialLineageV31{}
	if !db.Migrator().HasColumn(lineage, "ReplacesCredentialID") {
		if err := db.Migrator().AddColumn(lineage, "ReplacesCredentialID"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasIndex(lineage, "idx_credentials_replaces") {
		if err := db.Migrator().CreateIndex(lineage, "idx_credentials_replaces"); err != nil {
			return err
		}
	}
	return migrateTables(db, &credentialReplacementReceiptV31{})
}
