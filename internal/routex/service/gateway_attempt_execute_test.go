package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/upstream"
)

func TestGatewayAttemptExecutionRetriesOnlyProvenRejections(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		wantCalls   int32
		wantFailure string
		wantStop    string
	}{
		{name: "native authentication rejection", status: http.StatusUnauthorized, body: `{"error":{"code":"invalid_api_key"}}`, wantCalls: 2, wantFailure: "credential_rejected", wantStop: "succeeded"},
		{name: "native rate rejection", status: http.StatusTooManyRequests, body: `{"error":{"code":"rate_limit_exceeded"}}`, wantCalls: 2, wantFailure: "rate_limited", wantStop: "succeeded"},
		{name: "bare unauthorized", status: http.StatusUnauthorized, body: `{}`, wantCalls: 1, wantFailure: "permanent_failure", wantStop: "unsafe_to_replay"},
		{name: "ambiguous server failure", status: http.StatusServiceUnavailable, body: `{"error":{"code":"unavailable"}}`, wantCalls: 1, wantFailure: "permanent_failure", wantStop: "unsafe_to_replay"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				current := calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if current == 1 {
					w.WriteHeader(test.status)
					_, _ = io.WriteString(w, test.body)
					return
				}
				_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
			}))
			defer server.Close()

			svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
			addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
			result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_retry")
			if calls.Load() != test.wantCalls {
				t.Fatalf("upstream calls = %d, want %d", calls.Load(), test.wantCalls)
			}
			if result == nil || result.RouteStopReason != test.wantStop || len(result.Attempts) != 1 || result.Attempts[0].FailureClass != test.wantFailure {
				t.Fatalf("result = %+v, error = %v", result, err)
			}
			if test.wantCalls == 2 {
				if err != nil || result.Response == nil || result.AttemptID == "" || result.Attempts[0].AttemptNumber != 1 {
					t.Fatalf("retry result = %+v, error = %v", result, err)
				}
				if result.ConnectionID == result.Attempts[0].ConnectionID || result.ProviderName == "" || result.ConnectionName == "" || result.UpstreamModelName == "" {
					t.Fatalf("final route attribution did not follow failover: %+v", result)
				}
				if result.ConnectionID == "con_one" && (result.ProviderID != "prv_one" || result.ProviderName != "Provider One" || result.ConnectionName != "Primary" || result.UpstreamModelName != "provider-model") {
					t.Fatalf("primary route attribution mismatch: %+v", result)
				}
				if result.ConnectionID == "con_two" && (result.ProviderID != "prv_two" || result.ProviderName != "Provider Two" || result.ConnectionName != "Secondary" || result.UpstreamModelName != "provider-model-two") {
					t.Fatalf("secondary route attribution mismatch: %+v", result)
				}
				_ = result.Response.Body.Close()
				return
			}
			if err == nil || result.Response != nil || result.AttemptID != "" || result.Attempts[0].WorkEvidence != "unknown" {
				t.Fatalf("unsafe replay result = %+v, error = %v", result, err)
			}
		})
	}
}

type firstPreRequestTransport struct {
	calls      atomic.Int32
	preRequest http.RoundTripper
	success    http.RoundTripper
}

func (transport *firstPreRequestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if transport.calls.Add(1) == 1 {
		return transport.preRequest.RoundTrip(request)
	}
	return transport.success.RoundTrip(request)
}

func TestGatewayAttemptExecutionRetriesProvenPreRequestFailure(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer server.Close()

	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
	rejecting := upstream.NewClient(false)
	working := upstream.NewClient(true)
	defer rejecting.CloseIdleConnections()
	defer working.CloseIdleConnections()
	transport := &firstPreRequestTransport{preRequest: rejecting.Transport, success: working.Transport}
	routes := svc.runtime.routes.Load()
	for modelID, candidates := range routes.Models {
		for index := range candidates {
			candidates[index].Route.Client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
		}
		routes.Models[modelID] = candidates
	}

	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_connection")
	if err != nil || calls.Load() != 1 || result.Response == nil || result.RouteStopReason != "succeeded" || len(result.Attempts) != 1 {
		t.Fatalf("result = %+v, error = %v, upstream calls = %d", result, err, calls.Load())
	}
	defer func() { _ = result.Response.Body.Close() }()
	failed := result.Attempts[0]
	if failed.FailureClass != "connection_failure" || failed.WorkEvidence != "not_sent" || failed.EvidenceCode != "pre_request_connection" {
		t.Fatalf("failed attempt = %+v", failed)
	}
}

