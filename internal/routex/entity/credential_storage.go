package entity

import "time"

// CredentialStoragePolicy selects only future credential writes. Existing refs
// retain their immutable descriptor and auth revisions after a policy change.
type CredentialStoragePolicy struct {
	ID               int        `gorm:"primaryKey;autoIncrement:false;check:ck_credential_store_singleton,id = 1"`
	Mode             string     `gorm:"size:16;not null;default:inline;check:ck_credential_storage_policy_mode,((OCTET_LENGTH(mode) = 6 AND ASCII(SUBSTRING(mode,1,1)) = 105 AND ASCII(SUBSTRING(mode,2,1)) = 110 AND ASCII(SUBSTRING(mode,3,1)) = 108 AND ASCII(SUBSTRING(mode,4,1)) = 105 AND ASCII(SUBSTRING(mode,5,1)) = 110 AND ASCII(SUBSTRING(mode,6,1)) = 101) OR (OCTET_LENGTH(mode) = 5 AND ASCII(SUBSTRING(mode,1,1)) = 118 AND ASCII(SUBSTRING(mode,2,1)) = 97 AND ASCII(SUBSTRING(mode,3,1)) = 117 AND ASCII(SUBSTRING(mode,4,1)) = 108 AND ASCII(SUBSTRING(mode,5,1)) = 116)) AND ((mode = 'inline' AND integration_id IS NULL AND integration_birth IS NULL AND revision_id IS NULL) OR (mode = 'vault' AND integration_id IS NOT NULL AND integration_birth IS NOT NULL AND revision_id IS NOT NULL))"`
	IntegrationID    *string    `gorm:"size:30"`
	IntegrationBirth *time.Time `gorm:"precision:6"`
	RevisionID       *string    `gorm:"size:30"`
	Generation       string     `gorm:"size:30;not null"`
}

