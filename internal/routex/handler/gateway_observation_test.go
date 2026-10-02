package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/service"
)

func chatCompletionFixture(finish, message, usage string, stream bool, index int) string {
	kind, field := "chat.completion", "message"
	if stream {
		kind, field = "chat.completion.chunk", "delta"
	}
	return fmt.Sprintf(`{"object":%q,"choices":[{"index":%d,%q:%s,"finish_reason":%q}],"usage":%s}`, kind, index, field, message, finish, usage)
}

func TestChatNativeCompletionObservation(t *testing.T) {
	for _, test := range []struct{ finish, want string }{
		{"stop", "completed"}, {"tool_calls", "handoff"}, {"function_call", "handoff"},
		{"length", "incomplete"}, {"content_filter", "blocked"}, {"refusal", "blocked"}, {"future_reason", "unknown"},
	} {
		for _, usage := range []string{"null", `{"prompt_tokens":"invalid","completion_tokens":2}`} {
			t.Run(test.finish+usage, func(t *testing.T) {
				message := `{"role":"assistant","content":"hello"}`
				ordinary := chatCompletionFixture(test.finish, message, usage, false, 0)
				observation := parseGatewayUsage([]byte(ordinary))
				if observation.NativeCompletionEvidence != test.want {
					t.Fatalf("ordinary evidence = %s, want %s", observation.NativeCompletionEvidence, test.want)
				}
				body := "data: " + chatCompletionFixture(test.finish, message, usage, true, 0) + "\n\ndata: [DONE]\n\n"
				writer := httptest.NewRecorder()
				observation, err := proxyGatewayStream(context.Background(), writer, strings.NewReader(body), "public")
				if err != nil || observation.NativeCompletionEvidence != test.want || !strings.Contains(writer.Body.String(), "[DONE]") {
					t.Fatalf("stream evidence = %+v, error = %v, want %s", observation, err, test.want)
				}
			})
		}
	}
	for _, message := range []string{`{"role":"assistant","content":null,"tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]}`, `{"role":"assistant","content":null,"function_call":{"name":"lookup","arguments":"{}"}}`} {
		if got := parseGatewayUsage([]byte(chatCompletionFixture("stop", message, "null", false, 0))).NativeCompletionEvidence; got != "handoff" {
			t.Fatalf("tool work became completed text: %s", got)
		}
	}
	refusal := chatCompletionFixture("stop", `{"role":"assistant","content":null,"refusal":"Declined"}`, "null", false, 0)
	if got := parseGatewayUsage([]byte(refusal)).NativeCompletionEvidence; got != "blocked" {
		t.Fatalf("explicit refusal became completed text: %s", got)
	}
}

func TestChatWeakShapesKeepUnknownWithoutChangingForwarding(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"usage":{"prompt_tokens":2,"completion_tokens":3}}`, `{"object":"chat.completion","choices":[]}`,
		chatCompletionFixture("stop", `{}`, "null", false, 0),
		strings.Replace(chatCompletionFixture("stop", `{"role":"assistant","content":"text"}`, "null", false, 0), `"index":0`, `"index":null`, 1),
		chatCompletionFixture("stop", `{"role":"user","content":"text"}`, "null", false, 0),
		chatCompletionFixture("stop", `{"role":"assistant","content":{}}`, "null", false, 0),
		chatCompletionFixture("stop", `{"role":"assistant","tool_calls":[{}]}`, "null", false, 0),
		chatCompletionFixture("stop", `{"role":"assistant","content":"text"}`, "null", false, 1),
	} {
		if got := parseGatewayUsage([]byte(raw)).NativeCompletionEvidence; got != "unknown" {
			t.Fatalf("weak ordinary shape gained %s: %s", got, raw)
		}
		if _, err := rewriteGatewayModel([]byte(raw), "public"); err != nil {
			t.Fatalf("observation changed compatible wire acceptance: %s %v", raw, err)
		}
	}
	for _, body := range []string{"data: [DONE]\n\n", "data: {}\n\ndata: [DONE]\n\n", `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}` + "\n\ndata: [DONE]\n\n"} {
		observation, err := proxyGatewayStream(context.Background(), httptest.NewRecorder(), strings.NewReader(body), "public")
		if err != nil || observation.NativeCompletionEvidence != "unknown" {
			t.Fatalf("weak stream changed acceptance or gained evidence: %+v %v", observation, err)
		}
	}
}

func TestChatCompletionRequiresExactChoiceCoverageAndTerminal(t *testing.T) {
	message := `{"role":"assistant","content":"text"}`
	first := chatCompletionFixture("stop", message, "null", true, 0)
	second := chatCompletionFixture("stop", message, "null", true, 1)
	for _, test := range []struct {
		body string
		n    int
		want string
		err  bool
	}{
		{"data: " + first + "\n\ndata: " + second + "\n\ndata: [DONE]\n\n", 2, "completed", false},
		{"data: " + first + "\n\ndata: [DONE]\n\n", 2, "unknown", false},
		{"data: " + first + "\n\ndata: " + first + "\n\ndata: [DONE]\n\n", 1, "unknown", false},
		{"data: " + first + "\n\ndata: [DONE]\n\n", 0, "unknown", false},
		{"data: " + first + "\n\ndata: [DONE]\n\n", 129, "unknown", false},
		{"data: " + first + "\n\n", 1, "unknown", true},
	} {
		observation, err := proxyGatewayStream(context.Background(), httptest.NewRecorder(), strings.NewReader(test.body), "public", test.n)
		if (err != nil) != test.err || observation.NativeCompletionEvidence != test.want {
			t.Fatalf("choice coverage n=%d evidence=%+v error=%v, want %s", test.n, observation, err, test.want)
		}
	}
	ordinary := `{"object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"a"},"finish_reason":"stop"},{"index":1,"message":{"role":"assistant","content":"b"},"finish_reason":"length"}]}`
	if got := parseGatewayUsage([]byte(ordinary), 2).NativeCompletionEvidence; got != "incomplete" {
		t.Fatalf("partial multi-choice became completed: %s", got)
	}
	for _, test := range []struct {
		raw  string
		want int
	}{{`{}`, 1}, {`{"n":2}`, 2}, {`{"n":null}`, 0}, {`{"n":0}`, 0}, {`{"n":129}`, 0}, {`{"n":1.5}`, 0}, {`{"n":"2"}`, 0}} {
		if got := expectedChatChoices([]byte(test.raw)); got != test.want {
			t.Fatalf("expected choices = %d, want %d: %s", got, test.want, test.raw)
		}
	}
}

