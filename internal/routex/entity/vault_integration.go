package entity

import "time"

// Vault configuration is independent from the active internal Provider store.
type VaultIntegration struct {
	ID            string    `gorm:"primaryKey;size:30"`
	Name          string    `gorm:"size:100;not null"`
	RevisionID    string    `gorm:"size:30;not null"`
	ActiveProbeID *string   `gorm:"size:32"`
	CreatedAt     time.Time `gorm:"precision:6;not null"`
	UpdatedAt     time.Time `gorm:"precision:6;not null"`
}
type VaultCatalogue struct {
	ID         int    `gorm:"primaryKey;autoIncrement:false;check:ck_vault_catalogue_singleton,id = 1"`
	Generation string `gorm:"size:64;not null"`
}
type VaultRevision struct {
	ID               string    `gorm:"primaryKey;size:30"`
	IntegrationID    string    `gorm:"size:30;not null;index:idx_vault_revision_integration"`
	IntegrationBirth time.Time `gorm:"precision:6;not null"`
	Name             string    `gorm:"size:100;not null"`
	Endpoint         string    `gorm:"size:2048;not null"`
	Namespace        string    `gorm:"size:256;not null"`
	Mount            string    `gorm:"size:128;not null"`
	Prefix           string    `gorm:"size:256;not null"`
	DataField        string    `gorm:"size:64;not null"`
	CreatedAt        time.Time `gorm:"precision:6;not null"`
}

// Separate tables make each auth material an exact root-rotation inventory row.
type VaultWriterAuth struct {
	ID               string `gorm:"primaryKey;size:30"`
	SecretGeneration string `gorm:"size:30;not null"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
}
type VaultReaderAuth struct {
	ID               string `gorm:"primaryKey;size:30"`
	SecretGeneration string `gorm:"size:30;not null"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
}
type VaultConfigReceipt struct {
	RequestID        string    `gorm:"primaryKey;size:36"`
	ActorID          string    `gorm:"size:30;not null"`
	ActorBirth       time.Time `gorm:"precision:6;not null"`
	IntegrationID    string    `gorm:"size:30;not null"`
	IntegrationBirth time.Time `gorm:"precision:6;not null"`
	RevisionID       string    `gorm:"size:30;not null"`
	ReviewETag       string    `gorm:"size:129;not null" json:"-"`
	IntentJSON       string    `gorm:"type:text;not null" json:"-"`
	Changed          bool      `gorm:"not null"`
	CreatedAt        time.Time `gorm:"precision:6;not null"`
}
type VaultProbe struct {
	ID               string     `gorm:"primaryKey;size:32"`
	RequestID        string     `gorm:"size:36;not null;uniqueIndex:idx_vault_probe_write_request"`
	IntegrationID    string     `gorm:"size:30;not null;index:idx_vault_probe_integration"`
	IntegrationBirth time.Time  `gorm:"precision:6;not null"`
	RevisionID       string     `gorm:"size:30;not null"`
	ActorID          string     `gorm:"size:30;not null"`
	ActorBirth       time.Time  `gorm:"precision:6;not null"`
	ExpectedSHA256   string     `gorm:"size:64;not null" json:"-"`
	DescriptorSHA256 string     `gorm:"size:64;not null" json:"-"`
	State            string     `gorm:"size:24;not null;check:ck_vault_probe_state,(OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 112 AND ASCII(SUBSTRING(state,2,1)) = 108 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 110 AND ASCII(SUBSTRING(state,5,1)) = 110 AND ASCII(SUBSTRING(state,6,1)) = 101 AND ASCII(SUBSTRING(state,7,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 119 AND ASCII(SUBSTRING(state,2,1)) = 114 AND ASCII(SUBSTRING(state,3,1)) = 105 AND ASCII(SUBSTRING(state,4,1)) = 116 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 13 AND ASCII(SUBSTRING(state,1,1)) = 97 AND ASCII(SUBSTRING(state,2,1)) = 119 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 105 AND ASCII(SUBSTRING(state,5,1)) = 116 AND ASCII(SUBSTRING(state,6,1)) = 105 AND ASCII(SUBSTRING(state,7,1)) = 110 AND ASCII(SUBSTRING(state,8,1)) = 103 AND ASCII(SUBSTRING(state,9,1)) = 95 AND ASCII(SUBSTRING(state,10,1)) = 114 AND ASCII(SUBSTRING(state,11,1)) = 101 AND ASCII(SUBSTRING(state,12,1)) = 97 AND ASCII(SUBSTRING(state,13,1)) = 100) OR (OCTET_LENGTH(state) = 7 AND ASCII(SUBSTRING(state,1,1)) = 114 AND ASCII(SUBSTRING(state,2,1)) = 101 AND ASCII(SUBSTRING(state,3,1)) = 97 AND ASCII(SUBSTRING(state,4,1)) = 100 AND ASCII(SUBSTRING(state,5,1)) = 105 AND ASCII(SUBSTRING(state,6,1)) = 110 AND ASCII(SUBSTRING(state,7,1)) = 103) OR (OCTET_LENGTH(state) = 15 AND ASCII(SUBSTRING(state,1,1)) = 99 AND ASCII(SUBSTRING(state,2,1)) = 108 AND ASCII(SUBSTRING(state,3,1)) = 101 AND ASCII(SUBSTRING(state,4,1)) = 97 AND ASCII(SUBSTRING(state,5,1)) = 110 AND ASCII(SUBSTRING(state,6,1)) = 117 AND ASCII(SUBSTRING(state,7,1)) = 112 AND ASCII(SUBSTRING(state,8,1)) = 95 AND ASCII(SUBSTRING(state,9,1)) = 112 AND ASCII(SUBSTRING(state,10,1)) = 101 AND ASCII(SUBSTRING(state,11,1)) = 110 AND ASCII(SUBSTRING(state,12,1)) = 100 AND ASCII(SUBSTRING(state,13,1)) = 105 AND ASCII(SUBSTRING(state,14,1)) = 110 AND ASCII(SUBSTRING(state,15,1)) = 103) OR (OCTET_LENGTH(state) = 9 AND ASCII(SUBSTRING(state,1,1)) = 99 AND ASCII(SUBSTRING(state,2,1)) = 111 AND ASCII(SUBSTRING(state,3,1)) = 109 AND ASCII(SUBSTRING(state,4,1)) = 112 AND ASCII(SUBSTRING(state,5,1)) = 108 AND ASCII(SUBSTRING(state,6,1)) = 101 AND ASCII(SUBSTRING(state,7,1)) = 116 AND ASCII(SUBSTRING(state,8,1)) = 101 AND ASCII(SUBSTRING(state,9,1)) = 100) OR (OCTET_LENGTH(state) = 11 AND ASCII(SUBSTRING(state,1,1)) = 105 AND ASCII(SUBSTRING(state,2,1)) = 110 AND ASCII(SUBSTRING(state,3,1)) = 116 AND ASCII(SUBSTRING(state,4,1)) = 101 AND ASCII(SUBSTRING(state,5,1)) = 114 AND ASCII(SUBSTRING(state,6,1)) = 114 AND ASCII(SUBSTRING(state,7,1)) = 117 AND ASCII(SUBSTRING(state,8,1)) = 112 AND ASCII(SUBSTRING(state,9,1)) = 116 AND ASCII(SUBSTRING(state,10,1)) = 101 AND ASCII(SUBSTRING(state,11,1)) = 100)"`
	Generation       string     `gorm:"size:64;not null"`
	Version          int64      `gorm:"not null;check:ck_vault_probe_version,version = 0 OR version = 1"`
	WriteJSON        string     `gorm:"type:text;not null" json:"-"`
	ReadJSON         string     `gorm:"type:text;not null" json:"-"`
	CleanupJSON      string     `gorm:"type:text;not null" json:"-"`
	CreatedAt        time.Time  `gorm:"precision:6;not null"`
	FinishedAt       *time.Time `gorm:"precision:6"`
}

