package service

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
	"github.com/miclle/routex/pkg/routeattempt"
)

func gatewayAttemptFixture(t *testing.T) (*Service, *runtimeData) {
	t.Helper()
	svc, data, _ := runtimeFixture(t, "https://provider-one.example/v1")
	ciphertext, err := svc.secrets.Seal("crd_two", "test-upstream-secret-two")
	if err != nil {
		t.Fatal(err)
	}
	data.Providers = append(data.Providers, entity.Provider{ID: "prv_two", Name: "Provider Two", Enabled: true, ETag: "0", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	data.Connections = append(data.Connections, entity.ProviderConnection{TransportGeneration: "0", ETag: "0", Enabled: true, CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ID: "con_two", ProviderID: "prv_two", Name: "Secondary", BaseURL: "https://provider-two.example/v1", Protocol: entity.ProtocolOpenAIChat})
	data.ProviderModels = append(data.ProviderModels, entity.ProviderModel{CapabilityTransportGeneration: "0", ETag: "0", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ID: "pmd_two", ConnectionID: "con_two", UpstreamName: "provider-model-two"})
	data.Credentials = append(data.Credentials, entity.ProviderCredential{VerifiedTransportGeneration: "0", CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), ID: "crd_two", ConnectionID: "con_two", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified", Priority: 10})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_two", ProviderModelID: "pmd_two"})
	data.Bindings[0].Weight = 40
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_two", ModelID: "mdl_one", ProviderModelID: "pmd_two", Weight: 60})
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_attempts", Models: routes, PublishedAt: time.Now()})
	return svc, data
}

func TestGatewayAttemptPlanPreservesWeightsAndDetachedLookup(t *testing.T) {
	for draw, want := range map[int]string{39: "bnd_one", 40: "bnd_two", 99: "bnd_two"} {
		t.Run(want, func(t *testing.T) {
			svc, _ := gatewayAttemptFixture(t)
			plan, err := svc.gatewayAttemptPlanWithDraw("mdl_one", entity.ProtocolOpenAIChat, func(int) (int, error) { return draw, nil })
			if err != nil || len(plan.Candidates()) != 2 {
				t.Fatalf("plan candidates = %d, %v", len(plan.Candidates()), err)
			}
			result, err := plan.Plan.Run(context.Background(), routeattempt.Hooks{
				Eligible: func(ctx context.Context, attempt routeattempt.Attempt) (bool, error) {
					return svc.gatewayAttemptEligible(ctx, plan, attempt)
				},
				Prepare: func(context.Context, routeattempt.Attempt) error { return nil },
				Admit:   func(context.Context, routeattempt.Attempt) error { return nil },
				Execute: func(_ context.Context, attempt routeattempt.Attempt) (routeattempt.Outcome, error) {
					route, secret, ok := plan.Candidate(attempt)
					if !ok || secret == "" || route.SnapshotID != "cfg_attempts" {
						t.Fatal("detached candidate lookup failed")
					}
					return routeattempt.Outcome{Failure: routeattempt.Success, Work: routeattempt.Completed}, nil
				},
			})
			if err != nil || len(result.Attempts) != 1 || result.Attempts[0].Attempt.TargetID != want {
				t.Fatalf("selected %+v, want %s: %v", result.Attempts, want, err)
			}
		})
	}
}

func TestGatewayAttemptEligibilityRechecksRuntimeIdentityAndRevocation(t *testing.T) {
	svc, data := gatewayAttemptFixture(t)
	plan, err := svc.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	attempt := plan.Candidates()[0].attempt
	assertEligible := func(want bool) {
		t.Helper()
		got, checkErr := svc.gatewayAttemptEligible(context.Background(), plan, attempt)
		if checkErr != nil || got != want {
			t.Fatalf("eligible = %v, %v; want %v", got, checkErr, want)
		}
	}
	assertEligible(true)
	svc.runtime.deniedCredentials.Store(attempt.CredentialID, uint64(1))
	assertEligible(false)
	svc.runtime.deniedCredentials.Delete(attempt.CredentialID)
	svc.runtime.deniedProviderModels.Store(plan.Candidates()[0].route.ProviderModelID, uint64(1))
	assertEligible(false)
	svc.runtime.deniedProviderModels.Delete(plan.Candidates()[0].route.ProviderModelID)
	auth := buildRuntimeAuthorization(data, time.Now().Add(time.Minute))
	auth.CredentialAccess[attempt.CredentialID][plan.Candidates()[0].route.ProviderModelID] = false
	svc.runtime.auth.Store(auth)
	assertEligible(false)

	auth = buildRuntimeAuthorization(data, time.Now().Add(-time.Second))
	svc.runtime.auth.Store(auth)
	if _, checkErr := svc.gatewayAttemptEligible(context.Background(), plan, attempt); !errors.Is(checkErr, runtimeUnavailable) {
		t.Fatalf("expired lease error = %v", checkErr)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_replaced", Models: svc.runtime.routes.Load().Models})
	assertEligible(false)
	svc.runtime.routes.Store(&runtimeRoutes{ID: plan.snapshotID, Models: svc.runtime.routes.Load().Models})
	svc.egressGeneration.Add(1)
	assertEligible(false)
}

func TestGatewayAttemptAggregateQuotaBoundAndStableEvidence(t *testing.T) {
	svc, data := gatewayAttemptFixture(t)
	routes := svc.runtime.routes.Load()
	for index := range routes.Models["mdl_one"] {
		route := &routes.Models["mdl_one"][index].Route
		basis := testPriceBasis()
		basis.ETag = "price_" + route.ProviderModelID
		basis.Schedule.ProviderModelID = route.ProviderModelID
		if route.ProviderModelID == "pmd_two" {
			basis.Schedule.PriceID = "prc_two"
			for rateIndex := range basis.Schedule.Rates {
				basis.Schedule.Rates[rateIndex].Amount = "8"
			}
		}
		route.PriceBasis = basis
	}
	data.Quota.Bounds = map[string]entity.ReservationBound{
		"pmd_one": {ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 50, ETag: "cap_one"},
		"pmd_two": {ProviderModelID: "pmd_two", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 200, MaxOutputTokens: 50, ETag: "cap_two"},
	}
	data.Quota.Currency = "CNY"
	plan, err := svc.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	result := &GatewayResult{Protocol: entity.ProtocolOpenAIChat, quotaRequest: quotaRequest{Supported: true, MaxOutput: 10, CacheRead: true, CacheWrite: true}}
	policies := []eventqueue.QuotaLimit{{Tokens5H: limitNumber(1000), MoneyMonth: quotaTestString("100"), Currency: "CNY"}}
	filtered, bound, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), plan, result, policies, data.Quota)
	if err != nil || len(filtered.Candidates()) != 2 || bound.Tokens == nil || *bound.Tokens != 210 || bound.Money == nil || bound.Currency != "CNY" || len(bound.Revision) != 64 || len(bound.PriceRevision) != 64 || len(bound.BasisDigest) != 64 {
		t.Fatalf("aggregate bound = %+v, candidates %d, %v", bound, len(filtered.Candidates()), err)
	}
	second := filtered.Candidates()[1]
	quote, err := pricing.ReserveBound(second.route.PriceBasis.Schedule, second.route.PriceBasis.Currency, pricing.Capacity{Input: 200, Output: 10, CacheRead: true, CacheWrite: true})
	if err != nil || *bound.Money != quote.Amount {
		t.Fatalf("money bound = %v, want %v: %v", bound.Money, quote, err)
	}

	reversed := *plan
	reversed.candidates = slices.Clone(plan.candidates)
	slices.Reverse(reversed.candidates)
	_, again, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), &reversed, result, policies, data.Quota)
	if err != nil || again.Revision != bound.Revision || again.PriceRevision != bound.PriceRevision || again.BasisDigest != bound.BasisDigest || *again.Money != *bound.Money {
		t.Fatalf("candidate order changed aggregate evidence: %+v / %+v / %v", bound, again, err)
	}
	changedPrice := *plan
	changedPrice.candidates = slices.Clone(plan.candidates)
	changedPrice.candidates[1].route.PriceBasis = clonePriceBasis(changedPrice.candidates[1].route.PriceBasis)
	changedPrice.candidates[1].route.PriceBasis.ETag = "price_changed"
	changedPrice.candidates[1].route.PriceBasis.Schedule.Rates[0].Amount = "9"
	_, repriced, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), &changedPrice, result, policies, data.Quota)
	if err != nil || repriced.Revision != bound.Revision || repriced.PriceRevision == bound.PriceRevision || repriced.BasisDigest == bound.BasisDigest {
		t.Fatalf("price change did not preserve capacity evidence independence: %+v / %+v / %v", bound, repriced, err)
	}
}

