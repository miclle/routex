package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

const connectionEnablementVersion = 76

// Frozen V76 adds an independent routing gate. Existing and new rows default
// enabled; no child Credential, ProviderModel, binding or history is altered.
type connectionEnablementV76 struct {
	Enabled bool `gorm:"not null;default:true"`
}

func (connectionEnablementV76) TableName() string { return "provider_connections" }
func migrateConnectionEnablementV76(db *gorm.DB) error {
	frozen := &connectionEnablementV76{}
	if !db.Migrator().HasColumn(frozen, "Enabled") {
		if err := db.Migrator().AddColumn(frozen, "Enabled"); err != nil {
			return err
		}
	}
	columns, err := db.Migrator().ColumnTypes(frozen)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if column.Name() != "enabled" {
			continue
		}
		nullable, known := column.Nullable()
		value, defaulted := column.DefaultValue()
		kind := strings.ToLower(column.DatabaseTypeName())
		if !known || nullable || !defaulted || value != "true" && value != "1" && value != "'1'" || kind != "bool" && kind != "boolean" && kind != "tinyint" {
			return fmt.Errorf("invalid Connection enabled column")
		}
		return nil
	}
	return fmt.Errorf("missing Connection enabled column")
}
