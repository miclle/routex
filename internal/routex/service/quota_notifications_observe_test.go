package service

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func monthlyNotificationFixture() (entity.ResourceLimit, time.Time, *eventqueue.AccountQuotaUsage) {
	now := time.Date(2026, 3, 20, 12, 30, 0, 123456789, time.UTC)
	created := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	tokens := int64(10)
	money := "1.000000000000000001"
	row := entity.ResourceLimit{ScopeKind: "user", ScopeID: "usr_quota_notice", ETag: "lim_notice_v1", TokensMonth: &tokens, MoneyMonth: &money, Currency: "USD", IPMode: "none", IPRangesJSON: "[]"}
	usage := &eventqueue.AccountQuotaUsage{AsOf: now, TimeZone: "UTC", CoverageStart: created.Add(-time.Hour), Month: eventqueue.QuotaUsage{TokensUsed: 10, MoneyUsed: map[string]string{"USD": "1.000000000000000001"}}}
	return row, created, usage
}

func TestMonthlyQuotaObservationsExactSettledAndCalendar(t *testing.T) {
	row, created, usage := monthlyNotificationFixture()
	before := *usage
	observations := monthlyQuotaObservations(row, created, usage, "USD")
	if len(observations) != 2 || observations[0].Dimension != "tokens" || observations[0].Limit != "10" || observations[0].Settled != "10" || observations[0].Currency != "" || observations[1].Limit != "1.000000000000000001" || observations[1].Settled != observations[1].Limit || observations[1].Currency != "USD" {
		t.Fatalf("exact known exhaustion lost: %#v", observations)
	}
	for _, observation := range observations {
		if observation.PolicyRevision != row.ETag || observation.TimeZone != "UTC" || !observation.MonthStart.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) || !observation.MonthEnd.Equal(time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)) || observation.AsOf.Nanosecond() != 123456000 || !observation.ResourceCreatedAt.Equal(created) || !observation.CoverageStart.Equal(usage.CoverageStart) {
			t.Fatal("immutable calendar or evidence changed")
		}
	}
	if !reflect.DeepEqual(*usage, before) {
		t.Fatal("observer mutated authoritative usage")
	}
	usage.Month.MoneyUsed["USD"] = "1.000000000000000000"
	if values := monthlyQuotaObservations(row, created, usage, "USD"); len(values) != 1 || values[0].Dimension != "tokens" {
		t.Fatal("float rounding invented money exhaustion")
	}
	usage.Month.MoneyUsed["USD"] = "999999999999999999999999999999999999999999999999999999999999"
	if values := monthlyQuotaObservations(row, created, usage, "USD"); len(values) != 2 || values[1].Settled != usage.Month.MoneyUsed["USD"] {
		t.Fatal("large journal decimal was rounded or truncated")
	}
}

func TestMonthlyQuotaObservationsFailClosed(t *testing.T) {
	for _, kind := range []string{"coverage", "created_future", "coverage_future", "nil_usage", "timezone", "local", "empty_asof", "scope", "id", "revision", "negative_tokens", "tokens_unknown", "money_unknown", "mixed_currency", "wrong_currency", "wrong_platform", "bad_decimal", "holds_only", "unlimited"} {
		t.Run(kind, func(t *testing.T) {
			row, created, usage := monthlyNotificationFixture()
			platform := "USD"
			want := 0
			switch kind {
			case "coverage":
				created = usage.CoverageStart.Add(-time.Hour)
				usage.CoverageStart = usage.AsOf.Add(-time.Hour)
			case "created_future":
				created = usage.AsOf.Add(time.Second)
			case "coverage_future":
				usage.CoverageStart = usage.AsOf.Add(time.Second)
			case "nil_usage":
				usage = nil
			case "timezone":
				usage.TimeZone = "invalid/zone"
			case "local":
				usage.TimeZone = "Local"
			case "empty_asof":
				usage.AsOf = time.Time{}
			case "scope":
				row.ScopeKind = "USER"
			case "id":
				row.ScopeID = "unsafe/ID"
			case "revision":
				row.ETag = "bad revision"
			case "negative_tokens":
				usage.Month.TokensUsed = -1
				want = 1
			case "tokens_unknown":
				usage.Month.TokensUnknown = 1
				want = 1
			case "money_unknown":
				usage.Month.MoneyUnknown = 1
				want = 1
			case "mixed_currency":
				usage.Month.MoneyUsed["EUR"] = "0"
				want = 1
			case "wrong_currency":
				usage.Month.MoneyUsed = map[string]string{"usd": "2"}
				want = 1
			case "wrong_platform":
				platform = "EUR"
				want = 1
			case "bad_decimal":
				usage.Month.MoneyUsed["USD"] = "1e3"
				want = 1
			case "holds_only":
				usage.Month.TokensUsed = 0
				usage.Month.MoneyUsed = map[string]string{}
				usage.Month.TokensHeld = math.MaxInt64
				usage.Month.MoneyHeld = map[string]string{"USD": "1000"}
			case "unlimited":
				row.TokensMonth = nil
				row.MoneyMonth = nil
			}
			values := monthlyQuotaObservations(row, created, usage, platform)
			if len(values) != want {
				t.Fatalf("unsafe observation count %d want%d: %#v", len(values), want, values)
			}
		})
	}
}

