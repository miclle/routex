package service

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const memberAccessRoleBudget = 10000
const memberAccessTeamBudget = 1000

var memberAccessUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "member access summary unavailable"}

type MemberAccessSummary struct {
	UserID       string            `json:"user_id"`
	ObservedAt   time.Time         `json:"observed_at"`
	UpdatedAt    *time.Time        `json:"updated_at"`
	IdentityRole string            `json:"identity_role"`
	Roles        MemberAccessRoles `json:"roles"`
	Teams        MemberAccessTeams `json:"teams"`
}
type MemberAccessRoles struct {
	Status string             `json:"status"`
	Items  []MemberAccessRole `json:"items"`
}
type MemberAccessRole struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Builtin bool   `json:"builtin"`
}
type MemberAccessTeams struct {
	Status string             `json:"status"`
	Items  []MemberAccessTeam `json:"items"`
}
type MemberAccessTeam struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	MembershipStatus string `json:"membership_status"`
	MembershipRole   string `json:"membership_role"`
}

// Retained display text is not a write validator. Preserve it for escaped
// rendering, including empty names and controls, without silently normalizing it.
func memberAccessLabel(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 100
}
func memberAccessUserQuery(tx *gorm.DB, userID string) *gorm.DB {
	return tx.Session(&gorm.Session{}).Model(&entity.User{}).
		Select("ID", "Role", "Disabled", "OffboardedAt", "CreatedAt", "UpdatedAt", "ApprovalApplicationID").
		Where(database.ExactText(tx, clause.Column{Name: "id"}, userID))
}
func memberAccessRoleQuery(tx *gorm.DB, userID string) *gorm.DB {
	return tx.Session(&gorm.Session{}).Model(&entity.UserRole{}).Select("UserID", "RoleID").
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID)).Limit(memberAccessRoleBudget + 1)
}
func memberAccessTeamQuery(tx *gorm.DB, userID string) *gorm.DB {
	return tx.Session(&gorm.Session{}).Model(&entity.TeamMembership{}).
		Select("ID", "UserID", "TeamID", "Role", "Status").
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, userID)).Limit(memberAccessTeamBudget + 1)
}

func projectMemberAccessRoles(userID string, assignments []entity.UserRole, metadata []entity.Role) MemberAccessRoles {
	result := MemberAccessRoles{Status: "available", Items: []MemberAccessRole{}}
	if len(assignments) > memberAccessRoleBudget {
		return MemberAccessRoles{Status: "overflow"}
	}
	selected := make(map[string]struct{}, len(assignments))
	for _, row := range assignments {
		if row.UserID != userID || !safeTeamSessionID(row.RoleID) {
			return MemberAccessRoles{Status: "unavailable"}
		}
		if _, duplicate := selected[row.RoleID]; duplicate {
			return MemberAccessRoles{Status: "unavailable"}
		}
		selected[row.RoleID] = struct{}{}
	}
	for _, row := range metadata {
		if _, exists := selected[row.ID]; !exists || !memberAccessLabel(row.Name) {
			return MemberAccessRoles{Status: "unavailable"}
		}
		delete(selected, row.ID)
		result.Items = append(result.Items, MemberAccessRole{ID: row.ID, Name: row.Name, Builtin: row.Builtin})
	}
	if len(selected) != 0 {
		return MemberAccessRoles{Status: "unavailable"}
	}
	slices.SortFunc(result.Items, func(a, b MemberAccessRole) int { return strings.Compare(a.ID, b.ID) })
	return result
}
func projectMemberAccessTeams(userID string, memberships []entity.TeamMembership, metadata []entity.Team) MemberAccessTeams {
	result := MemberAccessTeams{Status: "available", Items: []MemberAccessTeam{}}
	if len(memberships) > memberAccessTeamBudget {
		return MemberAccessTeams{Status: "overflow"}
	}
	selected := make(map[string]entity.TeamMembership, len(memberships))
	identities := make(map[string]struct{}, len(memberships))
	for _, row := range memberships {
		if row.UserID != userID || !safeTeamSessionID(row.ID) || !safeTeamSessionID(row.TeamID) ||
			(row.Role != entity.TeamOwner && row.Role != entity.TeamMember) ||
			(row.Status != entity.ResourceActive && row.Status != entity.ResourceDisabled) {
			return MemberAccessTeams{Status: "unavailable"}
		}
		if _, duplicate := selected[row.TeamID]; duplicate {
			return MemberAccessTeams{Status: "unavailable"}
		}
		if _, duplicate := identities[row.ID]; duplicate {
			return MemberAccessTeams{Status: "unavailable"}
		}
		selected[row.TeamID], identities[row.ID] = row, struct{}{}
	}
	for _, row := range metadata {
		membership, exists := selected[row.ID]
		if !exists || !memberAccessLabel(row.Name) ||
			(row.Status != entity.ResourceActive && row.Status != entity.ResourceDisabled && row.Status != entity.ResourceArchived) {
			return MemberAccessTeams{Status: "unavailable"}
		}
		delete(selected, row.ID)
		result.Items = append(result.Items, MemberAccessTeam{ID: row.ID, Name: row.Name, Status: row.Status, MembershipStatus: membership.Status, MembershipRole: membership.Role})
	}
	if len(selected) != 0 {
		return MemberAccessTeams{Status: "unavailable"}
	}
	slices.SortFunc(result.Items, func(a, b MemberAccessTeam) int { return strings.Compare(a.ID, b.ID) })
	return result
}

