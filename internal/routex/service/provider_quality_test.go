package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestProviderQualityP95UsesNearestRank(t *testing.T) {
	for _, test := range []struct {
		count int64
		want  int
	}{{1, 0}, {2, 1}, {19, 18}, {20, 18}, {21, 19}, {100, 94}} {
		if got := providerQualityP95Offset(test.count); got != test.want {
			t.Fatalf("P95 offset for %d = %d, want %d", test.count, got, test.want)
		}
	}
}

func TestProviderQualitySummaryWindowUsesConfiguredOrDefaultPolicy(t *testing.T) {
	if got, ok := qualitySummaryWindowMinutes(entity.ProviderQualityPolicy{}); !ok || got != defaultQualityWindowMinutes {
		t.Fatalf("default summary window = %d, valid=%v", got, ok)
	}
	if got, ok := qualitySummaryWindowMinutes(entity.ProviderQualityPolicy{ProviderID: "prv_one", WindowMinutes: 15}); !ok || got != 15 {
		t.Fatalf("configured summary window = %d, valid=%v", got, ok)
	}
	if _, ok := qualitySummaryWindowMinutes(entity.ProviderQualityPolicy{ProviderID: "prv_one", WindowMinutes: 1441}); ok {
		t.Fatal("out-of-bounds persisted policy was accepted for a summary")
	}
}

func TestProviderQualityEvaluationContinuesAfterProviderFailure(t *testing.T) {
	policies := []entity.ProviderQualityPolicy{{ProviderID: "prv_overflow"}, {ProviderID: "prv_later"}}
	visited := []string{}
	failed, first := evaluateProviderQualityPolicies(context.Background(), policies, time.Now(), func(_ context.Context, policy entity.ProviderQualityPolicy, _ time.Time) error {
		visited = append(visited, policy.ProviderID)
		if policy.ProviderID == "prv_overflow" {
			return providerQualityTooLarge
		}
		return nil
	})
	if failed != 1 || !errors.Is(first, providerQualityTooLarge) {
		t.Fatalf("evaluation failures = %d/%v", failed, first)
	}
	if len(visited) != 2 || visited[1] != "prv_later" {
		t.Fatalf("provider failure starved later policies: %v", visited)
	}
}

func TestAdminOverviewKeepsUnrelatedProviderWhenOneQualityRangeOverflows(t *testing.T) {
	items := []AdminProviderReadinessItem{{ProviderID: "prv_overflow"}, {ProviderID: "prv_healthy"}}
	if err := assignOverviewProviderQuality(&items[0], nil, providerQualityTooLarge); err != nil {
		t.Fatal(err)
	}
	healthy := &ProviderQuality{ProviderID: "prv_healthy", Status: "healthy"}
	if err := assignOverviewProviderQuality(&items[1], healthy, nil); err != nil {
		t.Fatal(err)
	}
	if items[0].Quality != nil || items[1].Quality != healthy {
		t.Fatalf("overview quality isolation failed: %+v", items)
	}
	if items[0].QualityUnavailableReason != providerQualityUnavailableRangeTooLarge || items[1].QualityUnavailableReason != "" {
		t.Fatalf("overview quality availability reasons = %+v", items)
	}
}

func TestAdminOverviewProviderQualityQueryBudgetIsBounded(t *testing.T) {
	if !overviewProviderQualityInQueryBudget(adminOverviewProviderQualityQueryLimit - 1) {
		t.Fatal("last provider inside the quality query budget was skipped")
	}
	if overviewProviderQualityInQueryBudget(adminOverviewProviderQualityQueryLimit) || overviewProviderQualityInQueryBudget(-1) {
		t.Fatal("provider quality query budget exceeded its explicit bound")
	}
	if got := overviewProviderQualityUnavailableReason(true, adminOverviewProviderQualityQueryLimit); got != providerQualityUnavailableQueryBudget {
		t.Fatalf("query-budget reason = %q", got)
	}
	if got := overviewProviderQualityUnavailableReason(false, 0); got != providerQualityUnavailableInvalidPolicy {
		t.Fatalf("invalid-policy reason = %q", got)
	}
	if got := overviewProviderQualityUnavailableReason(true, 0); got != "" {
		t.Fatalf("available provider received reason %q", got)
	}
}

func TestProviderQualityState(t *testing.T) {
	maxDuration := int64(500)
	policy := entity.ProviderQualityPolicy{ProviderID: "prv_one", Enabled: true, MinimumAttempts: 2, MinSuccessRateBPS: 9000, MaxP95DurationMS: &maxDuration}
	rate := 9500
	p95 := int64(400)
	quality := &ProviderQuality{EligibleAttempts: 2, SuccessRateBPS: &rate, P95DurationMS: &p95}
	if state, detail := qualityState(quality, policy); state != "healthy" || detail != "" {
		t.Fatalf("healthy state = %q/%q", state, detail)
	}
	quality.EligibleAttempts = 1
	if state, _ := qualityState(quality, policy); state != "insufficient_data" {
		t.Fatalf("insufficient state = %q", state)
	}
	quality.EligibleAttempts, rate, p95 = 2, 8000, 600
	if state, detail := qualityState(quality, policy); state != "degraded" || detail != "multiple_thresholds_breached" {
		t.Fatalf("degraded state = %q/%q", state, detail)
	}
	policy.Enabled = false
	if state, _ := qualityState(quality, policy); state != "unconfigured" {
		t.Fatalf("disabled state = %q", state)
	}
}

func TestProviderQualityPolicyValidation(t *testing.T) {
	maxDuration := int64(1000)
	valid := ProviderQualityPolicyInput{Enabled: true, WindowMinutes: 60, MinimumAttempts: 20, MinSuccessRateBPS: 9500, MaxP95DurationMS: &maxDuration, ETag: "0", Reason: "Enable production thresholds"}
	if !validQualityPolicyInput(valid) {
		t.Fatal("valid quality policy rejected")
	}
	for _, mutate := range []func(*ProviderQualityPolicyInput){
		func(v *ProviderQualityPolicyInput) { v.WindowMinutes = 4 },
		func(v *ProviderQualityPolicyInput) { v.MinimumAttempts = 0 },
		func(v *ProviderQualityPolicyInput) { v.MinSuccessRateBPS = 10001 },
		func(v *ProviderQualityPolicyInput) { invalid := int64(0); v.MaxP95DurationMS = &invalid },
		func(v *ProviderQualityPolicyInput) { v.ETag = "" },
		func(v *ProviderQualityPolicyInput) { v.Reason = " padded " },
	} {
		candidate := valid
		mutate(&candidate)
		if validQualityPolicyInput(candidate) {
			t.Fatalf("invalid policy accepted: %+v", candidate)
		}
	}
}
