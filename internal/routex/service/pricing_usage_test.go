package service

import (
	"encoding/json"
	"testing"
)

func TestOpenAIUsagePreservesNativeCacheAndFinality(t *testing.T) {
	for _, test := range []struct {
		name, raw                              string
		stream, complete, unknown, unsupported bool
	}{
		{"normal", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10},"completion_tokens_details":{"reasoning_tokens":15,"rejected_prediction_tokens":3}}}`, false, true, false, false},
		{"final stream", `{"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}}}`, true, true, false, false},
		{"partial stream", `{"object":"chat.completion.chunk","choices":[{"finish_reason":null}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}}}`, true, false, false, false},
		{"omitted caches", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`, false, true, true, false},
		{"null cache", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":null,"cache_write_tokens":10}}}`, false, true, true, false},
		{"audio", `{"object":"chat.completion","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10,"audio_tokens":2}}}`, false, true, false, true},
		{"priority", `{"object":"chat.completion","service_tier":"priority","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":30,"cache_write_tokens":10}}}`, false, true, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ParseOpenAIUsage([]byte(test.raw), test.stream)
			if !got.Present || got.Complete != test.complete || got.Unsupported != test.unsupported || (got.CacheRead == nil || got.CacheWrite == nil) != test.unknown {
				t.Fatalf("unexpected usage: %+v", got)
			}
			if got.Input == nil || *got.Input != 100 || got.Output == nil || *got.Output != 20 {
				t.Fatal("detail counters changed total input/output")
			}
			if !test.unknown && (*got.CacheRead != 30 || *got.CacheWrite != 10) {
				t.Fatal("native cache counts lost")
			}
		})
	}
	for _, raw := range []string{`{}`, `{"usage":null}`} {
		if ParseOpenAIUsage([]byte(raw), true).Present {
			t.Fatal("absent usage replaced a prior final frame")
		}
	}
	for _, raw := range []string{`-1`, `9223372036854775808`, `1.5`, `"4"`, `null`} {
		if usageCounter([]byte(raw)) != nil {
			t.Fatalf("invalid counter accepted: %s", raw)
		}
	}
	if got := usageCounter([]byte("0")); got == nil || *got != 0 {
		t.Fatal("explicit zero lost")
	}
}

func TestTextPricingDimensions(t *testing.T) {
	for _, test := range []struct{ fields, want string }{
		{`"messages":[{"content":"text"}]`, ""},
		{`"messages":[{"content":[{"type":"text","text":"text"}]}],"service_tier":"default"`, ""},
		{`"messages":[{"content":[{"type":"image_url","image_url":{"url":"private-content"}}]}]`, "request_non_text"},
		{`"messages":[],"service_tier":"flex"`, "request_service_tier"},
		{`"messages":[],"prompt_cache_retention":"24h"`, "cache_retention"},
		{`"messages":[],"web_search_options":{}`, "external_tool"},
		{`"messages":[],"tools":[{"type":"function"}]`, ""},
		{`"messages":[],"modalities":["text","audio"]`, "request_non_text"},
	} {
		var payload map[string]json.RawMessage
		if err := json.Unmarshal([]byte("{"+test.fields+"}"), &payload); err != nil {
			t.Fatal(err)
		}
		reasons := pricingRequestDimensions(payload)
		if test.want == "" {
			if len(reasons) != 0 {
				t.Fatalf("supported request rejected: %v", reasons)
			}
		} else if len(reasons) != 1 || reasons[0] != test.want {
			t.Fatalf("classification %v, want %s", reasons, test.want)
		}
	}
}

func TestStreamRequestsFinalUsageWithoutDroppingOtherOptions(t *testing.T) {
	payload, _, _, err := parseGatewayChat([]byte(`{"model":"text","messages":[{"content":"hello"}],"stream":true,"stream_options":{"include_usage":false,"include_obfuscation":false}}`))
	if err != nil {
		t.Fatal(err)
	}
	var options map[string]bool
	if err := json.Unmarshal(payload["stream_options"], &options); err != nil {
		t.Fatal(err)
	}
	if !options["include_usage"] || options["include_obfuscation"] || len(options) != 2 {
		t.Fatal("stream options were lost or final usage not requested")
	}
	if _, _, _, err := parseGatewayChat([]byte(`{"model":"text","messages":[{}],"stream":true,"stream_options":[]}`)); err == nil {
		t.Fatal("invalid stream options accepted")
	}
}
