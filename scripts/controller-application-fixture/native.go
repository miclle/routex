package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

const prompt = "Controlled acceptance"
const answer = "Controlled response"
const upstreamModel = "controller-native-upstream"

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model     string    `json:"model"`
	Messages  []message `json:"messages"`
	Stream    bool      `json:"stream"`
	MaxTokens int       `json:"max_tokens"`
}
type usage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
	Total      int `json:"total_tokens"`
}
type choice struct {
	Index   int `json:"index"`
	Message struct {
		Role         string          `json:"role"`
		Content      string          `json:"content"`
		ToolCalls    json.RawMessage `json:"tool_calls,omitempty"`
		FunctionCall json.RawMessage `json:"function_call,omitempty"`
		Refusal      json.RawMessage `json:"refusal,omitempty"`
	} `json:"message"`
	Finish string `json:"finish_reason"`
}
type completion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type peer struct {
	exhausted   chan struct{}
	boundOnce   sync.Once
	mu          sync.Mutex
	secret      []byte
	finishes    []string
	maxRequests int
	requests    int
	calls       int
	out         io.Writer
}

func newPeer(secret []byte, finishes []string, maxRequests int, out io.Writer) *peer {
	return &peer{exhausted: make(chan struct{}), secret: secret, finishes: append([]string{}, finishes...), maxRequests: maxRequests, out: out}
}
func (p *peer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests++
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	reject := func(status int, code string) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":"controlled_request_rejected"}`)
		emit(p.out, result{Result: code, Status: status, Native: "unknown", Kind: "peer", Ordinal: p.requests})
	}
	if p.requests > p.maxRequests {
		p.boundOnce.Do(func() { close(p.exhausted) })
		reject(429, "request_bound")
		return
	}
	// Do not expose route, request bodies, headers, submitted IDs or auth material.
	auth := r.Header.Values("Authorization")
	expected := append([]byte("Bearer "), p.secret...)
	defer clear(expected)
	if len(auth) != 1 || subtle.ConstantTimeCompare([]byte(auth[0]), expected) != 1 {
		reject(401, "unauthorized")
		return
	}
	if r.URL.RawQuery != "" || r.URL.EscapedPath() != r.URL.Path {
		reject(404, "path_rejected")
		return
	}
	if r.URL.Path == "/v1/models" && r.Method == http.MethodGet {
		if r.ContentLength > 0 || r.TransferEncoding != nil {
			reject(400, "body_rejected")
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"controller-native-upstream"}]}`)
		emit(p.out, result{Result: "discovery", Status: 200, Native: "unknown", Kind: "peer", Ordinal: p.requests})
		return
	}
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		reject(404, "path_rejected")
		return
	}
	if r.ContentLength > maxBody {
		reject(413, "body_bound")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	defer clear(raw)
	if err != nil || len(raw) > maxBody {
		reject(413, "body_bound")
		return
	}
	var input chatRequest
	if decodeExact(raw, &input) != nil || input.Model != upstreamModel || input.Stream || input.MaxTokens != 16 || len(input.Messages) != 1 || input.Messages[0] != (message{Role: "user", Content: prompt}) {
		reject(400, "body_rejected")
		return
	}
	if p.calls >= len(p.finishes) {
		reject(429, "call_bound")
		return
	}
	finish := p.finishes[p.calls]
	p.calls++
	response := completion{ID: "chatcmpl-controller-native", Object: "chat.completion", Model: upstreamModel, Choices: make([]choice, 1), Usage: usage{3, 2, 5}}
	response.Choices[0].Message.Role = "assistant"
	response.Choices[0].Message.Content = answer
	response.Choices[0].Finish = finish
	_ = json.NewEncoder(w).Encode(response)
	native := "completed"
	if finish == "length" {
		native = "incomplete"
	}
	emit(p.out, result{Result: "call", Status: 200, Native: native, Kind: "peer", Ordinal: p.calls})
}

func newClient() *http.Client {
	// ProxyFromEnvironment and the default retry-capable shared transport are not used.
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, DisableKeepAlives: true, MaxResponseHeaderBytes: 4096, ResponseHeaderTimeout: 5 * time.Second, ExpectContinueTimeout: time.Second}
	return &http.Client{Transport: transport, Timeout: 8 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}
func invoke(parent context.Context, url string, key []byte, client *http.Client) result {
	r := result{Result: "request_failed", Native: "unknown"}
	input := chatRequest{Model: "controller-native", Messages: []message{{Role: "user", Content: prompt}}, MaxTokens: 16}
	body, _ := json.Marshal(input)
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return r
	}
	// A non-replayable request body and no idempotency headers forbid transport retries.
	request.GetBody = nil
	request.Header.Set("Authorization", "Bearer "+string(key))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	request.Header.Del("Authorization")
	if err != nil {
		return r
	}
	defer func() { _ = response.Body.Close() }()
	r.Status = response.StatusCode
	ids := response.Header.Values("X-Request-ID")
	if len(ids) == 1 && requestPattern.MatchString(ids[0]) {
		r.RequestID = ids[0]
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	defer clear(raw)
	if err != nil || len(raw) > maxResponse {
		r.Result = "response_invalid"
		return r
	}
	r.Result = "response"
	if r.Status != http.StatusOK {
		return r
	}
	if response.Header.Get("Content-Type") != "application/json" || r.RequestID == "" {
		r.Result = "response_invalid"
		return r
	}
	r.Native = observeNative(raw)
	if r.Native == "unknown" {
		r.Result = "response_invalid"
	}
	return r
}
func observeNative(raw []byte) string {
	var c completion
	if !validJSONObject(raw) || json.Unmarshal(raw, &c) != nil || c.ID != "chatcmpl-controller-native" || c.Object != "chat.completion" || (c.Model != upstreamModel && c.Model != "controller-native") || len(c.Choices) != 1 || c.Usage != (usage{3, 2, 5}) {
		return "unknown"
	}
	choice := c.Choices[0]
	present := func(raw json.RawMessage) bool {
		return len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
	}
	if choice.Index != 0 || choice.Message.Role != "assistant" || choice.Message.Content != answer || present(choice.Message.ToolCalls) || present(choice.Message.FunctionCall) || present(choice.Message.Refusal) {
		return "unknown"
	}
	switch choice.Finish {
	case "stop":
		return "completed"
	case "length":
		return "incomplete"
	default:
		return "unknown"
	}
}

// This distinct fixed-file mode never overrides the Agent-installed slot reader.
type probeResult struct {
	Result    string `json:"result"`
	Status    int    `json:"status,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func emitProbe(out io.Writer, r probeResult) { _ = json.NewEncoder(out).Encode(r) }
func probePredecessor(parent context.Context, url string, key []byte, client *http.Client) probeResult {
	observed := invoke(parent, url, key, client)
	r := probeResult{Result: "probe_failed", Status: observed.Status, RequestID: observed.RequestID}
	if observed.Result == "response" && observed.Status == http.StatusUnauthorized {
		r.Result = "revoked_rejected"
	}
	return r
}
