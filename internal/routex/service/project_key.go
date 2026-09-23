package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
)

type ProjectKeyRecord struct {
	Key      entity.ProjectKey
	ModelIDs []string
}
type CreatedProjectKey struct {
	Record ProjectKeyRecord
	Secret string
}
type ProjectKeyFilter struct {
	Status, Cursor string
	Limit          int
}
type ProjectKeyPage struct {
	Items      []ProjectKeyRecord
	NextCursor string
}

func projectKeyAccess(db *gorm.DB, actorID, projectID string) error {
	allowed, err := resourcePermission(db, actorID, ProjectResource, "write")
	if err != nil {
		return err
	}
	if !allowed {
		allowed, err = resourceManager(db, actorID, projectID)
		if err != nil {
			return err
		}
	}
	if !allowed {
		return apperrors.ErrNotFound
	}
	return nil
}
func lockProjectForKeys(tx *gorm.DB, actorID, projectID string, activeRequired bool) (*entity.Project, error) {
	// Shared with manager replacement, lifecycle changes and account suspension.
	if err := lockGovernance(tx); err != nil {
		return nil, err
	}
	if err := projectKeyAccess(tx, actorID, projectID); err != nil {
		return nil, err
	}
	var project entity.Project
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, "id = ?", projectID).Error; err != nil {
		return nil, err
	}
	if project.Status == entity.ResourceArchived || (activeRequired && project.Status != entity.ResourceActive) {
		return nil, errKeyConflict
	}
	var managers int64
	if err := tx.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND u.disabled = ?", projectID, false).Count(&managers).Error; err != nil {
		return nil, err
	}
	if managers == 0 {
		return nil, errKeyConflict
	}
	return &project, nil
}
func projectKeyModelIDs(db *gorm.DB, keyID string) ([]string, error) {
	result := []string{}
	err := db.Model(&entity.ProjectKeyModel{}).Where("key_id = ?", keyID).Order("model_id").Pluck("model_id", &result).Error
	return result, err
}
func projectKeyRecord(db *gorm.DB, projectID, keyID string, lock bool) (*ProjectKeyRecord, error) {
	query := db.Where("project_id = ? AND id = ?", projectID, keyID)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var key entity.ProjectKey
	if err := query.First(&key).Error; err != nil {
		return nil, err
	}
	models, err := projectKeyModelIDs(db, key.ID)
	return &ProjectKeyRecord{Key: key, ModelIDs: models}, err
}
func validateProjectKeyModels(tx *gorm.DB, projectID string, modelIDs []string) error {
	if len(modelIDs) == 0 || len(modelIDs) > 128 {
		return apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, modelID := range modelIDs {
		if modelID == "" || seen[modelID] {
			return apperrors.ErrBadRequest
		}
		seen[modelID] = true
	}
	var count int64
	if err := tx.Table("project_model_grants g").Joins("JOIN models m ON m.id = g.model_id").Where("g.project_id = ? AND g.model_id IN ? AND m.status = ?", projectID, modelIDs, entity.ResourceActive).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(modelIDs)) {
		return apperrors.ErrForbidden
	}
	return nil
}
func (s *Service) ListProjectKeys(ctx context.Context, actorID, projectID string, filter ProjectKeyFilter) (*ProjectKeyPage, error) {
	if filter.Limit == 0 {
		filter.Limit = 40
	}
	if filter.Limit < 1 || filter.Limit > 100 || len(filter.Cursor) > 30 || (filter.Status != "" && filter.Status != entity.KeyPending && filter.Status != entity.KeyActive && filter.Status != entity.KeyDisabled && filter.Status != entity.KeyRevoked) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := projectKeyAccess(db, actorID, projectID); err != nil {
		return nil, catalogError(err)
	}
	var project entity.Project
	if err := db.First(&project, "id = ?", projectID).Error; err != nil {
		return nil, catalogError(err)
	}
	query := db.Where("project_id = ?", projectID)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Cursor != "" {
		query = query.Where("id < ?", filter.Cursor)
	}
	var keys []entity.ProjectKey
	if err := query.Order("id DESC").Limit(filter.Limit + 1).Find(&keys).Error; err != nil {
		return nil, catalogError(err)
	}
	page := &ProjectKeyPage{Items: []ProjectKeyRecord{}}
	if len(keys) > filter.Limit {
		keys = keys[:filter.Limit]
		page.NextCursor = keys[len(keys)-1].ID
	}
	for _, key := range keys {
		models, err := projectKeyModelIDs(db, key.ID)
		if err != nil {
			return nil, catalogError(err)
		}
		page.Items = append(page.Items, ProjectKeyRecord{Key: key, ModelIDs: models})
	}
	return page, nil
}
func (s *Service) GetProjectKey(ctx context.Context, actorID, projectID, keyID string) (*ProjectKeyRecord, error) {
	db := s.authDB(ctx)
	if err := projectKeyAccess(db, actorID, projectID); err != nil {
		return nil, catalogError(err)
	}
	result, err := projectKeyRecord(db, projectID, keyID, false)
	return result, catalogError(err)
}
func createPendingProjectKey(tx *gorm.DB, actorID, projectID, name string, models []string, expiresAt *time.Time, replaces *string, activate bool) (*CreatedProjectKey, error) {
	keyID, err := id.NewPrefixed("pky")
	if err != nil {
		return nil, err
	}
	random, err := secret.RandomURLSafe(32)
	if err != nil {
		return nil, err
	}
	bearer := "rxp_" + random
	deadline := time.Now().UTC().Add(10 * time.Minute)
	key := entity.ProjectKey{ID: keyID, ProjectID: projectID, CreatorID: actorID, Name: name, Prefix: bearer[:12], TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyPending, DeliveryMode: "manual", ExpiresAt: expiresAt, DeliveryExpiresAt: &deadline, ReplacesKeyID: replaces, ActivateOnConfirm: activate}
	if err := tx.Create(&key).Error; err != nil {
		return nil, err
	}
	for _, modelID := range models {
		if err := tx.Create(&entity.ProjectKeyModel{KeyID: keyID, ModelID: modelID}).Error; err != nil {
			return nil, err
		}
	}
	if err := appendAudit(tx, actorID, "project_key.create", "project_api_key", keyID); err != nil {
		return nil, err
	}
	return &CreatedProjectKey{Record: ProjectKeyRecord{Key: key, ModelIDs: slices.Clone(models)}, Secret: bearer}, nil
}
func (s *Service) CreateProjectKey(ctx context.Context, actorID, projectID, name, deliveryMode string, models []string, expiresAt *time.Time) (*CreatedProjectKey, error) {
	name = strings.TrimSpace(name)
	if !validCatalogLabel(name) || deliveryMode != "manual" || (expiresAt != nil && !expiresAt.After(time.Now())) {
		return nil, apperrors.ErrBadRequest
	}
	var result *CreatedProjectKey
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, true); err != nil {
			return err
		}
		if err := validateProjectKeyModels(tx, projectID, models); err != nil {
			return err
		}
		var err error
		result, err = createPendingProjectKey(tx, actorID, projectID, name, models, expiresAt, nil, true)
		return err
	})
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}
func (s *Service) ConfirmProjectKey(ctx context.Context, actorID, projectID, keyID string) (*ProjectKeyRecord, error) {
	var result *ProjectKeyRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, true); err != nil {
			return err
		}
		record, err := projectKeyRecord(tx, projectID, keyID, true)
		if err != nil {
			return err
		}
		key := &record.Key
		if key.Status != entity.KeyPending || key.DeliveryExpiresAt == nil || !key.DeliveryExpiresAt.After(time.Now()) || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
			return errKeyConflict
		}
		if err := validateProjectKeyModels(tx, projectID, record.ModelIDs); err != nil {
			return err
		}
		if key.ReplacesKeyID != nil {
			old, err := projectKeyRecord(tx, projectID, *key.ReplacesKeyID, true)
			if err != nil {
				return err
			}
			if old.Key.Status != entity.KeyActive && old.Key.Status != entity.KeyDisabled {
				return errKeyConflict
			}
			if !slices.Equal(record.ModelIDs, old.ModelIDs) || !sameExpiry(key.ExpiresAt, old.Key.ExpiresAt) {
				return errKeyConflict
			}
			key.ActivateOnConfirm = old.Key.Status == entity.KeyActive
		}
		key.Status = entity.KeyDisabled
		if key.ActivateOnConfirm {
			key.Status = entity.KeyActive
		}
		key.DeliveryExpiresAt = nil
		if err := tx.Save(key).Error; err != nil {
			return err
		}
		// Delivery confirmation does not prove deployment or a successful call.
		if err := appendAudit(tx, actorID, "project_key.confirm", "project_api_key", key.ID); err != nil {
			return err
		}
		result = record
		return nil
	})
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}
func (s *Service) UpdateProjectKey(ctx context.Context, actorID, projectID, keyID string, name *string, enabled *bool) (*ProjectKeyRecord, error) {
	if name == nil && enabled == nil {
		return nil, apperrors.ErrBadRequest
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if !validCatalogLabel(trimmed) {
			return nil, apperrors.ErrBadRequest
		}
		name = &trimmed
	}
	var result *ProjectKeyRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, enabled != nil && *enabled); err != nil {
			return err
		}
		record, err := projectKeyRecord(tx, projectID, keyID, true)
		if err != nil {
			return err
		}
		key := &record.Key
		if key.Status == entity.KeyRevoked || key.Status == entity.KeyPending {
			return errKeyConflict
		}
		if name != nil {
			key.Name = *name
		}
		if enabled != nil {
			key.Status = entity.KeyDisabled
			if *enabled {
				if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
					return errKeyConflict
				}
				if err := validateProjectKeyModels(tx, projectID, record.ModelIDs); err != nil {
					return err
				}
				key.Status = entity.KeyActive
			}
		}
		if err := tx.Save(key).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "project_key.update", "project_api_key", keyID); err != nil {
			return err
		}
		result = record
		return nil
	})
	if err == nil && enabled != nil && !*enabled {
		s.InvalidateRuntimeKey(keyID)
	}
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}
func (s *Service) RevokeProjectKey(ctx context.Context, actorID, projectID, keyID string) error {
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, false); err != nil {
			return err
		}
		record, err := projectKeyRecord(tx, projectID, keyID, true)
		if err != nil {
			return err
		}
		if record.Key.Status == entity.KeyRevoked {
			return nil
		}
		if err := tx.Model(&record.Key).Update("status", entity.KeyRevoked).Error; err != nil {
			return err
		}
		return appendAudit(tx, actorID, "project_key.revoke", "project_api_key", keyID)
	})
	if err == nil {
		s.InvalidateRuntimeKey(keyID)
	}
	return s.refreshAfterMutation(ctx, catalogError(err))
}
func (s *Service) RotateProjectKey(ctx context.Context, actorID, projectID, keyID, deliveryMode string) (*CreatedProjectKey, error) {
	if deliveryMode != "manual" {
		return nil, apperrors.ErrBadRequest
	}
	var result *CreatedProjectKey
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, true); err != nil {
			return err
		}
		old, err := projectKeyRecord(tx, projectID, keyID, true)
		if err != nil {
			return err
		}
		if (old.Key.Status != entity.KeyActive && old.Key.Status != entity.KeyDisabled) || (old.Key.ExpiresAt != nil && !old.Key.ExpiresAt.After(time.Now())) {
			return errKeyConflict
		}
		if err := validateProjectKeyModels(tx, projectID, old.ModelIDs); err != nil {
			return err
		}
		result, err = createPendingProjectKey(tx, actorID, projectID, old.Key.Name, old.ModelIDs, old.Key.ExpiresAt, &old.Key.ID, old.Key.Status == entity.KeyActive)
		return err
	})
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}
func (s *Service) CompleteProjectKeyRotation(ctx context.Context, actorID, projectID, keyID, replacementID string) error {
	if replacementID == "" || replacementID == keyID {
		return apperrors.ErrBadRequest
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockProjectForKeys(tx, actorID, projectID, true); err != nil {
			return err
		}
		old, err := projectKeyRecord(tx, projectID, keyID, true)
		if err != nil {
			return err
		}
		replacement, err := projectKeyRecord(tx, projectID, replacementID, true)
		if err != nil {
			return err
		}
		if replacement.Key.ReplacesKeyID == nil || *replacement.Key.ReplacesKeyID != keyID {
			return errKeyConflict
		}
		completed, err := keyRotationCompleted(tx, "project_key.rotation.complete", "project_api_key", replacementID)
		if err != nil {
			return err
		}
		if completed && old.Key.Status == entity.KeyRevoked {
			return nil
		}
		if replacement.Key.Status != entity.KeyActive || (replacement.Key.ExpiresAt != nil && !replacement.Key.ExpiresAt.After(time.Now())) {
			return errKeyConflict
		}
		if old.Key.Status != entity.KeyActive && old.Key.Status != entity.KeyDisabled && old.Key.Status != entity.KeyRevoked {
			return errKeyConflict
		}
		if !slices.Equal(old.ModelIDs, replacement.ModelIDs) || !sameExpiry(old.Key.ExpiresAt, replacement.Key.ExpiresAt) {
			return errKeyConflict
		}
		if err := validateProjectKeyModels(tx, projectID, replacement.ModelIDs); err != nil {
			return err
		}
		var calls int64
		if err := tx.Model(&entity.CallRecord{}).Where("project_id = ? AND key_id = ? AND status = ? AND started_at >= ?", projectID, replacementID, "success", replacement.Key.CreatedAt).Count(&calls).Error; err != nil {
			return err
		}
		if calls == 0 {
			return errKeyConflict
		}
		if old.Key.Status != entity.KeyRevoked {
			if err := tx.Model(&old.Key).Update("status", entity.KeyRevoked).Error; err != nil {
				return err
			}
			if err := appendAudit(tx, actorID, "project_key.revoke", "project_api_key", keyID); err != nil {
				return err
			}
		}
		return appendAudit(tx, actorID, "project_key.rotation.complete", "project_api_key", replacementID)
	})
	if err == nil {
		s.InvalidateRuntimeKey(keyID)
	}
	return s.refreshAfterMutation(ctx, catalogError(err))
}

