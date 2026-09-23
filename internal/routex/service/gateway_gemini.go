package service

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

var geminiModelSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Native options are transport metadata, never injected into the provider JSON.
type gatewayNativeOptions struct {
	Messages     MessagesHeaders
	GeminiModel  string
	GeminiStream bool
}

func (s *Service) GatewayGemini(ctx context.Context, bearer string, body []byte, requestID, model string, stream bool) (*GatewayResult, error) {
	return s.gatewayNative(ctx, bearer, body, requestID, "gemini_generate_content", gatewayNativeOptions{GeminiModel: model, GeminiStream: stream})
}

// GeminiAction resolves only a single public model path segment and two native methods.
func GeminiAction(segment string) (string, bool, error) {
	model, action, ok := strings.Cut(segment, ":")
	if !ok || !geminiModelSegment.MatchString(model) || (action != "generateContent" && action != "streamGenerateContent") {
		return "", false, gatewayError(404, "model_not_found", "The native model endpoint is unavailable.")
	}
	return model, action == "streamGenerateContent", nil
}

func parseGatewayGemini(body []byte, model string, stream bool) (map[string]json.RawMessage, string, bool, error) {
	invalid := gatewayError(400, "invalid_request_error", "A public model and native contents are required; provider-owned resources are unsupported.")
	payload := usageObject(body)
	if payload == nil || !geminiModelSegment.MatchString(model) {
		return nil, model, stream, invalid
	}
	// Reject ambiguous protobuf JSON aliases at protocol-owned levels only. User
	// function arguments and JSON schemas remain opaque and are never traversed.
	for _, pair := range [][2]string{{"systemInstruction", "system_instruction"}, {"generationConfig", "generation_config"}, {"cachedContent", "cached_content"}, {"toolConfig", "tool_config"}, {"serviceTier", "service_tier"}} {
		if payload[pair[0]] != nil && payload[pair[1]] != nil {
			return nil, model, stream, invalid
		}
	}
	for _, name := range []string{"cachedContent", "cached_content", "model", "stream", "session", "sessionId", "session_id"} {
		if nativePresent(payload[name]) {
			return nil, model, stream, invalid
		}
	}
	var contents []json.RawMessage
	if json.Unmarshal(payload["contents"], &contents) != nil || len(contents) == 0 {
		return nil, model, stream, invalid
	}
	for _, content := range contents {
		if !safeGeminiContent(content) {
			return nil, model, stream, invalid
		}
	}
	if raw := geminiField(payload, "systemInstruction", "system_instruction"); nativePresent(raw) && !safeGeminiContent(raw) {
		return nil, model, stream, invalid
	}
	var tools []map[string]json.RawMessage
	if raw := payload["tools"]; nativePresent(raw) {
		if json.Unmarshal(raw, &tools) != nil || tools == nil {
			return nil, model, stream, invalid
		}
		for _, tool := range tools {
			for _, name := range []string{"retrieval", "fileSearch", "file_search", "enterpriseWebSearch", "enterprise_web_search", "computerUse", "computer_use"} {
				if nativePresent(tool[name]) {
					return nil, model, stream, invalid
				}
			}
		}
	}
	if _, ok := GeminiCandidateCount(body); !ok {
		return nil, model, stream, invalid
	}
	return payload, model, stream, nil
}
func geminiField(object map[string]json.RawMessage, camel, snake string) json.RawMessage {
	if raw, ok := object[camel]; ok {
		return raw
	}
	return object[snake]
}
func safeGeminiContent(raw json.RawMessage) bool { return safeGeminiContentDepth(raw, 0) }
func safeGeminiContentDepth(raw json.RawMessage, depth int) bool {
	if depth >= 8 {
		return false
	}
	content := usageObject(raw)
	var parts []map[string]json.RawMessage
	if content == nil || json.Unmarshal(content["parts"], &parts) != nil || len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		for _, name := range []string{"fileData", "file_data", "fileUri", "file_uri"} {
			if nativePresent(part[name]) {
				return false
			}
		}
		for _, pair := range [][2]string{{"inlineData", "inline_data"}, {"functionCall", "function_call"}, {"functionResponse", "function_response"}, {"thoughtSignature", "thought_signature"}} {
			if part[pair[0]] != nil && part[pair[1]] != nil {
				return false
			}
		}
		// Function response JSON is opaque; native multimedia parts are not.
		response := usageObject(geminiField(part, "functionResponse", "function_response"))
		if nativePresent(response["parts"]) && !safeGeminiContentDepth(mustGeminiJSON(map[string]json.RawMessage{"parts": response["parts"]}), depth+1) {
			return false
		}
	}
	return true
}
func mustGeminiJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

// RouteX bounds candidate bookkeeping independently of model-specific limits.
func GeminiCandidateCount(body []byte) (int, bool) {
	payload := usageObject(body)
	config := usageObject(geminiField(payload, "generationConfig", "generation_config"))
	if config["candidateCount"] != nil && config["candidate_count"] != nil {
		return 0, false
	}
	raw := geminiField(config, "candidateCount", "candidate_count")
	if raw == nil {
		return 1, true
	}
	count := usageCounter(raw)
	if count == nil || *count < 1 || *count > 8 {
		return 0, false
	}
	return int(*count), true
}
