package service

import (
	"encoding/json"
	"slices"
)

// ParseResponsesUsage is a native Responses adapter. It does not alias chat
// fields or infer omitted cache counters, and reasoning is already in output.
func ParseResponsesUsage(raw []byte) GatewayUsage {
	envelope := usageObject(raw)
	result := GatewayUsage{}
	var kind, status, tier string
	_ = json.Unmarshal(envelope["object"], &kind)
	_ = json.Unmarshal(envelope["status"], &status)
	if raw := envelope["service_tier"]; nativePresent(raw) {
		if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "auto") {
			result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_service_tier")
		}
	}
	var output []map[string]json.RawMessage
	if raw := envelope["output"]; nativePresent(raw) {
		if json.Unmarshal(raw, &output) != nil {
			result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_non_text")
		}
		for _, item := range output {
			var typ string
			_ = json.Unmarshal(item["type"], &typ)
			if !slices.Contains([]string{"message", "reasoning", "function_call", "custom_tool_call"}, typ) {
				result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "external_tool")
			}
			if typ == "message" && !responsesTextContent(item["content"]) {
				result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_non_text")
			}
		}
	}
	result.Unsupported = len(result.UnsupportedDimensions) > 0
	if !nativePresent(envelope["usage"]) {
		return result
	}
	result.Present = true
	usage := usageObject(envelope["usage"])
	if usage == nil {
		return result
	}
	result.Input = usageCounter(usage["input_tokens"])
	result.Output = usageCounter(usage["output_tokens"])
	details := usageObject(usage["input_tokens_details"])
	result.CacheRead = usageCounter(details["cached_tokens"])
	result.CacheWrite = usageCounter(details["cache_write_tokens"])
	for _, part := range []map[string]json.RawMessage{details, usageObject(usage["output_tokens_details"])} {
		for _, name := range []string{"audio_tokens", "image_tokens", "video_tokens"} {
			if raw := part[name]; nativePresent(raw) {
				n := usageCounter(raw)
				if n == nil || *n != 0 {
					result.Unsupported = true
					result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_non_text")
				}
			}
		}
	}
	result.Complete = kind == "response" && slices.Contains([]string{"completed", "failed", "incomplete", "cancelled"}, status)
	return result
}
func responsesTextContent(raw json.RawMessage) bool {
	if !nativePresent(raw) {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return true
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || parts == nil {
		return false
	}
	for _, part := range parts {
		var kind string
		_ = json.Unmarshal(part["type"], &kind)
		if !slices.Contains([]string{"input_text", "output_text", "refusal"}, kind) {
			return false
		}
		if nativePresent(part["prompt_cache_breakpoint"]) {
			return false
		}
	}
	return true
}
func responsesPricingDimensions(payload map[string]json.RawMessage) []string {
	result := []string{}
	for _, key := range []string{"prompt_cache_options", "prompt_cache_retention"} {
		if nativePresent(payload[key]) {
			result = appendDimension(result, "cache_retention")
		}
	}
	if raw := payload["service_tier"]; nativePresent(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "auto") {
			result = appendDimension(result, "request_service_tier")
		}
	}
	var text string
	if json.Unmarshal(payload["input"], &text) != nil {
		var items []map[string]json.RawMessage
		if json.Unmarshal(payload["input"], &items) != nil {
			result = appendDimension(result, "request_non_text")
		}
		for _, item := range items {
			var kind string
			_ = json.Unmarshal(item["type"], &kind)
			switch kind {
			case "", "message":
				if !responsesTextContent(item["content"]) {
					result = appendDimension(result, "request_non_text")
				}
			case "function_call_output", "custom_tool_call_output":
				if !responsesTextContent(item["output"]) {
					result = appendDimension(result, "request_non_text")
				}
			case "reasoning", "function_call", "custom_tool_call":
			default:
				result = appendDimension(result, "request_non_text")
			}
		}
	}
	var tools []struct {
		Type string `json:"type"`
	}
	if nativePresent(payload["tools"]) {
		if json.Unmarshal(payload["tools"], &tools) != nil {
			result = appendDimension(result, "external_tool")
		}
		for _, tool := range tools {
			if tool.Type != "function" && tool.Type != "custom" {
				result = appendDimension(result, "external_tool")
			}
		}
	}
	if nativePresent(payload["audio"]) {
		result = appendDimension(result, "request_non_text")
	}
	return result
}
