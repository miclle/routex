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
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestGeminiNativeRequestOwnership(t *testing.T) {
	body := `{"contents":[{"role":"user","parts":[{"text":"hello","thoughtSignature":"opaque"},{"functionResponse":{"name":"f","response":{"fileUri":"opaque user JSON"}}}]}],"generationConfig":{"temperature":0.1234567890123456789},"tools":[{"functionDeclarations":[{"name":"f","parameters":{"properties":{"cachedContent":{"type":"string"}}}}]}]}`
	payload, name, stream, err := parseGatewayGemini([]byte(body), "public.alias", true)
	if err != nil || name != "public.alias" || !stream || !strings.Contains(string(payload["generationConfig"]), "0.1234567890123456789") {
		t.Fatal("native request changed", err)
	}
	for _, tail := range []string{`"cachedContent":"cachedContents/foreign"`, `"cached_content":"foreign"`, `"contents":[{"parts":[{"file_data":{"file_uri":"https://foreign"}}]}]`, `"contents":[{"parts":[{"fileData":{"fileUri":"files/foreign"}}]}]`, `"tools":[{"file_search":{"file_search_store_names":["foreign"]}}]`, `"generationConfig":{"candidateCount":9}`, `"generationConfig":{"candidateCount":1,"candidate_count":2}`, `"systemInstruction":{},"system_instruction":{}`} {
		if _, _, _, err := parseGatewayGemini([]byte(`{"contents":[{"parts":[{"text":"hi"}]}],`+tail+`}`), "model", false); err == nil {
			t.Fatal("unsafe payload accepted", tail)
		}
	}
	for _, path := range []string{"model:generateContent:other", "tunedModels/foreign:generateContent", "../model:generateContent", "model:countTokens"} {
		if _, _, err := GeminiAction(path); err == nil {
			t.Fatal("foreign method/path accepted")
		}
	}
}
func TestGeminiNativeDispatchAndErrorIsolation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1beta/models/provider-model:streamGenerateContent" || r.URL.RawQuery != "alt=sse" || r.Header.Get("x-goog-api-key") != "test-upstream-secret" || r.Header.Get("Authorization") != "" {
			t.Error("native dispatch wrong")
		}
		var payload map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["contents"] == nil || payload["model"] != nil || payload["stream"] != nil {
			t.Error("translated native request")
		}
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"private secret","details":["private"]}}`)
	}))
	defer server.Close()
	svc, data, bearer := runtimeFixture(t, server.URL+"/v1beta")
	data.Connections[0].Protocol = entity.ProtocolGeminiGenerateContent
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_gemini", Models: routes, PublishedAt: time.Now()})
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "gemini.db"), 1, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayGemini(context.Background(), bearer, []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`), "req_gemini", "public-model", true)
	if result != nil && result.Response != nil {
		_ = result.Response.Body.Close()
	}
	native, ok := err.(*GatewayError)
	if !ok || native.Status != 429 || result.NativeProtocol() != entity.ProtocolGeminiGenerateContent {
		t.Fatalf("wrong result %v", err)
	}
	raw, _ := json.Marshal(native.NativeError)
	if strings.Contains(string(raw), "private") || !strings.Contains(string(raw), "RESOURCE_EXHAUSTED") {
		t.Fatal("error not sanitized")
	}
	if _, _, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIChat); err == nil {
		t.Fatal("cross protocol fallback")
	}
	if calls != 1 {
		t.Fatal("unexpected retry")
	}
}
func TestGeminiDiscoveryPaginationAndCapabilities(t *testing.T) {
	for _, mode := range []string{"complete", "cycle", "bad-name", "too-many", "oversized", "unauthorized", "invalid-json"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/models" || r.Header.Get("x-goog-api-key") != "secret" || r.Header.Get("Authorization") != "" || r.URL.Query().Get("pageSize") != "1000" {
					t.Error("discovery transport wrong")
				}
				switch mode {
				case "unauthorized":
					w.WriteHeader(401)
					return
				case "oversized":
					_, _ = io.WriteString(w, strings.Repeat("x", (2<<20)+1))
					return
				case "bad-name":
					_, _ = io.WriteString(w, `{"models":[{"name":"tunedModels/foreign"}]}`)
					return
				case "invalid-json":
					_, _ = io.WriteString(w, `{"models":null}`)
					return
				case "too-many":
					_, _ = io.WriteString(w, `{"models":[`)
					for i := 0; i < 1001; i++ {
						if i > 0 {
							_, _ = io.WriteString(w, ",")
						}
						_, _ = fmt.Fprintf(w, `{"name":"models/a%d"}`, i)
					}
					_, _ = io.WriteString(w, `]}`)
					return
				}
				if calls == 1 || mode == "cycle" {
					_, _ = io.WriteString(w, `{"models":[{"name":"models/text","supportedGenerationMethods":["generateContent"]},{"name":"models/embed","supportedGenerationMethods":["embedContent"]}],"nextPageToken":"cursor"}`)
					return
				}
				if r.URL.Query().Get("pageToken") != "cursor" {
					t.Error("missing cursor")
				}
				_, _ = io.WriteString(w, `{"models":[{"name":"models/text","supportedGenerationMethods":["generateContent"]},{"name":"models/text2","supportedGenerationMethods":["generateContent"]}]}`)
			}))
			defer server.Close()
			svc, _, _ := runtimeFixture(t, server.URL)
			names, valid := svc.discoverModels(context.Background(), entity.ProviderConnection{Protocol: entity.ProtocolGeminiGenerateContent, BaseURL: server.URL}, "secret")
			if valid != (mode == "complete") {
				t.Fatalf("bad discovery %v %v", names, valid)
			}
			if valid && (len(names) != 2 || calls != 2) {
				t.Fatal("capability filter/dedup failed")
			}
		})
	}
}
