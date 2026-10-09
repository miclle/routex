package database

import (
	"time"

	"gorm.io/gorm"
)

// modelWeightVersionV90 retains an observed complete routing configuration only.
// It contains no credentials, authorization, prices, or publication receipt.
type modelWeightVersionV90 struct {
	ID                string     `gorm:"primaryKey;size:30"`
	ModelID           string     `gorm:"size:30;not null;uniqueIndex:idx_model_weight_sequence,priority:1"`
	ModelBirth        *time.Time `gorm:"precision:6"`
	Sequence          uint64     `gorm:"not null;uniqueIndex:idx_model_weight_sequence,priority:2;check:ck_model_weight_sequence,sequence > 0"`
	CapturedAt        time.Time  `gorm:"precision:6;not null"`
	Source            string     `gorm:"size:20;not null;check:ck_model_weight_source,source IN ('observed_baseline','legacy_editor','rollback')"`
	ParentVersionID   *string    `gorm:"size:30"`
	RollbackVersionID *string    `gorm:"size:30"`
	ActorID           string     `gorm:"size:30;not null"`
	Reason            *string    `gorm:"size:1024"`
	BindingCount      int        `gorm:"not null;check:ck_model_weight_count,binding_count >= 0 AND binding_count <= 1000"`
	ValidWeightSet    bool       `gorm:"not null"`
	Snapshot          []byte     `gorm:"size:524288;not null;check:ck_model_weight_snapshot,OCTET_LENGTH(snapshot) <= 524288"`
	SnapshotDigest    string     `gorm:"size:64;not null;check:ck_model_weight_digest,length(snapshot_digest) = 64"`
}

// modelWeightRollbackCommandV90 is immutable durable operation evidence. Current
// routing application is evaluated independently and is never stored as success.
type modelWeightRollbackCommandV90 struct {
	RequestID       string    `gorm:"primaryKey;size:36"`
	ActorID         string    `gorm:"size:30;not null"`
	ActorBirth      time.Time `gorm:"precision:6;not null"`
	ModelID         string    `gorm:"size:30;not null;index:idx_model_weight_command_model"`
	ModelBirth      time.Time `gorm:"precision:6;not null"`
	VersionID       string    `gorm:"size:30;not null"`
	SourceVersionID *string   `gorm:"size:30"`
	SavedVersionID  *string   `gorm:"size:30"`
	ReviewETag      string    `gorm:"size:64;not null"`
	InputDigest     string    `gorm:"size:64;not null"`
	DesiredDigest   string    `gorm:"size:64;not null"`
	Effect          string    `gorm:"size:10;not null;check:ck_model_weight_command_effect,effect IN ('changed','noop')"`
	Reason          string    `gorm:"size:1024;not null"`
	CreatedAt       time.Time `gorm:"precision:6;not null;autoCreateTime:false"`
}

func (modelWeightVersionV90) TableName() string         { return "model_weight_versions" }
func (modelWeightRollbackCommandV90) TableName() string { return "model_weight_rollback_commands" }

// V90 is frozen and additive; partial MySQL DDL reentry uses the same bounded
// Migrator table/index/constraint reconciliation as the other frozen versions.
func modelWeightHistoryMigration(db *gorm.DB) error {
	return migrateTables(db, &modelWeightVersionV90{}, &modelWeightRollbackCommandV90{})
}
