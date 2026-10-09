package entity

import "time"

// Published commands retain independent immutable public receipts. KnownNoEffect
// is private control-flow evidence persisted only when this command stopped
// before the permanent physical remote claim, never inferred from an HTTP code,
// deadline or absence. A new intent also requires complete joined current proof.
type CredentialPublishedCleanup struct {
	PhysicalObject    string     `gorm:"size:64;not null" json:"-"`
	KnownNoEffect     bool       `gorm:"not null;check:ck_credential_published_no_effect_v88,known_no_effect = false OR state = 'failed'" json:"-"`
	CreationRequestID string     `gorm:"size:36;not null;index:idx_credential_published_creation" json:"-"`
	RequestID         string     `gorm:"primaryKey;size:36" json:"-"`
	ActorID           string     `gorm:"size:30;not null" json:"-"`
	ActorBirth        time.Time  `gorm:"precision:6;not null" json:"-"`
	IntegrationID     string     `gorm:"size:30;not null;index:idx_credential_published_integration" json:"-"`
	IntegrationBirth  time.Time  `gorm:"precision:6;not null" json:"-"`
	RevisionID        string     `gorm:"size:30;not null" json:"-"`
	OperationProof    string     `gorm:"size:64;not null" json:"-"`
	ReviewedETag      string     `gorm:"column:reviewed_etag;size:129;not null" json:"-"`
	Reason            string     `gorm:"type:text;not null" json:"-"`
	State             string     `gorm:"size:16;not null;check:ck_credential_published_cleanup_state_v88,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 110 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 117 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 110) OR (OCTET_LENGTH(state) = 6 AND ASCII(SUBSTRING(state,1,1)) = 102 AND ASCII(SUBSTRING(state,2,1)) = 97 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 108 AND ASCII(SUBSTRING(state,5,1)) = 101 AND ASCII(SUBSTRING(state,6,1)) = 100) OR (OCTET_LENGTH(state) = 12 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 99 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 108 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100 AND ASCII(SUBSTRING(state,10,1)) = 103 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 100)" json:"-"`
	OwnershipJSON     string     `gorm:"type:text;not null" json:"-"`
	CleanupJSON       string     `gorm:"type:text;not null" json:"-"`
	RootEpoch         uint64     `gorm:"not null" json:"-"`
	StartedAt         time.Time  `gorm:"precision:6;not null" json:"-"`
	Deadline          time.Time  `gorm:"precision:6;not null" json:"-"`
	FinishedAt        *time.Time `gorm:"precision:6" json:"-"`
}
