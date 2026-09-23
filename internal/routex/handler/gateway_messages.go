package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func messagesRequestHeaders(request *http.Request) (string, service.MessagesHeaders, error) {
	invalid := &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Native authentication, version or beta headers are invalid."}
	headers := service.MessagesHeaders{Version: request.Header.Get("anthropic-version"), Beta: strings.Join(request.Header.Values("anthropic-beta"), ",")}
	if len(request.Header.Values("anthropic-version")) != 1 || len(request.Header.Values("x-api-key")) > 1 || len(request.Header.Values("Authorization")) > 1 {
		return "", headers, invalid
	}
	for _, name := range []string{"anthropic-workspace-id", "anthropic-user-profile-id"} {
		if len(request.Header.Values(name)) > 0 {
			return "", headers, invalid
		}
	}
	bearer := gatewayBearer(request)
	native := request.Header.Get("x-api-key")
	if native != "" {
		if request.Header.Get("Authorization") != "" {
			return "", headers, invalid
		}
		bearer = native
	}
	if err := service.ValidateMessagesHeaders(headers); err != nil {
		return "", headers, err
	}
	return bearer, headers, nil
}
func writeMessagesError(c *fox.Context, err error, requestID string) {
	status := 502
	kind := "api_error"
	message := "The request could not be completed."
	var gateway *service.GatewayError
	if errors.As(err, &gateway) {
		status, message = gateway.Status, gateway.Message
		switch status {
		case 400:
			kind = "invalid_request_error"
		case 401:
			kind = "authentication_error"
		case 403:
			kind = "permission_error"
		case 404:
			kind = "not_found_error"
		case 413:
			kind = "request_too_large"
		case 429:
			kind = "rate_limit_error"
		case 504:
			kind = "timeout_error"
		case 529:
			kind = "overloaded_error"
		}
		if gateway.NativeError != nil {
			safe := map[string]any{"type": "error", "error": gateway.NativeError["error"], "request_id": requestID}
			c.JSON(status, safe)
			return
		}
	}
	c.JSON(status, map[string]any{"type": "error", "error": map[string]string{"type": kind, "message": message}, "request_id": requestID})
}
func (ctrl *Ctrl) GatewayMessages(c *fox.Context) {
	started := time.Now().UTC()
	requestID, err := prepareGateway(c)
	if err != nil {
		writeMessagesError(c, err, requestID)
		return
	}
	c.Header("request-id", requestID)
	bearer, headers, err := messagesRequestHeaders(c.Request)
	if err != nil {
		writeMessagesError(c, err, requestID)
		return
	}
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		writeMessagesError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "A valid client network address is required."}, requestID)
		return
	}
	contentType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeMessagesError(c, &service.GatewayError{Status: 415, Code: "invalid_request_error", Message: "Content-Type must be application/json."}, requestID)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestLimit))
	if err != nil {
		status := 400
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = 413
		}
		writeMessagesError(c, &service.GatewayError{Status: status, Code: "invalid_request_error", Message: "The request body is invalid or too large."}, requestID)
		return
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	defer cancel()
	result, callErr := ctrl.service.GatewayMessages(ctx, bearer, body, requestID, headers)
	var usage gatewayUsage
	defer func() { ctrl.recordGatewayCall(ctx, requestID, started, result, usage, callErr) }()
	if result != nil && result.Response != nil {
		defer func() { _ = result.Response.Body.Close() }()
	}
	if callErr != nil {
		writeMessagesError(c, callErr, requestID)
		return
	}
	if result.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Accel-Buffering", "no")
		c.Status(result.Response.StatusCode)
		usage, callErr = proxyMessagesStream(ctx, c.Writer, result.Response.Body, result.ModelName, requestID)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(result.Response.Body, gatewayResponseLimit+1))
	if err != nil || len(raw) > gatewayResponseLimit {
		callErr = invalidGatewayResponse()
		writeMessagesError(c, callErr, requestID)
		return
	}
	encoded, callErr := rewriteMessagesObject(raw, result.ModelName, true)
	if callErr != nil {
		writeMessagesError(c, callErr, requestID)
		return
	}
	usage = service.ParseMessagesUsage(encoded, true)
	c.Data(result.Response.StatusCode, "application/json", encoded)
}
func rewriteMessagesObject(raw []byte, model string, final bool) ([]byte, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, invalidGatewayResponse()
	}
	var kind, role, identity, upstreamModel string
	if json.Unmarshal(object["type"], &kind) != nil || kind != "message" || json.Unmarshal(object["role"], &role) != nil || role != "assistant" || json.Unmarshal(object["id"], &identity) != nil || identity == "" || len(identity) > 256 || json.Unmarshal(object["model"], &upstreamModel) != nil || upstreamModel == "" {
		return nil, invalidGatewayResponse()
	}
	var content []json.RawMessage
	if json.Unmarshal(object["content"], &content) != nil || content == nil {
		return nil, invalidGatewayResponse()
	}
	if !final && len(content) != 0 {
		return nil, invalidGatewayResponse()
	}
	if final && !validMessagesStop(object["stop_reason"]) {
		return nil, invalidGatewayResponse()
	}
	object["model"] = mustJSON(model)
	return json.Marshal(object)
}
func validMessagesStop(raw json.RawMessage) bool {
	var stop string
	return json.Unmarshal(raw, &stop) == nil && responseEventName.MatchString(stop)
}
