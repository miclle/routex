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
	DeliveryMode      string `gorm:"column:delivery_mode;size:20;not null;check:ck_project_keys_delivery,(OCTET_LENGTH(delivery_mode) = 6 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 108) OR (OCTET_LENGTH(delivery_mode) = 13 AND ASCII(SUBSTRING(delivery_mode,1,1)) = 109 AND ASCII(SUBSTRING(delivery_mode,2,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,3,1)) = 110 AND ASCII(SUBSTRING(delivery_mode,4,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,5,1)) = 103 AND ASCII(SUBSTRING(delivery_mode,6,1)) = 101 AND ASCII(SUBSTRING(delivery_mode,7,1)) = 100 AND ASCII(SUBSTRING(delivery_mode,8,1)) = 95 AND ASCII(SUBSTRING(delivery_mode,9,1)) = 118 AND ASCII(SUBSTRING(delivery_mode,10,1)) = 97 AND ASCII(SUBSTRING(delivery_mode,11,1)) = 117 AND ASCII(SUBSTRING(delivery_mode,12,1)) = 108 AND ASCII(SUBSTRING(delivery_mode,13,1)) = 116)"`
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
