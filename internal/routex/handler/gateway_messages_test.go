package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

const messagesFinalUsage = `{"input_tokens":7,"output_tokens":4,"cache_read_input_tokens":2,"cache_creation_input_tokens":1,"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":0},"service_tier":"standard"}`

func messagesFixture(stop, usage string) string {
	reason := "null"
	if stop != "" {
		reason = string(mustJSON(stop))
	}
	return `{"id":"msg_one","type":"message","model":"private-model","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":` + reason + `,"stop_sequence":null,"usage":` + usage + `}`
}
func messagesStartFixture() string {
	return "event: message_start\ndata: {\"type\":\"message_start\",\"message\":" + strings.Replace(messagesFixture("", messagesFinalUsage), `"content":[{"type":"text","text":"hello"}]`, `"content":[]`, 1) + "}\n\n"
}
func messagesFinalFixture() string {
	return "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":4}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}
func TestMessagesNativeSSEAndCancellation(t *testing.T) {
	body := messagesStartFixture() + "event: ping\ndata: {\"type\":\"ping\"}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"opaque-signed\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" + messagesFinalFixture()
	recorder := httptest.NewRecorder()
	usage, err := proxyMessagesStream(context.Background(), recorder, strings.NewReader(body), "public", "req_one")
	if err != nil || !usage.Complete || usage.Input == nil || *usage.Input != 10 || *usage.Output != 4 || strings.Contains(recorder.Body.String(), "private-model") || !strings.Contains(recorder.Body.String(), "opaque-signed") {
		t.Fatalf("native SSE changed %+v %v %s", usage, err, recorder.Body.String())
	}
	for _, final := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		trigger := "message_start"
		if final {
			trigger = "message_stop"
		}
		writer := &cancelResponseWriter{httptest.NewRecorder(), cancel, trigger}
		usage, err := proxyMessagesStream(ctx, writer, strings.NewReader(body), "public", "req_one")
		cancel()
		if !errors.Is(err, context.Canceled) || usage.Complete != final {
			t.Fatalf("cancellation finality %+v %v", usage, err)
		}
	}
	// A duplicated terminal cannot create a second request fact or accumulate usage.
	recorder = httptest.NewRecorder()
	usage, err = proxyMessagesStream(context.Background(), recorder, strings.NewReader(body+messagesFinalFixture()), "public", "req_one")
	if err != nil || strings.Count(recorder.Body.String(), "event: message_stop") != 1 || *usage.Output != 4 {
		t.Fatal("duplicate terminal forwarded or charged")
	}
}
func TestMessagesMalformedAndOutOfOrderStreams(t *testing.T) {
	for name, body := range map[string]string{
		"missing-terminal":      messagesStartFixture(),
		"out-of-order-stop":     "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		"duplicate-start":       messagesStartFixture() + messagesStartFixture(),
		"delta-before-start":    "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n",
		"unopened-block":        messagesStartFixture() + "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"decreasing-usage":      messagesStartFixture() + strings.Replace(messagesFinalFixture(), `"output_tokens":4`, `"output_tokens":3`, 1),
		"duplicate-final-delta": messagesStartFixture() + strings.Split(messagesFinalFixture(), "event: message_stop")[0] + messagesFinalFixture(),
		"native-error":          messagesStartFixture() + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"provider-secret\",\"debug\":\"provider-secret\"}}\n\n",
		"oversized":             "data: " + strings.Repeat("x", gatewayEventLimit) + "\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			usage, err := proxyMessagesStream(context.Background(), recorder, strings.NewReader(body), "public", "req_one")
			if err == nil || usage.Complete || strings.Contains(recorder.Body.String(), "provider-secret") {
				t.Fatalf("malformed native stream accepted %+v %v", usage, err)
			}
		})
	}
	recorder := httptest.NewRecorder()
	usage, err := proxyMessagesStream(context.Background(), recorder, strings.NewReader(messagesStartFixture()+strings.Replace(messagesFinalFixture(), `"output_tokens":4`, `"output_tokens":"broken"`, 1)), "public", "req_one")
	if err != nil || !usage.Complete || usage.Output != nil {
		t.Fatal("invalid final output inherited preliminary count")
	}
}
func TestMessagesHeaderOwnershipAndOrdinaryReply(t *testing.T) {
	request := httptest.NewRequest("POST", "/v1/messages", nil)
	request.Header.Set("x-api-key", "rx_test")
	request.Header.Set("anthropic-version", "2023-06-01")
	request.Header.Add("anthropic-beta", "one")
	request.Header.Add("anthropic-beta", "two")
	bearer, headers, err := messagesRequestHeaders(request)
	if err != nil || bearer != "rx_test" || headers.Beta != "one,two" {
		t.Fatal("native headers lost")
	}
	request.Header.Set("Authorization", "Bearer another")
	if _, _, err := messagesRequestHeaders(request); err == nil {
		t.Fatal("ambiguous auth accepted")
	}
	request.Header.Del("Authorization")
	request.Header.Set("anthropic-workspace-id", "foreign")
	if _, _, err := messagesRequestHeaders(request); err == nil {
		t.Fatal("workspace escalation allowed")
	}
	response, err := rewriteMessagesObject([]byte(messagesFixture("stop_sequence", messagesFinalUsage)), "public", true)
	if err != nil || !strings.Contains(string(response), `"stop_reason":"stop_sequence"`) || strings.Contains(string(response), "private-model") {
		t.Fatal("native ordinary reply altered")
	}
	if _, err := rewriteMessagesObject([]byte(messagesFixture("", messagesFinalUsage)), "public", true); err == nil {
		t.Fatal("nonfinal ordinary reply accepted")
	}
}
func FuzzMessagesNativeEvents(f *testing.F) {
	f.Add(messagesStartFixture())
	f.Add("event: error\ndata: {\"type\":\"error\"}\n")
	f.Add("data: [DONE]\n")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > gatewayEventLimit {
			return
		}
		state := messagesStreamState{}
		_, _, _ = state.event([]byte(raw), "public", "req_fuzz")
	})
}
