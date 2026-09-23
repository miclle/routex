package entity

import "time"

// Egress is a managed proxy, separate from a connection's default/direct choice.
type Egress struct {
	ID               string `gorm:"primaryKey;size:30"`
	Name             string `gorm:"size:100;not null"`
	Kind             string `gorm:"size:12;not null"`
	Host             string `gorm:"size:253;not null"`
	Port             int    `gorm:"not null"`
	Enabled          bool   `gorm:"not null"`
	ETag             string `gorm:"size:30;not null"`
	SecretGeneration string `gorm:"size:30;not null" json:"-"`
	AuthCiphertext   string `gorm:"type:text;not null" json:"-"`
	LastDiagnostic   string `gorm:"type:text;not null"`
	LastCheckedAt    *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type EgressSetting struct {
	ID              int     `gorm:"primaryKey;autoIncrement:false"`
	DefaultEgressID *string `gorm:"size:30"`
	ETag            string  `gorm:"size:30;not null"`
	UpdatedAt       time.Time
}
