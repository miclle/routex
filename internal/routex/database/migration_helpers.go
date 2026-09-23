package database

import "gorm.io/gorm"

// migrateTables creates only the frozen models explicitly owned by a version.
// It does not recursively AutoMigrate relation targets or alter existing columns.
// Index/constraint reconciliation makes interrupted nontransactional DDL safe
// to retry before the migration ledger has been advanced.
func migrateTables(db *gorm.DB, models ...any) error {
	for _, model := range models {
		if !db.Migrator().HasTable(model) {
			if err := db.Migrator().CreateTable(model); err != nil {
				return err
			}
		}
		statement := &gorm.Statement{DB: db}
		if err := statement.Parse(model); err != nil {
			return err
		}
		for _, index := range statement.Schema.ParseIndexes() {
			if !db.Migrator().HasIndex(model, index.Name) {
				if err := db.Migrator().CreateIndex(model, index.Name); err != nil {
					return err
				}
			}
		}
		for _, relation := range statement.Schema.Relationships.Relations {
			constraint := relation.ParseConstraint()
			if constraint != nil && constraint.Schema == statement.Schema && !relation.Field.IgnoreMigration && !db.Migrator().HasConstraint(model, constraint.Name) {
				if err := db.Migrator().CreateConstraint(model, constraint.Name); err != nil {
					return err
				}
			}
		}
		for name := range statement.Schema.ParseCheckConstraints() {
			if !db.Migrator().HasConstraint(model, name) {
				if err := db.Migrator().CreateConstraint(model, name); err != nil {
					return err
				}
			}
		}
		for name := range statement.Schema.ParseUniqueConstraints() {
			if !db.Migrator().HasConstraint(model, name) {
				if err := db.Migrator().CreateConstraint(model, name); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

type migrationLedger struct {
	Version   int    `gorm:"primaryKey;autoIncrement:false"`
	AppliedAt string `gorm:"size:40;not null"`
}

func (migrationLedger) TableName() string { return "schema_migrations" }
