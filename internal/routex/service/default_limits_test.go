package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

const fullDefaultLimitBody = `{"policy":{"tokens_5h":null,"tokens_7d":null,"tokens_month":0,"tpm":null,"money_month":null,"currency":"","rpm":null,"concurrency":null},"reason":"Reviewed creation defaults"}`

func TestDefaultLimitStrictCompletePolicy(t *testing.T) {
	var input DefaultLimitInput
	if err := json.Unmarshal([]byte(fullDefaultLimitBody), &input); err != nil {
		t.Fatal(err)
	}
	policy, err := defaultLimitPolicy(input.Policy)
	if err != nil || policy.TokensMonth == nil || *policy.TokensMonth != 0 || policy.Tokens5H != nil || policy.MoneyMonth != nil {
		t.Fatal("zero or unlimited changed", err)
	}
	for _, body := range []string{
		`null`, `[]`, `{"policy":null,"reason":"review"}`, strings.Replace(fullDefaultLimitBody, `"tokens_5h":null,`, "", 1),
		strings.Replace(fullDefaultLimitBody, `"tokens_month":0`, `"tokens_month":0,"tokens_month":1`, 1),
		strings.Replace(fullDefaultLimitBody, `"reason":"Reviewed creation defaults"`, `"reason":null`, 1),
		strings.Replace(fullDefaultLimitBody, `"currency":""`, `"currency":null`, 1),
		strings.Replace(fullDefaultLimitBody, `"tokens_month":0`, `"tokens_month":0.1`, 1),
		strings.Replace(fullDefaultLimitBody, `"money_month":null`, `"money_month":1`, 1),
		strings.Replace(fullDefaultLimitBody, `"policy":`, `"ip_mode":"none","policy":`, 1),
		fullDefaultLimitBody + ` {}`, strings.Replace(fullDefaultLimitBody, `"reason":`, `"policy":{},"reason":`, 1),
	} {
		if err := json.Unmarshal([]byte(body), &input); err == nil {
			t.Fatalf("accepted malformed full policy: %s", body)
		}
	}
	for _, value := range []int64{-1, limits.MaxInteger + 1} {
		if _, err := defaultLimitPolicy(EffectiveLimitValues{TokensMonth: &value}); err == nil {
			t.Fatal("unsafe numeric bound accepted", value)
		}
	}
	if _, err := defaultLimitPolicy(EffectiveLimitValues{Currency: "USD"}); err == nil {
		t.Fatal("unlimited money retained denomination")
	}
	money := "0.000000000000000001"
	policy, err = defaultLimitPolicy(EffectiveLimitValues{MoneyMonth: &money, Currency: "USD"})
	if err != nil || *policy.MoneyMonth != money || policy.IPMode != "none" || len(policy.IPRanges) != 0 {
		t.Fatal("exact money or supported policy changed", err)
	}
}

func TestDefaultLimitRuleReviewBindsGenerationAndDenomination(t *testing.T) {
	policy, _ := limits.Normalize(limits.Policy{})
	row := entity.DefaultLimitRule{Kind: "user", RuleETag: strings.Repeat("a", 64)}
	pricing := entity.PricingSetting{ETag: "pricing_one", PlatformCurrency: "USD"}
	original, err := defaultLimitReviewETag(row, policy, pricing)
	if err != nil || !teamSessionDigest.MatchString(original) {
		t.Fatal(err)
	}
	checks := []struct {
		row     entity.DefaultLimitRule
		policy  limits.Policy
		pricing entity.PricingSetting
	}{
		{entity.DefaultLimitRule{Kind: "team", RuleETag: row.RuleETag}, policy, pricing},
		{entity.DefaultLimitRule{Kind: "user", RuleETag: strings.Repeat("b", 64)}, policy, pricing},
		{row, limits.Policy{TokensMonth: limitNumber(0)}, pricing},
		{row, policy, entity.PricingSetting{ETag: "pricing_two", PlatformCurrency: "USD"}},
		{row, policy, entity.PricingSetting{ETag: pricing.ETag, PlatformCurrency: "EUR"}},
	}
	for _, test := range checks {
		changed, err := defaultLimitReviewETag(test.row, test.policy, test.pricing)
		if err != nil || changed == original {
			t.Fatal("default review lost changed dependency", err)
		}
	}
}

func TestDefaultLimitResetCopiesCapsPreservesIPAndDoesNotMutateSource(t *testing.T) {
	before, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(2), IPMode: "allowlist", IPRanges: []string{"192.0.2.0/24"}})
	defaults, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(9), RPM: limitNumber(0)})
	state := &defaultLimitResetState{Target: LimitTarget{Kind: "user", ID: "usr_reset"}, Stored: before, Policy: defaults, Pricing: entity.PricingSetting{PlatformCurrency: "USD"}}
	result, err := defaultLimitResetPolicy(state)
	if err != nil || *result.TokensMonth != 9 || result.RPM == nil || *result.RPM != 0 || result.IPMode != before.IPMode || !reflect.DeepEqual(result.IPRanges, before.IPRanges) || *state.Stored.TokensMonth != 2 {
		t.Fatal("reset lost policy or source", err)
	}
	result.IPRanges[0] = "198.51.100.0/24"
	if state.Stored.IPRanges[0] != "192.0.2.0/24" {
		t.Fatal("reset borrowed mutable IP range source")
	}
	state.Target.Kind = "team"
	result, err = defaultLimitResetPolicy(state)
	if err != nil || result.IPMode != "none" || len(result.IPRanges) != 0 {
		t.Fatal("User IP copied to Team", err)
	}
	money := "5"
	state.Policy.MoneyMonth, state.Policy.Currency = &money, "EUR"
	if _, err := defaultLimitResetPolicy(state); err == nil {
		t.Fatal("reset reinterpreted a money denomination")
	}
}