func TestChatCompletionWaitsForDoneAndPreservesTerminalCancellation(t *testing.T) {
	frame := "data: " + chatCompletionFixture("stop", `{"role":"assistant","content":"text"}`, "null", true, 0) + "\n\n"
	observation, err := proxyGatewayStream(context.Background(), httptest.NewRecorder(), io.MultiReader(strings.NewReader(frame), geminiErrorReader{}), "public")
	if err == nil || observation.NativeCompletionEvidence != "unknown" {
		t.Fatalf("transport failure fabricated completion: %+v %v", observation, err)
	}
	for _, terminal := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		trigger, want := "chat.completion.chunk", "unknown"
		if terminal {
			trigger, want = "[DONE]", "completed"
		}
		writer := &cancelResponseWriter{httptest.NewRecorder(), cancel, trigger}
		observation, err := proxyGatewayStream(ctx, writer, strings.NewReader(frame+"data: [DONE]\n\n"), "public")
		cancel()
		if !errors.Is(err, context.Canceled) || observation.NativeCompletionEvidence != want {
			t.Fatalf("terminal cancellation observation = %+v %v, want %s", observation, err, want)
		}
	}
}

func TestResponsesCompletionObservation(t *testing.T) {
	function := `{"type":"function_call","name":"weather","call_id":"call_1","arguments":"{}","status":"completed"}`
	refusal := `{"type":"message","role":"assistant","status":"completed","content":[{"type":"refusal","refusal":"Cannot answer"}]}`
	for _, test := range []struct{ name, status, output, want string }{
		{"text", "completed", responsesTextOutput, "completed"},
		{"refusal", "completed", "[" + refusal + "]", "blocked"},
		{"function", "completed", "[" + function + "]", "handoff"},
		{"custom_tool", "completed", `[{"type":"custom_tool_call","name":"compute","call_id":"call_2","input":"calculate","status":"completed"}]`, "handoff"},
		{"mixed_text_tool", "completed", strings.TrimSuffix(responsesTextOutput, "]") + "," + function + "]", "handoff"},
		{"mixed_refusal_tool", "completed", "[" + refusal + "," + function + "]", "blocked"},
		{"reasoning_text", "completed", `[{"type":"reasoning","summary":[]},` + strings.TrimPrefix(responsesTextOutput, "["), "completed"},
		{"reasoning_only", "completed", `[{"type":"reasoning","summary":[]}]`, "unknown"},
		{"future_item", "completed", `[{"type":"future_result","value":"done"}]`, "unknown"},
		{"future_content", "completed", `[{"type":"message","role":"assistant","status":"completed","content":[{"type":"future_text","text":"hello"}]}]`, "unknown"},
		{"weak_message", "completed", `[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]`, "unknown"},
		{"null_text", "completed", `[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":null}]}]`, "unknown"},
		{"weak_function", "completed", `[{"type":"function_call","name":"weather","arguments":"{}"}]`, "unknown"},
		{"mixed_future_text", "completed", `[{"type":"future_result"},` + strings.TrimPrefix(responsesTextOutput, "["), "unknown"},
		{"nonterminal_message", "completed", strings.Replace(responsesTextOutput, `"status":"completed"`, `"status":"in_progress"`, 1), "unknown"},
		{"incomplete_message", "completed", strings.Replace(responsesTextOutput, `"status":"completed"`, `"status":"incomplete"`, 1), "unknown"},
		{"nonterminal_function", "completed", "[" + strings.Replace(function, `"status":"completed"`, `"status":"in_progress"`, 1) + "]", "unknown"},
		{"empty_output", "completed", `[]`, "unknown"},
		{"empty_content", "completed", `[{"type":"message","role":"assistant","status":"completed","content":[]}]`, "unknown"},
		{"incomplete", "incomplete", `[]`, "incomplete"},
		{"failed", "failed", responsesTextOutput, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, usage := range []string{"null", responsesFinalUsage, `{"input_tokens":"invalid"}`} {
				raw := responseOutputFixture(test.status, usage, test.output)
				encoded, status, err := rewriteResponsesObject([]byte(raw), "public", false)
				if err != nil || responsesCompletionEvidence(encoded, status) != test.want || !json.Valid(encoded) {
					t.Fatalf("ordinary observation = %s, want %s; %v", responsesCompletionEvidence(encoded, status), test.want, err)
				}
				stream := fmt.Sprintf("event: response.%s\ndata: {\"type\":\"response.%s\",\"sequence_number\":0,\"response\":%s}\n\n", test.status, test.status, raw)
				writer := httptest.NewRecorder()
				observation, err := proxyResponsesStream(context.Background(), writer, strings.NewReader(stream), "public")
				if (err == nil) != (test.status == "completed") || observation.NativeCompletionEvidence != test.want || !strings.Contains(writer.Body.String(), "response."+test.status) {
					t.Fatalf("Responses terminal observation = %+v %v, want %s", observation, err, test.want)
				}
				parsedUsage := service.ParseResponsesUsage(encoded)
				if !reflect.DeepEqual(
					[]any{observation.Input, observation.Output, observation.CacheRead, observation.CacheWrite, observation.Present, observation.Complete, observation.Unsupported},
					[]any{parsedUsage.Input, parsedUsage.Output, parsedUsage.CacheRead, parsedUsage.CacheWrite, parsedUsage.Present, parsedUsage.Complete, parsedUsage.Unsupported},
				) {
					t.Fatal("native completion changed independent economic usage")
				}
			}
		})
	}
	for _, extra := range []struct {
		fields, want string
		rejected     bool
	}{
		{`,"error":{"code":"server_error"}`, "unknown", true},
		{`,"incomplete_details":{"reason":"max_output_tokens"}`, "unknown", false},
		{`,"error":null,"incomplete_details":null`, "completed", false},
	} {
		raw := strings.TrimSuffix(responseFixture("completed", responsesFinalUsage), "}") + extra.fields + "}"
		encoded, status, err := rewriteResponsesObject([]byte(raw), "public", false)
		if (err != nil) != extra.rejected || responsesCompletionEvidence([]byte(raw), "completed") != extra.want {
			t.Fatalf("contradictory ordinary envelope: %s %v", extra.fields, err)
		}
		if !extra.rejected && responsesCompletionEvidence(encoded, status) != extra.want {
			t.Fatal("rewritten envelope changed native evidence")
		}
		stream := fmt.Sprintf("event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":0,\"response\":%s}\n\n", raw)
		observed, err := proxyResponsesStream(context.Background(), httptest.NewRecorder(), strings.NewReader(stream), "public")
		if (err != nil) != extra.rejected || observed.NativeCompletionEvidence != extra.want || observed.Complete == extra.rejected {
			t.Fatalf("contradictory SSE envelope changed observation/usage: %+v %v", observed, err)
		}
	}

	observation, err := proxyResponsesStream(context.Background(), httptest.NewRecorder(), strings.NewReader(responseEventFixture("response.created", "in_progress", responsesFinalUsage, 0)), "public")
	if err == nil || observation.NativeCompletionEvidence != "unknown" {
		t.Fatal("Responses nonterminal usage fabricated native completion")
	}
}

