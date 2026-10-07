package service

import (
	"errors"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"testing"
)

func TestTeamMemberMonthlyBehaviorSparseAuthority(t *testing.T) {
	before, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only"})
	for _, body := range []string{`{"tokens_month":null}`, `{"tokens_month":0}`, `{"rpm":1}`} {
		next, err := applyTeamLimitInput(before, teamLimitInput(t, body), "team_member", teamMemberLimitFields, "USD")
		if err != nil || next.TokensMonthBehavior != "alert_only" || next.MoneyMonthBehavior != "alert_only" {
			t.Fatal("sparse member policy lost modes", body, err, next)
		}
	}
	next, err := applyTeamLimitInput(before, teamLimitInput(t, `{"tokens_month_behavior":"stop"}`), "team_member", []string{"tokens_month_behavior"}, "USD")
	if err != nil || next.TokensMonthBehavior != "" || next.MoneyMonthBehavior != "alert_only" || next.TokensMonth == nil || *next.TokensMonth != 0 {
		t.Fatal("independent mode patch", err, next)
	}
	_, err = applyTeamLimitInput(before, teamLimitInput(t, `{"tokens_month_behavior":"stop"}`), "team_member", []string{"money_month_behavior"}, "USD")
	if !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("money writer acquired Tokens mode", err)
	}
	for _, body := range []string{`{"money_month_behavior":null}`, `{"money_month_behavior":"ALERT_ONLY"}`, `{"money_month_behavior":"alert_only "}`, `{"tokens_month_behavior":false}`} {
		if _, err := applyTeamLimitInput(before, teamLimitInput(t, body), "team_member", teamMemberLimitFields, "USD"); err == nil {
			t.Fatal("invalid mode", body)
		}
	}
}

func TestTeamMemberMonthlyBehaviorWarningsAndPublicationModes(t *testing.T) {
	current, usage := memberWarningFixture(t)
	current.Row.TokensMonthBehavior = "alert_only"
	current.Row.MoneyMonthBehavior = "alert_only"
	var err error
	current.Stored, err = policyFromRow(current.Row)
	if err != nil {
		t.Fatal(err)
	}
	observations := monthlyTeamMemberQuotaWarnings(current, usage)
	if len(observations) != 2 || observations[0].Level != "near" || observations[1].Level != "critical" {
		t.Fatal("soft modes lost80/90 own-recipient warnings", observations)
	}
	usage.Month.TokensUsed = 100
	usage.Month.MoneyUnknown = 1
	if got := monthlyTeamMemberQuotaWarnings(current, usage); len(got) != 0 {
		t.Fatal("exhaustion/unknown manufactured warning", got)
	}
	svc, auth, context, _, _ := memberWarningRuntimeFixture(t)
	context.Stored = current.Stored
	auth.LimitPolicies[limitAccount("team_member", context.Row.ScopeID)] = current.Stored
	if !svc.teamLimitApplied(context) {
		t.Fatal("exact member publication not confirmed")
	}
	changed := current.Stored
	changed.MoneyMonthBehavior = ""
	context.Stored = changed
	if svc.teamLimitApplied(context) {
		t.Fatal("publication ignored independent member mode")
	}
}
