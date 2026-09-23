package database

import (
	"gorm.io/gorm"
	"time"
)

// projectModelRequestV12 freezes the request history independently of live grants.
type projectModelRequestV12 struct {
	ID              string `gorm:"primaryKey;size:30"`
	RequestID       string `gorm:"size:64;not null;uniqueIndex:uq_project_model_request"`
	RequestHash     string `gorm:"size:64;not null"`
	ProjectID       string `gorm:"size:30;not null;index:idx_project_model_request_project"`
	ApplicantUserID string `gorm:"size:30;not null"`
	BaselineJSON    string `gorm:"type:text;not null"`
	RequestedJSON   string `gorm:"type:text;not null"`
	Reason          string `gorm:"size:2000;not null"`
	Status          string `gorm:"size:20;not null;index:idx_project_model_request_status"`
	DecisionActorID string `gorm:"size:30;not null"`
	DecisionReason  string `gorm:"size:2000;not null"`
	CreatedAt       time.Time
	DecidedAt       *time.Time
}

func (projectModelRequestV12) TableName() string { return "project_model_requests" }
func projectRequestMigration(db *gorm.DB) error  { return migrateTables(db, &projectModelRequestV12{}) }
