package service

import (
	"context"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func (s *Service) SetTeamMembers(ctx context.Context, actorID, teamID string, members []TeamMemberInput) (*ResourceRecord, error) {
	if len(members) == 0 || len(members) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	activeIDs := []string{}
	ownerCount := 0
	for _, member := range members {
		if member.UserID == "" || seen[member.UserID] || (member.Role != entity.TeamOwner && member.Role != entity.TeamMember) || (member.Status != entity.ResourceActive && member.Status != entity.ResourceDisabled) {
			return nil, apperrors.ErrBadRequest
		}
		seen[member.UserID] = true
		if member.Status == entity.ResourceActive {
			activeIDs = append(activeIDs, member.UserID)
			if member.Role == entity.TeamOwner {
				ownerCount++
			}
		}
	}
	if ownerCount == 0 {
		return nil, catalogConflict
	}
	var result *ResourceRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, "teams.write"); err != nil {
			return err
		}
		current, err := resourceRecord(tx, TeamResource, teamID, true)
		if err != nil {
			return err
		}
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		if err := activeResourceUsers(tx, activeIDs); err != nil {
			return err
		}
		var userCount int64
		userIDs := make([]string, 0, len(members))
		for _, member := range members {
			userIDs = append(userIDs, member.UserID)
		}
		if err := tx.Model(&entity.User{}).Where("id IN ?", userIDs).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount != int64(len(members)) {
			return apperrors.ErrBadRequest
		}
		existing := map[string]string{}
		for _, member := range current.Members {
			existing[member.UserID] = member.ID
		}
		if err := tx.Where("team_id = ?", teamID).Delete(&entity.TeamMembership{}).Error; err != nil {
			return err
		}
		for _, member := range members {
			relationID := existing[member.UserID]
			if relationID == "" {
				relationID, err = id.NewPrefixed("tmm")
				if err != nil {
					return err
				}
			}
			if err := tx.Create(&entity.TeamMembership{ID: relationID, TeamID: teamID, UserID: member.UserID, Role: member.Role, Status: member.Status}).Error; err != nil {
				return err
			}
		}
		if err := appendAudit(tx, actorID, "team.members.replace", "teams", teamID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, TeamResource, teamID, false)
		return err
	})
	return result, catalogError(err)
}
func (s *Service) SetProjectManagers(ctx context.Context, actorID, projectID string, userIDs []string) (*ResourceRecord, error) {
	if len(userIDs) == 0 {
		return nil, catalogConflict
	}
	var result *ResourceRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		allowed, err := resourcePermission(tx, actorID, ProjectResource, "write")
		if err != nil {
			return err
		}
		if !allowed {
			allowed, err = resourceManager(tx, actorID, projectID)
			if err != nil {
				return err
			}
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		current, err := resourceRecord(tx, ProjectResource, projectID, true)
		if err != nil {
			return err
		}
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		if err := activeResourceUsers(tx, userIDs); err != nil {
			return err
		}
		existing := map[string]string{}
		for _, manager := range current.Managers {
			existing[manager.UserID] = manager.ID
		}
		if err := tx.Where("project_id = ?", projectID).Delete(&entity.ProjectManager{}).Error; err != nil {
			return err
		}
		for _, userID := range userIDs {
			relationID := existing[userID]
			if relationID == "" {
				relationID, err = id.NewPrefixed("pmg")
				if err != nil {
					return err
				}
			}
			if err := tx.Create(&entity.ProjectManager{ID: relationID, ProjectID: projectID, UserID: userID}).Error; err != nil {
				return err
			}
		}
		if err := appendAudit(tx, actorID, "project.managers.replace", "projects", projectID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, ProjectResource, projectID, false)
		return err
	})
	return result, catalogError(err)
}
func (s *Service) SetResourceModels(ctx context.Context, actorID string, kind ResourceKind, resourceID string, modelIDs []string) (*ResourceRecord, error) {
	if !resourceKindValid(kind) || len(modelIDs) > 1000 {
		return nil, apperrors.ErrBadRequest
	}
	seen := map[string]bool{}
	for _, modelID := range modelIDs {
		if modelID == "" || seen[modelID] {
			return nil, apperrors.ErrBadRequest
		}
		seen[modelID] = true
	}
	var result *ResourceRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actorID, string(kind)+".models.write"); err != nil {
			return err
		}
		current, err := resourceRecord(tx, kind, resourceID, true)
		if err != nil {
			return err
		}
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		if len(modelIDs) > 0 {
			var count int64
			if err := tx.Model(&entity.Model{}).Where("id IN ? AND status = ?", modelIDs, entity.ResourceActive).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(modelIDs)) {
				return apperrors.ErrBadRequest
			}
		}
		if kind == TeamResource {
			if err := tx.Where("team_id = ?", resourceID).Delete(&entity.TeamModelGrant{}).Error; err != nil {
				return err
			}
			for _, modelID := range modelIDs {
				if err := tx.Create(&entity.TeamModelGrant{TeamID: resourceID, ModelID: modelID}).Error; err != nil {
					return err
				}
			}
		} else {
			if err := tx.Where("project_id = ?", resourceID).Delete(&entity.ProjectModelGrant{}).Error; err != nil {
				return err
			}
			for _, modelID := range modelIDs {
				if err := tx.Create(&entity.ProjectModelGrant{ProjectID: resourceID, ModelID: modelID}).Error; err != nil {
					return err
				}
			}
		}
		if err := appendAudit(tx, actorID, "resource.models.replace", string(kind), resourceID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, kind, resourceID, false)
		return err
	})
	return result, catalogError(err)
}

// The caller must hold the governance singleton lock. All owner/manager changes
// take that same lock, serializing account suspension with resource continuity.
func validateResourceContinuity(tx *gorm.DB, userID string) error {
	var teamIDs []string
	if err := tx.Table("team_memberships m").Select("m.team_id").Joins("JOIN teams t ON t.id = m.team_id").Where("m.user_id = ? AND m.role = ? AND m.status = ? AND t.status <> ?", userID, entity.TeamOwner, entity.ResourceActive, entity.ResourceArchived).Scan(&teamIDs).Error; err != nil {
		return err
	}
	for _, teamID := range teamIDs {
		var count int64
		if err := tx.Table("team_memberships m").Joins("JOIN users u ON u.id = m.user_id").Where("m.team_id = ? AND m.user_id <> ? AND m.role = ? AND m.status = ? AND u.disabled = ?", teamID, userID, entity.TeamOwner, entity.ResourceActive, false).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return catalogConflict
		}
	}
	var projectIDs []string
	if err := tx.Table("project_managers m").Select("m.project_id").Joins("JOIN projects p ON p.id = m.project_id").Where("m.user_id = ? AND p.status <> ?", userID, entity.ResourceArchived).Scan(&projectIDs).Error; err != nil {
		return err
	}
	for _, projectID := range projectIDs {
		var count int64
		if err := tx.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND m.user_id <> ? AND u.disabled = ?", projectID, userID, false).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			return catalogConflict
		}
	}
	return nil
}
