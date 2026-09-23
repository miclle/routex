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

type messagesStreamState struct {
	started, final bool
	blocks         map[int]bool
	nextIndex      int
	usage          service.MessagesStreamUsage
	unsupported    bool
	unknown        bool
}

func (state *messagesStreamState) event(raw []byte, model, requestID string) ([]byte, bool, error) {
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	data := []string{}
	name := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
		if strings.HasPrefix(line, "event:") {
			name = strings.TrimPrefix(strings.TrimPrefix(line, "event:"), " ")
		}
	}
	if len(data) == 0 {
		return append(bytes.Clone(raw), '\n'), false, nil
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(strings.Join(data, "\n")), &envelope) != nil || envelope == nil {
		return nil, false, invalidGatewayResponse()
	}
	var kind string
	if json.Unmarshal(envelope["type"], &kind) != nil || !responseEventName.MatchString(kind) || (name != "" && name != kind) {
		return nil, false, invalidGatewayResponse()
	}
	terminal := false
	switch kind {
	case "error":
		envelope = map[string]json.RawMessage{"type": mustJSON("error"), "error": mustJSON(service.SanitizeMessagesError(envelope["error"])), "request_id": mustJSON(requestID)}
		terminal = true
	case "ping":
	case "message_start":
		if state.started {
			return nil, false, invalidGatewayResponse()
		}
		encoded, err := rewriteMessagesObject(envelope["message"], model, false)
		if err != nil {
			return nil, false, err
		}
		var message map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &message)
		if validMessagesStop(message["stop_reason"]) {
			return nil, false, invalidGatewayResponse()
		}
		state.started = true
		state.blocks = map[int]bool{}
		state.usage.Start(message["usage"])
		envelope["message"] = encoded
	case "content_block_start", "content_block_delta", "content_block_stop":
		var index int
		if !state.started || state.final || json.Unmarshal(envelope["index"], &index) != nil || index < 0 || index > 100000 {
			return nil, false, invalidGatewayResponse()
		}
		switch kind {
		case "content_block_start":
			if index != state.nextIndex {
				return nil, false, invalidGatewayResponse()
			}
			state.nextIndex++
			state.blocks[index] = true
			var block struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(envelope["content_block"], &block) != nil || block.Type == "" {
				return nil, false, invalidGatewayResponse()
			}
			switch block.Type {
			case "text", "thinking", "redacted_thinking", "tool_use":
			default:
				state.unsupported = true
			}
		case "content_block_delta":
			if !state.blocks[index] {
				return nil, false, invalidGatewayResponse()
			}
		case "content_block_stop":
			if !state.blocks[index] {
				return nil, false, invalidGatewayResponse()
			}
			delete(state.blocks, index)
		}
	case "message_delta":
		if !state.started || state.final || len(state.blocks) != 0 {
			return nil, false, invalidGatewayResponse()
		}
		var delta map[string]json.RawMessage
		if json.Unmarshal(envelope["delta"], &delta) != nil || delta == nil {
			return nil, false, invalidGatewayResponse()
		}
		final := validMessagesStop(delta["stop_reason"])
		if !state.usage.Delta(envelope["usage"], final) {
			return nil, false, invalidGatewayResponse()
		}
		state.final = final
	case "message_stop":
		if !state.started || !state.final || len(state.blocks) != 0 {
			return nil, false, invalidGatewayResponse()
		}
		terminal = true
	default: // Future native events are forwarded without inventing usage/finality.
		state.unknown = true
	}
	encoded := mustJSON(envelope)
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
	if kind == "error" {
		return out.Bytes(), true, &service.GatewayError{Status: 502, Code: "upstream_error", Message: "The upstream stream failed."}
	}
	return out.Bytes(), terminal, nil
}
func proxyMessagesStream(ctx context.Context, writer http.ResponseWriter, body io.Reader, model, requestID string) (gatewayUsage, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), gatewayEventLimit)
	state := messagesStreamState{}
	var event bytes.Buffer
	var usage gatewayUsage
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
		_ = write(append(append([]byte("event: error\ndata: "), mustJSON(map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "message": "The upstream returned an invalid or incomplete stream."}, "request_id": requestID})...), '\n', '\n'))
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
		encoded, terminal, err := state.event(event.Bytes(), model, requestID)
		event.Reset()
		if err != nil && len(encoded) == 0 {
			return failed()
		}
		usage = state.usage.Usage(terminal && err == nil)
		if state.unknown {
			usage.Unsupported = true
			usage.UnsupportedDimensions = append(usage.UnsupportedDimensions, "request_condition")
		}
		if state.unsupported {
			usage.Unsupported = true
			usage.UnsupportedDimensions = append(usage.UnsupportedDimensions, "external_tool")
		}
		if writeErr := write(encoded); writeErr != nil {
			return usage, writeErr
		}
		if terminal {
			return usage, err
		}
	}
	return failed()
}
