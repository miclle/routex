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

func TestTeamMemberMonthlyBehaviorFrozenExactScopeAndRegistry(t *testing.T) {
	parsed, err := schema.Parse(&teamMemberMonthlyBehaviorV75{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	checks := parsed.ParseCheckConstraints()
	if teamMemberMonthlyBehaviorVersion != 75 || parsed.Table != "resource_limits" || len(parsed.Fields) != 3 || len(checks) != 3 || len(parsed.ParseIndexes()) != 0 || len(parsed.Relationships.Relations) != 0 {
		t.Fatal("unbounded V75 schema")
	}
	predicate := checks["ck_resource_limits_monthly_behavior_scope_v75"].Constraint
	if strings.Count(predicate, "ASCII(SUBSTRING(scope_kind,") != 29 || !strings.Contains(predicate, "= 116") || !strings.Contains(predicate, "= 117") || strings.Contains(predicate, " IN (") {
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
		if len(steps) != 84 || reflect.ValueOf(steps[83]).Pointer() != reflect.ValueOf(teamRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[82]).Pointer() != reflect.ValueOf(projectRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[81]).Pointer() != reflect.ValueOf(personalKeyRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[80]).Pointer() != reflect.ValueOf(personalRollingQuotaWarningMigration).Pointer() || reflect.ValueOf(steps[76]).Pointer() != reflect.ValueOf(credentialStorageMigration).Pointer() || reflect.ValueOf(steps[75]).Pointer() != reflect.ValueOf(migrateConnectionEnablementV76).Pointer() || reflect.ValueOf(steps[74]).Pointer() != reflect.ValueOf(teamMemberMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[73]).Pointer() != reflect.ValueOf(projectMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[72]).Pointer() != reflect.ValueOf(personalKeyMonthlyBehaviorMigration).Pointer() || reflect.ValueOf(steps[71]).Pointer() != reflect.ValueOf(vaultIntegrationMigration).Pointer() || reflect.ValueOf(steps[70]).Pointer() != reflect.ValueOf(teamMonthlyBehaviorMigration).Pointer() {
			t.Fatal("ordered V71/V72/V73/V75 registration changed", dialect)
		}
	}
}

type teamMemberBehaviorMigrator struct{ *monthlyBehaviorMigrator }

func (m *teamMemberBehaviorMigrator) DropConstraint(_ any, name string) error {
	m.ops = append(m.ops, "drop:"+name)
	if m.fail == "drop:"+name {
		return errors.New("interrupted scope replacement")
	}
	delete(m.checks, name)
	return nil
}

type teamMemberBehaviorDialector struct {
	gorm.Dialector
	m *teamMemberBehaviorMigrator
}

func (d teamMemberBehaviorDialector) Migrator(*gorm.DB) gorm.Migrator { return d.m }

func TestTeamMemberMonthlyBehaviorPartialFenceRepairRepeatAndFailure(t *testing.T) {
	const old = "ck_resource_limits_monthly_behavior_scope_v74"
	const current = "ck_resource_limits_monthly_behavior_scope_v75"
	for _, fail := range []string{"", "columns", "check:" + current, "drop:" + old} {
		t.Run(fail, func(t *testing.T) {
			m := &teamMemberBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true, "ck_resource_limits_tokens_month_behavior": true, "ck_resource_limits_money_month_behavior": true}, fail: fail}}
			db := &gorm.DB{Config: &gorm.Config{Dialector: teamMemberBehaviorDialector{m: m}}}
			db.Statement = &gorm.Statement{DB: db}
			err := teamMemberMonthlyBehaviorMigration(db)
			if (err != nil) != (fail != "") {
				t.Fatal("DDL failure hidden", err)
			}
			if fail != "" && !m.checks[old] {
				t.Fatal("old scope fence removed on failure")
			}
			m.fail = ""
			if err = teamMemberMonthlyBehaviorMigration(db); err != nil {
				t.Fatal(err)
			}
			if !m.checks[current] || m.checks[old] {
				t.Fatal("scope replacement incomplete")
			}
			before := append([]string(nil), m.ops...)
			if err = teamMemberMonthlyBehaviorMigration(db); err != nil || !reflect.DeepEqual(before, m.ops) {
				t.Fatal("repeat changed schema", err, m.ops)
			}
		})
	}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		m := &teamMemberBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{old: true}}}
		delete(m.columns, field)
		db := &gorm.DB{Config: &gorm.Config{Dialector: teamMemberBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if teamMemberMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks[old] {
			t.Fatal("missing V70 prerequisite accepted", field)
		}
	}
}

func TestTeamMemberMonthlyBehaviorInvalidInheritedColumnFailsBeforeDDL(t *testing.T) {
	for _, column := range []roleRevisionColumn{
		{name: "tokens_month_behavior", size: 16, defaultValue: "stop", nullable: true},
		{name: "tokens_month_behavior", size: 10, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 17, defaultValue: "stop"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "alert_only"},
		{name: "tokens_month_behavior", size: 16, defaultValue: "'stop' || 'other'"},
		{name: "tokens_month_behavior", size: 16},
	} {
		m := &teamMemberBehaviorMigrator{&monthlyBehaviorMigrator{columns: map[string]bool{"TokensMonthBehavior": true, "MoneyMonthBehavior": true}, checks: map[string]bool{"ck_resource_limits_monthly_behavior_scope": true}, metadata: []gorm.ColumnType{column, roleRevisionColumn{name: "money_month_behavior", size: 16, defaultValue: "stop"}}}}
		db := &gorm.DB{Config: &gorm.Config{Dialector: teamMemberBehaviorDialector{m: m}}}
		db.Statement = &gorm.Statement{DB: db}
		if teamMemberMonthlyBehaviorMigration(db) == nil || len(m.ops) != 0 || !m.checks["ck_resource_limits_monthly_behavior_scope"] {
			t.Fatal("unsafe inherited column changed schema or was accepted", column, m.ops)
		}
	}
}
