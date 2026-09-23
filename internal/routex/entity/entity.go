// Package entity provides data models and domain types.
package entity

import (
	"time"

	"gorm.io/gorm"
)

// Example is a sample entity to demonstrate the GORM model pattern.
// Replace or extend this with your actual domain models.
type Example struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	Title     string         `gorm:"size:255;not null" json:"title"`
	Body      string         `gorm:"type:text" json:"body"`
}
