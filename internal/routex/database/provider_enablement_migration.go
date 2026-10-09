package database

import (
	"fmt"
	"gorm.io/gorm"
	"strings"
)

// V91 adds a Provider gate and shared name/status revision without changing any
// child state or routing history. Its private schema remains frozen.
type providerEnablementV91 struct {
	Enabled bool   `gorm:"not null;default:true"`
	ETag    string `gorm:"size:30;not null;default:0"`
}

func (providerEnablementV91) TableName() string { return "providers" }
func providerEnablementMigration(db *gorm.DB) error {
	frozen := &providerEnablementV91{}
	for _, field := range []string{"Enabled", "ETag"} {
		if !db.Migrator().HasColumn(frozen, field) {
			if err := db.Migrator().AddColumn(frozen, field); err != nil {
				return err
			}
		}
	}
	columns, err := db.Migrator().ColumnTypes(frozen)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, column := range columns {
		name := column.Name()
		if name != "enabled" && name != "e_tag" {
			continue
		}
		nullable, known := column.Nullable()
		value, defaulted := column.DefaultValue()
		kind := strings.ToLower(column.DatabaseTypeName())
		if !known || nullable || !defaulted {
			return fmt.Errorf("invalid Provider %s column", name)
		}
		if name == "enabled" {
			if value != "true" && value != "1" && value != "'1'" || kind != "bool" && kind != "boolean" && kind != "tinyint" {
				return fmt.Errorf("invalid Provider enabled column")
			}
		} else {
			size, sized := column.Length()
			if value != "0" && value != "'0'" && value != "'0'::character varying" || kind != "varchar" && kind != "character varying" || !sized || size != 30 {
				return fmt.Errorf("invalid Provider revision column")
			}
		}
		found[name] = true
	}
	if !found["enabled"] || !found["e_tag"] {
		return fmt.Errorf("missing Provider enablement column")
	}
	return nil
}
