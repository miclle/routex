package entity

import "time"

// RuntimePublication records safe local publication outcomes, never snapshot secrets.
type RuntimePublication struct {
	ID         string `gorm:"primaryKey;size:30"`
	SnapshotID string `gorm:"size:30;not null"`
	Status     string `gorm:"size:20;not null"`
	ErrorCode  string `gorm:"size:40;not null"`
	CreatedAt  time.Time
}
