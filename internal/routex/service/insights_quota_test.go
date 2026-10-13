package service

import (
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func insightsQuotaFixture() (InsightsQuotaRow, limits.Policy, time.Time, eventqueue.QuotaUsageAccountProof) {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	cap := int64(1000)
	return InsightsQuotaRow{ID: "tem_quota", Name: "Quota Team", Status: "active"}, limits.Policy{TokensMonth: &cap}, created, eventqueue.QuotaUsageAccountProof{Registered: true, CreatedAt: created, Usage: eventqueue.AccountQuotaUsage{AsOf: asOf, CoverageStart: created, TimeZone: "UTC", Month: eventqueue.QuotaUsage{TokensUsed: 900}, SevenDays: eventqueue.QuotaUsage{TokensUsed: 700}, Active: eventqueue.QuotaUsage{TokensHeld: 300}}}
}

func TestInsightsQuotaExactForecastAndThresholds(t *testing.T) {
	row, policy, created, proof := insightsQuotaFixture()
	got := insightsQuotaProject(row, policy, "tokens", "", created, &proof)
	want := proof.Usage.AsOf.Add(24 * time.Hour).Format(time.RFC3339Nano)
	if got.EstimateStatus != "estimated" || got.EstimateAt == nil || *got.EstimateAt != want || got.Level != "critical" || *got.UtilizationPercent != "90.00" || *got.MonthHeld != "0" || *got.ActiveHeld != "300" || *got.MonthSettled != "900" {
		t.Fatalf("settled forecast or separated holds changed: %+v", got)
	}
	for _, check := range []struct {
		used                   int64
		level, percent, status string
	}{{799, "normal", "79.90", "estimated"}, {800, "near", "80.00", "estimated"}, {899, "near", "89.90", "estimated"}, {900, "critical", "90.00", "estimated"}, {1000, "reached", "100.00", "already_reached"}, {1200, "reached", "120.00", "already_reached"}} {
		proof.Usage.Month.TokensUsed = check.used
		got = insightsQuotaProject(row, policy, "tokens", "", created, &proof)
		if got.Level != check.level || *got.UtilizationPercent != check.percent || got.EstimateStatus != check.status {
			t.Fatal(check, got)
		}
		if check.status == "already_reached" && got.EstimateAt != nil {
			t.Fatal("already reached must not fabricate a future date")
		}
	}
	proof.Usage.Month.TokensUsed = 900
	for _, check := range []struct {
		baseline int64
		within   bool
	}{{99, false}, {100, true}, {101, true}} {
		proof.Usage.SevenDays.TokensUsed = check.baseline
		got = insightsQuotaProject(row, policy, "tokens", "", created, &proof)
		if got.EstimateStatus != "estimated" || got.EstimateWithinSevenDays != check.within {
			t.Fatal("exact next-seven-days boundary changed", check, got)
		}
		if check.baseline == 100 && *got.EstimateAt != proof.Usage.AsOf.Add(7*24*time.Hour).Format(time.RFC3339Nano) {
			t.Fatal("seven-day boundary was rounded", got)
		}
	}
}

func TestInsightsQuotaUnavailableFactsNeverPredict(t *testing.T) {
	for _, check := range []struct {
		name, status string
		change       func(*InsightsQuotaRow, *limits.Policy, *time.Time, *eventqueue.QuotaUsageAccountProof)
	}{
		{"unregistered", "unavailable", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Registered = false
		}},
		{"birth_mismatch", "unavailable", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.CreatedAt = p.CreatedAt.Add(time.Microsecond)
		}},
		{"legacy_birth", "unavailable", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.CreatedAt = time.Time{}
		}},
		{"journal_gap", "incomplete_coverage", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.CoverageStart = p.Usage.AsOf.Add(-6 * 24 * time.Hour)
		}},
		{"recent_birth", "incomplete_coverage", func(_ *InsightsQuotaRow, _ *limits.Policy, c *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			*c = p.Usage.AsOf.Add(-6 * 24 * time.Hour)
			p.CreatedAt = *c
		}},
		{"bounded_unknown_month", "unknown_usage", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.Month.TokensHeld = 1
		}},
		{"unknown_month", "unknown_usage", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.Month.TokensUnknown = 1
		}},
		{"bounded_unknown_baseline", "unknown_usage", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.SevenDays.TokensHeld = 1
		}},
		{"unknown_baseline", "unknown_usage", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.SevenDays.TokensUnknown = 1
		}},
		{"zero_rate", "zero_rate", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.SevenDays.TokensUsed = 0
		}},
		{"negative", "unavailable", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.Month.TokensUsed = -1
		}},
		{"inactive", "inactive", func(r *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, _ *eventqueue.QuotaUsageAccountProof) {
			r.Status = "disabled"
		}},
		{"unset", "no_local_cap", func(_ *InsightsQuotaRow, p *limits.Policy, _ *time.Time, _ *eventqueue.QuotaUsageAccountProof) {
			p.TokensMonth = nil
		}},
		{"zero_cap", "zero_cap", func(_ *InsightsQuotaRow, p *limits.Policy, _ *time.Time, _ *eventqueue.QuotaUsageAccountProof) {
			zero := int64(0)
			p.TokensMonth = &zero
		}},
		{"after_reset", "after_reset", func(_ *InsightsQuotaRow, _ *limits.Policy, _ *time.Time, p *eventqueue.QuotaUsageAccountProof) {
			p.Usage.SevenDays.TokensUsed = 1
		}},
	} {
		t.Run(check.name, func(t *testing.T) {
			row, policy, created, proof := insightsQuotaFixture()
			check.change(&row, &policy, &created, &proof)
			got := insightsQuotaProject(row, policy, "tokens", "", created, &proof)
			if got.EstimateStatus != check.status || got.EstimateAt != nil || got.EstimateWithinSevenDays {
				t.Fatalf("%s: %+v", check.status, got)
			}
		})
	}
	row, policy, created, proof := insightsQuotaFixture()
	proof.Usage.Active.TokensUnknown = 1
	got := insightsQuotaProject(row, policy, "tokens", "", created, &proof)
	if got.ActiveHeld != nil || got.EstimateStatus != "estimated" {
		t.Fatal("unknown pending holds must stay separate from settled rate", got)
	}
	proof.Usage.Active.TokensUnknown = 0
	proof.Usage.Month.TokensHeld = 3
	got = insightsQuotaProject(row, policy, "tokens", "", created, &proof)
	if got.EstimateStatus != "unknown_usage" || got.UtilizationPercent != nil || got.Level != "unknown" || *got.MonthHeld != "3" || *got.MonthSettled != "900" {
		t.Fatal("a bounded terminal unknown must retain its hold without inventing settled usage", got)
	}
}

