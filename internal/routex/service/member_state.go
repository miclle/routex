package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemberStateRecord struct {
	UserID                      string     `json:"user_id"`
	Name                        string     `json:"name"`
	BaseRole                    string     `json:"base_role"`
	Disabled                    bool       `json:"disabled"`
	OffboardedAt                *time.Time `json:"offboarded_at"`
	Status                      string     `json:"status"`
	CanChangeBaseRole           bool       `json:"can_change_base_role"`
	CanChangeStatus             bool       `json:"can_change_status"`
	ActivationMode              *string    `json:"activation_mode"`
	ETag                        string     `json:"etag"`
	AccountAccessRuntimeApplied bool       `json:"account_access_runtime_applied"`
}
type MemberStateResult struct {
	MemberStateRecord
	Confirmation string `json:"confirmation"`
	Effect       string `json:"effect"`
}
type MemberStateInput struct {
	Role     *string `json:"role,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
	Reason   string  `json:"reason"`
}

var memberStateUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "member state confirmation unavailable"}

func (input *MemberStateInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := memberRolesObject(raw)
	if err != nil || len(fields) != 2 {
		return apperrors.ErrBadRequest
	}
	var next MemberStateInput
	for name, value := range fields {
		if string(value) == "null" {
			return apperrors.ErrBadRequest
		}
		switch name {
		case "role":
			var v string
			if json.Unmarshal(value, &v) != nil {
				return apperrors.ErrBadRequest
			}
			next.Role = &v
		case "disabled":
			var v bool
			if json.Unmarshal(value, &v) != nil {
				return apperrors.ErrBadRequest
			}
			next.Disabled = &v
		case "reason":
			if json.Unmarshal(value, &next.Reason) != nil {
				return apperrors.ErrBadRequest
			}
		default:
			return apperrors.ErrBadRequest
		}
	}
	if !validMemberStateInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func validMemberStateInput(input MemberStateInput) bool {
	return (input.Role == nil) != (input.Disabled == nil) && (input.Role == nil || *input.Role == entity.RoleAdmin || *input.Role == entity.RoleMember) && validCredentialMetadataReason(input.Reason) && strings.TrimSpace(input.Reason) == input.Reason
}
func memberStateError(err error) error {
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
	return memberStateUnavailable
}
func memberStateUserQuery(tx *gorm.DB, id string, lock bool) *gorm.DB {
	q := memberRolesExact(memberRolesDB(tx).Model(&entity.User{}), "id", id).Select("ID", "Name", "Role", "Disabled", "OffboardedAt", "CreatedAt", "ApprovalApplicationID", "UpdatedAt", "MemberRoleRevision")
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return q
}
func memberStatePeople(tx *gorm.DB, actorID, userID string, writeOnly, lock bool) (entity.User, entity.User, bool, error) {
	var actor, target entity.User
	readUser := func(id string) (entity.User, error) {
		var u entity.User
		err := memberStateUserQuery(tx, id, lock).First(&u).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && u.ID != id {
			if id == actorID {
				return u, apperrors.ErrUnauthorized
			}
			return u, apperrors.ErrNotFound
		}
		return u, err
	}
	if lock {
		ids := []string{actorID, userID}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		for _, id := range ids {
			u, err := readUser(id)
			if err != nil {
				return actor, target, false, err
			}
			if id == actorID {
				actor = u
			}
			if id == userID {
				target = u
			}
		}
	} else {
		var err error
		actor, err = readUser(actorID)
		if err != nil {
			return actor, target, false, err
		}
	}
	if actor.Disabled || actor.OffboardedAt != nil || actor.CreatedAt.IsZero() || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember || !validMemberRoleDigest(actor.MemberRoleRevision) {
		return actor, target, false, apperrors.ErrUnauthorized
	}
	write, err := exactGovernancePermission(memberRolesDB(tx), actor, "members.write")
	if err != nil {
		return actor, target, false, err
	}
	if !write {
		if writeOnly {
			return actor, target, false, apperrors.ErrForbidden
		}
		read, err := exactGovernancePermission(memberRolesDB(tx), actor, "members.read")
		if err != nil {
			return actor, target, false, err
		}
		if !read {
			return actor, target, false, apperrors.ErrForbidden
		}
	}
	if !lock {
		if actorID == userID {
			target = actor
		} else {
			target, err = readUser(userID)
			if err != nil {
				return actor, target, false, err
			}
		}
	}
	if target.CreatedAt.IsZero() || target.Role != entity.RoleAdmin && target.Role != entity.RoleMember || !utf8.ValidString(target.Name) || len(target.Name) > 64*1024 || !validMemberRoleDigest(target.MemberRoleRevision) || target.OffboardedAt != nil && (!target.Disabled || target.OffboardedAt.IsZero()) {
		return actor, target, false, memberStateUnavailable
	}
	return actor, target, write, nil
}
func memberStateRecord(actor, target entity.User, write bool) MemberStateRecord {
	r := MemberStateRecord{UserID: target.ID, Name: target.Name, BaseRole: target.Role, Disabled: target.Disabled, OffboardedAt: utcMemberMetadataTime(target.OffboardedAt), Status: "active", CanChangeBaseRole: write && actor.Role == entity.RoleAdmin, CanChangeStatus: write && (actor.Role == entity.RoleAdmin || actor.ID != target.ID && target.Role == entity.RoleMember)}
	if target.Disabled {
		mode := "enable"
		r.Status = "disabled"
		r.ActivationMode = &mode
	}
	if target.OffboardedAt != nil {
		mode := "reactivate"
		r.Status = "offboarded"
		r.ActivationMode = &mode
	}
	raw, _ := json.Marshal(struct {
		Version                                       string
		ActorID, ActorRole, ActorRevision             string
		ActorCreatedAt                                time.Time
		ActorDisabled                                 bool
		ActorOffboardedAt                             *time.Time
		CanRole, CanStatus                            bool
		SubjectID, SubjectRole, SubjectRevision, Name string
		SubjectCreatedAt, SubjectUpdatedAt            time.Time
		SubjectDisabled                               bool
		SubjectOffboardedAt                           *time.Time
	}{"member.state.v1", actor.ID, actor.Role, actor.MemberRoleRevision, actor.CreatedAt.UTC(), actor.Disabled, utcMemberMetadataTime(actor.OffboardedAt), r.CanChangeBaseRole, r.CanChangeStatus, target.ID, target.Role, target.MemberRoleRevision, target.Name, target.CreatedAt.UTC(), target.UpdatedAt.UTC(), target.Disabled, utcMemberMetadataTime(target.OffboardedAt)})
	digest := sha256.Sum256(raw)
	r.ETag = hex.EncodeToString(digest[:])
	return r
}
func (s *Service) GetMemberState(ctx context.Context, actorID, userID string) (*MemberStateRecord, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	var result MemberStateRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, target, write, err := memberStatePeople(tx, actorID, userID, false, false)
		if err != nil {
			return err
		}
		result = memberStateRecord(actor, target, write)
		result.AccountAccessRuntimeApplied = s.memberStateRuntimeApplied(ctx, target)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, memberStateError(err)
	}
	return &result, nil
}