// CredentialStorageOperation is the durable claim before a remote effect. It
// retains no submitted secret or secret-value digest. Unfinished plans remain
// explicitly unresolved; this phase never schedules or authorizes destruction.
type CredentialStorageOperation struct {
	StorageSource        string     `gorm:"size:16;not null;check:ck_credential_storage_operation_source,(OCTET_LENGTH(storage_source) = 6 AND ASCII(SUBSTRING(storage_source,1,1)) = 105 AND ASCII(SUBSTRING(storage_source,2,1)) = 110 AND ASCII(SUBSTRING(storage_source,3,1)) = 108 AND ASCII(SUBSTRING(storage_source,4,1)) = 105 AND ASCII(SUBSTRING(storage_source,5,1)) = 110 AND ASCII(SUBSTRING(storage_source,6,1)) = 101) OR (OCTET_LENGTH(storage_source) = 5 AND ASCII(SUBSTRING(storage_source,1,1)) = 118 AND ASCII(SUBSTRING(storage_source,2,1)) = 97 AND ASCII(SUBSTRING(storage_source,3,1)) = 117 AND ASCII(SUBSTRING(storage_source,4,1)) = 108 AND ASCII(SUBSTRING(storage_source,5,1)) = 116)"`
	RequestID            string     `gorm:"primaryKey;size:36"`
	ActorID              string     `gorm:"size:30;not null"`
	ActorBirth           time.Time  `gorm:"precision:6;not null"`
	Kind                 string     `gorm:"size:16;not null;check:ck_credential_storage_operation_kind,(OCTET_LENGTH(kind) = 8 AND ASCII(SUBSTRING(kind,1,1)) = 112 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 111 AND ASCII(SUBSTRING(kind,4,1)) = 118 AND ASCII(SUBSTRING(kind,5,1)) = 105 AND ASCII(SUBSTRING(kind,6,1)) = 100 AND ASCII(SUBSTRING(kind,7,1)) = 101 AND ASCII(SUBSTRING(kind,8,1)) = 114) OR (OCTET_LENGTH(kind) = 10 AND ASCII(SUBSTRING(kind,1,1)) = 99 AND ASCII(SUBSTRING(kind,2,1)) = 111 AND ASCII(SUBSTRING(kind,3,1)) = 110 AND ASCII(SUBSTRING(kind,4,1)) = 110 AND ASCII(SUBSTRING(kind,5,1)) = 101 AND ASCII(SUBSTRING(kind,6,1)) = 99 AND ASCII(SUBSTRING(kind,7,1)) = 116 AND ASCII(SUBSTRING(kind,8,1)) = 105 AND ASCII(SUBSTRING(kind,9,1)) = 111 AND ASCII(SUBSTRING(kind,10,1)) = 110) OR (OCTET_LENGTH(kind) = 10 AND ASCII(SUBSTRING(kind,1,1)) = 99 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 101 AND ASCII(SUBSTRING(kind,4,1)) = 100 AND ASCII(SUBSTRING(kind,5,1)) = 101 AND ASCII(SUBSTRING(kind,6,1)) = 110 AND ASCII(SUBSTRING(kind,7,1)) = 116 AND ASCII(SUBSTRING(kind,8,1)) = 105 AND ASCII(SUBSTRING(kind,9,1)) = 97 AND ASCII(SUBSTRING(kind,10,1)) = 108) OR (OCTET_LENGTH(kind) = 11 AND ASCII(SUBSTRING(kind,1,1)) = 114 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 112 AND ASCII(SUBSTRING(kind,4,1)) = 108 AND ASCII(SUBSTRING(kind,5,1)) = 97 AND ASCII(SUBSTRING(kind,6,1)) = 99 AND ASCII(SUBSTRING(kind,7,1)) = 101 AND ASCII(SUBSTRING(kind,8,1)) = 109 AND ASCII(SUBSTRING(kind,9,1)) = 101 AND ASCII(SUBSTRING(kind,10,1)) = 110 AND ASCII(SUBSTRING(kind,11,1)) = 116)"`
	TargetID             string     `gorm:"size:30;not null"`
	TargetBirth          time.Time  `gorm:"precision:6;not null"`
	PolicyGeneration     string     `gorm:"size:30;not null"`
	IntentJSON           string     `gorm:"type:text;not null" json:"-"`
	CredentialID         string     `gorm:"size:30;not null;uniqueIndex:idx_credential_storage_result"`
	CredentialBirth      time.Time  `gorm:"precision:6;not null"`
	ProviderID           string     `gorm:"size:30;not null"`
	ConnectionID         string     `gorm:"size:30;not null"`
	ProviderBirth        time.Time  `gorm:"precision:6;not null"`
	ConnectionBirth      time.Time  `gorm:"precision:6;not null"`
	ReferenceID          string     `gorm:"size:32;not null;uniqueIndex:idx_credential_storage_reference"`
	ExpectedMarkerSHA256 string     `gorm:"size:64;not null" json:"-"`
	DescriptorSHA256     string     `gorm:"size:64;not null" json:"-"`
	IntegrationID        string     `gorm:"size:30;not null"`
	IntegrationBirth     *time.Time `gorm:"precision:6"`
	RevisionID           string     `gorm:"size:30;not null"`
	WriterGeneration     string     `gorm:"size:30;not null"`
	ReaderGeneration     string     `gorm:"size:30;not null"`
	RootEpoch            uint64     `gorm:"not null"`
	State                string     `gorm:"size:16;not null;check:ck_credential_storage_operation_state,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 119 AND ASCII(SUBSTRING(state,2,1)) = 114 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 116 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 13 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 119 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 105 AND ASCII(SUBSTRING(state,5,1)) = 116 AND ASCII(SUBSTRING(state,6,1)) = 105 AND ASCII(SUBSTRING(state,7,1)) = 110 AND ASCII(SUBSTRING(state,8,1)) = 103 AND ASCII(SUBSTRING(state,9,1)) = 95 AND ASCII(SUBSTRING(state,10,1)) = 114 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 97 AND ASCII(SUBSTRING(state,13,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 117 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 107 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 111 AND ASCII(SUBSTRING(state,6,1)) = 119 AND ASCII(SUBSTRING(state,7,1)) = 110) OR (OCTET_LENGTH(state) = 5 AND ASCII(SUBSTRING(state,1,1)) = 111 AND ASCII(SUBSTRING(state,2,1)) = 119 AND ASCII(SUBSTRING(state,3,1)) = 110 AND ASCII(SUBSTRING(state,4,1)) = 101 AND ASCII(SUBSTRING(state,5,1)) = 100) OR (OCTET_LENGTH(state) = 6 AND ASCII(SUBSTRING(state,1,1)) = 111 AND ASCII(SUBSTRING(state,2,1)) = 114 AND ASCII(SUBSTRING(state,3,1)) = 112 AND ASCII(SUBSTRING(state,4,1)) = 104 AND ASCII(SUBSTRING(state,5,1)) = 97 AND ASCII(SUBSTRING(state,6,1)) = 110) OR (OCTET_LENGTH(state) = 9 AND ASCII(SUBSTRING(state,1,1)) = 99 AND ASCII(SUBSTRING(state,2,1)) = 111 AND ASCII(SUBSTRING(state,3,1)) = 109 AND ASCII(SUBSTRING(state,4,1)) = 109 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 116 AND ASCII(SUBSTRING(state,7,1)) = 116 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100)"`
	Claim                string     `gorm:"size:30;not null" json:"-"`
	ClaimedUntil         time.Time  `gorm:"precision:6;not null"`
	WriteJSON            string     `gorm:"type:text;not null" json:"-"`
	ReadJSON             string     `gorm:"type:text;not null" json:"-"`
	CreatedAt            time.Time  `gorm:"precision:6;not null"`
}

// CredentialVaultReference survives credential deletion so unresolved external
// objects never become falsely absent. It is not a cleanup authorization.
type CredentialVaultReference struct {
	ReaderMethod         string    `gorm:"-" json:"-"`
	ReaderCiphertext     string    `gorm:"-" json:"-"`
	SourceContext        string    `gorm:"-" json:"-"`
	CredentialID         string    `gorm:"primaryKey;size:30"`
	CredentialBirth      time.Time `gorm:"precision:6;not null"`
	ReferenceID          string    `gorm:"size:32;not null;uniqueIndex:idx_credential_vault_reference"`
	ExpectedMarkerSHA256 string    `gorm:"size:64;not null" json:"-"`
	DescriptorSHA256     string    `gorm:"size:64;not null" json:"-"`
	IntegrationID        string    `gorm:"size:30;not null"`
	IntegrationBirth     time.Time `gorm:"precision:6;not null"`
	RevisionID           string    `gorm:"size:30;not null"`
	ReaderGeneration     string    `gorm:"size:30;not null"`
}
