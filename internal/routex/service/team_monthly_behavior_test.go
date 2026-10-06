package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func TestTeamMonthlyBehaviorSparseModesAndAuthority(t *testing.T) {
	money := "10.000000000000000001"
	before, _ := limits.Normalize(limits.Policy{TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only", TokensMonth: limitNumber(10), MoneyMonth: &money, Currency: "USD", RPM: limitNumber(4)})
	for _, body := range []string{`{"tokens_month":0}`, `{"money_month":null}`, `{"rpm":3}`} {
		next, err := applyTeamLimitInput(before, teamLimitInput(t, body), "team", teamResourceLimitFields, "USD")
		if err != nil || next.TokensMonthBehavior != before.TokensMonthBehavior || next.MoneyMonthBehavior != before.MoneyMonthBehavior {
			t.Fatal("omitted mode changed sparse policy", body, err, next)
		}
	}
	input := teamLimitInput(t, `{"tokens_month_behavior":"stop"}`)
	next, err := applyTeamLimitInput(before, input, "team", []string{"tokens_month_behavior"}, "USD")
	if err != nil || next.TokensMonthBehavior != "" || next.MoneyMonthBehavior != "alert_only" || *next.MoneyMonth != money || *next.RPM != 4 {
		t.Fatal("mode-only patch lost independent facts", err, next)
	}
	if _, err = applyTeamLimitInput(before, input, "team", []string{"money_month_behavior"}, "USD"); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("money writer acquired Token behavior", err)
	}
	if _, err = applyTeamLimitInput(limits.Policy{}, input, "team_member", teamMemberLimitFields, "USD"); !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatal("child accepted mode", err)
	}
	for _, body := range []string{`{"tokens_month_behavior":null}`, `{"tokens_month_behavior":""}`, `{"tokens_month_behavior":"STOP"}`, `{"money_month_behavior":"alert_only "}`, `{"money_month_behavior":1}`, `{"money_month_behavior":"stop","money_month_behavior":"alert_only"}`} {
		var input TeamLimitInput
		err := json.Unmarshal([]byte(body), &input)
		if err == nil {
			_, err = applyTeamLimitInput(before, input, "team", teamResourceLimitFields, "USD")
		}
		if err == nil {
			t.Fatal("invalid mode accepted", body)
		}
	}
	if teamLimitPermission("tokens_month_behavior") != "teams.tokens.write" || teamLimitPermission("money_month_behavior") != "teams.money.write" {
		t.Fatal("mode permission grouped incorrectly")
	}
	for _, kind := range []string{"team_member", "project", "key", "Team", "team "} {
		if _, err := policyFromRow(entity.ResourceLimit{ScopeKind: kind, TokensMonthBehavior: "alert_only"}); err == nil {
			t.Fatal("foreign soft policy admitted", kind)
		}
	}
	if _, err := policyFromRow(entity.ResourceLimit{ScopeKind: "team", TokensMonthBehavior: "alert_only"}); err != nil {
		t.Fatal(err)
	}
}

func TestTeamMonthlyBehaviorRuntimeHardChildAndIndependentGates(t *testing.T) {
	for _, test := range []struct {
		name          string
		parent, child limits.Policy
		admits        bool
	}{
		{"soft_aggregate", limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only"}, limits.Policy{TokensMonth: limitNumber(10)}, true},
		{"hard_child", limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only"}, limits.Policy{TokensMonth: limitNumber(0)}, false},
		{"hard_aggregate", limits.Policy{TokensMonth: limitNumber(0)}, limits.Policy{TokensMonth: limitNumber(10)}, false},
		{"hard_rolling", limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", Tokens5H: limitNumber(0)}, limits.Policy{}, false},
		{"hard_rate", limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", RPM: limitNumber(0)}, limits.Policy{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, result, queue := teamFiniteAdmissionFixture(t, test.parent, test.child)
			err := svc.admitLimitedGatewayCall(context.Background(), "req_team_modes", result)
			if (err == nil) != test.admits {
				t.Fatal("independent account admission", err)
			}
			if err == nil {
				if len(result.admissionQuota) != 2 || result.admissionQuota[0].TokensMonthBehavior != test.parent.TokensMonthBehavior || result.admissionQuota[1].TokensMonthBehavior != "" {
					t.Fatal("captured mode chain changed")
				}
				if _, err = queue.CompleteQuota("req_team_modes", []byte("complete"), eventqueue.QuotaSettlement{Tokens: limitNumber(5)}, time.Now()); err != nil {
					t.Fatal(err)
				}
				for _, account := range []string{result.admissionQuota[0].Account, result.admissionQuota[1].Account} {
					usage, err := queue.AccountQuotaUsage(account, time.Now())
					if err != nil || usage.Month.TokensUsed != 5 || usage.Active.TokensHeld != 0 {
						t.Fatal("aggregate/member settlement", usage, err)
					}
				}
			}
			svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
			if _, err := svc.gatewayLimits(context.Background(), result); err == nil {
				t.Fatal("expired publication used", err)
			}
		})
	}
}

func TestTeamMonthlyBehaviorReviewRequestsAndAudit(t *testing.T) {
	before, _ := limits.Normalize(limits.Policy{TokensMonthBehavior: "alert_only", MoneyMonthBehavior: "alert_only", TokensMonth: limitNumber(10)})
	for _, dimension := range []string{"tokens", "money"} {
		value := "20"
		if dimension == "money" {
			value = "20.000000000000000001"
		}
		after, err := teamQuotaPatch(before, dimension, value, "USD")
		if err != nil || after.TokensMonthBehavior != before.TokensMonthBehavior || after.MoneyMonthBehavior != before.MoneyMonthBehavior {
			t.Fatal("request patch discarded modes", err)
		}
	}
	team := entity.Team{ID: "tea_review", Status: entity.ResourceActive, CreatedAt: time.Now().UTC()}
	row := entity.ResourceLimit{ETag: "same_revision"}
	base, _ := teamLimitReviewETag(team, nil, row, before, entity.ResourceLimit{}, limits.Policy{}, entity.PricingSetting{})
	changed := before
	changed.TokensMonthBehavior = ""
	next, _ := teamLimitReviewETag(team, nil, row, changed, entity.ResourceLimit{}, limits.Policy{}, entity.PricingSetting{})
	if base == next {
		t.Fatal("review validator ignored mode")
	}
	raw, _ := json.Marshal(map[string]any{"team_id": team.ID, "before": changed, "after": before, "reason": "review", "etag": "revision"})
	detail := string(raw)
	if len(auditRecord(entity.AuditEvent{Action: "limits.update", ResourceType: "team", ResourceID: team.ID, DetailsJSON: &detail}).Changes) == 0 {
		t.Fatal("Team typed audit lost modes")
	}
	if validateTeamLimitPolicy("team_member", before) == nil {
		t.Fatal("member soft policy validated")
	}
	defaults, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(100)})
	reset, err := defaultLimitResetPolicy(&defaultLimitResetState{Target: LimitTarget{Kind: "team"}, Stored: before, Policy: defaults})
	if err != nil || reset.TokensMonthBehavior != "" || reset.MoneyMonthBehavior != "" {
		t.Fatal("numeric default reset retained soft behavior", err, reset)
	}
	if !limits.Narrower(before, limits.Policy{TokensMonth: limitNumber(100)}) || limits.Narrower(changed, limits.Policy{TokensMonth: limitNumber(100)}) {
		t.Fatal("soft monthly parent confused hard narrowing")
	}
	if reflect.DeepEqual(before, changed) {
		t.Fatal("test failed to change mode")
	}
}
