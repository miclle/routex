package database

import (
	"gorm.io/gorm"
	"time"
)

type providerCredentialCleanupV80 struct {
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

func (providerCredentialCleanupV80) TableName() string { return "provider_credential_cleanups" }

// V80 freezes the one-command disposition separately from original V77 stage history.
func providerCredentialCleanupMigration(db *gorm.DB) error {
	return migrateTables(db, &providerCredentialCleanupV80{}, &providerCredentialCreationUseV80{})
}

type providerCredentialCreationUseV80 struct {
	CreationRequestID string `gorm:"primaryKey;size:36" json:"-"`
	ProcessID         string `gorm:"size:30;not null" json:"-"`
	ProcessGeneration string `gorm:"size:64;not null" json:"-"`
	Exposed           bool   `gorm:"not null" json:"-"`
}

func (providerCredentialCreationUseV80) TableName() string {
	return "provider_credential_creation_uses"
}
