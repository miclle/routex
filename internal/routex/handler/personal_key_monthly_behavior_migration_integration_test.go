package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

// The historical fixtures retain their own V70/V71 assertions. Only this
// companion reconstructs V71 while V72 and later ledger entries remain recorded,
// then explicitly replays V73 to restore the current scope fence.
func personalKeyBehaviorHistoricalReplay(t *testing.T, db *gorm.DB, historical func(*testing.T, *gorm.DB)) {
	t.Helper()
	replayed := []int{71, 73}
	if reflect.ValueOf(historical).Pointer() == reflect.ValueOf(testPersonalMonthlyBehaviorMigration).Pointer() {
		replayed = append(replayed, 70)
	} else if reflect.ValueOf(historical).Pointer() != reflect.ValueOf(testTeamMonthlyBehaviorMigration).Pointer() {
		t.Fatal("unapproved historical companion")
	}
	before := personalKeyBehaviorLedger(t, db)
	if _, err := personalKeyBehaviorWithoutVersion(before, 71); err != nil {
		t.Fatal(err)
	}
	if _, err := personalKeyBehaviorWithoutVersion(before, 73); err != nil {
		t.Fatal(err)
	}
	// Register restoration before a failure-capable schema change.
	defer func() {
		if err := db.Table("schema_migrations").Where("version = ?", 73).Delete(&struct{}{}).Error; err != nil {
			t.Error("restore V73 boundary", err)
			return
		}
		if err := database.Migrate(context.Background(), db); err != nil {
			t.Error("restore V73 after historical fixture", err)
			return
		}
		after := personalKeyBehaviorLedger(t, db)
		if !personalKeyBehaviorLedgerPreserved(before, after, replayed...) {
			t.Error("historical replay changed unrelated ledger timestamps or versions")
		}
		if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73") || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") {
			t.Error("historical replay did not restore exact V73 fence")
		}
	}()
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	result := db.Table("schema_migrations").Where("version = ?", 71).Delete(&struct{}{})
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("reconstruct only V71", result.Error)
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") {
		t.Fatal("V71 reconstruction missing")
	}
	historical(t, db)
}

type personalKeyBehaviorMigrationEntry struct {
	Version   int
	AppliedAt string
}

