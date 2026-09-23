package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/fox-gonic/fox"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
)

const gatewayRequestLimit = 4 << 20
const gatewayResponseLimit = 16 << 20
const gatewayEventLimit = 1 << 20

func (ctrl *Ctrl) GatewayModels(c *fox.Context) {
	_, err := prepareGateway(c)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	models, err := ctrl.service.GatewayModels(c.Request.Context(), gatewayBearer(c.Request))
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	c.JSON(http.StatusOK, map[string]any{"object": "list", "data": models})
}

func (ctrl *Ctrl) GatewayChat(c *fox.Context) {
	started := time.Now().UTC()
	requestID, err := prepareGateway(c)
	if err != nil {
		writeGatewayError(c, err)
		return
	}
	mediaType, _, err := mime.ParseMediaType(c.Request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
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
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	result, callErr := ctrl.service.GatewayChat(ctx, gatewayBearer(c.Request), body, requestID)
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
		c.Status(http.StatusOK)
		usage, callErr = proxyGatewayStream(ctx, c.Writer, result.Response.Body, result.ModelName)
		return
	}
	response, err := io.ReadAll(io.LimitReader(result.Response.Body, gatewayResponseLimit+1))
	if err != nil || len(response) > gatewayResponseLimit {
		callErr = invalidGatewayResponse()
		writeGatewayError(c, callErr)
		return
	}
	response, callErr = rewriteGatewayModel(response, result.ModelName)
	if callErr != nil {
		writeGatewayError(c, callErr)
		return
	}
	usage = parseGatewayUsage(response)
	c.Data(http.StatusOK, "application/json", response)
}

