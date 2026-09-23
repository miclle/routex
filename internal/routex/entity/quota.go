package entity

import "time"

type QuotaSetting struct {
	AccountingStarted bool   `gorm:"not null;default:false"`
	ID                int    `gorm:"primaryKey;autoIncrement:false"`
	TimeZone          string `gorm:"size:100;not null"`
	ETag              string `gorm:"size:64;not null"`
	PreviousETag      string `gorm:"size:64;not null"`
	ActorID           string `gorm:"size:30;not null"`
	Reason            string `gorm:"size:2000;not null"`
	UpdatedAt         time.Time
}

func (QuotaSetting) TableName() string { return "quota_settings" }

// ReservationBound is an explicit capacity attestation for one native provider
// model. Discovery metadata and output-cap guesses never populate it implicitly.
type ReservationBound struct {
	ProviderModelID string `gorm:"primaryKey;size:30"`
	Protocol        string `gorm:"size:30;not null"`
	MaxInputTokens  int64  `gorm:"not null"`
	MaxOutputTokens int64  `gorm:"not null"`
	Evidence        string `gorm:"size:2000;not null"`
	ETag            string `gorm:"size:64;not null"`
	PreviousETag    string `gorm:"size:64;not null"`
	ActorID         string `gorm:"size:30;not null"`
	Reason          string `gorm:"size:2000;not null"`
	UpdatedAt       time.Time
}

func (ReservationBound) TableName() string { return "reservation_bounds" }
