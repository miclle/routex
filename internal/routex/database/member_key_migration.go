package database

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/pkg/id"
)

// Frozen V52 definitions; this migration never uses evolving business entities.
type memberKeyRevisionV52 struct {
	ID                string `gorm:"primaryKey;size:30"`
	LifecycleRevision string `gorm:"size:30;not null;default:''"`
}

func (memberKeyRevisionV52) TableName() string { return "api_keys" }

type memberKeyPermissionV52 struct {
	RoleID     string `gorm:"primaryKey;size:30"`
	Permission string `gorm:"primaryKey;size:80"`
}

func (memberKeyPermissionV52) TableName() string { return "role_permissions" }

func memberKeyMigration(db *gorm.DB) error {
	model := &memberKeyRevisionV52{}
	if !db.Migrator().HasColumn(model, "LifecycleRevision") {
		if err := db.Migrator().AddColumn(model, "LifecycleRevision"); err != nil {
			return err
		}
	}
	// Reconcile a partially committed MySQL column/backfill independently before
	// acknowledging V52. Assigned revisions are immutable across repeat startup.
	for {
		var rows []memberKeyRevisionV52
		if err := db.Where("lifecycle_revision = ?", "").Order("id").Limit(200).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}
		for _, row := range rows {
			revision, err := id.NewPrefixed("kvr")
			if err != nil {
				return err
			}
			if err := db.Model(model).Where("id = ? AND lifecycle_revision = ?", row.ID, "").Update("lifecycle_revision", revision).Error; err != nil {
				return err
			}
		}
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&memberKeyPermissionV52{RoleID: "rol_admin", Permission: "members.keys.disable"}).Error
}
