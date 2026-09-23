package entity

import "time"

const (
	ProjectRequestPending   = "pending"
	ProjectRequestApproved  = "approved"
	ProjectRequestRejected  = "rejected"
	ProjectRequestWithdrawn = "withdrawn"
)

// ProjectModelRequest retains the original intent separately from live grants.
// Historical identifiers are retained without cascading live relationships.
type ProjectModelRequest struct {
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
