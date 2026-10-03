package entity

// TeamRole assigns a live role to one Team. It does not create direct user
// roles or grant authority outside the assigned Team.
type TeamRole struct {
	TeamID string `gorm:"primaryKey;size:30;not null"`
	RoleID string `gorm:"primaryKey;size:30;not null;index:idx_team_roles_role"`
	Team   Team   `gorm:"belongsTo:Team;foreignKey:TeamID;references:ID;constraint:fk_team_roles_team,OnDelete:RESTRICT,OnUpdate:RESTRICT"`
	Role   Role   `gorm:"belongsTo:Role;foreignKey:RoleID;references:ID;constraint:fk_team_roles_role,OnDelete:RESTRICT,OnUpdate:RESTRICT"`
}

func (TeamRole) TableName() string { return "team_roles" }
