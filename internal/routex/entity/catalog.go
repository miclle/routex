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
	ReplacesCredentialID *string `gorm:"size:30;index:idx_credentials_replaces"`
	ID                   string  `gorm:"primaryKey;size:30"`
	ConnectionID         string  `gorm:"size:30;not null"`
	Name                 string  `gorm:"size:100;not null"`
	Ciphertext           string  `gorm:"type:text;not null"`
	Priority             int     `gorm:"not null"`
	Enabled              bool    `gorm:"not null"`
	VerificationStatus   string  `gorm:"size:20;not null"`
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
	UserID    string `gorm:"primaryKey;size:30"`
	ModelID   string `gorm:"primaryKey;size:30"`
	CreatedAt time.Time
}
