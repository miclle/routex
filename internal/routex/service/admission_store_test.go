package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

func TestAdmissionAuthorityIsExplicitAndNeverInferredFromSpool(t *testing.T) {
	if newLocalAdmissionStore(nil) != nil {
		t.Fatal("missing journal became a configured authority")
	}
	journal, _ := admissionTestJournal(t)
	svc := &Service{recorder: &callRecorder{queue: journal}}
	result := &GatewayResult{UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model"}
	if err := svc.AdmitGatewayCall("req_missing", result); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("admission without authority = %v", err)
	}
	if err := svc.CheckpointGatewayCall("req_missing", result); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("checkpoint without authority = %v", err)
	}
	if err := svc.PersistGatewayCall(context.Background(), CallFact{}); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("settlement without authority = %v", err)
	}
	if depth, err := journal.Depth(); err != nil || depth != 0 {
		t.Fatalf("missing authority wrote a spool fact: depth=%d error=%v", depth, err)
	}
}

type unavailableAdmissionStore struct {
	admissionStore
}

func (*unavailableAdmissionStore) QuotaStatus(context.Context) (*eventqueue.QuotaStatus, error) {
	return nil, eventqueue.ErrFull
}

func (*unavailableAdmissionStore) ReserveWithLimits(context.Context, string, []byte, []eventqueue.Limit, time.Time) error {
	return eventqueue.ErrFull
}

func (*unavailableAdmissionStore) UpdatePending(context.Context, string, []byte) error {
	return eventqueue.ErrFull
}

func TestAdmissionAuthorityFailureCannotFallBackToAvailableSpool(t *testing.T) {
	journal, _ := admissionTestJournal(t)
	svc := &Service{admission: &unavailableAdmissionStore{}, recorder: &callRecorder{queue: journal}}
	result := &GatewayResult{UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model"}
	if err := svc.AdmitGatewayCall("req_unavailable", result); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("unavailable authority admission = %v", err)
	}
	if err := svc.CheckpointGatewayCall("req_unavailable", result); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("unavailable authority checkpoint = %v", err)
	}
	now := time.Now().UTC()
	fact := CallFact{RequestID: "req_unavailable", UserID: "usr_one", KeyID: "key_one", ModelID: "mdl_one", ModelName: "public-model", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now, ErrorCode: "process_interrupted"}
	if err := svc.PersistGatewayCall(context.Background(), fact); !errors.Is(err, callQueueUnavailable) {
		t.Fatalf("unavailable authority settlement = %v", err)
	}
	if depth, err := journal.Depth(); err != nil || depth != 0 {
		t.Fatalf("failed authority wrote a spool fact: depth=%d error=%v", depth, err)
	}
}