func TestDefaultLimitResetReviewAndLastIntentAreIndependent(t *testing.T) {
	stamp := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	before, _ := limits.Normalize(limits.Policy{IPMode: "allowlist", IPRanges: []string{"192.0.2.0/24"}})
	template, _ := limits.Normalize(limits.Policy{TokensMonth: limitNumber(9)})
	state := defaultLimitResetState{Target: LimitTarget{Kind: "user", ID: "usr_review"}, User: entity.User{CreatedAt: stamp, UpdatedAt: stamp}, Row: entity.ResourceLimit{ETag: "lim_one"}, Stored: before, Rule: entity.DefaultLimitRule{RuleETag: strings.Repeat("a", 64)}, Policy: template, Pricing: entity.PricingSetting{ETag: "pricing_one", PlatformCurrency: "USD"}}
	original, err := defaultLimitResetETag(&state)
	if err != nil {
		t.Fatal(err)
	}
	for _, modify := range []func(*defaultLimitResetState){
		func(s *defaultLimitResetState) { s.Target.ID = "usr_other" },
		func(s *defaultLimitResetState) { s.User.UpdatedAt = stamp.Add(time.Microsecond) },
		func(s *defaultLimitResetState) { s.Row.ETag = "lim_two" },
		func(s *defaultLimitResetState) { s.Rule.RuleETag = strings.Repeat("b", 64) },
		func(s *defaultLimitResetState) { s.Pricing.ETag = "pricing_two" },
		func(s *defaultLimitResetState) { s.Pricing.PlatformCurrency = "EUR" },
		func(s *defaultLimitResetState) { s.Policy.TokensMonth = limitNumber(0) },
		func(s *defaultLimitResetState) { s.Stored.IPRanges = []string{"198.51.100.0/24"} },
	} {
		changed := state
		modify(&changed)
		hash, err := defaultLimitResetETag(&changed)
		if err != nil || hash == original {
			t.Fatal("reset composite omitted dependency", err)
		}
	}
	row := entity.ResourceLimit{ETag: "lim_saved", ActorID: "usr_actor", Reason: "Reviewed reset", AppliedDefaultETag: &state.Rule.RuleETag, DefaultResetETag: &original}
	if !defaultLimitResetMatches(row, "usr_actor", "Reviewed reset", original) {
		t.Fatal("saved original intent missing")
	}
	for _, change := range []func(*entity.ResourceLimit){
		func(r *entity.ResourceLimit) { r.AppliedDefaultETag = nil }, func(r *entity.ResourceLimit) { r.DefaultResetETag = nil },
		func(r *entity.ResourceLimit) { r.ActorID = "usr_other" }, func(r *entity.ResourceLimit) { r.Reason = "Different reason" },
	} {
		changed := row
		change(&changed)
		if defaultLimitResetMatches(changed, "usr_actor", "Reviewed reset", original) {
			t.Fatal("superseded or different intent resolved")
		}
	}
	for _, body := range []string{`{}`, `{"reason":null}`, `{"reason":"a","reason":"b"}`, `{"reason":"a","request_id":"new"}`} {
		var input DefaultLimitResetInput
		if json.Unmarshal([]byte(body), &input) == nil {
			t.Fatal("malformed reset intent accepted", body)
		}
	}
}

func TestDefaultLimitResetAppliedProofRequiresExactRevisionLeaseAndCurrency(t *testing.T) {
	money := "5"
	policy, _ := limits.Normalize(limits.Policy{MoneyMonth: &money, Currency: "USD"})
	state := &defaultLimitResetState{Target: LimitTarget{Kind: "user", ID: "usr_proof"}, User: entity.User{ID: "usr_proof"}, Row: entity.ResourceLimit{ETag: "lim_proof"}, Stored: policy, Pricing: entity.PricingSetting{PlatformCurrency: "USD"}}
	s := &Service{runtime: &gatewayRuntime{}, recorder: &callRecorder{}}
	auth := &runtimeAuthorization{ValidUntil: time.Now().Add(time.Hour), LimitPolicies: map[string]limits.Policy{"user_usr_proof": policy}, Quota: &runtimeQuotaData{Revisions: map[string]string{"user_usr_proof": "lim_proof"}, Currency: "USD"}}
	s.runtime.auth.Store(auth)
	if !s.defaultUserLimitApplied(state) {
		t.Fatal("exact applied proof rejected")
	}
	auth.Quota.Revisions["user_usr_proof"] = "lim_older"
	if s.defaultUserLimitApplied(state) {
		t.Fatal("matching caps with older revision acknowledged")
	}
	auth.Quota.Revisions["user_usr_proof"] = "lim_proof"
	auth.Quota.Currency = "EUR"
	if s.defaultUserLimitApplied(state) {
		t.Fatal("contradictory denomination acknowledged")
	}
	auth.Quota.Currency = "USD"
	auth.ValidUntil = time.Now().Add(-time.Second)
	if s.defaultUserLimitApplied(state) {
		t.Fatal("expired authorization acknowledged")
	}
	auth.ValidUntil = time.Now().Add(time.Hour)
	s.runtime.deniedLimits.Store("user_usr_proof", uint64(1))
	if s.defaultUserLimitApplied(state) {
		t.Fatal("revoked old policy acknowledged")
	}
}
