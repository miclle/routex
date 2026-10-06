package database

import (
	"time"

	"gorm.io/gorm"
)

// Frozen V66 is additive. V63 snapshots and legacy empty receipts remain unchanged.
type teamCreationModelsReceiptV66 struct {
	CreationID           string  `gorm:"primaryKey;size:36"`
	ModelSnapshotVersion int     `gorm:"not null;default:0"`
	ModelCount           int     `gorm:"not null;default:0;check:ck_team_creation_models,(model_snapshot_version = 0 AND model_count = 0 AND model_digest IS NULL) OR (model_snapshot_version = 1 AND model_count BETWEEN 1 AND 1000 AND model_digest IS NOT NULL AND CHAR_LENGTH(model_digest) = 64)"`
	ModelDigest          *string `gorm:"size:64"`
}

func (teamCreationModelsReceiptV66) TableName() string { return "team_creation_receipts" }

type teamCreationModelGrantV66 struct {
	TeamID                  string  `gorm:"primaryKey;size:30"`
	ModelID                 string  `gorm:"primaryKey;size:30"`
	SourceCreationReceiptID *string `gorm:"column:source_creation_receipt_id;size:36;check:ck_team_model_creation_source,source_creation_receipt_id IS NULL OR CHAR_LENGTH(source_creation_receipt_id) = 36"`
}

func (teamCreationModelGrantV66) TableName() string { return "team_model_grants" }

type teamCreationReceiptModelV66 struct {
	CreationID     string    `gorm:"primaryKey;size:36;check:ck_team_creation_model_identity,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(model_id) BETWEEN 1 AND 30"`
	ModelID        string    `gorm:"primaryKey;size:30"`
	ModelCreatedAt time.Time `gorm:"precision:6;not null"`
}

func (teamCreationReceiptModelV66) TableName() string { return "team_creation_receipt_models" }
func teamCreationModelsMigration(db *gorm.DB) error {
	parent := &teamCreationModelsReceiptV66{}
	for _, field := range []string{"ModelSnapshotVersion", "ModelCount", "ModelDigest"} {
		if !db.Migrator().HasColumn(parent, field) {
			if err := db.Migrator().AddColumn(parent, field); err != nil {
				return err
			}
		}
	}
	grant := &teamCreationModelGrantV66{}
	if !db.Migrator().HasColumn(grant, "SourceCreationReceiptID") {
		if err := db.Migrator().AddColumn(grant, "SourceCreationReceiptID"); err != nil {
			return err
		}
	}
	for _, item := range []struct {
		model any
		check string
	}{{parent, "ck_team_creation_models"}, {grant, "ck_team_model_creation_source"}} {
		if !db.Migrator().HasConstraint(item.model, item.check) {
			if err := db.Migrator().CreateConstraint(item.model, item.check); err != nil {
				return err
			}
		}
	}
	return migrateTables(db, &teamCreationReceiptModelV66{})
}
