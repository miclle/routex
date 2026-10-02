package service

import (
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func exactEnabledActor(tx *gorm.DB, actorID string) (entity.User, error) {
	var actor entity.User
	err := tx.Where(database.ExactText(tx, clause.Column{Name: "id"}, actorID)).
		Where("disabled = ? AND offboarded_at IS NULL", false).First(&actor).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && actor.ID != actorID {
		return actor, apperrors.ErrUnauthorized
	}
	return actor, err
}

type exactPermissionIdentity struct {
	RoleID           string
	PermissionRoleID string
	Permission       string
	AssignmentRoleID string
	AssignmentUserID string
}

// Exact bindings prevent database collation from turning an alias into authority.
func exactGovernancePermission(tx *gorm.DB, actor entity.User, permission string) (bool, error) {
	builtin := "rol_member"
	if actor.Role == entity.RoleAdmin {
		builtin = "rol_admin"
	}
	builtinScope := tx.Where(database.ExactText(tx, clause.Column{Table: "role", Name: "id"}, builtin))
	customScope := tx.Where(database.ExactText(tx, clause.Column{Table: "assignment", Name: "user_id"}, actor.ID)).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "assignment", Name: "role_id"}, clause.Column{Table: "role", Name: "id"}))
	var rows []exactPermissionIdentity
	err := tx.Table("role_permissions AS permission").
		Select("role.id AS role_id, permission.role_id AS permission_role_id, permission.permission, assignment.role_id AS assignment_role_id, assignment.user_id AS assignment_user_id").
		Joins("JOIN roles AS role ON role.id = permission.role_id").
		Joins("LEFT JOIN user_roles AS assignment ON assignment.role_id = role.id AND assignment.user_id = ?", actor.ID).
		Where(database.ExactTextColumns(tx, clause.Column{Table: "role", Name: "id"}, clause.Column{Table: "permission", Name: "role_id"})).
		Where(database.ExactText(tx, clause.Column{Table: "permission", Name: "permission"}, permission)).
		Where(tx.Where(builtinScope).Or(customScope)).Limit(1).Scan(&rows).Error
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	row := rows[0]
	return row.Permission == permission && row.PermissionRoleID == row.RoleID &&
		(row.RoleID == builtin || row.AssignmentUserID == actor.ID && row.AssignmentRoleID == row.RoleID), nil
}
