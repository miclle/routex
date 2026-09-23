package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func projectRequestDecisionStatus(input ProjectRequestDecision) (string, error) {
	switch input.Action {
	case "approve":
		return entity.ProjectRequestApproved, nil
	case "reject":
		if input.Reason != "" {
			return entity.ProjectRequestRejected, nil
		}
	case "withdraw":
		return entity.ProjectRequestWithdrawn, nil
	}
	return "", apperrors.ErrBadRequest
}
func (s *Service) DecideProjectRequest(ctx context.Context, actorID, projectID, requestID string, input ProjectRequestDecision) (*ProjectRequestRecord, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	status, err := projectRequestDecisionStatus(input)
	if err != nil {
		return nil, err
	}
	if !validResourceDescription(input.Reason) {
		return nil, apperrors.ErrBadRequest
	}
	var result *ProjectRequestRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if _, err := permissionsFor(tx, actorID); err != nil {
			return err
		}
		if input.Action != "withdraw" {
			if err := authorizeGovernance(tx, actorID, "projects.models.write"); err != nil {
				return err
			}
		}
		// Match the resource mutation lock order: policy, project, request, models.
		var project entity.Project
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&project, "id = ?", projectID).Error; err != nil {
			return err
		}
		var row entity.ProjectModelRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ? AND id = ?", projectID, requestID).First(&row).Error; err != nil {
			return err
		}
		if input.Action == "withdraw" {
			if row.ApplicantUserID != actorID {
				return apperrors.ErrForbidden
			}
		} else if row.ApplicantUserID == actorID {
			return apperrors.ErrForbidden
		}
		if row.Status != entity.ProjectRequestPending {
			if row.Status != status || row.DecisionActorID != actorID || row.DecisionReason != input.Reason {
				return catalogConflict
			}
			result, err = projectRequestRecord(&row)
			return err
		}
		if input.Action == "approve" {
			if project.Status != entity.ResourceActive {
				return catalogConflict
			}
			if err := projectRequestManager(tx, row.ApplicantUserID, projectID); err != nil {
				if errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrForbidden) {
					return catalogConflict
				}
				return err
			}
			record, err := projectRequestRecord(&row)
			if err != nil {
				return err
			}
			if err := activeProjectRequestModels(tx, record.RequestedModelIDs); err != nil {
				return err
			}
			// The baseline is historical evidence, never the desired replacement set.
			for _, modelID := range record.RequestedModelIDs {
				grant := entity.ProjectModelGrant{ProjectID: projectID, ModelID: modelID}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&grant).Error; err != nil {
					return err
				}
			}
		}
		now := time.Now().UTC()
		if err := tx.Model(&row).Updates(map[string]any{"status": status, "decision_actor_id": actorID, "decision_reason": input.Reason, "decided_at": now}).Error; err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "project.request."+input.Action, "project_request", row.ID); err != nil {
			return err
		}
		result, err = projectRequestRecord(&row)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if input.Action == "approve" {
		return result, s.refreshAfterMutation(ctx, catalogError(err))
	}
	return result, catalogError(err)
}
