package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const messagesModel = "controller-native-messages"
const messagesUpstreamModel = "controller-messages-upstream"
const maxMessagesBody = 256 << 10

type messagesIdentity struct {
	SourceCommit string `json:"source_commit"`
	RunID        string `json:"run_id"`
	Phase        string `json:"phase"`
}
type messagesServeConfig struct {
	messagesIdentity
	Listen          string `json:"listen"`
	LifetimeSeconds int    `json:"lifetime_seconds"`
	MaxRequests     int    `json:"max_requests"`
}

// These separate inputs cannot widen the existing Chat operation's grammar.
func readMessagesConfig(path string, target any, fields ...string) error {
	raw, err := privateRead(path, 4096, false)
	if err != nil {
		return errInvalid
	}
	defer clear(raw)
	if !validJSONObject(raw) {
		return errInvalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) != len(fields) {
		return errInvalid
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return errInvalid
		}
	}
	return decodeExact(raw, target)
}
func matchesMessagesIdentity(identity messagesIdentity) bool {
	return validClaudeBinding() && identity.SourceCommit == claudeSourceCommit && identity.RunID == claudeRunID && identity.Phase == claudePhase
}
func runMessagesServe(out io.Writer) int {
	var c messagesServeConfig
	if !validClaudeBinding() || readMessagesConfig(inputRoot+"/messages-serve.json", &c, "source_commit", "run_id", "phase", "listen", "lifetime_seconds", "max_requests") != nil || !matchesMessagesIdentity(c.messagesIdentity) || !validServeConfig(serveConfig{Listen: c.Listen, LifetimeSeconds: c.LifetimeSeconds, MaxRequests: c.MaxRequests, FinishReasons: []string{"stop"}}) {
		emit(out, result{Result: "messages_config_invalid", Native: "unknown"})
		return 1
	}
	secret, err := privateRead(inputRoot+"/upstream.secret", 4096, false)
	if err != nil || !validSecret(secret) {
		emit(out, result{Result: "secret_invalid", Native: "unknown"})
		return 1
	}
	defer clear(secret)
	parent, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(parent, time.Duration(c.LifetimeSeconds)*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp4", c.Listen)
	if err != nil {
		emit(out, result{Result: "listen_failed", Native: "unknown"})
		return 1
	}
	peer := newMessagesPeer(secret, c.MaxRequests, out)
	server := &http.Server{Handler: peer, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 4 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 4096, ErrorLog: log.New(io.Discard, "", 0)}
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		select {
		case <-ctx.Done():
		case <-peer.exhausted:
		}
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 2*time.Second)
		defer done()
		if server.Shutdown(shutdown) != nil {
			_ = server.Close()
		}
	}()
	emit(out, result{Result: "ready", Native: "unknown"})
	err = server.Serve(listener)
	cancel()
	<-closed
	if err != nil && err != http.ErrServerClosed {
		emit(out, result{Result: "serve_failed", Native: "unknown"})
		return 1
	}
	select {
	case <-peer.exhausted:
		emit(out, result{Result: "request_bound", Native: "unknown"})
		return 1
	default:
	}
	if parent.Err() != nil {
		emit(out, result{Result: "shutdown", Native: "unknown"})
		return 0
	}
	emit(out, result{Result: "deadline", Native: "unknown"})
	return 124
}

type messagesPeer struct {
	mu                       sync.Mutex
	secret                   []byte
	requests, calls, maximum int
	pair                     bool
	completed                int
	out                      io.Writer
	exhausted                chan struct{}
	once                     sync.Once
}

