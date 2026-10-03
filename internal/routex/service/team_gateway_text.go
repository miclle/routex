package service

import (
	"encoding/json"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
)

func teamNativeProtocol(protocol string) bool {
	return slices.Contains([]string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent}, protocol)
}

func teamTextUnsupported() error {
	return gatewayError(400, "unsupported_input", "Team Session inference supports text input only.")
}

// Inspect native content positions only. Function arguments, tool schemas and
// ordinary text remain opaque; no Team request can reach a Personal object read.
func validateTeamNativeText(protocol string, payload map[string]json.RawMessage) error {
	for _, field := range []string{"team", "team_id", "teamId", "user_id", "project", "project_id", "key_id", "workspace", "workspace_id", "session", "session_id", "sessionId", "attachment_scope"} {
		if _, exists := payload[field]; exists {
			return gatewayError(400, "invalid_request_error", "Workspace selectors are not supported in a Team request.")
		}
	}
	valid := false
	switch protocol {
	case entity.ProtocolOpenAIChat:
		return validateTeamChatText(payload)
	case entity.ProtocolOpenAIResponses:
		valid = teamResponsesText(payload)
	case entity.ProtocolAnthropicMessages:
		valid = teamMessagesText(payload)
	case entity.ProtocolGeminiGenerateContent:
		valid = teamGeminiText(payload)
	}
	if !valid {
		return teamTextUnsupported()
	}
	return nil
}

func teamResponsesContent(raw json.RawMessage) bool {
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
		if json.Unmarshal(part["type"], &kind) != nil || !slices.Contains([]string{"input_text", "output_text", "refusal"}, kind) {
			return false
		}
		field := "text"
		if kind == "refusal" {
			field = "refusal"
		}
		if json.Unmarshal(part[field], &text) != nil {
			return false
		}
	}
	return true
}
func teamResponsesText(payload map[string]json.RawMessage) bool {
	var text string
	if json.Unmarshal(payload["input"], &text) != nil {
		var items []map[string]json.RawMessage
		if json.Unmarshal(payload["input"], &items) != nil || items == nil {
			return false
		}
		for _, item := range items {
			var kind string
			if raw, exists := item["type"]; exists && json.Unmarshal(raw, &kind) != nil {
				return false
			}
			switch kind {
			case "", "message":
				if !teamResponsesContent(item["content"]) {
					return false
				}
			case "function_call_output", "custom_tool_call_output":
				if !teamResponsesContent(item["output"]) {
					return false
				}
			case "function_call", "custom_tool_call", "reasoning":
			default:
				return false
			}
		}
	}
	if raw, exists := payload["instructions"]; exists && json.Unmarshal(raw, &text) != nil {
		return false
	}
	var tools []map[string]json.RawMessage
	if raw := payload["tools"]; raw != nil {
		if json.Unmarshal(raw, &tools) != nil || tools == nil {
			return false
		}
		for _, tool := range tools {
			var kind string
			if json.Unmarshal(tool["type"], &kind) != nil || !slices.Contains([]string{"function", "custom"}, kind) {
				return false
			}
		}
	}
	return true
}

func teamMessagesContent(raw json.RawMessage, depth int) bool {
	if depth >= 8 {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return true
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil || blocks == nil {
		return false
	}
	for _, block := range blocks {
		var kind string
		if json.Unmarshal(block["type"], &kind) != nil {
			return false
		}
		switch kind {
		case "text":
			if json.Unmarshal(block["text"], &text) != nil {
				return false
			}
		case "thinking", "redacted_thinking", "tool_use":
		case "tool_result", "search_result":
			if raw := block["content"]; nativePresent(raw) && !teamMessagesContent(raw, depth+1) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func teamMessagesText(payload map[string]json.RawMessage) bool {
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil || len(messages) == 0 {
		return false
	}
	for _, message := range messages {
		if !teamMessagesContent(message["content"], 0) {
			return false
		}
	}
	return !nativePresent(payload["system"]) || teamMessagesContent(payload["system"], 0)
}

func teamGeminiContent(raw json.RawMessage, depth int) bool {
	if depth >= 8 {
		return false
	}
	object := usageObject(raw)
	var parts []map[string]json.RawMessage
	if object == nil || json.Unmarshal(object["parts"], &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		for field := range part {
			if !slices.Contains([]string{"text", "thought", "thoughtSignature", "thought_signature", "functionCall", "function_call", "functionResponse", "function_response"}, field) {
				return false
			}
		}
		if raw, exists := part["text"]; exists {
			var text string
			if json.Unmarshal(raw, &text) != nil {
				return false
			}
		}
		response := usageObject(geminiField(part, "functionResponse", "function_response"))
		if raw := response["parts"]; raw != nil && !teamGeminiContent(mustGeminiJSON(map[string]json.RawMessage{"parts": raw}), depth+1) {
			return false
		}
	}
	return true
}
func teamGeminiText(payload map[string]json.RawMessage) bool {
	var contents []json.RawMessage
	if json.Unmarshal(payload["contents"], &contents) != nil || len(contents) == 0 {
		return false
	}
	for _, content := range contents {
		if !teamGeminiContent(content, 0) {
			return false
		}
	}
	if raw := geminiField(payload, "systemInstruction", "system_instruction"); nativePresent(raw) && !teamGeminiContent(raw, 0) {
		return false
	}
	config := usageObject(geminiField(payload, "generationConfig", "generation_config"))
	for _, field := range []string{"speechConfig", "speech_config"} {
		if _, exists := config[field]; exists {
			return false
		}
	}
	if nativeAliasCollision(config, "responseModalities", "response_modalities") {
		return false
	}
	if raw := geminiField(config, "responseModalities", "response_modalities"); raw != nil {
		var modalities []string
		if json.Unmarshal(raw, &modalities) != nil {
			return false
		}
		for _, value := range modalities {
			if value != "TEXT" {
				return false
			}
		}
	}
	count, ok := GeminiCandidateCount(mustGeminiJSON(payload))
	return ok && count == 1
}

func validateTeamChatText(payload map[string]json.RawMessage) error {
	unsupported := func() error {
		return gatewayError(400, "unsupported_input", "Team Session inference supports text input only.")
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil {
		return unsupported()
	}
	for _, message := range messages {
		if message["audio"] != nil {
			return unsupported()
		}
		raw := message["content"]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			continue
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &parts) != nil {
			return unsupported()
		}
		for _, part := range parts {
			var kind string
			if json.Unmarshal(part["type"], &kind) != nil || kind != "text" || json.Unmarshal(part["text"], &text) != nil {
				return unsupported()
			}
		}
	}
	if payload["audio"] != nil {
		return unsupported()
	}
	if raw := payload["modalities"]; raw != nil {
		var values []string
		if json.Unmarshal(raw, &values) != nil {
			return unsupported()
		}
		for _, value := range values {
			if value != "text" {
				return unsupported()
			}
		}
	}
	return nil
}
