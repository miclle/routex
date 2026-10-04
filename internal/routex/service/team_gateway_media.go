package service

import (
	"encoding/json"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
)

// Validate native media positions using the same parser-owned locations as
// rewriting. Only managed references are removed from a detached validation
// view; opaque text, function arguments and tool schemas are never traversed.
func validateTeamNativeInput(protocol string, payload map[string]json.RawMessage) error {
	for _, field := range []string{"attachment_team_id", "attachment_membership_id", "creator_user_id", "creator_membership_id", "membership_id", "team_membership_id", "attachment_target"} {
		if _, exists := payload[field]; exists {
			return gatewayError(400, "invalid_request_error", "Workspace selectors are not supported in a Team request.")
		}
	}
	plan, err := planGatewayAttachments(protocol, payload)
	if err != nil {
		return teamTextUnsupported()
	}
	for _, ref := range plan.Occurrences {
		path := ref.path[:len(ref.path)-1]
		if protocol != entity.ProtocolOpenAIResponses {
			path = ref.path[:len(ref.path)-2]
		}
		part, ok := attachmentObject(plan.root, path)
		if !ok {
			return unsupportedGatewayAttachmentReference()
		}
		if err := teamManagedMediaPart(protocol, part); err != nil {
			return err
		}
		clear(part)
		switch protocol {
		case entity.ProtocolOpenAIResponses:
			part["type"], part["text"] = "input_text", ""
		case entity.ProtocolGeminiGenerateContent:
			part["text"] = ""
		default:
			part["type"], part["text"] = "text", ""
		}
	}
	if protocol == entity.ProtocolAnthropicMessages {
		messages, _ := plan.root["messages"].([]any)
		for i, raw := range messages {
			message, _ := raw.(map[string]any)
			normalizeTeamDocumentContent(plan, message["content"], []any{"messages", i, "content"}, 0)
		}
		normalizeTeamDocumentContent(plan, plan.root["system"], []any{"system"}, 0)
	}
	raw, err := json.Marshal(plan.root)
	if err != nil {
		return invalidGatewayAttachmentReference()
	}
	var detached map[string]json.RawMessage
	if json.Unmarshal(raw, &detached) != nil {
		return invalidGatewayAttachmentReference()
	}
	return validateTeamNativeText(protocol, detached)
}

func normalizeTeamDocumentContent(plan *gatewayAttachmentPlan, raw any, path []any, depth int) {
	if depth >= 8 {
		return
	}
	blocks, _ := raw.([]any)
	for i, rawBlock := range blocks {
		block, _ := rawBlock.(map[string]any)
		blockPath := appendPath(path, i)
		if block["type"] == "tool_result" || block["type"] == "search_result" {
			normalizeTeamDocumentContent(plan, block["content"], appendPath(blockPath, "content"), depth+1)
		}
		source, _ := block["source"].(map[string]any)
		if block["type"] != "document" || source["type"] != "content" || !teamMediaFields(source, "type", "content") || !teamMediaFields(block, "type", "source", "cache_control", "title", "context", "citations") {
			continue
		}
		contentPath := appendPath(blockPath, "source", "content")
		managed := false
		for _, ref := range plan.Occurrences {
			if len(ref.path) >= len(contentPath) && slices.Equal(ref.path[:len(contentPath)], contentPath) {
				managed = true
			}
		}
		if !managed {
			continue
		}
		normalizeTeamDocumentContent(plan, source["content"], contentPath, depth+1)
		content := source["content"]
		clear(block)
		block["type"], block["content"] = "tool_result", content
	}
}

func teamMediaFields(object map[string]any, allowed ...string) bool {
	for key := range object {
		if !slices.Contains(allowed, key) {
			return false
		}
	}
	return true
}

func teamManagedMediaPart(protocol string, part map[string]any) error {
	valid := false
	switch protocol {
	case entity.ProtocolOpenAIChat:
		switch part["type"] {
		case "image_url":
			container, _ := part["image_url"].(map[string]any)
			valid = container != nil && teamMediaFields(part, "type", "image_url") && teamMediaFields(container, "url", "detail")
		case "file":
			container, _ := part["file"].(map[string]any)
			valid = container != nil && teamMediaFields(part, "type", "file") && teamMediaFields(container, "file_data", "filename")
		}
	case entity.ProtocolOpenAIResponses:
		switch part["type"] {
		case "input_image":
			valid = teamMediaFields(part, "type", "image_url", "detail")
		case "input_file":
			valid = teamMediaFields(part, "type", "file_data", "filename")
		}
	case entity.ProtocolAnthropicMessages:
		source, _ := part["source"].(map[string]any)
		valid = (part["type"] == "image" || part["type"] == "document") && source != nil && source["type"] == "base64" &&
			teamMediaFields(part, "type", "source", "cache_control", "title", "context", "citations") && teamMediaFields(source, "type", "data", "media_type")
	case entity.ProtocolGeminiGenerateContent:
		for _, field := range []string{"inlineData", "inline_data"} {
			if inline, ok := part[field].(map[string]any); ok {
				valid = teamMediaFields(part, field) && teamMediaFields(inline, "data", "mimeType", "mime_type") && (inline["mimeType"] == nil || inline["mime_type"] == nil)
			}
		}
	}
	if !valid {
		return unsupportedGatewayAttachmentReference()
	}
	return nil
}
