package database

import (
	"time"

	"gorm.io/gorm"
)

// offboardingCaseV10 freezes the historical plan/receipt schema. IDs reference
// retained business history rather than cascading live account relationships.
type offboardingCaseV10 struct {
	ID               string `gorm:"primaryKey;size:30"`
	RequestID        string `gorm:"size:64;not null;uniqueIndex:uq_offboarding_request"`
	RequestHash      string `gorm:"size:64;not null"`
	UserID           string `gorm:"size:30;not null;index:idx_offboarding_user"`
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

func (offboardingCaseV10) TableName() string { return "offboarding_cases" }

type offboardingUserV10 struct {
	ID           string     `gorm:"primaryKey;size:30"`
	OffboardedAt *time.Time `gorm:"precision:6"`
}

func (offboardingUserV10) TableName() string { return "users" }
func offboardingMigration(db *gorm.DB) error {
	if err := migrateTables(db, &offboardingCaseV10{}); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(&offboardingUserV10{}, "OffboardedAt") {
		return db.Migrator().AddColumn(&offboardingUserV10{}, "OffboardedAt")
	}
	return nil
}
