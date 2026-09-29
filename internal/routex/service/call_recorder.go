package service

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
)

const callQueueCapacity = 4096
const callQueuePayloadLimit = 64 << 10

var callQueueUnavailable = &GatewayError{Status: 503, Code: "event_buffer_unavailable", Message: "Request recording is temporarily unavailable."}

type callRecorder struct {
	queue           *eventqueue.Queue
	cancel          context.CancelFunc
	done            chan struct{}
	flush           sync.Mutex
	quotaActivation sync.Mutex
	closeOnce       sync.Once
	closeErr        error
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
	if err := s.bindLimitJournal(ctx, queue); err != nil {
		_ = queue.Close()
		return errors.New("bind durable call buffer failed")
	}
	if err := s.validateQuotaJournal(ctx, queue); err != nil {
		_ = queue.Close()
		return errors.New("validate quota journal failed")
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
		if result != nil {
			for _, policy := range result.admissionQuota {
				if policy.Tokens5H != nil || policy.Tokens7D != nil || policy.TokensMonth != nil || policy.TPM != nil || policy.MoneyMonth != nil {
					return callQueueUnavailable
				}
			}
			for _, limit := range result.admissionLimits {
				if limit.RPM != nil || limit.Concurrency != nil {
					return callQueueUnavailable
				}
			}
		}
		return nil
	}
	if result == nil {
		return callQueueUnavailable
	}
	now := time.Now().UTC()
	payload, err := gatewayFallbackPayload(requestID, result, now)
	if err != nil {
		return callQueueUnavailable
	}
	if len(result.admissionQuota) == 0 {
		// Compatibility for callers preparing legacy format1 facts. Native gateway
		// dispatch always prepares quota scopes, including before first activation.
		// The journal atomically rejects this entry point once format2 is active.
		if err := s.recorder.queue.ReserveWithLimits(requestID, payload, result.admissionLimits, now); err != nil {
			return quotaGatewayError(err)
		}
		return nil
	}
	if result.quotaTimeZone == "" {
		return callQueueUnavailable
	}
	if err := s.ensureQuotaActive(result.quotaTimeZone, now); err != nil {
		return quotaGatewayError(err)
	}
	if err := s.recorder.queue.ReserveWithQuotaRecovery(requestID, payload, result.admissionQuota, result.quotaBound, zeroQuotaSettlement(result.quotaBound), now); err != nil {
		return quotaGatewayError(err)
	}
	return nil
}

// CheckpointGatewayCall fsyncs the latest ordered attempt evidence without
// changing the single logical admission, quota receipt, RPM, or concurrency.
func (s *Service) CheckpointGatewayCall(requestID string, result *GatewayResult) error {
	if s.recorder == nil {
		return nil
	}
	payload, err := gatewayFallbackPayload(requestID, result, time.Now().UTC())
	if err != nil {
		return callQueueUnavailable
	}
	if len(result.admissionQuota) != 0 {
		if err := s.recorder.queue.UpdatePendingQuota(requestID, payload, gatewayInterruptedSettlement(result)); err != nil {
			return callQueueUnavailable
		}
		return nil
	}
	if s.recorder.queue.UpdatePending(requestID, payload) != nil {
		return callQueueUnavailable
	}
	return nil
}

func gatewayInterruptedSettlement(result *GatewayResult) eventqueue.QuotaSettlement {
	settlement := eventqueue.QuotaSettlement{}
	if result == nil || result.AttemptID != "" || !callAttemptsProveNoWork(result.Attempts) {
		return settlement
	}
	if result.quotaBound.Tokens != nil {
		zero := int64(0)
		settlement.Tokens = &zero
	}
	if result.quotaBound.Money != nil {
		zero := "0"
		settlement.Money = &zero
		settlement.Currency = result.quotaBound.Currency
	}
	return settlement
}

func zeroQuotaSettlement(bound eventqueue.QuotaBound) eventqueue.QuotaSettlement {
	settlement := eventqueue.QuotaSettlement{}
	if bound.Tokens != nil {
		zero := int64(0)
		settlement.Tokens = &zero
	}
	if bound.Money != nil {
		zero := "0"
		settlement.Money = &zero
		settlement.Currency = bound.Currency
	}
	return settlement
}

