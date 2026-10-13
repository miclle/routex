package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

const insightsAIRequestLimit = 2048

// The fixed report grammar accepts optional transient question text, never
// caller-defined facts, query plans, native messages or conversation history.
func decodeInsightsAIInput(raw []byte) (service.InsightsAIInput, error) {
	var input service.InsightsAIInput
	if len(raw) == 0 || len(raw) > insightsAIRequestLimit || !utf8.Valid(raw) {
		return input, apperrors.ErrBadRequest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return input, apperrors.ErrBadRequest
	}
	fields := map[string]bool{"report": true, "model_id": true, "from": true, "to": true, "timezone": true, "language": true, "question": true}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !fields[name] || seen[name] {
			return input, apperrors.ErrBadRequest
		}
		seen[name] = true
		var rawValue json.RawMessage
		if decoder.Decode(&rawValue) != nil || bytes.Equal(bytes.TrimSpace(rawValue), []byte("null")) {
			return input, apperrors.ErrBadRequest
		}
		if name == "question" {
			if _, err := service.DecodeInsightsAIQuestion(rawValue); err != nil {
				return input, err
			}
		}
		var text string
		if json.Unmarshal(rawValue, &text) != nil {
			return input, apperrors.ErrBadRequest
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return input, apperrors.ErrBadRequest
	}
	for _, field := range []string{"report", "model_id", "from", "to", "timezone", "language"} {
		if !seen[field] {
			return input, apperrors.ErrBadRequest
		}
	}
	if json.Unmarshal(raw, &input) != nil {
		return input, apperrors.ErrBadRequest
	}
	return input, nil
}
func (ctrl *Ctrl) insightsAISession(c *fox.Context, csrf bool) (*service.PersonalSessionIdentity, error) {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return nil, apperrors.ErrBadRequest
	}
	for _, header := range []string{"Authorization", "x-api-key", "x-goog-api-key"} {
		if len(c.Request.Header.Values(header)) != 0 {
			return nil, apperrors.ErrForbidden
		}
	}
	value, count := "", 0
	for _, cookie := range c.Request.Cookies() {
		if cookie.Name == sessionCookie {
			value, count = cookie.Value, count+1
		}
	}
	if count != 1 {
		return nil, apperrors.ErrUnauthorized
	}
	identity, err := ctrl.service.RuntimeAuthenticatePersonalSession(c.Request.Context(), value)
	if err != nil {
		return nil, err
	}
	if csrf && (len(c.Request.Header.Values("X-CSRF-Token")) != 1 || !service.ValidatePersonalSessionCSRF(identity, c.Request.Header.Get("X-CSRF-Token"))) {
		return nil, apperrors.ErrForbidden
	}
	return identity, nil
}
func writeInsightsAIError(c *fox.Context, err error) {
	var native *service.GatewayError
	if errors.As(err, &native) {
		status := native.Status
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
		// Native/provider bodies are never an administrative diagnostic channel.
		c.JSON(status, ErrorResponse{Code: status, Message: "The analysis request could not be completed."})
		return
	}
	renderAPIError(c, err)
}
func (ctrl *Ctrl) InsightsAIModels(c *fox.Context) {
	identity, err := ctrl.insightsAISession(c, false)
	if err != nil {
		writeInsightsAIError(c, err)
		return
	}
	view, err := ctrl.service.InsightsAIModels(c.Request.Context(), identity)
	if err != nil {
		writeInsightsAIError(c, err)
		return
	}
	c.JSON(http.StatusOK, view)
}
func (ctrl *Ctrl) AnalyzeInsightsAI(c *fox.Context) {
	started := time.Now().UTC()
	requestID, err := prepareGateway(c)
	if err != nil {
		writeInsightsAIError(c, err)
		return
	}
	identity, err := ctrl.insightsAISession(c, true)
	if err != nil {
		writeInsightsAIError(c, err)
		return
	}
	if c.ContentType() != "application/json" {
		writeInsightsAIError(c, apperrors.ErrBadRequest)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, insightsAIRequestLimit))
	if err != nil {
		writeInsightsAIError(c, apperrors.ErrBadRequest)
		return
	}
	input, err := decodeInsightsAIInput(raw)
	if err != nil {
		writeInsightsAIError(c, err)
		return
	}
	clientIP, err := ctrl.service.GatewayClientIP(c.Request)
	if err != nil {
		writeInsightsAIError(c, apperrors.ErrBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(service.WithGatewayClientIP(c.Request.Context(), clientIP), 5*time.Minute)
	defer cancel()
	view, result, callErr := ctrl.service.AnalyzeInsightsAI(ctx, identity, input, requestID)
	ctrl.finishInsightsAI(c, ctx, requestID, started, identity, input.ModelID, input.Report, view, result, callErr)
}
func (ctrl *Ctrl) finishInsightsAI(c *fox.Context, ctx context.Context, requestID string, started time.Time, identity *service.PersonalSessionIdentity, modelID, report string, view *service.InsightsAIResponse, result *service.GatewayResult, callErr error) {
	usage, text, responseErr := readInsightsAIResponse(result)
	if callErr == nil {
		callErr = responseErr
	}
	defer func() { ctrl.recordGatewayCall(ctx, requestID, started, result, usage, callErr) }()
	if callErr == nil && view != nil {
		view.Completion, view.Text = usage.NativeCompletionEvidence, text
	}
	if ctx.Err() != nil {
		callErr = &service.GatewayError{Status: 503, Code: "canceled", Message: "The analysis request ended before completion."}
	}
	if callErr != nil {
		writeInsightsAIError(c, callErr)
		return
	}
	if view == nil {
		callErr = invalidGatewayResponse()
		writeInsightsAIError(c, callErr)
		return
	}
	encoded, err := json.Marshal(view)
	if err != nil || len(encoded) > service.InsightsAIResponseLimit {
		callErr = invalidGatewayResponse()
		writeInsightsAIError(c, callErr)
		return
	}
	// Publication authority is fresh after native work. A denial discards private
	// output without changing the actual call outcome recorded by the defer.
	if err := ctrl.service.ReauthorizeInsightsAIReport(ctx, identity, modelID, report); err != nil {
		writeInsightsAIError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/json", encoded)
}
func readInsightsAIResponse(result *service.GatewayResult) (gatewayObservation, *string, error) {
	usage := observeGatewayUsage(service.GatewayUsage{})
	if result == nil || result.Response == nil || result.Response.Body == nil {
		return usage, nil, invalidGatewayResponse()
	}
	defer func() { _ = result.Response.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(result.Response.Body, gatewayResponseLimit+1))
	var text *string
	var contentErr error
	if len(raw) <= gatewayResponseLimit {
		// Economic usage and parser-owned completion survive content/read failures.
		usage, text, contentErr = observeInsightsAIContent(raw)
	}
	if readErr != nil || len(raw) > gatewayResponseLimit || result.Stream || result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return usage, nil, invalidGatewayResponse()
	}
	return usage, text, contentErr
}
func observeInsightsAIContent(raw []byte) (gatewayObservation, *string, error) {
	usage := parseGatewayUsage(raw, 1)
	text, err := insightsAIText(raw, usage.NativeCompletionEvidence)
	return usage, text, err
}
func insightsAIText(raw []byte, evidence string) (*string, error) {
	var response struct {
		Object  string          `json:"object"`
		Error   json.RawMessage `json:"error"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Object != "chat.completion" || len(response.Choices) != 1 || response.Choices[0].Index != 0 || response.Choices[0].Message.Role != "assistant" || len(response.Error) != 0 && !bytes.Equal(response.Error, []byte("null")) {
		return nil, invalidGatewayResponse()
	}
	switch evidence {
	case "unknown", "handoff", "blocked", "incomplete":
		return nil, nil
	case "completed":
		var text string
		if bytes.Equal(response.Choices[0].Message.Content, []byte("null")) || json.Unmarshal(response.Choices[0].Message.Content, &text) != nil || !utf8.ValidString(text) || strings.TrimSpace(text) == "" || len(text) > service.InsightsAITextLimit {
			return nil, invalidGatewayResponse()
		}
		return &text, nil
	default:
		return nil, invalidGatewayResponse()
	}
}