func TestGatewayAttemptCheckpointRecoversOneAdmissionWithOrderedEvidence(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer server.Close()

	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
	journal := filepath.Join(t.TempDir(), "attempts.db")
	queue, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_recovery")
	if err != nil || result.Response == nil {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	_ = result.Response.Body.Close()
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	recovered, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = recovered.Close() }()
	entries, err := recovered.Read(8)
	if err != nil || len(entries) != 1 || entries[0].ID != "req_attempt_recovery" {
		t.Fatalf("recovered entries = %+v, error = %v", entries, err)
	}
	var fallback CallFact
	if err := json.Unmarshal(entries[0].Payload, &fallback); err != nil {
		t.Fatal(err)
	}
	if len(fallback.Attempts) != 2 || fallback.Attempts[0].AttemptNumber != 1 || fallback.Attempts[0].FailureClass != "credential_rejected" || fallback.Attempts[1].AttemptNumber != 2 || fallback.Attempts[1].ErrorCode != "process_interrupted" {
		t.Fatalf("recovered fallback = %+v", fallback)
	}
}

func TestGatewayCancellationAfterAdmissionPersistsNoWorkEconomics(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()

	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	now := time.Now().UTC()
	data.Limits = []entity.ResourceLimit{{ScopeKind: "user", ScopeID: "usr_one", ETag: "policy_cancel", Tokens5H: limitNumber(500)}}
	data.Quota.Bounds = map[string]entity.ReservationBound{
		"pmd_one": {ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, MaxInputTokens: 100, MaxOutputTokens: 20, ETag: "bound_cancel"},
	}
	data.Quota.Created = map[string]time.Time{"user_usr_one": now.Add(-time.Minute), "key_key_one": now.Add(-time.Minute)}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_cancel", Models: routes, PublishedAt: now})

	journal := filepath.Join(t.TempDir(), "cancel.db")
	queue, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.EnableQuota("UTC", now.Add(-2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	svc.recorder = &callRecorder{queue: queue}
	ctx, cancel := context.WithCancel(context.Background())
	svc.afterGatewayAdmission = cancel
	started := time.Now().UTC()
	result, callErr := svc.GatewayChat(ctx, bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":10}`), "req_admitted_cancel")
	cancel()
	if callErr == nil || result == nil || calls.Load() != 0 || !result.Admitted || result.AttemptID != "" || len(result.Attempts) != 0 || result.RouteStopReason != "canceled" || !result.NoUpstreamWork() {
		t.Fatalf("admitted cancellation = %+v, calls = %d, error = %v", result, calls.Load(), callErr)
	}
	if attribution := result.ProviderAttribution(); attribution != (CallProviderAttribution{}) {
		t.Fatalf("unattempted cancellation gained provider attribution: %+v", attribution)
	}
	fact := CallFact{RequestID: "req_admitted_cancel", SnapshotID: result.SnapshotID, UserID: result.UserID, KeyID: result.KeyID, ModelID: result.ModelID, ModelName: result.ModelName, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, RouteStopReason: result.RouteStopReason, Protocol: result.NativeProtocol(), Status: "canceled", StartedAt: started, CompletedAt: time.Now().UTC(), NoWork: result.NoUpstreamWork(), PricingUnsupported: result.PricingUnsupported, PricingDimensions: result.PricingDimensions, PriceBasis: result.PriceBasis, ErrorCode: "canceled"}
	if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	receipt, err := queue.QuotaReceipt(fact.RequestID)
	if err != nil || receipt.State != "complete" || receipt.Actual.Tokens == nil || *receipt.Actual.Tokens != 0 || receipt.Overrun {
		t.Fatalf("zero-work receipt = %+v, error = %v", receipt, err)
	}
	entries, err := queue.Read(8)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %+v, error = %v", entries, err)
	}
	var persisted CallFact
	if err := json.Unmarshal(entries[0].Payload, &persisted); err != nil || !persisted.NoWork || persisted.Pricing == nil || persisted.Pricing.Status != "no_work" || !zeroCounter(persisted.InputTokens) || !zeroCounter(persisted.OutputTokens) || !zeroCounter(persisted.CacheReadTokens) || !zeroCounter(persisted.CacheWriteTokens) || persisted.ProviderID != "" || persisted.ProviderName != "" || persisted.ProviderModelID != "" || persisted.ConnectionID != "" || persisted.ConnectionName != "" || persisted.UpstreamModelName != "" {
		t.Fatalf("persisted no-work fact = %+v, error = %v", persisted, err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	usage, err := reopened.AccountQuotaUsage("user_usr_one", time.Now().UTC())
	if err != nil || usage.Active.TokensHeld != 0 || usage.FiveHours.TokensUsed != 0 || usage.FiveHours.TokensHeld != 0 || usage.FiveHours.TokensUnknown != 0 {
		t.Fatalf("reopened no-work usage = %+v, error = %v", usage, err)
	}
}

func TestGatewayAttemptFinalizesOneReceiptAfterRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"rate_limit_exceeded"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer server.Close()
	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "final.db"), 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_final")
	if err != nil || result.Response == nil {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	_ = result.Response.Body.Close()
	completed := time.Now().UTC()
	attempts := append([]CallAttempt(nil), result.Attempts...)
	attempts = append(attempts, CallAttempt{ID: result.AttemptID, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, AttemptNumber: 2, Status: "success", FailureClass: "success", WorkEvidence: "completed", StartedAt: result.AttemptStartedAt, CompletedAt: completed, HTTPStatus: http.StatusOK})
	fact := CallFact{RequestID: "req_attempt_final", SnapshotID: result.SnapshotID, UserID: result.UserID, KeyID: result.KeyID, ModelID: result.ModelID, ModelName: result.ModelName, ProviderID: result.ProviderID, ProviderName: result.ProviderName, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, ConnectionName: result.ConnectionName, UpstreamModelName: result.UpstreamModelName, RouteStopReason: "succeeded", Protocol: result.NativeProtocol(), Status: "success", StartedAt: attempts[0].StartedAt, CompletedAt: completed, Attempts: attempts}
	if err := svc.PersistGatewayCall(context.Background(), fact); err != nil {
		t.Fatal(err)
	}
	receipt, err := queue.QuotaReceipt(fact.RequestID)
	if err != nil || receipt.State != "complete" {
		t.Fatalf("receipt = %+v, error = %v", receipt, err)
	}
	entries, err := queue.Read(8)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %+v, error = %v", entries, err)
	}
	var persisted CallFact
	if err := json.Unmarshal(entries[0].Payload, &persisted); err != nil || len(persisted.Attempts) != 2 || persisted.ProviderID != result.ProviderID || persisted.ProviderName != result.ProviderName || persisted.ConnectionName != result.ConnectionName || persisted.UpstreamModelName != result.UpstreamModelName {
		t.Fatalf("persisted = %+v, error = %v", persisted, err)
	}
}

func TestGatewayRetryRechecksCallerAndAdmissionEvidence(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*runtimeAuthorization)
		wantStop string
	}{
		{
			name: "key revoked",
			mutate: func(auth *runtimeAuthorization) {
				delete(auth.KeysByID, "key_one")
			},
			wantStop: "no_candidates",
		},
		{
			name: "model grant removed",
			mutate: func(auth *runtimeAuthorization) {
				key := auth.KeysByID["key_one"]
				key.Models = nil
				auth.KeysByID["key_one"] = key
			},
			wantStop: "no_candidates",
		},
		{
			name: "limit changed before retry",
			mutate: func(auth *runtimeAuthorization) {
				policy := auth.LimitPolicies["user_usr_one"]
				rpm := int64(10)
				policy.RPM = &rpm
				auth.LimitPolicies["user_usr_one"] = policy
			},
			wantStop: "blocked",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			first := make(chan struct{})
			release := make(chan struct{})
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if calls.Add(1) == 1 {
					close(first)
					<-release
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key"}}`)
					return
				}
				_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
			}))
			defer server.Close()

			svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
			addSecondGatewayAttemptRoute(t, svc, data, server.URL+"/v1")
			type response struct {
				result *GatewayResult
				err    error
			}
			done := make(chan response, 1)
			go func() {
				result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_recheck")
				done <- response{result: result, err: err}
			}()
			<-first
			auth := *svc.runtime.auth.Load()
			auth.KeysByID = cloneRuntimeKeysByID(auth.KeysByID)
			auth.LimitPolicies = cloneRuntimeLimitPolicies(auth.LimitPolicies)
			test.mutate(&auth)
			svc.runtime.auth.Store(&auth)
			close(release)
			got := <-done
			if got.err == nil || got.result == nil || got.result.RouteStopReason != test.wantStop || calls.Load() != 1 || len(got.result.Attempts) != 1 {
				t.Fatalf("result = %+v, error = %v, calls = %d", got.result, got.err, calls.Load())
			}
		})
	}
}

