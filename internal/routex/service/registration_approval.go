package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type MemberApprovalApplication struct {
	ID              string     `json:"id"`
	State           string     `json:"state"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at"`
	DecisionActorID *string    `json:"decision_actor_id"`
	DecisionReason  *string    `json:"decision_reason"`
}
type MemberApprovalRecord struct {
	UserID            string                     `json:"user_id"`
	Name              string                     `json:"name"`
	IdentityRole      string                     `json:"identity_role"`
	Disabled          bool                       `json:"disabled"`
	OffboardedAt      *time.Time                 `json:"offboarded_at"`
	ApprovalStatus    string                     `json:"approval_status"`
	Application       *MemberApprovalApplication `json:"application"`
	CanApprove        bool                       `json:"can_approve"`
	CanReject         bool                       `json:"can_reject"`
	AdmissionEligible bool                       `json:"admission_eligible"`
	RuntimeApplied    bool                       `json:"runtime_applied"`
	ReviewETag        string                     `json:"review_etag"`
}
type MemberApprovalInput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
type MemberApprovalResult struct {
	Confirmation      string `json:"confirmation"`
	UserID            string `json:"user_id"`
	ApplicationID     string `json:"application_id"`
	Decision          string `json:"decision"`
	AdmissionEligible bool   `json:"admission_eligible"`
	RuntimeApplied    bool   `json:"runtime_applied"`
}

var registrationApprovalUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "account approval confirmation unavailable"}

