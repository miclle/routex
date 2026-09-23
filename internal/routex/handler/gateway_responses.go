package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"slices"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GatewayResponses(c *fox.Context) {
	started := time.Now().UTC()
	requestID, err := prepareGateway(c)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		writeGatewayError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "A valid client network address is required."})
		return
	}
	contentType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeGatewayError(c, &service.GatewayError{Status: 415, Code: "invalid_request_error", Message: "Content-Type must be application/json."})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestLimit))
	if err != nil {
		status := 400
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = 413
		}
		writeGatewayError(c, &service.GatewayError{Status: status, Code: "invalid_request_error", Message: "The request body is invalid or too large."})
		return
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	defer cancel()
	result, callErr := ctrl.service.GatewayResponses(ctx, gatewayBearer(c.Request), body, requestID)
	var usage gatewayUsage
	defer func() { ctrl.recordGatewayCall(ctx, requestID, started, result, usage, callErr) }()
	if result != nil && result.Response != nil {
		defer func() { _ = result.Response.Body.Close() }()
	}
	if callErr != nil {
		writeGatewayError(c, callErr)
		return
	}
	if result.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Accel-Buffering", "no")
		c.Status(result.Response.StatusCode)
		usage, callErr = proxyResponsesStream(ctx, c.Writer, result.Response.Body, result.ModelName)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(result.Response.Body, gatewayResponseLimit+1))
	if err != nil || len(raw) > gatewayResponseLimit {
		callErr = invalidGatewayResponse()
		writeGatewayError(c, callErr)
		return
	}
	encoded, status, err := rewriteResponsesObject(raw, result.ModelName, false)
	if err != nil {
		callErr = err
		writeGatewayError(c, callErr)
		return
	}
	usage = service.ParseResponsesUsage(encoded)
	callErr = responsesTerminalError(status)
	c.Data(result.Response.StatusCode, "application/json", encoded)
}

func responsesTerminalError(status string) error {
	switch status {
	case "completed":
		return nil
	case "cancelled":
		return &service.GatewayError{Status: 502, Code: "canceled", Message: "The upstream response was canceled."}
	case "failed":
		return &service.GatewayError{Status: 502, Code: "upstream_error", Message: "The upstream response failed."}
	default:
		return invalidGatewayResponse()
	}
}
func rewriteResponsesObject(raw []byte, model string, allowNonterminal bool) ([]byte, string, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, "", invalidGatewayResponse()
	}
	var kind, status, responseID, upstreamModel string
	if json.Unmarshal(object["object"], &kind) != nil || kind != "response" || json.Unmarshal(object["status"], &status) != nil || json.Unmarshal(object["id"], &responseID) != nil || responseID == "" || len(responseID) > 256 || json.Unmarshal(object["model"], &upstreamModel) != nil || upstreamModel == "" {
		return nil, "", invalidGatewayResponse()
	}
	var output []json.RawMessage
	if json.Unmarshal(object["output"], &output) != nil || output == nil {
		return nil, "", invalidGatewayResponse()
	}
	for index := range output {
		output[index] = sanitizeResponsesItem(output[index])
	}
	object["output"] = mustJSON(output)
	allowed := []string{"completed", "failed", "incomplete", "cancelled"}
	if allowNonterminal {
		allowed = append(allowed, "in_progress", "queued")
	}
	if !slices.Contains(allowed, status) {
		return nil, "", invalidGatewayResponse()
	}
	encodedModel, _ := json.Marshal(model)
	object["model"] = encodedModel
	if value, exists := object["error"]; exists && string(value) != "null" {
		if status == "completed" {
			return nil, "", invalidGatewayResponse()
		}
		safe := service.SanitizeResponsesError(value)
		encoded, _ := json.Marshal(safe)
		object["error"] = encoded
	}
	if value, exists := object["incomplete_details"]; exists && string(value) != "null" {
		var detail struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(value, &detail)
		if !slices.Contains([]string{"max_output_tokens", "content_filter"}, detail.Reason) {
			detail.Reason = "upstream_error"
		}
		encoded, _ := json.Marshal(detail)
		object["incomplete_details"] = encoded
	}
	encoded, err := json.Marshal(object)
	return encoded, status, err
}

// Tool-result error fields are provider diagnostics; function arguments and
// message content remain opaque native user/model data.
func sanitizeResponsesItem(raw json.RawMessage) json.RawMessage {
	var item map[string]json.RawMessage
	if json.Unmarshal(raw, &item) != nil || item == nil {
		return raw
	}
	if value, exists := item["error"]; exists && string(value) != "null" {
		var message string
		if json.Unmarshal(value, &message) == nil {
			item["error"] = mustJSON("The upstream tool could not complete the request.")
		} else {
			item["error"] = mustJSON(service.SanitizeResponsesError(value))
		}
		return mustJSON(item)
	}
	return raw
}
