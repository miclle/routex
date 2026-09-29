package entity

import "time"

// SystemInstance is one process generation in the RouteX deployment. A new
// process always creates a new row; heartbeats are the only source of liveness.
type SystemInstance struct {
	ID                string `gorm:"primaryKey;size:30"`
	LeaseToken        string `gorm:"size:30;not null" json:"-"`
	HeartbeatRevision uint64 `gorm:"not null;check:chk_system_instances_revision,heartbeat_revision > 0"`
	Name              string `gorm:"size:100;not null"`
	Hostname          string `gorm:"size:253;not null"`
	Role              string `gorm:"size:20;not null;check:chk_system_instances_role,role = 'combined'"`
	Version           string `gorm:"size:100;not null"`
	Commit            string `gorm:"size:64;not null"`
	BuildTime         string `gorm:"size:64;not null"`
	GoVersion         string `gorm:"size:40;not null"`
	OS                string `gorm:"size:32;not null"`
	Arch              string `gorm:"size:32;not null"`
	CPUUsed           *int64
	CPUTotal          *int64
	CPUScope          string `gorm:"size:40;not null"`
	MemoryUsed        *int64
	MemoryTotal       *int64
	MemoryScope       string `gorm:"size:40;not null"`
	StorageUsed       *int64
	StorageTotal      *int64
	StorageScope      string     `gorm:"size:40;not null"`
	StartedAt         time.Time  `gorm:"precision:6;not null"`
	LastHeartbeatAt   time.Time  `gorm:"precision:6;not null;index:idx_system_instances_cleanup,priority:2"`
	LeaseExpiresAt    time.Time  `gorm:"precision:6;not null;index:idx_system_instances_lease"`
	StoppedAt         *time.Time `gorm:"precision:6"`
	RetiredAt         *time.Time `gorm:"precision:6;index:idx_system_instances_cleanup,priority:1"`
	RetiredBy         string     `gorm:"size:30;not null"`
}