func newMessagesPeer(secret []byte, maximum int, out io.Writer) *messagesPeer {
	return &messagesPeer{secret: secret, maximum: maximum, out: out, exhausted: make(chan struct{})}
}
func (p *messagesPeer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests++
	w.Header().Set("Cache-Control", "no-store")
	reject := func(status int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"fixture_rejected"}`)
		emit(p.out, result{Result: "rejected", Status: status, Native: "unknown"})
	}
	if p.requests > p.maximum {
		p.once.Do(func() { close(p.exhausted) })
		reject(429)
		return
	}
	keys := r.Header.Values("X-Api-Key")
	if len(keys) != 1 || len(r.Header.Values("Authorization")) != 0 || subtle.ConstantTimeCompare([]byte(keys[0]), p.secret) != 1 || len(r.Header.Values("Anthropic-Version")) != 1 || r.Header.Get("Anthropic-Version") != "2023-06-01" {
		reject(401)
		return
	}
	if r.URL.EscapedPath() != r.URL.Path {
		reject(400)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/v1/models" && r.URL.RawQuery == "limit=1000" && r.ContentLength <= 0 && len(r.TransferEncoding) == 0 {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": messagesUpstreamModel, "type": "model", "display_name": "Controlled Messages"}}, "has_more": false, "first_id": messagesUpstreamModel, "last_id": messagesUpstreamModel})
		emit(p.out, result{Result: "discovery", Status: 200, Native: "unknown"})
		return
	}
	// count_tokens is optional for the client and is deliberately not implemented.
	if r.Method != http.MethodPost || r.URL.Path != "/v1/messages" || (r.URL.RawQuery != "" && r.URL.RawQuery != "beta=true") {
		reject(404)
		return
	}
	callLimit := 1
	if p.pair {
		callLimit = 2
	}
	if p.calls >= callLimit {
		p.once.Do(func() { close(p.exhausted) })
		reject(429)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMessagesBody+1))
	defer clear(body)
	if err != nil || len(body) > maxMessagesBody || !validMessagesRequest(body) {
		reject(400)
		return
	}
	p.calls++
	w.Header().Set("Content-Type", "text/event-stream")
	for _, frame := range messagesFrames() {
		if _, err = io.WriteString(w, frame); err != nil {
			emit(p.out, result{Result: "write_failed", Native: "unknown", Kind: "messages", Ordinal: p.calls})
			return
		}
		if err = http.NewResponseController(w).Flush(); err != nil {
			emit(p.out, result{Result: "write_failed", Native: "unknown", Kind: "messages", Ordinal: p.calls})
			return
		}
	}
	p.completed++
	emit(p.out, result{Result: "call", Status: 200, Native: "completed", Kind: "messages", Ordinal: p.calls})
}
func validMessagesRequest(raw []byte) bool {
	if !validJSONObject(raw) {
		return false
	}
	var body struct {
		Model     string `json:"model"`
		Stream    bool   `json:"stream"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	for key := range fields {
		for _, critical := range []string{"model", "stream", "max_tokens", "messages", "tools"} {
			if strings.EqualFold(key, critical) && key != critical {
				return false
			}
		}
	}
	if tools, present := fields["tools"]; present {
		var entries []json.RawMessage
		if json.Unmarshal(tools, &entries) != nil || len(entries) != 0 {
			return false
		}
	}
	if json.Unmarshal(raw, &body) != nil || body.Model != messagesUpstreamModel || !body.Stream || body.MaxTokens < 1 || body.MaxTokens > 65536 || len(body.Messages) != 1 || body.Messages[0].Role != "user" {
		return false
	}
	var text string
	if json.Unmarshal(body.Messages[0].Content, &text) != nil {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(body.Messages[0].Content, &blocks) != nil || len(blocks) < 1 || len(blocks) > 32 {
			return false
		}
		for _, block := range blocks {
			if block.Type != "text" {
				return false
			}
			text += block.Text + "\n"
		}
	}
	// Claude may add bounded text-only system reminders. The fixed launcher owns
	// the actual prompt; opaque SDK metadata is never executed, logged or stored.
	return strings.Count(text, prompt) == 1 && strings.HasSuffix(strings.TrimSpace(text), prompt)
}
func messagesFrames() []string {
	return []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_controller_native\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"" + messagesUpstreamModel + "\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Controlled response\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":2}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}
}
