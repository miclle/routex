package entity

import "time"

const (
	TeamModelRequestPending   = "pending"
	TeamModelRequestApproved  = "approved"
	TeamModelRequestRejected  = "rejected"
	TeamModelRequestWithdrawn = "withdrawn"
	TeamModelRequestCancelled = "cancelled"
)

// TeamModelRequest preserves the submitted Team and membership identities and
// the first terminal receipt independently of live catalogue and membership rows.
type TeamModelRequest struct {
	ID                    string     `gorm:"primaryKey;size:30;index:idx_team_model_request_team_cursor,priority:3;index:idx_team_model_request_user_cursor,priority:3"`
	RequestID             string     `gorm:"size:36;not null;uniqueIndex:uq_team_model_request_intent;check:ck_team_model_request_intent,CHAR_LENGTH(request_id) = 36 AND CHAR_LENGTH(request_hash) = 64 AND CHAR_LENGTH(review_etag) = 64"`
	RequestHash           string     `gorm:"size:64;not null"`
	TeamID                string     `gorm:"size:30;not null;index:idx_team_model_request_team_cursor,priority:1"`
	TeamName              string     `gorm:"size:100;not null"`
	ApplicantUserID       string     `gorm:"size:30;not null;index:idx_team_model_request_user_cursor,priority:1"`
	ApplicantName         string     `gorm:"size:100;not null"`
	ApplicantMembershipID string     `gorm:"size:30;not null"`
	ModelID               string     `gorm:"size:30;not null"`
	ModelName             string     `gorm:"size:128;not null"`
	ReviewETag            string     `gorm:"column:review_etag;size:64;not null"`
	Reason                string     `gorm:"size:1024;not null"`
	Status                string     `gorm:"size:20;not null;check:ck_team_model_request_status,status IN ('pending','approved','rejected','withdrawn','cancelled')"`
	CancelledReason       string     `gorm:"size:128;not null;default:'';check:ck_team_model_request_cancelled,status <> 'cancelled' OR (cancelled_reason <> '' AND resolved_at IS NOT NULL AND decided_at IS NULL AND decision_id IS NULL AND decision_actor_id IS NULL AND decision_actor_name IS NULL AND decision_action = '' AND decision_reason = '' AND decision_review_etag = '' AND decision_request_hash = '')"`
	DecisionID            *string    `gorm:"size:36;uniqueIndex:uq_team_model_request_decision;check:ck_team_model_request_pending,status <> 'pending' OR (decision_id IS NULL AND decision_actor_id IS NULL AND decision_actor_name IS NULL AND decision_action = '' AND decision_reason = '' AND decision_review_etag = '' AND decision_request_hash = '' AND cancelled_reason = '' AND decided_at IS NULL AND resolved_at IS NULL)"`
	DecisionActorID       *string    `gorm:"size:30"`
	DecisionActorName     *string    `gorm:"size:100"`
	DecisionAction        string     `gorm:"size:20;not null;default:''"`
	DecisionReason        string     `gorm:"size:1024;not null;default:''"`
	DecisionReviewETag    string     `gorm:"column:decision_review_etag;size:64;not null;default:''"`
	DecisionRequestHash   string     `gorm:"size:64;not null;default:'';check:ck_team_model_request_receipt,status NOT IN ('approved','rejected','withdrawn') OR (decision_id IS NOT NULL AND CHAR_LENGTH(decision_id) = 36 AND decision_actor_id IS NOT NULL AND decision_actor_id <> '' AND decision_actor_name IS NOT NULL AND CHAR_LENGTH(decision_review_etag) = 64 AND CHAR_LENGTH(decision_request_hash) = 64 AND decided_at IS NOT NULL AND resolved_at IS NOT NULL AND cancelled_reason = '' AND ((status = 'approved' AND decision_action = 'approve' AND decision_actor_id <> applicant_user_id) OR (status = 'rejected' AND decision_action = 'reject' AND decision_actor_id <> applicant_user_id AND decision_reason <> '') OR (status = 'withdrawn' AND decision_action = 'withdraw' AND decision_actor_id = applicant_user_id AND decision_reason = '')))"`
	CreatedAt             time.Time  `gorm:"precision:6;not null;index:idx_team_model_request_team_cursor,priority:2;index:idx_team_model_request_user_cursor,priority:2"`
	UpdatedAt             time.Time  `gorm:"precision:6;not null"`
	ResolvedAt            *time.Time `gorm:"precision:6"`
	DecidedAt             *time.Time `gorm:"precision:6"`
}

func (TeamModelRequest) TableName() string { return "team_model_requests" }

// A Team Model grant is shared, so every applicant competes for the same slot.
type TeamModelRequestPendingSlot struct {
	TeamID    string    `gorm:"primaryKey;size:30;not null"`
	ModelID   string    `gorm:"primaryKey;size:30;not null"`
	RequestID string    `gorm:"size:30;not null;uniqueIndex:uq_team_model_pending_request"`
	CreatedAt time.Time `gorm:"precision:6;not null"`
}

func (TeamModelRequestPendingSlot) TableName() string { return "team_model_request_pending_slots" }
