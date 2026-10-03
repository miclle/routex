package entity

import "time"

const (
	PersonalModelRequestPending   = "pending"
	PersonalModelRequestApproved  = "approved"
	PersonalModelRequestRejected  = "rejected"
	PersonalModelRequestWithdrawn = "withdrawn"
	PersonalModelRequestCancelled = "cancelled"
)

// PersonalModelRequest keeps submitted names and first-terminal decisions as
// immutable history. Live identity and catalogue removal cannot erase receipts.
type PersonalModelRequest struct {
	ID                  string     `gorm:"primaryKey;size:30;index:idx_personal_model_request_user_cursor,priority:3;index:idx_personal_model_request_status_cursor,priority:3"`
	RequestID           string     `gorm:"size:36;not null;uniqueIndex:uq_personal_model_request_intent;check:ck_personal_model_request_intent,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	RequestHash         string     `gorm:"size:64;not null"`
	ApplicantUserID     string     `gorm:"size:30;not null;index:idx_personal_model_request_user_cursor,priority:1"`
	ApplicantName       string     `gorm:"size:100;not null"`
	ModelID             string     `gorm:"size:30;not null"`
	ModelName           string     `gorm:"size:128;not null"`
	ReviewETag          string     `gorm:"column:review_etag;size:64;not null"`
	Reason              string     `gorm:"size:1024;not null"`
	Status              string     `gorm:"size:20;not null;index:idx_personal_model_request_status_cursor,priority:1;check:ck_personal_model_request_status,status IN ('pending','approved','rejected','withdrawn','cancelled')"`
	CancelledReason     string     `gorm:"size:128;not null;default:'';check:ck_personal_model_request_cancelled,status <> 'cancelled' OR (cancelled_reason <> '' AND resolved_at IS NOT NULL AND decision_id IS NULL)"`
	DecisionID          *string    `gorm:"size:36;uniqueIndex:uq_personal_model_request_decision;check:ck_personal_model_request_pending,status <> 'pending' OR (decision_id IS NULL AND decided_at IS NULL AND resolved_at IS NULL)"`
	DecisionActorID     *string    `gorm:"size:30"`
	DecisionActorName   *string    `gorm:"size:100"`
	DecisionAction      string     `gorm:"size:20;not null;default:''"`
	DecisionReason      string     `gorm:"size:1024;not null;default:''"`
	DecisionReviewETag  string     `gorm:"column:decision_review_etag;size:64;not null;default:''"`
	DecisionRequestHash string     `gorm:"size:64;not null;default:'';check:ck_personal_model_request_receipt,status NOT IN ('approved','rejected','withdrawn') OR (decision_id IS NOT NULL AND CHAR_LENGTH(decision_id) = 36 AND decision_actor_id IS NOT NULL AND decision_actor_id <> '' AND CHAR_LENGTH(decision_review_etag) = 64 AND CHAR_LENGTH(decision_request_hash) = 64 AND decided_at IS NOT NULL AND resolved_at IS NOT NULL AND ((status = 'approved' AND decision_action = 'approve') OR (status = 'rejected' AND decision_action = 'reject') OR (status = 'withdrawn' AND decision_action = 'withdraw')))"`
	CreatedAt           time.Time  `gorm:"precision:6;not null;index:idx_personal_model_request_user_cursor,priority:2;index:idx_personal_model_request_status_cursor,priority:2"`
	UpdatedAt           time.Time  `gorm:"precision:6;not null"`
	ResolvedAt          *time.Time `gorm:"precision:6"`
	DecidedAt           *time.Time `gorm:"precision:6"`
}

func (PersonalModelRequest) TableName() string { return "personal_model_requests" }

// A separate slot enforces one pending applicant/model pair portably; terminal
// requests release it without deleting their historical decisions.
type PersonalModelRequestPendingSlot struct {
	ApplicantUserID string    `gorm:"primaryKey;size:30;not null"`
	ModelID         string    `gorm:"primaryKey;size:30;not null"`
	RequestID       string    `gorm:"size:30;not null;uniqueIndex:uq_personal_model_pending_request"`
	CreatedAt       time.Time `gorm:"precision:6;not null"`
}

func (PersonalModelRequestPendingSlot) TableName() string {
	return "personal_model_request_pending_slots"
}
