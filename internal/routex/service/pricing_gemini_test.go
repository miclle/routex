package service

import (
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestGeminiUsageExactNullableAndConditions(t *testing.T) {
	base := `{"usageMetadata":{"promptTokenCount":10,"cachedContentTokenCount":4,"candidatesTokenCount":3,"thoughtsTokenCount":2,"totalTokenCount":15}}`
	got := ParseGeminiUsage([]byte(base), true)
	if !got.Complete || got.Unsupported || got.Input == nil || *got.Input != 10 || got.Output == nil || *got.Output != 5 || got.CacheRead == nil || *got.CacheRead != 4 || got.CacheWrite == nil || *got.CacheWrite != 0 {
		t.Fatalf("native usage %#v", got)
	}
	for _, field := range []string{`"cachedContentTokenCount":4,`, `"thoughtsTokenCount":2,`} {
		got := ParseGeminiUsage([]byte(strings.Replace(base, field, "", 1)), true)
		if field[1] == 'c' && got.CacheRead != nil || field[1] == 't' && got.Output != nil {
			t.Fatal("missing counter inferred zero")
		}
	}
	for _, raw := range []string{`{"usageMetadata":{"candidatesTokenCount":9223372036854775807,"thoughtsTokenCount":1}}`, `{"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"thoughtsTokenCount":0,"totalTokenCount":1}}`, `{"usageMetadata":{"candidatesTokenCount":3.5,"thoughtsTokenCount":0}}`} {
		if ParseGeminiUsage([]byte(raw), true).Output != nil {
			t.Fatal("invalid total accepted")
		}
	}
	for _, raw := range []string{`{"usageMetadata":{"serviceTier":"PRIORITY"}}`, `{"usageMetadata":{"toolUsePromptTokenCount":2}}`, `{"usageMetadata":{"promptTokensDetails":[{"modality":"IMAGE"}]}}`, `{"candidates":[{"groundingMetadata":{"searchEntryPoint":{}}}]}`, `{"candidates":[{"content":{"parts":[{"inlineData":{"data":"abc"}}]}}]}`} {
		if !ParseGeminiUsage([]byte(raw), true).Unsupported {
			t.Fatal("unsupported pricing accepted", raw)
		}
	}
	for _, raw := range []string{`{"contents":[{"parts":[{"text":"hello"}]}],"tools":[{"googleSearch":{}}]}`, `{"contents":[{"parts":[{"inlineData":{"data":"a"}}]}]}`, `{"generationConfig":{"responseModalities":["AUDIO"]}}`, `{"serviceTier":"FLEX"}`, `{"futureBillingDimension":true}`} {
		if len(geminiPricingDimensions(usageObject([]byte(raw)))) == 0 {
			t.Fatal("unknown request pricing accepted", raw)
		}
	}
	if len(geminiPricingDimensions(usageObject([]byte(`{"contents":[{"parts":[{"text":"hello","thoughtSignature":"signed"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":100}},"tools":[{"functionDeclarations":[{"name":"f","parameters":{"properties":{"fileUri":{"type":"string"}}}}]}]}`)))) != 0 {
		t.Fatal("text native dimensions misclassified")
	}
}

func TestGeminiExactChargeAndFinality(t *testing.T) {
	usage := ParseGeminiUsage([]byte(`{"usageMetadata":{"promptTokenCount":1000000,"cachedContentTokenCount":200000,"candidatesTokenCount":300000,"thoughtsTokenCount":200000,"totalTokenCount":1500000}}`), true)
	fact := pricedFact()
	fact.Protocol = entity.ProtocolGeminiGenerateContent
	fact.PriceBasis.Schedule.Protocol = fact.Protocol
	fact.InputTokens, fact.OutputTokens, fact.CacheReadTokens, fact.CacheWriteTokens = usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite
	fact.UsageComplete = usage.Complete
	fact.Status = "canceled"
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "priced" || fact.Pricing.Amount == nil || *fact.Pricing.Amount != "25.9" {
		t.Fatalf("exact Gemini charge %+v", fact.Pricing)
	}
	fact.Pricing = nil
	fact.UsageComplete = false
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "not_final" || fact.Pricing.Amount != nil {
		t.Fatal("unproven terminal charged")
	}
	fact.Pricing = nil
	fact.UsageComplete = true
	fact.CacheReadTokens = nil
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "unknown_usage" || fact.Pricing.Amount != nil {
		t.Fatal("omitted cache counter charged")
	}
}
