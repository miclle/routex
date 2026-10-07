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

func TestProjectMonthlyBehaviorFrozenExactScopeAndRegistry(t *testing.T) {
	parsed, err := schema.Parse(&projectMonthlyBehaviorV74{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	checks := parsed.ParseCheckConstraints()
	if projectMonthlyBehaviorVersion != 74 || parsed.Table != "resource_limits" || len(parsed.Fields) != 3 || len(checks) != 3 || len(parsed.ParseIndexes()) != 0 || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("unbounded V74 schema")
	}
	predicate := checks["ck_resource_limits_monthly_behavior_scope_v74"].Constraint
	if strings.Count(predicate, "ASCII(SUBSTRING(scope_kind,") != 18 || !strings.Contains(predicate, "= 116") || !strings.Contains(predicate, "= 117") || strings.Contains(predicate, " IN (") {
		t.Fatal("Personal Key/User/Team/Project scope aliases possible", predicate)
	}
	for _, name := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		field := parsed.LookUpField(name)
		if field.Size != 16 || !field.NotNull || field.DefaultValue != "stop" {
			t.Fatal("mode column definition changed", name)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 76 || reflect.ValueOf(steps[75]).Pointer() != reflect.ValueOf(migrateConnectionEnablementV76).Pointer() || reflect.ValueOf(steps[74]).Pointer() != reflect.ValueOf(teamMemberMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[73]).Pointer() != reflect.ValueOf(projectMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[72]).Pointer() != reflect.ValueOf(personalKeyMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[71]).Pointer() != reflect.ValueOf(vaultIntegrationMigration).Pointer() || reflect.ValueOf(steps[70]).Pointer() != reflect.ValueOf(teamMonthlyBehaviorMigration).Pointer() {
			t.Fatal("ordered V71/V72/V73/V74 registration changed", dialect)
		}
	}
}

type projectBehaviorMigrator struct{ *monthlyBehaviorMigrator }

func (m *projectBehaviorMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New("interrupted scope replacement")
	}
	delete(m.checks, name)
	return nil
}

type projectBehaviorDialector struct {
	gorm.Dialector
	m *projectBehaviorMigrator
}

func (d projectBehaviorDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestProjectMonthlyBehaviorPartialFenceRepairRepeatAndFailure(t *testing.T) {
	const old = "ck_resource_limits_monthly_behavior_scope_v73"
	const current = "ck_resource_limits_monthly_behavior_scope_v74"
	for _, fail := range []string{"", "columns", "check:" + current, "drop:" + old} {
		t.Run(fail, func(t *testing.T) {
			m := &projectBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true, "ck_resource_limits_tokens_month_behavior": true, "ck_resource_limits_money_month_behavior": true}, fail: fail}}
			db := &gorm.DB{Config: &gorm.Config{Dialector: projectBehaviorDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			err := projectMonthlyBehaviorMigration(db)
			if (err != nil) != (fail != "") {
				t.Fatal("DDL failure hidden", err)
			}
			if fail != "" && !m.checks[old] {
				t.Fatal("old scope fence removed on failure")
			}
			m.fail = ""
			if err = projectMonthlyBehaviorMigration(db); err != nil {
				t.Fatal(err)
			}
			if !m.checks[current] || m.checks[old] {
				t.Fatal("scope replacement incomplete")
			}
			before := append([]string(nil), m.ops...)
			if err = projectMonthlyBehaviorMigration(db); err != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat changed schema", err, m.ops)
			}
		})
	}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		m := &projectBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true}}}
		delete(m.columns, field)
		db := &gorm.DB{Config: &gorm.Config{Dialector: projectBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if projectMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks[old] {
			t.Fatal("missing V70 prerequisite accepted", field)
		}
	}
}

func TestProjectMonthlyBehaviorInvalidInheritedColumnFailsBeforeDDL(t *testing.T) {
	for _, column := range []roleRevisionColumn{
		{name: "tokens_month_behavior", size: 16, defaultValue: "stop", nullable: true},
		{name: "tokens_month_behavior", size: 10, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 17, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "alert_only"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "'stop' || 'other'"},
		{name: "tokens_month_behavior", size: 16},
	} {
		m := &projectBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{"ck_resource_limits_monthly_behavior_scope": true}, metadata: []gorm.ColumnType{column, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: projectBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if projectMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks["ck_resource_limits_monthly_behavior_scope"] {
			t.Fatal("unsafe inherited column changed schema or was accepted", column, m.ops)
		}
	}
}
