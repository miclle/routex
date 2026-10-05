package database

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const memberRecentLoginVersion = 55

// Frozen V55 retains historical NULLs. No default, index or synthetic backfill.
type memberRecentLoginV55 struct {
	ID          string     `gorm:"primaryKey;size:30"`
	LastLoginAt *time.Time `json:"-" gorm:"precision:6"`
}

func (memberRecentLoginV55) TableName() string { return "users" }
func memberRecentLoginMigration(db *gorm.DB) error {
	model := &memberRecentLoginV55{}
	if !db.Migrator().HasColumn(model, "LastLoginAt") {
		if err := db.Migrator().AddColumn(model, "LastLoginAt"); err != nil {
			return err
		}
	}
	columns, err := db.Migrator().ColumnTypes(model)
	if err != nil {
		return err
	}
	for _, col := range columns {
		if col.Name() != "last_login_at" {
			continue
		}
		nullable, known := col.Nullable()
		precision, _, sized := col.DecimalSize()
		value, hasDefault := col.DefaultValue()
		kind := strings.ToLower(col.DatabaseTypeName())
		if !known || !nullable || !sized || precision != 6 || hasDefault && value != "" && !strings.EqualFold(value, "NULL") || kind != "timestamp" && kind != "timestamptz" && kind != "datetime" {
			return fmt.Errorf("invalid retained recent-login column shape")
		}
		return nil
	}
	return fmt.Errorf("recent-login column is unavailable")
}
