package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestParseGatewayChat(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"model":"m","messages":[]}`, `{"model":"m","messages":{}}`, `{"model":"m","messages":[{}],"stream":"yes"}`, `{"model":"m","messages":[{}],"stream":null}`, `{"model":"bad name","messages":[{}]}`} {
		if _, _, _, err := parseGatewayChat([]byte(raw)); err == nil {
			t.Errorf("invalid request accepted: %s", raw)
		}
	}
	payload, model, stream, err := parseGatewayChat([]byte(`{"model":"m","messages":[{"role":"user","content":"hello"}],"stream":true,"temperature":0.125,"tools":[{"type":"function"}]}`))
	if err != nil || model != "m" || !stream {
		t.Fatalf("valid request: %v", err)
	}
	if !json.Valid(payload["tools"]) || string(payload["temperature"]) != "0.125" {
		t.Fatal("native parameters changed")
	}
}

func TestGatewayWeightSelection(t *testing.T) {
	for _, weights := range [][]int{nil, {0}, {50}, {101}, {-1, 101}, {50, 51}} {
		if _, err := chooseGatewayRoute(weights); err == nil {
			t.Fatalf("invalid weights accepted: %v", weights)
		}
	}
	for range 100 {
		if selected, err := chooseGatewayRoute([]int{0, 100, 0}); err != nil || selected != 1 {
			t.Fatal("zero-weight candidate selected")
		}
	}
	seen := [2]bool{}
	for range 1000 {
		selected, err := chooseGatewayRoute([]int{50, 50})
		if err != nil {
			t.Fatal(err)
		}
		seen[selected] = true
	}
	if !seen[0] || !seen[1] {
		t.Fatal("weighted candidates were not both selected")
	}
}

func TestDirectGatewayCheckpointFailurePreventsDispatch(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer upstream.Close()
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "direct.db"), 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	result := directGatewayCheckpointResult()
	payload, err := gatewayFallbackPayload("req_direct_failed", result, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Reserve("req_direct_failed", payload); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	result.AttemptID, result.AttemptStartedAt = "att_direct_failed", time.Now().UTC()
	svc := &Service{recorder: &callRecorder{queue: queue}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := svc.dispatchCheckpointedGatewayRequest("req_direct_failed", result, upstream.Client(), request)
	if !errors.Is(err, callQueueUnavailable) || response != nil || calls.Load() != 0 || result.AttemptID != "" || !result.AttemptStartedAt.IsZero() || len(result.Attempts) != 0 {
		t.Fatalf("failed checkpoint dispatched or invented an attempt: result=%+v response=%v calls=%d error=%v", result, response, calls.Load(), err)
	}
	if result.ProviderAttribution() != (CallProviderAttribution{}) {
		t.Fatal("unattempted route became immutable provider attribution")
	}
}

func TestDirectGatewayCheckpointRecoversInterruptedAttempt(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, `{}`)
	}))
	defer upstream.Close()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	path := filepath.Join(t.TempDir(), "direct.db")
	queue, err := eventqueue.Open(path, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	result := directGatewayCheckpointResult()
	payload, err := gatewayFallbackPayload("req_direct_held", result, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Reserve("req_direct_held", payload); err != nil {
		t.Fatal(err)
	}
	result.AttemptID, result.AttemptStartedAt = "att_direct_held", time.Now().UTC()
	svc := &Service{recorder: &callRecorder{queue: queue}}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		response, err := svc.dispatchCheckpointedGatewayRequest("req_direct_held", result, upstream.Client(), request)
		if response != nil {
			_ = response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter controlled upstream")
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.Open(path, 8, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	entries, err := reopened.Read(8)
	if err != nil || len(entries) != 1 {
		t.Fatalf("direct interruption recovery = %+v, error = %v", entries, err)
	}
	var fact CallFact
	if err := json.Unmarshal(entries[0].Payload, &fact); err != nil {
		t.Fatal(err)
	}
	if len(fact.Attempts) != 1 || fact.Attempts[0].CredentialID != result.CredentialID || fact.Attempts[0].SnapshotID != "" || fact.Attempts[0].Status != "error" || fact.Attempts[0].WorkEvidence != "unknown" || fact.Attempts[0].ErrorCode != "process_interrupted" {
		t.Fatalf("direct interruption lost exact credential or invented snapshot/success: %+v", fact)
	}
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func directGatewayCheckpointResult() *GatewayResult {
	return &GatewayResult{UserID: "usr_direct", KeyID: "key_direct", ModelID: "mdl_direct", ModelName: "direct", CredentialID: "crd_direct", Protocol: entity.ProtocolOpenAIChat}
}
