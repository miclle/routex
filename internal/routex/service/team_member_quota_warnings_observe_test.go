package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func memberWarningFixture(t *testing.T) (*teamLimitContext, *eventqueue.AccountQuotaUsage) {
	t.Helper()
	current, usage := memberMonthlyNotificationFixture(t)
	current.Actor.CreatedAt = current.Team.CreatedAt.Add(-time.Hour)
	current.Row.TokensMonth = limitNumber(100)
	money := "0.000000000000000010"
	current.Row.MoneyMonth = &money
	var err error
	current.Stored, err = policyFromRow(current.Row)
	if err != nil {
		t.Fatal(err)
	}
	usage.Month.TokensUsed = 80
	usage.Month.MoneyUsed = map[string]string{"USD": "0.000000000000000009"}
	return current, usage
}
func TestTeamMemberQuotaWarningKnownLevelsAndStablePair(t *testing.T) {
	current, usage := memberWarningFixture(t)
	before := *usage
	w := monthlyTeamMemberQuotaWarnings(current, usage)
	if len(w) != 2 || w[0].Level != "near" || w[1].Level != "critical" || w[1].Limit != "0.00000000000000001" || w[1].Settled != "0.000000000000000009" || !reflect.DeepEqual(before, *usage) {
		t.Fatal("independent known settled levels altered", w)
	}
	for _, v := range w {
		if !validTeamMemberQuotaWarningObservation(v) || v.ScopeID != teamMemberLimitScopeID(current.Team.ID, current.Actor.ID) || v.TeamID != current.Team.ID || v.MemberUserID != current.Actor.ID || !v.UserCreatedAt.Equal(current.Actor.CreatedAt) {
			t.Fatal("private pair or birth lost")
		}
	}
	current.Member.ID = "tmm_rejoined"
	fresh := monthlyTeamMemberQuotaWarnings(current, usage)
	if !sameTeamMemberQuotaWarningIdentity(w[0], fresh[0]) {
		t.Fatal("membership replacement reset stable pair history")
	}
	usage.Month.TokensHeld = math.MaxInt64
	usage.Month.MoneyHeld = map[string]string{"USD": "999"}
	if got := monthlyTeamMemberQuotaWarnings(current, usage); len(got) != 2 || got[0].Level != "near" {
		t.Fatal("holds counted as settled")
	}
	usage.Month.TokensUsed = 100
	if got := monthlyTeamMemberQuotaWarnings(current, usage); len(got) != 1 || got[0].Dimension != "money" {
		t.Fatal("exhaustion invented earlier warning")
	}
	usage.Month.TokensUsed = 90
	usage.Month.MoneyUnknown = 1
	if got := monthlyTeamMemberQuotaWarnings(current, usage); len(got) != 1 || got[0].Dimension != "tokens" || got[0].Level != "critical" {
		t.Fatal("unknown money erased known tokens")
	}
	for _, name := range []string{"wrong_pair", "inactive_team", "inactive_member", "disabled_user", "wrong_user", "wrong_team", "parent_only", "zero_birth", "future_birth", "future_team", "partial_coverage", "no_usage", "tokens_unknown", "mixed_currency", "wrong_currency", "finite_money_no_currency", "unset_money_no_currency", "zero_caps", "unset_caps", "holds_only", "invalid_zone"} {
		t.Run(name, func(t *testing.T) {
			c, u := memberWarningFixture(t)
			want := 0
			switch name {
			case "inactive_team":
				c.Team.Status = entity.ResourceArchived
			case "inactive_member":
				c.Member.Status = entity.ResourceDisabled
			case "disabled_user":
				c.Actor.Disabled = true
			case "wrong_pair":
				c.Row.ScopeID = teamMemberLimitScopeID(c.Team.ID, "usr_other")
			case "wrong_user":
				c.Member.UserID = "usr_other"
			case "wrong_team":
				c.Member.TeamID = "tem_other"
			case "parent_only":
				c.Row = c.ParentRow
			case "zero_birth":
				c.Actor.CreatedAt = time.Time{}
			case "future_birth":
				c.Actor.CreatedAt = u.AsOf.Add(time.Second)
			case "future_team":
				c.Team.CreatedAt = u.AsOf.Add(time.Second)
			case "partial_coverage":
				u.CoverageStart = u.AsOf.Add(-time.Hour)
			case "no_usage":
				u = nil
			case "tokens_unknown":
				u.Month.TokensUnknown = 1
				want = 1
			case "mixed_currency":
				u.Month.MoneyUsed["EUR"] = "0"
				want = 1
			case "wrong_currency":
				c.Pricing.PlatformCurrency = "EUR"
				want = 1
			case "finite_money_no_currency":
				c.Row.Currency = ""
			case "unset_money_no_currency":
				c.Row.MoneyMonth = nil
				c.Row.Currency = ""
				want = 1
			case "zero_caps":
				c.Row.TokensMonth = limitNumber(0)
				m := "0"
				c.Row.MoneyMonth = &m
			case "unset_caps":
				c.Row.TokensMonth = nil
				c.Row.MoneyMonth = nil
			case "holds_only":
				u.Month.TokensUsed = 0
				u.Month.MoneyUsed = nil
				u.Active.TokensHeld = 999
				u.Active.MoneyHeld = map[string]string{"USD": "999"}
			case "invalid_zone":
				u.TimeZone = "Local"
			}
			if got := monthlyTeamMemberQuotaWarnings(c, u); len(got) != want {
				t.Fatal("unsafe pair warning", name, got)
			}
		})
	}
}
func TestTeamMemberQuotaWarningExactIdentityAndCalendar(t *testing.T) {
	c, u := memberWarningFixture(t)
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	u.TimeZone = zone.String()
	u.AsOf = time.Date(2026, 3, 20, 0, 0, 0, 123456789, zone)
	u.CoverageStart = time.Date(2026, 3, 1, 0, 0, 0, 0, zone)
	c.Team.CreatedAt = u.CoverageStart
	c.Actor.CreatedAt = c.Team.CreatedAt.Add(-time.Hour)
	v := monthlyTeamMemberQuotaWarnings(c, u)[0]
	if v.MonthEnd.Sub(v.MonthStart) != 743*time.Hour || v.AsOf.Nanosecond() != 123456000 {
		t.Fatal("server calendar precision changed")
	}
	copy := v
	copy.Settled = "85"
	copy.AsOf = copy.AsOf.Add(time.Hour)
	if !sameTeamMemberQuotaWarningIdentity(v, copy) {
		t.Fatal("replay changed original identity")
	}
	for _, name := range []string{"scope", "team", "user", "team_birth", "user_birth", "dimension", "month", "revision", "currency", "level", "generation"} {
		t.Run(name, func(t *testing.T) {
			other := v
			switch name {
			case "scope":
				other.ScopeID = strings.ToLower(v.ScopeID)
			case "team":
				other.TeamID = strings.ToUpper(v.TeamID)
			case "user":
				other.MemberUserID = strings.ToUpper(v.MemberUserID)
			case "team_birth":
				other.ResourceCreatedAt = other.ResourceCreatedAt.Add(time.Millisecond)
			case "user_birth":
				other.UserCreatedAt = other.UserCreatedAt.Add(time.Millisecond)
			case "dimension":
				other.Dimension = "money"
			case "month":
				other.MonthStart = other.MonthEnd
			case "revision":
				other.PolicyRevision += "x"
			case "currency":
				other.Currency = "EUR"
			case "level":
				other.Level = "critical"
				other.Threshold = 90
			case "generation":
				other.ThresholdGeneration += "x"
			}
			if sameTeamMemberQuotaWarningIdentity(v, other) {
				t.Fatal("different tuple/generation borrowed history")
			}
		})
	}
}
