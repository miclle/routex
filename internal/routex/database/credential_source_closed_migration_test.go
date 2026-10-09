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

func TestCredentialSourceClosedV89FrozenNullableNoBackfill(t *testing.T) {
	f, err := schema.Parse(&credentialSourceClosedV89{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	c, err := schema.Parse(&entity.CredentialSourceProcess{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	v := f.FieldsByName["ClosedAt"]
	current := c.FieldsByName["ClosedAt"]
	if len(f.Fields) != 2 || f.Table != "credential_source_processes" || v.DBName != current.DBName || v.Tag.Get("gorm") != current.Tag.Get("gorm") || v.FieldType != current.FieldType || v.NotNull || v.AutoCreateTime != 0 || v.AutoUpdateTime != 0 || v.HasDefaultValue {
		t.Fatal("closure proof must be nullable with no invented historical default")
	}
	old, err := schema.Parse(&credentialSourceProcessV88{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if old.FieldsByName["ClosedAt"] != nil || len(old.Fields) != 4 {
		t.Fatal("released V88 was rewritten")
	}
	for _, driver := range []string{"postgres", "mysql"} {
		steps := migrationSteps(driver)
		if len(steps) != 92 || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() {
			t.Fatal("V89 exact append-only suffix")
		}
	}
}

// Interrupted additive DDL resumes; malformed retained proof columns are rejected.
type credentialClosedColumn struct{ recentLoginShapeColumn }

func (credentialClosedColumn) Name() string { return "closed_at" }

type credentialClosedMigrator struct {
	gorm.Migrator
	present bool
	fail    string
	adds    int
	column  credentialClosedColumn
}

func (m *credentialClosedMigrator) HasColumn(model any, field string) bool {
	if _, ok := model.(*credentialSourceClosedV89); !ok || field != "ClosedAt" {
		panic("unexpected closure schema")
	}
	return m.present
}
func (m *credentialClosedMigrator) AddColumn(model any, field string) error {
	if _, ok := model.(*credentialSourceClosedV89); !ok || field != "ClosedAt" {
		panic("unexpected closure column")
	}
	if m.fail == "add" {
		return errInterruptedRateDDL
	}
	m.adds++
	m.present = true
	return nil
}
func (m *credentialClosedMigrator) ColumnTypes(model any) ([]gorm.ColumnType, error) {
	if _, ok := model.(*credentialSourceClosedV89); !ok || !m.present {
		panic("unexpected closure inspection")
	}
	if m.fail == "columns" {
		return nil, errInterruptedRateDDL
	}
	if m.fail == "missing" {
		return nil, nil
	}
	return []gorm.ColumnType{m.column}, nil
}
func TestCredentialSourceClosedV89PartialDDLAndRepeat(t *testing.T) {
	valid := credentialClosedColumn{recentLoginShapeColumn{nullable: true, precision: 6, kind: "timestamp"}}
	for _, failure := range []string{"add", "columns"} {
		t.Run(failure, func(t *testing.T) {
			m := &credentialClosedMigrator{fail: failure, column: valid}
			db := projectRateCheckDB(t, m)
			if !errors.Is(credentialSourceClosedMigration(db), errInterruptedRateDDL) {
				t.Fatal("lost DDL failure")
			}
			m.fail = ""
			for range 2 {
				if err := credentialSourceClosedMigration(db); err != nil {
					t.Fatal(err)
				}
			}
			if !m.present || m.adds != 1 {
				t.Fatal("partial DDL reentry rewrote retained history", m)
			}
		})
	}
	for _, fault := range []string{"missing", "not_nullable", "precision", "default", "kind"} {
		t.Run(fault, func(t *testing.T) {
			m := &credentialClosedMigrator{present: true, column: valid}
			switch fault {
			case "missing":
				m.fail = "missing"
			case "not_nullable":
				m.column.nullable = false
			case "precision":
				m.column.precision = 3
			case "default":
				m.column.defaultValue = "CURRENT_TIMESTAMP"
			case "kind":
				m.column.kind = "text"
			}
			if credentialSourceClosedMigration(projectRateCheckDB(t, m)) == nil || m.adds != 0 {
				t.Fatal("malformed historical proof silently rewritten")
			}
		})
	}
}
