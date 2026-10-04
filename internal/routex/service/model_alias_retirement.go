package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type ModelAliasRetirementInput struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

func (input *ModelAliasRetirementInput) UnmarshalJSON(data []byte) error {
	values, err := personalModelStringObject(data, []string{"name", "reason"}, nil)
	if err != nil {
		return err
	}
	if !publicModelName.MatchString(values["name"]) || !validCredentialMetadataReason(values["reason"]) || strings.TrimSpace(values["reason"]) == "" {
		return apperrors.ErrBadRequest
	}
	*input = ModelAliasRetirementInput{Name: values["name"], Reason: values["reason"]}
	return nil
}

type ModelAliasRetirementName struct {
	Name      string     `json:"name"`
	IsCurrent bool       `json:"is_current"`
	ExpiresAt *time.Time `json:"expires_at"`
}
type ModelAliasRetirementReview struct {
	ModelID        string                   `json:"model_id"`
	CurrentName    string                   `json:"current_name"`
	Alias          ModelAliasRetirementName `json:"alias"`
	State          string                   `json:"state"`
	ObservedAt     time.Time                `json:"observed_at"`
	ETag           string                   `json:"etag"`
	CanRetire      bool                     `json:"can_retire"`
	RuntimeApplied bool                     `json:"runtime_applied"`
}
type ModelAliasRetirementResult struct {
	Alias          *ModelAliasRetirementReview `json:"alias"`
	Retired        bool                        `json:"retired"`
	Changed        bool                        `json:"changed"`
	RuntimeApplied bool                        `json:"runtime_applied"`
}

type modelAliasSubject struct {
	Model    entity.Model
	Current  entity.ModelName
	Selected entity.ModelName
}

func normalizeAliasName(row entity.ModelName) entity.ModelName {
	row.CreatedAt = row.CreatedAt.UTC()
	if row.ExpiresAt != nil {
		value := row.ExpiresAt.UTC()
		row.ExpiresAt = &value
	}
	if row.CurrentModelID != nil {
		value := *row.CurrentModelID
		row.CurrentModelID = &value
	}
	return row
}
func modelAliasReview(subject modelAliasSubject, now time.Time, canWrite bool) *ModelAliasRetirementReview {
	subject.Model.CreatedAt = subject.Model.CreatedAt.UTC()
	subject.Current = normalizeAliasName(subject.Current)
	subject.Selected = normalizeAliasName(subject.Selected)
	state := "retired"
	if subject.Selected.CurrentModelID != nil {
		state = "current"
	} else if subject.Selected.ExpiresAt != nil && now.Before(*subject.Selected.ExpiresAt) {
		state = "compatibility"
	}
	return &ModelAliasRetirementReview{ModelID: subject.Model.ID, CurrentName: subject.Current.Name,
		Alias: ModelAliasRetirementName{Name: subject.Selected.Name, IsCurrent: state == "current", ExpiresAt: subject.Selected.ExpiresAt},
		State: state, ObservedAt: now.UTC(), ETag: personalHash(subject), CanRetire: canWrite && state == "compatibility"}
}

func loadModelAliasSubject(tx *gorm.DB, modelID, name string, locking bool) (modelAliasSubject, error) {
	var subject modelAliasSubject
	// An initialized locking query is not reusable: each query gets independent
	// model/WHERE state while preserving transaction, context and lock clauses.
	query := tx.Session(&gorm.Session{})
	if locking {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"}).Session(&gorm.Session{})
	}
	if err := personalExact(query, "id", modelID).Take(&subject.Model).Error; err != nil {
		return subject, err
	}
	if subject.Model.ID != modelID {
		return subject, apperrors.ErrNotFound
	}
	if err := personalExact(query, "current_model_id", modelID).Take(&subject.Current).Error; err != nil {
		return subject, err
	}
	if subject.Current.ModelID != modelID || subject.Current.CurrentModelID == nil || *subject.Current.CurrentModelID != modelID {
		return subject, apperrors.ErrNotFound
	}
	if err := personalExact(personalExact(query, "name", name), "model_id", modelID).Take(&subject.Selected).Error; err != nil {
		return subject, err
	}
	if subject.Selected.Name != name || subject.Selected.ModelID != modelID || subject.Selected.CurrentModelID != nil && (*subject.Selected.CurrentModelID != modelID || name != subject.Current.Name) {
		return subject, apperrors.ErrNotFound
	}
	return subject, nil
}

