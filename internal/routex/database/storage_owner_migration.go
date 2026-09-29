package database

import "gorm.io/gorm"

// storageOwnerV23 freezes the ownership scope added by migration 23.
type storageOwnerV23 struct {
	OwnerKind string `gorm:"size:16;not null;default:user;index:idx_storage_objects_scope,priority:1;check:ck_storage_objects_owner_kind,owner_kind IN ('user','project')"`
	OwnerID   string `gorm:"size:30;not null;index:idx_storage_objects_scope,priority:2"`
}

func (storageOwnerV23) TableName() string { return "storage_objects" }

// storageOwnerMigration adds the scoped owner_kind/owner_id index while
// retaining the released owner_id index. Each operation is independently
// retryable because MySQL may commit DDL before the migration ledger advances.
func storageOwnerMigration(db *gorm.DB) error {
	model := &storageOwnerV23{}
	if !db.Migrator().HasColumn(model, "OwnerKind") {
		if err := db.Migrator().AddColumn(model, "OwnerKind"); err != nil {
			return err
		}
	}
	if err := db.Model(model).Where("owner_kind IS NULL OR owner_kind = ?", "").Update("owner_kind", "user").Error; err != nil {
		return err
	}
	if !db.Migrator().HasConstraint(model, "ck_storage_objects_owner_kind") {
		if err := db.Migrator().CreateConstraint(model, "ck_storage_objects_owner_kind"); err != nil {
			return err
		}
	}

	if !db.Migrator().HasIndex(model, "idx_storage_objects_scope") {
		return db.Migrator().CreateIndex(model, "idx_storage_objects_scope")
	}
	return nil
}
