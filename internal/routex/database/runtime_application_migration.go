package database

import (
	"gorm.io/gorm"
	"time"
)

// Frozen V87 adds only routing application evidence. Old publication rows have
// no process identity and cannot be backfilled into this table.
type runtimeRoutingApplicationV87 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_routing_application,priority:1;index:idx_runtime_routing_application_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_routing_application,priority:2"`
	RouteDigest       string    `gorm:"size:64;not null;check:ck_runtime_routing_application_digest,length(route_digest) = 64"`
	PublishedAt       time.Time `gorm:"precision:6;not null"`
	AppliedAt         time.Time `gorm:"precision:6;not null;index:idx_runtime_routing_application_instance,priority:2"`
}

func (runtimeRoutingApplicationV87) TableName() string { return "runtime_routing_applications" }
func runtimeApplicationMigration(db *gorm.DB) error {
	return migrateTables(db, &runtimeRoutingApplicationV87{})
}