func TestMessagesCompletionObservation(t *testing.T) {
	for _, test := range []struct{ reason, want string }{
		{"end_turn", "completed"}, {"stop_sequence", "completed"}, {"tool_use", "handoff"}, {"pause_turn", "handoff"},
		{"refusal", "blocked"}, {"max_tokens", "incomplete"}, {"future_reason", "unknown"},
	} {
		encoded, err := rewriteMessagesObject([]byte(messagesFixture(test.reason, "null")), "public", true)
		if err != nil || observedMessagesCompletion(encoded) != test.want {
			t.Fatalf("Messages ordinary observation = %s %v, want %s", observedMessagesCompletion(encoded), err, test.want)
		}
		final := strings.Replace(messagesFinalFixture(), "end_turn", test.reason, 1)
		observation, err := proxyMessagesStream(context.Background(), httptest.NewRecorder(), strings.NewReader(messagesStartFixture()+final), "public", "req")
		if err != nil || observation.NativeCompletionEvidence != test.want {
			t.Fatalf("Messages stream observation = %+v %v, want %s", observation, err, test.want)
		}
		withoutStop := strings.Split(final, "event: message_stop")[0]
		observation, err = proxyMessagesStream(context.Background(), httptest.NewRecorder(), strings.NewReader(messagesStartFixture()+withoutStop), "public", "req")
		if err == nil || observation.NativeCompletionEvidence != "unknown" {
			t.Fatalf("Messages final delta fabricated terminal observation: %+v %v", observation, err)
		}
	}
	body := strings.Replace(messagesStartFixture()+messagesFinalFixture(), messagesFinalUsage, "null", 1)
	observation, err := proxyMessagesStream(context.Background(), httptest.NewRecorder(), strings.NewReader(body), "public", "req")
	if err != nil || observation.NativeCompletionEvidence != "completed" {
		t.Fatalf("missing usage suppressed Messages completion: %+v %v", observation, err)
	}
}

