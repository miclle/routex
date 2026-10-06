package database

import (
	"fmt"

	"gorm.io/gorm"
)

const personalMonthlyBehaviorVersion = 70

// Frozen V70 adds only independent Personal monthly behavior. GORM check tags
// cannot bind constants in DDL. Exact ASCII/length predicates follow frozen V56
// and avoid case/trailing-space aliases under either supported collation.
// Storage is wider than both valid values: MySQL truncates excess trailing spaces
// before CHECK evaluation, so VARCHAR(10) normalizes alert_only-space into a valid
// value. VARCHAR(16) retains an invalid suffix for the unchanged exact checks.
type personalMonthlyBehaviorV70 struct {
	ScopeKind           string `gorm:"size:20;check:ck_resource_limits_monthly_behavior_scope,(OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR ((OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) AND (OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112))"`
	TokensMonthBehavior string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_tokens_month_behavior,(OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(tokens_month_behavior) = 10 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(tokens_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(tokens_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(tokens_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,10,1)) = 121)"`
	MoneyMonthBehavior  string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_money_month_behavior,(OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(money_month_behavior) = 10 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(money_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(money_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(money_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,10,1)) = 121)"`
}

func (personalMonthlyBehaviorV70) TableName() string { return "resource_limits" }

func personalMonthlyBehaviorMigration(db *gorm.DB) error {
	frozen := &personalMonthlyBehaviorV70{}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		if !db.Migrator().HasColumn(frozen, field) {
			if err := db.Migrator().AddColumn(frozen, field); err != nil {
				return err
			}
		}
	}
	// Existing columns can come from interrupted DDL, not only this version.
	// A nullable or differently sized/defaulted column is not a valid stop baseline.
	columns, err := db.Migrator().ColumnTypes(frozen)
	if err != nil {
		return err
	}
	for _, name := range []string{"tokens_month_behavior", "money_month_behavior"} {
		found := false
		for _, column := range columns {
			if column.Name() != name {
				continue
			}
			found = true
			length, sized := column.Length()
			nullable, known := column.Nullable()
			value, defaulted := column.DefaultValue()
			if !sized || length != 16 || !known || nullable || !defaulted || (value != "stop" && value != "'stop'::character varying" && value != "'stop'::text" && value != "'stop'") {
				return fmt.Errorf("invalid Personal monthly behavior column %s", name)
			}
		}
		if !found {
			return fmt.Errorf("missing Personal monthly behavior column %s", name)
		}
	}
	for _, check := range []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior", "ck_resource_limits_monthly_behavior_scope"} {
		if !db.Migrator().HasConstraint(frozen, check) {
			if err := db.Migrator().CreateConstraint(frozen, check); err != nil {
				return err
			}
		}
	}
	return nil
}
