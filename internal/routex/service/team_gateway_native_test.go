package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

func TestTeamNativeTextPreservesOpaqueDataAndRejectsNativeMedia(t *testing.T) {
	cases := []struct {
		protocol, valid string
		invalid         []string
	}{
		{entity.ProtocolOpenAIResponses, `{"input":[{"type":"function_call","arguments":"{\"input_image\":\"opaque\"}"},{"type":"function_call_output","output":"routex://attachments/obj_private"},{"role":"user","content":[{"type":"input_text","text":"input_file is ordinary text"}]}],"tools":[{"type":"function","parameters":{"image_url":{"file_data":"opaque"}}}]}`, []string{
			`{"input":[{"role":"user","content":[{"type":"input_image","image_url":"routex://attachments/obj_private"}]}]}`,
			`{"input":[{"type":"function_call_output","output":[{"type":"input_file","file_data":"data:application/pdf;base64,AA=="}]}]}`,
			`{"input":[{"type":"computer_call_output","output":{"type":"computer_screenshot","image_url":"https://example.invalid/image"}}]}`,
			`{"input":"text","tools":[{"type":"image_generation","input_image_mask":{"image_url":"https://example.invalid/image"}}]}`,
		}},
		{entity.ProtocolAnthropicMessages, `{"messages":[{"role":"assistant","content":[{"type":"tool_use","input":{"source":{"type":"image","data":"opaque"}}}]},{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"routex://attachments/obj_private"}]}]}],"tools":[{"name":"opaque","input_schema":{"properties":{"image":{"type":"string"}}}}]}`, []string{
			`{"messages":[{"content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AA=="}}]}]}`,
			`{"messages":[{"content":[{"type":"tool_result","content":[{"type":"search_result","content":[{"type":"document","source":{"type":"text","data":"private"}}]}]}]}]}`,
			`{"messages":[{"content":"text"}],"system":[{"type":"image","source":{"type":"url","url":"https://example.invalid/image"}}]}`,
		}},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"text":"inlineData is text"},{"functionCall":{"args":{"inlineData":{"data":"opaque"}}}},{"functionResponse":{"response":{"fileUri":"opaque"},"parts":[{"text":"routex://attachments/obj_private"}]}}]}],"tools":[{"functionDeclarations":[{"parameters":{"properties":{"fileData":{"type":"string"}}}}]}],"generationConfig":{"candidateCount":1,"responseModalities":["TEXT"]}}`, []string{
			`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"routex://attachments/obj_private"}}]}]}`,
			`{"contents":[{"parts":[{"functionResponse":{"parts":[{"inline_data":{"mime_type":"application/pdf","data":"AA=="}}]}}]}]}`,
			`{"contents":[{"parts":[{"text":"text","videoMetadata":{}}]}]}`,
			`{"contents":[{"parts":[{"text":"text"}]}],"generationConfig":{"response_modalities":["AUDIO"]}}`,
			`{"contents":[{"parts":[{"text":"text"}]}],"generationConfig":{"candidateCount":2}}`,
			`{"contents":[{"parts":[{"text":"text"}]}],"systemInstruction":{"parts":[{"inlineData":{"data":"AA=="}}]}}`,
		}},
	}
	for _, tc := range cases {
		t.Run(tc.protocol, func(t *testing.T) {
			payload := quotaPayload(t, tc.valid)
			before, _ := json.Marshal(payload)
			if err := validateTeamNativeText(tc.protocol, payload); err != nil {
				t.Fatal("opaque native text rejected", err)
			}
			after, _ := json.Marshal(payload)
			if string(before) != string(after) {
				t.Fatal("native data rewritten")
			}
			for _, raw := range tc.invalid {
				if validateTeamNativeText(tc.protocol, quotaPayload(t, raw)) == nil {
					t.Fatal("native media accepted", raw)
				}
			}
			for _, field := range []string{"team_id", "project_id", "workspace_id", "user_id", "session_id"} {
				payload[field] = json.RawMessage(`null`)
				if validateTeamNativeText(tc.protocol, payload) == nil {
					t.Fatal("workspace selector accepted", field)
				}
				delete(payload, field)
			}
		})
	}
	// Native recursive positions are bounded; opaque inputs are never recursed.
	content := `[{"type":"text","text":"text"}]`
	for range 8 {
		content = `[{"type":"tool_result","content":` + content + `}]`
	}
	if teamMessagesContent(json.RawMessage(content), 0) {
		t.Fatal("unbounded native tool result depth")
	}
}

func teamNativeFixture(t *testing.T, base, protocol string) (*Service, *TeamSessionIdentity) {
	t.Helper()
	svc, identity := teamGatewayFixture(t, base)
	routes := svc.runtime.routes.Load()
	for model, candidates := range routes.Models {
		for i := range candidates {
			candidates[i].Route.Protocol = protocol
		}
		routes.Models[model] = candidates
	}
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "native-team.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close(); svc.upstream.CloseIdleConnections() })
	svc.recorder = &callRecorder{queue: queue}
	return svc, identity
}
func teamNativeInvoke(svc *Service, identity *TeamSessionIdentity, protocol, raw, requestID string) (*GatewayResult, error) {
	switch protocol {
	case entity.ProtocolOpenAIResponses:
		return svc.TeamGatewayResponses(context.Background(), identity, []byte(raw), requestID)
	case entity.ProtocolAnthropicMessages:
		return svc.TeamGatewayMessages(context.Background(), identity, []byte(raw), requestID, MessagesHeaders{Version: "2023-06-01"})
	default:
		return svc.TeamGatewayGemini(context.Background(), identity, []byte(raw), requestID, "public-model", false)
	}
}
func teamNativeInput(protocol string) string {
	switch protocol {
	case entity.ProtocolOpenAIResponses:
		return `{"model":"public-model","input":"hello","max_output_tokens":10}`
	case entity.ProtocolAnthropicMessages:
		return `{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`
	default:
		return `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":10,"candidateCount":1}}`
	}
}
func TestTeamNativeDispatchUsesExactTeamAndProtocolWithoutKeyOrDatabase(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				paths := map[string]string{entity.ProtocolOpenAIResponses: "/v1/responses", entity.ProtocolAnthropicMessages: "/v1/messages", entity.ProtocolGeminiGenerateContent: "/v1/models/provider-model:generateContent"}
				if r.URL.Path != paths[protocol] {
					t.Error("cross protocol route", r.URL.Path)
				}
				if protocol == entity.ProtocolGeminiGenerateContent {
					if r.Header.Get("x-goog-api-key") != "test-upstream-secret" || r.Header.Get("Authorization") != "" {
						t.Error("native provider authentication changed")
					}
				} else if protocol == entity.ProtocolAnthropicMessages {
					if r.Header.Get("x-api-key") != "test-upstream-secret" || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
						t.Error("native Messages headers changed")
					}
				} else if r.Header.Get("Authorization") != "Bearer test-upstream-secret" {
					t.Error("provider authentication changed")
				}
				var payload map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&payload) != nil {
					t.Error("native body invalid")
				}
				if payload["team_id"] != nil || payload["user_id"] != nil || payload["session_id"] != nil {
					t.Error("private invocation identity forwarded")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			svc, identity := teamNativeFixture(t, server.URL+"/v1", protocol)
			svc.db = nil
			svc.secrets = nil
			result, err := teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_native_team")
			if err != nil || result == nil || result.Response == nil {
				t.Fatal(result, err)
			}
			_ = result.Response.Body.Close()
			if calls.Load() != 1 || !result.Admitted || result.TeamID != identity.TeamID || result.TeamMembershipID != identity.TeamMembershipID || result.UserID != identity.UserID || result.KeyID != "" || result.ProjectID != "" || result.NativeProtocol() != protocol {
				t.Fatal("native Team identity lost", result, calls.Load())
			}
			scopes, err := svc.gatewayLimits(context.Background(), result)
			if err != nil || len(scopes) != 2 || scopes[0].Account != "team_tem_one" || scopes[1].Account != teamMemberLimitAccount(identity.TeamID, identity.UserID) {
				t.Fatal("native invocation borrowed Key/Personal accounting", scopes, err)
			}
		})
	}
}
func TestTeamNativeRevocationAfterAdmissionDoesNotDispatch(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		for _, revoke := range []string{"session", "User", "Team", "member", "rejoin", "grant", "lease"} {
			t.Run(protocol+"/"+revoke, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{}`) }))
				defer server.Close()
				svc, identity := teamNativeFixture(t, server.URL+"/v1", protocol)
				svc.afterGatewayAdmission = func() {
					switch revoke {
					case "session":
						svc.invalidateRuntimeSession(identity.SessionID)
					case "User":
						svc.invalidateRuntimeSessionUser(identity.UserID)
					case "Team":
						svc.invalidateRuntimeTeam(identity.TeamID)
					case "member":
						svc.invalidateRuntimeTeamMember(identity.TeamID, identity.UserID)
					case "rejoin":
						auth := svc.runtime.auth.Load()
						team := auth.Teams[identity.TeamID]
						team.Members[identity.UserID] = "tmm_rejoined"
					case "grant":
						auth := svc.runtime.auth.Load()
						team := auth.Teams[identity.TeamID]
						delete(team.Models, "mdl_one")
					case "lease":
						svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
					}
				}
				result, err := teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_native_revoked")
				if err == nil || result == nil || !result.Admitted || calls.Load() != 0 || result.AttemptID != "" || !result.NoUpstreamWork() {
					t.Fatal("revoked Team native subject dispatched", result, err, calls.Load())
				}
			})
		}
	}
}
func TestTeamNativeFiniteTokenPoliciesFailBeforeDispatch(t *testing.T) {
	for _, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		t.Run(protocol, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{}`) }))
			defer server.Close()
			svc, identity := teamNativeFixture(t, server.URL+"/v1", protocol)
			auth := svc.runtime.auth.Load()
			auth.LimitPolicies[limitAccount("team", identity.TeamID)] = limits.Policy{TokensMonth: limitNumber(1000)}
			result, err := teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_native_missing_bound")
			var gateway *GatewayError
			if !errors.As(err, &gateway) || gateway.Code != "quota_bound_unavailable" || result == nil || result.Admitted || calls.Load() != 0 {
				t.Fatal("native capacity inferred", result, err, calls.Load())
			}
			auth.Quota.Bounds["pmd_one"] = entity.ReservationBound{Protocol: protocol, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_native"}
			auth.LimitPolicies[limitAccount("team", identity.TeamID)] = limits.Policy{TokensMonth: limitNumber(1000), MoneyMonth: quotaTestString("10"), Currency: "USD"}
			_, err = teamNativeInvoke(svc, identity, protocol, teamNativeInput(protocol), "req_native_missing_price")
			if !errors.As(err, &gateway) || gateway.Code != "quota_price_unavailable" || calls.Load() != 0 {
				t.Fatal("native money treated as free", err, calls.Load())
			}
		})
	}
}
func TestTeamNativeDiscoveryIncludesIndependentlyReadyProtocols(t *testing.T) {
	svc, identity := teamGatewayFixture(t, "https://provider.example/v1")
	routes := svc.runtime.routes.Load()
	original := routes.Models["mdl_one"][0]
	auth := svc.runtime.auth.Load()
	for _, protocol := range []string{entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		candidate := original
		candidate.Route.Protocol = protocol
		candidate.Route.BindingID = "bnd_" + protocol
		candidate.Route.ProviderModelID = "pmd_" + protocol
		auth.ProviderModels[candidate.Route.ProviderModelID] = true
		auth.CredentialAccess["crd_one"][candidate.Route.ProviderModelID] = true
		routes.Models["mdl_one"] = append(routes.Models["mdl_one"], candidate)
	}
	models, err := svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || len(models[0].Protocols) != 4 || len(models[0].InputCapabilities) != 4 {
		t.Fatal("ready protocol discovery incomplete", models, err)
	}
	for _, capabilities := range models[0].InputCapabilities {
		if !reflect.DeepEqual(capabilities, []string{}) {
			t.Fatal("Team media advertised")
		}
	}
	svc.runtime.deniedProviderModels.Store("pmd_one", uint64(1))
	models, err = svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || len(models[0].Protocols) != 3 || strings.Contains(strings.Join(models[0].Protocols, ","), entity.ProtocolOpenAIChat) {
		t.Fatal("nonChat model requires Chat readiness", models, err)
	}
	svc.runtime.deniedProviderModels.Store("pmd_"+entity.ProtocolOpenAIResponses, uint64(2))
	models, err = svc.TeamGatewayModels(context.Background(), identity)
	if err != nil || len(models) != 1 || len(models[0].Protocols) != 2 {
		t.Fatal("one protocol revocation widened other availability", models, err)
	}
}

