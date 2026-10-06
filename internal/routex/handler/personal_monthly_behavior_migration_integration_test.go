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

// Appended by the root after the unchanged 130-case predecessor; V70 is not a
// released historical schema. The harness first exercises empty startup.
func testPersonalMonthlyBehaviorMigration(t *testing.T, db *gorm.DB) {
	const version = 70
	// Exercise the immutable User-only V70 contract, then restore the current
	// V71 schema and complete ledger. Keep V71 recorded during the controlled
	// V70 replay so the original User-only scope assertions exercise V70, then
	// explicitly replay V71 in teardown. Later support must not weaken V70.
	var currentLedger []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &currentLedger).Error; err != nil {
		t.Fatal(err)
	}
	seenV71 := 0
	for i, v := range currentLedger {
		if v != i+1 {
			t.Fatal("incomplete current ledger", currentLedger)
		}
		if v == 71 {
			seenV71++
		}
	}
	if seenV71 != 1 {
		t.Fatal("expected V71 once", currentLedger)
	}
	if db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") {
		if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71"); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		if result := db.Table("schema_migrations").Where("version = ?", 71).Delete(&struct{}{}); result.Error != nil || result.RowsAffected != 1 {
			t.Error("restore V71 ledger boundary", result.Error)
			return
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Error("restore V71 after historical fixture", err)
			return
		}
		var restored []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &restored).Error; err != nil || !reflect.DeepEqual(restored, currentLedger) {
			t.Error("current ledger changed after V70 fixture", err, restored)
		}
	}()
	var baseline []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &baseline).Error; err != nil {
		t.Fatal(err)
	}
	without := []int{}
	count := 0
	for i, v := range baseline {
		if i > 0 && v <= baseline[i-1] {
			t.Fatal("invalid ordered ledger")
		}
		if v == version {
			count++
		} else {
			without = append(without, v)
		}
	}
	if count != 1 {
		t.Fatal("V70 must occur exactly once")
	}
	ledger := func(want []int) {
		t.Helper()
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("unrelated ledger changed", err, got, want)
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", version).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("V70 ledger removal", q.Error)
		}
		ledger(without)
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		ledger(baseline)
	}
	concurrent := func() {
		t.Helper()
		var wg sync.WaitGroup
		failures := make(chan error, 2)
		for range 2 {
			wg.Go(func() { failures <- database.Migrate(context.Background(), db) })
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		ledger(baseline)
	}
	checks := []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior", "ck_resource_limits_monthly_behavior_scope"}
	dropChecks := func() {
		t.Helper()
		for _, name := range checks {
			if db.Migrator().HasConstraint(&entity.ResourceLimit{}, name) {
				if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, name); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	one := int64(1)
	zero := int64(0)
	money := "0.000000000000000001"
	for _, kind := range []string{"user", "project", "key", "project_key", "team", "team_member"} {
		row := entity.ResourceLimit{ScopeKind: kind, ScopeID: "retained70_" + kind, ETag: "original70", PreviousETag: "previous70", ActorID: "usr_history70", Reason: "Historical reason", TokensMonth: &zero, Tokens5H: &one, MoneyMonth: &money, Currency: "USD", IPMode: "none", IPRangesJSON: "[]"}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Read only pre-V70 columns, so a historical comparison never borrows defaults.
	type retained struct {
		ScopeKind, ScopeID, ETag, PreviousETag, ActorID, Reason, Currency, IPMode, IPRangesJSON string
		TokensMonth, Tokens5H                                                                   *int64
		MoneyMonth                                                                              *string
	}
	read := func() []retained {
		t.Helper()
		var rows []retained
		if err := db.Table("resource_limits").Select("scope_kind", "scope_id", "e_tag", "previous_e_tag", "actor_id", "reason", "currency", "ip_mode", "ip_ranges_json", "tokens_month", "tokens5_h", "money_month").Order("scope_kind, scope_id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	// GORM names Tokens5H as tokens5_h; use the model schema's generated mapping.
	original := read()
	preserved := func() {
		t.Helper()
		if !reflect.DeepEqual(read(), original) {
			t.Fatal("historical cap/identity/revision/reason changed")
		}
	}
	dropChecks()
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		if err := db.Migrator().DropColumn(&entity.ResourceLimit{}, field); err != nil {
			t.Fatal(err)
		}
	}
	remove()
	preserved()
	concurrent()
	migrate()
	preserved()
	assertRows := func() {
		t.Helper()
		columns, err := db.Migrator().ColumnTypes(&entity.ResourceLimit{})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, column := range columns {
			if column.Name() != "tokens_month_behavior" && column.Name() != "money_month_behavior" {
				continue
			}
			found++
			length, sized := column.Length()
			nullable, known := column.Nullable()
			if !sized || length != 16 || !known || nullable {
				t.Fatal("behavior storage may truncate an invalid alias", column.Name(), length)
			}
		}
		if found != 2 {
			t.Fatal("missing independent behavior columns")
		}
		var rows []entity.ResourceLimit
		if err := db.Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if row.TokensMonthBehavior != "stop" || row.MoneyMonthBehavior != "stop" {
				t.Fatal("upgrade fabricated soft behavior", row.ScopeKind)
			}
		}
		for _, name := range checks {
			if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, name) {
				t.Fatal("missing exact constraint", name)
			}
		}
	}
	assertRows()
	concurrent()
	assertRows()
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		for _, value := range []any{nil, "", "STOP", "ALERT_ONLY", "stop ", "alert_only ", "unknown", "alert_only       ", "stop             ", "alert_only\t", "alert_only\x00"} {
			var before, after entity.ResourceLimit
			if err := db.Take(&before, "scope_kind = ? AND scope_id = ?", "user", "retained70_user").Error; err != nil {
				t.Fatal(err)
			}
			err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", "retained70_user").UpdateColumn(field, value).Error
			if readErr := db.Take(&after, "scope_kind = ? AND scope_id = ?", "user", "retained70_user").Error; readErr != nil {
				t.Fatal(readErr)
			}
			if err == nil || !reflect.DeepEqual(before, after) {
				t.Fatal("invalid behavior constraint accepted or changed persisted row", field, value, err)
			}
		}
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", "retained70_user").UpdateColumn(field, "alert_only").Error; err != nil {
			t.Fatal("exact User mode rejected", err)
		}
		for _, kind := range []string{"project", "key", "project_key", "team", "team_member"} {
			if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", kind, "retained70_"+kind).UpdateColumn(field, "alert_only").Error; err == nil {
				t.Fatal("foreign soft accepted", kind, field)
			}
		}
	}
	for _, kind := range []string{"User", "user ", "USER"} {
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "user", "retained70_user").UpdateColumn("ScopeKind", kind).Error; err == nil {
			t.Fatal("scope collation alias borrowed User soft", kind)
		}
	}
	// Interrupted second-column/check creation preserves the first saved mode.
	dropChecks()
	if err := db.Migrator().DropColumn(&entity.ResourceLimit{}, "MoneyMonthBehavior"); err != nil {
		t.Fatal(err)
	}
	remove()
	concurrent()
	migrate()
	preserved()
	var user entity.ResourceLimit
	if err := db.Take(&user, "scope_kind = ? AND scope_id = ?", "user", "retained70_user").Error; err != nil || user.TokensMonthBehavior != "alert_only" || user.MoneyMonthBehavior != "stop" {
		t.Fatal("partial DDL lost independent mode", err)
	}
	// Invalid already-present nonuser soft values fail closed, not silently reset.
	dropChecks()
	remove()
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", "retained70_key").UpdateColumn("MoneyMonthBehavior", "alert_only").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err == nil {
		t.Fatal("invalid historical foreign soft migration succeeded")
	}
	ledger(without)
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", "retained70_key").UpdateColumn("MoneyMonthBehavior", "stop").Error; err != nil {
		t.Fatal(err)
	}
	concurrent()
	migrate()
	preserved()
}
