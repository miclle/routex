package entity

import "time"

// SystemJob is a bounded, secret-free operational run snapshot. Codes and
// detail codes are allowlisted by the service so this table cannot become an
// arbitrary error or request-content log.
type SystemJob struct {
	ID             string     `gorm:"primaryKey;size:30"`
	Code           string     `gorm:"size:40;not null;index:idx_system_jobs_code_started,priority:1;check:ck_system_jobs_code,code IN ('runtime_publication','call_record_delivery','storage_cleanup')"`
	Status         string     `gorm:"size:20;not null;index:idx_system_jobs_status_updated,priority:1;check:ck_system_jobs_status,status IN ('running','completed','failed')"`
	ExecutorID     string     `gorm:"size:30;not null;default:''"`
	Progress       *int       `gorm:"check:chk_system_jobs_progress,progress IS NULL OR (progress >= 0 AND progress <= 100)"`
	ItemsTotal     int        `gorm:"not null;default:0;check:ck_system_jobs_total,items_total >= 0"`
	ItemsCompleted int        `gorm:"not null;default:0;check:ck_system_jobs_completed,items_completed >= 0 AND items_completed <= items_total"`
	DetailCode     string     `gorm:"size:40;not null;default:''"`
	StartedAt      time.Time  `gorm:"precision:6;not null;index:idx_system_jobs_code_started,priority:2"`
	UpdatedAt      time.Time  `gorm:"precision:6;not null;index:idx_system_jobs_status_updated,priority:2"`
	CompletedAt    *time.Time `gorm:"precision:6"`
}
