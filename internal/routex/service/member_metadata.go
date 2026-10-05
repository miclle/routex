package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemberMetadataRecord struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	CanEdit bool   `json:"can_edit"`
	ETag    string `json:"etag"`
}
type MemberMetadataInput struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}
type MemberMetadataWriteResult struct {
	MemberMetadataRecord
	Confirmation string `json:"confirmation"`
}

var memberMetadataUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "member metadata confirmation unavailable"}

func (input *MemberMetadataInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"name", "reason"})
	if err != nil {
		return err
	}
	var next MemberMetadataInput
	for name, target := range map[string]*string{"name": &next.Name, "reason": &next.Reason} {
		if string(fields[name]) == "null" || json.Unmarshal(fields[name], target) != nil {
			return apperrors.ErrBadRequest
		}
	}
	if !validMemberMetadataInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func validMemberMetadataInput(input MemberMetadataInput) bool {
	return validCatalogLabel(input.Name) && validCredentialMetadataReason(input.Reason) && strings.TrimSpace(input.Reason) == input.Reason
}
func memberMetadataEditable(actor, target entity.User, write bool) bool {
	return write && (target.Role == entity.RoleAdmin || target.Role == entity.RoleMember) && !actor.Disabled && actor.OffboardedAt == nil && (actor.Role == entity.RoleAdmin || actor.Role == entity.RoleMember && actor.ID != target.ID && target.Role == entity.RoleMember) && target.OffboardedAt == nil
}
func memberMetadataRecord(actor, target entity.User, write bool) MemberMetadataRecord {
	status := "active"
	if target.Disabled {
		status = "disabled"
	}
	if target.OffboardedAt != nil {
		status = "offboarded"
	}
	canEdit := memberMetadataEditable(actor, target, write)
	// Private identity/resource revisions never enter this label-only review.
	raw, _ := json.Marshal(struct {
		Version              string
		ActorID, ActorRole   string
		CanEdit              bool
		UserID, Name, Role   string
		Disabled             bool
		OffboardedAt         *time.Time
		CreatedAt, UpdatedAt time.Time
	}{"member.metadata.v1", actor.ID, actor.Role, canEdit, target.ID, target.Name, target.Role, target.Disabled, utcMemberMetadataTime(target.OffboardedAt), target.CreatedAt.UTC(), target.UpdatedAt.UTC()})
	hash := sha256.Sum256(raw)
	return MemberMetadataRecord{UserID: target.ID, Name: target.Name, Status: status, CanEdit: canEdit, ETag: hex.EncodeToString(hash[:])}
}
func utcMemberMetadataTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	next := value.UTC()
	return &next
}
func memberMetadataUserQuery(tx *gorm.DB, userID string) *gorm.DB {
	return tx.Session(&gorm.Session{}).Model(&entity.User{}).Select("id", "name", "role", "disabled", "offboarded_at", "created_at", "updated_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, userID))
}
func memberMetadataPeople(tx *gorm.DB, actorID, userID string, writeOnly, lock bool) (entity.User, entity.User, bool, error) {
	var actor, target entity.User
	err := memberMetadataUserQuery(tx, actorID).Where("disabled = ? AND offboarded_at IS NULL", false).First(&actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (actor.ID != actorID || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember) {
		return actor, target, false, apperrors.ErrUnauthorized
	}
	if err != nil {
		return actor, target, false, err
	}
	write, err := exactGovernancePermission(tx.Session(&gorm.Session{NewDB: true}), actor, "members.write")
	if err != nil {
		return actor, target, false, err
	}
	if !write {
		if writeOnly {
			return actor, target, false, apperrors.ErrForbidden
		}
		read, err := exactGovernancePermission(tx.Session(&gorm.Session{NewDB: true}), actor, "members.read")
		if err != nil {
			return actor, target, false, err
		}
		if !read {
			return actor, target, false, apperrors.ErrForbidden
		}
	}
	query := memberMetadataUserQuery(tx, userID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&target).Error; err != nil {
		return actor, target, false, err
	}
	if target.ID != userID || target.CreatedAt.IsZero() || target.Role != entity.RoleAdmin && target.Role != entity.RoleMember {
		return actor, target, false, apperrors.ErrNotFound
	}
	if writeOnly {
		if actor.Role != entity.RoleAdmin && (actor.ID == target.ID || target.Role == entity.RoleAdmin) {
			return actor, target, false, apperrors.ErrForbidden
		}
		if target.OffboardedAt != nil {
			return actor, target, false, catalogConflict
		}
	}
	return actor, target, write, nil
}
func memberMetadataIDs(actorID, userID string) error {
	if !safeTeamSessionID(actorID) {
		return apperrors.ErrUnauthorized
	}
	if !safeTeamSessionID(userID) {
		return apperrors.ErrBadRequest
	}
	return nil
}
func (s *Service) GetMemberMetadata(ctx context.Context, actorID, userID string) (*MemberMetadataRecord, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	var result MemberMetadataRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, target, write, err := memberMetadataPeople(tx, actorID, userID, false, false)
		if err != nil {
			return err
		}
		result = memberMetadataRecord(actor, target, write)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return &result, nil
}
func memberMetadataReview(actor, target entity.User, input MemberMetadataInput, etag string) error {
	if !memberMetadataEditable(actor, target, true) {
		if target.OffboardedAt != nil {
			return catalogConflict
		}
		return apperrors.ErrForbidden
	}
	if target.Name != input.Name && memberMetadataRecord(actor, target, true).ETag != etag {
		return catalogConflict
	}
	return nil
}
func (s *Service) SetMemberMetadata(ctx context.Context, actorID, userID, etag string, input MemberMetadataInput) (*MemberMetadataWriteResult, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	if !validMemberMetadataInput(input) || !memberKeyStrongETag.MatchString(etag) {
		return nil, apperrors.ErrBadRequest
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, target, _, err := memberMetadataPeople(tx, actorID, userID, true, true)
		if err != nil {
			return err
		}
		if err := memberMetadataReview(actor, target, input, etag); err != nil {
			return err
		}
		if target.Name == input.Name {
			return nil
		}
		if err := memberMetadataUserQuery(tx, userID).Updates(map[string]any{"name": input.Name}).Error; err != nil {
			return err
		}
		return appendMemberMetadataAudit(tx, actor.ID, target, input)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	// A response confirms fresh CURRENT state, never the identity of an earlier
	// operation. Reload persisted precision and recheck even after a no-op.
	var result MemberMetadataWriteResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, target, write, err := memberMetadataPeople(tx, actorID, userID, true, true)
		if err != nil {
			return err
		}
		if target.Name != input.Name {
			return catalogConflict
		}
		result = MemberMetadataWriteResult{MemberMetadataRecord: memberMetadataRecord(actor, target, write), Confirmation: "current_member_name"}
		return nil
	})
	if err != nil {
		var app *apperrors.Error
		if errors.As(err, &app) {
			return nil, app
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrNotFound
		}
		return nil, memberMetadataUnavailable
	}
	return &result, nil
}
