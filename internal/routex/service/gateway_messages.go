package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
)

type MessagesHeaders struct{ Version, Beta string }

var messagesVersion = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
var messagesBeta = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidateMessagesHeaders(headers MessagesHeaders) error {
	if !messagesVersion.MatchString(headers.Version) || len(headers.Beta) > 4096 || strings.ContainsAny(headers.Beta, "\r\n") {
		return gatewayError(400, "invalid_request_error", "Valid anthropic-version and bounded anthropic-beta headers are required.")
	}
	if headers.Beta != "" {
		parts := strings.Split(headers.Beta, ",")
		if len(parts) > 32 {
			return gatewayError(400, "invalid_request_error", "Too many beta headers.")
		}
		for _, part := range parts {
			if !messagesBeta.MatchString(strings.TrimSpace(part)) {
				return gatewayError(400, "invalid_request_error", "Invalid beta header.")
			}
		}
	}
	return nil
}
func (s *Service) GatewayMessages(ctx context.Context, bearer string, body []byte, requestID string, headers MessagesHeaders) (*GatewayResult, error) {
	if err := ValidateMessagesHeaders(headers); err != nil {
		return nil, err
	}
	return s.gatewayNative(ctx, bearer, body, requestID, entity.ProtocolAnthropicMessages, gatewayNativeOptions{Messages: headers})
}
func parseGatewayMessages(body []byte) (map[string]json.RawMessage, string, bool, error) {
	invalid := gatewayError(400, "invalid_request_error", "A public model, native messages and nonnegative max_tokens are required.")
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil || payload == nil {
		return nil, "", false, invalid
	}
	var model string
	if json.Unmarshal(payload["model"], &model) != nil || !publicModelName.MatchString(model) {
		return nil, "", false, invalid
	}
	stream := false
	if raw, exists := payload["stream"]; exists {
		if string(raw) != "true" && string(raw) != "false" {
			return nil, model, false, invalid
		}
		stream = string(raw) == "true"
	}
	if usageCounter(payload["max_tokens"]) == nil {
		return nil, model, stream, invalid
	}
	var messages []map[string]json.RawMessage
	if json.Unmarshal(payload["messages"], &messages) != nil || len(messages) == 0 {
		return nil, model, stream, invalid
	}
	for _, message := range messages {
		var role string
		if json.Unmarshal(message["role"], &role) != nil || (role != "user" && role != "assistant") {
			return nil, model, stream, invalid
		}
		if !safeMessagesContent(message["content"]) {
			return nil, model, stream, unsupportedMessagesResource()
		}
	}
	for _, key := range []string{"container", "skills", "session_id", "conversation_id"} {
		if nativePresent(payload[key]) {
			return nil, model, stream, unsupportedMessagesResource()
		}
	}
	if raw := payload["system"]; nativePresent(raw) && !safeMessagesContent(raw) {
		return nil, model, stream, unsupportedMessagesResource()
	}
	var tools []map[string]json.RawMessage
	if raw := payload["tools"]; nativePresent(raw) {
		if json.Unmarshal(raw, &tools) != nil || tools == nil {
			return nil, model, stream, invalid
		}
		for _, tool := range tools {
			for _, key := range []string{"container_id", "file_id", "skill_id", "connector_id", "server_id"} {
				if nativePresent(tool[key]) {
					return nil, model, stream, unsupportedMessagesResource()
				}
			}
		}
	}
	// A URL-based MCP server carries caller-owned authorization. Existing provider
	// connector identities do not have a RouteX ownership map.
	var servers []map[string]json.RawMessage
	if raw := payload["mcp_servers"]; nativePresent(raw) {
		if json.Unmarshal(raw, &servers) != nil {
			return nil, model, stream, invalid
		}
		for _, server := range servers {
			if !nativePresent(server["url"]) || nativePresent(server["id"]) || nativePresent(server["connector_id"]) {
				return nil, model, stream, unsupportedMessagesResource()
			}
		}
	}
	return payload, model, stream, nil
}
func unsupportedMessagesResource() *GatewayError {
	return gatewayError(400, "invalid_request_error", "Stored files, containers, skills and provider-owned contexts require ownership support that is not available.")
}
func safeMessagesContent(raw json.RawMessage) bool {
	if !nativePresent(raw) {
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
		case "text", "thinking", "redacted_thinking", "tool_use": // Client tool inputs are opaque data.
		case "tool_result":
			if nativePresent(block["content"]) && !safeMessagesContent(block["content"]) {
				return false
			}
		case "image", "document":
			source := usageObject(block["source"])
			var sourceType string
			if source == nil || json.Unmarshal(source["type"], &sourceType) != nil || nativePresent(source["file_id"]) {
				return false
			}
			switch sourceType {
			case "base64", "url", "text":
			case "content":
				if !safeMessagesContent(source["content"]) {
					return false
				}
			default:
				return false
			}
		case "search_result":
			if !safeMessagesContent(block["content"]) {
				return false
			}
		default:
			return false
		}
		// Citation file references are native metadata, not arbitrary tool input.
		var citations []map[string]json.RawMessage
		if raw := block["citations"]; nativePresent(raw) {
			if json.Unmarshal(raw, &citations) != nil {
				return false
			}
			for _, citation := range citations {
				if nativePresent(citation["file_id"]) {
					return false
				}
			}
		}
	}
	return true
}
