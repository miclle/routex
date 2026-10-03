package service

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
)

func teamQuotaPending(status string) bool {
	return status == entity.TeamQuotaRequestPendingOwner || status == entity.TeamQuotaRequestPendingAdmin
}
func teamQuotaPendingQuery(tx *gorm.DB) *gorm.DB {
	return tx.Where(clause.Or(teamQuotaEquality(tx, "status", entity.TeamQuotaRequestPendingOwner), teamQuotaEquality(tx, "status", entity.TeamQuotaRequestPendingAdmin)))
}
func teamQuotaEquality(tx *gorm.DB, column, value string) clause.Expression {
	return database.ExactText(tx, clause.Column{Name: column}, value)
}

func cancelTeamQuotaRequestsForTeam(tx *gorm.DB, teamID, reason string) error {
	return cancelTeamQuotaPendingRows(quotaExact(tx, "team_id", teamID), reason)
}
func cancelTeamQuotaRequestsForMember(tx *gorm.DB, teamID, userID, reason string) error {
	return cancelTeamQuotaPendingRows(quotaExact(quotaExact(tx, "team_id", teamID), "applicant_user_id", userID), reason)
}
func cancelTeamQuotaRequestsForUser(tx *gorm.DB, userID, reason string) error {
	if err := cancelTeamQuotaPendingRows(quotaExact(tx, "applicant_user_id", userID), reason); err != nil {
		return err
	}
	var members []entity.TeamMembership
	if err := quotaExact(tx, "user_id", userID).Order("team_id").Find(&members).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, member := range members {
		if member.UserID == userID && !seen[member.TeamID] {
			seen[member.TeamID] = true
			if err := reconcileTeamQuotaRequestOwners(tx, member.TeamID); err != nil {
				return err
			}
		}
	}
	return nil
}
func cancelTeamQuotaPendingRows(query *gorm.DB, reason string) error {
	var rows []entity.TeamQuotaRequest
	if err := teamQuotaPendingQuery(query).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := cancelTeamQuotaRequest(query.Session(&gorm.Session{NewDB: true}), &row, reason); err != nil {
			return err
		}
	}
	return nil
}
func cancelTeamQuotaRequest(tx *gorm.DB, row *entity.TeamQuotaRequest, reason string) error {
	if !teamQuotaPending(row.Status) {
		return nil
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	var steps []entity.TeamQuotaRequestStep
	if err := quotaExact(tx, "request_id", row.ID).Find(&steps).Error; err != nil {
		return err
	}
	for _, step := range steps {
		if step.RequestID == row.ID && step.Status == entity.TeamQuotaStepPending {
			step.Status = entity.TeamQuotaStepCancelled
			step.Reason = reason
			step.DecidedAt = &now
			if err := tx.Save(&step).Error; err != nil {
				return err
			}
		}
	}
	row.Status = entity.TeamQuotaRequestCancelled
	row.CancelledReason = reason
	row.CurrentStepID = nil
	row.ResolvedAt = &now
	row.UpdatedAt = now
	if err := tx.Save(row).Error; err != nil {
		return err
	}
	return quotaExact(tx, "request_id", row.ID).Delete(&entity.TeamQuotaPendingSlot{}).Error
}
func reconcileTeamQuotaRequestOwners(tx *gorm.DB, teamID string) error {
	var rows []entity.TeamQuotaRequest
	if err := quotaExact(quotaExact(tx, "team_id", teamID), "status", entity.TeamQuotaRequestPendingOwner).Clauses(clause.Locking{Strength: "UPDATE"}).Order("id").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if row.TeamID != teamID || row.Status != entity.TeamQuotaRequestPendingOwner {
			continue
		}
		owners, err := teamQuotaOwners(tx, teamID, row.ApplicantUserID)
		if err != nil {
			return err
		}
		if len(owners) != 0 {
			continue
		}
		if row.CurrentStepID == nil {
			return gorm.ErrInvalidData
		}
		var step entity.TeamQuotaRequestStep
		if err := quotaExact(quotaExact(tx, "request_id", row.ID), "id", *row.CurrentStepID).First(&step).Error; err != nil {
			return err
		}
		if step.Ordinal != 1 || step.Stage != entity.TeamQuotaStageOwner || step.Status != entity.TeamQuotaStepPending {
			return gorm.ErrInvalidData
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		step.Status = entity.TeamQuotaStepCancelled
		step.Reason = "owner_unavailable"
		step.DecidedAt = &now
		if err := tx.Save(&step).Error; err != nil {
			return err
		}
		nextID, err := id.NewPrefixed("qst")
		if err != nil {
			return err
		}
		next := entity.TeamQuotaRequestStep{ID: nextID, RequestID: row.ID, Ordinal: 2, Stage: entity.TeamQuotaStageAdmin, Status: entity.TeamQuotaStepPending, EnteredAt: now}
		if err := tx.Create(&next).Error; err != nil {
			return err
		}
		row.Status = entity.TeamQuotaRequestPendingAdmin
		row.CurrentStepID = &nextID
		row.EscalationReason = "owner_unavailable"
		row.UpdatedAt = now
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
	}
	return nil
}
