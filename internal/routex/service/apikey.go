package service

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/secret"
)

var errKeyConflict = &apperrors.Error{Code: http.StatusConflict, Message: "key state changed or delivery expired"}

// KeyRecord separates a Key's stable model scope from its verification secret.
type KeyRecord struct {
	ProjectID string
	Key       entity.APIKey
	ModelIDs  []string
}

type CreatedKey struct {
	Record KeyRecord
	Secret string
}

func appendAudit(tx *gorm.DB, actorID, action, resourceType, resourceID string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: action, ResourceType: resourceType, ResourceID: resourceID}).Error
}

func (s *Service) ListPersonalKeys(ctx context.Context, userID string) ([]KeyRecord, error) {
	db := s.authDB(ctx)
	var keys []entity.APIKey
	if err := db.Where("user_id = ?", userID).Order("created_at DESC, id DESC").Find(&keys).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	result := make([]KeyRecord, 0, len(keys))
	for _, key := range keys {
		ids, err := keyModelIDs(db, key.ID)
		if err != nil {
			return nil, apperrors.ErrInternal
		}
		result = append(result, KeyRecord{Key: key, ModelIDs: ids})
	}
	return result, nil
}

func keyModelIDs(db *gorm.DB, keyID string) ([]string, error) {
	ids := []string{}
	err := db.Model(&entity.APIKeyModel{}).Where("key_id = ?", keyID).Order("model_id").Pluck("model_id", &ids).Error
	return ids, err
}

func validateKeyModels(db *gorm.DB, userID string, modelIDs []string) error {
	if len(modelIDs) == 0 || len(modelIDs) > 128 {
		return apperrors.ErrBadRequest
	}
	seen := make(map[string]bool, len(modelIDs))
	for _, modelID := range modelIDs {
		if seen[modelID] || modelID == "" {
			return apperrors.ErrBadRequest
		}
		seen[modelID] = true
	}
	var count int64
	if err := db.Table("user_model_grants AS grants").Joins("JOIN models ON models.id = grants.model_id").Where("grants.user_id = ? AND grants.model_id IN ? AND models.status = ?", userID, modelIDs, "active").Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(modelIDs)) {
		return apperrors.ErrForbidden
	}
	return nil
}

func keyServiceError(err error) error {
	if err == nil {
		return nil
	}
	var public *apperrors.Error
	if errors.As(err, &public) {
		return public
	}
	return apperrors.ErrInternal
}

func (s *Service) CreatePersonalKey(ctx context.Context, userID, name string, modelIDs []string, expiresAt *time.Time) (*CreatedKey, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 100 || (expiresAt != nil && !expiresAt.After(time.Now())) {
		return nil, apperrors.ErrBadRequest
	}
	var result *CreatedKey
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		if err := validateKeyModels(tx, userID, modelIDs); err != nil {
			return err
		}
		var err error
		result, err = createPendingKey(tx, userID, name, modelIDs, expiresAt, nil, true)
		return err
	})
	return result, s.refreshAfterMutation(ctx, keyServiceError(err))
}

func createPendingKey(tx *gorm.DB, userID, name string, modelIDs []string, expiresAt *time.Time, replaces *string, activate bool) (*CreatedKey, error) {
	keyID, err := id.NewPrefixed("key")
	if err != nil {
		return nil, err
	}
	random, err := secret.RandomURLSafe(32)
	if err != nil {
		return nil, err
	}
	bearer := "rx_" + random
	deadline := time.Now().UTC().Add(10 * time.Minute)
	key := entity.APIKey{ID: keyID, UserID: userID, Name: name, Prefix: bearer[:11], TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyPending, ExpiresAt: expiresAt, DeliveryExpiresAt: &deadline, ReplacesKeyID: replaces, ActivateOnConfirm: activate}
	if err := tx.Create(&key).Error; err != nil {
		return nil, err
	}
	for _, modelID := range modelIDs {
		if err := tx.Create(&entity.APIKeyModel{KeyID: keyID, ModelID: modelID}).Error; err != nil {
			return nil, err
		}
	}
	if err := appendAudit(tx, userID, "key.create", "api_key", key.ID); err != nil {
		return nil, err
	}
	return &CreatedKey{Record: KeyRecord{Key: key, ModelIDs: slices.Clone(modelIDs)}, Secret: bearer}, nil
}

// lockActiveKeyOwner serializes key issuance with account disable/revocation.
// Take this lock before every key lock, including replacement confirmation.
func lockActiveKeyOwner(tx *gorm.DB, userID string) error {
	var owner entity.User
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "disabled").First(&owner, "id = ?", userID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && owner.Disabled) {
		return apperrors.ErrUnauthorized
	}
	return err
}

