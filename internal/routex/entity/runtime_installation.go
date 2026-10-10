package entity

import "time"

// RuntimeInstallationObservation retains the first observed installed Gateway
// source for one process generation. It is not a command or fleet receipt.
type RuntimeInstallationObservation struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_installation_source,priority:1;index:idx_runtime_installation_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null"`
	ProjectionVersion int       `gorm:"not null;check:ck_runtime_installation_projection,projection_version = 1"`
	SourceDigest      string    `gorm:"size:64;not null;uniqueIndex:idx_runtime_installation_source,priority:2" json:"-"`
	RoutesPublishedAt time.Time `gorm:"precision:6;not null"`
	FirstObservedAt   time.Time `gorm:"precision:6;not null;index:idx_runtime_installation_instance,priority:2"`
}

func (RuntimeInstallationObservation) TableName() string { return "runtime_installation_observations" }
