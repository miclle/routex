package entity

import "time"

// ResourceLimit stores implemented restrictions. Key scope IDs identify the
// oldest immutable rotation ancestor, shared by overlapping credentials.
type ResourceLimit struct {
	ScopeKind    string `gorm:"primaryKey;size:20"`
	ScopeID      string `gorm:"primaryKey;size:30"`
	ETag         string `gorm:"size:64;not null"`
	PreviousETag string `gorm:"size:64;not null"`
	ActorID      string `gorm:"size:30;not null"`
	Reason       string `gorm:"size:2000;not null"`
	Tokens5H     *int64
	Tokens7D     *int64
	TokensMonth  *int64
	TPM          *int64
	MoneyMonth   *string `gorm:"size:40"`
	Currency     string  `gorm:"size:3;not null;default:''"`
	RPM          *int64
	Concurrency  *int64
	IPMode       string `gorm:"size:20;not null"`
	IPRangesJSON string `gorm:"type:text;not null"`
	UpdatedAt    time.Time
}

func (ResourceLimit) TableName() string { return "resource_limits" }

type LimitInstallation struct {
	ID          int    `gorm:"primaryKey;autoIncrement:false"`
	JournalID   string `gorm:"size:64;not null"`
	Initialized bool   `gorm:"not null"`
}

func (LimitInstallation) TableName() string { return "limit_installation" }
