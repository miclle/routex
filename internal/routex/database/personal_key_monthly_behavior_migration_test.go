package database

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestPersonalKeyMonthlyBehaviorFrozenExactScopeAndRegistry(t *testing.T) {
	parsed, err := schema.Parse(&personalKeyMonthlyBehaviorV73{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	checks := parsed.ParseCheckConstraints()
	if personalKeyMonthlyBehaviorVersion != 73 || parsed.Table != "resource_limits" || len(parsed.Fields) != 3 || len(checks) != 3 || len(parsed.ParseIndexes()) != 0 || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("unbounded V73 schema")
	}
	predicate := checks["ck_resource_limits_monthly_behavior_scope_v73"].Constraint
	if strings.Count(predicate, "ASCII(SUBSTRING(scope_kind,") != 11 || !strings.Contains(predicate, "= 116") || !strings.Contains(predicate, "= 117") || strings.Contains(predicate, " IN (") {
		t.Fatal("Personal Key/User/Team scope aliases possible", predicate)
	}
	for _, name := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		field := parsed.LookUpField(name)
		if field.Size != 16 || !field.NotNull || field.DefaultValue != "stop" {
			t.Fatal("mode column definition changed", name)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) < 73 || reflect.ValueOf(steps[72]).Pointer() != reflect.ValueOf(personalKeyMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[71]).Pointer() != reflect.ValueOf(vaultIntegrationMigration).Pointer() || reflect.ValueOf(steps[70]).Pointer() != reflect.ValueOf(teamMonthlyBehaviorMigration).Pointer() {
			t.Fatal("V73 must follow the exact V72 and V71 prefix", dialect)
		}
	}
}

type personalKeyBehaviorMigrator struct{ *monthlyBehaviorMigrator }

func (m *personalKeyBehaviorMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New("interrupted scope replacement")
	}
	delete(m.checks, name)
	return nil
}

type personalKeyBehaviorDialector struct {
	gorm.Dialector
	m *personalKeyBehaviorMigrator
}

func (d personalKeyBehaviorDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestPersonalKeyMonthlyBehaviorPartialFenceRepairRepeatAndFailure(t *testing.T) {
	const old = "ck_resource_limits_monthly_behavior_scope_v71"
	const current = "ck_resource_limits_monthly_behavior_scope_v73"
	for _, fail := range []string{"", "columns", "check:" + current, "drop:" + old} {
		t.Run(fail, func(t *testing.T) {
			m := &personalKeyBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true, "ck_resource_limits_tokens_month_behavior": true, "ck_resource_limits_money_month_behavior": true}, fail: fail}}
			db := &gorm.DB{Config: &gorm.Config{Dialector: personalKeyBehaviorDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			err := personalKeyMonthlyBehaviorMigration(db)
			if (err != nil) != (fail != "") {
				t.Fatal("DDL failure hidden", err)
			}
			if fail != "" && !m.checks[old] {
				t.Fatal("old scope fence removed on failure")
			}
			m.fail = ""
			if err = personalKeyMonthlyBehaviorMigration(db); err != nil {
				t.Fatal(err)
			}
			if !m.checks[current] || m.checks[old] {
				t.Fatal("scope replacement incomplete")
			}
			before := append([]string(nil), m.ops...)
			if err = personalKeyMonthlyBehaviorMigration(db); err != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat changed schema", err, m.ops)
			}
		})
	}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		m := &personalKeyBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true}}}
		delete(m.columns, field)
		db := &gorm.DB{Config: &gorm.Config{Dialector: personalKeyBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if personalKeyMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks[old] {
			t.Fatal("missing V70 prerequisite accepted", field)
		}
	}
}

func TestPersonalKeyMonthlyBehaviorInvalidInheritedColumnFailsBeforeDDL(t *testing.T) {
	for _, column := range []roleRevisionColumn{
		{name: "tokens_month_behavior", size: 16, defaultValue: "stop", nullable: true},
		{name: "tokens_month_behavior", size: 10, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 17, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "alert_only"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "'stop' || 'other'"},
		{name: "tokens_month_behavior", size: 16},
	} {
		m := &personalKeyBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{"ck_resource_limits_monthly_behavior_scope": true}, metadata: []gorm.ColumnType{column, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: personalKeyBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if personalKeyMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks["ck_resource_limits_monthly_behavior_scope"] {
			t.Fatal("unsafe inherited column changed schema or was accepted", column, m.ops)
		}
	}
}
