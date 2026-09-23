package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func responseFixture(status string, usage string) string {
	return `{"id":"resp_1","object":"response","model":"private-model","status":"` + status + `","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":` + usage + `}`
}

const responsesFinalUsage = `{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":2,"cache_write_tokens":1}}`

func responseEventFixture(kind, status, usage string, sequence int) string {
	return fmt.Sprintf("event: %s\ndata: {\"type\":%q,\"sequence_number\":%d,\"response\":%s}\n\n", kind, kind, sequence, responseFixture(status, usage))
}

func TestResponsesOrdinaryNativeRewriteAndErrors(t *testing.T) {
	raw := responseFixture("completed", responsesFinalUsage)
	response, status, err := rewriteResponsesObject([]byte(raw), "public-model", false)
	if err != nil || status != "completed" || strings.Contains(string(response), "private-model") || !strings.Contains(string(response), `"id":"resp_1"`) {
		t.Fatalf("native response changed: %s %v", response, err)
	}
	for _, raw := range []string{`null`, `{"object":"chat.completion"}`, strings.Replace(responseFixture("completed", "null"), `"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]`, `"output":null`, 1), responseFixture("in_progress", "null")} {
		if _, _, err := rewriteResponsesObject([]byte(raw), "public", false); err == nil {
			t.Fatal("nonfinal/malformed ordinary response accepted")
		}
	}
	failed := strings.TrimSuffix(responseFixture("failed", responsesFinalUsage), "}") + `,"error":{"code":"server_error","message":"provider-secret","debug":"provider-secret"}}`
	encoded, status, err := rewriteResponsesObject([]byte(failed), "public", false)
	if err != nil || status != "failed" || strings.Contains(string(encoded), "provider-secret") || !strings.Contains(string(encoded), "server_error") {
		t.Fatal("native failed object not sanitized")
	}
}
func TestResponsesStreamPreservesNativeEventsAndFinalUsage(t *testing.T) {
	stream := ": keepalive\n\n" + responseEventFixture("response.created", "in_progress", "null", 0) + "id: native-event\nevent: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"sequence_number\":1,\"delta\":\"{\\\"city\\\":\"}\n\n" + responseEventFixture("response.completed", "completed", responsesFinalUsage, 2)
	writer := httptest.NewRecorder()
	usage, err := proxyResponsesStream(context.Background(), writer, strings.NewReader(stream), "public")
	output := writer.Body.String()
	if err != nil || !usage.Complete || usage.Input == nil || *usage.Input != 10 || usage.CacheWrite == nil || *usage.CacheWrite != 1 {
		t.Fatalf("stream usage wrong: %+v %v", usage, err)
	}
	for _, expected := range []string{": keepalive", "id: native-event", "event: response.function_call_arguments.delta", "event: response.completed", `"sequence_number":2`, `"model":"public"`} {
		if !strings.Contains(output, expected) {
			t.Fatalf("native envelope lost %s: %s", expected, output)
		}
	}
	if strings.Contains(output, "private-model") || strings.Contains(output, "[DONE]") {
		t.Fatal("native stream became chat")
	}
}
func TestResponsesStreamFailuresAndWholeUsageFrames(t *testing.T) {
	complete := responseEventFixture("response.completed", "completed", responsesFinalUsage, 1)
	cases := map[string]string{
		"eof":                responseEventFixture("response.created", "in_progress", responsesFinalUsage, 0),
		"chat_done":          "data: [DONE]\n\n",
		"bad_json":           "event: response.created\ndata: {\n\n",
		"wrong_type":         "event: response.completed\ndata: {\"type\":\"response.failed\"}\n\n",
		"wrong_status":       responseEventFixture("response.completed", "in_progress", responsesFinalUsage, 1),
		"wrong_id":           responseEventFixture("response.created", "in_progress", "null", 0) + strings.Replace(complete, "resp_1", "resp_other", 1),
		"duplicate_sequence": responseEventFixture("response.created", "in_progress", "null", 1) + complete,
		"oversize":           "data: " + strings.Repeat("x", gatewayEventLimit) + "\n\n",
		"native_error":       "event: error\ndata: {\"type\":\"error\",\"code\":\"server_error\",\"message\":\"provider-secret\",\"debug\":\"provider-secret\"}\n\n",
	}
	for name, stream := range cases {
		t.Run(name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			_, err := proxyResponsesStream(context.Background(), writer, strings.NewReader(stream), "public")
			if err == nil {
				t.Fatal("incomplete/invalid stream succeeded")
			}
			if strings.Contains(writer.Body.String(), "provider-secret") {
				t.Fatal("upstream error secret leaked")
			}
		})
	}
	// A malformed final usage object must not inherit preliminary cache counters.
	stream := responseEventFixture("response.created", "in_progress", responsesFinalUsage, 0) + responseEventFixture("response.completed", "completed", `{"input_tokens":10,"output_tokens":4,"input_tokens_details":"invalid"}`, 1)
	writer := httptest.NewRecorder()
	usage, err := proxyResponsesStream(context.Background(), writer, strings.NewReader(stream), "public")
	if err != nil || !usage.Complete || usage.CacheRead != nil || usage.CacheWrite != nil {
		t.Fatalf("partial counters merged: %+v %v", usage, err)
	}
	for _, status := range []string{"incomplete", "failed"} {
		writer := httptest.NewRecorder()
		usage, err := proxyResponsesStream(context.Background(), writer, strings.NewReader(responseEventFixture("response."+status, status, responsesFinalUsage, 1)), "public")
		if err == nil || !usage.Complete {
			t.Fatal("failed final usage discarded")
		}
	}
}

