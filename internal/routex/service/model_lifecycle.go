package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ModelLifecycleInput struct {
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func (input *ModelLifecycleInput) UnmarshalJSON(data []byte) error {
	values, err := personalModelStringObject(data, []string{"status", "reason"}, nil)
	if err != nil {
		return err
	}
	if !knownModelLifecycleStatus(values["status"]) || !validCredentialMetadataReason(values["reason"]) || strings.TrimSpace(values["reason"]) == "" {
		return apperrors.ErrBadRequest
	}
	*input = ModelLifecycleInput{Status: values["status"], Reason: values["reason"]}
	return nil
}

type ModelLifecycleReview struct {
	ModelID        string    `json:"model_id"`
	CurrentName    string    `json:"current_name"`
	CreatedAt      time.Time `json:"created_at"`
	Status         string    `json:"status"`
	ObservedAt     time.Time `json:"observed_at"`
	ETag           string    `json:"etag"`
	CanChange      bool      `json:"can_change"`
	RuntimeApplied bool      `json:"runtime_applied"`
}
type ModelLifecycleResult struct {
	Model          *ModelLifecycleReview `json:"model"`
	Changed        bool                  `json:"changed"`
	RuntimeApplied bool                  `json:"runtime_applied"`
}
type modelLifecycleSubject struct {
	Model   entity.Model
	Current entity.ModelName
}

func reversibleModelStatus(status string) bool {
	return status == entity.ResourceActive || status == entity.ResourceDisabled
}
func knownModelLifecycleStatus(status string) bool {
	return reversibleModelStatus(status) || status == entity.ResourceArchived
}
func allowedModelLifecycleTransition(before, after string) bool {
	return before != after && (reversibleModelStatus(before) && reversibleModelStatus(after) || before == entity.ResourceDisabled && after == entity.ResourceArchived)
}
func validModelLifecycleETag(etag string) bool {
	return len(etag) == 129 && etag[64] == '.' && personalModelETag(etag[:64]) && personalModelETag(etag[65:])
}
func modelLifecycleIdentity(subject modelLifecycleSubject) string {
	return personalHash(struct {
		ID        string
		CreatedAt time.Time
	}{subject.Model.ID, subject.Model.CreatedAt.UTC()})
}
func modelLifecycleReview(subject modelLifecycleSubject, now time.Time, canWrite bool) *ModelLifecycleReview {
	subject.Model.CreatedAt = subject.Model.CreatedAt.UTC()
	if subject.Model.ConfigUpdatedAt != nil {
		stamp := subject.Model.ConfigUpdatedAt.UTC()
		subject.Model.ConfigUpdatedAt = &stamp
	}
	subject.Current = normalizeAliasName(subject.Current)
	return &ModelLifecycleReview{ModelID: subject.Model.ID, CurrentName: subject.Current.Name, CreatedAt: subject.Model.CreatedAt, Status: subject.Model.Status, ObservedAt: now.UTC(), ETag: modelLifecycleIdentity(subject) + "." + personalHash(struct {
		Status          string
		ConfigUpdatedAt *time.Time
		Current         entity.ModelName
	}{subject.Model.Status, subject.Model.ConfigUpdatedAt, subject.Current}), CanChange: canWrite && reversibleModelStatus(subject.Model.Status)}
}
func loadModelLifecycleSubject(tx *gorm.DB, modelID string, locking bool) (modelLifecycleSubject, error) {
	var subject modelLifecycleSubject
	query := tx.Session(&gorm.Session{})
	if locking {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"}).Session(&gorm.Session{})
	}
	if err := personalExact(query, "id", modelID).Take(&subject.Model).Error; err != nil {
		return subject, err
	}
	if subject.Model.ID != modelID || subject.Model.CreatedAt.IsZero() {
		return subject, apperrors.ErrNotFound
	}
	if !knownModelLifecycleStatus(subject.Model.Status) {
		return subject, runtimeUnavailable
	}
	if err := personalExact(query, "current_model_id", modelID).Take(&subject.Current).Error; err != nil {
		return subject, err
	}
	if subject.Current.ModelID != modelID || subject.Current.CurrentModelID == nil || *subject.Current.CurrentModelID != modelID {
		return subject, apperrors.ErrNotFound
	}
	return subject, nil
}

// Both reads and writes revalidate the exact live Session and actor birth. The
// read path remains read-only; the writer holds governance before row locks.
func (s *Service) modelLifecycleAuthority(tx *gorm.DB, auth *Authentication, permission string, locking bool) (entity.User, error) {
	if auth == nil || auth.User.CreatedAt.IsZero() || auth.Session.CreatedAt.IsZero() || auth.Session.ID == "" {
		return entity.User{}, apperrors.ErrUnauthorized
	}
	actor, err := registrationAdmittedUser(tx, auth.User.ID, locking)
	if err != nil {
		return actor, err
	}
	if actor.ID != auth.User.ID || !actor.CreatedAt.Equal(auth.User.CreatedAt) {
		return actor, apperrors.ErrUnauthorized
	}
	query := tx.Session(&gorm.Session{})
	if locking {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var session entity.Session
	err = personalExact(query, "id", auth.Session.ID).Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return actor, apperrors.ErrUnauthorized
	}
	if err != nil {
		return actor, err
	}
	if session.ID != auth.Session.ID || session.UserID != actor.ID || !authenticationSessionEqual(session, auth.Session) || session.TokenHash != secret.SHA256Hex(auth.Token) || !session.ExpiresAt.After(time.Now()) {
		return actor, apperrors.ErrUnauthorized
	}
	if err := s.primaryValidateSession(tx, session); err != nil {
		return actor, err
	}
	allowed, err := exactGovernancePermission(tx, actor, permission)
	if err != nil {
		return actor, err
	}
	if !allowed {
		return actor, apperrors.ErrForbidden
	}
	return actor, tx.Statement.Context.Err()
}
func modelLifecycleError(err error) error {
	if err == nil {
		return nil
	}
	mapped := catalogError(err)
	if mapped == apperrors.ErrInternal {
		return runtimeUnavailable
	}
	return mapped
}
func (s *Service) GetModelLifecycle(ctx context.Context, auth *Authentication, modelID string) (*ModelLifecycleReview, error) {
	return s.readModelLifecycle(ctx, auth, modelID, "models.read_all")
}
func (s *Service) readModelLifecycle(ctx context.Context, auth *Authentication, modelID, permission string) (*ModelLifecycleReview, error) {
	if !validAdminModelTarget(modelID) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var subject modelLifecycleSubject
	var canWrite bool
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := s.modelLifecycleAuthority(tx, auth, permission, false)
		if err != nil {
			return err
		}
		canWrite, err = exactGovernancePermission(tx, actor, "models.write")
		if err != nil {
			return err
		}
		subject, err = loadModelLifecycleSubject(tx, modelID, false)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, modelLifecycleError(err)
	}
	review := modelLifecycleReview(subject, time.Now(), canWrite)
	review.RuntimeApplied = s.runtimeModelLifecycleApplied(subject)
	return review, nil
}

