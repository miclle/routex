package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
)

func teamLimitInput(t *testing.T, body string) TeamLimitInput {
	t.Helper()
	var input TeamLimitInput
	if err := json.Unmarshal([]byte(body), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestTeamLimitSparseIntentPreservesUntouchedDimensions(t *testing.T) {
	money := "12.000000000000000001"
	before, err := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10), RPM: limitNumber(8), MoneyMonth: &money, Currency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	input := teamLimitInput(t, `{"tokens_month":0,"reason":"Reviewed zero cap"}`)
	result, err := applyTeamLimitInput(before, input, "team_member", []string{"tokens_month"}, "USD")
	if err != nil || result.TokensMonth == nil || *result.TokensMonth != 0 || *result.MoneyMonth != money || *result.RPM != 8 || *before.TokensMonth != 10 {
		t.Fatal("sparse patch replaced untouched policy", err)
	}
	input = teamLimitInput(t, `{"tokens_month":null,"reason":"Inherit parent"}`)
	result, err = applyTeamLimitInput(result, input, "team_member", []string{"tokens_month"}, "USD")
	if err != nil || result.TokensMonth != nil || result.MoneyMonth == nil {
		t.Fatal("null lost independent inheritance", err)
	}
	input = teamLimitInput(t, `{"money_month":null,"reason":"Clear local money"}`)
	result, err = applyTeamLimitInput(result, input, "team_member", []string{"money_month"}, "USD")
	if err != nil || result.MoneyMonth != nil || result.Currency != "" {
		t.Fatal("clear retained denomination", err)
	}
	input = teamLimitInput(t, `{"money_month":"0.000000000000000001","currency":"USD","reason":"Exact amount"}`)
	result, err = applyTeamLimitInput(before, input, "team", []string{"money_month"}, "USD")
	if err != nil || *result.MoneyMonth != "0.000000000000000001" || *before.MoneyMonth != money || result.MoneyMonth == before.MoneyMonth {
		t.Fatal("money precision lost", err)
	}
}

func TestTeamLimitStrictShapeAndDimensionAuthority(t *testing.T) {
	for _, body := range []string{`null`, `[]`, `{"tokens_month":1,"tokens_month":2}`, `{"ip_mode":"none"}`, `{"reason":123}`, `{"unknown":0}`} {
		var input TeamLimitInput
		if err := json.Unmarshal([]byte(body), &input); err == nil {
			t.Fatalf("accepted strict body %s", body)
		}
	}
	before, _ := limits.Normalize(limits.Policy{})
	for _, body := range []string{`{"tokens_month":"1"}`, `{"tokens_month":1.1}`, `{"tokens_month":-1}`, `{"tokens_month":9007199254740992}`, `{"money_month":1,"currency":"USD"}`, `{"money_month":"2"}`, `{"money_month":null,"currency":"USD"}`, `{"currency":"USD"}`, `{"money_month":"2","currency":"EUR"}`, `{"tokens_5h":1}`} {
		input := teamLimitInput(t, body)
		if _, err := applyTeamLimitInput(before, input, "team_member", teamMemberLimitFields, "USD"); err == nil {
			t.Fatalf("accepted unsupported patch %s", body)
		}
	}
	input := teamLimitInput(t, `{"rpm":0}`)
	if _, err := applyTeamLimitInput(before, input, "team", []string{"tokens_month"}, "USD"); !errors.Is(err, apperrors.ErrForbidden) {
		t.Fatal("token writer acquired rate mutation", err)
	}
	input = teamLimitInput(t, `{"money_month":null,"currency":null}`)
	if _, err := applyTeamLimitInput(before, input, "team", teamLimitFields, "USD"); err == nil {
		t.Fatal("null currency silently discarded")
	}
	if validateTeamLimitPolicy("team", limits.Policy{IPMode: "allowlist", IPRanges: []string{"192.0.2.0/24"}}) == nil {
		t.Fatal("Team IP accepted")
	}
}

func TestTeamLimitCompositeReviewBindsParentCurrencyMembershipAndLifecycle(t *testing.T) {
	stamp := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	team := entity.Team{ID: "tea_review", Status: entity.ResourceActive, CreatedAt: stamp, UpdatedAt: stamp}
	member := entity.TeamMembership{ID: "tmm_review", TeamID: team.ID, UserID: "usr_review", Role: entity.TeamMember, Status: entity.ResourceActive}
	row := entity.ResourceLimit{ETag: "lim_member"}
	parentRow := entity.ResourceLimit{ETag: "lim_parent"}
	policy, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(5)})
	parent, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10)})
	pricing := entity.PricingSetting{ETag: "pricing_one", PlatformCurrency: "USD"}
	hash, err := teamLimitReviewETag(team, &member, row, policy, parentRow, parent, pricing)
	if err != nil || len(hash) != 64 {
		t.Fatal(err)
	}
	check := func(changed string) {
		t.Helper()
		if hash == changed {
			t.Fatal("review did not bind changed authoritative input")
		}
	}
	changedTeam := team
	changedTeam.Status = entity.ResourceDisabled
	next, _ := teamLimitReviewETag(changedTeam, &member, row, policy, parentRow, parent, pricing)
	check(next)
	changedTeam.Status = entity.ResourceActive
	changedTeam.UpdatedAt = stamp.Add(time.Millisecond)
	next, _ = teamLimitReviewETag(changedTeam, &member, row, policy, parentRow, parent, pricing)
	check(next)
	changedMember := member
	changedMember.ID = "tmm_rejoined"
	next, _ = teamLimitReviewETag(team, &changedMember, row, policy, parentRow, parent, pricing)
	check(next)
	changedParent := parent
	changedParent.TokensMonth = limitNumber(9)
	next, _ = teamLimitReviewETag(team, &member, row, policy, parentRow, changedParent, pricing)
	check(next)
	pricing.ETag = "pricing_two"
	pricing.PlatformCurrency = "EUR"
	next, _ = teamLimitReviewETag(team, &member, row, policy, parentRow, parent, pricing)
	check(next)
	// Policies and historical pair accounting remain distinct from live membership.
	if teamMemberLimitScopeID(team.ID, member.UserID) != teamMemberLimitScopeID(team.ID, changedMember.UserID) || len(teamMemberLimitScopeID(team.ID, member.UserID)) != 52 {
		t.Fatal("membership generation changed quota identity")
	}
}

