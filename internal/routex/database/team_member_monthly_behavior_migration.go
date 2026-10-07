package database

import (
	"fmt"

	"gorm.io/gorm"
)

const teamMemberMonthlyBehaviorVersion = 75

// Frozen V75 keeps the V70 columns/value checks and admits soft monthly modes
// on exact User, Team aggregate, Project aggregate, shared Key and Team-member scopes.
// Key roots and Team-member pairs still require exact source identity proof in
// service/runtime readers; this CHECK cannot join those tables. ASCII/length
// checks prevent case/trailing-space aliases under both supported collations.
type teamMemberMonthlyBehaviorV75 struct {
	ScopeKind           string `gorm:"size:20;check:ck_resource_limits_monthly_behavior_scope_v75,(OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR (OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109) OR (OCTET_LENGTH(scope_kind) = 3 AND ASCII(SUBSTRING(scope_kind,1,1)) = 107 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 121) OR (OCTET_LENGTH(scope_kind) = 7 AND ASCII(SUBSTRING(scope_kind,1,1)) = 112 AND ASCII(SUBSTRING(scope_kind,2,1)) = 114 AND ASCII(SUBSTRING(scope_kind,3,1)) = 111 AND ASCII(SUBSTRING(scope_kind,4,1)) = 106 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 99 AND ASCII(SUBSTRING(scope_kind,7,1)) = 116) OR (OCTET_LENGTH(scope_kind) = 11 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109 AND ASCII(SUBSTRING(scope_kind,5,1)) = 95 AND ASCII(SUBSTRING(scope_kind,6,1)) = 109 AND ASCII(SUBSTRING(scope_kind,7,1)) = 101 AND ASCII(SUBSTRING(scope_kind,8,1)) = 109 AND ASCII(SUBSTRING(scope_kind,9,1)) = 98 AND ASCII(SUBSTRING(scope_kind,10,1)) = 101 AND ASCII(SUBSTRING(scope_kind,11,1)) = 114) OR ((OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) AND (OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112))"`
	TokensMonthBehavior string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_tokens_month_behavior,(OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(tokens_month_behavior) = 10 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(tokens_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(tokens_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(tokens_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,10,1)) = 121)"`
	MoneyMonthBehavior  string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_money_month_behavior,(OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(money_month_behavior) = 10 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(money_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(money_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(money_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,10,1)) = 121)"`
}

func (teamMemberMonthlyBehaviorV75) TableName() string { return "resource_limits" }

func teamMemberMonthlyBehaviorMigration(db *gorm.DB) error {
	frozen := &teamMemberMonthlyBehaviorV75{}
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		if !db.Migrator().HasColumn(frozen, field) {
			return fmt.Errorf("missing Team-member monthly behavior column %s", field)
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
				return fmt.Errorf("invalid Team-member monthly behavior column %s", name)
			}
		}
		if !found {
			return fmt.Errorf("missing Team-member monthly behavior column %s", name)
		}
	}
	for _, check := range []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior", "ck_resource_limits_monthly_behavior_scope_v75"} {
		if !db.Migrator().HasConstraint(frozen, check) {
			if err := db.Migrator().CreateConstraint(frozen, check); err != nil {
				return err
			}
		}
	}
	// Install the broader exact check before dropping the V74 check. A MySQL
	// interruption leaves at least one scope fence in place; replay completes it.
	if db.Migrator().HasConstraint(frozen, "ck_resource_limits_monthly_behavior_scope_v74") {
		if err := db.Migrator().DropConstraint(frozen, "ck_resource_limits_monthly_behavior_scope_v74"); err != nil {
			return err
		}
	}
	return nil
}
