package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/secret"
)

func TestTeamRuntimeLimitCompilationUsesExactCurrentPairs(t *testing.T) {
	created := time.Now().UTC().Add(-time.Hour)
	data := &runtimeData{
		Users: []entity.User{{ID: "usr_one"}, {ID: "usr_disabled", Disabled: true}},
		TeamSessionData: &teamSessionRuntimeData{
			Teams: []entity.Team{{ID: "tem_one", Status: entity.ResourceActive, CreatedAt: created}, {ID: "tem_inactive", Status: entity.ResourceDisabled}},
			Memberships: []entity.TeamMembership{
				{ID: "tmm_one", TeamID: "tem_one", UserID: "usr_one", Role: entity.TeamMember, Status: entity.ResourceActive},
				{ID: "tmm_alias", TeamID: "TEM_ONE", UserID: "usr_one", Role: entity.TeamMember, Status: entity.ResourceActive},
				{ID: "tmm_disabled", TeamID: "tem_one", UserID: "usr_disabled", Role: entity.TeamMember, Status: entity.ResourceActive},
			},
		},
		Limits: []entity.ResourceLimit{
			{ScopeKind: "team", ScopeID: "tem_one", Tokens5H: limitNumber(100), TokensMonth: limitNumber(200), RPM: limitNumber(10)},
			{ScopeKind: "team_member", ScopeID: strings.TrimPrefix(teamMemberLimitAccount("tem_one", "usr_one"), "team_member_"), TokensMonth: limitNumber(50), RPM: limitNumber(2)},
			{ScopeKind: "team_member", ScopeID: strings.TrimPrefix(teamMemberLimitAccount("tem_one", "usr_disabled"), "team_member_"), TokensMonth: limitNumber(0)},
			{ScopeKind: "team", ScopeID: "TEM_ONE", TokensMonth: limitNumber(0)},
		},
	}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	pair := teamMemberLimitAccount("tem_one", "usr_one")
	if len(data.LimitPolicies) != 2 || *data.LimitPolicies[pair].TokensMonth != 50 || *data.LimitPolicies["team_tem_one"].Tokens5H != 100 {
		t.Fatal("inactive or collated alias policy became eligible", data.LimitPolicies)
	}
	accounts := runtimeTeamLimitAccounts(data)
	if len(accounts) != 2 || !accounts[pair].Equal(created) || !accounts["team_tem_one"].Equal(created) {
		t.Fatal("pair coverage is not bound to immutable Team creation", accounts)
	}
	data.TeamSessionData.Memberships[0].ID = "tmm_rejoined"
	if !runtimeTeamLimitAccounts(data)[pair].Equal(created) {
		t.Fatal("rejoin resets quota account birth")
	}
	for _, row := range []entity.ResourceLimit{
		{ScopeKind: "team", ScopeID: "tem_one", IPMode: "allowlist", IPRangesJSON: `["192.0.2.0/24"]`},
		{ScopeKind: "team_member", ScopeID: strings.TrimPrefix(pair, "team_member_"), Tokens5H: limitNumber(1)},
		{ScopeKind: "team_member", ScopeID: strings.TrimPrefix(pair, "team_member_"), Tokens7D: limitNumber(1)},
	} {
		data.Limits = []entity.ResourceLimit{row}
		if err := compileRuntimeLimits(data); err == nil {
			t.Fatal("unsupported Team restriction was silently published", row)
		}
	}
}

