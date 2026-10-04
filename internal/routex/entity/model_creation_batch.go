package entity

import "time"

// ModelCreationBatchReceipt preserves historical creation identity without live relations.
type ModelCreationBatchReceipt struct {
	RequestID    string    `gorm:"primaryKey;size:36;check:ck_model_creation_batch_intent,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID      string    `gorm:"size:30;not null;index:idx_model_creation_batch_actor"`
	ConnectionID string    `gorm:"size:30;not null;index:idx_model_creation_batch_connection"`
	RequestHash  string    `gorm:"size:64;not null"`
	ReviewETag   string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON string    `gorm:"type:text;not null"`
	CreatedAt    time.Time `gorm:"precision:6;not null"`
}
