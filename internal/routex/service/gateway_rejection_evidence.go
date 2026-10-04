package service

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
)

// Rejection evidence uses only native envelope positions. Diagnostic text and
// nested provider details remain opaque; they cannot prove or disprove work.
func gatewayNativeRejectionError(protocol string, status int, body []byte) (json.RawMessage, bool) {
	root, ok := gatewayRejectionObject(body)
	if !ok {
		return nil, false
	}
	var workFields []string
	switch protocol {
	case entity.ProtocolOpenAIChat:
		workFields = []string{"choices", "usage", "id", "object", "created", "model"}
	case entity.ProtocolOpenAIResponses:
		workFields = []string{"response", "output", "output_text", "usage", "status", "incomplete_details", "id", "object", "model", "created_at", "completed_at"}
		if raw, exists := root["type"]; exists {
			kind, valid := gatewayRejectionOptionalString(raw)
			// Native response events are not error-only HTTP rejection envelopes.
			// Their payload may be absent, incomplete or already contain output.
			if !valid || strings.HasPrefix(strings.ToLower(kind), "response.") {
				return nil, false
			}
		}
	case entity.ProtocolAnthropicMessages:
		workFields = []string{"content", "usage", "stop_reason", "stop_sequence", "id", "model", "role"}
		if raw, exists := root["type"]; exists {
			var kind string
			if json.Unmarshal(raw, &kind) != nil || kind != "error" {
				return nil, false
			}
		}
	case entity.ProtocolGeminiGenerateContent:
		workFields = []string{"candidates", "usageMetadata", "promptFeedback", "modelVersion", "responseId"}
	default:
		return nil, false
	}
	for key := range root {
		// Even null/empty/zero success fields conflict with an error-only envelope.
		// Preserve unknown economics rather than guessing that these prove no work.
		for _, field := range workFields {
			if strings.EqualFold(key, field) {
				return nil, false
			}
		}
		if key != "error" && strings.EqualFold(key, "error") || (protocol == entity.ProtocolAnthropicMessages || protocol == entity.ProtocolOpenAIResponses) && key != "type" && strings.EqualFold(key, "type") {
			return nil, false
		}
	}
	raw, exists := root["error"]
	if !exists {
		return nil, false
	}
	fields, ok := gatewayRejectionObject(raw)
	if !ok {
		return nil, false
	}
	for key := range fields {
		for _, field := range []string{"code", "type", "status"} {
			if key != field && strings.EqualFold(key, field) {
				return nil, false
			}
		}
	}
	if !gatewayRejectionDiscriminatorsAgree(protocol, status, fields) {
		return nil, false
	}
	return raw, true
}

// json.Unmarshal otherwise accepts duplicate object members with last-value
// semantics. Evidence must have one interpretation at the envelope/error level.
// Decode opaque child values without recursively inspecting their member names.
func gatewayRejectionObject(raw []byte) (map[string]json.RawMessage, bool) {
	if !utf8.Valid(raw) {
		return nil, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, false
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, false
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, false
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, false
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, false
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, false
	}
	return fields, true
}

// Discrimination fields must not give a different interpretation of the same
// rejection. This does not close or inspect arbitrary provider diagnostics.
func gatewayRejectionDiscriminatorsAgree(protocol string, status int, fields map[string]json.RawMessage) bool {
	auth := status == http.StatusUnauthorized
	expectedCode, expectedType, expectedStatus := "rate_limit_exceeded", "rate_limit_error", "RESOURCE_EXHAUSTED"
	if auth {
		expectedCode, expectedType, expectedStatus = "invalid_api_key", "authentication_error", "UNAUTHENTICATED"
	}
	if protocol == entity.ProtocolGeminiGenerateContent {
		if raw, present := fields["code"]; present {
			// Gemini uses an integer HTTP error code. null/strings/fractions cannot
			// confirm that the native status and HTTP rejection describe one outcome.
			var code int
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &code) != nil || code != status {
				return false
			}
		}
	} else {
		code, ok := gatewayRejectionOptionalString(fields["code"])
		if !ok || code != "" && code != expectedCode {
			return false
		}
	}
	kind, ok := gatewayRejectionOptionalString(fields["type"])
	genericOpenAI := (protocol == entity.ProtocolOpenAIChat || protocol == entity.ProtocolOpenAIResponses) && kind == "invalid_request_error"
	if !ok || kind != "" && kind != expectedType && !genericOpenAI {
		return false
	}
	nativeStatus, ok := gatewayRejectionOptionalString(fields["status"])
	return ok && (nativeStatus == "" || nativeStatus == expectedStatus)
}

func gatewayRejectionOptionalString(raw json.RawMessage) (string, bool) {
	if raw == nil {
		return "", true
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}