type cancelResponseWriter struct {
	*httptest.ResponseRecorder
	cancel  context.CancelFunc
	trigger string
}

func (w *cancelResponseWriter) Write(raw []byte) (int, error) {
	if strings.Contains(string(raw), w.trigger) {
		w.cancel()
		return 0, context.Canceled
	}
	return w.ResponseRecorder.Write(raw)
}
func TestResponsesCancelBeforeAndAfterFinalUsage(t *testing.T) {
	for _, final := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		trigger := "response.created"
		if final {
			trigger = "response.completed"
		}
		writer := &cancelResponseWriter{httptest.NewRecorder(), cancel, trigger}
		stream := responseEventFixture("response.created", "in_progress", "null", 0) + responseEventFixture("response.completed", "completed", responsesFinalUsage, 1)
		usage, err := proxyResponsesStream(ctx, writer, strings.NewReader(stream), "public")
		cancel()
		if !errors.Is(err, context.Canceled) || usage.Complete != final {
			t.Fatalf("cancellation finality: %+v %v", usage, err)
		}
	}
}
func FuzzResponsesNativeEvents(f *testing.F) {
	f.Add(responseEventFixture("response.completed", "completed", responsesFinalUsage, 0))
	f.Add("event: error\ndata: {\"type\":\"error\"}\n\n")
	f.Add("data: [DONE]\n\n")
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > gatewayEventLimit {
			return
		}
		event, err := rewriteResponsesEvent([]byte(raw), "public")
		if err == nil && event.Response != nil && !json.Valid(event.Response) {
			t.Fatal("invalid rewritten response")
		}
	})
}

func TestResponsesToolDiagnosticSanitization(t *testing.T) {
	raw := `{"type":"mcp_call","arguments":"user arguments","error":"provider-secret"}`
	if safe := string(sanitizeResponsesItem([]byte(raw))); strings.Contains(safe, "provider-secret") || !strings.Contains(safe, "user arguments") {
		t.Fatal("tool error sanitizer modified arguments or leaked diagnostic")
	}
	event, err := rewriteResponsesEvent([]byte("event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"item\":"+raw+"}\n"), "public")
	if err != nil || strings.Contains(string(event.Bytes), "provider-secret") {
		t.Fatal("stream tool error diagnostic leaked")
	}
}