// commonProjectKeyMetadata exposes only the common authentication shape. The
// empty UserID prevents attribution or revocation through the creator account.
func commonProjectKeyMetadata(key entity.ProjectKey) entity.APIKey {
	return entity.APIKey{ID: key.ID, Name: key.Name, Prefix: key.Prefix, TokenHash: key.TokenHash, Status: key.Status, ExpiresAt: key.ExpiresAt, DeliveryExpiresAt: key.DeliveryExpiresAt, ReplacesKeyID: key.ReplacesKeyID, ActivateOnConfirm: key.ActivateOnConfirm, CreatedAt: key.CreatedAt, UpdatedAt: key.UpdatedAt}
}
func (s *Service) authenticateProjectKey(ctx context.Context, bearer string) (*KeyRecord, error) {
	if len(bearer) != 47 || !strings.HasPrefix(bearer, "rxp_") {
		return nil, apperrors.ErrUnauthorized
	}
	db := s.authDB(ctx)
	var key entity.ProjectKey
	err := db.Where("token_hash = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", secret.SHA256Hex(bearer), entity.KeyActive, time.Now().UTC()).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrUnauthorized
	}
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var projects int64
	if err := db.Model(&entity.Project{}).Where("id = ? AND status = ?", key.ProjectID, entity.ResourceActive).Count(&projects).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	var managers int64
	if err := db.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND u.disabled = ?", key.ProjectID, false).Count(&managers).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	if projects != 1 || managers == 0 {
		return nil, apperrors.ErrUnauthorized
	}
	models := []string{}
	if err := db.Table("project_api_key_models s").Joins("JOIN project_model_grants g ON g.model_id = s.model_id AND g.project_id = ?", key.ProjectID).Joins("JOIN models m ON m.id = s.model_id").Where("s.key_id = ? AND m.status = ?", key.ID, entity.ResourceActive).Order("s.model_id").Pluck("s.model_id", &models).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	return &KeyRecord{Key: commonProjectKeyMetadata(key), ProjectID: key.ProjectID, ModelIDs: models}, nil
}
