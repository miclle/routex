package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/service"
)

type geminiStreamState struct {
	expected       int
	stopped        map[int]bool
	blocked        bool
	candidatesSeen bool
	usage          gatewayUsage
	usageAfterStop bool
	dimensions     []string
}

func (state *geminiStreamState) finished() bool {
	return state.blocked || (state.expected > 0 && len(state.stopped) == state.expected)
}
func (state *geminiStreamState) object(raw []byte, model, requestID string) ([]byte, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, invalidGatewayResponse()
	}
	if native, ok := object["error"]; ok {
		safe := map[string]any{"error": service.SanitizeGeminiError(native, 502)}
		return mustJSON(safe), &service.GatewayError{Status: 502, Code: "upstream_error", Message: "The upstream stream failed."}
	}
	var candidates []map[string]json.RawMessage
	if raw, ok := object["candidates"]; ok && json.Unmarshal(raw, &candidates) != nil {
		return nil, invalidGatewayResponse()
	}
	if state.stopped == nil {
		state.stopped = map[int]bool{}
	}
	seen := map[int]bool{}
	for _, candidate := range candidates {
		state.candidatesSeen = true
		index := 0
		if value, ok := candidate["index"]; ok && json.Unmarshal(value, &index) != nil {
			return nil, invalidGatewayResponse()
		}
		if index < 0 || index >= state.expected || seen[index] || state.stopped[index] || state.blocked {
			return nil, invalidGatewayResponse()
		}
		seen[index] = true
		var finish string
		if raw, ok := candidate["finishReason"]; ok {
			if json.Unmarshal(raw, &finish) != nil || (finish != "" && !responseEventName.MatchString(finish)) {
				return nil, invalidGatewayResponse()
			}
			if finish != "" && finish != "FINISH_REASON_UNSPECIFIED" {
				state.stopped[index] = true
			}
		}
	}
	if len(candidates) == 0 {
		var feedback map[string]json.RawMessage
		if json.Unmarshal(object["promptFeedback"], &feedback) == nil {
			var reason string
			if json.Unmarshal(feedback["blockReason"], &reason) == nil && reason != "" && reason != "BLOCK_REASON_UNSPECIFIED" {
				if state.blocked || state.candidatesSeen {
					return nil, invalidGatewayResponse()
				}
				state.blocked = true
			}
		}
	}
	if raw, ok := object["modelVersion"]; ok {
		var version string
		if json.Unmarshal(raw, &version) != nil || version == "" {
			return nil, invalidGatewayResponse()
		}
		object["modelVersion"] = mustJSON(model)
	}
	frameUsage := service.ParseGeminiUsage(raw, false)
	if state.usageAfterStop && frameUsage.Present {
		for _, pair := range [][2]*int64{{state.usage.Input, frameUsage.Input}, {state.usage.Output, frameUsage.Output}, {state.usage.CacheRead, frameUsage.CacheRead}} {
			if pair[0] != nil && pair[1] != nil && *pair[1] < *pair[0] {
				return nil, invalidGatewayResponse()
			}
		}
	}
	for _, dimension := range frameUsage.UnsupportedDimensions {
		found := false
		for _, existing := range state.dimensions {
			if dimension == existing {
				found = true
			}
		}
		if !found {
			state.dimensions = append(state.dimensions, dimension)
		}
	}
	if _, ok := object["usageMetadata"]; ok {
		state.usage = frameUsage
		state.usageAfterStop = state.finished()
	}
	if _, ok := object["usage_metadata"]; ok {
		state.usage = frameUsage
		state.usageAfterStop = state.finished()
	}
	// No mutation of opaque parts, thought signatures, function JSON or native IDs.
	return json.Marshal(object)
}
func (state *geminiStreamState) result(complete bool) gatewayUsage {
	usage := state.usage
	usage.Complete = complete && state.finished() && state.usageAfterStop
	usage.UnsupportedDimensions = state.dimensions
	usage.Unsupported = len(state.dimensions) > 0
	return usage
}
func proxyGeminiStream(ctx context.Context, writer http.ResponseWriter, body io.Reader, model, requestID string, candidates int) (gatewayUsage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), gatewayEventLimit)
	state := geminiStreamState{expected: candidates}
	var event bytes.Buffer
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
	fail := func() (gatewayUsage, error) {
		if ctx.Err() != nil {
			return state.result(false), ctx.Err()
		}
		_ = write(append(append([]byte("data: "), mustJSON(map[string]any{"error": service.SanitizeGeminiError(nil, 502)})...), '\n', '\n'))
		return state.result(false), invalidGatewayResponse()
	}
	process := func(raw []byte) error {
		lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
		data := []string{}
		for _, line := range lines {
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if len(data) == 0 {
			return write(append(bytes.Clone(raw), '\n'))
		}
		encoded, err := state.object([]byte(strings.Join(data, "\n")), model, requestID)
		if len(encoded) == 0 {
			return err
		}
		// Preserve SSE comments/id/retry fields; replace only the native JSON data.
		var output bytes.Buffer
		inserted := false
		for _, line := range lines {
			if strings.HasPrefix(line, "data:") {
				if !inserted {
					output.WriteString("data: ")
					output.Write(encoded)
					output.WriteByte('\n')
					inserted = true
				}
				continue
			}
			output.WriteString(line)
			output.WriteByte('\n')
		}
		output.WriteByte('\n')
		if writeErr := write(output.Bytes()); writeErr != nil {
			return writeErr
		}
		return err
	}
	for scanner.Scan() {
		if ctx.Err() != nil {
			return state.result(false), ctx.Err()
		}
		line := scanner.Bytes()
		if len(line) != 0 {
			if event.Len()+len(line)+1 > gatewayEventLimit {
				return fail()
			}
			event.Write(line)
			event.WriteByte('\n')
			continue
		}
		if event.Len() == 0 {
			continue
		}
		err := process(event.Bytes())
		event.Reset()
		if err != nil {
			var native *service.GatewayError
			if errors.As(err, &native) && native.Code == "invalid_upstream_response" {
				return fail()
			}
			return state.result(false), err
		}
	}
	if scanner.Err() != nil || ctx.Err() != nil {
		return fail()
	}
	if event.Len() > 0 {
		if err := process(event.Bytes()); err != nil {
			return state.result(false), err
		}
	}
	if !state.finished() {
		return fail()
	}
	return state.result(true), nil
}
