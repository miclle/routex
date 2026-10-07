package handler

import (
	"context"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"os"
	"reflect"
	"regexp"
	"sync"
	"testing"
)

// Literal V74 private schema for real retained-data upgrade tests only.
type teamMemberBehaviorFrozenV74 struct {
	ScopeKind           string `gorm:"size:20;check:ck_resource_limits_monthly_behavior_scope_v74,(OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 117 AND ASCII(SUBSTRING(scope_kind,2,1)) = 115 AND ASCII(SUBSTRING(scope_kind,3,1)) = 101 AND ASCII(SUBSTRING(scope_kind,4,1)) = 114) OR (OCTET_LENGTH(scope_kind) = 4 AND ASCII(SUBSTRING(scope_kind,1,1)) = 116 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 97 AND ASCII(SUBSTRING(scope_kind,4,1)) = 109) OR (OCTET_LENGTH(scope_kind) = 3 AND ASCII(SUBSTRING(scope_kind,1,1)) = 107 AND ASCII(SUBSTRING(scope_kind,2,1)) = 101 AND ASCII(SUBSTRING(scope_kind,3,1)) = 121) OR (OCTET_LENGTH(scope_kind) = 7 AND ASCII(SUBSTRING(scope_kind,1,1)) = 112 AND ASCII(SUBSTRING(scope_kind,2,1)) = 114 AND ASCII(SUBSTRING(scope_kind,3,1)) = 111 AND ASCII(SUBSTRING(scope_kind,4,1)) = 106 AND ASCII(SUBSTRING(scope_kind,5,1)) = 101 AND ASCII(SUBSTRING(scope_kind,6,1)) = 99 AND ASCII(SUBSTRING(scope_kind,7,1)) = 116) OR ((OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) AND (OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112))"`
	TokensMonthBehavior string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_tokens_month_behavior,(OCTET_LENGTH(tokens_month_behavior) = 4 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(tokens_month_behavior) = 10 AND ASCII(SUBSTRING(tokens_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(tokens_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(tokens_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(tokens_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(tokens_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(tokens_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(tokens_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(tokens_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(tokens_month_behavior,10,1)) = 121)"`
	MoneyMonthBehavior  string `gorm:"size:16;not null;default:stop;check:ck_resource_limits_money_month_behavior,(OCTET_LENGTH(money_month_behavior) = 4 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 115 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 112) OR (OCTET_LENGTH(money_month_behavior) = 10 AND ASCII(SUBSTRING(money_month_behavior,1,1)) = 97 AND ASCII(SUBSTRING(money_month_behavior,2,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,3,1)) = 101 AND ASCII(SUBSTRING(money_month_behavior,4,1)) = 114 AND ASCII(SUBSTRING(money_month_behavior,5,1)) = 116 AND ASCII(SUBSTRING(money_month_behavior,6,1)) = 95 AND ASCII(SUBSTRING(money_month_behavior,7,1)) = 111 AND ASCII(SUBSTRING(money_month_behavior,8,1)) = 110 AND ASCII(SUBSTRING(money_month_behavior,9,1)) = 108 AND ASCII(SUBSTRING(money_month_behavior,10,1)) = 121)"`
}

func (teamMemberBehaviorFrozenV74) TableName() string { return "resource_limits" }

func testTeamMemberMonthlyBehaviorMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	const old = "ck_resource_limits_monthly_behavior_scope_v74"
	const current = "ck_resource_limits_monthly_behavior_scope_v75"
	before := personalKeyBehaviorLedger(t, db)
	if len(before) != 75 || before[74].Version != 75 || !db.Migrator().HasConstraint(&entity.ResourceLimit{}, current) {
		t.Fatal("exact V75 ledger/check required")
	}
	row := entity.ResourceLimit{ScopeKind: "team_member", ScopeID: "member_v75_retained", ETag: "retained", TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	var retained entity.ResourceLimit
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&retained, "scope_kind = ? AND scope_id = ?", row.ScopeKind, row.ScopeID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, current); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().CreateConstraint(&teamMemberBehaviorFrozenV74{}, old); err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", row.ScopeKind, row.ScopeID).Update("tokens_month_behavior", "alert_only").Error
	}); err == nil {
		t.Fatal("V74 permitted member soft")
	}
	replay := func() {
		t.Helper()
		if err := db.Table("schema_migrations").Where("version = ?", 75).Delete(&struct{}{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	replay()
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, current) || db.Migrator().HasConstraint(&entity.ResourceLimit{}, old) {
		t.Fatal("V75 scope not repaired")
	}
	// Simulate interruption after both fences exist; replay must remove only V74.
	if err := db.Migrator().CreateConstraint(&teamMemberBehaviorFrozenV74{}, old); err != nil {
		t.Fatal(err)
	}
	replay()
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
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after entity.ResourceLimit
	if err := db.Session(&gorm.Session{QueryFields: true}).Take(&after, "scope_kind = ? AND scope_id = ?", row.ScopeKind, row.ScopeID).Error; err != nil || !teamBehaviorSameLimit(retained, after) {
		t.Fatal("upgrade rewrote original policy", err)
	}
	if !personalKeyBehaviorLedgerPreserved(before, personalKeyBehaviorLedger(t, db), 75) {
		t.Fatal("unrelated ledger rewritten")
	}
	for _, kind := range []string{"team_member", "Team_member", "team_member ", "project_key", "other"} {
		err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Create(&entity.ResourceLimit{ScopeKind: kind, ScopeID: "member_new_v75", ETag: "new", TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only"}).Error
		})
		if (err == nil) != (kind == "team_member") {
			t.Fatal("exact member scope CHECK", kind, err)
		}
	}
	for _, mode := range []string{"ALERT_ONLY", "alert_only ", "", "unknown"} {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return tx.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "member_new_v75").Update("money_month_behavior", mode).Error
		}); err == nil {
			t.Fatal("invalid mode stored", mode)
		}
	}
	if !reflect.DeepEqual(before[:74], personalKeyBehaviorLedger(t, db)[:74]) {
		t.Fatal("V74 predecessor ledger changed")
	}
}

func teamMemberMonthlyRegistryTail(names []string) bool {
	return len(names) == 144 && names[142] == "team_member_monthly_behavior_migration:testTeamMemberMonthlyBehaviorMigration" && names[143] == "team_member_monthly_behavior:testTeamMemberMonthlyBehaviorLifecycle"
}

func TestTeamMemberMonthlyBehaviorExactRegistryTail(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	pairs := regexp.MustCompile(`\{"([^"\n]+)", (test[A-Za-z0-9]+)\}`).FindAllStringSubmatch(string(raw), -1)
	names := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		names = append(names, pair[1]+":"+pair[2])
	}
	if !teamMemberMonthlyRegistryTail(names) || !personalKeyBehaviorRegistryMatches(names) || !projectKeyMonthlyBehaviorRegistryMatches(names) {
		t.Fatal("current144 or inherited142 registry drift")
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:143] },
		func(x []string) []string { x[142], x[143] = x[143], x[142]; return x },
		func(x []string) []string { x[143] = x[142]; return x },
		func(x []string) []string { x[143] = "team_member_monthly_behavior:unreviewed"; return x },
		func(x []string) []string { return append(x, "extra:unreviewed") },
	} {
		if teamMemberMonthlyRegistryTail(mutate(append([]string(nil), names...))) {
			t.Fatal("missing/reordered/duplicate/unreviewed/extra tail accepted")
		}
	}
	changed := append([]string(nil), names...)
	changed[0] = "unreviewed:unreviewed"
	if personalKeyBehaviorRegistryMatches(changed) || projectKeyMonthlyBehaviorRegistryMatches(changed) {
		t.Fatal("tail acceptance weakened retained prefix")
	}
}
