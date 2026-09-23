package service

import "encoding/json"

func quotaOnlyFields(payload map[string]json.RawMessage, names ...string) bool {
	allowed := map[string]bool{}
	for _, name := range names {
		allowed[name] = true
	}
	for name := range payload {
		if !allowed[name] {
			return false
		}
	}
	return true
}
func inspectResponsesQuotaRequest(payload map[string]json.RawMessage) quotaRequest {
	if len(responsesPricingDimensions(payload)) != 0 || !quotaOnlyFields(payload, "model", "input", "instructions", "max_output_tokens", "stream", "stream_options", "temperature", "top_p", "top_logprobs", "metadata", "user", "safety_identifier", "prompt_cache_key", "service_tier", "text", "reasoning", "tools", "tool_choice", "parallel_tool_calls", "include", "truncation", "store", "background") {
		return quotaRequest{}
	}
	maximum := usageCounter(payload["max_output_tokens"])
	if maximum == nil || *maximum <= 0 {
		return quotaRequest{}
	}
	return quotaRequest{Supported: true, MaxOutput: *maximum, CacheRead: true, CacheWrite: true}
}
func inspectMessagesQuotaRequest(payload map[string]json.RawMessage) quotaRequest {
	if len(messagesPricingDimensions(payload)) != 0 || !quotaOnlyFields(payload, "model", "messages", "system", "max_tokens", "stream", "temperature", "top_p", "top_k", "stop_sequences", "metadata", "service_tier", "thinking", "tools", "tool_choice", "cache_control", "output_config") {
		return quotaRequest{}
	}
	// Top-level cache control is not traversed by the existing pricing classifier.
	if raw := payload["cache_control"]; nativePresent(raw) {
		var control map[string]json.RawMessage
		if json.Unmarshal(raw, &control) != nil || !quotaOnlyFields(control, "type", "ttl") {
			return quotaRequest{}
		}
		var kind, ttl string
		if json.Unmarshal(control["type"], &kind) != nil || kind != "ephemeral" {
			return quotaRequest{}
		}
		if nativePresent(control["ttl"]) && (json.Unmarshal(control["ttl"], &ttl) != nil || ttl != "5m") {
			return quotaRequest{}
		}
	}
	maximum := usageCounter(payload["max_tokens"])
	if maximum == nil || *maximum <= 0 {
		return quotaRequest{}
	}
	return quotaRequest{Supported: true, MaxOutput: *maximum, CacheRead: true, CacheWrite: true}
}
func inspectGeminiQuotaRequest(payload map[string]json.RawMessage) quotaRequest {
	if len(geminiPricingDimensions(payload)) != 0 || nativeAliasCollision(payload, "generationConfig", "generation_config") {
		return quotaRequest{}
	}
	config := usageObject(geminiField(payload, "generationConfig", "generation_config"))
	if config == nil {
		return quotaRequest{}
	}
	for _, pair := range [][2]string{{"maxOutputTokens", "max_output_tokens"}, {"candidateCount", "candidate_count"}, {"thinkingConfig", "thinking_config"}} {
		if nativeAliasCollision(config, pair[0], pair[1]) {
			return quotaRequest{}
		}
	}
	if raw := geminiField(config, "candidateCount", "candidate_count"); nativePresent(raw) {
		var count int64
		if json.Unmarshal(raw, &count) != nil || count != 1 {
			return quotaRequest{}
		}
	}
	if raw := geminiField(config, "thinkingConfig", "thinking_config"); nativePresent(raw) {
		thinking := usageObject(raw)
		if thinking == nil || !quotaOnlyFields(thinking, "includeThoughts", "include_thoughts", "thinkingBudget", "thinking_budget", "thinkingLevel", "thinking_level") {
			return quotaRequest{}
		}
	}
	maximum := usageCounter(geminiField(config, "maxOutputTokens", "max_output_tokens"))
	if maximum == nil || *maximum <= 0 {
		return quotaRequest{}
	}
	// This stateless endpoint cannot create explicit caches. Thought tokens are
	// part of the documented maxOutputTokens total and final native output usage.
	return quotaRequest{Supported: true, MaxOutput: *maximum, CacheRead: true, CacheWrite: false}
}
func nativeAliasCollision(payload map[string]json.RawMessage, first, second string) bool {
	return payload[first] != nil && payload[second] != nil
}
