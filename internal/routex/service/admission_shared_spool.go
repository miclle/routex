package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/routeattempt"
)

// This adapter directly consumes the coordinator contract. It does not open a
// local economic Queue, register an owner, select a runtime mode or settle SQL.
// Its caller owns the explicitly opened shared spool's file lifetime.
type durableSharedCallSpool struct {
	journal *eventqueue.SharedSpool
}

var _ sharedCallSpool = (*durableSharedCallSpool)(nil)

func newDurableSharedCallSpool(journal *eventqueue.SharedSpool) (*durableSharedCallSpool, error) {
	if journal == nil {
		return nil, errSharedAdmissionInvalid
	}
	return &durableSharedCallSpool{journal: journal}, nil
}

func (s *durableSharedCallSpool) Reserve(ctx context.Context, intent sharedAdmissionIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateSharedSpoolAdmission(intent); err != nil {
		return err
	}
	body, err := sharedSpoolBody(intent, intent.Digest)
	if err != nil {
		return err
	}
	return sharedSpoolError(s.journal.Reserve(ctx, sharedJournalKey(sharedIntentKey(intent)), body))
}

func (s *durableSharedCallSpool) Read(ctx context.Context, key sharedAdmissionKey) (*sharedSpoolRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := s.journal.ReadRecord(ctx, sharedJournalKey(key))
	if err != nil {
		return nil, sharedSpoolError(err)
	}
	var intent sharedAdmissionIntent
	if !sharedDecodeExact(record.Intent.Data, &intent) || intent.Digest != record.Intent.Digest || !sharedHasDigest(intent, intent.Digest) || sharedIntentKey(intent) != key {
		return nil, errSharedAdmissionConflict
	}
	if err := validateSharedSpoolAdmission(intent); err != nil {
		return nil, err
	}
	local := &sharedSpoolRecord{Intent: intent, ReceiptID: record.ReceiptID, Ready: record.Ready != nil}
	for _, stored := range record.Attempts {
		var attempt sharedAttemptIntent
		if !sharedDecodeExact(stored.Body.Data, &attempt) || attempt.Digest != stored.Body.Digest || attempt.Number != stored.Number || !sharedHasDigest(attempt, attempt.Digest) || attempt.Key != key {
			return nil, errSharedAdmissionConflict
		}
		if err := validateSharedSpoolAttempt(local, attempt); err != nil {
			return nil, err
		}
		local.Attempts = append(local.Attempts, attempt)
		// Previous checkpoint evidence is decoded below. Full prefix checks are
		// deferred until all stored evidence is available.
	}
	for _, stored := range record.Dispatches {
		local.Dispatches = append(local.Dispatches, sharedDispatchReceipt{Key: key, Number: stored.Number, AttemptDigest: stored.AttemptDigest})
	}
	for _, stored := range record.Checkpoints {
		var checkpoint sharedAttemptCheckpoint
		if !sharedDecodeExact(stored.Body.Data, &checkpoint) || checkpoint.Digest != stored.Body.Digest || checkpoint.Number != stored.Number || checkpoint.AttemptDigest != stored.AttemptDigest || !sharedHasDigest(checkpoint, checkpoint.Digest) || checkpoint.Key != key {
			return nil, errSharedAdmissionConflict
		}
		if err := validateSharedSpoolCheckpoint(local, checkpoint); err != nil {
			return nil, err
		}
		local.Checkpoints = append(local.Checkpoints, checkpoint)
	}
	for _, attempt := range local.Attempts {
		if err := validateSharedSpoolPrefix(local, attempt); err != nil {
			return nil, err
		}
	}
	if record.Terminal != nil {
		var terminal sharedTerminalIntent
		if !sharedDecodeExact(record.Terminal.Body.Data, &terminal) || terminal.Digest != record.Terminal.Body.Digest || terminal.LastAttempt != record.Terminal.LastAttempt {
			return nil, errSharedAdmissionConflict
		}
		if err := validateSharedSpoolTerminal(local, terminal); err != nil {
			return nil, err
		}
		local.Terminal = &terminal
	}
	return local, nil
}