func readMemberAccessRoles(tx *gorm.DB, userID string) (MemberAccessRoles, error) {
	var assignments []entity.UserRole
	if err := memberAccessRoleQuery(tx, userID).Find(&assignments).Error; err != nil {
		return MemberAccessRoles{}, err
	}
	if len(assignments) == 0 || len(assignments) > memberAccessRoleBudget {
		return projectMemberAccessRoles(userID, assignments, nil), nil
	}
	ids := make([]string, len(assignments))
	for i, row := range assignments {
		ids[i] = row.RoleID
	}
	var rows []entity.Role
	if err := tx.Session(&gorm.Session{}).Model(&entity.Role{}).Select("ID", "Name", "Builtin").Where("id IN ?", ids).Limit(memberAccessRoleBudget + 1).Find(&rows).Error; err != nil {
		return MemberAccessRoles{}, err
	}
	return projectMemberAccessRoles(userID, assignments, rows), nil
}
func readMemberAccessTeams(tx *gorm.DB, userID string) (MemberAccessTeams, error) {
	var memberships []entity.TeamMembership
	if err := memberAccessTeamQuery(tx, userID).Find(&memberships).Error; err != nil {
		return MemberAccessTeams{}, err
	}
	if len(memberships) == 0 || len(memberships) > memberAccessTeamBudget {
		return projectMemberAccessTeams(userID, memberships, nil), nil
	}
	ids := make([]string, len(memberships))
	for i, row := range memberships {
		ids[i] = row.TeamID
	}
	var rows []entity.Team
	if err := tx.Session(&gorm.Session{}).Model(&entity.Team{}).Select("ID", "Name", "Status").Where("id IN ?", ids).Limit(memberAccessTeamBudget + 1).Find(&rows).Error; err != nil {
		return MemberAccessTeams{}, err
	}
	return projectMemberAccessTeams(userID, memberships, rows), nil
}

func (s *Service) GetMemberAccessSummary(ctx context.Context, actorID, userID string) (*MemberAccessSummary, error) {
	if err := memberMetadataIDs(actorID, userID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result MemberAccessSummary
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var actor entity.User
		err := memberAccessUserQuery(tx, actorID).Where("disabled = ? AND offboarded_at IS NULL", false).First(&actor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (actor.ID != actorID || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember) {
			return apperrors.ErrUnauthorized
		}
		if err != nil {
			return err
		}
		if err := requireRegistrationAdmission(tx.Session(&gorm.Session{NewDB: true}), actor); err != nil {
			return err
		}
		read, err := exactGovernancePermissionForAdmittedActor(tx.Session(&gorm.Session{NewDB: true}), actor, "members.read")
		if err != nil {
			return err
		}
		if !read {
			return apperrors.ErrForbidden
		}
		var subject entity.User
		if err := memberAccessUserQuery(tx, userID).First(&subject).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.ErrNotFound
			}
			return err
		}
		if subject.ID != userID || subject.CreatedAt.IsZero() || subject.Role != entity.RoleAdmin && subject.Role != entity.RoleMember {
			return apperrors.ErrNotFound
		}
		result = MemberAccessSummary{UserID: subject.ID, ObservedAt: time.Now().UTC(), IdentityRole: subject.Role,
			Roles: MemberAccessRoles{Status: "not_authorized"}, Teams: MemberAccessTeams{Status: "not_authorized"}}
		if !subject.UpdatedAt.IsZero() {
			updated := subject.UpdatedAt.UTC()
			result.UpdatedAt = &updated
		}
		for _, section := range []struct {
			permission string
			read       func() error
		}{
			{"roles.read", func() error { var err error; result.Roles, err = readMemberAccessRoles(tx, userID); return err }},
			{"teams.read_all", func() error { var err error; result.Teams, err = readMemberAccessTeams(tx, userID); return err }},
		} {
			allowed, err := exactGovernancePermissionForAdmittedActor(tx.Session(&gorm.Session{NewDB: true}), actor, section.permission)
			if err != nil {
				return err
			}
			if allowed {
				if err := section.read(); err != nil {
					return err
				}
			}
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		var domain *apperrors.Error
		if errors.As(err, &domain) {
			return nil, err
		}
		return nil, memberAccessUnavailable
	}
	return &result, nil
}
