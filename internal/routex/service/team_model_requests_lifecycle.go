package service

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// Lifecycle callers hold the governance lock and authorize exact persisted IDs.
// Only pending requests are cancelled; approved shared grants survive departure.
func CancelTeamModelRequestsForUser(tx *gorm.DB, actorID, userID, reason string) error {
	if !safeTeamSessionID(userID) {
		return apperrors.ErrBadRequest
	}
	return cancelTeamModelRequests(personalExact(tx, "applicant_user_id", userID), actorID, reason)
}
func CancelTeamModelRequestsForTeam(tx *gorm.DB, actorID, teamID, reason string) error {
	if !safeTeamSessionID(teamID) {
		return apperrors.ErrBadRequest
	}
	return cancelTeamModelRequests(personalExact(tx, "team_id", teamID), actorID, reason)
}
func CancelTeamModelRequestsForMember(tx *gorm.DB, actorID, teamID, userID, reason string) error {
	if !safeTeamSessionID(teamID) || !safeTeamSessionID(userID) {
		return apperrors.ErrBadRequest
	}
	return cancelTeamModelRequests(personalExact(personalExact(tx, "team_id", teamID), "applicant_user_id", userID), actorID, reason)
}
func CancelTeamModelRequestsForModel(tx *gorm.DB, actorID, modelID, reason string) error {
	if !safeTeamSessionID(modelID) {
		return apperrors.ErrBadRequest
	}
	return cancelTeamModelRequests(personalExact(tx, "model_id", modelID), actorID, reason)
}
func cancelTeamModelRequests(query *gorm.DB, actorID, reason string) error {
	if !safeTeamSessionID(actorID) || !personalModelReason(reason, true) || len(reason) > 128 {
		return apperrors.ErrBadRequest
	}
	var rows []entity.TeamModelRequest
	if err := personalExact(query, "status", entity.TeamModelRequestPending).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	tx := query.Session(&gorm.Session{NewDB: true})
	for _, row := range rows {
		if row.Status != entity.TeamModelRequestPending {
			return apperrors.ErrInternal
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row.Status, row.CancelledReason = entity.TeamModelRequestCancelled, reason
		row.ResolvedAt, row.UpdatedAt = &now, now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := personalExact(tx, "request_id", row.ID).Delete(&entity.TeamModelRequestPendingSlot{}).Error; err != nil {
			return err
		}
		if err := appendTeamModelRequestAudit(tx, actorID, "cancel", row, reason); err != nil {
			return err
		}
	}
	return nil
}
