package service

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const roleListMaximumMemberCount int64 = 9007199254740991

var roleListOverflow = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "role list exceeds supported bounds"}

type roleListBaseCount struct {
	Role        string
	MemberCount int64
}
type roleListCustomCount struct {
	RoleID      string
	MemberCount int64
}

// Counts describe retained identities and explicit global assignments. They do
// not attest current admission, effective permissions or Team-scoped membership.
func roleListMemberCounts(tx *gorm.DB, roles []entity.Role) (map[string]int64, error) {
	counts := make(map[string]int64, len(roles))
	custom := make(map[string]bool, len(roles))
	for _, role := range roles {
		counts[role.ID] = 0
		kind, err := roleAssignmentKind(role)
		if err != nil {
			return nil, err
		}
		if kind == RoleAssignmentExplicit {
			custom[role.ID] = true
		}
	}
	var builtin []roleListBaseCount
	err := memberRolesDB(tx).Model(&entity.User{}).Select("role, COUNT(*) AS member_count").
		Where(clause.Or(database.ExactText(tx, clause.Column{Name: "role"}, entity.RoleAdmin), database.ExactText(tx, clause.Column{Name: "role"}, entity.RoleMember))).
		Group("role").Limit(3).Scan(&builtin).Error
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, row := range builtin {
		if row.Role != entity.RoleAdmin && row.Role != entity.RoleMember || seen[row.Role] || row.MemberCount < 0 {
			return nil, memberRolesUnavailable
		}
		if row.MemberCount > roleListMaximumMemberCount {
			return nil, roleListOverflow
		}
		seen[row.Role] = true
		if _, ok := counts["rol_"+row.Role]; !ok {
			return nil, memberRolesUnavailable
		}
		counts["rol_"+row.Role] = row.MemberCount
	}
	var assigned []roleListCustomCount
	err = memberRolesDB(tx).Table("user_roles AS assignment").Clauses(clause.From{
		Tables: []clause.Table{{Name: "user_roles", Alias: "assignment"}},
		Joins: []clause.Join{
			{Type: clause.InnerJoin, Table: clause.Table{Name: "users", Alias: "person"}, ON: clause.Where{Exprs: []clause.Expression{database.ExactTextColumns(tx, clause.Column{Table: "person", Name: "id"}, clause.Column{Table: "assignment", Name: "user_id"})}}},
			{Type: clause.InnerJoin, Table: clause.Table{Name: "roles", Alias: "selected_role"}, ON: clause.Where{Exprs: []clause.Expression{database.ExactTextColumns(tx, clause.Column{Table: "selected_role", Name: "id"}, clause.Column{Table: "assignment", Name: "role_id"})}}},
		},
	}).Select("selected_role.id AS role_id, COUNT(*) AS member_count").
		Where(explicitRoleScope(tx, "selected_role")).
		Group("selected_role.id").Limit(memberRolesCatalogueBudget + 1).Scan(&assigned).Error
	if err != nil {
		return nil, err
	}
	if len(assigned) > memberRolesCatalogueBudget {
		return nil, roleListOverflow
	}
	seen = map[string]bool{}
	for _, row := range assigned {
		if !custom[row.RoleID] || seen[row.RoleID] || row.MemberCount < 0 {
			return nil, memberRolesUnavailable
		}
		if row.MemberCount > roleListMaximumMemberCount {
			return nil, roleListOverflow
		}
		seen[row.RoleID] = true
		counts[row.RoleID] = row.MemberCount
	}
	return counts, nil
}

func (s *Service) ListRoles(ctx context.Context, actorID string) ([]RoleRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result []RoleRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, err := exactEnabledActor(memberRolesDB(tx), actorID)
		if err != nil {
			return err
		}
		if actor.Role != entity.RoleAdmin && actor.Role != entity.RoleMember {
			return apperrors.ErrUnauthorized
		}
		allowed, err := exactGovernancePermissionForAdmittedActor(memberRolesDB(tx), actor, "roles.read")
		if err != nil {
			return err
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var roles []entity.Role
		if err := memberRolesDB(tx).Select("ID", "Name", "Description", "Builtin", "DefinitionRevision").Order("builtin DESC, name, id").Limit(memberRolesCatalogueBudget + 1).Find(&roles).Error; err != nil {
			return err
		}
		if len(roles) > memberRolesCatalogueBudget {
			return roleListOverflow
		}
		ids := map[string]bool{}
		for _, role := range roles {
			_, kindErr := roleAssignmentKind(role)
			if !validRoleDescription(role.Description, true) || ids[role.ID] || kindErr != nil {
				return memberRolesUnavailable
			}
			ids[role.ID] = true
		}
		permissions, err := loadMemberRolePermissions(tx, roles)
		if err != nil {
			if err == memberRolesOverflow {
				return roleListOverflow
			}
			return err
		}
		counts, err := roleListMemberCounts(tx, roles)
		if err != nil {
			return err
		}
		result = make([]RoleRecord, 0, len(roles))
		for _, role := range roles {
			definition, err := projectMemberRoleDefinition(role, permissions[role.ID])
			if err != nil {
				return err
			}
			count := counts[role.ID]
			result = append(result, RoleRecord{Role: role, AssignmentKind: definition.Summary.AssignmentKind, Permissions: slices.Clone(definition.Permissions), MemberCount: &count})
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return result, nil
}
