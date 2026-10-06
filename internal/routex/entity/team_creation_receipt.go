package entity

import "time"

// TeamCreationReceipt preserves an intent's original identity and application
// boundary without live relationships that could erase historical receipts.
type TeamCreationReceipt struct {
	ModelSnapshotVersion int       `gorm:"not null;default:0"`
	ModelCount           int       `gorm:"not null;default:0;check:ck_team_creation_models,(model_snapshot_version = 0 AND model_count = 0 AND model_digest IS NULL) OR (model_snapshot_version = 1 AND model_count BETWEEN 1 AND 1000 AND model_digest IS NOT NULL AND CHAR_LENGTH(model_digest) = 64)"`
	ModelDigest          *string   `gorm:"size:64"`
	CreationID           string    `gorm:"primaryKey;size:36;check:ck_team_creation_intent,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID              string    `gorm:"size:30;not null;index:idx_team_creation_actor"`
	ActorCreatedAt       time.Time `gorm:"precision:6;not null"`
	TeamID               string    `gorm:"size:30;not null;uniqueIndex:uq_team_creation_team"`
	TeamCreatedAt        time.Time `gorm:"precision:6;not null"`
	RequestHash          string    `gorm:"size:64;not null"`
	ReviewETag           string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON         string    `gorm:"size:131072;not null;check:ck_team_creation_snapshot,OCTET_LENGTH(snapshot_json) <= 131072"`
	CreatedAt            time.Time `gorm:"precision:6;not null"`
}

// TeamCreationReceiptModel retains the original Model incarnation without a live foreign key.
type TeamCreationReceiptModel struct {
	CreationID     string    `gorm:"primaryKey;size:36;check:ck_team_creation_model_identity,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(model_id) BETWEEN 1 AND 30"`
	ModelID        string    `gorm:"primaryKey;size:30"`
	ModelCreatedAt time.Time `gorm:"precision:6;not null"`
}
