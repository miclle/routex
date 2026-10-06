package service

import (
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RoleAssignmentKind string

const (
	RoleAssignmentIntrinsic RoleAssignmentKind = "intrinsic"
	RoleAssignmentExplicit  RoleAssignmentKind = "explicit"
)

// Definition immutability and explicit assignment eligibility are independent.
// Only these fixed identities may be builtin; classification grants no powers.
func roleAssignmentKind(role entity.Role) (RoleAssignmentKind, error) {
	if !memberRoleID(role.ID) {
		return "", memberRolesUnavailable
	}
	switch role.ID {
	case "rol_admin", "rol_member":
		if !role.Builtin {
			return "", memberRolesUnavailable
		}
		return RoleAssignmentIntrinsic, nil
	case "rol_procurement", "rol_finance", "rol_operations":
		if !role.Builtin {
			return "", memberRolesUnavailable
		}
		return RoleAssignmentExplicit, nil
	default:
		if role.Builtin {
			return "", memberRolesUnavailable
		}
		return RoleAssignmentExplicit, nil
	}
}

func explicitRoleScope(tx *gorm.DB, table string) clause.Expression {
	duties := []clause.Expression{}
	for _, id := range []string{"rol_procurement", "rol_finance", "rol_operations"} {
		duties = append(duties, database.ExactText(tx, clause.Column{Table: table, Name: "id"}, id))
	}
	return clause.Or(
		clause.Eq{Column: clause.Column{Table: table, Name: "builtin"}, Value: false},
		clause.And(clause.Eq{Column: clause.Column{Table: table, Name: "builtin"}, Value: true}, clause.Or(duties...)),
	)
}