func teamFiniteAdmissionFixture(t *testing.T, parent, child limits.Policy) (*Service, *GatewayResult, *eventqueue.Queue) {
	t.Helper()
	svc, identity := teamGatewayFixture(t, "https://provider-one.example/v1")
	auth := svc.runtime.auth.Load()
	parentAccount, childAccount := limitAccount("team", identity.TeamID), teamMemberLimitAccount(identity.TeamID, identity.UserID)
	auth.LimitPolicies[parentAccount], auth.LimitPolicies[childAccount] = parent, child
	auth.Quota.Revisions[parentAccount], auth.Quota.Revisions[childAccount] = "lim_parent", "lim_child"
	auth.Quota.Bounds["pmd_one"] = entity.ReservationBound{ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bnd_team"}
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "finite-team.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	if err := queue.EnableQuota("UTC", time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	svc.recorder = &callRecorder{queue: queue}
	result := (gatewayIdentity{team: identity}).result(entity.ProtocolOpenAIChat, "public-model", false)
	result.ModelID, result.ProviderModelID = "mdl_one", "pmd_one"
	result.quotaRequest = quotaRequest{Supported: true, MaxOutput: 1}
	return svc, result, queue
}

func TestTeamFiniteQuotaAndRatePoliciesAreAtomicAndSeparateFromPersonal(t *testing.T) {
	svc, result, queue := teamFiniteAdmissionFixture(t,
		limits.Policy{TokensMonth: limitNumber(10), RPM: limitNumber(10)},
		limits.Policy{TokensMonth: limitNumber(5), RPM: limitNumber(3)},
	)
	ctx := context.Background()
	if err := svc.admitLimitedGatewayCall(ctx, "req_team_finite", result); err != nil {
		t.Fatal(err)
	}
	if len(result.admissionQuota) != 2 || result.admissionQuota[0].Revision != "lim_parent" || result.admissionQuota[1].Revision != "lim_child" || !result.admissionQuota[1].CreatedAt.Equal(result.identity.team.teamCreatedAt) {
		t.Fatal("missing captured Team policy revision/coverage", result.admissionQuota)
	}
	for _, account := range []string{result.admissionQuota[0].Account, result.admissionQuota[1].Account} {
		usage, err := queue.AccountQuotaUsage(account, time.Now())
		if err != nil || usage.Active.TokensHeld != 5 {
			t.Fatal("aggregate/member hold not atomic", usage, err)
		}
	}
	if _, err := queue.CompleteQuota("req_team_finite", []byte("completed"), eventqueue.QuotaSettlement{Tokens: limitNumber(5)}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := svc.admitLimitedGatewayCall(ctx, "req_team_child_exhausted", result); err == nil {
		t.Fatal("child exhaustion did not block aggregate admission")
	}
	for _, account := range []string{result.admissionQuota[0].Account, result.admissionQuota[1].Account} {
		rpm, active, err := queue.AccountUsage(account, time.Now())
		usage, usageErr := queue.AccountQuotaUsage(account, time.Now())
		if err != nil || usageErr != nil || rpm != 1 || active != 0 || usage.Month.TokensUsed != 5 || usage.Active.TokensHeld != 0 {
			t.Fatal("rejected child partially debited Team", rpm, active, usage, err, usageErr)
		}
	}
	personal, err := queue.AccountQuotaUsage("user_usr_one", time.Now())
	if err != nil || personal.Month.TokensUsed != 0 || personal.Month.TokensUnknown != 0 {
		t.Fatal("Team inherited or debited Personal policy", personal, err)
	}
}

func TestTeamFiniteAdmissionUsesLeasedPoliciesAndRejectsUnknownUsage(t *testing.T) {
	for _, kind := range []string{"team", "team_member"} {
		t.Run(kind, func(t *testing.T) {
			svc, result, queue := teamFiniteAdmissionFixture(t, limits.Policy{TokensMonth: limitNumber(10)}, limits.Policy{TokensMonth: limitNumber(10)})
			scopeID := result.TeamID
			if kind == "team_member" {
				scopeID = strings.TrimPrefix(teamMemberLimitAccount(result.TeamID, result.UserID), "team_member_")
			}
			svc.denyLimitScope(kind, scopeID)
			if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_denied", result); !errors.Is(err, runtimeUnavailable) {
				t.Fatal("unpublished Team reduction admitted work", err)
			}
			if depth, _ := queue.Depth(); depth != 0 {
				t.Fatal("denied policy reserved journal work")
			}
		})
	}
	svc, result, queue := teamFiniteAdmissionFixture(t, limits.Policy{}, limits.Policy{})
	// Unknown means no proven bound; a bounded missing terminal usage retains
	// its conservative hold rather than being converted to an unknown count.
	result.quotaRequest = quotaRequest{}
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_unknown", result); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.CompleteQuota("req_team_unknown", []byte("unknown"), eventqueue.QuotaSettlement{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	auth := svc.runtime.auth.Load()
	auth.LimitPolicies["team_tem_one"] = limits.Policy{TokensMonth: limitNumber(10)}
	auth.Quota.Revisions["team_tem_one"] = "lim_after_unknown"
	result.quotaRequest = quotaRequest{Supported: true, MaxOutput: 1}
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_after_unknown", result); err == nil {
		t.Fatal("unknown Team usage became zero")
	}
	auth.ValidUntil = time.Now().Add(-time.Second)
	if _, err := svc.gatewayLimits(context.Background(), result); err == nil {
		t.Fatal("expired Team policy lease accepted")
	}
}

