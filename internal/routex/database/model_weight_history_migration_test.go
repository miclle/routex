package database

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestModelWeightHistoryV90FrozenSchemasAndPortableBudget(t *testing.T) {
	for _, pair := range [][2]any{{&modelWeightVersionV90{}, &entity.ModelWeightVersion{}}, {&modelWeightRollbackCommandV90{}, &entity.ModelWeightRollbackCommand{}}} {
		f, e := schema.Parse(pair[0], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		c, e := schema.Parse(pair[1], &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		if len(f.Fields) != len(c.Fields) {
			t.Fatal("frozen/current field census changed")
		}
		for name, field := range f.FieldsByName {
			current := c.FieldsByName[name]
			if current == nil || field.FieldType != current.FieldType || field.Tag.Get("gorm") != current.Tag.Get("gorm") || field.AutoCreateTime != 0 || field.AutoUpdateTime != 0 {
				t.Fatal("frozen shape or invented time", name)
			}
		}
		if f.Table == "model_weight_versions" {
			field := f.FieldsByName["Snapshot"]
			if field.Size != 524288 || mysql.New(mysql.Config{SkipInitializeWithVersion: true}).DataTypeOf(field) != "mediumblob" || postgres.New(postgres.Config{}).DataTypeOf(field) != "bytea" {
				t.Fatal("512KiB snapshot must not use 64KiB MySQL TEXT")
			}
			checks := f.ParseCheckConstraints()
			if len(checks) != 5 || checks["ck_model_weight_snapshot"].Constraint != "OCTET_LENGTH(snapshot) <= 524288" {
				t.Fatal("bounded snapshot constraint absent")
			}
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 93 || reflect.ValueOf(steps[92]).Pointer() != reflect.ValueOf(credentialAttemptStatisticsMigration).Pointer() || reflect.ValueOf(steps[91]).Pointer() != reflect.ValueOf(connectionTransportMigration).Pointer() || reflect.ValueOf(steps[90]).Pointer() != reflect.ValueOf(providerEnablementMigration).Pointer() || reflect.ValueOf(steps[89]).Pointer() != reflect.ValueOf(modelWeightHistoryMigration).Pointer() || reflect.ValueOf(steps[88]).Pointer() != reflect.ValueOf(credentialSourceClosedMigration).Pointer() || reflect.ValueOf(steps[87]).Pointer() != reflect.ValueOf(credentialSourceDrainMigration).Pointer() {
			t.Fatal("exact append-only 1..89 prefix changed")
		}
	}
}

type weightHistoryMigrator struct {
	gorm.Migrator
	tables  map[string]bool
	indexes map[string]bool
	checks  map[string]bool
	creates map[string]int
	fail    string
}

func weightHistoryTable(v any) string {
	switch v.(type) {
	case *modelWeightVersionV90:
		return "model_weight_versions"
	case *modelWeightRollbackCommandV90:
		return "model_weight_rollback_commands"
	default:
		panic("off-scope schema")
	}
}
func (m *weightHistoryMigrator) HasTable(v any) bool { return m.tables[weightHistoryTable(v)] }
func (m *weightHistoryMigrator) CreateTable(v ...any) error {
	if len(v) != 1 {
		panic("unbounded DDL")
	}
	name := weightHistoryTable(v[0])
	if m.fail == name {
		return errInterruptedRateDDL
	}
	m.tables[name] = true
	m.creates[name]++
	return nil
}
func (m *weightHistoryMigrator) HasIndex(v any, name string) bool {
	return m.indexes[weightHistoryTable(v)+name]
}
func (m *weightHistoryMigrator) CreateIndex(v any, name string) error {
	if m.fail == name {
		return errInterruptedRateDDL
	}
	m.indexes[weightHistoryTable(v)+name] = true
	return nil
}
func (m *weightHistoryMigrator) HasConstraint(v any, name string) bool {
	return m.checks[weightHistoryTable(v)+name]
}
func (m *weightHistoryMigrator) CreateConstraint(v any, name string) error {
	if m.fail == name {
		return errInterruptedRateDDL
	}
	m.checks[weightHistoryTable(v)+name] = true
	return nil
}
func TestModelWeightHistoryV90InterruptedDDLReentry(t *testing.T) {
	for _, failure := range []string{"model_weight_rollback_commands", "idx_model_weight_sequence", "ck_model_weight_snapshot"} {
		t.Run(failure, func(t *testing.T) {
			m := &weightHistoryMigrator{tables: map[string]bool{}, indexes: map[string]bool{}, checks: map[string]bool{}, creates: map[string]int{}, fail: failure}
			db := projectRateCheckDB(t, m)
			if !errors.Is(modelWeightHistoryMigration(db), errInterruptedRateDDL) {
				t.Fatal("lost partial DDL failure")
			}
			m.fail = ""
			for range 2 {
				if err := modelWeightHistoryMigration(db); err != nil {
					t.Fatal(err)
				}
			}
			if m.creates["model_weight_versions"] != 1 || m.creates["model_weight_rollback_commands"] != 1 {
				t.Fatal("reentry rewrote retained table", m.creates)
			}
		})
	}
}
