package entity

import "time"

// ProjectKey belongs to its Project. CreatorID is immutable audit metadata and
// never becomes the gateway authorization owner.
type ProjectKey struct {
	ID                string `gorm:"primaryKey;size:30"`
	ProjectID         string `gorm:"size:30;not null"`
	CreatorID         string `gorm:"size:30;not null"`
	Name              string `gorm:"size:100;not null"`
	Prefix            string `gorm:"size:16;not null"`
	TokenHash         string `gorm:"size:64;not null;uniqueIndex"`
	Status            string `gorm:"size:20;not null"`
	DeliveryMode      string `gorm:"size:20;not null"`
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string `gorm:"size:30"`
	ActivateOnConfirm bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (ProjectKey) TableName() string { return "project_api_keys" }

type ProjectKeyModel struct {
	KeyID   string `gorm:"primaryKey;size:30"`
	ModelID string `gorm:"primaryKey;size:30"`
}

func (ProjectKeyModel) TableName() string { return "project_api_key_models" }
