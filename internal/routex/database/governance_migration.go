package database

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/pkg/secret"
)

// These private types freeze schema version 6 independently of live entities.
// The users stub declares only the referenced key; migrations never create or
// alter it recursively.
type governanceV6Setting struct {
	ID                  int  `gorm:"primaryKey;autoIncrement:false"`
	RegistrationEnabled bool `gorm:"not null;default:false"`
}

func (governanceV6Setting) TableName() string { return "governance_settings" }

type governanceV6Role struct {
	ID        string    `gorm:"primaryKey;size:30"`
	Name      string    `gorm:"size:100;not null"`
	NameKey   string    `gorm:"size:64;not null;uniqueIndex:uq_roles_name_key"`
	Builtin   bool      `gorm:"not null;default:false"`
	CreatedAt time.Time `gorm:"not null"`
}

func (governanceV6Role) TableName() string { return "roles" }

type governanceV6Permission struct {
	RoleID     string           `gorm:"primaryKey;size:30"`
	Permission string           `gorm:"primaryKey;size:80"`
	Role       governanceV6Role `gorm:"foreignKey:RoleID;references:ID;constraint:fk_permissions_role,OnDelete:RESTRICT"`
}

func (governanceV6Permission) TableName() string { return "role_permissions" }

type governanceV6User struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (governanceV6User) TableName() string { return "users" }

type governanceV6UserRole struct {
	UserID string           `gorm:"primaryKey;size:30"`
	RoleID string           `gorm:"primaryKey;size:30"`
	User   governanceV6User `gorm:"foreignKey:UserID;references:ID;constraint:fk_user_roles_user,OnDelete:RESTRICT"`
	Role   governanceV6Role `gorm:"foreignKey:RoleID;references:ID;constraint:fk_user_roles_role,OnDelete:RESTRICT"`
}

func (governanceV6UserRole) TableName() string { return "user_roles" }

func governanceMigration(db *gorm.DB) error {
	if err := migrateTables(db, &governanceV6Setting{}, &governanceV6Role{}, &governanceV6Permission{}, &governanceV6UserRole{}); err != nil {
		return err
	}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&governanceV6Setting{ID: 1}).Error; err != nil {
		return err
	}
	for _, role := range []governanceV6Role{{ID: "rol_admin", Name: "Administrator", NameKey: secret.SHA256Hex("Administrator"), Builtin: true}, {ID: "rol_member", Name: "Member", NameKey: secret.SHA256Hex("Member"), Builtin: true}} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&role).Error; err != nil {
			return err
		}
	}
	// Frozen permission metadata. Future permissions require a new migration.
	for _, permission := range []string{"members.read", "members.write", "roles.read", "roles.write", "registration.write", "providers.read", "providers.write", "models.read_all", "models.write", "calls.read_all", "audit.read", "system.read"} {
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&governanceV6Permission{RoleID: "rol_admin", Permission: permission}).Error; err != nil {
			return err
		}
	}
	return nil
}
