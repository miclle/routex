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

func TestTeamMonthlyBehaviorFrozenExactScopeAndRegistry(t *testing.T) {
	parsed, err := schema.Parse(&teamMonthlyBehaviorV71{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	checks := parsed.ParseCheckConstraints()
	if teamMonthlyBehaviorVersion != 71 || parsed.Table != "resource_limits" || len(parsed.Fields) != 3 || len(checks) != 3 || len(parsed.ParseIndexes()) != 0 || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("unbounded V71 schema")
	}
	predicate := checks["ck_resource_limits_monthly_behavior_scope_v71"].Constraint
	if strings.Count(predicate, "ASCII(SUBSTRING(scope_kind,") != 8 || !strings.Contains(predicate, "= 116") || !strings.Contains(predicate, "= 117") || strings.Contains(predicate, " IN (") {
		t.Fatal("Team/User scope aliases possible", predicate)
	}
	for _, name := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		field := parsed.LookUpField(name)
		if field.Size != 16 || !field.NotNull || field.DefaultValue != "stop" {
			t.Fatal("mode column definition changed", name)
		}
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		steps := migrationSteps(dialect)
		if len(steps) != 71 || reflect.ValueOf(steps[70]).Pointer() != reflect.ValueOf(teamMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[69]).Pointer() != reflect.ValueOf(personalMonthlyBehaviorMigration).Pointer() {
			t.Fatal("historical registry or V71 append changed", dialect)
		}
	}
}

type teamBehaviorMigrator struct{ *monthlyBehaviorMigrator }

func (m *teamBehaviorMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New("interrupted scope replacement")
	}
	delete(m.checks, name)
	return nil
}

type teamBehaviorDialector struct {
	gorm.Dialector
	m *teamBehaviorMigrator
}

func (d teamBehaviorDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestTeamMonthlyBehaviorPartialFenceRepairRepeatAndFailure(t *testing.T) {
	const old = "ck_resource_limits_monthly_behavior_scope"
	const current = "ck_resource_limits_monthly_behavior_scope_v71"
	for _, fail := range []string{"", "columns", "check:" + current, "drop:" + old} {
		t.Run(fail, func(t *testing.T) {
			m := &teamBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true, "ck_resource_limits_tokens_month_behavior": true, "ck_resource_limits_money_month_behavior": true}, fail: fail}}
			db := &gorm.DB{Config: &gorm.Config{Dialector: teamBehaviorDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			err := teamMonthlyBehaviorMigration(db)
			if (err != nil) != (fail != "") {
				t.Fatal("DDL failure hidden", err)
			}
			if fail != "" && !m.checks[old] {
				t.Fatal("old scope fence removed on failure")
			}
			m.fail = ""
			if err = teamMonthlyBehaviorMigration(db); err != nil {
				t.Fatal(err)
			}
			if !m.checks[current] || m.checks[old] {
				t.Fatal("scope replacement incomplete")
			}
			before := append([]string(nil), m.ops...)
			if err = teamMonthlyBehaviorMigration(db); err != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat changed schema", err, m.ops)
			}
		})
	}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		m := &teamBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true}}}
		delete(m.columns, field)
		db := &gorm.DB{Config: &gorm.Config{Dialector: teamBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if teamMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks[old] {
			t.Fatal("missing V70 prerequisite accepted", field)
		}
	}
}

func TestTeamMonthlyBehaviorInvalidInheritedColumnFailsBeforeDDL(t *testing.T) {
	for _, column := range []roleRevisionColumn{
		{name: "tokens_month_behavior", size: 16, defaultValue: "stop", nullable: true},
		{name: "tokens_month_behavior", size: 10, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 17, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "alert_only"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "'stop' || 'other'"},
		{name: "tokens_month_behavior", size: 16},
	} {
		m := &teamBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{"ck_resource_limits_monthly_behavior_scope": true}, metadata: []gorm.ColumnType{column, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: teamBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if teamMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks["ck_resource_limits_monthly_behavior_scope"] {
			t.Fatal("unsafe inherited column changed schema or was accepted", column, m.ops)
		}
	}
}