func TestTeamNativeMediaFailsBeforeCatalogStorageAdmissionAndDispatch(t *testing.T) {
	for _, tc := range []struct{ protocol, raw string }{
		{entity.ProtocolOpenAIResponses, `{"model":"public-model","input":[{"role":"user","content":[{"type":"input_image","image_url":"routex://attachments/obj_private"}]}]}`},
		{entity.ProtocolAnthropicMessages, `{"model":"public-model","messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"url","url":"routex://attachments/obj_private"}}]}]}],"max_tokens":10}`},
		{entity.ProtocolGeminiGenerateContent, `{"contents":[{"parts":[{"functionResponse":{"parts":[{"inlineData":{"mimeType":"image/png","data":"routex://attachments/obj_private"}}]}}]}]}`},
	} {
		t.Run(tc.protocol, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{}`) }))
			defer server.Close()
			svc, identity := teamNativeFixture(t, server.URL+"/v1", tc.protocol)
			svc.db = nil
			svc.secrets = nil
			result, err := teamNativeInvoke(svc, identity, tc.protocol, tc.raw, "req_team_no_media")
			var gateway *GatewayError
			if !errors.As(err, &gateway) || gateway.Code != "unsupported_input" || result == nil || result.Admitted || result.ModelID != "" || calls.Load() != 0 {
				t.Fatal("Team media reached catalog or storage", result, err, calls.Load())
			}
		})
	}
}
