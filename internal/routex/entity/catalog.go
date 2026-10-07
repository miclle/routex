package entity

import "time"

const ProtocolOpenAIChat = "openai_chat"

// Provider is a stable upstream supplier; transport and secrets belong to connections.
type Provider struct {
	ID        string `gorm:"primaryKey;size:30"`
	Name      string `gorm:"size:100;not null"`
	CreatedAt time.Time
}

type ProviderConnection struct {
	Adapter    string  `gorm:"size:24;not null;default:native;check:ck_connection_adapter_v79,(OCTET_LENGTH(adapter) = 6 AND ASCII(SUBSTRING(adapter,1,1)) = 110 AND ASCII(SUBSTRING(adapter,2,1)) = 97 AND ASCII(SUBSTRING(adapter,3,1)) = 116 AND ASCII(SUBSTRING(adapter,4,1)) = 105 AND ASCII(SUBSTRING(adapter,5,1)) = 118 AND ASCII(SUBSTRING(adapter,6,1)) = 101 AND api_version IS NULL) OR (OCTET_LENGTH(adapter) = 20 AND ASCII(SUBSTRING(adapter,1,1)) = 97 AND ASCII(SUBSTRING(adapter,2,1)) = 122 AND ASCII(SUBSTRING(adapter,3,1)) = 117 AND ASCII(SUBSTRING(adapter,4,1)) = 114 AND ASCII(SUBSTRING(adapter,5,1)) = 101 AND ASCII(SUBSTRING(adapter,6,1)) = 95 AND ASCII(SUBSTRING(adapter,7,1)) = 111 AND ASCII(SUBSTRING(adapter,8,1)) = 112 AND ASCII(SUBSTRING(adapter,9,1)) = 101 AND ASCII(SUBSTRING(adapter,10,1)) = 110 AND ASCII(SUBSTRING(adapter,11,1)) = 97 AND ASCII(SUBSTRING(adapter,12,1)) = 105 AND ASCII(SUBSTRING(adapter,13,1)) = 95 AND ASCII(SUBSTRING(adapter,14,1)) = 99 AND ASCII(SUBSTRING(adapter,15,1)) = 108 AND ASCII(SUBSTRING(adapter,16,1)) = 97 AND ASCII(SUBSTRING(adapter,17,1)) = 115 AND ASCII(SUBSTRING(adapter,18,1)) = 115 AND ASCII(SUBSTRING(adapter,19,1)) = 105 AND ASCII(SUBSTRING(adapter,20,1)) = 99 AND OCTET_LENGTH(protocol) = 11 AND ASCII(SUBSTRING(protocol,1,1)) = 111 AND ASCII(SUBSTRING(protocol,2,1)) = 112 AND ASCII(SUBSTRING(protocol,3,1)) = 101 AND ASCII(SUBSTRING(protocol,4,1)) = 110 AND ASCII(SUBSTRING(protocol,5,1)) = 97 AND ASCII(SUBSTRING(protocol,6,1)) = 105 AND ASCII(SUBSTRING(protocol,7,1)) = 95 AND ASCII(SUBSTRING(protocol,8,1)) = 99 AND ASCII(SUBSTRING(protocol,9,1)) = 104 AND ASCII(SUBSTRING(protocol,10,1)) = 97 AND ASCII(SUBSTRING(protocol,11,1)) = 116 AND api_version IS NOT NULL AND OCTET_LENGTH(api_version) IN (10,18))"`
	APIVersion *string `gorm:"size:18"`
	Enabled    bool    `gorm:"not null;default:true"`
	EgressMode string  `gorm:"size:12;not null;default:default"`
	EgressID   *string `gorm:"size:30"`
	ETag       string  `gorm:"size:30;not null;default:0"`
	ID         string  `gorm:"primaryKey;size:30"`
	ProviderID string  `gorm:"size:30;not null"`
	Name       string  `gorm:"size:100;not null"`
	BaseURL    string  `gorm:"size:2048;not null"`
	Protocol   string  `gorm:"size:30;not null"`
	CreatedAt  time.Time
}

