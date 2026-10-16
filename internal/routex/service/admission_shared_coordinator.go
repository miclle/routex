package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
)

type sharedAttemptProgress struct {
	intent              sharedAttemptIntent
	forbidden           bool
	executed            bool
	checkpoint          *sharedAttemptCheckpoint
	checkpointConfirmed bool
}

// One coordinator belongs to one transient logical gateway executor. Its
// execution identity is never reconstructed from a persisted dispatch receipt.
// Durable retries keep this object and its original captured bodies; restart
// recovery needs separate fenced authority and never calls Dispatch.
type sharedAdmissionCoordinator struct {
	mu        sync.Mutex
	spool     sharedCallSpool
	authority sharedEconomicAuthority
	timeout   time.Duration
	intent    sharedAdmissionIntent
	receiptID string
	admitted  bool
	running   bool
	closed    bool
	attempts  []sharedAttemptProgress
	terminal  *sharedTerminalIntent
}

func newSharedAdmissionCoordinator(owner sharedAdmissionOwner, request sharedAdmissionRequest, spool sharedCallSpool, authority sharedEconomicAuthority, options sharedCoordinatorOptions) (*sharedAdmissionCoordinator, error) {
	if spool == nil || authority == nil || options.OperationTimeout <= 0 || options.OperationTimeout > 30*time.Second ||
		!sharedIdentity(owner.InstallationID) || !sharedIdentity(owner.SpoolID) || !sharedIdentity(owner.IncarnationID) ||
		owner.ModeRevision == 0 || owner.Fence == 0 || !sharedIdentity(request.RequestID) || !sharedAuthorizationValid(request.Authorization) ||
		request.CalendarRevision == "" || len(request.CalendarRevision) > 128 ||
		request.TimeZone == "" || len(request.Policies) == 0 || len(request.Policies) > 8 {
		return nil, errSharedAdmissionInvalid
	}
	initial, err := sharedCallPayload(request.RequestID, request.Fallback)
	if err != nil {
		return nil, err
	}
	if len(initial.Attempts) != 0 || !request.Authorization.matchesCall(initial) || !reflect.DeepEqual(request.Recovery, zeroQuotaSettlement(request.Bound)) {
		return nil, errSharedAdmissionInvalid
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	intent, err := sharedCaptured(sharedAdmissionIntent{sharedAdmissionRequest: request, Owner: owner, ExecutionID: hex.EncodeToString(nonce[:])})
	if err != nil {
		return nil, err
	}
	intent.Digest, err = sharedDigest(intent)
	if err != nil {
		return nil, err
	}
	if err := validateSharedSpoolAdmission(intent); err != nil {
		return nil, err
	}
	return &sharedAdmissionCoordinator{spool: spool, authority: authority, timeout: options.OperationTimeout, intent: intent}, nil
}

func (c *sharedAdmissionCoordinator) key() sharedAdmissionKey {
	return sharedAdmissionKey{Owner: c.intent.Owner, RequestID: c.intent.RequestID, ExecutionID: c.intent.ExecutionID, AdmissionDigest: c.intent.Digest}
}

// Admit first reserves a durable local payload slot. A confirmed shared receipt
// plus the local acknowledgement is required before preparing any attempt.
// Every explicit retry uses the original frozen request; no partial authority
// result or available local spool can substitute for a shared receipt.
func (c *sharedAdmissionCoordinator) Admit(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.running {
		return errSharedDispatchForbidden
	}
	if c.admitted {
		return nil
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	intent, err := sharedCaptured(c.intent)
	if err != nil {
		return err
	}
	intent.Digest = c.intent.Digest
	if err := c.spool.Reserve(op, intent); err != nil {
		return sharedUncertain(err)
	}
	local, err := c.spool.Read(op, c.key())
	if err != nil || !c.matchesLocal(local) {
		return sharedReadFailure(err)
	}
	// A reserved record from another execution cannot be adopted by this object.
	receipt, err := c.authority.Accept(op, intent)
	if err != nil {
		if definitiveSharedAdmissionRejection(err) {
			if rejectErr := c.spool.RejectAdmission(op, c.key()); rejectErr != nil {
				return sharedUncertain(rejectErr)
			}
			return fmt.Errorf("%w: %w", errSharedAdmissionRejected, err)
		}
		return sharedUncertain(err)
	}
	if !c.matchesReceipt(receipt) || receipt.Terminal != nil || len(receipt.Dispatches) != 0 {
		return errSharedAdmissionConflict
	}
	if err := c.spool.ConfirmAcceptance(op, c.key(), receipt.ReceiptID); err != nil {
		return sharedUncertain(err)
	}
	c.receiptID, c.admitted = receipt.ReceiptID, true
	return nil
}

var errSharedAdmissionRejected = errors.New("shared admission was definitively rejected")

func definitiveSharedAdmissionRejection(err error) bool {
	for _, rejected := range []error{
		eventqueue.ErrRateLimit,
		eventqueue.ErrConcurrency,
		eventqueue.ErrQuotaCoverage,
		eventqueue.ErrQuotaUnknown,
		eventqueue.ErrQuotaBound,
		eventqueue.ErrQuotaTokens,
		eventqueue.ErrQuotaMoney,
		eventqueue.ErrQuotaCurrency,
	} {
		if errors.Is(err, rejected) {
			return true
		}
	}
	return false
}

// Dispatch creates a conservative shared dispatch intent before invoking the
// supplied transport exactly once. Returning an error from MarkDispatch poisons
// this attempt even if read-back later proves a commit. A durable receipt is not
// a reusable execution permit. The callback uses the caller's original deadline,
// not the short storage-operation deadline, and must not spawn detached inference.
func (c *sharedAdmissionCoordinator) Dispatch(ctx context.Context, attempt sharedAttemptIntent, execute func(context.Context) error) error {
	c.mu.Lock()
	if !c.admitted || c.closed || execute == nil {
		c.mu.Unlock()
		return errSharedDispatchForbidden
	}
	if c.running {
		c.mu.Unlock()
		return errSharedExecutorBusy
	}
	progress, err := c.prepareAttempt(attempt)
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if progress.forbidden {
		c.mu.Unlock()
		return errSharedDispatchForbidden
	}
	op, cancel := context.WithTimeout(ctx, c.timeout)
	if err := op.Err(); err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	intent, err := sharedCaptured(progress.intent)
	if err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	intent.Digest = progress.intent.Digest
	if err := c.spool.PrepareAttempt(op, intent); err != nil {
		cancel()
		c.mu.Unlock()
		return sharedUncertain(err)
	}
	// Set this before contacting SQL: no error/read-back/retry can reissue the
	// transport after an uncertain shared dispatch commit.
	progress.forbidden = true
	receipt, err := c.authority.MarkDispatch(op, intent)
	if err != nil {
		cancel()
		c.mu.Unlock()
		return sharedUncertain(err)
	}
	if receipt == nil || receipt.Key != c.key() || receipt.Number != intent.Number || receipt.AttemptDigest != intent.Digest {
		cancel()
		c.mu.Unlock()
		return errSharedAdmissionConflict
	}
	if err := c.spool.ConfirmDispatch(op, *receipt); err != nil {
		cancel()
		c.mu.Unlock()
		return sharedUncertain(err)
	}
	if err := op.Err(); err != nil {
		cancel()
		c.mu.Unlock()
		return err
	}
	c.running = true
	progress.executed = true
	c.mu.Unlock()
	cancel()
	err = execute(ctx)
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	return err
}

func (c *sharedAdmissionCoordinator) prepareAttempt(attempt sharedAttemptIntent) (*sharedAttemptProgress, error) {
	if attempt.Number < 1 || attempt.Number > routeattempt.MaxAttempts || !sharedIdentity(attempt.AttemptID) ||
		!sharedIdentity(attempt.CredentialID) || !sharedIdentity(attempt.SnapshotID) || !sharedIdentity(attempt.ConnectionID) || !sharedIdentity(attempt.ProviderModelID) {
		return nil, errSharedAdmissionInvalid
	}
	parsed, err := sharedCallPayload(c.intent.RequestID, attempt.Fallback)
	if err != nil {
		return nil, err
	}
	if !c.matchesScope(parsed) || len(parsed.Attempts) != attempt.Number {
		return nil, errSharedAdmissionInvalid
	}
	last := parsed.Attempts[len(parsed.Attempts)-1]
	if last.ID != attempt.AttemptID || last.AttemptNumber != attempt.Number || last.CredentialID != attempt.CredentialID || last.SnapshotID != attempt.SnapshotID ||
		last.ConnectionID != attempt.ConnectionID || last.ProviderModelID != attempt.ProviderModelID {
		return nil, errSharedAdmissionConflict
	}
	if last.Status != "error" || last.WorkEvidence != "unknown" || last.NativeCompletionEvidence != "" && last.NativeCompletionEvidence != "unknown" {
		return nil, errSharedAdmissionInvalid
	}
	attempt.Key = c.key()
	attempt.Digest = ""
	intent, err := sharedCaptured(attempt)
	if err != nil {
		return nil, err
	}
	intent.Digest, err = sharedDigest(intent)
	if err != nil {
		return nil, err
	}
	if attempt.Number <= len(c.attempts) {
		progress := &c.attempts[attempt.Number-1]
		if progress.intent.Digest != intent.Digest {
			return nil, errSharedAdmissionConflict
		}
		return progress, nil
	}
	if attempt.Number != len(c.attempts)+1 || len(c.attempts) > 0 && !c.attempts[len(c.attempts)-1].checkpointConfirmed {
		return nil, errSharedDispatchForbidden
	}
	if len(c.attempts) > 0 {
		previous := c.attempts[len(c.attempts)-1]
		if !previous.executed {
			return nil, errSharedDispatchForbidden
		}
		checkpoint, err := sharedCallPayload(c.intent.RequestID, previous.checkpoint.Fallback)
		if err != nil || !callAttemptsProveNoWork(checkpoint.Attempts) || !sharedRetryEvidence(checkpoint.Attempts[len(checkpoint.Attempts)-1]) || !reflect.DeepEqual(parsed.Attempts[:len(parsed.Attempts)-1], checkpoint.Attempts) {
			return nil, errSharedDispatchForbidden
		}
	}
	c.attempts = append(c.attempts, sharedAttemptProgress{intent: intent})
	return &c.attempts[len(c.attempts)-1], nil
}

// Checkpoint freezes ordered attempt evidence locally before publishing its
// exact digest to SQL. A failed acknowledgement blocks the next attempt; retry
// can only repeat this checkpoint, never modify it or repeat transport.
func (c *sharedAdmissionCoordinator) Checkpoint(ctx context.Context, number int, fallback []byte, recovery eventqueue.QuotaSettlement) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return errSharedExecutorBusy
	}
	if c.closed || !c.admitted || number < 1 || number != len(c.attempts) {
		return errSharedDispatchForbidden
	}
	progress := &c.attempts[number-1]
	if !progress.forbidden {
		return errSharedDispatchForbidden
	}
	parsed, err := sharedCallPayload(c.intent.RequestID, fallback)
	if err != nil {
		return err
	}
	if !c.matchesScope(parsed) || !c.matchesAttemptFacts(parsed) || !reflect.DeepEqual(recovery, sharedCheckpointRecovery(parsed, c.intent.Bound)) {
		return errSharedAdmissionConflict
	}
	checkpoint, err := sharedCaptured(sharedAttemptCheckpoint{Key: c.key(), Number: number, AttemptDigest: progress.intent.Digest, Fallback: fallback, Recovery: recovery})
	if err != nil {
		return err
	}
	checkpoint.Digest, err = sharedDigest(checkpoint)
	if err != nil {
		return err
	}
	if progress.checkpoint != nil && progress.checkpoint.Digest != checkpoint.Digest {
		return errSharedAdmissionConflict
	}
	// Capture before a potentially uncertain local commit. A changed retry body
	// must not replace evidence from the original operation.
	if progress.checkpoint == nil {
		captured, err := sharedCaptured(checkpoint)
		if err != nil {
			return err
		}
		progress.checkpoint = &captured
	}
	op, cancel := c.detachedOperation(ctx)
	defer cancel()
	if err := c.spool.RecordAttempt(op, checkpoint); err != nil {
		return sharedUncertain(err)
	}
	checkpoint, err = sharedCaptured(*progress.checkpoint)
	if err != nil {
		return err
	}
	if err := c.authority.Checkpoint(op, checkpoint); err != nil {
		return sharedUncertain(err)
	}
	// This boolean is separate from captured checkpoint intent: an uncertain
	// checkpoint cannot unblock the next native fallback.
	progress.checkpointConfirmed = true
	return nil
}

