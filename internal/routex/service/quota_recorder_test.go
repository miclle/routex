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

func TestQuotaNativeAdmissionPreparesScopesBeforeAndAfterActivation(t *testing.T) {
	var dispatches atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		dispatches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"chat.completion","choices":[{"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	svc, _, bearer := runtimeFixture(t, server.URL+"/v1")
	queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "quota.db"), 20, callQueuePayloadLimit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	svc.recorder = &callRecorder{queue: queue}
	// An internal legacy caller can prepare a format1 fact before activation.
	legacy := &GatewayResult{UserID: "usr_legacy", KeyID: "key_legacy", ModelID: "mdl_one", ModelName: "legacy"}
	if err := svc.AdmitGatewayCall("legacy_before", legacy); err != nil {
		t.Fatal(err)
	}
	if status, err := queue.QuotaStatus(); err != nil || status.Active {
		t.Fatal("legacy admission falsely asserted quota coverage", status, err)
	}
	for _, requestID := range []string{"native_first", "native_after_activation"} {
		result, err := svc.GatewayChat(context.Background(), bearer, []byte(`{"model":"public-model","messages":[{"role":"user","content":"hello"}]}`), requestID)
		if err != nil {
			t.Fatal(err)
		}
		_ = result.Response.Body.Close()
		if len(result.admissionQuota) != 2 || result.quotaTimeZone != "UTC" {
			t.Fatal("native dispatch skipped quota scope preparation")
		}
		receipt, err := queue.QuotaReceipt(requestID)
		if err != nil || len(receipt.Accounts) != 2 || receipt.Accounts[0].Account != "user_usr_one" || receipt.Accounts[1].Account != "key_key_one" {
			t.Fatal("native request lacks durable aggregate/child receipt", receipt, err)
		}
	}
	if status, err := queue.QuotaStatus(); err != nil || !status.Active {
		t.Fatal("native admission failed activation", status, err)
	}
	if err := svc.AdmitGatewayCall("legacy_after", legacy); !errors.Is(err, callQueueUnavailable) {
		t.Fatal("direct admission bypassed format2", err)
	}
	if _, err := queue.QuotaReceipt("legacy_after"); !errors.Is(err, eventqueue.ErrMissing) {
		t.Fatal(err)
	}
	if depth, err := queue.Depth(); err != nil || depth != 3 || dispatches.Load() != 2 {
		t.Fatal("rejected bypass left a reservation or dispatch", depth, dispatches.Load(), err)
	}
}
