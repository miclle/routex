package service

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
)

func SanitizeGeminiError(raw json.RawMessage, status int) map[string]any {
	object := usageObject(raw)
	var kind string
	_ = json.Unmarshal(object["status"], &kind)
	if !slices.Contains([]string{"INVALID_ARGUMENT", "FAILED_PRECONDITION", "UNAUTHENTICATED", "PERMISSION_DENIED", "NOT_FOUND", "RESOURCE_EXHAUSTED", "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED", "CANCELLED", "OUT_OF_RANGE", "UNIMPLEMENTED"}, kind) {
		kind = "INTERNAL"
	}
	return map[string]any{"code": status, "status": kind, "message": "The upstream could not complete the request."}
}
func nativeGeminiHTTPError(response *http.Response) *GatewayError {
	status := response.StatusCode
	if status < 400 || status > 599 {
		status = 502
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	safe := SanitizeGeminiError(nil, status)
	if err == nil && len(body) <= 1<<20 {
		safe = SanitizeGeminiError(usageObject(body)["error"], status)
	}
	code := "upstream_error"
	if status == 429 {
		code = "rate_limit_exceeded"
	}
	if status == 408 || status == 504 {
		code = "upstream_timeout"
	}
	return &GatewayError{Status: status, Code: code, Message: "The upstream could not complete the request.", NativeError: map[string]any{"error": safe}}
}