// CloseExecutor is an explicit trusted caller assertion that the logical
// gateway request, its response body/stream and all local transport lifetimes
// have ended. It is not inferred from heartbeats, SQL fences or missing replies.
func (c *sharedAdmissionCoordinator) CloseExecutor() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return errSharedExecutorBusy
	}
	c.closed = true
	return nil
}

// Settle operates after executor closure, even if the client context is canceled.
// Its finite detached context preserves the original fact and independent known/
// unknown dimensions across explicit retries. Local readiness follows confirmed
// SQL settlement and can never precede it.
func (c *sharedAdmissionCoordinator) Settle(ctx context.Context, fact []byte, actual eventqueue.QuotaSettlement, upstream sharedUpstreamClosure) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.admitted || !c.closed || c.running {
		return errSharedDispatchForbidden
	}
	parsed, err := sharedCallPayload(c.intent.RequestID, fact)
	if err != nil {
		return err
	}
	if !c.matchesScope(parsed) || !c.matchesAttemptFacts(parsed) || !reflect.DeepEqual(actual, quotaCallSettlement(parsed)) || !sharedValidClosure(parsed, upstream) {
		return errSharedAdmissionInvalid
	}
	terminal, err := sharedCaptured(sharedTerminalIntent{Key: c.key(), LastAttempt: len(c.attempts), Fact: fact, Actual: actual, ExecutorClosed: true, Upstream: upstream})
	if err != nil {
		return err
	}
	terminal.Digest, err = sharedDigest(terminal)
	if err != nil {
		return err
	}
	if c.terminal != nil && c.terminal.Digest != terminal.Digest {
		return errSharedAdmissionConflict
	}
	if c.terminal == nil {
		captured, err := sharedCaptured(terminal)
		if err != nil {
			return err
		}
		c.terminal = &captured
	}
	op, cancel := c.detachedOperation(ctx)
	defer cancel()
	if err := c.spool.PrepareTerminal(op, terminal); err != nil {
		return sharedUncertain(err)
	}
	terminal, err = sharedCaptured(*c.terminal)
	if err != nil {
		return err
	}
	receipt, err := c.authority.Settle(op, terminal)
	if err != nil {
		return sharedUncertain(err)
	}
	if !c.matchesReceipt(receipt) || receipt.ReceiptID != c.receiptID || !sharedMatchesTerminal(receipt.Terminal, &terminal) {
		return errSharedAdmissionConflict
	}
	terminal, err = sharedCaptured(*c.terminal)
	if err != nil {
		return err
	}
	if err := c.spool.MakeReady(op, terminal, receipt.ReceiptID); err != nil {
		return sharedUncertain(err)
	}
	return nil
}

