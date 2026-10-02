package service

import (
	"context"
	"database/sql"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func validCallTeamAttribution(fact CallFact) bool {
	if fact.TeamID == "" {
		return fact.TeamMembershipID == ""
	}
	return validMemberCatalogID(fact.TeamID) && validMemberCatalogID(fact.TeamMembershipID) &&
		validMemberCatalogID(fact.UserID) && fact.ProjectID == "" && fact.KeyID == ""
}

// History authority is current membership, never a platform directory permission
// or ownership inferred from a historical call. Owners also read only their calls.
func authorizeTeamCalls(tx *gorm.DB, actorID, teamID string) error {
	if !validMemberCatalogID(teamID) {
		return apperrors.ErrNotFound
	}
	if _, err := exactEnabledActor(tx, actorID); err != nil {
		return err
	}
	var team entity.Team
	if err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, teamID)).
		Where("status = ?", entity.ResourceActive).First(&team).Error; err != nil {
		return catalogError(err)
	}
	if team.ID != teamID || team.Status != entity.ResourceActive {
		return apperrors.ErrNotFound
	}
	var member entity.TeamMembership
	if err := tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).
		Where("status = ?", entity.ResourceActive).First(&member).Error; err != nil {
		return catalogError(err)
	}
	if member.TeamID != teamID || member.UserID != actorID || member.Status != entity.ResourceActive ||
		(member.Role != entity.TeamOwner && member.Role != entity.TeamMember) {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Service) ListTeamCalls(ctx context.Context, actorID, teamID string, filter CallFilter) (*CallPage, error) {
	if err := validateCallFilter(filter, false); err != nil {
		return nil, err
	}
	if filter.KeyID != "" || filter.ProjectID != "" || filter.TeamID != "" {
		return nil, apperrors.ErrBadRequest
	}
	var page *CallPage
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeTeamCalls(tx, actorID, teamID); err != nil {
			return err
		}
		filter.TeamID, filter.UserID = teamID, actorID
		// Use the same read snapshot for authorization and immutable facts.
		var err error
		page, err = listCallsDB(tx, "", filter)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return page, catalogError(err)
}

func (s *Service) GetTeamCall(ctx context.Context, actorID, teamID, requestID string) (*entity.CallRecord, error) {
	if !safeCallID.MatchString(requestID) {
		return nil, apperrors.ErrNotFound
	}
	var record entity.CallRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeTeamCalls(tx, actorID, teamID); err != nil {
			return err
		}
		return tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).
			Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).
			Where(database.ExactText(tx, clause.Column{Name: "request_id"}, requestID)).First(&record).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return &record, nil
}
