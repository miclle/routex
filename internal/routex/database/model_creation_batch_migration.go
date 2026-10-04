package database

import (
	"gorm.io/gorm"
	"time"
)

const modelCreationBatchMigrationVersion = 50

// Version 50 owns one bounded immutable receipt table; no live foreign keys.
type modelCreationBatchReceiptV50 struct {
	RequestID    string    `gorm:"primaryKey;size:36;check:ck_model_creation_batch_intent,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null;index:idx_model_creation_batch_actor"`
	ConnectionID string    `gorm:"size:30;not null;index:idx_model_creation_batch_connection"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}

func (modelCreationBatchReceiptV50) TableName() string { return "model_creation_batch_receipts" }
func modelCreationBatchMigration(db *gorm.DB) error {
	frozen := &modelCreationBatchReceiptV50{}
	if db.Migrator().HasTable(frozen) {
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(frozen); err != nil {
			return err
		}
		for _, field := range statement.Schema.Fields {
			if !db.Migrator().HasColumn(frozen, field.DBName) {
				if err := db.Migrator().AddColumn(frozen, field.Name); err != nil {
					return err
				}
			}
		}
	}
	return migrateTables(db, frozen)
}
