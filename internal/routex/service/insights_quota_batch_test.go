package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func TestInsightsQuotaBatchPoliciesPreserveStoredValuesAndAbsentDefaults(t *testing.T) {
	zero, money := int64(0), "0.000000000000000001"
	targets := []insightsQuotaTarget{
		{kind: "team", row: InsightsQuotaRow{ID: "tem_saved"}},
		{kind: "key", row: InsightsQuotaRow{ID: "key_saved"}},
		{kind: "team", row: InsightsQuotaRow{ID: "tem_absent"}},
		{kind: "key", row: InsightsQuotaRow{ID: "pky_absent"}},
	}
	rows := []entity.ResourceLimit{
		{ScopeKind: "key", ScopeID: "key_saved", TokensMonth: &zero, MoneyMonth: &money, Currency: "USD", TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "stop", IPMode: "allowlist", IPRangesJSON: `["198.51.100.0/24"]`},
		{ScopeKind: "team", ScopeID: "tem_saved", TokensMonth: &zero, MoneyMonth: &money, Currency: "USD", TokensMonthBehavior: "alert_only"},
	}
	if err := projectInsightsQuotaPolicies(targets, rows); err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{0, 1} {
		if targets[i].policy.TokensMonth == nil || *targets[i].policy.TokensMonth != 0 || targets[i].policy.MoneyMonth == nil || *targets[i].policy.MoneyMonth != money || targets[i].policy.TokensMonthBehavior != "alert_only" {
			t.Fatal("batch rewrote a stored zero, exact money or monthly behavior")
		}
	}
	if len(targets[1].policy.IPRanges) != 1 || targets[1].policy.IPMode != "allowlist" {
		t.Fatal("proved Key decoder lost its independent IP restriction")
	}
	for _, i := range []int{2, 3} {
		if targets[i].policy.TokensMonth != nil || targets[i].policy.MoneyMonth != nil || targets[i].policy.Currency != "" {
			t.Fatal("missing stored policy acquired defaults or another account's cap")
		}
	}
}

func TestInsightsQuotaBatchRejectsAliasesDuplicatesAndCorruptPoliciesAtomically(t *testing.T) {
	for name, rows := range map[string][]entity.ResourceLimit{
		"scope_alias":          {{ScopeKind: "TEAM", ScopeID: "tem_one"}},
		"identity_alias":       {{ScopeKind: "team", ScopeID: "TEM_ONE"}},
		"other_target":         {{ScopeKind: "key", ScopeID: "key_other"}},
		"duplicate":            {{ScopeKind: "team", ScopeID: "tem_one"}, {ScopeKind: "team", ScopeID: "tem_one"}},
		"invalid_team_ip":      {{ScopeKind: "team", ScopeID: "tem_one", IPMode: "allowlist", IPRangesJSON: `["198.51.100.0/24"]`}},
		"invalid_key_json":     {{ScopeKind: "key", ScopeID: "key_two", IPRangesJSON: "invalid"}},
		"invalid_key_behavior": {{ScopeKind: "key", ScopeID: "key_two", TokensMonthBehavior: "unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			targets := []insightsQuotaTarget{{kind: "team", row: InsightsQuotaRow{ID: "tem_one"}, policy: limits.Policy{RPM: limitNumber(7)}}, {kind: "key", row: InsightsQuotaRow{ID: "key_two"}, policy: limits.Policy{RPM: limitNumber(9)}}}
			before := append([]insightsQuotaTarget(nil), targets...)
			if err := projectInsightsQuotaPolicies(targets, rows); err == nil || !reflect.DeepEqual(targets, before) {
				t.Fatal("unproved policy rows accepted or left a partial page", err)
			}
		})
	}
	for name, targets := range map[string][]insightsQuotaTarget{
		"invalid_kind":     {{kind: "user", row: InsightsQuotaRow{ID: "usr_other"}}},
		"invalid_identity": {{kind: "team", row: InsightsQuotaRow{ID: "unsafe/team"}}},
		"duplicate_target": {{kind: "team", row: InsightsQuotaRow{ID: "tem_one"}}, {kind: "team", row: InsightsQuotaRow{ID: "tem_one"}}},
		"overflow":         make([]insightsQuotaTarget, 52),
	} {
		t.Run(name, func(t *testing.T) {
			if err := hydrateInsightsQuotaPolicies(nil, targets); err == nil {
				t.Fatal("invalid target set reached SQL or acquired a default")
			}
		})
	}
	if err := hydrateInsightsQuotaPolicies(nil, nil); err != nil {
		t.Fatal("empty authorized page attempted a database read", err)
	}
}

func TestInsightsQuotaBatchSQLKeepsExactPairsAndCurrentMembership(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Query().Register("test:insights-quota-query", func(query *gorm.DB) {
			query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "ORDER BY", "LIMIT"}
			callbacks.BuildQuerySQL(query)
		}); err != nil {
			t.Fatal(err)
		}
		targets := []insightsQuotaTarget{{kind: "team", row: InsightsQuotaRow{ID: "tem_one"}}, {kind: "key", row: InsightsQuotaRow{ID: "key_two"}}}
		query := insightsQuotaPolicyQuery(db, targets).Find(&[]entity.ResourceLimit{})
		if !reflect.DeepEqual(query.Statement.Vars, []any{"team", "tem_one", "key", "key_two"}) || strings.Contains(query.Statement.SQL.String(), " IN ") {
			t.Fatal("policy batch lost exact selected kind/ID pairs", query.Statement.SQL.String(), query.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(query.Statement.SQL.String(), "AS BINARY") != 8 {
			t.Fatal("policy identities borrowed database collation", query.Statement.SQL.String())
		}
		team := insightsQuotaTeamQuery(db.Session(&gorm.Session{NewDB: true}), "usr_reader", false).Find(&[]entity.Team{})
		if !reflect.DeepEqual(team.Statement.Vars, []any{"usr_reader", "active", "owner", "member", "active"}) || !strings.Contains(team.Statement.SQL.String(), "EXISTS (") || strings.Count(team.Statement.SQL.String(), "team_memberships") != 2 {
			t.Fatal("Team selector lost exact current own-membership correlation", team.Statement.SQL.String(), team.Statement.Vars)
		}
		if dialect.Name() == "mysql" && strings.Count(team.Statement.SQL.String(), "AS BINARY") != 12 {
			t.Fatal("Team membership or lifecycle borrowed database collation", team.Statement.SQL.String())
		}
		platform := insightsQuotaTeamQuery(db.Session(&gorm.Session{NewDB: true}), "usr_reader", true).Find(&[]entity.Team{})
		if strings.Contains(platform.Statement.SQL.String(), "team_memberships") || len(platform.Statement.Vars) != 0 {
			t.Fatal("existing platform Team scope was narrowed to personal memberships")
		}
	}
}

func TestInsightsQuotaBatchPreservesDecoderFailures(t *testing.T) {
	row := entity.ResourceLimit{ScopeKind: "key", ScopeID: "key_one", TokensMonthBehavior: "invalid"}
	_, want := decodeLimitPolicy(row, true)
	got := projectInsightsQuotaPolicies([]insightsQuotaTarget{{kind: "key", row: InsightsQuotaRow{ID: row.ScopeID}}}, []entity.ResourceLimit{row})
	if want == nil || !errors.Is(got, want) {
		t.Fatal("batch hid the existing proved-Key decoder failure", got, want)
	}
}
