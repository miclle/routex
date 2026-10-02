package entity

import "time"

// CredentialRetirementReceipt retains committed historical intent independently
// of deletable credentials, actors and call evidence. It has no live relations.
type CredentialRetirementReceipt struct {
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
