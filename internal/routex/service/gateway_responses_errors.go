package service

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
)

// SanitizeResponsesError retains recognized native identifiers without trusting
// arbitrary upstream strings, which may include a credential or diagnostic body.
func SanitizeResponsesError(raw json.RawMessage) map[string]any {
	object := usageObject(raw)
	result := map[string]any{"message": "The upstream could not complete the request.", "code": "upstream_error"}
	var code, kind, param string
	_ = json.Unmarshal(object["code"], &code)
	if slices.Contains([]string{"server_error", "rate_limit_exceeded", "invalid_api_key", "insufficient_quota", "model_not_found", "context_length_exceeded", "invalid_value", "unsupported_parameter", "missing_required_parameter", "content_policy_violation", "invalid_prompt", "invalid_image", "invalid_image_format", "image_too_large", "image_too_small", "invalid_base64_image", "invalid_image_url", "invalid_file", "vector_store_timeout"}, code) {
		result["code"] = code
	}
	_ = json.Unmarshal(object["type"], &kind)
	if slices.Contains([]string{"invalid_request_error", "rate_limit_error", "server_error", "api_error", "authentication_error", "permission_error", "not_found_error"}, kind) {
		result["type"] = kind
	}
	if _, exists := object["param"]; exists {
		result["param"] = nil
		_ = json.Unmarshal(object["param"], &param)
		if slices.Contains([]string{"model", "input", "tools", "tool_choice", "reasoning", "text", "stream", "stream_options", "store", "background", "max_output_tokens", "temperature", "top_p", "service_tier", "previous_response_id", "conversation"}, param) {
			result["param"] = param
		}
	}
	return result
}
func nativeResponsesHTTPError(response *http.Response) *GatewayError {
	status := response.StatusCode
	if status < 400 || status > 599 {
		status = 502
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	native := SanitizeResponsesError(nil)
	if err == nil && len(body) <= 1<<20 {
		if object := usageObject(body); object != nil {
			native = SanitizeResponsesError(object["error"])
		}
	}
	if _, exists := native["type"]; !exists {
		native["type"] = "api_error"
	}
	code := "upstream_error"
	if status == 429 {
		code = "rate_limit_exceeded"
	}
	if status == 408 || status == 504 {
		code = "upstream_timeout"
	}
	return &GatewayError{Status: status, Code: code, Message: "The upstream could not complete the request.", NativeError: map[string]any{"error": native}}
}