func (ctrl *Ctrl) recordGatewayCall(ctx context.Context, requestID string, started time.Time, result *service.GatewayResult, usage gatewayUsage, callErr error) {
	if result == nil || result.UserID == "" {
		return
	}
	status, code := "success", ""
	if callErr != nil {
		status, code = "error", "upstream_error"
		var public *service.GatewayError
		if errors.As(callErr, &public) {
			code = public.Code
		}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		status, code = "canceled", "canceled"
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status, code = "error", "upstream_timeout"
	}
	completed := time.Now().UTC()
	fact := service.CallFact{RequestID: requestID, SnapshotID: result.SnapshotID, UserID: result.UserID, KeyID: result.KeyID, ModelID: result.ModelID, ModelName: result.ModelName, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, Protocol: entity.ProtocolOpenAIChat, Status: status, Stream: result.Stream, StartedAt: started, CompletedAt: completed, InputTokens: usage.Input, OutputTokens: usage.Output, ErrorCode: code}
	if result.AttemptID != "" {
		httpStatus := 0
		if result.Response != nil {
			httpStatus = result.Response.StatusCode
		}
		fact.Attempts = []service.CallAttempt{{ID: result.AttemptID, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, Status: status, StartedAt: result.AttemptStartedAt, CompletedAt: completed, HTTPStatus: httpStatus, ErrorCode: code}}
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := ctrl.service.PersistGatewayCall(recordCtx, fact); err != nil {
		// The response may already be streaming. A bounded failure cannot be
		// reported as successful persistence or change the completed response.
		log.Printf("gateway call recording failed (request_id=%s)", requestID)
	}
}

type gatewayUsage struct{ Input, Output *int64 }

func parseGatewayUsage(raw []byte) gatewayUsage {
	var payload struct {
		Usage *struct {
			Input  *int64 `json:"prompt_tokens"`
			Output *int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Usage == nil {
		return gatewayUsage{}
	}
	usage := gatewayUsage{Input: payload.Usage.Input, Output: payload.Usage.Output}
	if usage.Input != nil && *usage.Input < 0 {
		usage.Input = nil
	}
	if usage.Output != nil && *usage.Output < 0 {
		usage.Output = nil
	}
	return usage
}

func prepareGateway(c *fox.Context) (string, error) {
	requestID, err := id.NewPrefixed("req")
	if err != nil {
		return "", &service.GatewayError{Status: 500, Code: "internal_error", Message: "The request could not be initialized."}
	}
	c.Header("X-Request-ID", requestID)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	return requestID, nil
}

func gatewayBearer(req *http.Request) string {
	if len(req.Header.Values("Authorization")) != 1 {
		return ""
	}
	parts := strings.Fields(req.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func invalidGatewayResponse() *service.GatewayError {
	return &service.GatewayError{Status: 502, Code: "invalid_upstream_response", Message: "The upstream returned an invalid or incomplete response."}
}

func gatewayErrorBody(err error) (int, map[string]any) {
	public := &service.GatewayError{Status: 500, Code: "internal_error", Message: "The request could not be completed."}
	var known *service.GatewayError
	if errors.As(err, &known) {
		public = known
	}
	kind := "api_error"
	if public.Status < 500 {
		kind = "invalid_request_error"
	}
	if public.Status == 429 {
		kind = "rate_limit_error"
	}
	return public.Status, map[string]any{"error": map[string]any{"message": public.Message, "type": kind, "code": public.Code}}
}

func writeGatewayError(c *fox.Context, err error) {
	status, body := gatewayErrorBody(err)
	if status == 401 {
		c.Header("WWW-Authenticate", "Bearer")
	}
	c.JSON(status, body)
}

func rewriteGatewayModel(raw []byte, model string) ([]byte, error) {
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		return nil, invalidGatewayResponse()
	}
	if value, exists := data["error"]; exists && !bytes.Equal(value, []byte("null")) {
		return nil, invalidGatewayResponse()
	}
	if _, exists := data["model"]; exists {
		encoded, err := json.Marshal(model)
		if err != nil {
			return nil, err
		}
		data["model"] = encoded
	}
	return json.Marshal(data)
}

func proxyGatewayStream(ctx context.Context, writer http.ResponseWriter, body io.Reader, model string) (gatewayUsage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), gatewayEventLimit)
	var event bytes.Buffer
	var usage gatewayUsage
	write := func(data []byte) error {
		controller := http.NewResponseController(writer)
		if err := controller.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		return controller.Flush()
	}
	failed := func() (gatewayUsage, error) {
		if ctx.Err() != nil {
			return usage, ctx.Err()
		}
		_, public := gatewayErrorBody(invalidGatewayResponse())
		encoded, err := json.Marshal(public)
		if err == nil {
			_ = write(append(append([]byte("data: "), encoded...), '\n', '\n'))
		}
		return usage, invalidGatewayResponse()
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return usage, ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			if event.Len() == 0 {
				continue
			}
			data, terminal, err := rewriteGatewayEvent(event.Bytes(), model)
			event.Reset()
			if err != nil {
				return failed()
			}
			if len(data) != 0 {
				if !terminal {
					observed := parseGatewayUsage(bytes.TrimSpace(bytes.TrimPrefix(data, []byte("data: "))))
					if observed.Input != nil {
						usage.Input = observed.Input
					}
					if observed.Output != nil {
						usage.Output = observed.Output
					}
				}
				if err := write(data); err != nil {
					return usage, err
				}
			}
			if terminal {
				return usage, nil
			}
			continue
		}
		if event.Len()+len(line)+1 > gatewayEventLimit {
			return failed()
		}
		event.Write(line)
		event.WriteByte('\n')
	}
	// Missing [DONE] is an incomplete stream, even after a clean transport EOF.
	return failed()
}

func rewriteGatewayEvent(event []byte, model string) ([]byte, bool, error) {
	var data []string
	for _, line := range strings.Split(string(event), "\n") {
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(data) == 0 {
		return nil, false, nil
	}
	payload := strings.Join(data, "\n")
	if payload == "[DONE]" {
		return []byte("data: [DONE]\n\n"), true, nil
	}
	encoded, err := rewriteGatewayModel([]byte(payload), model)
	if err != nil {
		return nil, false, err
	}
	return append(append([]byte("data: "), encoded...), '\n', '\n'), false, nil
}