func TestInsightsQuotaMoneyExactDecimalAndReset(t *testing.T) {
	row, policy, created, proof := insightsQuotaFixture()
	cap := "0.000000000000000007"
	policy.MoneyMonth, policy.Currency = &cap, "USD"
	policy.MoneyMonthBehavior = limits.MonthlyBehaviorAlertOnly
	proof.Usage.Month.MoneyUsed = map[string]string{"USD": "0.000000000000000006"}
	proof.Usage.SevenDays.MoneyUsed = map[string]string{"USD": "604800"}
	got := insightsQuotaProject(row, policy, "money", "USD", created, &proof)
	if got.EstimateAt == nil || *got.EstimateAt != proof.Usage.AsOf.Add(time.Nanosecond).Format(time.RFC3339Nano) || got.Behavior != "alert_only" || *got.UtilizationPercent != "85.71" || got.Level != "near" {
		t.Fatal("exact positive sub-nanosecond duration must ceil once", got)
	}
	for _, value := range []string{"1/2", "1e3", "-1", "01", "0.0000000000000000001", strings.Repeat("9", 61)} {
		proof.Usage.Month.MoneyUsed = map[string]string{"USD": value}
		got = insightsQuotaProject(row, policy, "money", "USD", created, &proof)
		if got.EstimateAt != nil || got.EstimateStatus != "unavailable" {
			t.Fatal("invalid journal decimal", value, got)
		}
	}
	proof.Usage.Month.MoneyUsed = map[string]string{"EUR": "1"}
	if got = insightsQuotaProject(row, policy, "money", "USD", created, &proof); got.EstimateStatus != "unavailable" || got.MonthSettled != nil {
		t.Fatal("foreign amount cannot become zero", got)
	}
	row, policy, created, proof = insightsQuotaFixture()
	proof.Usage.AsOf = time.Date(2026, 3, 1, 5, 0, 0, 0, time.UTC)
	proof.Usage.TimeZone = "America/New_York"
	start, reset, ok := insightsQuotaCalendar(proof.Usage.AsOf, proof.Usage.TimeZone)
	if !ok || start.Format(time.RFC3339) != "2026-03-01T05:00:00Z" || reset.Format(time.RFC3339) != "2026-04-01T04:00:00Z" {
		t.Fatal("calendar reset must preserve DST", start, reset)
	}
	proof.Usage.AsOf = reset.Add(-24 * time.Hour)
	if got = insightsQuotaProject(row, policy, "tokens", "", created, &proof); got.EstimateStatus != "after_reset" || got.EstimateAt != nil {
		t.Fatal("exact reset belongs to next month", got)
	}
	proof.Usage.Month.TokensUsed = 9223372036854775807
	got = insightsQuotaProject(row, policy, "tokens", "", created, &proof)
	if got.MonthSettled == nil || *got.MonthSettled != "9223372036854775807" || got.Level != "reached" || got.EstimateStatus != "already_reached" {
		t.Fatal("int64 facts must survive beyond JavaScript's exact integer range", got)
	}
	cap = "0.000000000000000001"
	policy.MoneyMonth, policy.Currency = &cap, "USD"
	proof.Usage.Month.MoneyUsed = map[string]string{"USD": strings.Repeat("9", 60)}
	proof.Usage.SevenDays.MoneyUsed = map[string]string{"USD": "1"}
	got = insightsQuotaProject(row, policy, "money", "USD", created, &proof)
	if got.MonthSettled == nil || *got.MonthSettled != strings.Repeat("9", 60) || got.Level != "reached" || got.UtilizationPercent == nil || strings.Contains(*got.UtilizationPercent, "e") {
		t.Fatal("large exact money must remain plain decimal text", got)
	}
}

