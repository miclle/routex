package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"strings"
	"testing"
)

const messagesTestUsage = `{"input_tokens":7,"output_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":0},"service_tier":"standard"}`

func TestMessagesUsageNormalizationAndTTL(t *testing.T) {
	usage := messagesUsage([]byte(messagesTestUsage), true)
	if !usage.Complete || usage.Unsupported || usage.Input == nil || *usage.Input != 10 || *usage.Output != 4 {
		t.Fatalf("native normalized usage %+v", usage)
	}
	for _, raw := range []string{strings.Replace(messagesTestUsage, `"ephemeral_5m_input_tokens":1`, `"ephemeral_5m_input_tokens":0`, 1), strings.Replace(messagesTestUsage, `"ephemeral_1h_input_tokens":0`, `"ephemeral_1h_input_tokens":1`, 1), strings.Replace(messagesTestUsage, `"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":0},`, "", 1)} {
		if usage := messagesUsage([]byte(raw), true); !usage.Unsupported {
			t.Fatal("unknown/mixed TTL charged")
		}
	}
	for _, raw := range []string{`{"input_tokens":7,"output_tokens":4}`, `{"input_tokens":9223372036854775807,"cache_read_input_tokens":1,"cache_creation_input_tokens":0,"output_tokens":4}`} {
		if usage := messagesUsage([]byte(raw), true); usage.Input != nil {
			t.Fatal("missing cache or overflow became known")
		}
	}
	var request map[string]json.RawMessage
	_ = json.Unmarshal([]byte(`{"messages":[{"content":"hello"}],"cache_control":{"type":"ephemeral","ttl":"1h"}}`), &request)
	if len(messagesPricingDimensions(request)) == 0 {
		t.Fatal("1h requested cache priced")
	}
}
func TestMessagesCumulativeStreamUsage(t *testing.T) {
	state := MessagesStreamUsage{}
	if !state.Start([]byte(messagesTestUsage)) {
		t.Fatal("start failed")
	}
	if !state.Delta([]byte(`{"output_tokens":8}`), false) || !state.Delta([]byte(`{"output_tokens":12}`), true) {
		t.Fatal("valid cumulative deltas rejected")
	}
	usage := state.Usage(true)
	if !usage.Complete || *usage.Input != 10 || *usage.Output != 12 {
		t.Fatalf("cumulative output summed %+v", usage)
	}
	if state.Delta([]byte(`{"output_tokens":13}`), true) {
		t.Fatal("duplicate terminal delta accepted")
	}
	state = MessagesStreamUsage{}
	state.Start([]byte(messagesTestUsage))
	state.Delta([]byte(`{"output_tokens":8}`), false)
	if state.Delta([]byte(`{"output_tokens":7}`), true) {
		t.Fatal("decreasing usage accepted")
	}
	state = MessagesStreamUsage{}
	state.Start([]byte(messagesTestUsage))
	state.Delta([]byte(`{"output_tokens":"invalid","cache_read_input_tokens":null}`), true)
	usage = state.Usage(true)
	if usage.Output != nil || usage.Input != nil {
		t.Fatal("malformed late counters inherited old values")
	}
	if state.Usage(false).Complete {
		t.Fatal("nonterminal usage priced")
	}
}

func TestMessagesExactChargeUsesNormalizedFinalUsage(t *testing.T) {
	usage := messagesUsage([]byte(`{"input_tokens":700000,"output_tokens":500000,"cache_read_input_tokens":200000,"cache_creation_input_tokens":100000,"cache_creation":{"ephemeral_5m_input_tokens":100000,"ephemeral_1h_input_tokens":0},"output_tokens_details":{"thinking_tokens":300000}}`), true)
	fact := pricedFact()
	fact.Protocol = entity.ProtocolAnthropicMessages
	fact.PriceBasis.Schedule.Protocol = entity.ProtocolAnthropicMessages
	fact.InputTokens, fact.OutputTokens, fact.CacheReadTokens, fact.CacheWriteTokens = usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite
	fact.Status = "canceled"
	fact.UsageComplete = true
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "priced" || fact.Pricing.Amount == nil || *fact.Pricing.Amount != "26.6" {
		t.Fatalf("native exact charge wrong: %+v", fact.Pricing)
	}
	fact.Pricing = nil
	fact.UsageComplete = false
	finalizeCallPricing(&fact)
	if fact.Pricing.Status != "not_final" || fact.Pricing.Amount != nil {
		t.Fatal("partial native usage charged")
	}
}
