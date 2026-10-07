package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"testing"
	"time"
)

func TestTeamMemberMonthlyMixedAdmissionSettlementAndProof(t *testing.T) {
	for _, tc := range []struct {
		name          string
		parent, child limits.Policy
		allowed       bool
	}{
		{"member_soft_parent_hard_capacity", limits.Policy{TokensMonth: limitNumber(10)}, limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only"}, true},
		{"hard_parent_zero", limits.Policy{TokensMonth: limitNumber(0)}, limits.Policy{TokensMonth: limitNumber(100), TokensMonthBehavior: "alert_only"}, false},
		{"soft_parent_hard_member", limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only"}, limits.Policy{TokensMonth: limitNumber(0)}, false},
		{"member_soft_hard_tpm", limits.Policy{}, limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", TPM: limitNumber(0)}, false},
		{"member_soft_hard_rpm", limits.Policy{}, limits.Policy{TokensMonth: limitNumber(0), TokensMonthBehavior: "alert_only", RPM: limitNumber(0)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, result, q := teamFiniteAdmissionFixture(t, tc.parent, tc.child)
			account := teamMemberLimitAccount(result.TeamID, result.UserID)
			auth := svc.runtime.auth.Load()
			auth.Quota.Created[account] = result.identity.team.teamCreatedAt
			err := svc.admitLimitedGatewayCall(context.Background(), "req_member_modes", result)
			if (err == nil) != tc.allowed {
				t.Fatal("account-specific monthly outcome", err)
			}
			if !tc.allowed {
				return
			}
			if len(result.admissionQuota) != 2 || !result.admissionQuota[1].TeamMember || result.admissionQuota[0].TeamMember {
				t.Fatal("member proof escaped its account", result.admissionQuota)
			}
			if _, err = q.CompleteQuota("req_member_modes", []byte("settled"), eventqueue.QuotaSettlement{Tokens: limitNumber(5)}, time.Now()); err != nil {
				t.Fatal(err)
			}
			for _, account := range []string{limitAccount("team", result.TeamID), account} {
				usage, err := q.AccountQuotaUsage(account, time.Now())
				if err != nil || usage.Month.TokensUsed != 5 || usage.Active.TokensHeld != 0 {
					t.Fatal("stable accounting lost", usage, err)
				}
			}
			baseline := *result
			for _, mutate := range []func(){
				func() { result.UserID = "usr_other" }, func() { result.TeamMembershipID = "tmb_other" }, func() { result.ProjectID = "prj_wrong" }, func() { result.KeyID = "key_wrong" }, func() { auth.Teams[result.TeamID] = runtimeTeam{} }, func() { delete(auth.Quota.Created, account) },
			} {
				*result = baseline
				old := auth.Teams[result.TeamID]
				auth.Quota.Created[account] = baseline.identity.team.teamCreatedAt
				mutate()
				if teamMemberQuotaProof(auth, result, account) {
					t.Fatal("wrong subject/birth acquired proof")
				}
				auth.Teams[baseline.TeamID] = old
			}
		})
	}
}

func TestTeamMemberMonthlyScopeAndRemovedPairRuntime(t *testing.T) {
	scope := teamMemberLimitScopeID("tea_one", "usr_one")
	row := entity.ResourceLimit{ScopeKind: "team_member", ScopeID: scope, TokensMonthBehavior: "alert_only"}
	if _, err := policyFromRow(row); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", scope + "A", scope[:51] + "B", "member_stable"} {
		row.ScopeID = bad
		if _, err := policyFromRow(row); err == nil {
			t.Fatal("invalid pair digest admitted", bad)
		}
	}
	created := time.Now().Add(-time.Hour)
	data := &runtimeData{Users: []entity.User{{ID: "usr_one"}}, TeamSessionData: &teamSessionRuntimeData{Teams: []entity.Team{{ID: "tea_one", Status: entity.ResourceActive, CreatedAt: created}}, Memberships: []entity.TeamMembership{{ID: "tmm_one", TeamID: "tea_one", UserID: "usr_one", Status: entity.ResourceActive, Role: entity.TeamMember}}}, Limits: []entity.ResourceLimit{{ScopeKind: "team_member", ScopeID: scope, TokensMonthBehavior: "alert_only"}}}
	if err := compileRuntimeLimits(data); err != nil || data.LimitPolicies["team_member_"+scope].TokensMonthBehavior != "alert_only" {
		t.Fatal("current pair did not publish", err)
	}
	data.TeamSessionData.Memberships[0].Status = entity.ResourceDisabled
	if err := compileRuntimeLimits(data); err != nil || len(data.LimitPolicies) != 0 {
		t.Fatal("removed pair policy remained live", err)
	}
	data.TeamSessionData.Memberships[0].Status = entity.ResourceActive
	data.TeamSessionData.Memberships[0].ID = "tmm_rejoined"
	if err := compileRuntimeLimits(data); err != nil || !runtimeTeamLimitAccounts(data)["team_member_"+scope].Equal(created) {
		t.Fatal("rejoin reset stable account birth", err)
	}
}
