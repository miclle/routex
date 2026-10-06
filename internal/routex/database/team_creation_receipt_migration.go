package database

import (
	"time"

	"gorm.io/gorm"
)

// Frozen V63 records additive creation receipts. The explicit GORM
// string size maps to MySQL MEDIUMTEXT and PostgreSQL varchar(131072); plain TEXT
// would truncate the supported 128 KiB receipt on MySQL. OCTET_LENGTH bounds
// encoded UTF-8 bytes consistently. No live foreign keys rewrite history.
type teamCreationReceiptV63 struct {
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

func (teamCreationReceiptV63) TableName() string { return "team_creation_receipts" }
func teamCreationReceiptMigration(db *gorm.DB) error {
	return migrateTables(db, &teamCreationReceiptV63{})
}
