package entity

import "time"

type SiteSetting struct {
	ID              int       `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Name            string    `gorm:"size:100;not null" json:"name"`
	ServiceURL      string    `gorm:"size:2048;not null" json:"service_url"`
	LogoURL         string    `gorm:"size:2048;not null" json:"logo_url"`
	Footer          string    `gorm:"size:500;not null" json:"footer"`
	DefaultLanguage string    `gorm:"size:8;not null" json:"default_language"`
	ETag            string    `gorm:"size:30;not null" json:"etag"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Announcement struct {
	ID        string     `gorm:"primaryKey;size:30" json:"id"`
	Content   string     `gorm:"type:text;not null" json:"content"`
	Status    string     `gorm:"size:12;not null;index:idx_announcements_status" json:"status"`
	ETag      string     `gorm:"size:30;not null" json:"etag"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}