func TestGatewayCredentialRejectionUsesNextPriorityOnSameTarget(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch request.Header.Get("Authorization") {
		case "Bearer test-upstream-secret":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_api_key"}}`)
		case "Bearer backup-upstream-secret":
			_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
		default:
			t.Errorf("unexpected credential %q", request.Header.Get("Authorization"))
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()

	svc, data, bearer := runtimeFixture(t, server.URL+"/v1")
	ciphertext, err := svc.secrets.Seal("crd_two", "backup-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	data.Credentials = append(data.Credentials, entity.ProviderCredential{ID: "crd_two", ConnectionID: "con_one", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified", Priority: 1})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_two", ProviderModelID: "pmd_one"})
	publishGatewayAttemptFixture(t, svc, data)
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_attempt_credential")
	if err != nil || result.Response == nil || calls.Load() != 2 || len(result.Attempts) != 1 || result.Attempts[0].ConnectionID != result.ConnectionID || result.CredentialID != "crd_two" {
		t.Fatalf("result = %+v, error = %v, calls = %d", result, err, calls.Load())
	}
	_ = result.Response.Body.Close()
	if svc.gatewayAttemptHealthy("con_one", "crd_one") || !svc.gatewayAttemptHealthy("con_one", "crd_two") {
		t.Fatal("credential cooldown was not scoped to the rejected credential")
	}
}