func TestInsightsQuotaPaginationScopeAndCSV(t *testing.T) {
	input := InsightsQuotaInput{Dimension: "tokens", ScopeKind: "member", ScopeID: "usr_subject", Limit: 50}
	input.Cursor = insightsQuotaCursor("usr_actor", input, "key_exact")
	if _, after, err := normalizeInsightsQuota("usr_actor", input, true); err != nil || after != "key_exact" {
		t.Fatal(after, err)
	}
	for _, change := range []func(*InsightsQuotaInput){func(i *InsightsQuotaInput) { i.ScopeID = "usr_other" }, func(i *InsightsQuotaInput) { i.ScopeKind = "project" }, func(i *InsightsQuotaInput) { i.Dimension = "money" }, func(i *InsightsQuotaInput) { i.Limit = 51 }, func(i *InsightsQuotaInput) { i.Cursor += "=" }} {
		bad := input
		change(&bad)
		if _, _, err := normalizeInsightsQuota("usr_actor", bad, true); err == nil {
			t.Fatal("scope/cursor/bound accepted", bad)
		}
	}
	if _, _, err := normalizeInsightsQuota("usr_other", input, true); err == nil {
		t.Fatal("cross-actor cursor accepted")
	}
	row, policy, created, proof := insightsQuotaFixture()
	row.Name = "=HYPERLINK(\"bad\")\nprivate"
	row = insightsQuotaProject(row, policy, "tokens", "", created, &proof)
	report := &InsightsQuotaReport{Kind: "team_quota", Dimension: "tokens", Scope: InsightsQuotaScope{Kind: "readable_teams"}, BaselineBasis: "settled_admissions_168h", Items: []InsightsQuotaRow{row}}
	encoded, err := encodeInsightsQuotaCSV(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(encoded))).ReadAll()
	if err != nil || len(rows) != 3 || len(rows[0]) != len(rows[2]) || rows[2][19] != "'"+row.Name || rows[1][16] != "1" || rows[2][24] != "'900" {
		t.Fatal("bounded rectangular CSV or reversible text encoding changed", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = encodeInsightsQuotaCSV(ctx, report); err == nil {
		t.Fatal("cancelled export published bytes")
	}
}