func (s *durableSharedCallSpool) ConfirmAcceptance(ctx context.Context, key sharedAdmissionKey, receiptID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !sharedIdentity(receiptID) {
		return errSharedAdmissionInvalid
	}
	return sharedSpoolError(s.journal.ConfirmAcceptance(ctx, sharedJournalKey(key), receiptID))
}

func (s *durableSharedCallSpool) RejectAdmission(ctx context.Context, key sharedAdmissionKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return sharedSpoolError(s.journal.RejectAdmission(ctx, sharedJournalKey(key)))
}

func (s *durableSharedCallSpool) PrepareAttempt(ctx context.Context, attempt sharedAttemptIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := s.Read(ctx, attempt.Key)
	if err != nil {
		return err
	}
	if err := validateSharedSpoolAttempt(local, attempt); err != nil {
		return err
	}
	if err := validateSharedSpoolPrefix(local, attempt); err != nil {
		return err
	}
	body, err := sharedSpoolBody(attempt, attempt.Digest)
	if err != nil {
		return err
	}
	return sharedSpoolError(s.journal.PrepareAttempt(ctx, sharedJournalKey(attempt.Key), eventqueue.SharedSpoolAttempt{Number: attempt.Number, Body: body}))
}

func (s *durableSharedCallSpool) ConfirmDispatch(ctx context.Context, receipt sharedDispatchReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := s.Read(ctx, receipt.Key)
	if err != nil {
		return err
	}
	if receipt.Number < 1 || receipt.Number > len(local.Attempts) || local.Attempts[receipt.Number-1].Digest != receipt.AttemptDigest {
		return errSharedAdmissionConflict
	}
	return sharedSpoolError(s.journal.ConfirmDispatch(ctx, eventqueue.SharedSpoolDispatch{Key: sharedJournalKey(receipt.Key), Number: receipt.Number, AttemptDigest: receipt.AttemptDigest}))
}

func (s *durableSharedCallSpool) RecordAttempt(ctx context.Context, checkpoint sharedAttemptCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := s.Read(ctx, checkpoint.Key)
	if err != nil {
		return err
	}
	if err := validateSharedSpoolCheckpoint(local, checkpoint); err != nil {
		return err
	}
	body, err := sharedSpoolBody(checkpoint, checkpoint.Digest)
	if err != nil {
		return err
	}
	return sharedSpoolError(s.journal.RecordAttempt(ctx, sharedJournalKey(checkpoint.Key), eventqueue.SharedSpoolCheckpoint{Number: checkpoint.Number, AttemptDigest: checkpoint.AttemptDigest, Body: body}))
}

func (s *durableSharedCallSpool) PrepareTerminal(ctx context.Context, terminal sharedTerminalIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := s.Read(ctx, terminal.Key)
	if err != nil {
		return err
	}
	if err := validateSharedSpoolTerminal(local, terminal); err != nil {
		return err
	}
	body, err := sharedSpoolBody(terminal, terminal.Digest)
	if err != nil {
		return err
	}
	return sharedSpoolError(s.journal.PrepareTerminal(ctx, sharedJournalKey(terminal.Key), eventqueue.SharedSpoolTerminal{LastAttempt: terminal.LastAttempt, Body: body}))
}

// The coordinator calls this only after verifying the exact shared authority
// terminal receipt. Receipt identity/digest are bound to that original local
// terminal intent here; the adapter does not pretend to independently query SQL.
func (s *durableSharedCallSpool) MakeReady(ctx context.Context, terminal sharedTerminalIntent, receiptID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	local, err := s.Read(ctx, terminal.Key)
	if err != nil {
		return err
	}
	if err := validateSharedSpoolTerminal(local, terminal); err != nil {
		return err
	}
	if !sharedMatchesTerminal(local.Terminal, &terminal) || local.ReceiptID != receiptID || !sharedIdentity(receiptID) {
		return errSharedAdmissionConflict
	}
	return sharedSpoolError(s.journal.MakeReady(ctx, eventqueue.SharedSpoolTerminalReceipt{Key: sharedJournalKey(terminal.Key), ReceiptID: receiptID, TerminalDigest: terminal.Digest}))
}