func cloneRuntimeKeysByID(source map[string]runtimeKey) map[string]runtimeKey {
	result := make(map[string]runtimeKey, len(source))
	for key, value := range source {
		value.Models = append([]string(nil), value.Models...)
		result[key] = value
	}
	return result
}

func cloneRuntimeLimitPolicies(source map[string]limits.Policy) map[string]limits.Policy {
	result := make(map[string]limits.Policy, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func addSecondGatewayAttemptRoute(t *testing.T, svc *Service, data *runtimeData, baseURL string) {
	t.Helper()
	ciphertext, err := svc.secrets.Seal("crd_two", "second-upstream-secret")
	if err != nil {
		t.Fatal(err)
	}
	data.Bindings[0].Weight = 50
	data.Providers = append(data.Providers, entity.Provider{ID: "prv_two", Name: "Provider Two"})
	data.Connections = append(data.Connections, entity.ProviderConnection{ID: "con_two", ProviderID: "prv_two", Name: "Secondary", BaseURL: baseURL, Protocol: entity.ProtocolOpenAIChat})
	data.ProviderModels = append(data.ProviderModels, entity.ProviderModel{ID: "pmd_two", ConnectionID: "con_two", UpstreamName: "provider-model-two"})
	data.Credentials = append(data.Credentials, entity.ProviderCredential{ID: "crd_two", ConnectionID: "con_two", Ciphertext: ciphertext, Enabled: true, VerificationStatus: "verified"})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: "crd_two", ProviderModelID: "pmd_two"})
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_two", ModelID: "mdl_one", ProviderModelID: "pmd_two", Weight: 50})
	publishGatewayAttemptFixture(t, svc, data)
}

func publishGatewayAttemptFixture(t *testing.T, svc *Service, data *runtimeData) {
	t.Helper()
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, now.Add(time.Minute)))
	routes, err := svc.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	svc.runtime.routes.Store(&runtimeRoutes{ID: "cfg_attempt_execution", Models: routes, PublishedAt: now})
}
