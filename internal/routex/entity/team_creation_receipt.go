package entity

import "time"

// TeamCreationReceipt preserves an intent's original identity and application
// boundary without live relationships that could erase historical receipts.
type TeamCreationReceipt struct {
	CreationID     string    `gorm:"primaryKey;size:36;check:ck_team_creation_intent,CHAR_LENGTH(creation_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	ActorID        string    `gorm:"size:30;not null;index:idx_team_creation_actor"`
	ActorCreatedAt time.Time `gorm:"precision:6;not null"`
	TeamID         string    `gorm:"size:30;not null;uniqueIndex:uq_team_creation_team"`
	TeamCreatedAt  time.Time `gorm:"precision:6;not null"`
	RequestHash    string    `gorm:"size:64;not null"`
	ReviewETag     string    `gorm:"column:review_etag;size:64;not null"`
	SnapshotJSON   string    `gorm:"size:131072;not null;check:ck_team_creation_snapshot,OCTET_LENGTH(snapshot_json) <= 131072"`
	CreatedAt      time.Time `gorm:"precision:6;not null"`
}
