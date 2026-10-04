package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func newMemberTeamJoinedAt(now time.Time) *time.Time {
	stamp := now.UTC().Truncate(time.Microsecond)
	return &stamp
}

// Complete replacement preserves the generation and its historical NULL date.
// Read the exact retained rows rather than the public person summary.
func retainedMemberTeamGenerations(tx *gorm.DB, teamID string) (map[string]entity.TeamMembership, error) {
	var rows []entity.TeamMembership
	if err := tx.Select("id", "team_id", "user_id", "joined_at").Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).Order("id").Limit(1001).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 1000 {
		return nil, apperrors.ErrInternal
	}
	return validateRetainedMemberTeamGenerations(rows, teamID)
}

func validateRetainedMemberTeamGenerations(rows []entity.TeamMembership, teamID string) (map[string]entity.TeamMembership, error) {
	if len(rows) > 1000 {
		return nil, apperrors.ErrInternal
	}
	result := map[string]entity.TeamMembership{}
	ids := map[string]bool{}
	for _, row := range rows {
		if row.TeamID != teamID || !safeTeamSessionID(row.ID) || !safeTeamSessionID(row.UserID) || ids[row.ID] || result[row.UserID].ID != "" || row.JoinedAt != nil && row.JoinedAt.IsZero() {
			return nil, apperrors.ErrInternal
		}
		result[row.UserID] = row
		ids[row.ID] = true
	}
	return result, nil
}
