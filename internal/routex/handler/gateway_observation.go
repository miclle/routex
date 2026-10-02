package handler

import (
	"bytes"
	"encoding/json"

	"github.com/miclle/routex/internal/routex/service"
)

// gatewayObservation keeps native completion separate from economic usage and
// transport status. It never changes what a native protocol forwards.
type gatewayObservation struct {
	service.GatewayUsage
	NativeCompletionEvidence string
}

func observeGatewayUsage(usage service.GatewayUsage) gatewayObservation {
	return gatewayObservation{GatewayUsage: usage, NativeCompletionEvidence: "unknown"}
}

func responsesCompletionEvidence(raw []byte, status string) string {
	if status == "incomplete" {
		return "incomplete"
	}
	if status != "completed" {
		return "unknown"
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(raw, &response) != nil {
		return "unknown"
	}
	for _, field := range []string{"error", "incomplete_details"} {
		if value, exists := response[field]; exists && !bytes.Equal(value, []byte("null")) {
			return "unknown"
		}
	}
	var output []map[string]json.RawMessage
	if json.Unmarshal(response["output"], &output) != nil || len(output) == 0 || len(output) > 128 {
		return "unknown"
	}
	completed, handoff, blocked := false, false, false
	for _, item := range output {
		if state, exists := item["status"]; exists && observedResponseString(state) != "completed" {
			return "unknown"
		}
		switch observedResponseString(item["type"]) {
		case "message":
			if observedResponseString(item["role"]) != "assistant" || observedResponseString(item["status"]) != "completed" {
				return "unknown"
			}
			var content []map[string]json.RawMessage
			if json.Unmarshal(item["content"], &content) != nil || len(content) == 0 || len(content) > 128 {
				return "unknown"
			}
			for _, block := range content {
				switch observedResponseString(block["type"]) {
				case "output_text":
					if observedResponseString(block["text"]) == "" {
						return "unknown"
					}
					completed = true
				case "refusal":
					if observedResponseString(block["refusal"]) == "" {
						return "unknown"
					}
					blocked = true
				default:
					return "unknown"
				}
			}
		case "function_call", "custom_tool_call":
			if observedResponseString(item["name"]) == "" || observedResponseString(item["call_id"]) == "" {
				return "unknown"
			}
			input := "arguments"
			if observedResponseString(item["type"]) == "custom_tool_call" {
				input = "input"
			}
			var value string
			if bytes.Equal(item[input], []byte("null")) || json.Unmarshal(item[input], &value) != nil {
				return "unknown"
			}
			handoff = true
		case "reasoning":
			// Reasoning is supplementary evidence; it never proves completion alone.
			var summary []map[string]json.RawMessage
			if json.Unmarshal(item["summary"], &summary) != nil || summary == nil || len(summary) > 128 {
				return "unknown"
			}
			for _, part := range summary {
				if observedResponseString(part["type"]) != "summary_text" || observedResponseString(part["text"]) == "" {
					return "unknown"
				}
			}
		default:
			return "unknown"
		}
	}
	switch {
	case blocked:
		return "blocked"
	case handoff:
		return "handoff"
	case completed:
		return "completed"
	default:
		return "unknown"
	}
}

func observedResponseString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func messagesCompletionEvidence(raw json.RawMessage) string {
	var reason string
	if json.Unmarshal(raw, &reason) != nil {
		return "unknown"
	}
	switch reason {
	case "end_turn", "stop_sequence":
		return "completed"
	case "tool_use", "pause_turn":
		return "handoff"
	case "refusal":
		return "blocked"
	case "max_tokens":
		return "incomplete"
	default:
		return "unknown"
	}
}

func observedMessagesCompletion(raw []byte) string {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "unknown"
	}
	return messagesCompletionEvidence(object["stop_reason"])
}

func expectedChatChoices(raw []byte) int {
	var request map[string]json.RawMessage
	if json.Unmarshal(raw, &request) != nil || request == nil {
		return 0
	}
	if count, exists := request["n"]; exists {
		var value int
		if json.Unmarshal(count, &value) != nil || value < 1 || value > 128 {
			return 0
		}
		return value
	}
	return 1
}

