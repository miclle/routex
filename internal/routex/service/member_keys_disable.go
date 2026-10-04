package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type MemberKeyDisableInput struct {
	Reason string `json:"reason"`
}

type MemberKeyDisableRecord struct {
	UserID         string `json:"user_id"`
	ID             string `json:"id"`
	Status         string `json:"status"`
	ETag           string `json:"etag"`
	RuntimeApplied bool   `json:"runtime_applied"`
	Confirmation   string `json:"confirmation"`
}

func memberKeyDisableReview(actor, subject entity.User, key entity.APIKey, models []string, etag string, now time.Time) error {
	if (actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember) || (subject.Role != entity.RoleAdmin && subject.Role != entity.RoleMember) || actor.Disabled || actor.OffboardedAt != nil || (actor.Role != entity.RoleAdmin && (actor.ID == subject.ID || subject.Role == entity.RoleAdmin)) {
		return apperrors.ErrForbidden
	}
	if key.UserID != subject.ID {
		return apperrors.ErrNotFound
	}
	if subject.Disabled || subject.OffboardedAt != nil || !memberKeyRevision.MatchString(key.LifecycleRevision) || (key.ExpiresAt != nil && !now.Before(*key.ExpiresAt)) {
		return errKeyConflict
	}
	if key.Status == entity.KeyDisabled {
		// A retained disabled target may confirm CURRENT state on an authorized
		// retry. This cannot prove an earlier actor, operation or historical receipt.
		return nil
	}
	if key.Status != entity.KeyActive || memberKeyETag(key, models) != etag {
		return errKeyConflict
	}
	return nil
}

func lockMemberKey(tx *gorm.DB, subjectID, keyID string) (*entity.APIKey, error) {
	var key entity.APIKey
	if err := tx.Omit("token_hash", "prefix").Clauses(clause.Locking{Strength: "UPDATE"}).First(&key, "id = ? AND user_id = ?", keyID, subjectID).Error; err != nil {
		return nil, err
	}
	if key.ID != keyID || key.UserID != subjectID {
		return nil, apperrors.ErrNotFound
	}
	return &key, nil
}

func memberKeyModels(tx *gorm.DB, keyID string) ([]string, error) {
	var scopes []entity.APIKeyModel
	if err := tx.Where("key_id = ?", keyID).Order("model_id").Find(&scopes).Error; err != nil {
		return nil, err
	}
	models := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if scope.KeyID != keyID {
			return nil, apperrors.ErrNotFound
		}
		models = append(models, scope.ModelID)
	}
	return models, nil
}

func lockMemberKeySubject(tx *gorm.DB, subjectID string) (entity.User, error) {
	var subject entity.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "role", "disabled", "offboarded_at").First(&subject, "id = ?", subjectID).Error
	if err == nil && subject.ID != subjectID {
		err = apperrors.ErrNotFound
	}
	return subject, err
}

func (s *Service) DisableMemberKey(ctx context.Context, actorID, subjectID, keyID, etag string, input MemberKeyDisableInput) (*MemberKeyDisableRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !memberKeyUserID.MatchString(actorID) || !memberKeyUserID.MatchString(subjectID) || !memberKeyID.MatchString(keyID) {
		return nil, apperrors.ErrNotFound
	}
	if !memberKeyStrongETag.MatchString(etag) || !validCredentialMetadataReason(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	releasePublication := s.pinPersonalKeyMutation()
	defer releasePublication()
	var changed *entity.APIKey
	var models []string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, subject, err := memberKeyPeople(tx, actorID, subjectID, "members.keys.disable")
		if err != nil {
			return err
		}
		subject, err = lockMemberKeySubject(tx, subject.ID)
		if err != nil {
			return err
		}
		key, err := lockMemberKey(tx, subject.ID, keyID)
		if err != nil {
			return err
		}
		models, err = memberKeyModels(tx, key.ID)
		if err != nil {
			return err
		}
		if err := memberKeyDisableReview(actor, subject, *key, models, etag, time.Now()); err != nil {
			return err
		}
		before := *key
		if key.Status != entity.KeyDisabled {
			if err := changePersonalKeyStatus(tx, key, entity.KeyDisabled); err != nil {
				return err
			}
			if err := appendMemberKeyDisableAudit(tx, actor.ID, before, *key, models, input.Reason); err != nil {
				return err
			}
		}
		changed = key
		return nil
	})
	if err != nil {
		return nil, catalogError(err)
	}
	// All product Key writers hold the same gate through commit and tombstones.
	// A newer owner re-enable cannot precede this older reduction's tombstone.
	s.InvalidateRuntimeKey(keyID)
	releasePublication()
	if s.runtime == nil {
		return nil, runtimeUnavailable
	}
	if err := s.refreshAfterMutation(ctx, nil); err != nil {
		return nil, err
	}
	return s.confirmMemberKeyDisabled(ctx, actorID, subjectID, *changed, models)
}

