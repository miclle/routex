package service

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
)

func SanitizeMessagesError(raw json.RawMessage) map[string]any {
	object := usageObject(raw)
	var kind string
	_ = json.Unmarshal(object["type"], &kind)
	if !slices.Contains([]string{"invalid_request_error", "authentication_error", "permission_error", "not_found_error", "conflict_error", "request_too_large", "rate_limit_error", "api_error", "timeout_error", "overloaded_error"}, kind) {
		kind = "api_error"
	}
	return map[string]any{"type": kind, "message": "The upstream could not complete the request."}
}
func nativeMessagesHTTPError(response *http.Response) *GatewayError {
	status := response.StatusCode
	if status < 400 || status > 599 {
		status = 502
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	safe := SanitizeMessagesError(nil)
	if err == nil && len(body) <= 1<<20 {
		safe = SanitizeMessagesError(usageObject(body)["error"])
	}
	code := "upstream_error"
	if status == 429 {
		code = "rate_limit_exceeded"
	}
	if status == 408 || status == 504 {
		code = "upstream_timeout"
	}
	return &GatewayError{Status: status, Code: code, Message: "The upstream could not complete the request.", NativeError: map[string]any{"type": "error", "error": safe}}
}
