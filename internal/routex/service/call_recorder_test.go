package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestCallJournalLegacyPayloadKeepsProviderAttributionUnknown(t *testing.T) {
	now := time.Now().UTC()
	legacy := CallFact{RequestID: "req_legacy_journal", UserID: "usr_legacy", KeyID: "key_legacy", ModelID: "mdl_legacy", ModelName: "legacy", ProviderModelID: "pmd_legacy", ConnectionID: "con_legacy", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now, ErrorCode: "process_interrupted"}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ProviderID", "ProviderName", "ConnectionName", "UpstreamModelName"} {
		delete(payload, field)
	}
	raw, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var recovered CallFact
	if err := json.Unmarshal(raw, &recovered); err != nil {
		t.Fatal(err)
	}
	if err := validateCallFact(recovered); err != nil {
		t.Fatalf("legacy call journal fact rejected: %v", err)
	}
	if recovered.ProviderID != "" || recovered.ProviderName != "" || recovered.ConnectionName != "" || recovered.UpstreamModelName != "" {
		t.Fatalf("legacy call journal gained provider attribution: %+v", recovered)
	}
}

func TestCallJournalLegacyAttemptsKeepCredentialAndSnapshotUnknown(t *testing.T) {
	legacy := []byte(`{"RequestID":"req_legacy_attempt","SnapshotID":"cfg_parent_only","UserID":"usr_legacy","KeyID":"key_legacy","ModelID":"mdl_legacy","ModelName":"legacy","Protocol":"openai_chat","Status":"error","StartedAt":"2026-01-01T00:00:00Z","CompletedAt":"2026-01-01T00:00:01Z","Attempts":[{"ID":"att_legacy","ConnectionID":"con_legacy","ProviderModelID":"pmd_legacy","AttemptNumber":1,"Status":"error","FailureClass":"permanent_failure","WorkEvidence":"unknown","StartedAt":"2026-01-01T00:00:00Z","CompletedAt":"2026-01-01T00:00:01Z"}]}`)
	journal := filepath.Join(t.TempDir(), "legacy-attempt.db")
	queue, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Reserve("req_legacy_attempt", legacy); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.Open(journal, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	entries, err := reopened.Read(8)
	if err != nil || len(entries) != 1 {
		t.Fatalf("legacy journal recovery = %+v, error = %v", entries, err)
	}
	var recovered CallFact
	if err := json.Unmarshal(entries[0].Payload, &recovered); err != nil {
		t.Fatal(err)
	}
	if err := validateCallFact(recovered); err != nil {
		t.Fatalf("legacy attempt rejected: %v", err)
	}
	if len(recovered.Attempts) != 1 || recovered.Attempts[0].CredentialID != "" || recovered.Attempts[0].SnapshotID != "" || recovered.Attempts[0].Status != "error" {
		t.Fatalf("legacy attempt gained inferred credential/configuration or success: %+v", recovered.Attempts)
	}
}

func TestGatewayFallbackOmitsPreparedButUnattemptedRoute(t *testing.T) {
	now := time.Now().UTC()
	result := &GatewayResult{
		UserID: "usr_prepared", KeyID: "key_prepared", ModelID: "mdl_prepared", ModelName: "prepared",
		ProviderID: "prv_prepared", ProviderName: "Prepared Provider", ProviderModelID: "pmd_prepared",
		ConnectionID: "con_prepared", ConnectionName: "Prepared Connection", UpstreamModelName: "prepared-model",
		CredentialID: "crd_prepared", SnapshotID: "cfg_prepared",
		Protocol: entity.ProtocolOpenAIChat,
	}
	raw, err := gatewayFallbackPayload("req_prepared", result, now)
	if err != nil {
		t.Fatal(err)
	}
	var fallback CallFact
	if err := json.Unmarshal(raw, &fallback); err != nil {
		t.Fatal(err)
	}
	if len(fallback.Attempts) != 0 {
		t.Fatalf("unattempted route gained credential/configuration evidence: %+v", fallback.Attempts)
	}
	if fallback.ProviderID != "" || fallback.ProviderName != "" || fallback.ProviderModelID != "" || fallback.ConnectionID != "" || fallback.ConnectionName != "" || fallback.UpstreamModelName != "" {
		t.Fatalf("prepared route became immutable attribution: %+v", fallback)
	}
}

func TestGatewayFallbackPreservesEachAttemptAttribution(t *testing.T) {
	now := time.Now().UTC()
	previous := CallAttempt{
		ID: "att_previous", CredentialID: "crd_previous", SnapshotID: "cfg_previous",
		AttemptNumber: 1, Status: "error", FailureClass: "credential_rejected", WorkEvidence: "rejected_without_work",
		StartedAt: now.Add(-2 * time.Second), CompletedAt: now.Add(-time.Second),
	}
	result := &GatewayResult{
		UserID: "usr_active", KeyID: "key_active", ModelID: "mdl_active", ModelName: "active",
		CredentialID: "crd_active", SnapshotID: "cfg_active", AttemptID: "att_active",
		AttemptStartedAt: now.Add(-time.Second), Attempts: []CallAttempt{previous}, Protocol: entity.ProtocolOpenAIChat,
	}
	raw, err := gatewayFallbackPayload("req_active", result, now)
	if err != nil {
		t.Fatal(err)
	}
	var fallback CallFact
	if err := json.Unmarshal(raw, &fallback); err != nil {
		t.Fatal(err)
	}
	if len(fallback.Attempts) != 2 || fallback.Attempts[0].CredentialID != previous.CredentialID || fallback.Attempts[0].SnapshotID != previous.SnapshotID {
		t.Fatalf("current route overwrote previous attempt attribution: %+v", fallback.Attempts)
	}
	active := fallback.Attempts[1]
	if active.CredentialID != result.CredentialID || active.SnapshotID != result.SnapshotID || active.Status != "error" || active.WorkEvidence != "unknown" || active.ErrorCode != "process_interrupted" {
		t.Fatalf("active interruption lost exact attribution or became success: %+v", active)
	}
	if len(result.Attempts) != 1 || result.Attempts[0] != previous {
		t.Fatal("building fallback mutated previous immutable attempt")
	}
}

func TestGatewayRejectsDispatchWhenJournalUnavailable(t *testing.T) {
	for _, failure := range []string{"full", "closed"} {
		t.Run(failure, func(t *testing.T) {
			var dispatched atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				dispatched.Add(1)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
			defer svc.upstream.CloseIdleConnections()
			queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "journal.db"), 1, callQueuePayloadLimit)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = queue.Close() }()
			svc.recorder = &callRecorder{queue: queue}
			if failure == "closed" {
				if err := queue.Close(); err != nil {
					t.Fatal(err)
				}
			} else if err := queue.Reserve("req_existing", []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_rejected")
			if !errors.Is(err, callQueueUnavailable) || result == nil || result.AttemptID != "" || dispatched.Load() != 0 {
				t.Fatal("unavailable journal consumed upstream resources")
			}
		})
	}
}

func TestGatewayReservesJournalBeforeUpstreamDispatch(t *testing.T) {
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "journal.db"), 1, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		depth, err := queue.Depth()
		if err != nil || depth != 1 {
			t.Error("request reached upstream before durable reservation")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"provider-model","choices":[]}`)
	}))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	defer svc.upstream.CloseIdleConnections()
	svc.recorder = &callRecorder{queue: queue}
	result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), "req_admitted")
	if err != nil {
		t.Fatal(err)
	}
	_ = result.Response.Body.Close()
	if result.AttemptID == "" {
		t.Fatal("admitted request has no attempt identity")
	}
}
