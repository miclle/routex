package entity

import "time"

// ProviderQualityPolicy controls evaluation for one current Provider.
type ProviderQualityPolicy struct {
	ProviderID        string `gorm:"primaryKey;size:30"`
	Enabled           bool   `gorm:"not null"`
	WindowMinutes     int    `gorm:"not null"`
	MinimumAttempts   int    `gorm:"not null"`
	MinSuccessRateBPS int    `gorm:"not null"`
	MaxP95DurationMS  *int64
	ETag              string    `gorm:"size:30;not null"`
	UpdatedBy         string    `gorm:"size:30;not null"`
	UpdateReason      string    `gorm:"size:500;not null"`
	UpdatedAt         time.Time `gorm:"precision:6;not null"`
}

// ProviderQualityWindow is an immutable evaluation with provider labels and
// policy identity captured at evaluation time.
type ProviderQualityWindow struct {
	ID                         string    `gorm:"primaryKey;size:30"`
	ProviderID                 string    `gorm:"size:30;not null;uniqueIndex:idx_provider_quality_windows_identity,priority:1;index:idx_provider_quality_windows_provider_end,priority:1"`
	ProviderName               string    `gorm:"size:100;not null"`
	WindowStart                time.Time `gorm:"precision:6;not null"`
	WindowEnd                  time.Time `gorm:"precision:6;not null;uniqueIndex:idx_provider_quality_windows_identity,priority:2;index:idx_provider_quality_windows_provider_end,priority:2"`
	PolicyETag                 string    `gorm:"size:30;not null"`
	EligibleAttempts           int64     `gorm:"not null"`
	ExcludedAttempts           int64     `gorm:"not null"`
	UnknownAttributionAttempts int64     `gorm:"not null"`
	Successes                  int64     `gorm:"not null"`
	HTTP429                    int64     `gorm:"column:http_429;not null"`
	HTTP5XX                    int64     `gorm:"column:http_5xx;not null"`
	KnownDurationAttempts      int64     `gorm:"not null"`
	P95DurationMS              *int64
	State                      string    `gorm:"size:20;not null"`
	DetailCode                 string    `gorm:"size:40;not null"`
	CreatedAt                  time.Time `gorm:"precision:6;not null"`
}

// ProviderQualityState stores the last durable transition for one current
// Provider. LastAlertID is a historical reference and intentionally has no FK.
type ProviderQualityState struct {
	ProviderID    string     `gorm:"primaryKey;size:30"`
	LastWindowEnd *time.Time `gorm:"precision:6"`
	LastState     string     `gorm:"size:20;not null"`
	LastAlertID   string     `gorm:"size:30;not null"`
	UpdatedAt     time.Time  `gorm:"precision:6;not null"`
}

func (ProviderQualityPolicy) TableName() string { return "provider_quality_policies" }
func (ProviderQualityWindow) TableName() string { return "provider_quality_windows" }
func (ProviderQualityState) TableName() string  { return "provider_quality_states" }
