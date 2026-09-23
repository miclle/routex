package database

import (
	"time"

	"gorm.io/gorm"
)

type projectKeyV9Project struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectKeyV9Project) TableName() string { return "projects" }

type projectKeyV9User struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectKeyV9User) TableName() string { return "users" }

type projectKeyV9Model struct {
	ID string `gorm:"primaryKey;size:30"`
}

func (projectKeyV9Model) TableName() string { return "models" }

type projectKeyV9Key struct {
	ID                string `gorm:"primaryKey;size:30"`
	ProjectID         string `gorm:"size:30;not null;index:idx_project_keys_project"`
	CreatorID         string `gorm:"size:30;not null"`
	Name              string `gorm:"size:100;not null"`
	Prefix            string `gorm:"size:16;not null"`
	TokenHash         string `gorm:"size:64;not null;uniqueIndex:uq_project_keys_hash"`
	Status            string `gorm:"size:20;not null;check:ck_project_keys_status,status IN ('pending','active','disabled','revoked')"`
	DeliveryMode      string `gorm:"size:20;not null;check:ck_project_keys_delivery,delivery_mode = 'manual'"`
	ExpiresAt         *time.Time
	DeliveryExpiresAt *time.Time
	ReplacesKeyID     *string             `gorm:"size:30;index:idx_project_keys_replacement"`
	ActivateOnConfirm bool                `gorm:"not null"`
	CreatedAt         time.Time           `gorm:"not null"`
	UpdatedAt         time.Time           `gorm:"not null"`
	Project           projectKeyV9Project `gorm:"belongsTo:Project;foreignKey:ProjectID;references:ID;constraint:fk_project_keys_project,OnDelete:RESTRICT"`
	Creator           projectKeyV9User    `gorm:"belongsTo:Creator;foreignKey:CreatorID;references:ID;constraint:fk_project_keys_creator,OnDelete:RESTRICT"`
	Replaces          *projectKeyV9Key    `gorm:"belongsTo:Replaces;foreignKey:ReplacesKeyID;references:ID;constraint:fk_project_keys_replaces,OnDelete:RESTRICT"`
}

func (projectKeyV9Key) TableName() string { return "project_api_keys" }

type projectKeyV9Scope struct {
	KeyID   string            `gorm:"primaryKey;size:30"`
	ModelID string            `gorm:"primaryKey;size:30;index:idx_project_key_models_model"`
	Key     projectKeyV9Key   `gorm:"belongsTo:Key;foreignKey:KeyID;references:ID;constraint:fk_project_key_scope_key,OnDelete:RESTRICT"`
	Model   projectKeyV9Model `gorm:"belongsTo:Model;foreignKey:ModelID;references:ID;constraint:fk_project_key_scope_model,OnDelete:RESTRICT"`
}

func (projectKeyV9Scope) TableName() string { return "project_api_key_models" }

// Only ProjectID is added to the existing immutable fact schema. The two other
// frozen columns describe the new composite index without altering their types.
type projectKeyV9Call struct {
	ID        string    `gorm:"column:request_id;primaryKey;size:64;index:idx_calls_project_time,priority:3"`
	ProjectID string    `gorm:"size:30;not null;default:'';index:idx_calls_project_time,priority:1"`
	StartedAt time.Time `gorm:"precision:6;not null;index:idx_calls_project_time,priority:2"`
}

func (projectKeyV9Call) TableName() string { return "call_records" }
func projectKeyMigration(db *gorm.DB) error {
	if err := migrateTables(db, &projectKeyV9Key{}, &projectKeyV9Scope{}); err != nil {
		return err
	}
	if !db.Migrator().HasColumn(&projectKeyV9Call{}, "ProjectID") {
		if err := db.Migrator().AddColumn(&projectKeyV9Call{}, "ProjectID"); err != nil {
			return err
		}
	}
	return migrateTables(db, &projectKeyV9Call{})
}
