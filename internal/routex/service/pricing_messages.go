package service

import (
	"encoding/json"
	"math"
	"slices"
)

// ParseMessagesUsage normalizes the native disjoint input categories into the
// common total-input contract. It never infers omitted cache counts as zero.
func ParseMessagesUsage(raw []byte, complete bool) GatewayUsage {
	envelope := usageObject(raw)
	result := messagesUsage(envelope["usage"], complete)
	var content []map[string]json.RawMessage
	if json.Unmarshal(envelope["content"], &content) == nil {
		for _, block := range content {
			var kind string
			_ = json.Unmarshal(block["type"], &kind)
			if !slices.Contains([]string{"text", "thinking", "redacted_thinking", "tool_use"}, kind) {
				result.Unsupported = true
				result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, "external_tool")
			}
		}
	}
	return result
}
func messagesUsage(raw json.RawMessage, complete bool) GatewayUsage {
	result := GatewayUsage{Present: nativePresent(raw)}
	object := usageObject(raw)
	if object == nil {
		return result
	}
	input, read, write := usageCounter(object["input_tokens"]), usageCounter(object["cache_read_input_tokens"]), usageCounter(object["cache_creation_input_tokens"])
	result.CacheRead, result.CacheWrite, result.Output = read, write, usageCounter(object["output_tokens"])
	if input != nil && read != nil && write != nil && *input <= math.MaxInt64-*read && *input+*read <= math.MaxInt64-*write {
		total := *input + *read + *write
		result.Input = &total
	}
	result.Complete = complete
	unsupported := func(dimension string) {
		result.Unsupported = true
		result.UnsupportedDimensions = appendDimension(result.UnsupportedDimensions, dimension)
	}
	if write != nil && *write > 0 {
		split := usageObject(object["cache_creation"])
		short, long := usageCounter(split["ephemeral_5m_input_tokens"]), usageCounter(split["ephemeral_1h_input_tokens"])
		if short == nil || long == nil || *long != 0 || *short != *write {
			unsupported("cache_retention")
		}
	}
	if raw := object["service_tier"]; nativePresent(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || tier != "standard" {
			unsupported("response_service_tier")
		}
	}
	if raw := object["inference_geo"]; nativePresent(raw) {
		var geo string
		if json.Unmarshal(raw, &geo) != nil || geo != "global" {
			unsupported("request_condition")
		}
	}
	if raw := object["speed"]; nativePresent(raw) {
		var speed string
		if json.Unmarshal(raw, &speed) != nil || speed != "standard" {
			unsupported("request_condition")
		}
	}
	if tools := usageObject(object["server_tool_use"]); tools != nil {
		for _, raw := range tools {
			if count := usageCounter(raw); count == nil || *count != 0 {
				unsupported("external_tool")
			}
		}
	}
	if raw := object["iterations"]; nativePresent(raw) {
		var iterations []map[string]json.RawMessage
		if json.Unmarshal(raw, &iterations) != nil || len(iterations) != 1 {
			unsupported("request_condition")
		} else {
			var kind string
			if json.Unmarshal(iterations[0]["type"], &kind) != nil || kind != "message" {
				unsupported("request_condition")
			}
		}
	}
	return result
}

// MessagesStreamUsage has a protocol-defined input snapshot and cumulative
// output updates. Missing fields in a delta are not zeroes; invalid present
// fields poison that counter rather than falling back to a previous value.
type MessagesStreamUsage struct {
	raw            map[string]json.RawMessage
	started        bool
	finalDelta     bool
	previousOutput *int64
}

func (state *MessagesStreamUsage) Start(raw json.RawMessage) bool {
	if state.started {
		return false
	}
	state.started = true
	state.raw = usageObject(raw)
	state.previousOutput = usageCounter(state.raw["output_tokens"])
	if state.raw == nil {
		state.raw = map[string]json.RawMessage{}
	}
	return true
}
func (state *MessagesStreamUsage) Delta(raw json.RawMessage, final bool) bool {
	if !state.started || state.finalDelta {
		return false
	}
	object := usageObject(raw)
	if object == nil {
		state.raw = map[string]json.RawMessage{}
		state.finalDelta = final
		return true
	}
	if raw, exists := object["output_tokens"]; exists {
		current := usageCounter(raw)
		if current != nil && state.previousOutput != nil && *current < *state.previousOutput {
			return false
		}
		state.previousOutput = current
	} else if final {
		state.raw["output_tokens"] = json.RawMessage("null")
	}
	for key, raw := range object {
		state.raw[key] = raw
	}
	state.finalDelta = final
	return true
}
func (state *MessagesStreamUsage) Usage(terminal bool) GatewayUsage {
	raw, _ := json.Marshal(state.raw)
	return messagesUsage(raw, terminal && state.started && state.finalDelta)
}

func messagesPricingDimensions(payload map[string]json.RawMessage) []string {
	dimensions := []string{}
	add := func(value string) { dimensions = appendDimension(dimensions, value) }
	cache := func(raw json.RawMessage) {
		if !nativePresent(raw) {
			return
		}
		object := usageObject(raw)
		var kind, ttl string
		if object == nil || json.Unmarshal(object["type"], &kind) != nil || kind != "ephemeral" {
			add("cache_retention")
			return
		}
		if nativePresent(object["ttl"]) && (json.Unmarshal(object["ttl"], &ttl) != nil || ttl != "5m") {
			add("cache_retention")
		}
	}
	cache(payload["cache_control"])
	var content func(json.RawMessage)
	content = func(raw json.RawMessage) {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return
		}
		var blocks []map[string]json.RawMessage
		if json.Unmarshal(raw, &blocks) != nil {
			add("request_non_text")
			return
		}
		for _, block := range blocks {
			cache(block["cache_control"])
			var kind string
			_ = json.Unmarshal(block["type"], &kind)
			switch kind {
			case "text", "thinking", "redacted_thinking", "tool_use":
			case "tool_result":
				if nativePresent(block["content"]) {
					content(block["content"])
				}
			default:
				add("request_non_text")
			}
		}
	}
	var messages []map[string]json.RawMessage
	_ = json.Unmarshal(payload["messages"], &messages)
	for _, message := range messages {
		content(message["content"])
	}
	if nativePresent(payload["system"]) {
		content(payload["system"])
	}
	var tools []map[string]json.RawMessage
	_ = json.Unmarshal(payload["tools"], &tools)
	for _, tool := range tools {
		cache(tool["cache_control"])
		var kind string
		_ = json.Unmarshal(tool["type"], &kind)
		if kind != "" && kind != "custom" {
			add("external_tool")
		}
	}
	for _, key := range []string{"mcp_servers", "container"} {
		if nativePresent(payload[key]) {
			add("external_tool")
		}
	}
	for _, key := range []string{"context_management", "inference_geo", "speed"} {
		if nativePresent(payload[key]) {
			add("request_condition")
		}
	}
	if raw := payload["service_tier"]; nativePresent(raw) {
		var tier string
		if json.Unmarshal(raw, &tier) != nil || !slices.Contains([]string{"auto", "standard_only"}, tier) {
			add("request_service_tier")
		}
	}
	return dimensions
}
