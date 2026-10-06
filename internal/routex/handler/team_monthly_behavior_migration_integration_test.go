package handler

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type teamBehaviorLegacyScopeV70 struct {
	ScopeKind string `gorm:"size:20;check:ck_resource_limits_monthly_behavior_scope,(OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR ((OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) AND (OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112))"`
}

func (teamBehaviorLegacyScopeV70) TableName() string { return "resource_limits" }

func teamBehaviorSameLimit(a, b entity.ResourceLimit) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return false
	}
	a.UpdatedAt = b.UpdatedAt
	return reflect.DeepEqual(a, b)
}

// Proposed case 134. The existing harness proves empty startup before this
// upgrade/repeat/concurrent/partial-DDL fixture; no real driver is run here.
func testTeamMonthlyBehaviorMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var ledger []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &ledger).Error; err != nil {
		t.Fatal(err)
	}
	if len(ledger) != 71 {
		t.Fatal("expected current V71 ledger", ledger)
	}
	for i, v := range ledger {
		if v != i+1 {
			t.Fatal("incomplete ledger", ledger)
		}
	}
	assertLedger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("ledger changed", err, got)
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 71).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove V71", q.Error)
		}
		assertLedger(ledger[:70])
	}
	current := func() {
		t.Helper()
		if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope") {
			t.Fatal("scope replacement incomplete")
		}
		for _, name := range []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior"} {
			if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, name) {
				t.Fatal("V70 value check lost", name)
			}
		}
		assertLedger(ledger)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		current()
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Go(func() { errs <- database.Migrate(ctx, db) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		current()
	}
	current()
	zero := int64(0)
	amount := "0.000000000000000001"
	for _, kind := range []string{"user", "team", "team_member", "project", "key", "project_key"} {
		row := entity.ResourceLimit{ScopeKind: kind, ScopeID: "retained71_" + kind, ETag: "revision71", PreviousETag: "previous71", ActorID: "usr_history71", Reason: "Retained exact policy", TokensMonth: &zero, MoneyMonth: &amount, Currency: "USD", IPMode: "none", IPRangesJSON: "[]", TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"}
		if kind == "user" {
			row.TokensMonthBehavior = "alert_only"
		}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	read := func(kind string) entity.ResourceLimit {
		t.Helper()
		var row entity.ResourceLimit
		if err := db.Take(&row, "scope_kind = ? AND scope_id = ?", kind, "retained71_"+kind).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	snapshots := map[string]entity.ResourceLimit{}
	for _, kind := range []string{"user", "team", "team_member", "project", "key", "project_key"} {
		snapshots[kind] = read(kind)
	}
	retained := func() {
		t.Helper()
		for kind, before := range snapshots {
			if !teamBehaviorSameLimit(before, read(kind)) {
				t.Fatal("retained row changed", kind)
			}
		}
	}
	// Existing V70 database with User soft policy and Team hard policy.
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71"); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&teamBehaviorLegacyScopeV70{}, "ck_resource_limits_monthly_behavior_scope"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	retained()
	// Partial MySQL DDL after the new check was installed but before old removal.
	if err := db.Migrator().CreateConstraint(&teamBehaviorLegacyScopeV70{}, "ck_resource_limits_monthly_behavior_scope"); err != nil {
		t.Fatal(err)
	}
	remove()
	migrate()
	retained()
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		for _, value := range []any{nil, "", "STOP", "ALERT_ONLY", "alert_only ", "stop ", "alert_only       "} {
			before := read("team")
			err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", "retained71_team").UpdateColumn(field, value).Error
			if err == nil || !teamBehaviorSameLimit(before, read("team")) {
				t.Fatal("invalid Team mode changed row", field, value, err)
			}
		}
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", "retained71_team").UpdateColumn(field, "alert_only").Error; err != nil {
			t.Fatal("exact aggregate soft rejected", err)
		}
		for _, kind := range []string{"team_member", "project", "key", "project_key"} {
			before := read(kind)
			err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", kind, "retained71_"+kind).UpdateColumn(field, "alert_only").Error
			if err == nil || !teamBehaviorSameLimit(before, read(kind)) {
				t.Fatal("foreign soft accepted", kind, field, err)
			}
		}
	}
	snapshots["team"] = read("team")
	for _, alias := range []string{"Team", "TEAM", "team "} {
		before := read("team")
		err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team", "retained71_team").UpdateColumn("ScopeKind", alias).Error
		if err == nil || !teamBehaviorSameLimit(before, read("team")) {
			t.Fatal("Team scope alias accepted", alias, err)
		}
	}
	remove()
	concurrent()
	migrate()
	retained() // Repeat retains independently saved modes.
	// An invalid retained member soft row must fail scope installation, not reset.
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71"); err != nil {
		t.Fatal(err)
	}
	remove()
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "retained71_team_member").UpdateColumn("TokensMonthBehavior", "alert_only").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("invalid retained member soft succeeded")
	}
	assertLedger(ledger[:70])
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "retained71_team_member").UpdateColumn("TokensMonthBehavior", "stop").Error; err != nil {
		t.Fatal(err)
	}
	concurrent()
	migrate()
	retained()
}
