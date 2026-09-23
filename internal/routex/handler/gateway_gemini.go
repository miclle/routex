package handler

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func writeGeminiError(c *fox.Context, err error, requestID string) {
	status := 502
	kind := "INTERNAL"
	message := "The request could not be completed."
	var gateway *service.GatewayError
	if errors.As(err, &gateway) {
		status = gateway.Status
		message = gateway.Message
		switch status {
		case 400, 413, 415:
			kind = "INVALID_ARGUMENT"
		case 401:
			kind = "UNAUTHENTICATED"
		case 403:
			kind = "PERMISSION_DENIED"
		case 404:
			kind = "NOT_FOUND"
		case 429:
			kind = "RESOURCE_EXHAUSTED"
		case 503:
			kind = "UNAVAILABLE"
		case 504:
			kind = "DEADLINE_EXCEEDED"
		}
		if gateway.NativeError != nil {
			c.JSON(status, gateway.NativeError)
			return
		}
	}
	c.JSON(status, map[string]any{"error": map[string]any{"code": status, "status": kind, "message": message}})
}
func (ctrl *Ctrl) GatewayGemini(c *fox.Context) {
	started := time.Now().UTC()
	requestID, err := prepareGateway(c)
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	model, stream, err := service.GeminiAction(c.Param("model_action"))
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	bearer, err := geminiRequestCredentials(c.Request, stream)
	if err != nil {
		writeGeminiError(c, err, requestID)
		return
	}
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		writeGeminiError(c, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "A valid client network address is required."}, requestID)
		return
	}
	contentType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeGeminiError(c, &service.GatewayError{Status: 415, Code: "invalid_request_error", Message: "Content-Type must be application/json."}, requestID)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, gatewayRequestLimit))
	if err != nil {
		status := 400
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			status = 413
		}
		writeGeminiError(c, &service.GatewayError{Status: status, Code: "invalid_request_error", Message: "The request body is invalid or too large."}, requestID)
		return
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	defer cancel()
	result, callErr := ctrl.service.GatewayGemini(ctx, bearer, body, requestID, model, stream)
	var usage gatewayUsage
	defer func() { ctrl.recordGatewayCall(ctx, requestID, started, result, usage, callErr) }()
	if result != nil && result.Response != nil {
		defer func() { _ = result.Response.Body.Close() }()
	}
	if callErr != nil {
		writeGeminiError(c, callErr, requestID)
		return
	}
	if result.Stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("X-Accel-Buffering", "no")
		c.Status(result.Response.StatusCode)
		count, _ := service.GeminiCandidateCount(body)
		usage, callErr = proxyGeminiStream(ctx, c.Writer, result.Response.Body, result.ModelName, requestID, count)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(result.Response.Body, gatewayResponseLimit+1))
	if err != nil || len(raw) > gatewayResponseLimit {
		callErr = invalidGatewayResponse()
		writeGeminiError(c, callErr, requestID)
		return
	}
	count, _ := service.GeminiCandidateCount(body)
	state := geminiStreamState{expected: count}
	encoded, callErr := state.object(raw, result.ModelName, requestID)
	if callErr == nil && !state.finished() {
		callErr = invalidGatewayResponse()
	}
	if callErr != nil {
		writeGeminiError(c, callErr, requestID)
		return
	}
	usage = service.ParseGeminiUsage(encoded, true)
	c.Data(result.Response.StatusCode, "application/json", encoded)
}
