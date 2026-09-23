package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
)

const callQueueCapacity = 4096
const callQueuePayloadLimit = 64 << 10

var callQueueUnavailable = &GatewayError{Status: 503, Code: "event_buffer_unavailable", Message: "Request recording is temporarily unavailable."}

type callRecorder struct {
	queue     *eventqueue.Queue
	cancel    context.CancelFunc
	done      chan struct{}
	flush     sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

// StartCallRecorder replays fsynced facts independently of the gateway hot path.
// Pending admissions recovered by Open already contain an interruption fallback.
func (s *Service) StartCallRecorder(ctx context.Context, path string) error {
	if s.recorder != nil {
		return errors.New("call recorder is already started")
	}
	queue, err := eventqueue.Open(path, callQueueCapacity, callQueuePayloadLimit)
	if err != nil {
		return errors.New("open durable call buffer failed")
	}
	runCtx, cancel := context.WithCancel(ctx)
	recorder := &callRecorder{queue: queue, cancel: cancel, done: make(chan struct{})}
	s.recorder = recorder
	go func() {
		defer close(recorder.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		deferred := false
		for {
			if runCtx.Err() != nil {
				return
			}
			err := s.FlushCallRecorder(runCtx)
			if err != nil && runCtx.Err() == nil && !deferred {
				log.Print("call buffer delivery deferred")
			} else if err == nil && deferred {
				log.Print("call buffer delivery resumed")
			}
			deferred = err != nil
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return nil
}

func (s *Service) StopCallRecorder() error {
	if s.recorder == nil {
		return nil
	}
	r := s.recorder
	r.closeOnce.Do(func() { r.cancel(); <-r.done; r.closeErr = r.queue.Close() })
	return r.closeErr
}

// AdmitGatewayCall must run before the first upstream dispatch. Capacity or
// filesystem failures reject the request before consuming provider resources.
func (s *Service) AdmitGatewayCall(requestID string, result *GatewayResult) error {
	if s.recorder == nil {
		return nil
	}
	if result == nil {
		return callQueueUnavailable
	}
	now := time.Now().UTC()
	fallback := CallFact{PriceBasis: clonePriceBasis(result.PriceBasis), PricingUnsupported: result.PricingUnsupported, PricingDimensions: result.PricingDimensions, RequestID: requestID, SnapshotID: result.SnapshotID, UserID: result.UserID, ProjectID: result.ProjectID, KeyID: result.KeyID, ModelID: result.ModelID, ModelName: result.ModelName, ProviderModelID: result.ProviderModelID, ConnectionID: result.ConnectionID, Protocol: entity.ProtocolOpenAIChat, Status: "error", Stream: result.Stream, StartedAt: now, CompletedAt: now, ErrorCode: "process_interrupted"}
	finalizeCallPricing(&fallback)
	if err := validateCallFact(fallback); err != nil {
		return callQueueUnavailable
	}
	payload, err := json.Marshal(fallback)
	if err != nil {
		return callQueueUnavailable
	}
	if err := s.recorder.queue.Reserve(requestID, payload); err != nil {
		return callQueueUnavailable
	}
	return nil
}

func (s *Service) PersistGatewayCall(ctx context.Context, fact CallFact) error {
	finalizeCallPricing(&fact)
	if s.recorder == nil {
		return s.RecordCall(ctx, fact)
	}
	if err := validateCallFact(fact); err != nil {
		return err
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return apperrors.ErrInternal
	}
	err = s.recorder.queue.Complete(fact.RequestID, payload)
	if errors.Is(err, eventqueue.ErrMissing) {
		// Rejected requests may finish before the upstream admission boundary.
		// Their final fact can be queued directly without pretending a dispatch ran.
		if err = s.recorder.queue.Reserve(fact.RequestID, payload); err == nil {
			err = s.recorder.queue.Complete(fact.RequestID, payload)
		}
	}
	if err != nil {
		return callQueueUnavailable
	}
	return nil
}

// FlushCallRecorder acknowledges only after an idempotent primary transaction.
// A failed delivery remains durable and is retried by the next cycle or restart.
func (s *Service) FlushCallRecorder(ctx context.Context) error {
	if s.recorder == nil {
		return nil
	}
	s.recorder.flush.Lock()
	defer s.recorder.flush.Unlock()
	entries, err := s.recorder.queue.Read(64)
	if err != nil {
		return errors.New("read durable call buffer failed")
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var fact CallFact
		if err := json.Unmarshal(entry.Payload, &fact); err != nil {
			return errors.New("invalid durable call fact")
		}
		if fact.RequestID != entry.ID {
			return errors.New("durable call fact identity mismatch")
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := s.RecordCall(deliveryCtx, fact)
		cancel()
		if err != nil {
			return errors.New("deliver durable call fact failed")
		}
		if err := s.recorder.queue.Ack(entry.ID); err != nil {
			return errors.New("acknowledge durable call fact failed")
		}
	}
	return nil
}