func chatChoiceCount(expected []int) int {
	if len(expected) == 0 {
		return 1
	}
	if len(expected) != 1 || expected[0] < 1 || expected[0] > 128 {
		return 0
	}
	return expected[0]
}

type chatCompletionChoice struct {
	assistant, message, handoff, refusal bool
	finish                               string
}

type chatCompletionObservation struct {
	expected int
	choices  map[int]chatCompletionChoice
	invalid  bool
}

func (state *chatCompletionObservation) observe(raw []byte, stream bool) {
	if state.invalid || state.expected < 1 || state.expected > 128 {
		state.invalid = true
		return
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		state.invalid = true
		return
	}
	var kind string
	wantKind := "chat.completion"
	if stream {
		wantKind = "chat.completion.chunk"
	}
	if json.Unmarshal(object["object"], &kind) != nil || kind != wantKind {
		state.invalid = true
		return
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(object["choices"], &choices) != nil || choices == nil || len(choices) > 128 {
		state.invalid = true
		return
	}
	if state.choices == nil {
		state.choices = map[int]chatCompletionChoice{}
	}
	seen := map[int]bool{}
	for _, choice := range choices {
		var index int
		if bytes.Equal(choice["index"], []byte("null")) || json.Unmarshal(choice["index"], &index) != nil || index < 0 || index >= state.expected || seen[index] {
			state.invalid = true
			return
		}
		seen[index] = true
		observed := state.choices[index]
		if observed.finish != "" {
			state.invalid = true
			return
		}
		field := "message"
		if stream {
			field = "delta"
		}
		var message map[string]json.RawMessage
		if json.Unmarshal(choice[field], &message) != nil || message == nil {
			state.invalid = true
			return
		}
		if role, exists := message["role"]; exists {
			var value string
			if json.Unmarshal(role, &value) != nil || value != "assistant" {
				state.invalid = true
				return
			}
			observed.assistant = true
		}
		if content, exists := message["content"]; exists {
			var value string
			if !bytes.Equal(content, []byte("null")) && json.Unmarshal(content, &value) != nil {
				state.invalid = true
				return
			}
			observed.message = true
		}
		if refusal, exists := message["refusal"]; exists && !bytes.Equal(refusal, []byte("null")) {
			var value string
			if json.Unmarshal(refusal, &value) != nil {
				state.invalid = true
				return
			}
			observed.refusal = observed.refusal || value != ""
			observed.message = true
		}
		if tools, exists := message["tool_calls"]; exists && !bytes.Equal(tools, []byte("null")) {
			var calls []map[string]json.RawMessage
			if json.Unmarshal(tools, &calls) != nil || calls == nil {
				state.invalid = true
				return
			}
			for _, call := range calls {
				var kind string
				if raw, exists := call["type"]; exists {
					if json.Unmarshal(raw, &kind) != nil || kind != "function" {
						state.invalid = true
						return
					}
				} else if !stream {
					state.invalid = true
					return
				}
				if !validChatFunction(call["function"], stream) {
					state.invalid = true
					return
				}
			}
			if len(calls) > 0 {
				observed.handoff, observed.message = true, true
			}
		}
		if function, exists := message["function_call"]; exists && !bytes.Equal(function, []byte("null")) {
			var call map[string]json.RawMessage
			if json.Unmarshal(function, &call) != nil || call == nil || !validChatFunction(function, stream) {
				state.invalid = true
				return
			}
			observed.handoff, observed.message = true, true
		}
		if finish, exists := choice["finish_reason"]; exists && !bytes.Equal(finish, []byte("null")) {
			if json.Unmarshal(finish, &observed.finish) != nil || observed.finish == "" || len(observed.finish) > 128 {
				state.invalid = true
				return
			}
		}
		state.choices[index] = observed
	}
}

func (state *chatCompletionObservation) evidence() string {
	if state.invalid || state.expected < 1 || len(state.choices) != state.expected {
		return "unknown"
	}
	result := "completed"
	for _, choice := range state.choices {
		if !choice.assistant || !choice.message {
			return "unknown"
		}
		var evidence string
		switch choice.finish {
		case "stop":
			evidence = "completed"
			if choice.handoff {
				evidence = "handoff"
			}
			if choice.refusal {
				evidence = "blocked"
			}
		case "tool_calls", "function_call":
			evidence = "handoff"
		case "length":
			evidence = "incomplete"
		case "content_filter", "refusal":
			evidence = "blocked"
		default:
			return "unknown"
		}
		result = mergeCompletionEvidence(result, evidence)
	}
	return result
}

func mergeCompletionEvidence(left, right string) string {
	for _, evidence := range []string{"unknown", "blocked", "incomplete", "handoff"} {
		if left == evidence || right == evidence {
			return evidence
		}
	}
	return "completed"
}

type geminiCompletionCandidate struct {
	content, function, invalid bool
	finish                     string
}

func (state *geminiStreamState) observeCandidateCompletion(candidate map[string]json.RawMessage, index int, finish string) {
	if state.completionCandidates == nil {
		state.completionCandidates = map[int]geminiCompletionCandidate{}
	}
	observed := state.completionCandidates[index]
	if raw, exists := candidate["index"]; exists && bytes.Equal(raw, []byte("null")) {
		observed.invalid = true
	}
	if raw, exists := candidate["content"]; exists {
		var content map[string]json.RawMessage
		var parts []map[string]json.RawMessage
		if json.Unmarshal(raw, &content) != nil || content == nil || json.Unmarshal(content["parts"], &parts) != nil || parts == nil {
			observed.invalid = true
		} else {
			if role, exists := content["role"]; exists {
				var value string
				if json.Unmarshal(role, &value) != nil || value != "model" {
					observed.invalid = true
				}
			}
			for _, part := range parts {
				known := false
				if text, exists := part["text"]; exists {
					var value string
					if !bytes.Equal(text, []byte("null")) && json.Unmarshal(text, &value) == nil {
						observed.content, known = true, true
					} else {
						observed.invalid = true
					}
				}
				if function, exists := part["functionCall"]; exists {
					var call map[string]json.RawMessage
					var name string
					if json.Unmarshal(function, &call) != nil || call == nil || json.Unmarshal(call["name"], &name) != nil || name == "" || len(name) > 256 {
						observed.invalid = true
					} else {
						if args, exists := call["args"]; exists {
							var object map[string]json.RawMessage
							if json.Unmarshal(args, &object) != nil || object == nil {
								observed.invalid = true
							}
						}
						observed.content, observed.function, known = true, true, true
					}
				}
				if !known {
					observed.invalid = true
				}
			}
		}
	}
	if finish != "" {
		observed.finish = finish
	}
	state.completionCandidates[index] = observed
}

func (state *geminiStreamState) completionEvidence() string {
	if !state.finished() || state.expected < 1 || state.expected > 128 {
		return "unknown"
	}
	if state.blocked {
		switch state.completionBlockReason {
		case "SAFETY", "OTHER", "BLOCKLIST", "PROHIBITED_CONTENT", "IMAGE_SAFETY":
			return "blocked"
		default:
			return "unknown"
		}
	}
	if len(state.completionCandidates) != state.expected {
		return "unknown"
	}
	result := "completed"
	for _, candidate := range state.completionCandidates {
		if candidate.invalid {
			return "unknown"
		}
		var evidence string
		switch candidate.finish {
		case "STOP":
			if candidate.invalid || !candidate.content {
				return "unknown"
			}
			evidence = "completed"
			if candidate.function {
				evidence = "handoff"
			}
		case "MAX_TOKENS":
			evidence = "incomplete"
		case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT":
			evidence = "blocked"
		default:
			return "unknown"
		}
		result = mergeCompletionEvidence(result, evidence)
	}
	return result
}

func validChatFunction(raw json.RawMessage, stream bool) bool {
	var call map[string]json.RawMessage
	if json.Unmarshal(raw, &call) != nil || call == nil {
		return false
	}
	seen := false
	for _, field := range []string{"name", "arguments"} {
		value, exists := call[field]
		if !exists {
			if !stream {
				return false
			}
			continue
		}
		var text string
		if bytes.Equal(value, []byte("null")) || json.Unmarshal(value, &text) != nil || field == "name" && text == "" {
			return false
		}
		seen = true
	}
	return seen
}
