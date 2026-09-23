package service

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
)

func (s *Service) GatewayResponses(ctx context.Context, bearer string, body []byte, requestID string) (*GatewayResult, error) {
	return s.gatewayNative(ctx, bearer, body, requestID, entity.ProtocolOpenAIResponses)
}

func parseGatewayResponses(body []byte) (map[string]json.RawMessage, string, bool, error) {
	invalid := gatewayError(400, "invalid_request_error", "A public model and native input string or array are required.")
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return nil, "", false, invalid
	}
	var model string
	if json.Unmarshal(payload["model"], &model) != nil || !publicModelName.MatchString(model) {
		return nil, "", false, invalid
	}
	stream := false
	if value, exists := payload["stream"]; exists {
		if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
			return nil, model, false, invalid
		}
		stream = bytes.Equal(value, []byte("true"))
	}
	if value, exists := payload["background"]; exists && !bytes.Equal(value, []byte("false")) && !bytes.Equal(value, []byte("null")) {
		return nil, model, stream, unsupportedResponsesState()
	}
	for _, key := range []string{"previous_response_id", "conversation", "prompt", "multi_agent", "betas"} {
		if nativePresent(payload[key]) {
			return nil, model, stream, unsupportedResponsesState()
		}
	}
	if raw, exists := payload["store"]; exists && !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
		return nil, model, stream, invalid
	}
	if !nativePresent(payload["input"]) {
		return nil, model, stream, invalid
	}
	var text string
	if json.Unmarshal(payload["input"], &text) != nil {
		var items []json.RawMessage
		if json.Unmarshal(payload["input"], &items) != nil || items == nil {
			return nil, model, stream, invalid
		}
		for _, raw := range items {
			if !safeResponsesInputItem(raw) {
				return nil, model, stream, unsupportedResponsesState()
			}
		}
	}
	if raw, exists := payload["tools"]; exists {
		var tools []json.RawMessage
		if json.Unmarshal(raw, &tools) != nil || tools == nil {
			return nil, model, stream, invalid
		}
		for _, raw := range tools {
			if !safeResponsesTool(raw) {
				return nil, model, stream, unsupportedResponsesState()
			}
		}
	}
	// A cache diagnostic referencing an upstream response also needs ownership.
	if options := usageObject(payload["prompt_cache_options"]); nativePresent(options["comparison_response_id"]) {
		return nil, model, stream, unsupportedResponsesState()
	}
	return payload, model, stream, nil
}
func nativePresent(raw json.RawMessage) bool {
	return len(raw) != 0 && !bytes.Equal(raw, []byte("null"))
}
func unsupportedResponsesState() *GatewayError {
	return gatewayError(400, "unsupported_parameter", "This endpoint supports foreground inline Responses requests. Stored response, conversation, file, vector-store, container, template, and connector references require ownership support that is not available.")
}

// These checks follow native resource-bearing fields only. A user function's
// parameters/schema or text may legitimately contain keys such as file_id.
func safeResponsesInputItem(raw json.RawMessage) bool {
	item := usageObject(raw)
	if item == nil {
		return false
	}
	var kind string
	if value, ok := item["type"]; ok && json.Unmarshal(value, &kind) != nil {
		return false
	}
	switch kind {
	case "", "message":
		return safeResponsesContent(item["content"])
	case "function_call", "custom_tool_call", "reasoning":
		return true
	case "function_call_output", "custom_tool_call_output":
		return safeResponsesContent(item["output"])
	case "computer_call":
		return true
	case "computer_call_output":
		output := usageObject(item["output"])
		if output == nil {
			return false
		}
		return !nativePresent(output["file_id"]) && !nativePresent(output["container_id"])
	default:
		return false
	}
}
func safeResponsesContent(raw json.RawMessage) bool {
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
		if json.Unmarshal(part["type"], &kind) != nil {
			return false
		}
		if !slices.Contains([]string{"input_text", "output_text", "refusal", "input_image", "input_file"}, kind) {
			return false
		}
		for _, field := range []string{"file_id", "container_id", "vector_store_id", "item_id"} {
			if nativePresent(part[field]) {
				return false
			}
		}
	}
	return true
}
func safeResponsesTool(raw json.RawMessage) bool {
	tool := usageObject(raw)
	if tool == nil {
		return false
	}
	var kind string
	if json.Unmarshal(tool["type"], &kind) != nil {
		return false
	}
	switch kind {
	case "function", "custom":
		return true // Schemas are opaque user data.
	case "web_search", "web_search_preview", "computer", "computer_use_preview", "image_generation":
		if mask := usageObject(tool["input_image_mask"]); nativePresent(mask["file_id"]) {
			return false
		}
		for _, field := range []string{"file_ids", "vector_store_ids", "container_id", "connector_id"} {
			if nativePresent(tool[field]) {
				return false
			}
		}
		return true
	case "code_interpreter":
		var container string
		if json.Unmarshal(tool["container"], &container) == nil {
			return container == "auto"
		}
		object := usageObject(tool["container"])
		if object == nil || nativePresent(object["file_ids"]) || nativePresent(object["id"]) {
			return false
		}
		return json.Unmarshal(object["type"], &container) == nil && container == "auto"
	case "mcp":
		return !nativePresent(tool["connector_id"]) && !nativePresent(tool["server_id"]) && nativePresent(tool["server_url"])
	default:
		return false
	}
}
