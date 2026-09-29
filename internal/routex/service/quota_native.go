package service

import (
	"bytes"
	"encoding/json"

	"github.com/miclle/routex/internal/routex/entity"
)

// quotaAttachmentPayload returns a detached text-only classification view when
// every non-text block is one of the structurally valid RouteX attachment
// occurrences discovered by gatewayAttachmentPlan. Ownership, readiness, MIME,
// and bytes are authorized later by resolveGatewayAttachments. This classifier
// never rewrites the upstream request or derives tokens from object properties.
func quotaAttachmentPayload(protocol string, plan *gatewayAttachmentPlan) (map[string]json.RawMessage, bool) {
	if plan == nil || len(plan.Occurrences) == 0 {
		return nil, false
	}
	raw, err := json.Marshal(plan.root)
	if err != nil {
		return nil, false
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&root) != nil || root == nil {
		return nil, false
	}
	for _, occurrence := range plan.Occurrences {
		var block map[string]any
		switch protocol {
		case entity.ProtocolOpenAIChat:
			if !quotaDirectAttachmentPath(occurrence.path, "messages", "content", 6) ||
				(occurrence.shape != "data_url" && occurrence.shape != "openai_file") {
				return nil, false
			}
			block, _ = attachmentObject(root, occurrence.path[:len(occurrence.path)-2])
			if !strictQuotaAttachmentBlock(block, occurrence.shape) {
				return nil, false
			}
			replaceQuotaAttachmentBlock(block, "text", "text")
		case entity.ProtocolOpenAIResponses:
			if !quotaDirectAttachmentPath(occurrence.path, "input", "content", 5) ||
				(occurrence.shape != "data_url" && occurrence.shape != "openai_file") {
				return nil, false
			}
			block, _ = attachmentObject(root, occurrence.path[:len(occurrence.path)-1])
			if !strictQuotaAttachmentBlock(block, occurrence.shape) {
				return nil, false
			}
			replaceQuotaAttachmentBlock(block, "input_text", "text")
		case entity.ProtocolAnthropicMessages:
			if !quotaDirectAttachmentPath(occurrence.path, "messages", "content", 6) || occurrence.shape != "anthropic_source" {
				return nil, false
			}
			block, _ = attachmentObject(root, occurrence.path[:len(occurrence.path)-2])
			if !strictQuotaAttachmentBlock(block, occurrence.shape) {
				return nil, false
			}
			replaceQuotaAttachmentBlock(block, "text", "text")
		case entity.ProtocolGeminiGenerateContent:
			if !quotaDirectAttachmentPath(occurrence.path, "contents", "parts", 6) ||
				(occurrence.shape != "gemini_inline_camel" && occurrence.shape != "gemini_inline_snake") {
				return nil, false
			}
			block, _ = attachmentObject(root, occurrence.path[:len(occurrence.path)-2])
			if !strictQuotaAttachmentBlock(block, occurrence.shape) {
				return nil, false
			}
			for key := range block {
				delete(block, key)
			}
			block["text"] = ""
		default:
			return nil, false
		}
	}
	raw, err = json.Marshal(root)
	if err != nil {
		return nil, false
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(raw, &payload) != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

func quotaDirectAttachmentPath(path []any, collection, content string, length int) bool {
	if len(path) != length || path[0] != collection || path[2] != content {
		return false
	}
	_, collectionIndex := path[1].(int)
	_, contentIndex := path[3].(int)
	return collectionIndex && contentIndex
}

func replaceQuotaAttachmentBlock(block map[string]any, kind, valueField string) {
	for key := range block {
		delete(block, key)
	}
	block["type"] = kind
	block[valueField] = ""
}

func strictQuotaAttachmentBlock(block map[string]any, shape string) bool {
	if block == nil {
		return false
	}
	switch shape {
	case "data_url":
		if block["type"] == "input_image" {
			if !quotaOnlyAnyFields(block, "type", "image_url") && !quotaOnlyAnyFields(block, "type", "image_url", "detail") {
				return false
			}
			_, ok := block["image_url"].(string)
			return ok && quotaImageDetail(block["detail"])
		}
		if !quotaOnlyAnyFields(block, "type", "image_url") {
			return false
		}
		container, ok := block["image_url"].(map[string]any)
		return block["type"] == "image_url" && ok &&
			(quotaOnlyAnyFields(container, "url") || quotaOnlyAnyFields(container, "url", "detail")) &&
			quotaImageDetail(container["detail"])
	case "openai_file":
		if block["type"] == "input_file" {
			return quotaOnlyAnyFields(block, "type", "file_data") || quotaOnlyAnyFields(block, "type", "file_data", "filename")
		}
		container, ok := block["file"].(map[string]any)
		return block["type"] == "file" && quotaOnlyAnyFields(block, "type", "file") && ok && (quotaOnlyAnyFields(container, "file_data") || quotaOnlyAnyFields(container, "file_data", "filename"))
	case "anthropic_source":
		if (block["type"] != "image" && block["type"] != "document") || !quotaOnlyAnyFields(block, "type", "source") {
			return false
		}
		source, ok := block["source"].(map[string]any)
		return ok && source["type"] == "base64" && quotaOnlyAnyFields(source, "type", "media_type", "data")
	case "gemini_inline_camel":
		inline, ok := block["inlineData"].(map[string]any)
		return quotaOnlyAnyFields(block, "inlineData") && ok && quotaOnlyAnyFields(inline, "mimeType", "data")
	case "gemini_inline_snake":
		inline, ok := block["inline_data"].(map[string]any)
		return quotaOnlyAnyFields(block, "inline_data") && ok && quotaOnlyAnyFields(inline, "mime_type", "data")
	default:
		return false
	}
}

func quotaImageDetail(value any) bool {
	if value == nil {
		return true
	}
	detail, ok := value.(string)
	return ok && (detail == "auto" || detail == "low" || detail == "high")
}

func quotaOnlyAnyFields(object map[string]any, names ...string) bool {
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	for key := range object {
		if !allowed[key] {
			return false
		}
	}
	return len(object) == len(allowed)
}

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
