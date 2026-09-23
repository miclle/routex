package service

import (
	"context"
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// CompletePersonalKeyRotation retires an old credential only after a successful
// call made with its delivered replacement. Emergency revocation stays separate.
func (s *Service) CompletePersonalKeyRotation(ctx context.Context, userID, keyID, replacementID string) error {
	if replacementID == "" || replacementID == keyID {
		return apperrors.ErrBadRequest
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockActiveKeyOwner(tx, userID); err != nil {
			return err
		}
		old, err := lockOwnedKey(tx, userID, keyID)
		if err != nil {
			return err
		}
		replacement, err := lockOwnedKey(tx, userID, replacementID)
		if err != nil {
			return err
		}
		if replacement.ReplacesKeyID == nil || *replacement.ReplacesKeyID != keyID {
			return errKeyConflict
		}
		completed, err := keyRotationCompleted(tx, "key.rotation.complete", "api_key", replacementID)
		if err != nil {
			return err
		}
		if completed && old.Status == entity.KeyRevoked {
			return nil
		}
		if replacement.Status != entity.KeyActive || (replacement.ExpiresAt != nil && !replacement.ExpiresAt.After(time.Now())) {
			return errKeyConflict
		}
		if old.Status != entity.KeyActive && old.Status != entity.KeyDisabled && old.Status != entity.KeyRevoked {
			return errKeyConflict
		}
		models, err := keyModelIDs(tx, keyID)
		if err != nil {
			return err
		}
		replacementModels, err := keyModelIDs(tx, replacementID)
		if err != nil {
			return err
		}
		if !slices.Equal(models, replacementModels) || !sameExpiry(old.ExpiresAt, replacement.ExpiresAt) {
			return errKeyConflict
		}
		if err := validateKeyModels(tx, userID, replacementModels); err != nil {
			return err
		}
		var calls int64
		if err := tx.Model(&entity.CallRecord{}).Where("user_id = ? AND project_id = ? AND key_id = ? AND status = ? AND started_at >= ?", userID, "", replacementID, "success", replacement.CreatedAt).Count(&calls).Error; err != nil {
			return err
		}
		if calls == 0 {
			return errKeyConflict
		}
		if old.Status != entity.KeyRevoked {
			if err := tx.Model(old).Update("status", entity.KeyRevoked).Error; err != nil {
				return err
			}
			if err := appendAudit(tx, userID, "key.revoke", "api_key", keyID); err != nil {
				return err
			}
		}
		// Audit the exact replacement; its immutable replacesKeyID is the source.
		return appendAudit(tx, userID, "key.rotation.complete", "api_key", replacementID)
	})
	if err == nil {
		s.InvalidateRuntimeKey(keyID)
	}
	return s.refreshAfterMutation(ctx, keyServiceError(err))
}
func keyRotationCompleted(tx *gorm.DB, action, resourceType, replacementID string) (bool, error) {
	var count int64
	err := tx.Model(&entity.AuditEvent{}).Where("action = ? AND resource_type = ? AND resource_id = ?", action, resourceType, replacementID).Count(&count).Error
	return count > 0, err
}