type ProviderCredential struct {
	CoverageRevision     int64                     `gorm:"not null;default:0;check:ck_credential_coverage_revision_v79,coverage_revision >= 0" json:"-"`
	CoverageReviewETag   string                    `gorm:"column:coverage_review_etag;size:129;not null;default:''" json:"-"`
	CoverageIntentSHA256 string                    `gorm:"size:64;not null;default:''" json:"-"`
	StorageSource        string                    `gorm:"size:16;not null;default:inline;check:ck_provider_credentials_storage_source,(OCTET_LENGTH(storage_source) = 6 AND ASCII(SUBSTRING(storage_source,1,1)) = 105 AND ASCII(SUBSTRING(storage_source,2,1)) = 110 AND ASCII(SUBSTRING(storage_source,3,1)) = 108 AND ASCII(SUBSTRING(storage_source,4,1)) = 105 AND ASCII(SUBSTRING(storage_source,5,1)) = 110 AND ASCII(SUBSTRING(storage_source,6,1)) = 101) OR (OCTET_LENGTH(storage_source) = 5 AND ASCII(SUBSTRING(storage_source,1,1)) = 118 AND ASCII(SUBSTRING(storage_source,2,1)) = 97 AND ASCII(SUBSTRING(storage_source,3,1)) = 117 AND ASCII(SUBSTRING(storage_source,4,1)) = 108 AND ASCII(SUBSTRING(storage_source,5,1)) = 116)" json:"-"`
	VaultReference       *CredentialVaultReference `gorm:"-" json:"-"`
	ReplacesCredentialID *string                   `gorm:"size:30;index:idx_credentials_replaces"`
	ID                   string                    `gorm:"primaryKey;size:30"`
	ConnectionID         string                    `gorm:"size:30;not null"`
	Name                 string                    `gorm:"size:100;not null"`
	Ciphertext           string                    `gorm:"type:text;not null"`
	Priority             int                       `gorm:"not null"`
	Enabled              bool                      `gorm:"not null"`
	VerificationStatus   string                    `gorm:"size:20;not null"`
	VerifiedAt           *time.Time
	CreatedAt            time.Time
}

// CredentialReplacementReceipt retains non-secret creation identity even when
// the source or result is deleted. Historical IDs deliberately have no live FKs.
type CredentialReplacementReceipt struct {
	RequestID          string    `gorm:"primaryKey;size:36"`
	ActorID            string    `gorm:"size:30;not null"`
	SourceCredentialID string    `gorm:"size:30;not null;index:idx_credential_replacement_source"`
	ConnectionID       string    `gorm:"size:30;not null"`
	ResultCredentialID string    `gorm:"size:30;not null;uniqueIndex:idx_credential_replacement_result"`
	RequestHash        string    `gorm:"size:64;not null"`
	CreatedAt          time.Time `gorm:"precision:6;not null"`
}

type ProviderModel struct {
	ID                 string `gorm:"primaryKey;size:30"`
	ConnectionID       string `gorm:"size:30;not null"`
	UpstreamName       string `gorm:"size:255;not null"`
	Disabled           bool   `gorm:"not null;default:false"`
	SupportsImageInput bool   `gorm:"not null;default:false"`
	SupportsPDFInput   bool   `gorm:"not null;default:false"`
	ETag               string `gorm:"size:64;not null;default:0"`
	CreatedAt          time.Time
}

// CredentialModelAccess records actual discovery results for one credential.
type CredentialModelAccess struct {
	CredentialID    string `gorm:"primaryKey;size:30"`
	ProviderModelID string `gorm:"primaryKey;size:30"`
}

type Model struct {
	ID        string `gorm:"primaryKey;size:30"`
	Status    string `gorm:"size:20;not null"`
	CreatedAt time.Time
}

// CurrentModelID is nullable and unique: at most one current name per model.
// Retired names remain reserved globally after their compatibility deadline.
type ModelName struct {
	Name           string  `gorm:"primaryKey;size:128"`
	ModelID        string  `gorm:"size:30;not null"`
	CurrentModelID *string `gorm:"size:30;uniqueIndex"`
	ExpiresAt      *time.Time
	CreatedAt      time.Time
}

type ModelProviderBinding struct {
	ID              string `gorm:"primaryKey;size:30"`
	ModelID         string `gorm:"size:30;not null"`
	ProviderModelID string `gorm:"size:30;not null"`
	Weight          int    `gorm:"not null"`
	CreatedAt       time.Time
}

type UserModelGrant struct {
	SourceRequestID *string `gorm:"column:source_request_id;size:30;check:ck_user_model_grant_source,source_request_id IS NULL OR (CHAR_LENGTH(source_request_id) >= 1 AND CHAR_LENGTH(source_request_id) <= 30)"`
	UserID          string  `gorm:"primaryKey;size:30"`
	ModelID         string  `gorm:"primaryKey;size:30"`
	CreatedAt       time.Time
}