func TestGatewayAttemptAggregateExcludesMissingEvidence(t *testing.T) {
	svc, data := gatewayAttemptFixture(t)
	data.Quota.Bounds = map[string]entity.ReservationBound{
		"pmd_one": {ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "cap_one"},
	}
	plan, err := svc.gatewayAttemptPlan("mdl_one", entity.ProtocolOpenAIChat)
	if err != nil {
		t.Fatal(err)
	}
	result := &GatewayResult{Protocol: entity.ProtocolOpenAIChat, quotaRequest: quotaRequest{Supported: true, MaxOutput: 10}}
	filtered, _, err := svc.prepareGatewayAttemptQuotaBound(context.Background(), plan, result, []eventqueue.QuotaLimit{{TPM: limitNumber(1000)}}, data.Quota)
	if err != nil || len(filtered.Candidates()) != 1 || filtered.Candidates()[0].route.ProviderModelID != "pmd_one" {
		t.Fatalf("missing capacity candidate retained: %+v, %v", filtered, err)
	}
	for _, candidate := range plan.Candidates() {
		if candidate.route.ProviderModelID == "pmd_two" {
			eligible, checkErr := svc.gatewayAttemptEligible(context.Background(), filtered, candidate.attempt)
			if checkErr != nil || eligible {
				t.Fatalf("aggregate allowlist accepted excluded candidate: %v, %v", eligible, checkErr)
			}
		}
	}
	delete(data.Quota.Bounds, "pmd_one")
	_, _, err = svc.prepareGatewayAttemptQuotaBound(context.Background(), plan, result, []eventqueue.QuotaLimit{{TPM: limitNumber(1000)}}, data.Quota)
	var gateway *GatewayError
	if !errors.As(err, &gateway) || gateway.Code != "quota_bound_unavailable" {
		t.Fatalf("missing all capacity evidence = %v", err)
	}
}
