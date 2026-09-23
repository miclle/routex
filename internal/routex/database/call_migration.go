package database

import (
	"gorm.io/gorm"
	"time"
)

// Version 5 owns frozen schema definitions; never replace these with live entities.
// Historical actor/catalog IDs deliberately have no cascading foreign keys.
func callMigration(db *gorm.DB) error {
	return migrateTables(db, &callRecordV5{}, &callAttemptV5{})
}

type callRecordV5 struct {
	SnapshotID      string    `gorm:"size:30;not null"`
	ID              string    `gorm:"column:request_id;primaryKey;size:64;index:idx_calls_owner_time,priority:3;index:idx_calls_time,priority:2"`
	UserID          string    `gorm:"size:30;not null;index:idx_calls_owner_time,priority:1"`
	KeyID           string    `gorm:"size:30;not null"`
	ModelID         string    `gorm:"size:30;not null"`
	ModelName       string    `gorm:"size:128;not null"`
	ProviderModelID string    `gorm:"size:30;not null"`
	ConnectionID    string    `gorm:"size:30;not null"`
	Protocol        string    `gorm:"size:30;not null"`
	Status          string    `gorm:"size:20;not null"`
	Stream          bool      `gorm:"not null"`
	StartedAt       time.Time `gorm:"precision:6;not null;index:idx_calls_owner_time,priority:2;index:idx_calls_time,priority:1"`
	CompletedAt     time.Time `gorm:"precision:6;not null"`
	DurationMS      int64     `gorm:"not null"`
	InputTokens     *int64
	OutputTokens    *int64
	ErrorCode       string `gorm:"size:40;not null"`
}

func (callRecordV5) TableName() string { return "call_records" }

type callAttemptV5 struct {
	ID              string       `gorm:"primaryKey;size:64"`
	RequestID       string       `gorm:"size:64;not null;index:idx_attempts_request"`
	ProviderModelID string       `gorm:"size:30;not null"`
	ConnectionID    string       `gorm:"size:30;not null"`
	Status          string       `gorm:"size:20;not null"`
	HTTPStatus      int          `gorm:"not null"`
	ErrorCode       string       `gorm:"size:40;not null"`
	StartedAt       time.Time    `gorm:"precision:6;not null"`
	CompletedAt     time.Time    `gorm:"precision:6;not null"`
	Request         callRecordV5 `gorm:"belongsTo:Request;foreignKey:RequestID;references:ID;constraint:fk_attempt_request"`
}

func (callAttemptV5) TableName() string { return "call_attempts" }
