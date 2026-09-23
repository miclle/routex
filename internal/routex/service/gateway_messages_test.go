package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestMessagesNativeParametersHeadersAndOwnership(t *testing.T) {
	raw := `{"model":"public-model","messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"5m"}}]}],"max_tokens":0,"system":"system","stop_sequences":["END"],"thinking":{"type":"adaptive"},"output_config":{"format":{"type":"json_schema","schema":{"properties":{"file_id":{"type":"string"}}}}},"tools":[{"name":"lookup","input_schema":{"properties":{"container_id":{"type":"string"}}}}],"temperature":0.1234567890123456789}`
	payload, name, stream, err := parseGatewayMessages([]byte(raw))
	if err != nil || name != "public-model" || stream || string(payload["temperature"]) != "0.1234567890123456789" {
		t.Fatalf("native payload altered %v", err)
	}
	for _, tail := range []string{`"container":"container_other"`, `"container":{"skills":[{"skill_id":"foreign"}]}`, `"messages":[{"role":"user","content":[{"type":"image","source":{"type":"file","file_id":"file_other"}}]}]`, `"messages":[{"role":"user","content":[{"type":"container_upload","file_id":"other"}]}]`, `"messages":[{"role":"assistant","content":[{"type":"text","text":"hello","citations":[{"file_id":"other"}]}]}]`, `"mcp_servers":[{"url":"https://example.com","connector_id":"other"}]`, `"messages":null`, `"max_tokens":-1`, `"stream":null`} {
		if _, _, _, err := parseGatewayMessages([]byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":8,` + tail + `}`)); err == nil {
			t.Fatalf("unsafe/invalid native field accepted: %s", tail)
		}
	}
	for _, content := range []string{`[{"type":"tool_use","id":"tool_1","name":"tool","input":{"file_id":"opaque-user-data"}}]`, `[{"type":"thinking","thinking":"unchanged","signature":"signed"},{"type":"redacted_thinking","data":"opaque"}]`, `[{"type":"document","source":{"type":"text","data":"inline"}}]`, `[{"type":"image","source":{"type":"base64","data":"aGk="}}]`} {
		if !safeMessagesContent([]byte(content)) {
			t.Fatal("inline native content rejected", content)
		}
	}
	for _, headers := range []MessagesHeaders{{"", ""}, {"2023-06-01\r\nsecret", ""}, {"2023-06-01", "beta\r\nsecret"}, {"2023-06-01", strings.Repeat("b,", 33)}, {"2023-06-01", strings.Repeat("x", 4097)}} {
		if ValidateMessagesHeaders(headers) == nil {
			t.Fatal("unsafe headers accepted")
		}
	}
	if ValidateMessagesHeaders(MessagesHeaders{"2023-06-01", "thinking-2025-01-01, beta-two"}) != nil {
		t.Fatal("valid native headers rejected")
	}
}
func TestMessagesNativeDispatchAndProtocolIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-upstream-secret" || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("anthropic-beta") != "test-beta" {
			t.Error("native transport headers lost")
		}
		var payload map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if string(payload["model"]) != `"provider-model"` || payload["messages"] == nil {
			t.Error("native body translated")
		}
		w.WriteHeader(529)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"upstream-secret","diagnostic":"upstream-secret"},"request_id":"private"}`)
	}))
	defer server.Close()
	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	data.Connections[0].Protocol = entity.ProtocolAnthropicMessages
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_messages", Models: routes, PublishedAt: time.Now()})
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "messages.db"), 1, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayMessages(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":8}`), "req_messages", MessagesHeaders{"2023-06-01", "test-beta"})
	if result != nil && result.Response != nil {
		_ = result.Response.Body.Close()
	}
	native, ok := err.(*GatewayError)
	if !ok || native.Status != 529 || result.NativeProtocol() != entity.ProtocolAnthropicMessages {
		t.Fatalf("native error lost %v", err)
	}
	raw, _ := json.Marshal(native.NativeError)
	if strings.Contains(string(raw), "upstream-secret") || !strings.Contains(string(raw), "overloaded_error") {
		t.Fatal("native error unsafe")
	}
	if _, _, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIChat); err == nil {
		t.Fatal("cross protocol fallback")
	}
	if calls.Load() != 1 {
		t.Fatal("unexpected retry")
	}
}
func TestMessagesBoundedNativeDiscovery(t *testing.T) {
	for _, mode := range []string{"complete", "cycle", "bad-last", "missing-has-more", "oversized", "unauthorized", "too-many"} {
		t.Run(mode, func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("x-api-key") != "secret" || r.Header.Get("Authorization") != "" || r.Header.Get("anthropic-version") != "2023-06-01" || r.URL.Query().Get("limit") != "1000" {
					t.Error("discovery headers wrong")
				}
				switch mode {
				case "unauthorized":
					w.WriteHeader(401)
					return
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat("x", (2<<20)+1))
					return
				case "missing-has-more":
					_, _ = io.WriteString(w, `{"data":[]}`)
					return
				case "too-many":
					_, _ = fmt.Fprint(w, `{"data":[`)
					for index := 0; index < 1001; index++ {
						if index > 0 {
							_, _ = fmt.Fprint(w, ",")
						}
						_, _ = fmt.Fprintf(w, `{"id":"m%d","type":"model"}`, index)
					}
					_, _ = fmt.Fprint(w, `],"has_more":false}`)
					return
				}
				if calls == 1 || mode == "cycle" {
					last := "a"
					if mode == "bad-last" {
						last = "wrong"
					}
					_, _ = fmt.Fprintf(w, `{"data":[{"id":"a","type":"model"}],"has_more":true,"last_id":%q}`, last)
					return
				}
				if r.URL.Query().Get("after_id") != "a" {
					t.Error("cursor not sent")
				}
				_, _ = io.WriteString(w, `{"data":[{"id":"a","type":"model"},{"id":"b","type":"model"}],"has_more":false,"last_id":"b"}`)
			}))
			defer server.Close()
			svc, _, _ := runtimeFixture(t, server.URL)
			names, valid := svc.discoverModels(context.Background(), entity.ProviderConnection{Protocol: entity.ProtocolAnthropicMessages, BaseURL: server.URL}, "secret")
			if (mode == "complete") != valid {
				t.Fatalf("discovery %s: %v %v", mode, names, valid)
			}
			if valid && (len(names) != 2 || calls != 2) {
				t.Fatal("paging/dedup broken")
			}
		})
	}
}
