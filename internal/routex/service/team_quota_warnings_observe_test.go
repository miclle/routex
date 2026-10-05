package service

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestTeamQuotaWarningExactThresholds(t *testing.T) {
	for _, tt := range []struct {
		used, cap, level string
		threshold        int
	}{
		{"79.999999999999999999", "100", "", 0}, {"80", "100", "near", 80}, {"89.999999999999999999", "100", "near", 80}, {"90", "100", "critical", 90}, {"99.999999999999999999", "100", "critical", 90}, {"100", "100", "", 0}, {"101", "100", "", 0}, {"0", "0", "", 0}, {"80", "0", "", 0}, {"1e2", "100", "", 0}, {"-80", "100", "", 0}, {"80", "NaN", "", 0},
		{"0.000000000000000008", "0.000000000000000010", "near", 80}, {"0.000000000000000009", "0.000000000000000010", "critical", 90},
		{"8301034833169298226", "9223372036854775807", "near", 80}, {"8301034833169298227", "9223372036854775807", "critical", 90},
		{"900000000000000000000000000000000000000000000000000000000000", "1000000000000000000000000000000000000000000000000000000000000", "", 0},
	} {
		t.Run(tt.used+"/"+tt.cap, func(t *testing.T) {
			level, n := quotaWarningLevel(tt.used, tt.cap)
			if level != tt.level || n != tt.threshold {
				t.Fatalf("exact level %s/%d, want %s/%d", level, n, tt.level, tt.threshold)
			}
		})
	}
}
func teamWarningFixture() (entity.ResourceLimit, time.Time, *eventqueue.AccountQuotaUsage) {
	row, created, usage := monthlyNotificationFixture()
	row.ScopeKind = "team"
	row.ScopeID = "tem_exact"
	v := int64(100)
	m := "0.000000000000000010"
	row.TokensMonth = &v
	row.MoneyMonth = &m
	usage.Month.TokensUsed = 80
	usage.Month.MoneyUsed = map[string]string{"USD": "0.000000000000000009"}
	return row, created, usage
}
func TestTeamQuotaWarningsIndependentCurrentLevels(t *testing.T) {
	row, created, usage := teamWarningFixture()
	before := *usage
	warnings := monthlyTeamQuotaWarnings(row, created, usage, "USD")
	if len(warnings) != 2 || warnings[0].Level != "near" || warnings[1].Level != "critical" || warnings[1].Settled != "0.000000000000000009" || warnings[1].Limit != "0.00000000000000001" {
		t.Fatal("independent exact levels lost", warnings)
	}
	if !reflect.DeepEqual(before, *usage) {
		t.Fatal("observer changed authoritative journal")
	}
	usage.Month.TokensHeld = math.MaxInt64
	usage.Month.MoneyHeld = map[string]string{"USD": "999"}
	if w := monthlyTeamQuotaWarnings(row, created, usage, "USD"); len(w) != 2 || w[0].Level != "near" {
		t.Fatal("hold changed settled level", w)
	}
	usage.Month.TokensUsed = 100
	if w := monthlyTeamQuotaWarnings(row, created, usage, "USD"); len(w) != 1 || w[0].Dimension != "money" {
		t.Fatal("exhaustion manufactured prior crossing", w)
	}
	usage.Month.MoneyUnknown = 1
	usage.Month.TokensUsed = 90
	if w := monthlyTeamQuotaWarnings(row, created, usage, "USD"); len(w) != 1 || w[0].Dimension != "tokens" || w[0].Level != "critical" {
		t.Fatal("unknown money erased known Tokens", w)
	}
	for _, name := range []string{"tokens_unknown", "mixed_currency", "platform_currency", "partial_coverage", "future_created", "nil_usage", "project", "user", "zero", "unset", "holds_only", "invalid_zone"} {
		t.Run(name, func(t *testing.T) {
			r, c, u := teamWarningFixture()
			currency := "USD"
			want := 0
			switch name {
			case "tokens_unknown":
				u.Month.TokensUnknown = 1
				want = 1
			case "mixed_currency":
				u.Month.MoneyUsed["EUR"] = "0"
				want = 1
			case "platform_currency":
				currency = "EUR"
				want = 1
			case "partial_coverage":
				u.CoverageStart = u.AsOf.Add(-time.Hour)
			case "future_created":
				c = u.AsOf.Add(time.Second)
			case "nil_usage":
				u = nil
			case "project":
				r.ScopeKind = "project"
			case "user":
				r.ScopeKind = "user"
			case "zero":
				v := int64(0)
				m := "0"
				r.TokensMonth = &v
				r.MoneyMonth = &m
			case "unset":
				r.TokensMonth = nil
				r.MoneyMonth = nil
			case "holds_only":
				u.Month.TokensUsed = 0
				u.Month.MoneyUsed = map[string]string{}
				u.Month.TokensHeld = math.MaxInt64
				u.Month.MoneyHeld = map[string]string{"USD": "1000"}
			case "invalid_zone":
				u.TimeZone = "Local"
			}
			if got := monthlyTeamQuotaWarnings(r, c, u, currency); len(got) != want {
				t.Fatal("unsafe current observation", name, got)
			}
		})
	}
}
func TestTeamQuotaWarningCalendarAndImmutableIdentity(t *testing.T) {
	row, _, usage := teamWarningFixture()
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	usage.TimeZone = zone.String()
	usage.AsOf = time.Date(2026, 3, 20, 0, 0, 0, 123456789, zone)
	created := time.Date(2026, 2, 1, 0, 0, 0, 0, zone)
	usage.CoverageStart = time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	w := monthlyTeamQuotaWarnings(row, created, usage, "USD")
	if len(w) != 2 || w[0].MonthEnd.Sub(w[0].MonthStart) != 743*time.Hour || w[0].AsOf.Nanosecond() != 123456000 {
		t.Fatal("server DST/calendar evidence changed", w)
	}
	old := w[0]
	fresh := old
	fresh.AsOf = fresh.AsOf.Add(time.Hour)
	fresh.Settled = "85"
	if !sameTeamQuotaWarningIdentity(old, fresh) {
		t.Fatal("current replay reset immutable observation")
	}
	for _, kind := range []string{"owner", "dimension", "month", "revision", "currency", "level", "generation", "birth"} {
		t.Run(kind, func(t *testing.T) {
			v := old
			switch kind {
			case "owner":
				v.TeamID = strings.ToUpper(v.TeamID)
			case "dimension":
				v.Dimension = "money"
			case "month":
				v.MonthStart = v.MonthEnd
			case "revision":
				v.PolicyRevision += "x"
			case "currency":
				v.Currency = "USD"
			case "level":
				v.Level = "critical"
				v.Threshold = 90
			case "generation":
				v.ThresholdGeneration += "x"
			case "birth":
				v.ResourceCreatedAt = v.ResourceCreatedAt.Add(time.Microsecond)
			}
			if sameTeamQuotaWarningIdentity(old, v) {
				t.Fatal("distinct source borrowed observation")
			}
		})
	}
}
