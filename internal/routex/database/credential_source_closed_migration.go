package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// V89 is additive. NULL is unproven historical closure, never backfilled from
// stopped instances, expired leases, missing processes or prior object ACKs.
type credentialSourceClosedV89 struct {
	ProcessID string     `gorm:"primaryKey;size:30"`
	ClosedAt  *time.Time `gorm:"precision:6"`
}

func (credentialSourceClosedV89) TableName() string { return "credential_source_processes" }
func credentialSourceClosedMigration(db *gorm.DB) error {
	schema := &credentialSourceClosedV89{}
	if !db.Migrator().HasColumn(schema, "ClosedAt") {
		if err := db.Migrator().AddColumn(schema, "ClosedAt"); err != nil {
			return err
		}
	}
	// Inspect retained partial DDL; never coerce an existing value/default into
	// a positive closed proof. Only the additive nullable column is created.
	columns, err := db.Migrator().ColumnTypes(schema)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "closed_at" {
			continue
		}
		nullable, known := column.Nullable()
		precision, _, sized := column.DecimalSize()
		value, hasDefault := column.DefaultValue()
		kind := strings.ToLower(column.DatabaseTypeName())
		if !known || !nullable || !sized || precision != 6 || hasDefault && value != "" && !strings.EqualFold(value, "NULL") || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
			return fmt.Errorf("invalid retained source-closure column shape")
		}
		return nil
	}
	return fmt.Errorf("source-closure column is unavailable")
}
