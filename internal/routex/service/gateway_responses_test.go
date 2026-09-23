package service

import (
	"context"
	"encoding/json"
	"errors"
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

func TestResponsesNativeRequestAndOwnership(t *testing.T) {
	raw := `{"model":"public-model","input":[{"role":"user","content":[{"type":"input_text","text":"file_id is ordinary text"}]}],"stream":true,"stream_options":{"include_obfuscation":false},"temperature":0.1234567890123456789,"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"file_id":{"type":"string"},"container":{"type":"string"}}}}],"text":{"format":{"type":"json_schema","name":"output","schema":{"type":"object"}}},"reasoning":{"effort":"high"},"store":false}`
	payload, name, stream, err := parseGatewayResponses([]byte(raw))
	if err != nil || name != "public-model" || !stream || string(payload["temperature"]) != "0.1234567890123456789" || strings.Contains(string(payload["stream_options"]), "include_usage") {
		t.Fatalf("native fields altered: %+v %v", payload, err)
	}
	for _, tail := range []string{
		`"input":null`, `"background":true`, `"previous_response_id":"resp_foreign"`, `"conversation":{"id":"conv_foreign"}`, `"prompt":{"id":"prompt_foreign"}`, `"prompt_cache_options":{"comparison_response_id":"resp_foreign"}`,
		`"input":[{"type":"item_reference","id":"foreign"}]`, `"input":[{"role":"user","content":[{"type":"input_image","file_id":"file_foreign"}]}]`, `"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_foreign"}]}]`,
		`"tools":[{"type":"file_search","vector_store_ids":["vs_foreign"]}]`, `"tools":[{"type":"code_interpreter","container":"cntr_foreign"}]`, `"tools":[{"type":"code_interpreter","container":{"type":"auto","file_ids":["file_foreign"]}}]`, `"tools":[{"type":"mcp","connector_id":"connector_foreign","server_url":"https://example.com"}]`,
		`"input":[{"type":"computer_call_output","output":{"type":"computer_screenshot","file_id":"file_foreign"}}]`, `"input":[{"type":"mcp_approval_response","approval_request_id":"foreign"}]`,
	} {
		if _, _, _, err := parseGatewayResponses([]byte(`{"model":"public-model","input":"hello",` + tail + `}`)); err == nil {
			t.Fatalf("foreign resource accepted: %s", tail)
		}
	}
	for _, input := range []string{`"hello"`, `[{"type":"function_call_output","call_id":"call_1","output":"{\"file_id\":\"literal\"}"}]`, `[{"type":"reasoning","id":"rs_1","encrypted_content":"client-owned-history"}]`, `[{"role":"user","content":[{"type":"input_file","filename":"text.txt","file_data":"data:text/plain;base64,aGk="}]}]`} {
		if _, _, _, err := parseGatewayResponses([]byte(`{"model":"public-model","input":` + input + `,"background":false,"store":false}`)); err != nil {
			t.Fatalf("inline history rejected: %v", err)
		}
	}
}
func responsesRuntimeFixture(t *testing.T, base string) (*Service, *runtimeData, string) {
	t.Helper()
	svc, data, bearer := runtimeFixture(t, base)
	data.Connections[0].Protocol = entity.ProtocolOpenAIResponses
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_responses", Models: routes, PublishedAt: time.Now()})
	return svc, data, bearer
}
func TestResponsesRuntimeProtocolIsolation(t *testing.T) {
	svc, data, _ := runtimeFixture(t, "http://127.0.0.1/v1")
	data.Connections = append(data.Connections, entity.ProviderConnection{ID: "con_responses", ProviderID: "prv_one", BaseURL: "http://127.0.0.1/v1", Protocol: entity.ProtocolOpenAIResponses})
	data.ProviderModels = append(data.ProviderModels, entity.ProviderModel{ID: "pmd_responses", ConnectionID: "con_responses", UpstreamName: "native-response-model"})
	cipher, err := svc.secrets.Seal("crd_responses", "responses-secret")
	if err != nil {
		t.Fatal(err)
	}
	data.Credentials = append(data.Credentials, entity.ProviderCredential{ID: "crd_responses", ConnectionID: "con_responses", Ciphertext: cipher, Enabled: true, VerificationStatus: "verified"})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_responses", ProviderModelID: "pmd_responses"})
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_responses", ModelID: "mdl_one", ProviderModelID: "pmd_responses", Weight: 100})
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal("independent protocol weights rejected", err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_two", Models: routes})
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	chat, _, err := svc.runtimeRoute("mdl_one")
	if err != nil || chat.ProviderModelID != "pmd_one" {
		t.Fatal("chat protocol changed")
	}
	responses, secret, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIResponses)
	if err != nil || responses.ProviderModelID != "pmd_responses" || secret != "responses-secret" {
		t.Fatal("responses routed to chat")
	}
	svc.runtime.deniedProviderModels.Store("pmd_responses", uint64(1))
	if _, _, err := svc.runtimeProtocolRoute("mdl_one", entity.ProtocolOpenAIResponses); err == nil {
		t.Fatal("disabled Responses supply fell back to Chat")
	}
	if _, _, err := svc.runtimeRoute("mdl_one"); err != nil {
		t.Fatal("Responses tombstone disabled Chat")
	}
	data.Bindings[1].Weight = 50
	if _, err := svc.buildRuntimeRoutes(data); err == nil {
		t.Fatal("partial Responses weights accepted")
	}
}
func TestResponsesModelProtocolAvailability(t *testing.T) {
	svc, _, bearer := responsesRuntimeFixture(t, "https://example.com/v1")
	listed, err := svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(listed) != 1 || len(listed[0].Protocols) != 1 || listed[0].Protocols[0] != entity.ProtocolOpenAIResponses {
		t.Fatalf("protocol list: %+v %v", listed, err)
	}
	for _, candidates := range svc.runtime.routes.Load().Models {
		for _, candidate := range candidates {
			for _, credential := range candidate.Credentials {
				svc.InvalidateRuntimeCredential(credential.ID)
			}
		}
	}
	listed, err = svc.GatewayModels(context.Background(), bearer)
	if err != nil || len(listed) != 1 || len(listed[0].Protocols) != 0 {
		t.Fatalf("revoked route advertised: %+v %v", listed, err)
	}
	if _, _, _, err := parseGatewayResponses([]byte(`{"model":"public-model","input":"hello","tools":[{"type":"image_generation","input_image_mask":{"file_id":"file_foreign"}}]}`)); err == nil {
		t.Fatal("foreign mask accepted")
	}
}

func TestResponsesDispatchAdmissionAndNativeErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-upstream-secret" {
			t.Error("wrong native endpoint or credential")
		}
		var payload map[string]json.RawMessage
		if json.NewDecoder(r.Body).Decode(&payload) != nil || string(payload["model"]) != `"provider-model"` || string(payload["input"]) != `"hello"` {
			t.Error("native payload was translated")
		}
		if _, exists := payload["messages"]; exists {
			t.Error("chat payload leaked")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"error":{"type":"rate_limit_error","code":"rate_limit_exceeded","param":"model","message":"test-upstream-secret","debug":"test-upstream-secret"}}`)
	}))
	defer server.Close()
	svc, _, bearer := responsesRuntimeFixture(t, server.URL+"/v1")
	defer svc.upstream.CloseIdleConnections()
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "journal.db"), 1, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayResponses(context.Background(), bearer, []byte(`{"model":"public-model","input":"hello"}`), "req_native")
	if result != nil && result.Response != nil {
		_ = result.Response.Body.Close()
	}
	var native *GatewayError
	if !errors.As(err, &native) || native.Status != 429 || result.NativeProtocol() != entity.ProtocolOpenAIResponses {
		t.Fatalf("native error status lost: %v", err)
	}
	raw, _ := json.Marshal(native.NativeError)
	if strings.Contains(string(raw), "test-upstream-secret") || !strings.Contains(string(raw), "rate_limit_exceeded") {
		t.Fatal("native error sanitization failed")
	}
	if calls.Load() != 1 {
		t.Fatal("request retried")
	}
	result, err = svc.GatewayResponses(context.Background(), bearer, []byte(`{"model":"public-model","input":"hello"}`), "req_full")
	if !errors.Is(err, callQueueUnavailable) || result.AttemptID != "" || calls.Load() != 1 {
		t.Fatal("journal-full request reached upstream")
	}
}
