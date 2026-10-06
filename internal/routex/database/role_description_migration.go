package database

import "gorm.io/gorm"

// Frozen V67 adds only recorded Role descriptions. Historical empty values are
// preserved; product text is never inferred for existing rows.
type roleDescriptionV67 struct {
	ID          string `gorm:"primaryKey;size:30"`
	Description string `gorm:"size:2000;not null;default:'';check:ck_role_description_bytes,OCTET_LENGTH(description) <= 2000"`
}

func (roleDescriptionV67) TableName() string { return "roles" }

func roleDescriptionMigration(db *gorm.DB) error {
	frozen := &roleDescriptionV67{}
	if !db.Migrator().HasColumn(frozen, "Description") {
		if err := db.Migrator().AddColumn(frozen, "Description"); err != nil {
			return err
		}
	}
	// Resume a partially completed MySQL DDL step without replacing the column.
	if !db.Migrator().HasConstraint(frozen, "ck_role_description_bytes") {
		return db.Migrator().CreateConstraint(frozen, "ck_role_description_bytes")
	}
	return nil
}
