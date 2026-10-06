package database

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestPersonalMonthlyBehaviorFrozenExactSchema(t *testing.T) {
	parsed, err := schema.Parse(&personalMonthlyBehaviorV70{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if personalMonthlyBehaviorVersion != 70 || parsed.Table != "resource_limits" || len(parsed.Fields) != 3 || len(parsed.Relationships.Relations) != 0 || len(parsed.ParseIndexes()) != 0 {
		t.Fatal("unbounded schema")
	}
	for _, name := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		f := parsed.LookUpField(name)
		if f.Size != 16 || !f.NotNull || f.DefaultValue != "stop" || f.AutoCreateTime != 0 || f.AutoUpdateTime != 0 {
			t.Fatal("legacy stop default lost", name)
		}
	}
	checks := parsed.ParseCheckConstraints()
	if len(checks) != 3 {
		t.Fatal("missing constraints")
	}
	for _, name := range []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior"} {
		s := checks[name].Constraint
		if !strings.Contains(s, "OCTET_LENGTH(") || strings.Count(s, "ASCII(SUBSTRING(") != 14 || strings.Contains(s, " IN (") {
			t.Fatal("collation alias possible", s)
		}
	}
	if strings.Count(checks["ck_resource_limits_monthly_behavior_scope"].Constraint, "ASCII(SUBSTRING(") != 12 {
		t.Fatal("scope not exact")
	}
}

type monthlyBehaviorMigrator struct {
	gorm.Migrator
	columns, checks map[string]bool
	ops             []string
	fail            string
	metadata        []gorm.ColumnType
}

func (m *monthlyBehaviorMigrator) HasColumn(_ any, name string) bool { return m.columns[name] }
func (m *monthlyBehaviorMigrator) AddColumn(_ any, name string) error {
	m.ops = append(m.ops, "column:"+name)
	if m.fail == "column:"+name {
		return errors.New("interrupted DDL")
	}
	m.columns[name] = true
	return nil
}
func (m *monthlyBehaviorMigrator) HasConstraint(_ any, name string) bool { return m.checks[name] }
func (m *monthlyBehaviorMigrator) CreateConstraint(_ any, name string) error {
	m.ops = append(m.ops, "check:"+name)
	if m.fail == "check:"+name {
		return errors.New("interrupted DDL")
	}
	m.checks[name] = true
	return nil
}

func (m *monthlyBehaviorMigrator) ColumnTypes(any) ([]gorm.ColumnType, error) {
	if m.fail == "columns" {
		return nil, errors.New("column metadata unavailable")
	}
	if m.metadata != nil {
		return m.metadata, nil
	}
	return []gorm.ColumnType{roleRevisionColumn{name: "tokens_month_behavior", size: 16, defaultValue: "stop"}, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}, nil
}

type monthlyBehaviorDialector struct {
	gorm.Dialector
	m *monthlyBehaviorMigrator
}

func (d monthlyBehaviorDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }
func TestPersonalMonthlyBehaviorPartialDDLRepeatAndFailure(t *testing.T) {
	expected := []string{"column:TokensMonthBehavior", "column:MoneyMonthBehavior", "check:ck_resource_limits_tokens_month_behavior", "check:ck_resource_limits_money_month_behavior", "check:ck_resource_limits_monthly_behavior_scope"}
	for _, fail := range append([]string{"", "columns"}, expected...) {
		t.Run(fail, func(t *testing.T) {
			m := &monthlyBehaviorMigrator{columns: map[string]bool{}, checks: map[string]bool{}, fail: fail}
			db := &gorm.DB{Config: &gorm.Config{Dialector: monthlyBehaviorDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			err := personalMonthlyBehaviorMigration(db)
			if (err != nil) != (fail != "") {
				t.Fatal("DDL failure swallowed", err)
			}
			m.fail = ""
			if err = personalMonthlyBehaviorMigration(db); err != nil {
				t.Fatal(err)
			}
			if len(m.columns) != 2 || len(m.checks) != 3 {
				t.Fatal("partial step not repaired")
			}
			before := append([]string(nil), m.ops...)
			if err = personalMonthlyBehaviorMigration(db); err != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat mutated existing schema", err)
			}
		})
	}
}

func TestPersonalMonthlyBehaviorInvalidPartialColumnFailsClosed(t *testing.T) {
	for _, column := range []roleRevisionColumn{
		{name: "tokens_month_behavior", size: 16, defaultValue: "stop", nullable: true},
		{name: "tokens_month_behavior", size: 9, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 10, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 15, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 17, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "alert_only"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "'stop' || 'other'"},
		{name: "tokens_month_behavior", size: 16},
	} {
		m := &monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{}, metadata: []gorm.ColumnType{column, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: monthlyBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if personalMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 {
			t.Fatal("unsafe partial schema altered or accepted", column, m.ops)
		}
	}
}

// Valid values must be shorter than storage width. MySQL discards excess trailing
// spaces before CHECK evaluation; a width equal to alert_only accepts its alias.
func TestPersonalMonthlyBehaviorStorageRetainsInvalidSuffix(t *testing.T) {
	for _, model := range []any{&personalMonthlyBehaviorV70{}, &entity.ResourceLimit{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
			field := parsed.LookUpField(name)
			if field.Size != 16 || field.Size <= len("alert_only") || !field.NotNull || field.DefaultValue != "stop" {
				t.Fatal("storage can truncate an invalid suffix into a valid mode", name)
			}
		}
	}
}
