package database

import "gorm.io/gorm"

// Version 41 assigns live roles without migrating the referenced Team or Role
// schemas. Lifecycle archival retains the Team row and its assignments.
type teamRoleTeamV41 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (teamRoleTeamV41) TableName() string { return "teams" }

type teamRoleRoleV41 struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (teamRoleRoleV41) TableName() string { return "roles" }

type teamRoleV41 struct {
	TeamID string          `gorm:"primaryKey;size:30;not null"`
	RoleID string          `gorm:"primaryKey;size:30;not null;index:idx_team_roles_role"`
	Team   teamRoleTeamV41 `gorm:"belongsTo:Team;foreignKey:TeamID;references:ID;constraint:fk_team_roles_team,OnDelete:RESTRICT,OnUpdate:RESTRICT"`
	Role   teamRoleRoleV41 `gorm:"belongsTo:Role;foreignKey:RoleID;references:ID;constraint:fk_team_roles_role,OnDelete:RESTRICT,OnUpdate:RESTRICT"`
}

func (teamRoleV41) TableName() string { return "team_roles" }

func teamRoleMigration(db *gorm.DB) error { return migrateTables(db, &teamRoleV41{}) }