func (s *Service) confirmMemberKeyDisabled(ctx context.Context, actorID, subjectID string, committed entity.APIKey, models []string) (*MemberKeyDisableRecord, error) {
	// Prevent a concurrent product state writer from changing the exact revision
	// while current persisted state and the published private proof are compared.
	releasePublication := s.pinPersonalKeyMutation()
	defer releasePublication()
	var result *MemberKeyDisableRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, subject, err := memberKeyPeople(tx, actorID, subjectID, "members.keys.disable")
		if err != nil {
			return err
		}
		subject, err = lockMemberKeySubject(tx, subject.ID)
		if err != nil {
			return err
		}
		current, err := lockMemberKey(tx, subject.ID, committed.ID)
		if err != nil {
			return err
		}
		currentModels, err := memberKeyModels(tx, current.ID)
		if err != nil {
			return err
		}
		if memberKeyETag(*current, currentModels) != memberKeyETag(committed, models) {
			return errKeyConflict
		}
		if err := memberKeyDisableReview(actor, subject, *current, currentModels, memberKeyETag(committed, models), time.Now()); err != nil {
			return err
		}
		if current.Status != entity.KeyDisabled || current.LifecycleRevision != committed.LifecycleRevision {
			return errKeyConflict
		}
		if s.runtime == nil || !s.RuntimeStatus().Ready || !publishedMemberKeyDisabled(s.runtime.auth.Load(), *current, time.Now()) {
			return runtimeUnavailable
		}
		result = &MemberKeyDisableRecord{UserID: subject.ID, ID: current.ID, Status: entity.KeyDisabled, ETag: memberKeyETag(*current, currentModels), RuntimeApplied: true, Confirmation: "current_disabled_state"}
		return nil
	})
	return result, catalogError(err)
}

type MemberKeyDisableAudit struct {
	UserID       string `json:"user_id"`
	BeforeStatus string `json:"before_status"`
	AfterStatus  string `json:"after_status"`
	BeforeETag   string `json:"before_etag"`
	AfterETag    string `json:"after_etag"`
	Reason       string `json:"reason"`
}

func appendMemberKeyDisableAudit(tx *gorm.DB, actorID string, before, after entity.APIKey, models []string, reason string) error {
	encoded, err := json.Marshal(MemberKeyDisableAudit{UserID: after.UserID, BeforeStatus: before.Status, AfterStatus: after.Status, BeforeETag: memberKeyETag(before, models), AfterETag: memberKeyETag(after, models), Reason: reason})
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "member.key.disable", ResourceType: "api_key", ResourceID: after.ID, DetailsJSON: &raw}).Error
}

func memberKeyDisableAuditProjection(row entity.AuditEvent) (MemberKeyDisableAudit, bool) {
	var result MemberKeyDisableAudit
	if row.DetailsJSON == nil || row.ResourceType != "api_key" || !memberKeyID.MatchString(row.ResourceID) {
		return result, false
	}
	err := json.Unmarshal([]byte(*row.DetailsJSON), &result)
	return result, err == nil && memberKeyUserID.MatchString(result.UserID) && result.BeforeStatus == entity.KeyActive && result.AfterStatus == entity.KeyDisabled && memberKeyStrongETag.MatchString(result.BeforeETag) && memberKeyStrongETag.MatchString(result.AfterETag) && result.BeforeETag != result.AfterETag && validCredentialMetadataReason(result.Reason)
}