func personalKeyBehaviorLedger(t *testing.T, db *gorm.DB) []personalKeyBehaviorMigrationEntry {
	t.Helper()
	var rows []personalKeyBehaviorMigrationEntry
	if err := db.Table("schema_migrations").Order("version").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func personalKeyBehaviorWithoutVersion(rows []personalKeyBehaviorMigrationEntry, version int) ([]personalKeyBehaviorMigrationEntry, error) {
	if len(rows) < 73 {
		return nil, fmt.Errorf("missing current V73 prefix")
	}
	result := make([]personalKeyBehaviorMigrationEntry, 0, len(rows)-1)
	found := false
	for i, row := range rows {
		if row.Version != i+1 {
			return nil, fmt.Errorf("noncontiguous migration history")
		}
		if row.Version == version {
			found = true
		} else {
			result = append(result, row)
		}
	}
	if !found || (version != 71 && version != 73) {
		return nil, fmt.Errorf("unsupported replay version")
	}
	return result, nil
}

func personalKeyBehaviorLedgerPreserved(before, after []personalKeyBehaviorMigrationEntry, replayed ...int) bool {
	if len(before) != len(after) {
		return false
	}
	for i, row := range before {
		if row.Version != after[i].Version || (!slices.Contains(replayed, row.Version) && row.AppliedAt != after[i].AppliedAt) {
			return false
		}
	}
	return true
}

func testPersonalKeyMonthlyBehaviorMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	beforeLedger := personalKeyBehaviorLedger(t, db)
	without, err := personalKeyBehaviorWithoutVersion(beforeLedger, 73)
	if err != nil {
		t.Fatal(err)
	}
	readLedger := func(want []personalKeyBehaviorMigrationEntry, replayed ...int) {
		t.Helper()
		if !personalKeyBehaviorLedgerPreserved(want, personalKeyBehaviorLedger(t, db), replayed...) {
			t.Fatal("unrelated migration ledger/version/time changed")
		}
	}
	remove := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 73).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("remove only V73", q.Error)
		}
		readLedger(without)
	}
	current := func() {
		t.Helper()
		for _, check := range []string{"ck_resource_limits_tokens_month_behavior", "ck_resource_limits_money_month_behavior", "ck_resource_limits_monthly_behavior_scope_v73"} {
			if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, check) {
				t.Fatal("missing current constraint", check)
			}
		}
		if db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") || db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope") {
			t.Fatal("historical scope fence retained")
		}
		columns, err := db.Migrator().ColumnTypes(&entity.ResourceLimit{})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, c := range columns {
			if c.Name() != "tokens_month_behavior" && c.Name() != "money_month_behavior" {
				continue
			}
			found++
			width, sized := c.Length()
			nullable, known := c.Nullable()
			if !sized || width != 16 || !known || nullable {
				t.Fatal("invalid mode storage", c.Name(), width)
			}
		}
		if found != 2 {
			t.Fatal("missing mode columns")
		}
		readLedger(beforeLedger, 73)
	}
	migrate := func(concurrent bool) {
		t.Helper()
		if concurrent {
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for range 2 {
				wg.Go(func() { results <- database.Migrate(ctx, db) })
			}
			wg.Wait()
			close(results)
			for err := range results {
				if err != nil {
					t.Fatal(err)
				}
			}
		} else if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		current()
	}
	current() // Empty-database creation is exercised by the unchanged harness.
	zero := int64(0)
	amount := "0.000000000000000001"
	kinds := []string{"user", "team", "key", "team_member", "project", "project_key"}
	for _, kind := range kinds {
		row := entity.ResourceLimit{ScopeKind: kind, ScopeID: "retained73_" + kind, ETag: "revision73", PreviousETag: "previous73", ActorID: "usr_history73", Reason: "Original exact policy", TokensMonth: &zero, MoneyMonth: &amount, Currency: "USD", IPMode: "none", IPRangesJSON: "[]", TokensMonthBehavior: "stop", MoneyMonthBehavior: "stop"}
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
		if err := db.Session(&gorm.Session{QueryFields: true}).Take(&row, "scope_kind = ? AND scope_id = ?", kind, "retained73_"+kind).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	snapshots := map[string]entity.ResourceLimit{}
	for _, kind := range kinds {
		snapshots[kind] = read(kind)
	}
	retained := func() {
		t.Helper()
		for kind, before := range snapshots {
			if !teamBehaviorSameLimit(before, read(kind)) {
				t.Fatal("retained policy/birth/revision changed", kind)
			}
		}
	}
	// Reconstruct the exact V71 upgrade boundary without changing V72 history.
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	q := db.Table("schema_migrations").Where("version = ?", 71).Delete(&struct{}{})
	if q.Error != nil || q.RowsAffected != 1 {
		t.Fatal("reconstruct V71", q.Error)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// V73 remains recorded, so reconstruction cannot silently replay it.
	if !db.Migrator().HasConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v71") {
		t.Fatal("expected frozen V71 scope")
	}
	beforeLedger = personalKeyBehaviorLedger(t, db)
	without, err = personalKeyBehaviorWithoutVersion(beforeLedger, 73)
	if err != nil {
		t.Fatal(err)
	}
	remove()
	migrate(true)
	migrate(false)
	retained()
	// Partial DDL with V73 already installed but its ledger entry absent.
	remove()
	migrate(false)
	retained()
	for _, field := range []string{"TokensMonthBehavior", "MoneyMonthBehavior"} {
		for _, value := range []any{nil, "", "STOP", "ALERT_ONLY", "stop ", "alert_only ", "alert_only       ", "alert_only\t", "alert_only\x00"} {
			before := read("key")
			err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", "retained73_key").UpdateColumn(field, value).Error
			if err == nil || !teamBehaviorSameLimit(before, read("key")) {
				t.Fatal("invalid Key behavior accepted or changed original", field, value, err)
			}
		}
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", "retained73_key").UpdateColumn(field, "alert_only").Error; err != nil {
			t.Fatal("exact Key soft storage rejected", err)
		}
		for _, kind := range []string{"team_member", "project", "project_key"} {
			before := read(kind)
			err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", kind, "retained73_"+kind).UpdateColumn(field, "alert_only").Error
			if err == nil || !teamBehaviorSameLimit(before, read(kind)) {
				t.Fatal("foreign soft mode admitted", kind, field, err)
			}
		}
	}
	snapshots["key"] = read("key")
	for _, alias := range []string{"Key", "KEY", "key "} {
		before := read("key")
		err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "key", "retained73_key").UpdateColumn("ScopeKind", alias).Error
		if err == nil || !teamBehaviorSameLimit(before, read("key")) {
			t.Fatal("Key scope alias accepted", alias, err)
		}
	}
	remove()
	migrate(true)
	retained()
	// Missing-current-check replay must fail on an invalid retained foreign row,
	// rather than normalize its behavior or lose the V72/later ledger.
	if err := db.Migrator().DropConstraint(&entity.ResourceLimit{}, "ck_resource_limits_monthly_behavior_scope_v73"); err != nil {
		t.Fatal(err)
	}
	remove()
	defer func() {
		if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "retained73_team_member").UpdateColumn("TokensMonthBehavior", "stop").Error; err != nil {
			t.Error(err)
			return
		}
		if err := database.Migrate(ctx, db); err != nil {
			t.Error("restore current V73", err)
		}
	}()
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "retained73_team_member").UpdateColumn("TokensMonthBehavior", "alert_only").Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err == nil {
		t.Fatal("invalid retained foreign mode normalized or admitted")
	}
	readLedger(without)
	if got := read("team_member"); got.TokensMonthBehavior != "alert_only" {
		t.Fatal("failed migration rewrote historical mode")
	}
	if err := db.Model(&entity.ResourceLimit{}).Where("scope_kind = ? AND scope_id = ?", "team_member", "retained73_team_member").UpdateColumn("TokensMonthBehavior", "stop").Error; err != nil {
		t.Fatal(err)
	}
	migrate(true)
	retained()
}

