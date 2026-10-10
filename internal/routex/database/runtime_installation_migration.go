package database

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
	"time"
)

// V100 owns only this observation table. No legacy routing record is promoted.
type runtimeInstallationV100 struct {
	ID                string    `gorm:"primaryKey;size:30"`
	InstanceID        string    `gorm:"size:30;not null;uniqueIndex:idx_runtime_installation_source,priority:1;index:idx_runtime_installation_instance,priority:1"`
	InstanceStartedAt time.Time `gorm:"precision:6;not null"`
	SnapshotID        string    `gorm:"size:30;not null"`
	ProjectionVersion int       `gorm:"not null;check:ck_runtime_installation_projection,projection_version = 1"`
	SourceDigest      string    `gorm:"size:64;not null;uniqueIndex:idx_runtime_installation_source,priority:2"`
	RoutesPublishedAt time.Time `gorm:"precision:6;not null"`
	FirstObservedAt   time.Time `gorm:"precision:6;not null;index:idx_runtime_installation_instance,priority:2"`
}

func (runtimeInstallationV100) TableName() string { return "runtime_installation_observations" }

func runtimeInstallationMigration(db *gorm.DB) error {
	model := &runtimeInstallationV100{}
	if !db.Migrator().HasTable(model) {
		if err := db.Migrator().CreateTable(model); err != nil {
			return err
		}
	} else {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(model); err != nil {
			return err
		}
		// A partial nontransactional CreateTable may leave columns absent. Resume
		// only named frozen columns; incompatible existing metadata is never altered.
		for _, field := range stmt.Schema.Fields {
			if !db.Migrator().HasColumn(model, field.DBName) {
				if err := db.Migrator().AddColumn(model, field.Name); err != nil {
					return err
				}
			}
		}
	}
	if err := validateRuntimeInstallationColumnsV100(db); err != nil {
		return err
	}
	var invalid int64
	if err := db.Model(model).Where("projection_version <> ? OR projection_version IS NULL", 1).Count(&invalid).Error; err != nil {
		return err
	}
	if invalid != 0 {
		return fmt.Errorf("invalid retained runtime installation projection")
	}
	// MySQL can retain this exact shortened index after dropping its second
	// column. A crash after AddColumn can leave all columns present but the same
	// one-column index. Repair only that known nonunique, full-column shape;
	// GORM then recreates the frozen index, and final ordered validation remains.
	columns, err := credentialAttemptStatisticsIndexColumns(db, model.TableName(), "idx_runtime_installation_instance")
	if err != nil {
		return err
	}
	if runtimeInstallationPartialInstanceIndexV100(columns) {
		if err := DropIndex(db, model, "idx_runtime_installation_instance"); err != nil {
			return err
		}
	}
	// Reinstall this version's frozen check after retained-row validation. A
	// same-named weak constraint in partial DDL is not accepted as proof.
	if db.Migrator().HasConstraint(model, "ck_runtime_installation_projection") {
		if err := db.Migrator().DropConstraint(model, "ck_runtime_installation_projection"); err != nil {
			return err
		}
	}
	if err := migrateTables(db, model); err != nil {
		return err
	}
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	for _, index := range stmt.Schema.ParseIndexes() {
		// Reuse the database-owned ordered metadata adapter; GORM GetIndexes loses
		// PostgreSQL key order. DDL still uses GORM, with bound metadata identities.
		columns, err := credentialAttemptStatisticsIndexColumns(db, stmt.Schema.Table, index.Name)
		if err != nil {
			return err
		}
		if len(columns) != len(index.Fields) {
			return fmt.Errorf("invalid runtime installation index %s", index.Name)
		}
		for i, c := range columns {
			if !c.ColumnName.Valid || c.ColumnName.String != index.Fields[i].DBName || !c.Position.Valid || c.Position.Int64 != int64(i+1) || !c.IsUnique.Valid || c.IsUnique.Bool != (index.Class == "UNIQUE") || c.PrefixLength.Valid {
				return fmt.Errorf("invalid runtime installation index %s", index.Name)
			}
		}
	}
	return nil
}

func runtimeInstallationPartialInstanceIndexV100(columns []credentialAttemptStatisticsIndexColumn) bool {
	if len(columns) != 1 {
		return false
	}
	c := columns[0]
	return c.ColumnName.Valid && c.ColumnName.String == "instance_id" && c.Position.Valid && c.Position.Int64 == 1 && c.IsUnique.Valid && !c.IsUnique.Bool && !c.PrefixLength.Valid
}

func validateRuntimeInstallationColumnsV100(db *gorm.DB) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(&runtimeInstallationV100{}); err != nil {
		return err
	}
	columns, err := db.Migrator().ColumnTypes(&runtimeInstallationV100{})
	if err != nil {
		return err
	}
	if len(columns) != len(stmt.Schema.Fields) {
		return fmt.Errorf("invalid runtime installation column set")
	}
	seen := map[string]bool{}
	for _, c := range columns {
		f := stmt.Schema.FieldsByDBName[c.Name()]
		if f == nil || seen[c.Name()] {
			return fmt.Errorf("invalid runtime installation column set")
		}
		seen[c.Name()] = true
		primary, known := c.PrimaryKey()
		if !known || primary != f.PrimaryKey {
			return fmt.Errorf("invalid runtime installation primary key")
		}
		nullable, known := c.Nullable()
		if !known || nullable {
			return fmt.Errorf("invalid runtime installation nullability")
		}
		_, hasDefault := c.DefaultValue()
		if hasDefault {
			return fmt.Errorf("invalid runtime installation default")
		}
		kind := strings.ToLower(c.DatabaseTypeName())
		switch string(f.DataType) {
		case "string":
			length, sized := c.Length()
			if (kind != "varchar" && kind != "character varying") || !sized || length != int64(f.Size) {
				return fmt.Errorf("invalid runtime installation string")
			}
		case "time":
			precision, _, known := c.DecimalSize()
			if !known || precision != 6 || (kind != "timestamp" && kind != "timestamptz" && kind != "datetime") {
				return fmt.Errorf("invalid runtime installation timestamp")
			}
		case "int":
			if kind != "int8" && kind != "bigint" {
				return fmt.Errorf("invalid runtime installation integer")
			}
		default:
			return fmt.Errorf("invalid runtime installation field")
		}
	}
	return nil
}
