package database

import (
	"time"

	"gorm.io/gorm"
)

// Version 45 records immutable creation receipts with no live foreign keys.
type projectCreationReceiptV45 struct {
	CreationID   string    `gorm:"primaryKey;size:36;check:ck_project_creation_intent,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null;index:idx_project_creation_actor"`
	ProjectID    string    `gorm:"size:30;not null;uniqueIndex:uq_project_creation_project"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}

func (projectCreationReceiptV45) TableName() string { return "project_creation_receipts" }
func projectCreationMigration(db *gorm.DB) error {
	return migrateTables(db, &projectCreationReceiptV45{})
}
