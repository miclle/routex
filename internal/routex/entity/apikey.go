package entity

import "time"

const (
	KeyPending  = "pending"
	KeyActive   = "active"
	KeyDisabled = "disabled"
	KeyRevoked  = "revoked"
)

// APIKey holds verification metadata; its bearer value is never persisted.
type APIKey struct {
	ID                string `gorm:"primaryKey;size:30"`
	UserID            string `gorm:"size:30;not null"`
	Name              string `gorm:"size:100;not null"`
	Prefix            string `gorm:"size:16;not null"`
	TokenHash         string `gorm:"size:64;not null;uniqueIndex"`
	Status            string `gorm:"size:20;not null"`
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string `gorm:"size:30"`
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (APIKey) TableName() string { return "api_keys" }

type APIKeyModel struct {
	KeyID   string `gorm:"primaryKey;size:30"`
	ModelID string `gorm:"primaryKey;size:30"`
}

func (APIKeyModel) TableName() string { return "api_key_models" }

// AuditEvent captures business actions without secret values or request bodies.
type AuditEvent struct {
	ID           string `gorm:"primaryKey;size:30"`
	ActorID      string `gorm:"size:30;not null"`
	Action       string `gorm:"size:80;not null"`
	ResourceType string `gorm:"size:40;not null"`
	ResourceID   string `gorm:"size:30;not null"`
	CreatedAt    time.Time
}
