package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Version 40 owns these frozen request, stage receipt and pending-slot schemas.
// No live identity or policy foreign keys may erase historical evidence.
type teamQuotaRequestV40 struct {
	ID                       string     `gorm:"primaryKey;size:30;index:idx_team_quota_request_team_cursor,priority:3;index:idx_team_quota_request_applicant_cursor,priority:3"`
	RequestID                string     `gorm:"size:36;not null;uniqueIndex:uq_team_quota_request_intent"`
	RequestHash              string     `gorm:"size:64;not null"`
	CreationReviewETag       string     `gorm:"column:creation_review_etag;size:64;not null"`
	TeamID                   string     `gorm:"size:30;not null;index:idx_team_quota_request_team_cursor,priority:1"`
	ApplicantUserID          string     `gorm:"size:30;not null;index:idx_team_quota_request_applicant_cursor,priority:1"`
	ApplicantMembershipID    string     `gorm:"size:30;not null"`
	TeamName                 string     `gorm:"size:100;not null"`
	ApplicantName            string     `gorm:"size:100;not null"`
	Dimension                string     `gorm:"size:8;not null;check:ck_team_quota_request_dimension,dimension IN ('tokens','money')"`
	TargetValue              string     `gorm:"size:40;not null"`
	Currency                 string     `gorm:"size:3;not null;default:'';check:ck_team_quota_request_currency,(dimension = 'tokens' AND currency = '') OR (dimension = 'money' AND currency <> '')"`
	Reason                   string     `gorm:"size:2000;not null"`
	SubmittedSnapshotJSON    string     `gorm:"type:text;not null"`
	Status                   string     `gorm:"size:24;not null;index:idx_team_quota_request_status;check:ck_team_quota_request_status,status IN ('pending_team_owner','pending_quota_admin','approved','rejected','withdrawn','cancelled')"`
	CurrentStepID            *string    `gorm:"size:30"`
	EscalationReason         string     `gorm:"size:32;not null;default:''"`
	CancelledReason          string     `gorm:"size:32;not null;default:''"`
	ApprovedTeamPolicyETag   string     `gorm:"column:approved_team_policy_etag;size:64;not null;default:''"`
	ApprovedMemberPolicyETag string     `gorm:"column:approved_member_policy_etag;size:64;not null;default:''"`
	ApprovedTeamPolicyJSON   string     `gorm:"type:text;not null;default:('')"`
	ApprovedMemberPolicyJSON string     `gorm:"type:text;not null;default:('');check:ck_team_quota_request_approval,status <> 'approved' OR (approved_team_policy_etag <> '' AND approved_member_policy_etag <> '' AND approved_team_policy_json <> '' AND approved_member_policy_json <> '' AND resolved_at IS NOT NULL)"`
	CreatedAt                time.Time  `gorm:"precision:6;not null;index:idx_team_quota_request_team_cursor,priority:2;index:idx_team_quota_request_applicant_cursor,priority:2"`
	UpdatedAt                time.Time  `gorm:"precision:6;not null"`
	ResolvedAt               *time.Time `gorm:"precision:6"`
}

func (teamQuotaRequestV40) TableName() string { return "team_quota_requests" }

type teamQuotaRequestStepV40 struct {
	ID           string     `gorm:"primaryKey;size:30"`
	RequestID    string     `gorm:"size:30;not null;uniqueIndex:uq_team_quota_request_step,priority:1"`
	Ordinal      int        `gorm:"not null;uniqueIndex:uq_team_quota_request_step,priority:2;check:ck_team_quota_step_ordinal,ordinal >= 1 AND ordinal <= 2"`
	Stage        string     `gorm:"size:16;not null;check:ck_team_quota_step_stage,stage IN ('team_owner','quota_admin')"`
	Status       string     `gorm:"size:16;not null;check:ck_team_quota_step_status,status IN ('pending','approved','rejected','withdrawn','cancelled')"`
	DecisionID   *string    `gorm:"size:36;uniqueIndex:uq_team_quota_step_decision"`
	DecisionHash string     `gorm:"size:64;not null;default:''"`
	ReviewETag   string     `gorm:"column:review_etag;size:64;not null;default:''"`
	ActorID      string     `gorm:"size:30;not null;default:''"`
	ActorName    string     `gorm:"size:100;not null;default:''"`
	Action       string     `gorm:"size:16;not null;default:''"`
	Reason       string     `gorm:"size:2000;not null;default:'';check:ck_team_quota_step_receipt,status NOT IN ('approved','rejected','withdrawn') OR (decision_id IS NOT NULL AND decision_id <> '' AND decision_hash <> '' AND review_etag <> '' AND actor_id <> '' AND action <> '' AND decided_at IS NOT NULL)"`
	EnteredAt    time.Time  `gorm:"precision:6;not null"`
	DecidedAt    *time.Time `gorm:"precision:6"`
}

func (teamQuotaRequestStepV40) TableName() string { return "team_quota_request_steps" }

// The slot exists only while a request is pending. A separate table provides
// portable uniqueness without a driver-specific filtered unique index.
type teamQuotaPendingSlotV40 struct {
	TeamID          string    `gorm:"primaryKey;size:30;not null"`
	ApplicantUserID string    `gorm:"primaryKey;size:30;not null"`
	Dimension       string    `gorm:"primaryKey;size:8;not null;check:ck_team_quota_slot_dimension,dimension IN ('tokens','money')"`
	RequestID       string    `gorm:"size:30;not null;uniqueIndex:uq_team_quota_pending_request"`
	CreatedAt       time.Time `gorm:"precision:6;not null"`
}

func (teamQuotaPendingSlotV40) TableName() string { return "team_quota_pending_slots" }

type teamQuotaRequestPermissionV40 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (teamQuotaRequestPermissionV40) TableName() string { return "role_permissions" }

func teamQuotaRequestMigration(db *gorm.DB) error {
	if err := migrateTables(db, &teamQuotaRequestV40{}, &teamQuotaRequestStepV40{}, &teamQuotaPendingSlotV40{}); err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&teamQuotaRequestPermissionV40{
		RoleID:     "rol_admin",
		Permission: "teams.quota_requests.read_all",
	}).Error
}
