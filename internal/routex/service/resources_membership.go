package service

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
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
	var removedUsers []string
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		allowed, err := teamTargetActionAllowed(tx, actorID, teamID, "teams.write")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		current, err := resourceRecord(tx, TeamResource, teamID, true)
		if err != nil {
			return err
		}
		if current.ID != teamID {
			return apperrors.ErrNotFound
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
		if err := teamRoleDefinitionQuery(tx.Model(&entity.User{}), userIDs).Count(&userCount).Error; err != nil {
			return err
		}
		if userCount != int64(len(members)) {
			return apperrors.ErrBadRequest
		}
		existing, err := retainedMemberTeamGenerations(tx, current.ID)
		if err != nil {
			return err
		}
		desiredActive := map[string]bool{}
		for _, member := range members {
			desiredActive[member.UserID] = member.Status == entity.ResourceActive
		}
		for _, member := range current.Members {
			if !desiredActive[member.UserID] {
				removedUsers = append(removedUsers, member.UserID)
				if err := CancelTeamModelRequestsForMember(tx, actorID, current.ID, member.UserID, "membership_unavailable"); err != nil {
					return err
				}
				if err := cancelTeamQuotaRequestsForMember(tx, current.ID, member.UserID, "membership_unavailable"); err != nil {
					return err
				}
			}
		}
		if err := tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, teamID)).Delete(&entity.TeamMembership{}).Error; err != nil {
			return err
		}
		for _, member := range members {
			previous, retained := existing[member.UserID]
			relationID := previous.ID
			joinedAt := previous.JoinedAt
			if relationID == "" {
				relationID, err = id.NewPrefixed("tmm")
				if err != nil {
					return err
				}
				joinedAt = newMemberTeamJoinedAt(time.Now())
			}
			if retained && (previous.TeamID != teamID || previous.UserID != member.UserID) {
				return apperrors.ErrInternal
			}
			if err := tx.Create(&entity.TeamMembership{ID: relationID, TeamID: teamID, UserID: member.UserID, Role: member.Role, Status: member.Status, JoinedAt: joinedAt}).Error; err != nil {
				return err
			}
		}
		if err := reconcileTeamQuotaRequestOwners(tx, current.ID); err != nil {
			return err
		}
		if err := appendAudit(tx, actorID, "team.members.replace", "teams", teamID); err != nil {
			return err
		}
		result, err = resourceRecord(tx, TeamResource, teamID, false)
		return err
	})
	if err == nil {
		for _, userID := range removedUsers {
			s.invalidateRuntimeTeamMember(result.ID, userID)
		}
	}
	return result, s.refreshAfterMutation(ctx, catalogError(err))
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
		if err := activeProjectResourceUsers(tx, userIDs); err != nil {
			return err
		}
		var existingRows []entity.ProjectManager
		if err := tx.Select("id", "project_id", "user_id").
			Where(database.ExactText(tx, clause.Column{Name: "project_id"}, current.ID)).Find(&existingRows).Error; err != nil {
			return err
		}
		existing := projectManagerIdentities(existingRows, current.ID, userIDs)
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
	return result, s.refreshAfterMutation(ctx, catalogError(err))
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
	reduced := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if kind == TeamResource {
			allowed, err := teamTargetActionAllowed(tx, actorID, resourceID, "teams.models.write")
			if err != nil {
				return err
			}
			if !allowed {
				return apperrors.ErrForbidden
			}
		} else {
			allowed, err := resourcePermission(tx, actorID, ProjectResource, "models.write")
			if err != nil {
				return err
			}
			if !allowed {
				return apperrors.ErrForbidden
			}
		}
		current, err := resourceRecord(tx, kind, resourceID, true)
		if err != nil {
			return err
		}
		if current.ID != resourceID {
			return apperrors.ErrNotFound
		}
		if current.Status == entity.ResourceArchived {
			return catalogConflict
		}
		for _, modelID := range current.ModelIDs {
			if !seen[modelID] {
				reduced = true
			}
		}
		if len(modelIDs) > 0 {
			if kind == ProjectResource {
				var models []entity.Model
				if err := projectIdentityQuery(tx.Model(&entity.Model{}), modelIDs).Select("id", "status").
					Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ResourceActive)).Find(&models).Error; err != nil {
					return err
				}
				selected := make([]string, 0, len(models))
				for _, model := range models {
					if model.Status != entity.ResourceActive {
						return apperrors.ErrBadRequest
					}
					selected = append(selected, model.ID)
				}
				if !projectSelectionMatches(selected, modelIDs) {
					return apperrors.ErrBadRequest
				}
			} else {
				var count int64
				query := tx.Model(&entity.Model{}).Where("id IN ? AND status = ?", modelIDs, entity.ResourceActive)
				if kind == TeamResource {
					query = teamRoleDefinitionQuery(tx.Model(&entity.Model{}), modelIDs).Where(teamQuotaEquality(tx, "status", entity.ResourceActive))
				}
				if err := query.Count(&count).Error; err != nil {
					return err
				}
				if count != int64(len(modelIDs)) {
					return apperrors.ErrBadRequest
				}
			}
		}
		if kind == TeamResource {
			if err := replaceTeamModelGrants(tx, current.ID, modelIDs); err != nil {
				return err
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
	if kind == ProjectResource {
		if err == nil && reduced {
			s.InvalidateRuntimeProject(resourceID)
		}
		return result, s.refreshAfterMutation(ctx, catalogError(err))
	}
	if err == nil && reduced {
		s.invalidateRuntimeTeam(result.ID)
	}
	return result, s.refreshAfterMutation(ctx, catalogError(err))
}

// The caller must hold the governance singleton lock. All owner/manager changes
// take that same lock, serializing account suspension with resource continuity.
func validateResourceContinuity(tx *gorm.DB, userID string) error {
	var teamIDs []string
	if err := tx.Table("team_memberships m").Select("m.team_id").Joins("JOIN teams t ON t.id = m.team_id").Where("m.user_id = ? AND m.role = ? AND m.status = ? AND t.status <> ?", userID, entity.TeamOwner, entity.ResourceActive, entity.ResourceArchived).Scan(&teamIDs).Error; err != nil {
		return err
	}
	for _, teamID := range teamIDs {
		count, err := admittedTeamOwners(tx, teamID, userID)
		if err != nil {
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
		count, err := admittedProjectManagers(tx, projectID, userID)
		if err != nil {
			return err
		}
		if count == 0 {
			return catalogConflict
		}
	}
	return nil
}
