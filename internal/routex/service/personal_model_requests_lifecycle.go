package service

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// Lifecycle callers authorize the exact persisted identity and hold the
// governance lock. Persisted legacy IDs need not use the public request ULID
// format; keep schema bounds and exact predicates without normalizing identity.
// Cancellation retains immutable request history and never changes any grant.
func CancelPersonalModelRequestsForUser(tx *gorm.DB, actorID, userID, reason string) error {
	if !safeTeamSessionID(userID) {
		return apperrors.ErrBadRequest
	}
	return cancelPersonalModelRequests(personalExact(tx, "applicant_user_id", userID), actorID, reason)
}
func CancelPersonalModelRequestsForModel(tx *gorm.DB, actorID, modelID, reason string) error {
	if !personalModelID(modelID, "mdl") {
		return apperrors.ErrBadRequest
	}
	return cancelPersonalModelRequests(personalExact(tx, "model_id", modelID), actorID, reason)
}
func cancelPersonalModelRequests(query *gorm.DB, actorID, reason string) error {
	if !safeTeamSessionID(actorID) || !personalModelReason(reason, true) || len(reason) > 128 {
		return apperrors.ErrBadRequest
	}
	var rows []entity.PersonalModelRequest
	if err := personalExact(query, "status", entity.PersonalModelRequestPending).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	tx := query.Session(&gorm.Session{NewDB: true})
	for _, row := range rows {
		if row.Status != entity.PersonalModelRequestPending {
			return apperrors.ErrInternal
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		row.Status = entity.PersonalModelRequestCancelled
		row.CancelledReason = reason
		row.ResolvedAt = &now
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		if err := appendPersonalModelAudit(tx, actorID, "cancel", row, reason); err != nil {
			return err
		}
		if err := personalExact(tx, "request_id", row.ID).Delete(&entity.PersonalModelRequestPendingSlot{}).Error; err != nil {
			return err
		}
	}
	return nil
}