func lockOwnedKey(tx *gorm.DB, userID, keyID string) (*entity.APIKey, error) {
	var key entity.APIKey
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", keyID, userID).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

func (s *Service) ConfirmKeyDelivery(ctx context.Context, userID, keyID string) (*KeyRecord, error) {
	var result *KeyRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		key, err := lockOwnedKey(tx, userID, keyID)
		if err != nil {
			return err
		}
		if key.Status != entity.KeyPending || key.DeliveryExpiresAt == nil || !key.DeliveryExpiresAt.After(time.Now()) || (key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now())) {
			return errKeyConflict
		}
		models, err := keyModelIDs(tx, keyID)
		if err != nil {
			return err
		}
		if err := validateKeyModels(tx, userID, models); err != nil {
			return err
		}
		if key.ReplacesKeyID != nil {
			old, err := lockOwnedKey(tx, userID, *key.ReplacesKeyID)
			if err != nil {
				return err
			}
			if old.Status != entity.KeyActive && old.Status != entity.KeyDisabled {
				return errKeyConflict
			}
			// Recheck current scope and activation at confirmation, not just at
			// rotation creation, so edits cannot broaden the replacement.
			oldModels, err := keyModelIDs(tx, old.ID)
			if err != nil {
				return err
			}
			slices.Sort(models)
			slices.Sort(oldModels)
			if !slices.Equal(models, oldModels) || !sameExpiry(old.ExpiresAt, key.ExpiresAt) {
				return errKeyConflict
			}
			key.ActivateOnConfirm = old.Status == entity.KeyActive
			if err := tx.Model(old).Update("status", entity.KeyRevoked).Error; err != nil {
				return err
			}
			if err := appendAudit(tx, userID, "key.revoke", "api_key", old.ID); err != nil {
				return err
			}
		}
		key.Status = entity.KeyDisabled
		if key.ActivateOnConfirm {
			key.Status = entity.KeyActive
		}
		key.DeliveryExpiresAt = nil
		if err := tx.Save(key).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, userID, "key.confirm", "api_key", key.ID); err != nil {
			return err
		}
		result = &KeyRecord{Key: *key, ModelIDs: models}
		return nil
	})
	if err == nil && result.Key.ReplacesKeyID != nil {
		s.InvalidateRuntimeKey(*result.Key.ReplacesKeyID)
	}
	return result, s.refreshAfterMutation(ctx, keyServiceError(err))
}

func sameExpiry(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (s *Service) UpdatePersonalKey(ctx context.Context, userID, keyID string, name *string, enabled *bool) (*KeyRecord, error) {
	if name == nil && enabled == nil {
		return nil, apperrors.ErrBadRequest
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" || !utf8.ValidString(n) || utf8.RuneCountInString(n) > 100 {
			return nil, apperrors.ErrBadRequest
		}
		name = &n
	}
	var result *KeyRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		key, err := lockOwnedKey(tx, userID, keyID)
		if err != nil {
			return err
		}
		if key.Status == entity.KeyRevoked || key.Status == entity.KeyPending {
			return errKeyConflict
		}
		models, err := keyModelIDs(tx, key.ID)
		if err != nil {
			return err
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
				if err := validateKeyModels(tx, userID, models); err != nil {
					return err
				}
				key.Status = entity.KeyActive
			}
		}
		if err := tx.Save(key).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, userID, "key.update", "api_key", key.ID); err != nil {
			return err
		}
		result = &KeyRecord{Key: *key, ModelIDs: models}
		return nil
	})
	if err == nil && enabled != nil && !*enabled {
		s.InvalidateRuntimeKey(keyID)
	}
	return result, s.refreshAfterMutation(ctx, keyServiceError(err))
}

func (s *Service) RevokePersonalKey(ctx context.Context, userID, keyID string) error {
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		key, err := lockOwnedKey(tx, userID, keyID)
		if err != nil {
			return err
		}
		if key.Status == entity.KeyRevoked {
			return nil
		}
		if err := tx.Model(key).Update("status", entity.KeyRevoked).Error; err != nil {
			return err
		}
		return appendAudit(tx, userID, "key.revoke", "api_key", key.ID)
	})
	if err == nil {
		s.InvalidateRuntimeKey(keyID)
	}
	return s.refreshAfterMutation(ctx, keyServiceError(err))
}

func (s *Service) RotatePersonalKey(ctx context.Context, userID, keyID string) (*CreatedKey, error) {
	var result *CreatedKey
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		key, err := lockOwnedKey(tx, userID, keyID)
		if err != nil {
			return err
		}
		if key.Status != entity.KeyActive && key.Status != entity.KeyDisabled {
			return errKeyConflict
		}
		if key.ExpiresAt != nil && !key.ExpiresAt.After(time.Now()) {
			return errKeyConflict
		}
		models, err := keyModelIDs(tx, key.ID)
		if err != nil {
			return err
		}
		if err := validateKeyModels(tx, userID, models); err != nil {
			return err
		}
		result, err = createPendingKey(tx, userID, key.Name, models, key.ExpiresAt, &key.ID, key.Status == entity.KeyActive)
		return err
	})
	return result, s.refreshAfterMutation(ctx, keyServiceError(err))
}

// AuthenticateAPIKey verifies current revocation, expiry, account, and grant
// state. Gateway runtime snapshots will preserve the same effective contract.
func (s *Service) AuthenticateAPIKey(ctx context.Context, bearer string) (*KeyRecord, error) {
	if s.runtime != nil {
		return s.authenticateRuntimeKey(bearer)
	}
	if strings.HasPrefix(bearer, "rxp_") {
		return s.authenticateProjectKey(ctx, bearer)
	}
	if len(bearer) != 46 || !strings.HasPrefix(bearer, "rx_") {
		return nil, apperrors.ErrUnauthorized
	}
	db := s.authDB(ctx)
	var key entity.APIKey
	err := db.Where("token_hash = ? AND status = ? AND (expires_at IS NULL OR expires_at > ?)", secret.SHA256Hex(bearer), entity.KeyActive, time.Now().UTC()).First(&key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.ErrUnauthorized
	}
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	var count int64
	if err := db.Model(&entity.User{}).Where("id = ? AND disabled = ?", key.UserID, false).Count(&count).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	if count != 1 {
		return nil, apperrors.ErrUnauthorized
	}
	ids := []string{}
	if err := db.Table("api_key_models AS scope").Joins("JOIN user_model_grants AS grants ON grants.model_id = scope.model_id AND grants.user_id = ?", key.UserID).Joins("JOIN models ON models.id = scope.model_id").Where("scope.key_id = ? AND models.status = ?", key.ID, "active").Order("scope.model_id").Pluck("scope.model_id", &ids).Error; err != nil {
		return nil, apperrors.ErrInternal
	}
	return &KeyRecord{Key: key, ModelIDs: ids}, nil
}
