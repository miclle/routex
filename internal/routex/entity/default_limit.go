package entity

import "time"

// DefaultLimitRule is a creation template, never an inherited runtime policy.
// Existing resource policies change only through an explicitly reviewed reset.
type DefaultLimitRule struct {
	Kind         string `gorm:"primaryKey;size:20;not null"`
	Tokens5H     *int64 `gorm:"column:tokens_5h"`
	Tokens7D     *int64 `gorm:"column:tokens_7d"`
	TokensMonth  *int64
	TPM          *int64
	MoneyMonth   *string `gorm:"size:40"`
	Currency     string  `gorm:"size:3;not null;default:''"`
	RPM          *int64
	Concurrency  *int64
	RuleETag     string    `gorm:"column:rule_etag;size:64;not null"`
	PreviousETag *string   `gorm:"column:previous_etag;size:64"`
	ActorID      string    `gorm:"size:30;not null"`
	Reason       string    `gorm:"size:1024;not null"`
	UpdatedAt    time.Time `gorm:"precision:6"`
}

func (DefaultLimitRule) TableName() string { return "default_limit_rules" }