func (in *MemberApprovalInput) UnmarshalJSON(raw []byte) error {
	fields, err := registrationStrictObject(raw, []string{"decision", "reason"})
	if err != nil {
		return err
	}
	var next MemberApprovalInput
	if json.Unmarshal(fields["decision"], &next.Decision) != nil || json.Unmarshal(fields["reason"], &next.Reason) != nil || !validMemberApprovalInput(next) {
		return apperrors.ErrBadRequest
	}
	*in = next
	return nil
}
func validRegistrationReason(s string) bool {
	return validCredentialMetadataReason(s) && strings.TrimSpace(s) == s
}
func validMemberApprovalInput(in MemberApprovalInput) bool {
	return (in.Decision == "approve" || in.Decision == "reject") && validRegistrationReason(in.Reason)
}
func registrationStrictObject(raw []byte, names []string) (map[string]json.RawMessage, error) {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return nil, apperrors.ErrBadRequest
	}
	fields, err := memberRolesObject(raw)
	if err != nil || len(fields) != len(names) {
		return nil, apperrors.ErrBadRequest
	}
	for name, v := range fields {
		if !slices.Contains(names, name) || string(v) == "null" {
			return nil, apperrors.ErrBadRequest
		}
	}
	return fields, nil
}
func registrationApprovalError(err error) error {
	if err == nil {
		return nil
	}
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return registrationApprovalUnavailable
}
func registrationDecisionPeople(tx *gorm.DB, actorID, targetID string, lock bool) (entity.User, entity.User, error) {
	var actor, target entity.User
	ids := []string{actorID, targetID}
	if lock {
		slices.Sort(ids)
		ids = slices.Compact(ids)
	}
	for _, id := range ids {
		var u entity.User
		q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, id))
		if lock {
			q = q.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		err := q.First(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && u.ID != id {
			if id == actorID {
				return actor, target, apperrors.ErrUnauthorized
			}
			return actor, target, apperrors.ErrNotFound
		}
		if err != nil {
			return actor, target, err
		}
		if id == actorID {
			actor = u
		}
		if id == targetID {
			target = u
		}
	}
	if err := requireRegistrationAdmission(tx, actor); err != nil {
		return actor, target, err
	}
	if actor.Role != entity.RoleAdmin {
		return actor, target, apperrors.ErrForbidden
	}
	for _, permission := range []string{"members.read", "members.approvals.write"} {
		ok, err := exactGovernancePermission(tx.Session(&gorm.Session{NewDB: true}), actor, permission)
		if err != nil {
			return actor, target, err
		}
		if !ok {
			return actor, target, apperrors.ErrForbidden
		}
	}
	if target.CreatedAt.IsZero() || target.Role != entity.RoleAdmin && target.Role != entity.RoleMember || !utf8.ValidString(target.Name) || len(target.Name) > 64*1024 || target.OffboardedAt != nil && (!target.Disabled || target.OffboardedAt.IsZero()) {
		return actor, target, catalogConflict
	}
	return actor, target, nil
}
func registrationLinkedApplication(tx *gorm.DB, u entity.User, lock bool) (*entity.RegistrationApprovalApplication, error) {
	if u.ApprovalApplicationID == nil {
		return nil, nil
	}
	if !registrationApplicationID(*u.ApprovalApplicationID) {
		return nil, catalogConflict
	}
	var a entity.RegistrationApprovalApplication
	q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, *u.ApprovalApplicationID))
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, catalogConflict
		}
		return nil, err
	}
	if !registrationApplicationValid(u, a) {
		return nil, catalogConflict
	}
	return &a, nil
}
func registrationApprovalETag(actor, target entity.User, a *entity.RegistrationApprovalApplication, permissionProof string) string {
	// LastLoginAt and runtime lease are intentionally excluded; metadata/lifecycle
	// and durable role/permission definitions remain changing-review fences.
	raw, _ := json.Marshal(struct {
		Version                           string
		ActorID, ActorRole, ActorRevision string
		ActorCreated                      time.Time
		TargetID, TargetName, TargetRole  string
		TargetCreated, TargetUpdated      time.Time
		Disabled                          bool
		Offboarded                        *time.Time
		Link                              *string
		Application                       *entity.RegistrationApprovalApplication
		Proof                             string
	}{"member.approval.v1", actor.ID, actor.Role, actor.MemberRoleRevision, actor.CreatedAt.UTC(), target.ID, target.Name, target.Role, target.CreatedAt.UTC(), target.UpdatedAt.UTC(), target.Disabled, utcMemberMetadataTime(target.OffboardedAt), target.ApprovalApplicationID, a, permissionProof})
	// Application fields are private json:-; bind their exact values explicitly.
	if a != nil {
		extra, _ := json.Marshal([]any{a.ID, a.UserID, a.UserCreatedAt.UTC(), a.CreatedAt.UTC(), a.State, a.Revision, a.DecidedAt, a.DecisionActorID, a.DecisionReason})
		raw = append(raw, extra...)
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func registrationReviewerProof(tx *gorm.DB, actor entity.User) (string, error) {
	// Both required capabilities are builtin-only for this workflow. Fence the
	// actual builtin definition revision and exact retained permission rows.
	var role entity.Role
	if err := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, "rol_admin")).Take(&role).Error; err != nil {
		return "", err
	}
	if role.ID != "rol_admin" || !role.Builtin || !validMemberRoleDigest(role.DefinitionRevision) {
		return "", registrationApprovalUnavailable
	}
	perms, err := rolePermissions(tx.Session(&gorm.Session{NewDB: true}), role.ID)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal([]any{role.DefinitionRevision, perms})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:]), nil
}
func (s *Service) approvalRecord(ctx context.Context, tx *gorm.DB, actor, target entity.User, a *entity.RegistrationApprovalApplication) (*MemberApprovalRecord, error) {
	apps := map[string]entity.RegistrationApprovalApplication{}
	if a != nil {
		apps[a.ID] = *a
	}
	summary, proof := registrationAdmission(target, apps)
	if summary.Status == "unknown" {
		return nil, catalogConflict
	}
	r := &MemberApprovalRecord{UserID: target.ID, Name: target.Name, IdentityRole: target.Role, Disabled: target.Disabled, OffboardedAt: utcMemberMetadataTime(target.OffboardedAt), ApprovalStatus: summary.Status, AdmissionEligible: summary.AdmissionEligible}
	if a != nil {
		r.Application = &MemberApprovalApplication{a.ID, a.State, a.CreatedAt.UTC(), utcMemberMetadataTime(a.DecidedAt), a.DecisionActorID, a.DecisionReason}
		r.CanApprove = a.State == "pending" && target.OffboardedAt == nil
		r.CanReject = a.State == "pending"
	}
	p, err := registrationReviewerProof(tx, actor)
	if err != nil {
		return nil, err
	}
	r.ReviewETag = registrationApprovalETag(actor, target, a, p)
	r.RuntimeApplied = s.registrationAdmissionPublished(ctx, target.ID, proof)
	return r, nil
}
func (s *Service) GetMemberApproval(ctx context.Context, actorID, userID string) (*MemberApprovalRecord, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	var result *MemberApprovalRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, target, err := registrationDecisionPeople(tx, actorID, userID, false)
		if err != nil {
			return err
		}
		a, err := registrationLinkedApplication(tx, target, false)
		if err != nil {
			return err
		}
		result, err = s.approvalRecord(ctx, tx, actor, target, a)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, registrationApprovalError(err)
}
func (s *Service) SetMemberApproval(ctx context.Context, actorID, userID, etag string, in MemberApprovalInput) (*MemberApprovalResult, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	if !validMemberRoleDigest(etag) || !validMemberApprovalInput(in) {
		return nil, apperrors.ErrBadRequest
	}
	desired := "approved"
	if in.Decision == "reject" {
		desired = "rejected"
	}
	release := s.pinPersonalKeyMutation()
	defer release()
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, target, err := registrationDecisionPeople(tx, actorID, userID, true)
		if err != nil {
			return err
		}
		a, err := registrationLinkedApplication(tx, target, true)
		if err != nil {
			return err
		}
		if a == nil {
			return catalogConflict
		}
		if a.State == desired {
			return nil
		}
		if a.State != "pending" || desired == "approved" && target.OffboardedAt != nil {
			return catalogConflict
		}
		r, err := s.approvalRecord(ctx, tx, actor, target, a)
		if err != nil {
			return err
		}
		if r.ReviewETag != etag {
			return catalogConflict
		}
		revision, err := newMemberRoleRevision()
		if err != nil {
			return err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		result := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.RegistrationApprovalApplication{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, a.ID)).Where("revision = ? AND state = ?", a.Revision, "pending").UpdateColumns(map[string]any{"State": desired, "Revision": revision, "DecidedAt": now, "DecisionActorID": actor.ID, "DecisionReason": in.Reason})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return catalogConflict
		}
		return appendRegistrationDecisionAudit(tx, actor.ID, target.ID, a.ID, a.State, desired, in.Reason)
	})
	if err != nil {
		return nil, registrationApprovalError(err)
	}
	release()
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, registrationApprovalError(err)
	}
	current, err := s.GetMemberApproval(ctx, actorID, userID)
	if err != nil {
		return nil, err
	}
	if current.Application == nil || current.Application.State != desired {
		return nil, catalogConflict
	}
	if !current.RuntimeApplied {
		return nil, registrationApprovalUnavailable
	}
	return &MemberApprovalResult{"current_account_approval", current.UserID, current.Application.ID, desired, current.AdmissionEligible, true}, nil
}
