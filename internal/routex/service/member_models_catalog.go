package service

import (
	"errors"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Global Model grants use the same governance -> sorted User -> Model order as
// the member workspace and Key issuance. Retained rows are never recreated.
func setCatalogModelGrants(tx *gorm.DB, actorID, modelID string, userIDs []string) ([]string, error) {
	if err := lockGovernance(tx); err != nil {
		return nil, err
	}
	if err := exactCatalogPermission(modelCreationDB(tx), actorID, "models.write"); err != nil {
		return nil, err
	}
	var current []entity.UserModelGrant
	if err := personalExact(modelCreationDB(tx), "model_id", modelID).Order("user_id").Limit(5001).Find(&current).Error; err != nil {
		return nil, err
	}
	if len(current) > 5000 {
		return nil, ErrModelCatalogOverflow
	}
	affected := slices.Clone(userIDs)
	existing := map[string]entity.UserModelGrant{}
	for _, g := range current {
		if g.ModelID != modelID {
			return nil, apperrors.ErrNotFound
		}
		affected = append(affected, g.UserID)
		existing[g.UserID] = g
	}
	slices.Sort(affected)
	affected = slices.Compact(affected)
	for _, userID := range affected {
		user, err := memberModelsSubject(tx, userID, true)
		if err != nil {
			// Preserve the catalog replacement contract for invalid requested owners.
			if slices.Contains(userIDs, userID) && (errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, apperrors.ErrNotFound)) {
				return nil, apperrors.ErrBadRequest
			}
			return nil, err
		}
		if slices.Contains(userIDs, userID) && (user.Disabled || user.OffboardedAt != nil) {
			return nil, apperrors.ErrBadRequest
		}
	}
	var model entity.Model
	if err := personalExact(modelCreationDB(tx), "id", modelID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&model).Error; err != nil {
		return nil, err
	}
	removed := []string{}
	changed := false
	for _, userID := range affected {
		grant, present := existing[userID]
		wanted := slices.Contains(userIDs, userID)
		if present == wanted {
			continue
		}
		changed = true
		if present {
			if err := personalExact(personalExact(modelCreationDB(tx), "user_id", userID), "model_id", modelID).Delete(&entity.UserModelGrant{}).Error; err != nil {
				return nil, err
			}
			removed = append(removed, userID)
		} else {
			grant = entity.UserModelGrant{UserID: userID, ModelID: modelID}
			if err := tx.Create(&grant).Error; err != nil {
				return nil, err
			}
		}
		if err := advancePersonalGrantRevision(modelCreationDB(tx), userID); err != nil {
			return nil, err
		}
	}
	if changed {
		return removed, appendAudit(tx, actorID, "model.grants.update", "model", modelID)
	}
	return removed, nil
}
