package service

import (
	"slices"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

func validateOffboardingAssignments(tx *gorm.DB, inventory *OffboardingInventory, assignments OffboardingAssignments) error {
	projects, teams := map[string]OffboardingResource{}, map[string]OffboardingResource{}
	for _, resource := range inventory.Projects {
		projects[resource.ID] = resource
	}
	for _, resource := range inventory.Teams {
		teams[resource.ID] = resource
	}
	assignedProjects, assignedTeams := map[string]bool{}, map[string]bool{}
	for _, assignment := range assignments.Projects {
		resource, exists := projects[assignment.ProjectID]
		if !exists || resource.Status == entity.ResourceArchived {
			return apperrors.ErrBadRequest
		}
		if err := activeResourceUsers(tx, assignment.ManagerUserIDs); err != nil {
			return err
		}
		assignedProjects[assignment.ProjectID] = len(assignment.ManagerUserIDs) > 0
	}
	for _, assignment := range assignments.Teams {
		resource, exists := teams[assignment.TeamID]
		if !exists || resource.Status == entity.ResourceArchived {
			return apperrors.ErrBadRequest
		}
		if err := activeResourceUsers(tx, assignment.OwnerUserIDs); err != nil {
			return err
		}
		for _, userID := range assignment.OwnerUserIDs {
			activeMember := false
			for _, person := range resource.People {
				if person.UserID == userID && !person.Disabled && person.Status == entity.ResourceActive {
					activeMember = true
					break
				}
			}
			if !activeMember && !slices.Contains(assignment.AddMemberUserIDs, userID) {
				return catalogConflict
			}
		}
		assignedTeams[assignment.TeamID] = len(assignment.OwnerUserIDs) > 0
	}
	for _, resource := range inventory.Projects {
		if resource.RequiresSuccessor && !assignedProjects[resource.ID] {
			return catalogConflict
		}
	}
	for _, resource := range inventory.Teams {
		if resource.RequiresSuccessor && !assignedTeams[resource.ID] {
			return catalogConflict
		}
	}
	return nil
}

func emergencyOffboardingAssignments(inventory *OffboardingInventory, actorID string, assignments OffboardingAssignments) OffboardingAssignments {
	for _, project := range inventory.Projects {
		if project.RequiresSuccessor {
			assignments.Projects = append(assignments.Projects, OffboardingProjectAssignment{ProjectID: project.ID, ManagerUserIDs: []string{actorID}})
		}
	}
	for _, team := range inventory.Teams {
		if !team.RequiresSuccessor {
			continue
		}
		explicit := false
		for _, assignment := range assignments.Teams {
			if assignment.TeamID == team.ID {
				explicit = true
				break
			}
		}
		if explicit {
			continue
		}
		// Inventory members are ordered by stable user ID. Prefer an existing
		// enabled active member; an empty team requires an explicit addition.
		for _, person := range team.People {
			if person.UserID != inventory.UserID && !person.Disabled && person.Status == entity.ResourceActive {
				assignments.Teams = append(assignments.Teams, OffboardingTeamAssignment{TeamID: team.ID, OwnerUserIDs: []string{person.UserID}})
				break
			}
		}
	}
	return assignments
}

func applyOffboarding(tx *gorm.DB, actorID string, inventory *OffboardingInventory, assignments OffboardingAssignments, row *entity.OffboardingCase) ([]string, error) {
	for _, assignment := range assignments.Projects {
		for _, userID := range assignment.ManagerUserIDs {
			var count int64
			if err := tx.Model(&entity.ProjectManager{}).Where("project_id = ? AND user_id = ?", assignment.ProjectID, userID).Count(&count).Error; err != nil {
				return nil, err
			}
			if count > 0 {
				continue
			}
			relationID, err := id.NewPrefixed("pjm")
			if err != nil {
				return nil, err
			}
			if err := tx.Create(&entity.ProjectManager{ID: relationID, ProjectID: assignment.ProjectID, UserID: userID}).Error; err != nil {
				return nil, err
			}
			if err := appendAudit(tx, actorID, "project.manager.add", "projects", assignment.ProjectID); err != nil {
				return nil, err
			}
		}
	}
	for _, assignment := range assignments.Teams {
		for _, userID := range assignment.OwnerUserIDs {
			var membership entity.TeamMembership
			err := tx.Where("team_id = ? AND user_id = ?", assignment.TeamID, userID).Find(&membership).Error
			if err != nil {
				return nil, err
			}
			if membership.ID == "" {
				relationID, err := id.NewPrefixed("tmm")
				if err != nil {
					return nil, err
				}
				membership = entity.TeamMembership{ID: relationID, TeamID: assignment.TeamID, UserID: userID, Role: entity.TeamMember, Status: entity.ResourceActive}
				if err := tx.Create(&membership).Error; err != nil {
					return nil, err
				}
				if err := appendAudit(tx, actorID, "team.member.add", "teams", assignment.TeamID); err != nil {
					return nil, err
				}
			} else if membership.Status != entity.ResourceActive {
				if err := tx.Model(&membership).Update("status", entity.ResourceActive).Error; err != nil {
					return nil, err
				}
				if err := appendAudit(tx, actorID, "team.member.enable", "teams", assignment.TeamID); err != nil {
					return nil, err
				}
			}
			if membership.Role != entity.TeamOwner {
				if err := tx.Model(&membership).Update("role", entity.TeamOwner).Error; err != nil {
					return nil, err
				}
				if err := appendAudit(tx, actorID, "team.owner.assign", "teams", assignment.TeamID); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := validateResourceContinuity(tx, inventory.UserID); err != nil {
		return nil, err
	}
	projects := make([]string, 0, len(inventory.Projects))
	for _, project := range inventory.Projects {
		if err := tx.Where("project_id = ? AND user_id = ?", project.ID, inventory.UserID).Delete(&entity.ProjectManager{}).Error; err != nil {
			return nil, err
		}
		if err := appendAudit(tx, actorID, "project.manager.remove", "projects", project.ID); err != nil {
			return nil, err
		}
		projects = append(projects, project.ID)
	}
	for _, membership := range inventory.memberships {
		if err := tx.Delete(&membership).Error; err != nil {
			return nil, err
		}
		if err := appendAudit(tx, actorID, "team.member.remove", "teams", membership.TeamID); err != nil {
			return nil, err
		}
	}
	for _, key := range inventory.PersonalKeys {
		if key.Status == entity.KeyRevoked {
			continue
		}
		if err := tx.Model(&entity.APIKey{}).Where("id = ?", key.ID).Update("status", entity.KeyRevoked).Error; err != nil {
			return nil, err
		}
		if err := appendAudit(tx, actorID, "key.revoke", "api_key", key.ID); err != nil {
			return nil, err
		}
	}
	if len(inventory.roles) > 0 {
		if err := tx.Where("user_id = ?", inventory.UserID).Delete(&entity.UserRole{}).Error; err != nil {
			return nil, err
		}
		if err := appendAudit(tx, actorID, "member.roles.clear", "user", inventory.UserID); err != nil {
			return nil, err
		}
	}
	if err := tx.Where("user_id = ?", inventory.UserID).Delete(&entity.Session{}).Error; err != nil {
		return nil, err
	}
	// The base administrator identity is a direct role too. Retaining it while
	// clearing only UserRole rows would restore old powers on account enable.
	if err := invalidateMFAChallenges(tx, inventory.UserID); err != nil {
		return nil, err
	}
	if err := tx.Model(&entity.User{}).Where("id = ?", inventory.UserID).Updates(map[string]any{"disabled": true, "offboarded_at": time.Now().UTC(), "role": entity.RoleMember}).Error; err != nil {
		return nil, err
	}
	if inventory.userRole != entity.RoleMember {
		if err := appendAudit(tx, actorID, "member.role.remove", "user", inventory.UserID); err != nil {
			return nil, err
		}
	}
	if err := appendAudit(tx, actorID, "member.disable", "user", inventory.UserID); err != nil {
		return nil, err
	}
	if err := appendAudit(tx, actorID, "member.sessions.revoke", "user", inventory.UserID); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row.Status = "completed"
	row.CompletedAt = &now
	row.CompletedBy = actorID
	if err := tx.Save(row).Error; err != nil {
		return nil, err
	}
	if err := appendAudit(tx, actorID, "offboarding.complete", "offboarding_case", row.ID); err != nil {
		return nil, err
	}
	return projects, nil
}