func TestGeminiCompletionObservation(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{geminiFixture(), "completed"},
		{strings.Replace(geminiFixture(), `{"text":"hello","thoughtSignature":"signed-opaque"}`, `{"functionCall":{"name":"lookup","args":{"value":1}}}`, 1), "handoff"},
		{strings.Replace(geminiFixture(), "STOP", "MAX_TOKENS", 1), "incomplete"},
		{strings.Replace(geminiFixture(), "STOP", "SAFETY", 1), "blocked"},
		{strings.Replace(geminiFixture(), "STOP", "future_reason", 1), "unknown"},
		{`{"candidates":[{"index":0,"finishReason":"STOP"}]}`, "unknown"},
		{strings.Replace(geminiFixture(), `"text":"hello"`, `"text":null`, 1), "unknown"},
		{strings.Replace(geminiFixture(), `"index":0`, `"index":null`, 1), "unknown"},
		{`{"promptFeedback":{"blockReason":"SAFETY"}}`, "blocked"},
		{`{"promptFeedback":{"blockReason":"future_reason"}}`, "unknown"},
	} {
		state := geminiStreamState{expected: 1}
		encoded, err := state.object([]byte(test.raw), "public", "req")
		if err != nil || state.completionEvidence() != test.want || !json.Valid(encoded) {
			t.Fatalf("Gemini ordinary observation = %s %v, want %s", state.completionEvidence(), err, test.want)
		}
		observation, err := proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader("data: "+test.raw+"\n\n"), "public", "req", 1)
		if err != nil || observation.NativeCompletionEvidence != test.want {
			t.Fatalf("Gemini stream observation = %+v %v, want %s", observation, err, test.want)
		}
	}
	withoutUsage := strings.Replace(geminiFixture(), `,"usageMetadata":`+geminiFinalUsage, "", 1)
	observation, err := proxyGeminiStream(context.Background(), httptest.NewRecorder(), strings.NewReader("data: "+withoutUsage+"\n\n"), "public", "req", 1)
	if err != nil || observation.Complete || observation.NativeCompletionEvidence != "completed" {
		t.Fatalf("missing usage changed independent native completion: %+v %v", observation, err)
	}
	for _, body := range []io.Reader{io.MultiReader(strings.NewReader("data: "+geminiFixture()+"\n\n"), geminiErrorReader{}), strings.NewReader("data: " + strings.Replace(geminiFixture(), `,"finishReason":"STOP"`, "", 1) + "\n\n")} {
		observation, err := proxyGeminiStream(context.Background(), httptest.NewRecorder(), body, "public", "req", 1)
		if err == nil || observation.NativeCompletionEvidence != "unknown" {
			t.Fatalf("missing clean native EOF fabricated completion: %+v %v", observation, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	observation, err = proxyGeminiStream(ctx, &cancelResponseWriter{httptest.NewRecorder(), cancel, "STOP"}, strings.NewReader("data: "+geminiFixture()+"\n\n"), "public", "req", 1)
	cancel()
	if !errors.Is(err, context.Canceled) || observation.NativeCompletionEvidence != "unknown" {
		t.Fatalf("Gemini canceled before EOF gained completion: %+v %v", observation, err)
	}
}