func TestTeamAggregateConcurrencyIncludesIndependentMembers(t *testing.T) {
	svc, first, queue := teamFiniteAdmissionFixture(t, limits.Policy{Concurrency: limitNumber(1)}, limits.Policy{})
	auth := svc.runtime.auth.Load()
	cookie := strings.Repeat("x", 43)
	auth.TeamSessions[secret.SHA256Hex(cookie)] = runtimeTeamSession{ID: "ses_second", UserID: "usr_two", TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour)}
	team := auth.Teams[first.TeamID]
	team.Members["usr_two"] = "tmm_second"
	secondIdentity, err := svc.RuntimeAuthenticateTeamSession(context.Background(), cookie, first.TeamID)
	if err != nil {
		t.Fatal(err)
	}
	second := (gatewayIdentity{team: secondIdentity}).result(entity.ProtocolOpenAIChat, "public-model", false)
	second.ModelID = first.ModelID
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_first", first); err != nil {
		t.Fatal(err)
	}
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_second", second); err == nil {
		t.Fatal("second member bypassed aggregate active lease")
	}
	rpm, active, err := queue.AccountUsage(teamMemberLimitAccount(first.TeamID, "usr_two"), time.Now())
	if err != nil || rpm != 0 || active != 0 {
		t.Fatal("rejected second member consumed own lease", rpm, active, err)
	}
}

func TestTeamBoundedMissingUsageRetainsBothConservativeHolds(t *testing.T) {
	svc, result, queue := teamFiniteAdmissionFixture(t, limits.Policy{TokensMonth: limitNumber(10)}, limits.Policy{TokensMonth: limitNumber(10)})
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_bounded_missing", result); err != nil {
		t.Fatal(err)
	}
	if _, err := queue.CompleteQuota("req_team_bounded_missing", []byte("bounded-missing"), eventqueue.QuotaSettlement{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, policy := range result.admissionQuota {
		usage, err := queue.AccountQuotaUsage(policy.Account, time.Now())
		if err != nil || usage.Month.TokensUsed != 0 || usage.Month.TokensHeld != 5 || usage.Month.TokensUnknown != 0 {
			t.Fatal("missing bounded usage released or invented economic evidence", usage, err)
		}
	}
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_remaining_bound", result); err != nil {
		t.Fatal(err)
	}
	if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_past_retained_bound", result); err == nil {
		t.Fatal("Team retained holds did not stop overspending")
	}
}

func TestTeamAggregateRollingWindowsRemainConjunctiveWithMemberMonthly(t *testing.T) {
	for _, window := range []string{"five_hours", "seven_days"} {
		t.Run(window, func(t *testing.T) {
			parent := limits.Policy{Tokens5H: limitNumber(5)}
			if window == "seven_days" {
				parent = limits.Policy{Tokens7D: limitNumber(5)}
			}
			svc, result, queue := teamFiniteAdmissionFixture(t, parent, limits.Policy{TokensMonth: limitNumber(100)})
			if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_rolling", result); err != nil {
				t.Fatal(err)
			}
			if _, err := queue.CompleteQuota("req_team_rolling", []byte("rolling"), eventqueue.QuotaSettlement{Tokens: limitNumber(5)}, time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_rolling_exhausted", result); err == nil {
				t.Fatal("member allowance bypassed Team rolling exhaustion")
			}
		})
	}
}

func TestTeamFiniteAdmissionRequiresCapacityAndOriginalCoverage(t *testing.T) {
	for _, failure := range []string{"shape", "capacity", "coverage"} {
		t.Run(failure, func(t *testing.T) {
			svc, result, queue := teamFiniteAdmissionFixture(t, limits.Policy{TokensMonth: limitNumber(10)}, limits.Policy{})
			switch failure {
			case "shape":
				result.quotaRequest = quotaRequest{}
			case "capacity":
				delete(svc.runtime.auth.Load().Quota.Bounds, "pmd_one")
			case "coverage":
				// An old Team cannot acquire complete monthly history by adding
				// a member or enabling a policy after journal activation.
				result.identity.team.teamCreatedAt = time.Now().Add(-30 * 24 * time.Hour)
			}
			if err := svc.admitLimitedGatewayCall(context.Background(), "req_team_unproven", result); err == nil {
				t.Fatal("unproven Team reservation admitted", failure)
			}
			if depth, _ := queue.Depth(); depth != 0 {
				t.Fatal("unproven Team reservation created journal work")
			}
			for _, account := range []string{limitAccount("team", result.TeamID), teamMemberLimitAccount(result.TeamID, result.UserID)} {
				rpm, active, err := queue.AccountUsage(account, time.Now())
				if err != nil || rpm != 0 || active != 0 {
					t.Fatal("failed capacity/history check partially admitted", rpm, active, err)
				}
			}
		})
	}
}