type sharedCallDelivery struct {
	Receipt eventqueue.SharedSpoolTerminalReceipt
	Fact    []byte
}

func (s *durableSharedCallSpool) ReadDelivery(ctx context.Context, limit int) ([]sharedCallDelivery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := s.journal.ReadReady(ctx, limit)
	if err != nil {
		return nil, sharedSpoolError(err)
	}
	result := make([]sharedCallDelivery, 0, len(entries))
	for _, entry := range entries {
		var terminal sharedTerminalIntent
		if !sharedDecodeExact(entry.Data, &terminal) || !sharedHasDigest(terminal, terminal.Digest) || sharedJournalKey(terminal.Key) != entry.Receipt.Key || terminal.Digest != entry.Receipt.TerminalDigest || !terminal.ExecutorClosed {
			return nil, errSharedAdmissionConflict
		}
		local, err := s.Read(ctx, terminal.Key)
		if err != nil || !local.Ready || local.ReceiptID != entry.Receipt.ReceiptID || !sharedMatchesTerminal(local.Terminal, &terminal) {
			return nil, sharedReadFailure(err)
		}
		fact, err := sharedCallPayload(terminal.Key.RequestID, terminal.Fact)
		if err != nil || !reflect.DeepEqual(terminal.Actual, quotaCallSettlement(fact)) || !sharedValidClosure(fact, terminal.Upstream) {
			return nil, errSharedAdmissionConflict
		}
		result = append(result, sharedCallDelivery{Receipt: entry.Receipt, Fact: bytes.Clone(terminal.Fact)})
	}
	return result, nil
}

func (s *durableSharedCallSpool) AckDelivery(ctx context.Context, receipt eventqueue.SharedSpoolTerminalReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return sharedSpoolError(s.journal.AckReady(ctx, receipt))
}

func sharedIntentKey(intent sharedAdmissionIntent) sharedAdmissionKey {
	return sharedAdmissionKey{Owner: intent.Owner, RequestID: intent.RequestID, ExecutionID: intent.ExecutionID, AdmissionDigest: intent.Digest}
}

func sharedJournalKey(key sharedAdmissionKey) eventqueue.SharedSpoolKey {
	return eventqueue.SharedSpoolKey{Identity: eventqueue.SharedSpoolIdentity{InstallationID: key.Owner.InstallationID, SpoolID: key.Owner.SpoolID, ModeRevision: key.Owner.ModeRevision},
		IncarnationID: key.Owner.IncarnationID, Fence: key.Owner.Fence, ExecutionID: key.ExecutionID, RequestID: key.RequestID, AdmissionDigest: key.AdmissionDigest}
}

func sharedSpoolBody(value any, digest string) (eventqueue.SharedSpoolBody, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return eventqueue.SharedSpoolBody{}, errSharedAdmissionInvalid
	}
	return eventqueue.SharedSpoolBody{Digest: digest, Data: raw}, nil
}

func sharedDecodeExact[T any](raw []byte, value *T) bool {
	if json.Unmarshal(raw, value) != nil {
		return false
	}
	canonical, err := json.Marshal(value)
	return err == nil && bytes.Equal(raw, canonical)
}

func sharedSpoolScopeMatches(intent sharedAdmissionIntent, fact CallFact) bool {
	initial, err := sharedCallPayload(intent.RequestID, intent.Fallback)
	return err == nil && intent.Reservation != nil && (fact.SnapshotID == "" || fact.SnapshotID == intent.Reservation.SnapshotID) && initial.UserID == fact.UserID && initial.ProjectID == fact.ProjectID && initial.TeamID == fact.TeamID &&
		initial.TeamMembershipID == fact.TeamMembershipID && initial.KeyID == fact.KeyID && initial.ModelID == fact.ModelID && initial.Protocol == fact.Protocol
}

