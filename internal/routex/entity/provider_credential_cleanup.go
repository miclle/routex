package entity

import "time"

// ProviderCredentialCleanup is a permanent business-recovery disposition and
// one durable remote command. It contains no authentication material. A failed
// or unknown command remains retained; neither absence nor expiry authorizes a
// second destroy. Original credential creation observations remain immutable.
type ProviderCredentialCleanup struct {
	CreationRequestID string     `gorm:"primaryKey;size:36" json:"-"`
	RequestID         string     `gorm:"size:36;not null;uniqueIndex:idx_provider_cleanup_command" json:"-"`
	ActorID           string     `gorm:"size:30;not null" json:"-"`
	ActorBirth        time.Time  `gorm:"precision:6;not null" json:"-"`
	IntegrationID     string     `gorm:"size:30;not null;index:idx_provider_cleanup_integration" json:"-"`
	IntegrationBirth  time.Time  `gorm:"precision:6;not null" json:"-"`
	RevisionID        string     `gorm:"size:30;not null" json:"-"`
	OperationProof    string     `gorm:"size:64;not null" json:"-"`
	ReviewedETag      string     `gorm:"column:reviewed_etag;size:129;not null" json:"-"`
	Reason            string     `gorm:"type:text;not null" json:"-"`
	State             string     `gorm:"size:16;not null;check:ck_provider_cleanup_state_v80,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 110 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 117 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 110) OR (OCTET_LENGTH(state) = 6 AND ASCII(SUBSTRING(state,1,1)) = 102 AND ASCII(SUBSTRING(state,2,1)) = 97 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 108 AND ASCII(SUBSTRING(state,5,1)) = 101 AND ASCII(SUBSTRING(state,6,1)) = 100) OR (OCTET_LENGTH(state) = 12 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 99 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 108 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100 AND ASCII(SUBSTRING(state,10,1)) = 103 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 100)" json:"-"`
	OwnershipJSON     string     `gorm:"type:text;not null" json:"-"`
	CleanupJSON       string     `gorm:"type:text;not null" json:"-"`
	RootEpoch         uint64     `gorm:"not null" json:"-"`
	StartedAt         time.Time  `gorm:"precision:6;not null" json:"-"`
	Deadline          time.Time  `gorm:"precision:6;not null" json:"-"`
	FinishedAt        *time.Time `gorm:"precision:6" json:"-"`
}

// ProviderCredentialCreationUse never disappears on instance retirement. It
// records whether another/unknown process could have recovered this exact plan.
// Legacy plans are never backfilled as locally owned.
type ProviderCredentialCreationUse struct {
	CreationRequestID string `gorm:"primaryKey;size:36" json:"-"`
	ProcessID         string `gorm:"size:30;not null" json:"-"`
	ProcessGeneration string `gorm:"size:64;not null" json:"-"`
	Exposed           bool   `gorm:"not null" json:"-"`
}
