package entity

import "time"

// ModelDiscoveryNotice freezes only the first-recorded count and bounded Provider
// identity from a successful explicit discovery. It contains no upstream payload.
type ModelDiscoveryNotice struct {
	AuditID      string    `gorm:"primaryKey;size:30"`
	ConnectionID string    `gorm:"size:30;not null"`
	ProviderID   string    `gorm:"size:30;not null"`
	ProviderName string    `gorm:"size:100;not null"`
	ModelCount   int       `gorm:"not null"`
	RecordedAt   time.Time `gorm:"precision:6;not null"`
}

func (ModelDiscoveryNotice) TableName() string { return "model_discovery_notices" }
