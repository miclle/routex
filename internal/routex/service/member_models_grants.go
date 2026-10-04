package service

import (
	"crypto/rand"
	"encoding/hex"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// Retained rows keep immutable creation and request provenance. Only a real
// membership change advances the complete Personal grant generation.
func memberModelsGrantDiff(current []entity.UserModelGrant, ids []string) (removed, added []string) {
	old := map[string]bool{}
	wanted := map[string]bool{}
	for _, grant := range current {
		old[grant.ModelID] = true
	}
	for _, id := range ids {
		wanted[id] = true
		if !old[id] {
			added = append(added, id)
		}
	}
	for id := range old {
		if !wanted[id] {
			removed = append(removed, id)
		}
	}
	slices.Sort(removed)
	slices.Sort(added)
	return
}
func advancePersonalGrantRevision(tx *gorm.DB, userID string) error {
	var buffer [32]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return err
	}
	result := personalExact(modelCreationDB(tx).Model(&entity.User{}), "id", userID).UpdateColumn("personal_grant_revision", hex.EncodeToString(buffer[:]))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrNotFound
	}
	return nil
}
func replacePersonalModelGrants(tx *gorm.DB, userID string, current []entity.UserModelGrant, ids []string) (bool, error) {
	removed, added := memberModelsGrantDiff(current, ids)
	for _, id := range removed {
		if err := personalExact(personalExact(modelCreationDB(tx), "user_id", userID), "model_id", id).Delete(&entity.UserModelGrant{}).Error; err != nil {
			return false, err
		}
	}
	for _, id := range added {
		if err := tx.Create(&entity.UserModelGrant{UserID: userID, ModelID: id, CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}).Error; err != nil {
			return false, err
		}
	}
	if len(removed)+len(added) > 0 {
		return len(removed) > 0, advancePersonalGrantRevision(tx, userID)
	}
	return false, nil
}
func (s *Service) invalidatePersonalModelGrants(userID string) {
	if s.runtime != nil {
		s.runtime.deniedPersonalGrants.Store(userID, s.runtime.epoch.Add(1))
	}
}