// Reconcile reads exact durable evidence without authorizing any transport.
// Even a matching committed dispatch cannot clear an attempt's forbidden flag.
// Terminal recovery can finish readiness only from this object's captured final
// body and a matching SQL terminal receipt; it cannot synthesize final evidence.
func (c *sharedAdmissionCoordinator) Reconcile(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return errSharedExecutorBusy
	}
	op, cancel := c.detachedOperation(ctx)
	defer cancel()
	local, err := c.spool.Read(op, c.key())
	if err != nil || !c.matchesLocal(local) {
		return sharedReadFailure(err)
	}
	receipt, err := c.authority.Read(op, c.key())
	if err != nil || !c.matchesReceipt(receipt) {
		return sharedReadFailure(err)
	}
	if c.receiptID != "" && c.receiptID != receipt.ReceiptID {
		return errSharedAdmissionConflict
	}
	if local.ReceiptID != "" && local.ReceiptID != receipt.ReceiptID {
		return errSharedAdmissionConflict
	}
	for _, dispatch := range receipt.Dispatches {
		if dispatch.Key != c.key() || dispatch.Number < 1 || dispatch.Number > len(c.attempts) ||
			c.attempts[dispatch.Number-1].intent.Digest != dispatch.AttemptDigest || dispatch.Number > len(local.Attempts) ||
			local.Attempts[dispatch.Number-1].Digest != dispatch.AttemptDigest || !sharedHasDigest(local.Attempts[dispatch.Number-1], dispatch.AttemptDigest) {
			return errSharedAdmissionConflict
		}
	}
	for _, checkpoint := range receipt.Checkpoints {
		if checkpoint.Key != c.key() || checkpoint.Number < 1 || checkpoint.Number > len(c.attempts) ||
			c.attempts[checkpoint.Number-1].checkpoint == nil || c.attempts[checkpoint.Number-1].checkpoint.Digest != checkpoint.Digest || !sharedHasDigest(checkpoint, checkpoint.Digest) ||
			checkpoint.Number > len(local.Checkpoints) || local.Checkpoints[checkpoint.Number-1].Digest != checkpoint.Digest || !sharedHasDigest(local.Checkpoints[checkpoint.Number-1], checkpoint.Digest) {
			return errSharedAdmissionConflict
		}
	}
	if receipt.Terminal != nil {
		if !c.closed || !sharedMatchesTerminal(receipt.Terminal, c.terminal) || !sharedMatchesTerminal(local.Terminal, c.terminal) {
			return errSharedAdmissionConflict
		}
	}
	if err := c.spool.ConfirmAcceptance(op, c.key(), receipt.ReceiptID); err != nil {
		return sharedUncertain(err)
	}
	c.receiptID, c.admitted = receipt.ReceiptID, true
	for _, dispatch := range receipt.Dispatches {
		c.attempts[dispatch.Number-1].forbidden = true
	}
	for _, checkpoint := range receipt.Checkpoints {
		c.attempts[checkpoint.Number-1].checkpointConfirmed = true
	}
	if receipt.Terminal != nil {
		terminal, err := sharedCaptured(*c.terminal)
		if err != nil {
			return err
		}
		return c.spool.MakeReady(op, terminal, receipt.ReceiptID)
	}
	return nil
}

