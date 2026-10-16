package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"sync"

	"github.com/miclle/routex/pkg/routeattempt"
)

// A handle belongs to one actual result/executor, never a request-ID registry.
// No persisted dispatch can reconstruct this transient transport authority.
type sharedGatewayExecution struct {
	runtime            *sharedGatewayRuntime
	result             *GatewayResult
	coordinator        *sharedAdmissionCoordinator
	authority          *sharedEconomicSQLStore
	admissionAttempted bool
	admissionRejected  bool
	bodies             []*sharedGatewayBody
}

type sharedGatewayBody struct {
	io.ReadCloser
	once sync.Once
	err  error
}

func sharedGatewayRejectionMayBeRecorded(e *sharedGatewayExecution, result *GatewayResult) bool {
	return e != nil && result != nil && !result.Admitted && (!e.admissionAttempted || e.admissionRejected)
}

func (b *sharedGatewayBody) Close() error {
	b.once.Do(func() { b.err = b.ReadCloser.Close() })
	return b.err
}

func gatewayExecutionIP(ctx context.Context) netip.Addr {
	ip, _ := ctx.Value(gatewayIPKey{}).(netip.Addr)
	return ip
}
func (e *sharedGatewayExecution) admit(ctx context.Context) error {
	if e == nil || e.runtime == nil || e.coordinator == nil || !e.runtime.current(e.runtime.service) {
		return errSharedDispatchForbidden
	}
	e.admissionAttempted = true
	if err := e.coordinator.Admit(ctx); err != nil {
		if errors.Is(err, errSharedAdmissionRejected) {
			e.admissionRejected = true
		}
		return quotaGatewayError(err)
	}
	return nil
}
func (s *Service) preflightGatewayExecution(ctx context.Context, requestID string, result *GatewayResult) error {
	if s.sharedGateway == nil {
		return s.preflightPreparedGatewayAttempt(ctx, requestID, result)
	}
	if result == nil || result.sharedExecution == nil || result.sharedExecution.result != result {
		return callQueueUnavailable
	}
	e := result.sharedExecution
	if e.runtime == nil || e.coordinator == nil || e.authority == nil || !e.runtime.current(s) || e.coordinator.intent.RequestID != requestID {
		return callQueueUnavailable
	}
	if err := e.authority.preflightSharedGatewaySQL(ctx, e.coordinator.intent, e.runtime.timeout); err != nil {
		return quotaGatewayError(err)
	}
	return nil
}

func (e *sharedGatewayExecution) dispatch(ctx context.Context, result *GatewayResult, attempt routeattempt.Attempt, prepared *preparedGatewayAttempt) (*http.Response, error) {
	if e == nil || e.runtime == nil || e.authority == nil || e.authority.live == nil || e.result != result || e.coordinator == nil || !e.runtime.current(e.runtime.service) {
		return nil, errSharedDispatchForbidden
	}
	for _, body := range e.bodies {
		if err := body.Close(); err != nil {
			return nil, errSharedExecutorBusy
		}
	}
	fallback, err := gatewayFallbackPayload(e.coordinator.intent.RequestID, result, result.AttemptStartedAt)
	if err != nil {
		return nil, err
	}
	intent := sharedAttemptIntent{Number: attempt.Number, AttemptID: result.AttemptID, CredentialID: result.CredentialID, SnapshotID: result.SnapshotID,
		ConnectionID: result.ConnectionID, ProviderModelID: result.ProviderModelID, Fallback: fallback}
	var response *http.Response
	err = e.coordinator.Dispatch(ctx, intent, func(_ context.Context) error {
		// SQL acknowledgement does not replace the immediate local dispatch guard.
		svc := e.runtime.service
		if !e.runtime.current(svc) {
			return errSharedDispatchForbidden
		}
		release, admitted := svc.pinConnectionDispatch(prepared.route)
		if !admitted {
			return runtimeUnavailable
		}
		defer release()
		if err := svc.reauthorizePreparedGatewayAttempt(ctx, e.authority.live.plan, result, attempt); err != nil {
			return err
		}
		if !prepared.holder.admitUse() {
			return runtimeUnavailable
		}
		release()
		var transportErr error
		response, transportErr = prepared.client.Do(prepared.request)
		return transportErr
	})
	return response, err
}

func (s *Service) checkpointGatewayExecution(ctx context.Context, requestID string, result *GatewayResult) error {
	if result == nil {
		return callQueueUnavailable
	}
	if result.sharedExecution == nil {
		if s.sharedGateway != nil {
			return callQueueUnavailable
		}
		return s.checkpointGatewayCall(ctx, requestID, result)
	}
	e := result.sharedExecution
	if e.runtime == nil || e.coordinator == nil || e.result != result || e.runtime.service != s || e.coordinator.intent.RequestID != requestID || len(result.Attempts) == 0 {
		return callQueueUnavailable
	}
	fallback, err := gatewayFallbackPayload(requestID, result, result.Attempts[len(result.Attempts)-1].CompletedAt)
	if err != nil {
		return callQueueUnavailable
	}
	fact, err := sharedCallPayload(requestID, fallback)
	if err != nil {
		return callQueueUnavailable
	}
	return e.coordinator.Checkpoint(ctx, len(result.Attempts), fallback, sharedCheckpointRecovery(fact, e.coordinator.intent.Bound))
}

// FinalizeGatewayCall is the common four-protocol completion seam. The exact
// live result must accompany the sanitized final fact. Close the retained body
// before asserting logical executor closure, including cancellation/error paths;
// HTTP headers, heartbeat expiry and native terminal evidence are not closure.
// Local recording keeps its existing contract and asynchronous delivery path.
func (s *Service) FinalizeGatewayCall(ctx context.Context, result *GatewayResult, fact CallFact) error {
	if result == nil || result.sharedExecution == nil {
		if s.sharedGateway != nil {
			// Before successful shared admission, rejection facts have no economic
			// receipt and may be recorded directly; never recreate a local ledger.
			if result == nil || result.Admitted {
				return callQueueUnavailable
			}
			finalizeCallPricing(&fact)
			return s.RecordCall(ctx, fact)
		}
		return s.PersistGatewayCall(ctx, fact)
	}
	e := result.sharedExecution
	if e.result != result || e.runtime == nil || e.runtime.service != s || e.coordinator == nil || fact.RequestID != e.coordinator.intent.RequestID {
		return callQueueUnavailable
	}
	for _, body := range e.bodies {
		if err := body.Close(); err != nil {
			return callQueueUnavailable
		}
	}
	if sharedGatewayRejectionMayBeRecorded(e, result) {
		finalizeCallPricing(&fact)
		return s.RecordCall(ctx, fact)
	}
	if !result.Admitted {
		return callQueueUnavailable
	}
	finalizeCallPricing(&fact)
	if err := validateCallFact(fact); err != nil {
		return err
	}
	payload, err := json.Marshal(fact)
	if err != nil {
		return callQueueUnavailable
	}
	if err := e.coordinator.CloseExecutor(); err != nil {
		return err
	}
	closure := sharedUpstreamUnknown
	if sharedValidClosure(fact, sharedUpstreamNoWork) {
		closure = sharedUpstreamNoWork
	} else if sharedValidClosure(fact, sharedUpstreamTerminal) {
		closure = sharedUpstreamTerminal
	}
	return e.coordinator.Settle(ctx, payload, quotaCallSettlement(fact), closure)
}
