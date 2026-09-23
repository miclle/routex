package service

import (
	"bytes"
	"encoding/json"
)

// GatewayUsage retains unknown counters instead of interpreting absence as zero.
// Complete refers to an authoritative final usage envelope, not HTTP success.
type GatewayUsage struct {
	Input, Output, CacheRead, CacheWrite *int64
	Present, Complete, Unsupported       bool
	UnsupportedDimensions                []string
}

func usageCounter(raw json.RawMessage) *int64 {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < 0 {
		return nil
	}
	return &value
}
func usageObject(raw json.RawMessage) map[string]json.RawMessage {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

// ParseOpenAIUsage understands the native Chat Completions usage envelope. It
// deliberately does not alias other protocols or infer omitted cache quantities.
func ParseOpenAIUsage(raw []byte, stream bool) GatewayUsage {
	envelope := usageObject(raw)
	result := GatewayUsage{}
	var tier string
	if raw, exists := envelope["service_tier"]; exists && !bytes.Equal(raw, []byte("null")) {
		if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "auto") {
			result.Unsupported = true
			result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_service_tier")
		}
	}
	usageRaw, exists := envelope["usage"]
	if !exists || bytes.Equal(usageRaw, []byte("null")) {
		return result
	}
	result.Present = true
	usage := usageObject(usageRaw)
	if usage == nil {
		return result
	}
	result.Input = usageCounter(usage["prompt_tokens"])
	result.Output = usageCounter(usage["completion_tokens"])
	prompt := usageObject(usage["prompt_tokens_details"])
	completion := usageObject(usage["completion_tokens_details"])
	result.CacheRead = usageCounter(prompt["cached_tokens"])
	result.CacheWrite = usageCounter(prompt["cache_write_tokens"])
	for _, details := range []map[string]json.RawMessage{prompt, completion} {
		for _, key := range []string{"audio_tokens", "image_tokens", "video_tokens"} {
			if raw, ok := details[key]; ok && !bytes.Equal(raw, []byte("null")) {
				count := usageCounter(raw)
				if count == nil || *count != 0 {
					result.Unsupported = true
					result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "response_non_text")
				}
			}
		}
	}
	// Native completion_tokens already includes reasoning and rejected prediction
	// tokens. Adding those detail counters would count the same output twice.
	var kind string
	_ = json.Unmarshal(envelope["object"], &kind)
	var choices []struct {
		FinishReason *string `json:"finish_reason"`
	}
	choicesRaw, hasChoices := envelope["choices"]
	if !hasChoices || bytes.Equal(choicesRaw, []byte("null")) || json.Unmarshal(choicesRaw, &choices) != nil {
		return result
	}
	if stream && kind == "chat.completion.chunk" && len(choices) == 0 {
		result.Complete = true
		return result
	}
	if stream || kind != "chat.completion" || len(choices) == 0 {
		return result
	}
	for _, choice := range choices {
		if choice.FinishReason == nil || *choice.FinishReason == "" {
			return result
		}
	}
	result.Complete = true
	return result
}

// supportsTextPricing examines only request structure and pricing dimensions;
// request content is never copied into a call or pricing snapshot.
func supportsTextPricing(payload map[string]json.RawMessage) bool {
	for _, key := range []string{"audio", "web_search_options", "prompt_cache_options", "prompt_cache_retention"} {
		if raw, exists := payload[key]; exists && !bytes.Equal(raw, []byte("null")) {
			return false
		}
	}
	if raw, exists := payload["service_tier"]; exists {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "auto") {
			return false
		}
	}
	if raw, exists := payload["modalities"]; exists {
		var modalities []string
		if json.Unmarshal(raw, &modalities) != nil || len(modalities) != 1 || modalities[0] != "text" {
			return false
		}
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil {
		return false
	}
	for _, message := range messages {
		if audio, exists := message["audio"]; exists && !bytes.Equal(audio, []byte("null")) {
			return false
		}
		content := message["content"]
		if len(content) == 0 || bytes.Equal(content, []byte("null")) {
			continue
		}
		var text string
		if json.Unmarshal(content, &text) == nil {
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(content, &parts) != nil {
			return false
		}
		for _, part := range parts {
			var kind string
			if json.Unmarshal(part["type"], &kind) != nil || kind != "text" {
				return false
			}
			if _, exists := part["prompt_cache_breakpoint"]; exists {
				return false
			}
		}
	}
	if raw, exists := payload["tools"]; exists {
		var tools []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &tools) != nil {
			return false
		}
		for _, tool := range tools {
			if tool.Type != "function" {
				return false
			}
		}
	}
	return true
}

// pricingRequestDimensions records classification only, never user-supplied
// values. Unsupported dimensions cannot silently receive text/default rates.
func pricingRequestDimensions(payload map[string]json.RawMessage) []string {
	result := []string{}
	if supportsTextPricing(payload) {
		return result
	}
	for _, key := range []string{"prompt_cache_options", "prompt_cache_retention"} {
		if raw, ok := payload[key]; ok && !bytes.Equal(raw, []byte("null")) {
			result = appendDimension(result, "cache_retention")
		}
	}
	if raw, ok := payload["service_tier"]; ok {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || (tier != "default" && tier != "auto") {
			result = appendDimension(result, "request_service_tier")
		}
	}
	if raw, ok := payload["web_search_options"]; ok && !bytes.Equal(raw, []byte("null")) {
		result = appendDimension(result, "external_tool")
	}
	if raw, ok := payload["tools"]; ok {
		var tools []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &tools) != nil {
			result = appendDimension(result, "external_tool")
		}
		for _, tool := range tools {
			if tool.Type != "function" {
				result = appendDimension(result, "external_tool")
			}
		}
	}
	// A second structural pass excludes already classified finite conditions.
	copy := make(map[string]json.RawMessage, len(payload))
	for key, value := range payload {
		copy[key] = value
	}
	for _, key := range []string{"prompt_cache_options", "prompt_cache_retention", "service_tier", "web_search_options", "tools"} {
		delete(copy, key)
	}
	if !supportsTextPricing(copy) {
		result = appendDimension(result, "request_non_text")
	}
	return result
}
func appendDimension(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