func sharedSpoolAttemptFacts(local *sharedSpoolRecord, fact CallFact, number int) bool {
	if number < 0 || number > len(local.Attempts) || len(fact.Attempts) != number || !sharedSpoolScopeMatches(local.Intent, fact) {
		return false
	}
	for i := range number {
		actual, expected := fact.Attempts[i], local.Attempts[i]
		if actual.ID != expected.AttemptID || actual.AttemptNumber != expected.Number || actual.CredentialID != expected.CredentialID || actual.SnapshotID != expected.SnapshotID || actual.ConnectionID != expected.ConnectionID || actual.ProviderModelID != expected.ProviderModelID {
			return false
		}
	}
	if number > 1 && len(local.Checkpoints) >= number-1 {
		previous, err := sharedCallPayload(local.Intent.RequestID, local.Checkpoints[number-2].Fallback)
		if err != nil || !reflect.DeepEqual(fact.Attempts[:number-1], previous.Attempts) {
			return false
		}
	}
	return true
}

func validateSharedSpoolAdmission(intent sharedAdmissionIntent) error {
	initial, err := sharedCallPayload(intent.RequestID, intent.Fallback)
	if err != nil || len(initial.Attempts) != 0 || !sharedHasDigest(intent, intent.Digest) ||
		!admissionOwnerTimeValid(intent.OwnerStartedAt) || intent.OwnerStartedAt.UnixNano() <= 0 ||
		!reflect.DeepEqual(intent.Recovery, zeroQuotaSettlement(intent.Bound)) || !sharedIdentity(intent.Owner.InstallationID) || !sharedIdentity(intent.Owner.SpoolID) ||
		!sharedIdentity(intent.Owner.IncarnationID) || !sharedIdentity(intent.ExecutionID) || intent.Owner.ModeRevision == 0 || intent.Owner.Fence == 0 ||
		!sharedAuthorizationValid(intent.Authorization) || !intent.Authorization.matchesCall(initial) || intent.CalendarRevision == "" || len(intent.CalendarRevision) > 128 ||
		intent.TimeZone == "" || len(intent.TimeZone) > 128 || len(intent.Policies) == 0 || len(intent.Policies) > 8 {
		return errSharedAdmissionInvalid
	}
	if _, err := gatewayReservationSQLSelection(intent.Reservation, nil); err != nil || intent.Reservation.RequestID != intent.RequestID ||
		intent.Reservation.Model.ID != intent.Authorization.Model.ID || !intent.Reservation.Model.Birth.Equal(intent.Authorization.Model.CreatedAt) ||
		intent.Reservation.Model.ID != initial.ModelID || intent.Reservation.Protocol != initial.Protocol || initial.SnapshotID != "" && intent.Reservation.SnapshotID != initial.SnapshotID ||
		!reflect.DeepEqual(intent.Reservation.MaximumHold, intent.Bound) || eventqueue.ValidateQuotaBound(intent.Bound) != nil {
		return errSharedAdmissionInvalid
	}
	// The complete manifest and owner birth belong to the original intent. The
	// smaller spool-body bound wins over the standalone manifest capture limit;
	// never truncate candidates or pricing constituents to make a capture fit.
	if body, err := json.Marshal(intent); err != nil || len(body) > eventqueue.SharedSpoolMaxDataBytes {
		return errSharedAdmissionInvalid
	}
	return nil
}

func validateSharedSpoolPrefix(local *sharedSpoolRecord, attempt sharedAttemptIntent) error {
	if attempt.Number == 1 {
		return nil
	}
	if len(local.Checkpoints) < attempt.Number-1 {
		return errSharedAdmissionConflict
	}
	previous, err := sharedCallPayload(attempt.Key.RequestID, local.Checkpoints[attempt.Number-2].Fallback)
	if err != nil || len(previous.Attempts) == 0 || !callAttemptsProveNoWork(previous.Attempts) || !sharedRetryEvidence(previous.Attempts[len(previous.Attempts)-1]) {
		return errSharedAdmissionConflict
	}
	parsed, err := sharedCallPayload(attempt.Key.RequestID, attempt.Fallback)
	if err != nil || len(parsed.Attempts) != attempt.Number || !reflect.DeepEqual(parsed.Attempts[:len(parsed.Attempts)-1], previous.Attempts) {
		return errSharedAdmissionConflict
	}
	return nil
}