func (s *Service) GetModelAliasRetirement(ctx context.Context, actorID, modelID, name string) (*ModelAliasRetirementReview, error) {
	if !validAdminModelTarget(modelID) || !publicModelName.MatchString(name) {
		return nil, apperrors.ErrBadRequest
	}
	var subject modelAliasSubject
	var canWrite bool
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := exactCatalogPermission(tx, actorID, "models.read_all"); err != nil {
			return err
		}
		actor, err := exactEnabledActor(tx, actorID)
		if err != nil {
			return err
		}
		canWrite, err = exactGovernancePermission(tx, actor, "models.write")
		if err != nil {
			return err
		}
		subject, err = loadModelAliasSubject(tx, modelID, name, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	review := modelAliasReview(subject, time.Now(), canWrite)
	review.RuntimeApplied = s.runtimeModelAliasApplied(subject)
	return review, nil
}

func (s *Service) RetireModelAlias(ctx context.Context, actorID, modelID, etag string, input ModelAliasRetirementInput) (*ModelAliasRetirementResult, error) {
	if !validAdminModelTarget(modelID) || !publicModelName.MatchString(input.Name) || !personalModelETag(etag) || !validCredentialMetadataReason(input.Reason) || strings.TrimSpace(input.Reason) == "" {
		return nil, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	var subject modelAliasSubject
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := exactCatalogPermission(tx, actorID, "models.write"); err != nil {
			return err
		}
		var err error
		subject, err = loadModelAliasSubject(tx, modelID, input.Name, true)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		review := modelAliasReview(subject, now, true)
		if review.State == "current" {
			return catalogConflict
		}
		// Only the current desired state is reconciled; this is not a receipt.
		if review.State == "retired" {
			return nil
		}
		if review.ETag != etag {
			return catalogConflict
		}
		before := subject.Selected.ExpiresAt
		// Existing name timestamps may persist at millisecond precision. Round
		// down so a stopped name never acquires a future rounded deadline.
		if err := personalExact(personalExact(tx.Model(&entity.ModelName{}), "name", input.Name), "model_id", modelID).Update("expires_at", now.Truncate(time.Millisecond)).Error; err != nil {
			return err
		}
		subject, err = loadModelAliasSubject(tx, modelID, input.Name, true)
		if err != nil {
			return err
		}
		changed = true
		return appendModelAliasRetirementAudit(tx, actorID, modelID, input.Name, before, subject.Selected.ExpiresAt, input.Reason)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	s.InvalidateRuntimeModel(modelID)
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
	}
	review := modelAliasReview(subject, time.Now(), true)
	review.RuntimeApplied = s.runtimeModelAliasApplied(subject)
	return &ModelAliasRetirementResult{Alias: review, Retired: review.State == "retired", Changed: changed, RuntimeApplied: review.RuntimeApplied}, nil
}

type modelAliasRetirementAudit struct {
	ModelID         string     `json:"model_id"`
	Name            string     `json:"name"`
	BeforeExpiresAt *time.Time `json:"before_expires_at"`
	AfterExpiresAt  *time.Time `json:"after_expires_at"`
	Reason          string     `json:"reason"`
}

func appendModelAliasRetirementAudit(tx *gorm.DB, actorID, modelID, name string, before, after *time.Time, reason string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(modelAliasRetirementAudit{modelID, name, before, after, reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "model.alias.retire", ResourceType: "model", ResourceID: modelID, DetailsJSON: &raw}).Error
}
func modelAliasRetirementAuditProjection(row entity.AuditEvent) (any, bool) {
	var value modelAliasRetirementAudit
	if row.DetailsJSON == nil || json.Unmarshal([]byte(*row.DetailsJSON), &value) != nil || row.ResourceType != "model" || row.ResourceID != value.ModelID || !validAdminModelTarget(value.ModelID) || !publicModelName.MatchString(value.Name) || !validCredentialMetadataReason(value.Reason) || strings.TrimSpace(value.Reason) == "" || value.BeforeExpiresAt == nil || value.AfterExpiresAt == nil || !value.AfterExpiresAt.Before(*value.BeforeExpiresAt) {
		return nil, false
	}
	return value, true
}
