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
	ID         string `gorm:"primaryKey;size:30"`
	ProviderID string `gorm:"size:30;not null"`
	Name       string `gorm:"size:100;not null"`
	BaseURL    string `gorm:"size:2048;not null"`
	Protocol   string `gorm:"size:30;not null"`
	CreatedAt  time.Time
}

type ProviderCredential struct {
	ID                 string `gorm:"primaryKey;size:30"`
	ConnectionID       string `gorm:"size:30;not null"`
	Name               string `gorm:"size:100;not null"`
	Ciphertext         string `gorm:"type:text;not null"`
	Priority           int    `gorm:"not null"`
	Enabled            bool   `gorm:"not null"`
	VerificationStatus string `gorm:"size:20;not null"`
	VerifiedAt         *time.Time
	CreatedAt          time.Time
}

type ProviderModel struct {
	ID           string `gorm:"primaryKey;size:30"`
	ConnectionID string `gorm:"size:30;not null"`
	UpstreamName string `gorm:"size:255;not null"`
	Disabled     bool   `gorm:"not null;default:false"`
	ETag         string `gorm:"size:64;not null;default:0"`
	CreatedAt    time.Time
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
