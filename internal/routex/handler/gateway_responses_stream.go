package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/service"
)

var responseEventName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type responsesEvent struct {
	Bytes      []byte
	Response   []byte
	ResponseID string
	Terminal   bool
	Status     string
	Sequence   *int64
}

func rewriteResponsesEvent(raw []byte, model string) (responsesEvent, error) {
	result := responsesEvent{}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	data := []string{}
	name := ""
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		case strings.HasPrefix(line, "event:"):
			name = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
		}
	}
	if len(data) == 0 {
		result.Bytes = append(bytes.Clone(raw), '\n')
		return result, nil
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.Join(data, "\n")), &envelope) != nil || envelope == nil {
		return result, invalidGatewayResponse()
	}
	var kind string
	if json.Unmarshal(envelope["type"], &kind) != nil || !responseEventName.MatchString(kind) || (name != "" && name != kind) {
		return result, invalidGatewayResponse()
	}
	if raw, exists := envelope["sequence_number"]; exists {
		var sequence int64
		if json.Unmarshal(raw, &sequence) != nil || sequence < 0 {
			return result, invalidGatewayResponse()
		}
		result.Sequence = &sequence
	}
	if kind == "error" {
		safe := service.SanitizeResponsesError(mustJSON(envelope))
		safe["type"] = "error"
		if result.Sequence != nil {
			safe["sequence_number"] = *result.Sequence
		}
		encoded, _ := json.Marshal(safe)
		envelope = map[string]json.RawMessage{}
		_ = json.Unmarshal(encoded, &envelope)
		result.Terminal = true
		result.Status = "failed"
	} else if response, exists := envelope["response"]; exists {
		encoded, status, err := rewriteResponsesObject(response, model, true)
		if err != nil {
			return result, err
		}
		envelope["response"] = encoded
		result.Response = encoded
		result.Status = status
		var identity struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(encoded, &identity)
		result.ResponseID = identity.ID
	}
	if value, exists := envelope["error"]; exists && string(value) != "null" {
		envelope["error"] = mustJSON(service.SanitizeResponsesError(value))
	}
	if item, exists := envelope["item"]; exists {
		envelope["item"] = sanitizeResponsesItem(item)
	}
	terminalStatus := map[string]string{"response.completed": "completed", "response.failed": "failed", "response.incomplete": "incomplete"}
	if status, terminal := terminalStatus[kind]; terminal {
		if result.Response == nil || result.Status != status {
			return result, invalidGatewayResponse()
		}
		result.Terminal = true
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return result, err
	}
	var out bytes.Buffer
	inserted := false
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			if !inserted {
				out.WriteString("data: ")
				out.Write(encoded)
				out.WriteByte('\n')
				inserted = true
			}
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	out.WriteByte('\n')
	result.Bytes = out.Bytes()
	return result, nil
}
func mustJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }

func proxyResponsesStream(ctx context.Context, writer http.ResponseWriter, body io.Reader, model string) (gatewayUsage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), gatewayEventLimit)
	var event bytes.Buffer
	var usage gatewayUsage
	responseID := ""
	var previousSequence *int64
	write := func(raw []byte) error {
		control := http.NewResponseController(writer)
		if err := control.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := writer.Write(raw); err != nil {
			return err
		}
		return control.Flush()
	}
	failed := func() (gatewayUsage, error) {
		if ctx.Err() != nil {
			return usage, ctx.Err()
		}
		payload := map[string]any{"type": "error", "code": "invalid_upstream_response", "message": "The upstream returned an invalid or incomplete response."}
		if previousSequence != nil && *previousSequence < math.MaxInt64 {
			payload["sequence_number"] = *previousSequence + 1
		}
		_ = write(append(append([]byte("event: error\ndata: "), mustJSON(payload)...), '\n', '\n'))
		return usage, invalidGatewayResponse()
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return usage, ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) != 0 {
			if event.Len()+len(line)+1 > gatewayEventLimit {
				return failed()
			}
			event.Write(line)
			event.WriteByte('\n')
			continue
		}
		if event.Len() == 0 {
			continue
		}
		observed, err := rewriteResponsesEvent(event.Bytes(), model)
		event.Reset()
		if err != nil {
			return failed()
		}
		if observed.Sequence != nil {
			if previousSequence != nil && *observed.Sequence <= *previousSequence {
				return failed()
			}
			previousSequence = observed.Sequence
		}
		if observed.ResponseID != "" {
			if responseID != "" && responseID != observed.ResponseID {
				return failed()
			}
			responseID = observed.ResponseID
		}
		if observed.Response != nil {
			parsed := service.ParseResponsesUsage(observed.Response)
			if !observed.Terminal {
				parsed.Complete = false
			}
			dimensions := append([]string{}, usage.UnsupportedDimensions...)
			for _, dimension := range parsed.UnsupportedDimensions {
				if !slices.Contains(dimensions, dimension) {
					dimensions = append(dimensions, dimension)
				}
			}
			// Replace the complete frame; never merge partial counters between events.
			usage = parsed
			usage.UnsupportedDimensions = dimensions
			usage.Unsupported = len(dimensions) > 0
		}
		if err := write(observed.Bytes); err != nil {
			return usage, err
		}
		if observed.Terminal {
			return usage, responsesTerminalError(observed.Status)
		}
	}
	return failed()
}
