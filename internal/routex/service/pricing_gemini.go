package service

import (
	"encoding/json"
	"math"
	"slices"
)

// ParseGeminiUsage consumes one complete aggregate snapshot, never a sum of
// stream frames. Missing thought/cache counters remain unknown. Cache writes
// are not applicable to stateless generation; explicit cache APIs are excluded.
func ParseGeminiUsage(raw []byte, complete bool) GatewayUsage {
	envelope := usageObject(raw)
	native := geminiField(envelope, "usageMetadata", "usage_metadata")
	object := usageObject(native)
	result := GatewayUsage{Present: nativePresent(native), Complete: complete}
	for _, pair := range [][2]string{{"promptTokenCount", "prompt_token_count"}, {"cachedContentTokenCount", "cached_content_token_count"}, {"candidatesTokenCount", "candidates_token_count"}, {"thoughtsTokenCount", "thoughts_token_count"}, {"totalTokenCount", "total_token_count"}, {"serviceTier", "service_tier"}} {
		if object[pair[0]] != nil && object[pair[1]] != nil {
			return result
		}
	}
	result.Input = usageCounter(geminiField(object, "promptTokenCount", "prompt_token_count"))
	result.CacheRead = usageCounter(geminiField(object, "cachedContentTokenCount", "cached_content_token_count"))
	candidates := usageCounter(geminiField(object, "candidatesTokenCount", "candidates_token_count"))
	thoughts := usageCounter(geminiField(object, "thoughtsTokenCount", "thoughts_token_count"))
	if candidates != nil && thoughts != nil && *candidates <= math.MaxInt64-*thoughts {
		v := *candidates + *thoughts
		result.Output = &v
	}
	zero := int64(0)
	result.CacheWrite = &zero
	add := func(dimension string) {
		result.Unsupported = true
		result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, dimension)
	}
	for key := range object {
		if !slices.Contains([]string{"promptTokenCount", "prompt_token_count", "cachedContentTokenCount", "cached_content_token_count", "candidatesTokenCount", "candidates_token_count", "thoughtsTokenCount", "thoughts_token_count", "totalTokenCount", "total_token_count", "toolUsePromptTokenCount", "tool_use_prompt_token_count", "promptTokensDetails", "prompt_tokens_details", "cacheTokensDetails", "cache_tokens_details", "candidatesTokensDetails", "candidates_tokens_details", "toolUsePromptTokensDetails", "tool_use_prompt_tokens_details", "serviceTier", "service_tier"}, key) {
			add("request_condition")
		}
	}
	if total := usageCounter(geminiField(object, "totalTokenCount", "total_token_count")); total != nil && result.Input != nil && result.Output != nil && (*result.Input > math.MaxInt64-*result.Output || *total != *result.Input+*result.Output) {
		result.Input = nil
		result.Output = nil
	}
	for _, names := range [][2]string{{"toolUsePromptTokenCount", "tool_use_prompt_token_count"}} {
		if raw := geminiField(object, names[0], names[1]); nativePresent(raw) {
			if count := usageCounter(raw); count == nil || *count != 0 {
				add("external_tool")
			}
		}
	}
	if raw := geminiField(object, "serviceTier", "service_tier"); nativePresent(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || tier != "STANDARD" {
			add("response_service_tier")
		}
	}
	for _, names := range [][2]string{{"promptTokensDetails", "prompt_tokens_details"}, {"cacheTokensDetails", "cache_tokens_details"}, {"candidatesTokensDetails", "candidates_tokens_details"}, {"toolUsePromptTokensDetails", "tool_use_prompt_tokens_details"}} {
		if raw := geminiField(object, names[0], names[1]); nativePresent(raw) {
			var details []map[string]json.RawMessage
			if json.Unmarshal(raw, &details) != nil {
				add("response_non_text")
				continue
			}
			for _, detail := range details {
				var modality string
				if json.Unmarshal(detail["modality"], &modality) != nil || modality != "TEXT" {
					add("response_non_text")
				}
			}
		}
	}
	var nativeCandidates []map[string]json.RawMessage
	if json.Unmarshal(envelope["candidates"], &nativeCandidates) == nil {
		for _, candidate := range nativeCandidates {
			for _, key := range []string{"groundingMetadata", "grounding_metadata", "urlContextMetadata", "url_context_metadata"} {
				if nativePresent(candidate[key]) {
					add("external_tool")
				}
			}
			for _, dimension := range geminiContentDimensions(candidate["content"]) {
				add(dimension)
			}
		}
	}
	return result
}
func geminiContentDimensions(raw json.RawMessage) []string {
	if !nativePresent(raw) {
		return nil
	}
	content := usageObject(raw)
	var parts []map[string]json.RawMessage
	if json.Unmarshal(content["parts"], &parts) != nil {
		return []string{"response_non_text"}
	}
	dimensions := []string{}
	for _, part := range parts {
		response := usageObject(geminiField(part, "functionResponse", "function_response"))
		if nativePresent(response["parts"]) {
			dimensions = appendDimension(dimensions, "response_non_text")
		}
		for key := range part {
			if !slices.Contains([]string{"text", "thought", "thoughtSignature", "thought_signature", "functionCall", "function_call", "functionResponse", "function_response"}, key) {
				dimensions = appendDimension(dimensions, "response_non_text")
			}
		}
	}
	return dimensions
}
func geminiPricingDimensions(payload map[string]json.RawMessage) []string {
	dimensions := []string{}
	add := func(value string) { dimensions = appendDimension(dimensions, value) }
	for key := range payload {
		if !slices.Contains([]string{"contents", "tools", "toolConfig", "tool_config", "safetySettings", "safety_settings", "systemInstruction", "system_instruction", "generationConfig", "generation_config", "serviceTier", "service_tier", "store"}, key) {
			add("request_condition")
		}
	}
	var contents []json.RawMessage
	_ = json.Unmarshal(payload["contents"], &contents)
	contents = append(contents, geminiField(payload, "systemInstruction", "system_instruction"))
	for _, content := range contents {
		if len(geminiContentDimensions(content)) > 0 {
			add("request_non_text")
		}
	}
	var tools []map[string]json.RawMessage
	_ = json.Unmarshal(payload["tools"], &tools)
	for _, tool := range tools {
		for key := range tool {
			if key != "functionDeclarations" && key != "function_declarations" {
				add("external_tool")
			}
		}
	}
	config := usageObject(geminiField(payload, "generationConfig", "generation_config"))
	for key := range config {
		if !slices.Contains([]string{"stopSequences", "stop_sequences", "responseMimeType", "response_mime_type", "responseSchema", "response_schema", "responseJsonSchema", "response_json_schema", "responseModalities", "response_modalities", "candidateCount", "candidate_count", "maxOutputTokens", "max_output_tokens", "temperature", "topP", "top_p", "topK", "top_k", "seed", "presencePenalty", "presence_penalty", "frequencyPenalty", "frequency_penalty", "responseLogprobs", "response_logprobs", "logprobs", "thinkingConfig", "thinking_config"}, key) {
			add("request_condition")
		}
	}
	if raw := geminiField(config, "responseModalities", "response_modalities"); nativePresent(raw) {
		var modalities []string
		if json.Unmarshal(raw, &modalities) != nil {
			add("request_non_text")
		} else {
			for _, modality := range modalities {
				if modality != "TEXT" {
					add("request_non_text")
				}
			}
		}
	}
	if raw := geminiField(payload, "serviceTier", "service_tier"); nativePresent(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || tier != "STANDARD" {
			add("request_service_tier")
		}
	}
	return dimensions
}
