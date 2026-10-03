package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

// Comparison lanes have independent admission, but native protocol choice must
// not split either the Team account or its stable Team/User accounting pair.
func TestTeamComparisonMixedConcurrentAdmissionAndIndependentSettlement(t *testing.T) {
	protocols := []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}
	for _, maximum := range []int64{2, 4} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) {
			svc, original, queue := teamFiniteAdmissionFixture(t,
				limits.Policy{TokensMonth: limitNumber(100), Concurrency: &maximum},
				limits.Policy{TokensMonth: limitNumber(100), Concurrency: limitNumber(4)},
			)
			ctx := context.Background()
			identity := original.identity.team
			accounts := []string{limitAccount("team", identity.TeamID), teamMemberLimitAccount(identity.TeamID, identity.UserID)}
			results := make([]*GatewayResult, len(protocols))
			for i, protocol := range protocols {
				providerModelID := "pmd_comparison_" + fmt.Sprint(i)
				svc.runtime.auth.Load().Quota.Bounds[providerModelID] = entity.ReservationBound{ProviderModelID: providerModelID, Protocol: protocol, MaxInputTokens: 4, MaxOutputTokens: 1, ETag: "bound_comparison"}
				result := (gatewayIdentity{team: identity}).result(protocol, "public-model", true)
				result.ModelID, result.ProviderModelID = original.ModelID, providerModelID
				result.quotaRequest = quotaRequest{Supported: true, MaxOutput: 1}
				results[i] = result
			}
			type admission struct {
				index int
				err   error
			}
			ready, completed := make(chan struct{}), make(chan admission, len(protocols))
			for i, result := range results {
				go func() {
					<-ready
					completed <- admission{i, svc.admitLimitedGatewayCall(ctx, fmt.Sprintf("req_compare_%d", i), result)}
				}()
			}
			close(ready)
			var accepted []int
			for range protocols {
				outcome := <-completed
				if outcome.err == nil {
					accepted = append(accepted, outcome.index)
				} else {
					var public *GatewayError
					if !errors.As(outcome.err, &public) || public.Status != 429 || public.Code != "concurrency_limit_exceeded" {
						t.Fatal("mixed lane was denied for something other than shared concurrency", outcome.index, outcome.err)
					}
				}
			}
			if int64(len(accepted)) != maximum {
				t.Fatal("protocol concurrency split or partially rejected group", accepted)
			}
			assertAccounts := func(active, used, held int64) {
				t.Helper()
				for _, account := range accounts {
					rpm, live, err := queue.AccountUsage(account, time.Now())
					usage, usageErr := queue.AccountQuotaUsage(account, time.Now())
					if err != nil || usageErr != nil || rpm != maximum || live != active || usage.Month.TokensUsed != used || usage.Month.TokensHeld != held-active*5 || usage.Active.TokensHeld != active*5 || usage.Month.TokensUnknown != 0 {
						t.Fatal("mixed protocol admission or settlement partly debited account", account, rpm, live, usage, err, usageErr)
					}
				}
			}
			assertAccounts(maximum, 0, maximum*5)
			for _, i := range accepted {
				result := results[i]
				if result.UserID != identity.UserID || result.TeamID != identity.TeamID || result.TeamMembershipID != identity.TeamMembershipID || result.KeyID != "" || result.ProjectID != "" || len(result.admissionQuota) != 2 || result.admissionQuota[0].Account != accounts[0] || result.admissionQuota[1].Account != accounts[1] {
					t.Fatal("native lane changed captured authority or accounting", result)
				}
			}
			// Cancel/incomplete numeric usage retains one bound; completing a
			// different lane releases only that lane's reservation.
			for position, i := range accepted {
				settlement := eventqueue.QuotaSettlement{}
				if position%2 == 0 {
					settlement.Tokens = limitNumber(3)
				}
				if _, err := queue.CompleteQuota(fmt.Sprintf("req_compare_%d", i), []byte(fmt.Sprint(i)), settlement, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			assertAccounts(0, maximum/2*3, maximum/2*5)
			personal, err := queue.AccountQuotaUsage(limitAccount("user", identity.UserID), time.Now())
			if err != nil || personal.Month.TokensUsed != 0 || personal.Month.TokensHeld != 0 || personal.Month.TokensUnknown != 0 {
				t.Fatal("mixed Team lanes borrowed Personal accounting", personal, err)
			}
			// The old capture cannot admit after rejoin, while the new generation
			// uses the same account with every prior used/held token retained.
			team := svc.runtime.auth.Load().Teams[identity.TeamID]
			team.Members[identity.UserID] = "tmm_comparison_rejoined"
			if err := svc.admitLimitedGatewayCall(ctx, "req_compare_old_generation", results[0]); err == nil {
				t.Fatal("old comparison membership survived replacement")
			}
			assertAccounts(0, maximum/2*3, maximum/2*5)
			fresh := *identity
			fresh.TeamMembershipID = "tmm_comparison_rejoined"
			result := (gatewayIdentity{team: &fresh}).result(protocols[0], "public-model", true)
			result.ModelID, result.ProviderModelID, result.quotaRequest = results[0].ModelID, results[0].ProviderModelID, results[0].quotaRequest
			if err := svc.admitLimitedGatewayCall(ctx, "req_compare_new_generation", result); err != nil || result.admissionQuota[1].Account != accounts[1] {
				t.Fatal("rejoin changed stable pair account", result, err)
			}
			for _, account := range accounts {
				usage, err := queue.AccountQuotaUsage(account, time.Now())
				if err != nil || usage.Month.TokensUsed != maximum/2*3 || usage.Month.TokensHeld != maximum/2*5 || usage.Active.TokensHeld != 5 {
					t.Fatal("fresh membership reset original usage or conservative holds", usage, err)
				}
			}
		})
	}
}
