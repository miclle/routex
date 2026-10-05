package service

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func TestQuotaWarningExactThresholds(t *testing.T) {
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
func warningFixture() (entity.ResourceLimit, time.Time, *eventqueue.AccountQuotaUsage) {
	row, created, usage := monthlyNotificationFixture()
	v := int64(100)
	m := "0.000000000000000010"
	row.TokensMonth = &v
	row.MoneyMonth = &m
	usage.Month.TokensUsed = 80
	usage.Month.MoneyUsed = map[string]string{"USD": "0.000000000000000009"}
	return row, created, usage
}
func TestQuotaWarningsIndependentCurrentLevels(t *testing.T) {
	row, created, usage := warningFixture()
	before := *usage
	warnings := monthlyQuotaWarnings(row, created, usage, "USD")
	if len(warnings) != 2 || warnings[0].Level != "near" || warnings[1].Level != "critical" || warnings[1].Settled != "0.000000000000000009" || warnings[1].Limit != "0.00000000000000001" {
		t.Fatal("independent exact levels lost", warnings)
	}
	if !reflect.DeepEqual(before, *usage) {
		t.Fatal("observer changed authoritative journal")
	}
	usage.Month.TokensHeld = math.MaxInt64
	usage.Month.MoneyHeld = map[string]string{"USD": "999"}
	if w := monthlyQuotaWarnings(row, created, usage, "USD"); len(w) != 2 || w[0].Level != "near" {
		t.Fatal("hold changed settled level", w)
	}
	usage.Month.TokensUsed = 100
	if w := monthlyQuotaWarnings(row, created, usage, "USD"); len(w) != 1 || w[0].Dimension != "money" {
		t.Fatal("exhaustion manufactured prior crossing", w)
	}
	usage.Month.MoneyUnknown = 1
	usage.Month.TokensUsed = 90
	if w := monthlyQuotaWarnings(row, created, usage, "USD"); len(w) != 1 || w[0].Dimension != "tokens" || w[0].Level != "critical" {
		t.Fatal("unknown money erased known Tokens", w)
	}
	for _, name := range []string{"tokens_unknown", "mixed_currency", "platform_currency", "partial_coverage", "future_created", "nil_usage", "project", "team", "zero", "unset", "holds_only", "invalid_zone"} {
		t.Run(name, func(t *testing.T) {
			r, c, u := warningFixture()
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
			case "team":
				r.ScopeKind = "team"
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
			if got := monthlyQuotaWarnings(r, c, u, currency); len(got) != want {
				t.Fatal("unsafe current observation", name, got)
			}
		})
	}
}
func TestQuotaWarningCalendarAndImmutableIdentity(t *testing.T) {
	row, _, usage := warningFixture()
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	usage.TimeZone = zone.String()
	usage.AsOf = time.Date(2026, 3, 20, 0, 0, 0, 123456789, zone)
	created := time.Date(2026, 2, 1, 0, 0, 0, 0, zone)
	usage.CoverageStart = time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	w := monthlyQuotaWarnings(row, created, usage, "USD")
	if len(w) != 2 || w[0].MonthEnd.Sub(w[0].MonthStart) != 743*time.Hour || w[0].AsOf.Nanosecond() != 123456000 {
		t.Fatal("server DST/calendar evidence changed", w)
	}
	old := w[0]
	fresh := old
	fresh.AsOf = fresh.AsOf.Add(time.Hour)
	fresh.Settled = "85"
	if !sameQuotaWarningIdentity(old, fresh) {
		t.Fatal("current replay reset immutable observation")
	}
	for _, kind := range []string{"owner", "dimension", "month", "revision", "currency", "level", "generation", "birth"} {
		t.Run(kind, func(t *testing.T) {
			v := old
			switch kind {
			case "owner":
				v.OwnerID = strings.ToUpper(v.OwnerID)
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
			if sameQuotaWarningIdentity(old, v) {
				t.Fatal("distinct source borrowed observation")
			}
		})
	}
}
func TestQuotaWarningCompleteAdmissionAndRuntimeProof(t *testing.T) {
	for _, kind := range []string{"approved", "pending", "rejected", "missing", "alias", "birth", "revision", "disabled", "offboarded", "lease", "policy", "zone", "currency", "tombstone", "cancel", "owner_alias", "scope", "calendar_revision", "accounting_revision"} {
		t.Run(kind, func(t *testing.T) {
			row, created, _ := warningFixture()
			user := entity.User{ID: row.ScopeID, CreatedAt: created}
			user, apps := managedAdvisorySubject(user)
			summary, proof := registrationAdmission(user, apps)
			if !summary.AdmissionEligible {
				t.Fatal("invalid source fixture")
			}
			policy, err := policyFromRow(row)
			if err != nil {
				t.Fatal(err)
			}
			auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), UserAdmissions: map[string]runtimeAdmissionProof{user.ID: proof}, Quota: &runtimeQuotaData{Setting: entity.QuotaSetting{TimeZone: "UTC"}, Currency: "USD", Revisions: map[string]string{limitAccount("user", user.ID): row.ETag}}, LimitPolicies: map[string]limits.Policy{limitAccount("user", user.ID): policy}}
			s := &Service{runtime: &gatewayRuntime{}}
			s.runtime.auth.Store(auth)
			ctx := context.Background()
			application := apps[*user.ApprovalApplicationID]
			switch kind {
			case "pending":
				application.State = "pending"
				application.DecidedAt = nil
				application.DecisionActorID = nil
				application.DecisionReason = nil
				apps[application.ID] = application
			case "rejected":
				application.State = "rejected"
				apps[application.ID] = application
			case "missing":
				apps = nil
			case "alias":
				application.UserID = strings.ToUpper(user.ID)
				apps[application.ID] = application
			case "birth":
				user.CreatedAt = user.CreatedAt.Add(time.Microsecond)
			case "revision":
				application.Revision = strings.Repeat("b", 64)
				apps[application.ID] = application
			case "disabled":
				user.Disabled = true
			case "offboarded":
				user.OffboardedAt = &created
			case "lease":
				auth.ValidUntil = time.Now().Add(-time.Second)
			case "policy":
				auth.Quota.Revisions[limitAccount("user", user.ID)] = "lim_changed"
			case "zone":
				auth.Quota.Setting.TimeZone = "Etc/UTC"
			case "currency":
				auth.Quota.Currency = "EUR"
			case "tombstone":
				s.runtime.deniedUsers.Store(user.ID, true)
			case "owner_alias":
				row.ScopeID = strings.ToUpper(user.ID)
			case "scope":
				row.ScopeKind = "team"
			case "calendar_revision":
				auth.Quota.Setting.ETag = "qcs_other"
			case "accounting_revision":
				auth.Quota.Setting.AccountingStarted = true
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if applied := s.quotaWarningApplied(ctx, row, user, apps, policy, entity.QuotaSetting{TimeZone: "UTC"}, "USD"); applied != (kind == "approved") {
				t.Fatal("incomplete/current admission or policy proof", kind, applied)
			}
		})
	}
}