func gatewayFallbackPayload(requestID string, result *GatewayResult, now time.Time) ([]byte, error) {
	imageInputs, pdfInputs := result.ImageInputs, result.PDFInputs
	attribution := result.ProviderAttribution()
	attempts := append([]CallAttempt(nil), result.Attempts...)
	if result.AttemptID != "" {
		attempts = append(attempts, CallAttempt{
			ID:              result.AttemptID,
			ProviderModelID: result.ProviderModelID,
			ConnectionID:    result.ConnectionID,
			AttemptNumber:   len(attempts) + 1,
			Status:          "error",
			FailureClass:    "permanent_failure",
			WorkEvidence:    "unknown",
			StartedAt:       result.AttemptStartedAt,
			CompletedAt:     now,
			ErrorCode:       "process_interrupted",
		})
	}
	fallback := CallFact{PriceBasis: clonePriceBasis(result.PriceBasis), PricingUnsupported: result.PricingUnsupported, PricingDimensions: result.PricingDimensions, NoWork: result.AttemptID == "" && callAttemptsProveNoWork(attempts), RequestID: requestID, SnapshotID: result.SnapshotID, UserID: result.UserID, ProjectID: result.ProjectID, KeyID: result.KeyID, ModelID: result.ModelID, ModelName: result.ModelName, ProviderID: attribution.ProviderID, ProviderName: attribution.ProviderName, ProviderModelID: attribution.ProviderModelID, ConnectionID: attribution.ConnectionID, ConnectionName: attribution.ConnectionName, UpstreamModelName: attribution.UpstreamModelName, RouteStopReason: result.RouteStopReason, Protocol: result.NativeProtocol(), Status: "error", Stream: result.Stream, StartedAt: now, CompletedAt: now, ImageInputs: &imageInputs, PDFInputs: &pdfInputs, ErrorCode: "process_interrupted", Attempts: attempts}
	if len(attempts) > 0 {
		fallback.StartedAt = attempts[0].StartedAt
	}
	finalizeCallPricing(&fallback)
	if err := validateCallFact(fallback); err != nil {
		return nil, err
	}
	return json.Marshal(fallback)
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
	status, statusErr := s.recorder.queue.QuotaStatus()
	if statusErr != nil {
		return callQueueUnavailable
	}
	if status.Active {
		_, err = s.recorder.queue.CompleteQuota(fact.RequestID, payload, quotaCallSettlement(fact), time.Now().UTC())
	} else {
		err = s.recorder.queue.Complete(fact.RequestID, payload)
	}
	if errors.Is(err, eventqueue.ErrMissing) {
		err = s.recorder.queue.Complete(fact.RequestID, payload)
	}
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
func (s *Service) FlushCallRecorder(ctx context.Context) (runErr error) {
	if s.recorder == nil {
		return nil
	}
	s.recorder.flush.Lock()
	defer s.recorder.flush.Unlock()
	entries, err := s.recorder.queue.Read(64)
	if err != nil {
		s.recordSystemJobOutcome(SystemJobCallRecordDelivery, systemJobFailed, "buffer_read_failed", 0, 0)
		return errors.New("read durable call buffer failed")
	}
	if len(entries) == 0 {
		return nil
	}
	jobID := s.beginSystemJob(SystemJobCallRecordDelivery, len(entries))
	completed, detailCode := 0, "delivered"
	defer func() {
		status := systemJobCompleted
		if runErr != nil {
			status = systemJobFailed
		}
		s.finishSystemJob(jobID, SystemJobCallRecordDelivery, status, detailCode, completed, len(entries))
	}()
	for _, entry := range entries {
		if ctx.Err() != nil {
			detailCode = "canceled"
			return ctx.Err()
		}
		var fact CallFact
		if err := json.Unmarshal(entry.Payload, &fact); err != nil {
			detailCode = "invalid_fact"
			return errors.New("invalid durable call fact")
		}
		if fact.RequestID != entry.ID {
			detailCode = "identity_mismatch"
			return errors.New("durable call fact identity mismatch")
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := s.RecordCall(deliveryCtx, fact)
		cancel()
		if err != nil {
			detailCode = "persistence_failed"
			return errors.New("deliver durable call fact failed")
		}
		if err := s.recorder.queue.Ack(entry.ID); err != nil {
			detailCode = "acknowledge_failed"
			return errors.New("acknowledge durable call fact failed")
		}
		completed++
	}
	return nil
}