func TestPersonalKeyBehaviorFixtureLedgerReplayPreservesLaterVersions(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]personalKeyBehaviorMigrationEntry, 75)
	for i := range rows {
		rows[i] = personalKeyBehaviorMigrationEntry{Version: i + 1, AppliedAt: now.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)}
	}
	for _, version := range []int{71, 73} {
		got, err := personalKeyBehaviorWithoutVersion(rows, version)
		if err != nil || len(got) != 74 || got[len(got)-1].Version != 75 {
			t.Fatal("later versions lost", version, err)
		}
		for _, row := range got {
			if row.Version == version {
				t.Fatal("replay version retained")
			}
		}
	}
	for _, invalid := range [][]personalKeyBehaviorMigrationEntry{rows[:72], append(slices.Clone(rows[:70]), rows[71:]...), append(slices.Clone(rows[:70]), rows[69:]...)} {
		if _, err := personalKeyBehaviorWithoutVersion(invalid, 73); err == nil {
			t.Fatal("incomplete/duplicate prefix accepted")
		}
	}
	changed := slices.Clone(rows)
	changed[71].AppliedAt = now.Format(time.RFC3339Nano)
	if personalKeyBehaviorLedgerPreserved(rows, changed, 71, 73) {
		t.Fatal("Vault V72 timestamp change accepted")
	}
	changed = slices.Clone(rows)
	changed[72].AppliedAt = now.Format(time.RFC3339Nano)
	if !personalKeyBehaviorLedgerPreserved(rows, changed, 73) {
		t.Fatal("explicit owned replay rejected")
	}
	changed = slices.Clone(rows)
	changed[74].Version = 76
	if personalKeyBehaviorLedgerPreserved(rows, changed, 73) || reflect.DeepEqual(rows, changed) {
		t.Fatal("later version replacement accepted")
	}
}

func personalKeyBehaviorRegistryMatches(names []string) bool {
	if len(names) == 141 && names[137] == "personal_key_monthly_behavior_migration:testProjectBehaviorPersonalKeyMigrationAtCurrentSchema" && names[139] == "project_monthly_behavior_migration:testProjectMonthlyBehaviorMigration" && names[140] == "project_monthly_behavior:testProjectMonthlyBehaviorLifecycle" {
		names = slices.Clone(names[:139])
		names[137] = "personal_key_monthly_behavior_migration:testPersonalKeyMonthlyBehaviorMigration"
	}
	if len(names) != 139 || names[137] != "personal_key_monthly_behavior_migration:testPersonalKeyMonthlyBehaviorMigration" || names[138] != "personal_key_monthly_behavior:testPersonalKeyMonthlyBehaviorLifecycle" {
		return false
	}
	digest := sha256.Sum256([]byte(strings.Join(names[:137], "\n")))
	return hex.EncodeToString(digest[:]) == "3398dbf1a44dc6b95678b825c7ae241eb48517e49fe5752d02a1feb21f15c918"
}

func TestPersonalKeyBehaviorFixtureExactRegistryPrefixAndNewPair(t *testing.T) {
	raw, err := os.ReadFile("auth_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "auth_integration_test.go", raw, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok || len(literal.Elts) != 2 {
			return true
		}
		first, ok := literal.Elts[0].(*ast.BasicLit)
		if !ok || first.Kind != token.STRING {
			return true
		}
		second, ok := literal.Elts[1].(*ast.Ident)
		if !ok || !strings.HasPrefix(second.Name, "test") {
			return true
		}
		name, err := strconv.Unquote(first.Value)
		if err != nil {
			return true
		}
		names = append(names, name+":"+second.Name)
		return true
	})
	if !personalKeyBehaviorRegistryMatches(names) {
		t.Fatal("original137 registry pairs or exact appended2 changed", len(names))
	}
	for _, mutate := range []func([]string) []string{
		func(x []string) []string { return x[:138] },
		func(x []string) []string { x[133] = "team_monthly_behavior_migration:replacement"; return x },
		func(x []string) []string { x[137], x[138] = x[138], x[137]; return x },
		func(x []string) []string { x[136] = x[135]; return x },
	} {
		if personalKeyBehaviorRegistryMatches(mutate(slices.Clone(names))) {
			t.Fatal("missing/replaced/reordered/duplicate registry accepted")
		}
	}
	if !strings.Contains(string(raw), "versions != 74") || !strings.Contains(string(raw), "personalKeyBehaviorHistoricalReplay(t, db, test.run)") {
		t.Fatal("current ledger or bounded historical companion not bound")
	}
}
