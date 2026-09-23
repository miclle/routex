package entity

import "time"

// CallRecord is an immutable, content-free request fact. Stable IDs and names
// are retained as historical snapshots rather than live cascading relations.
type CallRecord struct {
	SnapshotID      string    `gorm:"size:30;not null"`
	RequestID       string    `gorm:"primaryKey;size:64"`
	UserID          string    `gorm:"size:30;not null"`
	KeyID           string    `gorm:"size:30;not null"`
	ModelID         string    `gorm:"size:30;not null"`
	ModelName       string    `gorm:"size:128;not null"`
	ProviderModelID string    `gorm:"size:30;not null"`
	ConnectionID    string    `gorm:"size:30;not null"`
	Protocol        string    `gorm:"size:30;not null"`
	Status          string    `gorm:"size:20;not null"`
	Stream          bool      `gorm:"not null"`
	StartedAt       time.Time `gorm:"not null"`
	CompletedAt     time.Time `gorm:"not null"`
	DurationMS      int64     `gorm:"not null"`
	InputTokens     *int64
	OutputTokens    *int64
	ErrorCode       string `gorm:"size:40;not null"`
}

type CallAttempt struct {
	ID              string    `gorm:"primaryKey;size:64"`
	RequestID       string    `gorm:"size:64;not null"`
	ProviderModelID string    `gorm:"size:30;not null"`
	ConnectionID    string    `gorm:"size:30;not null"`
	Status          string    `gorm:"size:20;not null"`
	HTTPStatus      int       `gorm:"not null"`
	ErrorCode       string    `gorm:"size:40;not null"`
	StartedAt       time.Time `gorm:"not null"`
	CompletedAt     time.Time `gorm:"not null"`
}