// One receipt claims the entire finite command BEFORE any remote request.
type VaultProbeCommand struct {
	RequestID     string     `gorm:"primaryKey;size:36"`
	ProbeID       string     `gorm:"size:32;not null;index:idx_vault_command_probe"`
	ActorID       string     `gorm:"size:30;not null"`
	ActorBirth    time.Time  `gorm:"precision:6;not null"`
	Kind          string     `gorm:"size:16;not null;check:ck_vault_command_kind,(OCTET_LENGTH(kind) = 5 AND ASCII(SUBSTRING(kind,1,1)) = 119 AND ASCII(SUBSTRING(kind,2,1)) = 114 AND ASCII(SUBSTRING(kind,3,1)) = 105 AND ASCII(SUBSTRING(kind,4,1)) = 116 AND ASCII(SUBSTRING(kind,5,1)) = 101) OR (OCTET_LENGTH(kind) = 4 AND ASCII(SUBSTRING(kind,1,1)) = 114 AND ASCII(SUBSTRING(kind,2,1)) = 101 AND ASCII(SUBSTRING(kind,3,1)) = 97 AND ASCII(SUBSTRING(kind,4,1)) = 100) OR (OCTET_LENGTH(kind) = 7 AND ASCII(SUBSTRING(kind,1,1)) = 99 AND ASCII(SUBSTRING(kind,2,1)) = 108 AND ASCII(SUBSTRING(kind,3,1)) = 101 AND ASCII(SUBSTRING(kind,4,1)) = 97 AND ASCII(SUBSTRING(kind,5,1)) = 110 AND ASCII(SUBSTRING(kind,6,1)) = 117 AND ASCII(SUBSTRING(kind,7,1)) = 112)"`
	ReviewETag    string     `gorm:"size:129;not null" json:"-"`
	Reason        string     `gorm:"type:text;not null" json:"-"`
	Claim         string     `gorm:"size:30;not null" json:"-"`
	ExpiresAt     time.Time  `gorm:"precision:6;not null"`
	ResultJSON    string     `gorm:"type:text;not null" json:"-"`
	OwnershipJSON string     `gorm:"type:text;not null" json:"-"`
	CreatedAt     time.Time  `gorm:"precision:6;not null"`
	FinishedAt    *time.Time `gorm:"precision:6"`
}

func (VaultWriterAuth) TableName() string { return "vault_writer_auth" }
func (VaultReaderAuth) TableName() string { return "vault_reader_auth" }
