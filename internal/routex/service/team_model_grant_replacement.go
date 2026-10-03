package service

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
)

// replaceTeamModelGrants retains the original approval provenance of unchanged
// canonical grants. Removal and later ordinary re-addition create a new grant
// without claiming the historical request as its source.
// The caller holds the governance lock and has validated all selected Models.
func replaceTeamModelGrants(tx *gorm.DB, teamID string, modelIDs []string) error {
	var existing []entity.TeamModelGrant
	team := database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)
	if err := tx.Where(team).Find(&existing).Error; err != nil {
		return err
	}
	desired := make(map[string]bool, len(modelIDs))
	for _, modelID := range modelIDs {
		desired[modelID] = true
	}
	retained := map[string]bool{}
	for _, grant := range existing {
		if grant.TeamID == teamID && desired[grant.ModelID] {
			retained[grant.ModelID] = true
			continue
		}
		if err := tx.Where(team).Where(database.ExactText(tx, clause.Column{Name: "model_id"}, grant.ModelID)).Delete(&entity.TeamModelGrant{}).Error; err != nil {
			return err
		}
	}
	for _, modelID := range modelIDs {
		if !retained[modelID] {
			if err := tx.Create(&entity.TeamModelGrant{TeamID: teamID, ModelID: modelID}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
