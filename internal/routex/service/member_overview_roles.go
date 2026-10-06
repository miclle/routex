package service

import (
	"context"
	"database/sql"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MemberOverviewRole contains display facts only, never permissions or authority.
type MemberOverviewRole struct {
	ID             string             `json:"id"`
	Name           *string            `json:"name"`
	Builtin        bool               `json:"builtin"`
	AssignmentKind RoleAssignmentKind `json:"assignment_kind"`
}
type MemberOverviewRolesPage struct {
	ActorUserID  string               `json:"actor_user_id"`
	ObservedAt   time.Time            `json:"observed_at"`
	IdentityRole string               `json:"identity_role"`
	Roles        []MemberOverviewRole `json:"roles"`
	NextCursor   *string              `json:"next_cursor"`
}
type MemberOverviewRolesFilter struct {
	Cursor string
	Limit  int
}

func normalizeMemberOverviewRolesFilter(actorID string, filter MemberOverviewRolesFilter) (MemberOverviewRolesFilter, string, error) {
	normalized, after, err := normalizeMemberOverviewFilter(actorID, MemberOverviewAccountsFilter(filter))
	if err != nil {
		return filter, "", err
	}
	if after != "" && !memberRoleID(after) {
		return filter, "", apperrors.ErrBadRequest
	}
	return MemberOverviewRolesFilter(normalized), after, nil
}
func memberOverviewRoleAssignmentsQuery(tx *gorm.DB, actorID string) *gorm.DB {
	// Indexed candidate selection is conjunctive with byte-exact ownership.
	return tx.Model(&entity.UserRole{}).Select("UserID", "RoleID").Where("user_id = ?", actorID).
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).Limit(memberRolesReadBudget + 1)
}
func memberOverviewRolePageIDs(actorID, after string, limit int, assignments []entity.UserRole) ([]string, *string, error) {
	if len(assignments) > memberRolesReadBudget {
		return nil, nil, memberRolesOverflow
	}
	ids := make([]string, 0, len(assignments))
	seen := map[string]bool{}
	for _, row := range assignments {
		if row.UserID != actorID || !memberRoleID(row.RoleID) || row.RoleID == "rol_admin" || row.RoleID == "rol_member" || seen[row.RoleID] {
			return nil, nil, memberRolesUnavailable
		}
		seen[row.RoleID] = true
		ids = append(ids, row.RoleID)
	}
	slices.Sort(ids)
	start := 0
	for start < len(ids) && ids[start] <= after {
		start++
	}
	end := min(start+limit, len(ids))
	selected := ids[start:end]
	var next *string
	if end < len(ids) {
		cursor := memberOverviewCursor(actorID, ids[end-1])
		next = &cursor
	}
	return selected, next, nil
}
func projectMemberOverviewRoleLabels(ids []string, rows []entity.Role) ([]MemberOverviewRole, error) {
	wanted := make(map[string]bool, len(ids))
	found := make(map[string]MemberOverviewRole, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	for _, row := range rows {
		if !wanted[row.ID] {
			return nil, memberRolesUnavailable
		}
		delete(wanted, row.ID)
		kind, err := roleAssignmentKind(row)
		if err != nil || kind != RoleAssignmentExplicit {
			return nil, memberRolesUnavailable
		}
		var name *string
		if validCatalogLabel(row.Name) {
			value := row.Name
			name = &value
		}
		found[row.ID] = MemberOverviewRole{ID: row.ID, Name: name, Builtin: row.Builtin, AssignmentKind: kind}
	}
	if len(wanted) != 0 {
		return nil, memberRolesUnavailable
	}
	result := make([]MemberOverviewRole, 0, len(ids))
	for _, id := range ids {
		result = append(result, found[id])
	}
	return result, nil
}

// MemberOverviewRoles reads only the current actor's directly assigned labels.
// Pagination is a fresh snapshot, not a durable assignment receipt or runtime proof.
func (s *Service) MemberOverviewRoles(ctx context.Context, actorID string, filter MemberOverviewRolesFilter) (*MemberOverviewRolesPage, error) {
	filter, after, err := normalizeMemberOverviewRolesFilter(actorID, filter)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result *MemberOverviewRolesPage
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		if actor.CreatedAt.IsZero() || actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
			return apperrors.ErrUnauthorized
		}
		var assignments []entity.UserRole
		if e = memberOverviewRoleAssignmentsQuery(tx.Session(&gorm.Session{NewDB: true}), actorID).Find(&assignments).Error; e != nil {
			return e
		}
		ids, next, e := memberOverviewRolePageIDs(actorID, after, filter.Limit, assignments)
		if e != nil {
			return e
		}
		var rows []entity.Role
		if len(ids) > 0 {
			if e = memberRoleIDsQuery(tx.Session(&gorm.Session{NewDB: true}).Model(&entity.Role{}), "id", ids).
				Select("ID", "Name", "Builtin").Limit(len(ids) + 1).Find(&rows).Error; e != nil {
				return e
			}
		}
		labels, e := projectMemberOverviewRoleLabels(ids, rows)
		if e != nil {
			return e
		}
		result = &MemberOverviewRolesPage{ActorUserID: actor.ID, ObservedAt: time.Now().UTC(), IdentityRole: actor.Role, Roles: labels, NextCursor: next}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
