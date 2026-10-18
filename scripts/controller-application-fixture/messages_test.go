package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func messagesTestRequest() string {
	return `{"model":"controller-messages-upstream","stream":true,"max_tokens":32000,"messages":[{"role":"user","content":[{"type":"text","text":"Controlled acceptance"}]}],"system":[{"type":"text","text":"SDK system"}],"tools":[]}`
}
func messagesRecorder(peer *messagesPeer, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("X-Api-Key", "controlled-upstream-secret-1234")
	request.Header.Set("Anthropic-Version", "2023-06-01")
	response := httptest.NewRecorder()
	peer.ServeHTTP(response, request)
	return response
}
func TestMessagesDiscoveryAndOrderedNativeSSE(t *testing.T) {
	var log bytes.Buffer
	peer := newMessagesPeer([]byte("controlled-upstream-secret-1234"), 8, &log)
	discovery := messagesRecorder(peer, "GET", "http://localhost/v1/models?limit=1000", "")
	var models struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
		HasMore *bool `json:"has_more"`
	}
	if discovery.Code != 200 || json.Unmarshal(discovery.Body.Bytes(), &models) != nil || models.HasMore == nil || *models.HasMore || len(models.Data) != 1 || models.Data[0].ID != messagesUpstreamModel || models.Data[0].Type != "model" {
		t.Fatal("Anthropic discovery contract")
	}
	response := messagesRecorder(peer, "POST", "http://localhost/v1/messages?beta=true", messagesTestRequest())
	if response.Code != 200 || response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal("Messages stream rejected")
	}
	frames := strings.Split(strings.TrimSuffix(response.Body.String(), "\n\n"), "\n\n")
	expected := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(frames) != len(expected) {
		t.Fatal("missing native terminal frame")
	}
	for i, frame := range frames {
		lines := strings.Split(frame, "\n")
		var object map[string]json.RawMessage
		var kind string
		if len(lines) != 2 || lines[0] != "event: "+expected[i] || !strings.HasPrefix(lines[1], "data: ") || json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &object) != nil || json.Unmarshal(object["type"], &kind) != nil || kind != expected[i] {
			t.Fatal("SSE order/type changed")
		}
	}
	if !strings.Contains(frames[0], `"input_tokens":3`) || !strings.Contains(frames[4], `"output_tokens":2`) || !strings.Contains(frames[4], `"stop_reason":"end_turn"`) || !strings.Contains(frames[2], answer) {
		t.Fatal("native usage/completion changed")
	}
	if peer.calls != 1 || strings.Contains(log.String(), "controlled-upstream-secret") || strings.Contains(log.String(), prompt) {
		t.Fatal("call attribution/log privacy changed")
	}
	duplicate := messagesRecorder(peer, "POST", "http://localhost/v1/messages", messagesTestRequest())
	if duplicate.Code != 429 || peer.calls != 1 {
		t.Fatal("extra inference admitted")
	}
}
func TestMessagesRejectsAuthQueryAndOptionalCountTokens(t *testing.T) {
	for _, entry := range []struct {
		path     string
		mutation func(*http.Request)
	}{
		{"/v1/messages", func(r *http.Request) { r.Header.Del("X-Api-Key") }},
		{"/v1/messages", func(r *http.Request) { r.Header.Add("X-Api-Key", "controlled-upstream-secret-1234") }},
		{"/v1/messages", func(r *http.Request) { r.Header.Set("Authorization", "Bearer another") }},
		{"/v1/messages", func(r *http.Request) { r.Header.Set("Anthropic-Version", "future") }},
		{"/v1/messages?beta=true&beta=true", func(*http.Request) {}},
		{"/v1/messages/count_tokens", func(*http.Request) {}},
	} {
		peer := newMessagesPeer([]byte("controlled-upstream-secret-1234"), 8, io.Discard)
		request := httptest.NewRequest("POST", "http://localhost"+entry.path, strings.NewReader(messagesTestRequest()))
		request.Header.Set("X-Api-Key", "controlled-upstream-secret-1234")
		request.Header.Set("Anthropic-Version", "2023-06-01")
		entry.mutation(request)
		recorder := httptest.NewRecorder()
		peer.ServeHTTP(recorder, request)
		if recorder.Code == 200 || peer.calls != 0 {
			t.Fatal("invalid peer request admitted")
		}
	}
}
func TestMessagesCriticalRequestAndDuplicateBounds(t *testing.T) {
	good := messagesTestRequest()
	for _, bad := range []string{
		strings.Replace(good, `"stream":true`, `"stream":false`, 1),
		strings.Replace(good, `"max_tokens":32000`, `"max_tokens":65537`, 1),
		strings.Replace(good, `"model":"controller-messages-upstream"`, `"model":"foreign"`, 1),
		strings.Replace(good, `"role":"user"`, `"role":"assistant"`, 1),
		strings.Replace(good, `"type":"text","text":"Controlled acceptance"`, `"type":"image","text":"Controlled acceptance"`, 1),
		strings.Replace(good, prompt, "different prompt", 1),
		strings.Replace(good, prompt, prompt+prompt, 1),
		strings.Replace(good, prompt, prompt+" trailing content", 1),
		strings.Replace(good, `"tools":[]`, `"tools":[{"name":"foreign"}]`, 1),
		strings.Replace(good, `"stream":true`, `"stream":true,"stream":true`, 1),
		strings.Replace(good, `"stream":true`, `"stream":false,"Stream":true`, 1),
	} {
		if validMessagesRequest([]byte(bad)) {
			t.Fatal("invalid native request accepted")
		}
	}
	peer := newMessagesPeer([]byte("controlled-upstream-secret-1234"), 8, io.Discard)
	response := messagesRecorder(peer, "POST", "http://localhost/v1/messages", strings.Repeat("x", maxMessagesBody+1))
	if response.Code != 400 || peer.calls != 0 {
		t.Fatal("body bound lost")
	}
}

type failingMessagesWriter struct {
	header  http.Header
	written int
}

func (w *failingMessagesWriter) Header() http.Header { return w.header }
func (w *failingMessagesWriter) WriteHeader(int)     {}
func (w *failingMessagesWriter) Write(p []byte) (int, error) {
	w.written++
	if w.written == len(messagesFrames()) {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func (*failingMessagesWriter) Flush() {}
func TestMessagesFailedTerminalWriteRemainsUnknown(t *testing.T) {
	var log bytes.Buffer
	peer := newMessagesPeer([]byte("controlled-upstream-secret-1234"), 8, &log)
	request := httptest.NewRequest("POST", "http://localhost/v1/messages", strings.NewReader(messagesTestRequest()))
	request.Header.Set("X-Api-Key", "controlled-upstream-secret-1234")
	request.Header.Set("Anthropic-Version", "2023-06-01")
	peer.ServeHTTP(&failingMessagesWriter{header: make(http.Header)}, request)
	if strings.Contains(log.String(), `"native":"completed"`) || !strings.Contains(log.String(), `"result":"write_failed"`) {
		t.Fatal("failed terminal became completion")
	}
}