func (c *sharedAdmissionCoordinator) matchesLocal(record *sharedSpoolRecord) bool {
	return record != nil && record.Intent.Owner == c.intent.Owner && record.Intent.ExecutionID == c.intent.ExecutionID &&
		record.Intent.RequestID == c.intent.RequestID && record.Intent.Digest == c.intent.Digest && sharedHasDigest(record.Intent, record.Intent.Digest) && bytes.Equal(record.Intent.Fallback, c.intent.Fallback)
}

func (c *sharedAdmissionCoordinator) matchesAttemptFacts(fact CallFact) bool {
	if len(fact.Attempts) != len(c.attempts) {
		return false
	}
	for index, progress := range c.attempts {
		attempt := fact.Attempts[index]
		if attempt.ID != progress.intent.AttemptID || attempt.AttemptNumber != progress.intent.Number ||
			attempt.CredentialID != progress.intent.CredentialID || attempt.SnapshotID != progress.intent.SnapshotID ||
			attempt.ConnectionID != progress.intent.ConnectionID || attempt.ProviderModelID != progress.intent.ProviderModelID {
			return false
		}
	}
	if len(c.attempts) > 1 {
		previous := c.attempts[len(c.attempts)-2].checkpoint
		if previous == nil {
			return false
		}
		parsed, err := sharedCallPayload(c.intent.RequestID, previous.Fallback)
		if err != nil || !reflect.DeepEqual(fact.Attempts[:len(fact.Attempts)-1], parsed.Attempts) {
			return false
		}
	}
	return true
}

