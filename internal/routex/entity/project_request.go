package entity

import "time"

const (
	ProjectRequestModelAccess = "MODEL_ACCESS"
	ProjectRequestQuota       = "QUOTA"

	ProjectRequestPending   = "pending"
	ProjectRequestApproved  = "approved"
	ProjectRequestRejected  = "rejected"
	ProjectRequestWithdrawn = "withdrawn"
)

// ProjectModelRequest retains original model and quota intents independently
// of their current effective grants and policies.
// Historical identifiers are retained without cascading live relationships.
type ProjectModelRequest struct {
	ID                  string `gorm:"primaryKey;size:30"`
	RequestID           string `gorm:"size:64;not null;uniqueIndex:uq_project_model_request"`
	RequestHash         string `gorm:"size:64;not null"`
	ProjectID           string `gorm:"size:30;not null;index:idx_project_model_request_project"`
	ApplicantUserID     string `gorm:"size:30;not null"`
	Kind                string `gorm:"size:20;not null;default:MODEL_ACCESS;check:ck_project_request_kind,kind IN ('MODEL_ACCESS','QUOTA')"`
	BaselinePolicyETag  string `gorm:"column:baseline_policy_etag;size:64;not null;default:''"`
	DecisionReviewETag  string `gorm:"column:decision_review_etag;size:64;not null;default:''"`
	ApprovedPolicyETag  string `gorm:"column:approved_policy_etag;size:64;not null;default:''"`
	DecisionRequestHash string `gorm:"size:64;not null;default:''"`
	ApprovedPolicyJSON  string `gorm:"type:text;not null;default:('');check:ck_project_quota_approval,kind <> 'QUOTA' OR status <> 'approved' OR (decision_review_etag <> '' AND approved_policy_etag <> '' AND decision_request_hash <> '' AND approved_policy_json <> '')"`
	BaselineJSON        string `gorm:"type:text;not null"`
	RequestedJSON       string `gorm:"type:text;not null"`
	Reason              string `gorm:"size:2000;not null"`
	Status              string `gorm:"size:20;not null;index:idx_project_model_request_status"`
	DecisionActorID     string `gorm:"size:30;not null"`
	DecisionReason      string `gorm:"size:2000;not null"`
	CreatedAt           time.Time
	DecidedAt           *time.Time
}

func (ProjectModelRequest) TableName() string { return "project_model_requests" }
