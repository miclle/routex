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
	DeliveryMode      string `gorm:"column:delivery_mode;size:20;not null;default:manual;check:ck_api_keys_delivery_mode,(OCTET_LENGTH(delivery_mode) = 6 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 108) OR (OCTET_LENGTH(delivery_mode) = 13 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 103 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 101 AND ASCII(SUBSTRING(delivery_mode,7,1)) = 100 AND ASCII(SUBSTRING(delivery_mode,8,1)) = 95 AND ASCII(SUBSTRING(delivery_mode,9,1)) = 118 AND ASCII(SUBSTRING(delivery_mode,10,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,11,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,12,1)) = 108 AND ASCII(SUBSTRING(delivery_mode,13,1)) = 116)"`
	LifecycleRevision string `gorm:"size:30;not null;default:''"`
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
	DetailsJSON  *string `gorm:"type:text"`
	ID           string  `gorm:"primaryKey;size:30"`
	ActorID      string  `gorm:"size:30;not null"`
	Action       string  `gorm:"size:80;not null"`
	ResourceType string  `gorm:"size:40;not null"`
	ResourceID   string  `gorm:"size:30;not null"`
	CreatedAt    time.Time
}
