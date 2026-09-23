package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/miclle/routex/pkg/eventqueue"
)

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
