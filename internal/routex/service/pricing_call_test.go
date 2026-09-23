package service

import (
	"encoding/json"
	"testing"

	"github.com/miclle/routex/pkg/pricing"
)

func pricingCount(value int64) *int64 { return &value }
func testPriceBasis() *CallPriceBasis {
	rates := []pricing.Rate{}
	for index, metric := range []string{pricing.Input, pricing.Output, pricing.CacheRead, pricing.CacheWrite} {
		rates = append(rates, pricing.Rate{ID: metric, Metric: metric, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: []string{"2", "4", "0.5", "3"}[index], Enabled: true})
	}
	return &CallPriceBasis{Adapter: "routex_text_v1", ETag: "etag_old", Schedule: pricing.Schedule{PriceID: "prc_one", ProviderModelID: "pmd_one", Protocol: "openai_chat", Rates: rates}, Currency: pricing.FX{PlatformCurrency: "CNY", Rates: map[string]string{"USD": "7", "CNY": "1"}}}
}
func pricedFact() CallFact {
	return CallFact{ProviderModelID: "pmd_one", PriceBasis: testPriceBasis(), InputTokens: pricingCount(1000000), OutputTokens: pricingCount(500000), CacheReadTokens: pricingCount(200000), CacheWriteTokens: pricingCount(100000), UsageComplete: true}
}
func TestCallPriceFinalityAndUnknownAmounts(t *testing.T) {
	for _, test := range []struct {
		name, status string
		mutate       func(*CallFact)
	}{
		{"success", "priced", func(f *CallFact) { f.Status = "success" }},
		{"canceled after final", "priced", func(f *CallFact) { f.Status = "canceled" }},
		{"error after final", "priced", func(f *CallFact) { f.Status = "error" }},
		{"canceled before final", "not_final", func(f *CallFact) { f.Status = "canceled"; f.UsageComplete = false }},
		{"unknown cache", "unknown_usage", func(f *CallFact) { f.CacheWriteTokens = nil }},
		{"missing price", "missing_price", func(f *CallFact) { f.PriceBasis.Schedule.Rates = nil }},
		{"overlapping cache", "invalid_usage", func(f *CallFact) { f.CacheReadTokens = pricingCount(1000001) }},
		{"unsupported", "unsupported", func(f *CallFact) { f.PricingUnsupported = true; f.PricingDimensions = []string{"request_non_text"} }},
		{"legacy journal", "not_captured", func(f *CallFact) { f.PriceBasis = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fact := pricedFact()
			test.mutate(&fact)
			finalizeCallPricing(&fact)
			if fact.Pricing.Status != test.status {
				t.Fatalf("status %s", fact.Pricing.Status)
			}
			if err := validateCallPricing(fact); err != nil {
				t.Fatal(err)
			}
			if test.status == "priced" {
				// (ordinary 0.7M*2 + output 0.5M*4 + read 0.2M*.5 + write 0.1M*3)*7.
				if fact.Pricing.Amount == nil || *fact.Pricing.Amount != "26.6" || *fact.Pricing.Currency != "CNY" {
					t.Fatalf("wrong charge: %+v", fact.Pricing)
				}
			} else if fact.Pricing.Amount != nil || fact.Pricing.Currency != nil {
				t.Fatal("unpriced became zero/free")
			}
		})
	}
}
func TestCallPriceSnapshotAndReplayAreImmutable(t *testing.T) {
	fact := pricedFact()
	finalizeCallPricing(&fact)
	snapshot := *fact.Pricing.SnapshotJSON
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	fact.PriceBasis.Schedule.Rates[0].Amount = "999"
	fact.PriceBasis.Currency.Rates["USD"] = "100"
	if *fact.Pricing.SnapshotJSON != snapshot {
		t.Fatal("source mutation changed receipt")
	}
	var replay CallFact
	if err := json.Unmarshal(encoded, &replay); err != nil {
		t.Fatal(err)
	}
	replay.PriceBasis.Schedule.Rates[0].Amount = "999"
	finalizeCallPricing(&replay)
	if *replay.Pricing.Amount != "26.6" || *replay.Pricing.SnapshotJSON != snapshot {
		t.Fatal("replay recalculated accepted receipt")
	}
	clone := clonePriceBasis(fact.PriceBasis)
	clone.Schedule.Rates[0].Amount = "1"
	clone.Currency.Rates["USD"] = "2"
	if fact.PriceBasis.Schedule.Rates[0].Amount != "999" || fact.PriceBasis.Currency.Rates["USD"] != "100" {
		t.Fatal("request basis aliases runtime maps or slices")
	}
}
func TestCallPriceZeroIsNotMissing(t *testing.T) {
	fact := pricedFact()
	fact.InputTokens = pricingCount(0)
	fact.OutputTokens = pricingCount(0)
	fact.CacheReadTokens = pricingCount(0)
	fact.CacheWriteTokens = pricingCount(0)
	finalizeCallPricing(&fact)
	if fact.Pricing.Amount == nil || *fact.Pricing.Amount != "0" {
		t.Fatal("known zero usage was lost")
	}
	fact.Pricing = nil
	fact.PriceBasis.Schedule.Rates = nil
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "missing_price" || fact.Pricing.Amount != nil {
		t.Fatal("absent configuration became free")
	}
}
