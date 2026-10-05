package database

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestMemberRecentLoginV55FrozenHistoricalNullAndReservedVersion(t *testing.T) {
	frozen, err := schema.Parse(&memberRecentLoginV55{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	current, err := schema.Parse(&entity.User{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if memberRecentLoginVersion != 55 || reflect.ValueOf(memberRecentLoginMigration).IsNil() || frozen.Table != "users" || len(frozen.Fields) != 2 || len(frozen.Relationships.Relations) != 0 {
		t.Fatal("unbounded migration")
	}
	f := frozen.LookUpField("LastLoginAt")
	live := current.LookUpField("LastLoginAt")
	if f.Tag != live.Tag || f.FieldType != live.FieldType || f.DBName != "last_login_at" || f.Precision != 6 || f.NotNull || f.HasDefaultValue || f.AutoCreateTime != 0 || f.AutoUpdateTime != 0 || f.Tag.Get("json") != "-" || len(frozen.ParseIndexes()) != 0 {
		t.Fatal(f)
	}
}

// Existing partial DDL is inspected without rewriting retained timestamps.
type recentLoginColumnMethods interface{ gorm.ColumnType }

type recentLoginShapeColumn struct {
	recentLoginColumnMethods
	nullable     bool
	precision    int64
	defaultValue string
	kind         string
}

func (recentLoginShapeColumn) Name() string                        { return "last_login_at" }
func (c recentLoginShapeColumn) Nullable() (bool, bool)            { return c.nullable, true }
func (c recentLoginShapeColumn) DecimalSize() (int64, int64, bool) { return c.precision, 0, true }
func (c recentLoginShapeColumn) DefaultValue() (string, bool) {
	return c.defaultValue, c.defaultValue != ""
}
func (c recentLoginShapeColumn) DatabaseTypeName() string { return c.kind }

type recentLoginShapeMigrator struct {
	gorm.Migrator
	present bool
	column  recentLoginShapeColumn
	fail    string
	adds    int
}

func (m *recentLoginShapeMigrator) HasColumn(_ any, _ string) bool { return m.present }
func (m *recentLoginShapeMigrator) AddColumn(_ any, field string) error {
	if field != "LastLoginAt" {
		return errors.New("unexpected column")
	}
	if m.fail == "add" {
		return errInterruptedRateDDL
	}
	m.present = true
	m.adds++
	return nil
}
func (m *recentLoginShapeMigrator) ColumnTypes(_ any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errInterruptedRateDDL
	}
	return []gorm.ColumnType{m.column}, nil
}
func TestMemberRecentLoginMigrationPartialDDLAndInvalidShape(t *testing.T) {
	valid := recentLoginShapeColumn{nullable: true, precision: 6, kind: "timestamp"}
	for _, failure := range []string{"add", "columns"} {
		t.Run(failure, func(t *testing.T) {
			m := &recentLoginShapeMigrator{column: valid, fail: failure}
			db := projectRateCheckDB(t, m)
			if !errors.Is(memberRecentLoginMigration(db), errInterruptedRateDDL) {
				t.Fatal("DDL failure lost")
			}
			m.fail = ""
			if err := memberRecentLoginMigration(db); err != nil {
				t.Fatal(err)
			}
			if err := memberRecentLoginMigration(db); err != nil || m.adds != 1 {
				t.Fatal("repeat rewrote column", err, m.adds)
			}
		})
	}
	for _, kind := range []string{"timestamp", "timestamptz", "datetime"} {
		m := &recentLoginShapeMigrator{present: true, column: valid}
		m.column.kind = kind
		if err := memberRecentLoginMigration(projectRateCheckDB(t, m)); err != nil || m.adds != 0 {
			t.Fatal(kind, err)
		}
	}
	for _, field := range []string{"nullable", "precision", "default", "kind"} {
		t.Run(field, func(t *testing.T) {
			m := &recentLoginShapeMigrator{present: true, column: valid}
			switch field {
			case "nullable":
				m.column.nullable = false
			case "precision":
				m.column.precision = 3
			case "default":
				m.column.defaultValue = "CURRENT_TIMESTAMP"
			case "kind":
				m.column.kind = "text"
			}
			if memberRecentLoginMigration(projectRateCheckDB(t, m)) == nil || m.adds != 0 {
				t.Fatal("invalid history silently rewritten")
			}
		})
	}
}
