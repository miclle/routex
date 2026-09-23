package entity

import "time"

// OffboardingCase stores a reviewed plan and its completion receipt. It contains
// only responsibility metadata; credential values and password proofs never enter it.
type OffboardingCase struct {
	ID               string `gorm:"primaryKey;size:30"`
	RequestID        string `gorm:"size:64;not null;uniqueIndex"`
	RequestHash      string `gorm:"size:64;not null"`
	UserID           string `gorm:"size:30;not null;index"`
	ActorID          string `gorm:"size:30;not null"`
	Mode             string `gorm:"size:20;not null"`
	Status           string `gorm:"size:30;not null"`
	Reason           string `gorm:"size:2000;not null"`
	PlannedAt        *time.Time
	InventoryVersion string `gorm:"size:64;not null"`
	InventoryJSON    string `gorm:"type:text;not null"`
	AssignmentsJSON  string `gorm:"type:text;not null"`
	CompletedBy      string `gorm:"size:30;not null"`
	CreatedAt        time.Time
	CompletedAt      *time.Time
}