func validateSharedSpoolAttempt(local *sharedSpoolRecord, attempt sharedAttemptIntent) error {
	if attempt.Key != sharedIntentKey(local.Intent) || !sharedHasDigest(attempt, attempt.Digest) || attempt.Number < 1 || attempt.Number > routeattempt.MaxAttempts ||
		!sharedIdentity(attempt.AttemptID) || !sharedIdentity(attempt.CredentialID) || !sharedIdentity(attempt.SnapshotID) || !sharedIdentity(attempt.ConnectionID) || !sharedIdentity(attempt.ProviderModelID) {
		return errSharedAdmissionInvalid
	}
	parsed, err := sharedCallPayload(attempt.Key.RequestID, attempt.Fallback)
	if err != nil || len(parsed.Attempts) != attempt.Number || !sharedSpoolScopeMatches(local.Intent, parsed) ||
		local.Intent.Reservation == nil || attempt.SnapshotID != local.Intent.Reservation.SnapshotID {
		return errSharedAdmissionInvalid
	}
	matched := false
	for _, candidate := range local.Intent.Reservation.Candidates {
		if candidate.Credential.Identity.ID == attempt.CredentialID && candidate.Connection.ID == attempt.ConnectionID && candidate.ProviderModel.ID == attempt.ProviderModelID {
			matched = true
		}
	}
	if !matched {
		return errSharedAdmissionConflict
	}
	last := parsed.Attempts[attempt.Number-1]
	if last.ID != attempt.AttemptID || last.AttemptNumber != attempt.Number || last.CredentialID != attempt.CredentialID || last.SnapshotID != attempt.SnapshotID || last.ConnectionID != attempt.ConnectionID || last.ProviderModelID != attempt.ProviderModelID ||
		last.Status != "error" || last.WorkEvidence != "unknown" || last.NativeCompletionEvidence != "" && last.NativeCompletionEvidence != "unknown" {
		return errSharedAdmissionConflict
	}
	if attempt.Number <= len(local.Attempts) && local.Attempts[attempt.Number-1].Digest != attempt.Digest {
		return errSharedAdmissionConflict
	}
	return nil
}

func validateSharedSpoolCheckpoint(local *sharedSpoolRecord, checkpoint sharedAttemptCheckpoint) error {
	if checkpoint.Key != sharedIntentKey(local.Intent) || !sharedHasDigest(checkpoint, checkpoint.Digest) || checkpoint.Number < 1 || checkpoint.Number > len(local.Attempts) || local.Attempts[checkpoint.Number-1].Digest != checkpoint.AttemptDigest {
		return errSharedAdmissionConflict
	}
	parsed, err := sharedCallPayload(checkpoint.Key.RequestID, checkpoint.Fallback)
	if err != nil || !sharedSpoolAttemptFacts(local, parsed, checkpoint.Number) || !reflect.DeepEqual(checkpoint.Recovery, sharedCheckpointRecovery(parsed, local.Intent.Bound)) {
		return errSharedAdmissionConflict
	}
	return nil
}

func validateSharedSpoolTerminal(local *sharedSpoolRecord, terminal sharedTerminalIntent) error {
	if terminal.Key != sharedIntentKey(local.Intent) || !terminal.ExecutorClosed || !sharedHasDigest(terminal, terminal.Digest) || terminal.LastAttempt != len(local.Attempts) {
		return errSharedAdmissionConflict
	}
	parsed, err := sharedCallPayload(terminal.Key.RequestID, terminal.Fact)
	if err != nil || !sharedSpoolAttemptFacts(local, parsed, terminal.LastAttempt) || !reflect.DeepEqual(terminal.Actual, quotaCallSettlement(parsed)) || !sharedValidClosure(parsed, terminal.Upstream) {
		return errSharedAdmissionConflict
	}
	return nil
}

func sharedSpoolError(err error) error {
	switch {
	case errors.Is(err, eventqueue.ErrSharedSpoolConflict), errors.Is(err, eventqueue.ErrSharedSpoolMode):
		return errSharedAdmissionConflict
	case errors.Is(err, eventqueue.ErrInvalid):
		return errSharedAdmissionInvalid
	default:
		return err
	}
}