func TestTeamLimitWireAndLegacyRecordCompatibility(t *testing.T) {
	raw, err := json.Marshal(LimitRecord{Kind: "team", ID: "tea_one", TeamID: "tea_one"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"editable_fields":[]`) {
		t.Fatal("read-only authority omitted", string(raw))
	}
	raw, err = json.Marshal(LimitRecord{Kind: "user", ID: "usr_one"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "editable_fields") || strings.Contains(string(raw), "team_id") {
		t.Fatal("legacy record wire changed", string(raw))
	}
	parent := limits.Policy{TokensMonth: limitNumber(4)}
	child := limits.Policy{TokensMonth: limitNumber(8)}
	if limits.Narrower(parent, child) || !reflect.DeepEqual(effectiveQuotaValues(child, parent).TokensMonth, parent.TokensMonth) {
		t.Fatal("parent reduction not resolved conservatively")
	}
}

func TestTeamLimitAppliedRequiresExactCurrentPublication(t *testing.T) {
	policy, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(5)})
	parent, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(10)})
	team := entity.Team{ID: "tea_proof", Status: entity.ResourceActive}
	member := entity.TeamMembership{ID: "tmm_proof", TeamID: team.ID, UserID: "usr_proof", Role: entity.TeamMember, Status: entity.ResourceActive}
	scope := teamMemberLimitScopeID(team.ID, member.UserID)
	account := limitAccount("team_member", scope)
	parentAccount := limitAccount("team", team.ID)
	current := &teamLimitContext{Team: team, Member: &member, Resolved: resolvedLimitTarget{kind: "team_member", id: scope}, Row: entity.ResourceLimit{ETag: "child_revision"}, Stored: policy, ParentRow: entity.ResourceLimit{ETag: "parent_revision"}, Parent: parent, Pricing: entity.PricingSetting{PlatformCurrency: "USD"}}
	svc := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Minute), Teams: map[string]runtimeTeam{team.ID: {Members: map[string]string{member.UserID: member.ID}}}, Quota: &runtimeQuotaData{Revisions: map[string]string{account: current.Row.ETag, parentAccount: current.ParentRow.ETag}, Currency: "USD"}, LimitPolicies: map[string]limits.Policy{account: policy, parentAccount: parent}}
	svc.runtime.auth.Store(auth)
	if !svc.teamLimitApplied(current) {
		t.Fatal("exact current publication was not confirmed")
	}
	deny := func() {
		t.Helper()
		if svc.teamLimitApplied(current) {
			t.Fatal("saved policy incorrectly claimed current enforcement")
		}
	}
	auth.Quota.Revisions[account] = "older_revision"
	deny()
	auth.Quota.Revisions[account] = current.Row.ETag
	auth.LimitPolicies[parentAccount] = limits.Policy{TokensMonth: limitNumber(9)}
	deny()
	auth.LimitPolicies[parentAccount] = parent
	auth.Teams[team.ID].Members[member.UserID] = "tmm_rejoined"
	deny()
	auth.Teams[team.ID].Members[member.UserID] = member.ID
	svc.runtime.deniedTeamMembers.Store(teamMemberRuntimeKey(team.ID, member.UserID), true)
	deny()
	svc.runtime.deniedTeamMembers.Delete(teamMemberRuntimeKey(team.ID, member.UserID))
	auth.ValidUntil = time.Now().Add(-time.Second)
	deny()
	auth.ValidUntil = time.Now().Add(time.Minute)
	money := "1"
	current.Stored.MoneyMonth, current.Stored.Currency = &money, "USD"
	auth.LimitPolicies[account] = current.Stored
	auth.Quota.Currency = "EUR"
	deny()
	auth.Quota.Currency = "USD"
	current.Stored.Currency = "EUR"
	auth.LimitPolicies[account] = current.Stored
	deny()
	current.Stored.Currency = "USD"
	auth.LimitPolicies[account] = current.Stored
	svc.runtime.deniedLimits.Store("quota_settings", true)
	deny()
	svc.runtime.deniedLimits.Delete("quota_settings")
	svc.recorder = nil
	deny()
}