func modelLifecycleTransition(subject modelLifecycleSubject, etag, status string) (bool, error) {
	if !validModelLifecycleETag(etag) || !knownModelLifecycleStatus(status) {
		return false, apperrors.ErrBadRequest
	}
	if !knownModelLifecycleStatus(subject.Model.Status) || etag[:64] != modelLifecycleIdentity(subject) {
		return false, catalogConflict
	}
	if subject.Model.Status == status {
		return false, nil
	}
	if !allowedModelLifecycleTransition(subject.Model.Status, status) || modelLifecycleReview(subject, time.Now(), true).ETag != etag {
		return false, catalogConflict
	}
	return true, nil
}

func (s *Service) pinModelLifecycle(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.runtime == nil {
		return func() {}, nil
	}
	runtime := s.runtime
	if !runtime.publication.TryLock() {
		return nil, runtimeUnavailable
	}
	var once sync.Once
	return func() { once.Do(runtime.publication.Unlock) }, nil
}

func (s *Service) SetModelLifecycle(ctx context.Context, auth *Authentication, modelID, etag string, input ModelLifecycleInput) (*ModelLifecycleResult, error) {
	if !validAdminModelTarget(modelID) || !validModelLifecycleETag(etag) || !knownModelLifecycleStatus(input.Status) || !validCredentialMetadataReason(input.Reason) || strings.TrimSpace(input.Reason) == "" {
		return nil, apperrors.ErrBadRequest
	}
	input.Reason = strings.TrimSpace(input.Reason)
	local, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	release, err := s.pinModelLifecycle(local)
	if err != nil {
		return nil, modelLifecycleError(err)
	}
	defer release()
	var subject modelLifecycleSubject
	changed := false
	err = s.authDB(local).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, err := s.modelLifecycleAuthority(tx, auth, "models.write", true)
		if err != nil {
			return err
		}
		subject, err = loadModelLifecycleSubject(tx, modelID, true)
		if err != nil {
			return err
		}
		// A retry only reconciles the desired current state of this exact birth.
		// It is not a historical receipt and does not identify who changed it.
		apply, err := modelLifecycleTransition(subject, etag, input.Status)
		if err != nil || !apply {
			return err
		}
		before := subject.Model.Status
		now := time.Now().UTC()
		if err := personalExact(tx.Model(&entity.Model{}), "id", modelID).Update("status", input.Status).Error; err != nil {
			return err
		}
		if err := stampModelConfiguration(tx, modelID, now); err != nil {
			return err
		}
		if input.Status != entity.ResourceActive {
			if err := CancelPersonalModelRequestsForModel(tx, actor.ID, modelID, "model_unavailable"); err != nil {
				return err
			}
			if err := CancelTeamModelRequestsForModel(tx, actor.ID, modelID, "model_unavailable"); err != nil {
				return err
			}
		}
		changed = true
		return appendModelLifecycleAudit(tx, actor.ID, subject.Model, before, input.Status, input.Reason)
	})
	if err != nil {
		return nil, modelLifecycleError(err)
	}
	// Publish the denial before releasing the writer gate, including retries of
	// a committed disable/archive whose earlier refresh did not return successfully.
	if input.Status != entity.ResourceActive {
		s.InvalidateRuntimeModel(modelID)
	}
	release()
	if err := s.refreshRuntime(ctx, true); err != nil {
		return nil, modelLifecycleError(err)
	}
	review, err := s.readModelLifecycle(ctx, auth, modelID, "models.write")
	if err != nil {
		return nil, err
	}
	if !review.CreatedAt.Equal(subject.Model.CreatedAt) || review.Status != input.Status {
		return nil, catalogConflict
	}
	return &ModelLifecycleResult{Model: review, Changed: changed, RuntimeApplied: review.RuntimeApplied}, nil
}
func (s *Service) runtimeModelLifecycleApplied(subject modelLifecycleSubject) bool {
	if s.runtime == nil || !knownModelLifecycleStatus(subject.Model.Status) {
		return false
	}
	if !s.runtime.publication.TryRLock() {
		return false
	}
	defer s.runtime.publication.RUnlock()
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || runtimeDenied(&s.runtime.deniedModels, subject.Model.ID) || !auth.ModelCreated[subject.Model.ID].Equal(subject.Model.CreatedAt) {
		return false
	}
	active, exists := auth.Models[subject.Model.ID]
	current, named := auth.Names[subject.Current.Name]
	return exists && active == (subject.Model.Status == entity.ResourceActive) && named && sameRuntimeAliasName(current, subject.Current)
}

type modelLifecycleAudit struct {
	ModelID   string    `json:"model_id"`
	CreatedAt time.Time `json:"created_at"`
	Before    string    `json:"before"`
	After     string    `json:"after"`
	Reason    string    `json:"reason"`
}

func appendModelLifecycleAudit(tx *gorm.DB, actorID string, model entity.Model, before, after, reason string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(modelLifecycleAudit{model.ID, model.CreatedAt.UTC(), before, after, reason})
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "model.lifecycle.update", ResourceType: "model", ResourceID: model.ID, DetailsJSON: &raw}).Error
}
func modelLifecycleAuditProjection(row entity.AuditEvent) (any, bool) {
	var value modelLifecycleAudit
	if row.DetailsJSON == nil || json.Unmarshal([]byte(*row.DetailsJSON), &value) != nil || row.ResourceType != "model" || row.ResourceID != value.ModelID || !validAdminModelTarget(value.ModelID) || value.CreatedAt.IsZero() || !allowedModelLifecycleTransition(value.Before, value.After) || !validCredentialMetadataReason(value.Reason) || strings.TrimSpace(value.Reason) == "" {
		return nil, false
	}
	return value, true
}