func sharedCheckpointRecovery(fact CallFact, bound eventqueue.QuotaBound) eventqueue.QuotaSettlement {
	if len(fact.Attempts) > 0 && callAttemptsProveNoWork(fact.Attempts) {
		return zeroQuotaSettlement(bound)
	}
	return eventqueue.QuotaSettlement{}
}

func sharedRetryEvidence(attempt CallAttempt) bool {
	if attempt.OutputStarted || attempt.FinalUsageKnown {
		return false
	}
	switch routeattempt.Failure(attempt.FailureClass) {
	case routeattempt.CredentialRejected:
		return attempt.WorkEvidence == string(routeattempt.RejectedWithoutWork)
	case routeattempt.ConnectionFailure, routeattempt.RateLimited:
		return attempt.WorkEvidence == string(routeattempt.NotSent) || attempt.WorkEvidence == string(routeattempt.RejectedWithoutWork)
	default:
		return false
	}
}

func (c *sharedAdmissionCoordinator) matchesScope(fact CallFact) bool {
	initial, err := sharedCallPayload(c.intent.RequestID, c.intent.Fallback)
	return err == nil && initial.UserID == fact.UserID && initial.ProjectID == fact.ProjectID &&
		initial.TeamID == fact.TeamID && initial.TeamMembershipID == fact.TeamMembershipID && initial.KeyID == fact.KeyID &&
		initial.ModelID == fact.ModelID && initial.Protocol == fact.Protocol
}

