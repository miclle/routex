package entity

import "time"

// RuntimeRoutingApplication is first observed routing application by one process
// generation, not a full authorization version or fleet acknowledgement.
type RuntimeRoutingApplication struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_routing_application,priority:1;index:idx_runtime_routing_application_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_routing_application,priority:2"`
	RouteDigest       string    `gorm:"size:64;not null;check:ck_runtime_routing_application_digest,length(route_digest) = 64"`
	PublishedAt       time.Time `gorm:"precision:6;not null"`
	AppliedAt         time.Time `gorm:"precision:6;not null;index:idx_runtime_routing_application_instance,priority:2"`
}
