package service

import (
	"slices"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
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
		if err := admittedResourceUsers(tx, assignment.ManagerUserIDs); err != nil {
			return err
		}
		assignedProjects[assignment.ProjectID] = len(assignment.ManagerUserIDs) > 0
	}
	for _, assignment := range assignments.Teams {
		resource, exists := teams[assignment.TeamID]
		if !exists || resource.Status == entity.ResourceArchived {
			return apperrors.ErrBadRequest
		}
		if err := admittedResourceUsers(tx, assignment.OwnerUserIDs); err != nil {
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

// Automatic successor selection uses complete private admission facts, never the
// public People projection. Final sorted User locks and fresh assignment validation
// still run before completion, so this read does not replace continuity checks.
func prepareEmergencyOffboardingAssignments(tx *gorm.DB, inventory *OffboardingInventory, actorID string, assignments OffboardingAssignments) (OffboardingAssignments, error) {
	ids := []string{}
	for _, team := range inventory.Teams {
		if !team.RequiresSuccessor || slices.ContainsFunc(assignments.Teams, func(a OffboardingTeamAssignment) bool { return a.TeamID == team.ID }) {
			continue
		}
		for _, person := range team.People {
			if person.UserID != inventory.UserID && !person.Disabled && person.Status == entity.ResourceActive {
				ids = append(ids, person.UserID)
			}
		}
	}
	sort.Strings(ids)
	ids = slices.Compact(ids)
	admitted := map[string]bool{}
	for start := 0; start < len(ids); start += 500 {
		batch := ids[start:min(start+500, len(ids))]
		var users []entity.User
		if err := tx.Session(&gorm.Session{NewDB: true}).Where("id IN ?", batch).Limit(len(batch) + 1).Find(&users).Error; err != nil {
			return OffboardingAssignments{}, err
		}
		if len(users) > len(batch) {
			return OffboardingAssignments{}, apperrors.ErrInternal
		}
		applications, err := loadRegistrationApplications(tx, users)
		if err != nil {
			return OffboardingAssignments{}, err
		}
		for _, user := range users {
			// Collation aliases must not fill an exact inventory candidate slot.
			if slices.Contains(batch, user.ID) {
				admission, _ := registrationAdmission(user, applications)
				admitted[user.ID] = admission.AdmissionEligible
			}
		}
	}
	return emergencyOffboardingAssignments(inventory, actorID, assignments, admitted), nil
}

func emergencyOffboardingAssignments(inventory *OffboardingInventory, actorID string, assignments OffboardingAssignments, admitted map[string]bool) OffboardingAssignments {
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
		// admitted active member; an empty team requires an explicit addition.
		for _, person := range team.People {
			if person.UserID != inventory.UserID && !person.Disabled && person.Status == entity.ResourceActive && admitted[person.UserID] {
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
			err := tx.Where(database.ExactText(tx, clause.Column{Name: "team_id"}, assignment.TeamID)).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID)).Find(&membership).Error
			if err != nil {
				return nil, err
			}
			if membership.ID != "" && (membership.TeamID != assignment.TeamID || membership.UserID != userID || !safeTeamSessionID(membership.ID) || membership.JoinedAt != nil && membership.JoinedAt.IsZero()) {
				return nil, apperrors.ErrInternal
			}
			if membership.ID == "" {
				relationID, err := id.NewPrefixed("tmm")
				if err != nil {
					return nil, err
				}
				membership = entity.TeamMembership{ID: relationID, TeamID: assignment.TeamID, UserID: userID, Role: entity.TeamMember, Status: entity.ResourceActive, JoinedAt: newMemberTeamJoinedAt(time.Now())}
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
		var stored entity.APIKey
		if err := tx.Select("id", "user_id", "status").First(&stored, "id = ? AND user_id = ?", key.ID, inventory.UserID).Error; err != nil {
			return nil, err
		}
		if stored.ID != key.ID || stored.UserID != inventory.UserID {
			return nil, apperrors.ErrNotFound
		}
		if err := changePersonalKeyStatus(tx, &stored, entity.KeyRevoked); err != nil {
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
	if err := advanceMemberRoleRevision(tx, inventory.UserID); err != nil {
		return nil, err
	}
	if err := CancelTeamModelRequestsForUser(tx, actorID, inventory.UserID, "applicant_offboarded"); err != nil {
		return nil, err
	}
	if err := CancelPersonalModelRequestsForUser(tx, actorID, inventory.UserID, "applicant_offboarded"); err != nil {
		return nil, err
	}
	if err := cancelTeamQuotaRequestsForUser(tx, inventory.UserID, "applicant_offboarded"); err != nil {
		return nil, err
	}
	// The removed relationships still identify Teams whose pending owner stage
	// needs current eligibility review after responsibility handover.
	for _, membership := range inventory.memberships {
		if err := reconcileTeamQuotaRequestOwners(tx, membership.TeamID); err != nil {
			return nil, err
		}
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
	// Initial completion and idempotent retries expose the same persisted receipt.
	if err := tx.Session(&gorm.Session{NewDB: true}).Where("id = ?", row.ID).Take(row).Error; err != nil {
		return nil, err
	}
	return projects, nil
}
