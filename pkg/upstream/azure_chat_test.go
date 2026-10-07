package upstream

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAzureClassicChatExactTransport(t *testing.T) {
	payload := map[string]json.RawMessage{"model": json.RawMessage(`"logical"`), "messages": json.RawMessage(`[{"role":"user","content":"hello"}]`), "temperature": json.RawMessage(`0.1234567890123456789`), "stream": json.RawMessage(`true`), "stream_options": json.RawMessage(`{"include_usage":true}`)}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.EscapedPath() != "/openai/deployments/deploy-A.1/chat/completions" || r.URL.RawQuery != "api-version=2024-10-21" || r.Header.Get("api-key") != "test-only-key" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "text/event-stream" {
			t.Error("transport mismatch")
		}
		raw, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil || body["model"] != nil || string(body["temperature"]) != "0.1234567890123456789" || string(body["stream_options"]) != `{"include_usage":true}` {
			t.Error("body mismatch")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	req, e := NewAzureChatRequest(context.Background(), server.URL+"/", "deploy-A.1", "2024-10-21", "test-only-key", payload, true)
	if e != nil {
		t.Fatal(e)
	}
	client := NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	response, e := client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = response.Body.Close() }()
	raw, e := io.ReadAll(response.Body)
	if e != nil || string(raw) != "data: [DONE]\n\n" || calls.Load() != 1 {
		t.Fatal("response changed or replayed")
	}
	if string(payload["model"]) != `"logical"` || req.GetBody != nil {
		t.Fatal("caller mutated or replay body retained")
	}
}

func TestAzureClassicRejectsUnsafeRequests(t *testing.T) {
	for _, bad := range []struct{ url, model, version, key string }{
		{"https://example.com/v1", "deployment", "2024-10-21", "key"},
		{"https://example.com/a/..", "deployment", "2024-10-21", "key"},
		{"https://example.com/./", "deployment", "2024-10-21", "key"},
		{"https://example.com/%2e/", "deployment", "2024-10-21", "key"},
		{"https://example.com/%2e%2e", "deployment", "2024-10-21", "key"},
		{"https://example.com//", "deployment", "2024-10-21", "key"},
		{"https://example.com/\\", "deployment", "2024-10-21", "key"},
		{"https://example.com/\n", "deployment", "2024-10-21", "key"},
		{"https://example.com/\t", "deployment", "2024-10-21", "key"},
		{"https://example.com/%2f", "deployment", "2024-10-21", "key"},
		{"https://example.com/?q=x", "deployment", "2024-10-21", "key"},
		{"https://u:p@example.com", "deployment", "2024-10-21", "key"},
		{"https://example.com", "../x", "2024-10-21", "key"},
		{"https://example.com", "x%2fy", "2024-10-21", "key"},
		{"https://example.com", "x", "", "key"},
		{"https://example.com", "x", "2024-02-30", "key"},
		{"https://example.com", "x", "2024-10-21&x=y", "key"},
		{"https://example.com", "x", "2024-10-21", "private\r\nX: secret"},
	} {
		r, e := NewAzureChatRequest(context.Background(), bad.url, bad.model, bad.version, bad.key, map[string]json.RawMessage{}, false)
		if e == nil || r != nil || strings.Contains(e.Error(), bad.key) {
			t.Errorf("unsafe request accepted or leaked: %s", bad.version)
		}
	}
	for _, raw := range []string{`null`, `"true"`, `1`, `{`} {
		if _, e := NewAzureChatRequest(context.Background(), "https://example.com", "x", "2024-10-21", "key", map[string]json.RawMessage{"stream": json.RawMessage(raw)}, false); e == nil {
			t.Fatal("invalid stream accepted")
		}
	}
	if !ValidAzureAPIVersion("2024-10-21-preview") || ValidAzureAPIVersion("0000-01-01") || ValidAzureDeployment(strings.Repeat("a", 256)) {
		t.Fatal("bounds")
	}
}

func TestAzureClassicNoReplayOrRedirect(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusServiceUnavailable, 301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/other" {
					t.Error("api-key request forwarded to redirect target")
				}
				w.Header().Set("Location", "/other")
				w.WriteHeader(status)
			}))
			defer server.Close()
			req, e := NewAzureChatRequest(context.Background(), server.URL, "x", "2024-10-21", "key", map[string]json.RawMessage{}, true)
			if e != nil {
				t.Fatal(e)
			}
			client := NewNonReplayingClient(true)
			defer client.CloseIdleConnections()
			resp, e := client.Do(req)
			if resp != nil {
				_ = resp.Body.Close()
			}
			if status < 300 || status > 399 {
				if e != nil {
					t.Fatal(e)
				}
			}
			if resp == nil || resp.StatusCode != status {
				t.Fatal("status changed")
			}
			if (status == 307 || status == 308) && (e != nil || resp == nil || resp.StatusCode != status) {
				t.Fatal("redirect response changed")
			}
			if calls.Load() != 1 {
				t.Fatal("request replayed")
			}
		})
	}
}

func TestAzureClassicCancellationBeforeDispatch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, e := NewAzureChatRequest(ctx, server.URL, "x", "2024-10-21", "key", map[string]json.RawMessage{}, true)
	if e != nil {
		t.Fatal(e)
	}
	c := NewNonReplayingClient(true)
	defer c.CloseIdleConnections()
	resp, e := c.Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if e == nil || calls.Load() != 0 {
		t.Fatal("canceled request dispatched")
	}
}

func TestAzureClassicCancellationDuringSingleDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started, stopped := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		close(stopped)
	}))
	defer server.Close()
	req, e := NewAzureChatRequest(ctx, server.URL, "deployment", "2024-10-21", "key", map[string]json.RawMessage{}, true)
	if e != nil {
		t.Fatal(e)
	}
	client := NewNonReplayingClient(true)
	defer client.CloseIdleConnections()
	done := make(chan error, 1)
	go func() {
		response, err := client.Do(req)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("dispatch did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled dispatch succeeded")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation not bounded")
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("remote cancellation not propagated")
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled dispatch replayed")
	}
}
