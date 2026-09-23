package service

import (
	"encoding/json"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestResponsesUsageFinalityAndPrecision(t *testing.T) {
	final := `{"object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":1000000,"output_tokens":500000,"input_tokens_details":{"cached_tokens":200000,"cache_write_tokens":100000},"output_tokens_details":{"reasoning_tokens":400000}}}`
	usage := ParseResponsesUsage([]byte(final))
	if !usage.Complete || usage.Unsupported || usage.Input == nil || *usage.Input != 1000000 || usage.Output == nil || *usage.Output != 500000 || usage.CacheRead == nil || *usage.CacheRead != 200000 || usage.CacheWrite == nil || *usage.CacheWrite != 100000 {
		t.Fatalf("native usage wrong: %+v", usage)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(final), &object); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"incomplete", "failed", "cancelled", "in_progress"} {
		object["status"], _ = json.Marshal(status)
		raw, _ := json.Marshal(object)
		observed := ParseResponsesUsage(raw)
		if observed.Complete == (status == "in_progress") {
			t.Fatalf("wrong finality for %s", status)
		}
	}
	object["status"] = json.RawMessage(`"completed"`)
	object["usage"] = json.RawMessage(`{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}`)
	raw, _ := json.Marshal(object)
	missing := ParseResponsesUsage(raw)
	if !missing.Complete || missing.CacheWrite != nil {
		t.Fatal("missing cache write invented")
	}
	fact := pricedFact()
	fact.Protocol = entity.ProtocolOpenAIResponses
	fact.PriceBasis.Schedule.Protocol = entity.ProtocolOpenAIResponses
	fact.InputTokens, fact.OutputTokens, fact.CacheReadTokens, fact.CacheWriteTokens = usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite
	fact.Status = "canceled"
	fact.UsageComplete = true
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "priced" || fact.Pricing.Amount == nil || *fact.Pricing.Amount != "26.6" {
		t.Fatalf("native final canceled call not exactly settled: %+v", fact.Pricing)
	}
	fact.Pricing = nil
	fact.CacheWriteTokens = nil
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "unknown_usage" || fact.Pricing.Amount != nil {
		t.Fatal("unknown usage priced")
	}
	fact.Pricing = nil
	fact.UsageComplete = false
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "not_final" {
		t.Fatal("nonterminal usage priced")
	}
}
func TestResponsesPricingUnsupportedDimensions(t *testing.T) {
	for _, raw := range []string{`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}]}`, `{"input":"hello","tools":[{"type":"web_search"}]}`, `{"input":"hello","service_tier":"priority"}`, `{"input":"hello","prompt_cache_options":{"ttl":"30m"}}`} {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &payload)
		if len(responsesPricingDimensions(payload)) == 0 {
			t.Fatal("unsupported dimensions priced", raw)
		}
	}
	for _, raw := range []string{`{"input":"hello"}`, `{"input":[{"type":"function_call_output","call_id":"call_1","output":"hello"}],"tools":[{"type":"function","name":"run","parameters":{"properties":{"file_id":{"type":"string"}}}}]}`} {
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &payload)
		if len(responsesPricingDimensions(payload)) != 0 {
			t.Fatal("ordinary native text marked unsupported", raw)
		}
	}
}