func TestMonthlyQuotaObservationsZeroAndDSTCoverage(t *testing.T) {
	row, created, usage := monthlyNotificationFixture()
	zero := int64(0)
	money := "0"
	row.TokensMonth = &zero
	row.MoneyMonth = &money
	usage.Month.TokensUsed = 0
	usage.Month.MoneyUsed = map[string]string{}
	if values := monthlyQuotaObservations(row, created, usage, "USD"); len(values) != 2 || values[0].Settled != "0" || values[1].Settled != "0" {
		t.Fatal("zero finite cap treated as unlimited")
	}
	row.MoneyMonth = nil
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	usage.TimeZone = "America/New_York"
	usage.AsOf = time.Date(2026, 3, 20, 0, 0, 0, 0, zone)
	created = time.Date(2026, 2, 1, 0, 0, 0, 0, zone)
	usage.CoverageStart = time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	values := monthlyQuotaObservations(row, created, usage, "USD")
	if len(values) != 1 || values[0].MonthEnd.Sub(values[0].MonthStart) != 743*time.Hour || values[0].MonthStart.Hour() != 5 || values[0].MonthEnd.Hour() != 4 {
		t.Fatalf("journal DST month boundaries lost: %#v", values)
	}
	usage.CoverageStart = usage.CoverageStart.Add(time.Nanosecond)
	if len(monthlyQuotaObservations(row, created, usage, "USD")) != 0 {
		t.Fatal("partially covered month treated as complete")
	}
	created = usage.CoverageStart
	if len(monthlyQuotaObservations(row, created, usage, "USD")) != 1 {
		t.Fatal("resource created after activation should have complete lifetime coverage")
	}
}

func TestQuotaNotificationPolicyAppliedExactAndFresh(t *testing.T) {
	row, _, usage := monthlyNotificationFixture()
	policy, err := policyFromRow(row)
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{runtime: &gatewayRuntime{}}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Quota: &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: usage.TimeZone}, Currency: "USD", Revisions: map[string]string{limitAccount(row.ScopeKind, row.ScopeID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount(row.ScopeKind, row.ScopeID): policy}}
	service.runtime.auth.Store(auth)
	if !service.quotaNotificationPolicyApplied(row, policy, "UTC", "USD") {
		t.Fatal("exact applied current policy not recognized")
	}
	for _, kind := range []string{"nil_runtime", "expired", "revision", "values", "zone", "currency", "denied", "settings_denied"} {
		t.Run(kind, func(t *testing.T) {
			copyAuth := *auth
			copyAuth.Quota = &runtimeQuotaData{Setting: auth.Quota.Setting, Currency: auth.Quota.Currency, Revisions: map[string]string{limitAccount(row.ScopeKind, row.ScopeID): row.ETag}}
			copyAuth.LimitPolicies = map[string]limits.Policy{limitAccount(row.ScopeKind, row.ScopeID): policy}
			copyService := &Service{runtime: &gatewayRuntime{}}
			switch kind {
			case "nil_runtime":
				copyService.runtime = nil
			case "expired":
				copyAuth.ValidUntil = time.Now().Add(-time.Second)
			case "revision":
				copyAuth.Quota.Revisions[limitAccount(row.ScopeKind, row.ScopeID)] = "LIM_notice_v1"
			case "values":
				changed := policy
				changed.TokensMonth = limitNumber(11)
				copyAuth.LimitPolicies[limitAccount(row.ScopeKind, row.ScopeID)] = changed
			case "zone":
				copyAuth.Quota.Setting.TimeZone = "Etc/UTC"
			case "currency":
				copyAuth.Quota.Currency = "usd"
			case "denied":
				copyService.runtime.deniedLimits.Store(limitAccount(row.ScopeKind, row.ScopeID), uint64(1))
			case "settings_denied":
				copyService.runtime.deniedLimits.Store("quota_settings", uint64(1))
			}
			if copyService.runtime != nil {
				copyService.runtime.auth.Store(&copyAuth)
			}
			if copyService.quotaNotificationPolicyApplied(row, policy, "UTC", "USD") {
				t.Fatal("unapplied or unavailable policy promoted")
			}
		})
	}
}