func (c *sharedAdmissionCoordinator) matchesReceipt(receipt *sharedAuthorityReceipt) bool {
	return receipt != nil && receipt.Key == c.key() && sharedIdentity(receipt.ReceiptID)
}

func (c *sharedAdmissionCoordinator) detachedOperation(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), c.timeout)
}

func sharedCaptured[T any](value T) (T, error) {
	var result T
	raw, err := json.Marshal(value)
	if err != nil {
		return result, errSharedAdmissionInvalid
	}
	if json.Unmarshal(raw, &result) != nil {
		return result, errSharedAdmissionInvalid
	}
	return result, nil
}

func sharedDigest(value any) (string, error) {
	switch intent := value.(type) {
	case sharedAdmissionIntent:
		intent.Digest = ""
		value = intent
	case sharedAttemptIntent:
		intent.Digest = ""
		value = intent
	case sharedAttemptCheckpoint:
		intent.Digest = ""
		value = intent
	case sharedTerminalIntent:
		intent.Digest = ""
		value = intent
	default:
		return "", errSharedAdmissionInvalid
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", errSharedAdmissionInvalid
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func sharedHasDigest(value any, digest string) bool {
	actual, err := sharedDigest(value)
	return err == nil && digest != "" && actual == digest
}

func sharedMatchesTerminal(actual, expected *sharedTerminalIntent) bool {
	return actual != nil && expected != nil && actual.Key == expected.Key && actual.Digest == expected.Digest && sharedHasDigest(*actual, actual.Digest)
}

func sharedIdentity(value string) bool {
	return len(value) > 0 && len(value) <= 64 && safeCallID.MatchString(value)
}

func sharedCallPayload(requestID string, payload []byte) (CallFact, error) {
	var fact CallFact
	if len(payload) == 0 || len(payload) > callQueuePayloadLimit || json.Unmarshal(payload, &fact) != nil || fact.RequestID != requestID || validateCallFact(fact) != nil {
		return fact, errSharedAdmissionInvalid
	}
	return fact, nil
}

func sharedValidClosure(fact CallFact, closure sharedUpstreamClosure) bool {
	switch closure {
	case sharedUpstreamUnknown:
		return true
	case sharedUpstreamNoWork:
		return fact.NoWork || len(fact.Attempts) > 0 && callAttemptsProveNoWork(fact.Attempts)
	case sharedUpstreamTerminal:
		if len(fact.Attempts) == 0 {
			return false
		}
		for _, attempt := range fact.Attempts {
			if attempt.WorkEvidence == string(routeattempt.NotSent) || attempt.WorkEvidence == string(routeattempt.RejectedWithoutWork) {
				continue
			}
			if attempt.NativeCompletionEvidence == "" || attempt.NativeCompletionEvidence == "unknown" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sharedUncertain(err error) error {
	if errors.Is(err, errSharedAdmissionConflict) {
		return err
	}
	return fmt.Errorf("%w: %w", errSharedAdmissionUncertain, err)
}

func sharedReadFailure(err error) error {
	if err != nil {
		return sharedUncertain(err)
	}
	return errSharedAdmissionConflict
}
