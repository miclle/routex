package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Version 8 owns only these frozen definitions. Relation stubs never migrate
// existing identity or model tables recursively.
type resourcesV8User struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (resourcesV8User) TableName() string { return "users" }

type resourcesV8Model struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (resourcesV8Model) TableName() string { return "models" }

type resourcesV8Team struct {
	ID          string    `gorm:"primaryKey;size:30"`
	Name        string    `gorm:"size:100;not null"`
	Description string    `gorm:"size:2000;not null"`
	Status      string    `gorm:"size:20;not null;check:ck_teams_status,status IN ('active','disabled','archived')"`
	CreatedAt   time.Time `gorm:"not null"`
	UpdatedAt   time.Time `gorm:"not null"`
}

func (resourcesV8Team) TableName() string { return "teams" }

type resourcesV8Membership struct {
	ID     string          `gorm:"primaryKey;size:30"`
	TeamID string          `gorm:"size:30;not null;uniqueIndex:uq_team_member"`
	UserID string          `gorm:"size:30;not null;uniqueIndex:uq_team_member;index:idx_team_memberships_user"`
	Role   string          `gorm:"size:20;not null;check:ck_team_membership_role,role IN ('owner','member')"`
	Status string          `gorm:"size:20;not null;check:ck_team_membership_status,status IN ('active','disabled')"`
	Team   resourcesV8Team `gorm:"belongsTo:Team;foreignKey:TeamID;references:ID;constraint:fk_team_memberships_team,OnDelete:RESTRICT"`
	User   resourcesV8User `gorm:"belongsTo:User;foreignKey:UserID;references:ID;constraint:fk_team_memberships_user,OnDelete:RESTRICT"`
}

func (resourcesV8Membership) TableName() string { return "team_memberships" }

type resourcesV8TeamGrant struct {
	TeamID  string           `gorm:"primaryKey;size:30"`
	ModelID string           `gorm:"primaryKey;size:30;index:idx_team_grants_model"`
	Team    resourcesV8Team  `gorm:"belongsTo:Team;foreignKey:TeamID;references:ID;constraint:fk_team_grants_team,OnDelete:RESTRICT"`
	Model   resourcesV8Model `gorm:"belongsTo:Model;foreignKey:ModelID;references:ID;constraint:fk_team_grants_model,OnDelete:RESTRICT"`
}

func (resourcesV8TeamGrant) TableName() string { return "team_model_grants" }

type resourcesV8Project struct {
	ID          string          `gorm:"primaryKey;size:30"`
	Name        string          `gorm:"size:100;not null"`
	Description string          `gorm:"size:2000;not null"`
	Status      string          `gorm:"size:20;not null;check:ck_projects_status,status IN ('active','disabled','archived')"`
	CreatorID   string          `gorm:"size:30;not null;index:idx_projects_creator"`
	CreatedAt   time.Time       `gorm:"not null"`
	UpdatedAt   time.Time       `gorm:"not null"`
	Creator     resourcesV8User `gorm:"belongsTo:Creator;foreignKey:CreatorID;references:ID;constraint:fk_projects_creator,OnDelete:RESTRICT"`
}

func (resourcesV8Project) TableName() string { return "projects" }

type resourcesV8Manager struct {
	ID        string             `gorm:"primaryKey;size:30"`
	ProjectID string             `gorm:"size:30;not null;uniqueIndex:uq_project_manager"`
	UserID    string             `gorm:"size:30;not null;uniqueIndex:uq_project_manager;index:idx_project_managers_user"`
	Project   resourcesV8Project `gorm:"belongsTo:Project;foreignKey:ProjectID;references:ID;constraint:fk_project_managers_project,OnDelete:RESTRICT"`
	User      resourcesV8User    `gorm:"belongsTo:User;foreignKey:UserID;references:ID;constraint:fk_project_managers_user,OnDelete:RESTRICT"`
}

func (resourcesV8Manager) TableName() string { return "project_managers" }

type resourcesV8ProjectGrant struct {
	ProjectID string             `gorm:"primaryKey;size:30"`
	ModelID   string             `gorm:"primaryKey;size:30;index:idx_project_grants_model"`
	Project   resourcesV8Project `gorm:"belongsTo:Project;foreignKey:ProjectID;references:ID;constraint:fk_project_grants_project,OnDelete:RESTRICT"`
	Model     resourcesV8Model   `gorm:"belongsTo:Model;foreignKey:ModelID;references:ID;constraint:fk_project_grants_model,OnDelete:RESTRICT"`
}

func (resourcesV8ProjectGrant) TableName() string { return "project_model_grants" }

type resourcesV8Permission struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (resourcesV8Permission) TableName() string { return "role_permissions" }
func resourcesMigration(db *gorm.DB) error {
	if err := migrateTables(db, &resourcesV8Team{}, &resourcesV8Membership{}, &resourcesV8TeamGrant{}, &resourcesV8Project{}, &resourcesV8Manager{}, &resourcesV8ProjectGrant{}); err != nil {
		return err
	}
	for _, permission := range []string{"teams.read_all", "teams.write", "teams.models.write", "projects.read_all", "projects.write", "projects.models.write"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&resourcesV8Permission{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
