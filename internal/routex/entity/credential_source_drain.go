package entity

import "time"

// These facts are private, permanent and never inferred from heartbeat expiry.
// Registration precedes every first exposure; old process rows are not backfilled.
type CredentialSourceProcess struct {
	ProcessID    string     `gorm:"primaryKey;size:30" json:"-"`
	Generation   string     `gorm:"size:64;not null" json:"-"`
	Birth        time.Time  `gorm:"precision:6;not null" json:"-"`
	RegisteredAt time.Time  `gorm:"precision:6;not null" json:"-"`
	ClosedAt     *time.Time `gorm:"precision:6" json:"-"`
}

// A denial covers the validated physical version-1 object across logical owners
// and auth revisions. No expiry or failed receipt reopens the object.
type CredentialSourceDenial struct {
	RemoteRequestID   *string   `gorm:"size:36" json:"-"`
	PhysicalObject    string    `gorm:"primaryKey;size:64" json:"-"`
	CreationRequestID string    `gorm:"size:36;not null;uniqueIndex:idx_credential_source_denial_creation" json:"-"`
	RequestID         string    `gorm:"size:36;not null;uniqueIndex:idx_credential_source_denial_command" json:"-"`
	CreatedAt         time.Time `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
}

// Exposed is monotonic. JoinedAt is written only after this exact process has
// closed local admission and joined every holder without Close uncertainty.
// A new process can acknowledge an existing denial with Exposed=false before
// any preparation. Retiring an instance does not delete these facts.
type CredentialSourceUse struct {
	PhysicalObject string     `gorm:"primaryKey;size:64" json:"-"`
	ProcessID      string     `gorm:"primaryKey;size:30;index:idx_credential_source_use_process" json:"-"`
	Generation     string     `gorm:"size:64;not null" json:"-"`
	Birth          time.Time  `gorm:"precision:6;not null" json:"-"`
	Exposed        bool       `gorm:"not null" json:"-"`
	CreatedAt      time.Time  `gorm:"precision:6;not null;autoCreateTime:false" json:"-"`
	JoinedAt       *time.Time `gorm:"precision:6" json:"-"`
}
