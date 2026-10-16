package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
)

var errSharedInjected = errors.New("injected interrupted storage operation")

type sharedCommitFault struct {
	operation string
	after     bool
	used      bool
}

func (f *sharedCommitFault) fail(operation string, after bool) error {
	if f != nil && !f.used && f.operation == operation && f.after == after {
		f.used = true
		return errSharedInjected
	}
	return nil
}

// These stores model independent durable sides and interrupted commits. They
// deliberately do not implement SQL locking or claim PostgreSQL/MySQL proof.
type sharedFaultSpool struct {
	fault  *sharedCommitFault
	record *sharedSpoolRecord
}

func (s *sharedFaultSpool) Reserve(ctx context.Context, intent sharedAdmissionIntent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.fault.fail("reserve", false); err != nil {
		return err
	}
	if s.record != nil && s.record.Intent.Digest != intent.Digest {
		return errSharedAdmissionConflict
	}
	if s.record == nil {
		s.record = &sharedSpoolRecord{Intent: sharedTestCopy(intent)}
	}
	return s.fault.fail("reserve", true)
}

func (s *sharedFaultSpool) Read(ctx context.Context, key sharedAdmissionKey) (*sharedSpoolRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.record == nil {
		return nil, eventqueue.ErrMissing
	}
	if sharedTestKey(s.record.Intent) != key {
		return nil, errSharedAdmissionConflict
	}
	return sharedTestCopy(s.record), nil
}

func (s *sharedFaultSpool) ConfirmAcceptance(ctx context.Context, key sharedAdmissionKey, receipt string) error {
	if _, err := s.Read(ctx, key); err != nil {
		return err
	}
	if err := s.fault.fail("confirm_acceptance", false); err != nil {
		return err
	}
	if s.record.ReceiptID != "" && s.record.ReceiptID != receipt {
		return errSharedAdmissionConflict
	}
	s.record.ReceiptID = receipt
	return s.fault.fail("confirm_acceptance", true)
}

func (s *sharedFaultSpool) RejectAdmission(ctx context.Context, key sharedAdmissionKey) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.record == nil || sharedTestKey(s.record.Intent) != key || s.record.ReceiptID != "" || len(s.record.Attempts) != 0 || s.record.Terminal != nil {
		return errSharedAdmissionConflict
	}
	if err := s.fault.fail("reject_admission", false); err != nil {
		return err
	}
	s.record = nil
	return s.fault.fail("reject_admission", true)
}

func (s *sharedFaultSpool) PrepareAttempt(ctx context.Context, intent sharedAttemptIntent) error {
	if _, err := s.Read(ctx, intent.Key); err != nil {
		return err
	}
	if err := s.fault.fail("prepare_attempt", false); err != nil {
		return err
	}
	if intent.Number <= len(s.record.Attempts) {
		if s.record.Attempts[intent.Number-1].Digest != intent.Digest {
			return errSharedAdmissionConflict
		}
	} else if intent.Number == len(s.record.Attempts)+1 && s.record.ReceiptID != "" && s.record.Terminal == nil {
		s.record.Attempts = append(s.record.Attempts, sharedTestCopy(intent))
	} else {
		return errSharedAdmissionConflict
	}
	return s.fault.fail("prepare_attempt", true)
}

func (s *sharedFaultSpool) ConfirmDispatch(ctx context.Context, receipt sharedDispatchReceipt) error {
	if _, err := s.Read(ctx, receipt.Key); err != nil {
		return err
	}
	if err := s.fault.fail("confirm_dispatch", false); err != nil {
		return err
	}
	if receipt.Number < 1 || receipt.Number > len(s.record.Attempts) || s.record.Attempts[receipt.Number-1].Digest != receipt.AttemptDigest {
		return errSharedAdmissionConflict
	}
	if receipt.Number > len(s.record.Dispatches) {
		s.record.Dispatches = append(s.record.Dispatches, receipt)
	}
	return s.fault.fail("confirm_dispatch", true)
}

func (s *sharedFaultSpool) RecordAttempt(ctx context.Context, checkpoint sharedAttemptCheckpoint) error {
	if _, err := s.Read(ctx, checkpoint.Key); err != nil {
		return err
	}
	if err := s.fault.fail("record_attempt", false); err != nil {
		return err
	}
	if checkpoint.Number < 1 || checkpoint.Number > len(s.record.Attempts) || s.record.Attempts[checkpoint.Number-1].Digest != checkpoint.AttemptDigest {
		return errSharedAdmissionConflict
	}
	if checkpoint.Number <= len(s.record.Checkpoints) {
		if s.record.Checkpoints[checkpoint.Number-1].Digest != checkpoint.Digest {
			return errSharedAdmissionConflict
		}
	} else if checkpoint.Number == len(s.record.Checkpoints)+1 {
		s.record.Checkpoints = append(s.record.Checkpoints, sharedTestCopy(checkpoint))
	} else {
		return errSharedAdmissionConflict
	}
	return s.fault.fail("record_attempt", true)
}

func (s *sharedFaultSpool) PrepareTerminal(ctx context.Context, terminal sharedTerminalIntent) error {
	if _, err := s.Read(ctx, terminal.Key); err != nil {
		return err
	}
	if err := s.fault.fail("prepare_terminal", false); err != nil {
		return err
	}
	if s.record.Terminal != nil && s.record.Terminal.Digest != terminal.Digest {
		return errSharedAdmissionConflict
	}
	copy := sharedTestCopy(terminal)
	s.record.Terminal = &copy
	return s.fault.fail("prepare_terminal", true)
}

func (s *sharedFaultSpool) MakeReady(ctx context.Context, terminal sharedTerminalIntent, receipt string) error {
	if _, err := s.Read(ctx, terminal.Key); err != nil {
		return err
	}
	if err := s.fault.fail("make_ready", false); err != nil {
		return err
	}
	if s.record.Terminal == nil || s.record.Terminal.Digest != terminal.Digest || s.record.ReceiptID != receipt {
		return errSharedAdmissionConflict
	}
	s.record.Ready = true
	return s.fault.fail("make_ready", true)
}

type sharedFaultAuthority struct {
	fault             *sharedCommitFault
	owner             sharedAdmissionOwner
	receipt           *sharedAuthorityReceipt
	acceptCount       int
	settleCount       int
	rpm               int64
	concurrency       int64
	tokensUsed        int64
	tokensHeld        int64
	moneyHeld         string
	settleHadDeadline bool
	settleWasCanceled bool
}

type sharedRejectedAuthority struct {
	sharedEconomicAuthority
	err     error
	intents *[]sharedAdmissionIntent
}

func (a sharedRejectedAuthority) Accept(_ context.Context, intent sharedAdmissionIntent) (*sharedAuthorityReceipt, error) {
	if a.intents != nil {
		*a.intents = append(*a.intents, sharedTestCopy(intent))
	}
	return nil, a.err
}

func (a *sharedFaultAuthority) Accept(ctx context.Context, intent sharedAdmissionIntent) (*sharedAuthorityReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if intent.Owner != a.owner {
		return nil, errSharedAdmissionConflict
	}
	if err := a.fault.fail("accept", false); err != nil {
		return nil, err
	}
	key := sharedTestKey(intent)
	if a.receipt != nil && a.receipt.Key != key {
		return nil, errSharedAdmissionConflict
	}
	if a.receipt == nil {
		a.receipt = &sharedAuthorityReceipt{Key: key, ReceiptID: "receipt_shared"}
		a.acceptCount++
		a.rpm++
		a.concurrency++
		if intent.Bound.Tokens != nil {
			a.tokensHeld = *intent.Bound.Tokens
		}
		if intent.Bound.Money != nil {
			a.moneyHeld = *intent.Bound.Money
		}
	}
	if err := a.fault.fail("accept", true); err != nil {
		return nil, err
	}
	return sharedTestCopy(a.receipt), nil
}

func (a *sharedFaultAuthority) Read(ctx context.Context, key sharedAdmissionKey) (*sharedAuthorityReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if a.receipt == nil {
		return nil, eventqueue.ErrMissing
	}
	if key != a.receipt.Key || key.Owner != a.owner {
		return nil, errSharedAdmissionConflict
	}
	return sharedTestCopy(a.receipt), nil
}

func (a *sharedFaultAuthority) MarkDispatch(ctx context.Context, intent sharedAttemptIntent) (*sharedDispatchReceipt, error) {
	if _, err := a.Read(ctx, intent.Key); err != nil {
		return nil, err
	}
	if err := a.fault.fail("mark_dispatch", false); err != nil {
		return nil, err
	}
	if a.receipt.Terminal != nil {
		return nil, errSharedAdmissionConflict
	}
	receipt := sharedDispatchReceipt{Key: intent.Key, Number: intent.Number, AttemptDigest: intent.Digest}
	if intent.Number <= len(a.receipt.Dispatches) {
		if a.receipt.Dispatches[intent.Number-1] != receipt {
			return nil, errSharedAdmissionConflict
		}
	} else if intent.Number == len(a.receipt.Dispatches)+1 {
		a.receipt.Dispatches = append(a.receipt.Dispatches, receipt)
	} else {
		return nil, errSharedAdmissionConflict
	}
	if err := a.fault.fail("mark_dispatch", true); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (a *sharedFaultAuthority) Checkpoint(ctx context.Context, checkpoint sharedAttemptCheckpoint) error {
	if _, err := a.Read(ctx, checkpoint.Key); err != nil {
		return err
	}
	if err := a.fault.fail("checkpoint", false); err != nil {
		return err
	}
	if checkpoint.Number < 1 || checkpoint.Number > len(a.receipt.Dispatches) || a.receipt.Dispatches[checkpoint.Number-1].AttemptDigest != checkpoint.AttemptDigest {
		return errSharedAdmissionConflict
	}
	if checkpoint.Number <= len(a.receipt.Checkpoints) {
		if a.receipt.Checkpoints[checkpoint.Number-1].Digest != checkpoint.Digest {
			return errSharedAdmissionConflict
		}
	} else if checkpoint.Number == len(a.receipt.Checkpoints)+1 {
		a.receipt.Checkpoints = append(a.receipt.Checkpoints, sharedTestCopy(checkpoint))
	} else {
		return errSharedAdmissionConflict
	}
	return a.fault.fail("checkpoint", true)
}

func (a *sharedFaultAuthority) Settle(ctx context.Context, terminal sharedTerminalIntent) (*sharedAuthorityReceipt, error) {
	_, a.settleHadDeadline = ctx.Deadline()
	a.settleWasCanceled = ctx.Err() != nil
	if _, err := a.Read(ctx, terminal.Key); err != nil {
		return nil, err
	}
	if err := a.fault.fail("settle", false); err != nil {
		return nil, err
	}
	if !terminal.ExecutorClosed {
		return nil, errSharedAdmissionConflict
	}
	if a.receipt.Terminal != nil && a.receipt.Terminal.Digest != terminal.Digest {
		return nil, errSharedAdmissionConflict
	}
	if a.receipt.Terminal == nil {
		copy := sharedTestCopy(terminal)
		a.receipt.Terminal = &copy
		a.settleCount++
		// Existing gateway concurrency tracks the logical local executor, while
		// unknown economic dimensions retain their original conservative holds.
		a.concurrency--
		if terminal.Actual.Tokens != nil {
			a.tokensUsed += *terminal.Actual.Tokens
			a.tokensHeld = 0
		}
		if terminal.Actual.Money != nil {
			a.moneyHeld = "0"
		}
	}
	if err := a.fault.fail("settle", true); err != nil {
		return nil, err
	}
	return sharedTestCopy(a.receipt), nil
}

func sharedTestCopy[T any](value T) T {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		panic(err)
	}
	return result
}

func sharedTestKey(intent sharedAdmissionIntent) sharedAdmissionKey {
	return sharedAdmissionKey{Owner: intent.Owner, RequestID: intent.RequestID, ExecutionID: intent.ExecutionID, AdmissionDigest: intent.Digest}
}

func sharedTestFixture(t *testing.T) (*sharedAdmissionCoordinator, *sharedFaultSpool, *sharedFaultAuthority, sharedAdmissionRequest) {
	t.Helper()
	owner := sharedAdmissionOwner{InstallationID: "installation_shared", ModeRevision: 2, SpoolID: "spool_shared", IncarnationID: "incarnation_shared", Fence: 3}
	tokens := int64(100)
	proof, err := newSharedCapturedAuthorization(sharedAuthorizationTestFacts("personal_key"), sharedAuthorizationTestTime())
	if err != nil {
		t.Fatal(err)
	}
	manifest := sharedTestReservation(t, "req_shared", proof, true, true)
	request := sharedAdmissionRequest{RequestID: "req_shared", Reservation: manifest, OwnerStartedAt: sharedAuthorizationTestTime().Add(-time.Minute), Authorization: proof, CalendarRevision: "calendar_shared", TimeZone: "UTC",
		Policies: []eventqueue.QuotaLimit{{Limit: eventqueue.Limit{Account: "usr_shared"}, Revision: "policy_shared", TokensMonth: &tokens}},
		Bound:    manifest.MaximumHold, Recovery: zeroQuotaSettlement(manifest.MaximumHold), Fallback: sharedTestPayload(t, nil, false)}
	spool := &sharedFaultSpool{}
	authority := &sharedFaultAuthority{owner: owner}
	coordinator, err := newSharedAdmissionCoordinator(owner, request, spool, authority, sharedCoordinatorOptions{OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator, spool, authority, request
}

func sharedTestPayload(t *testing.T, attempts []CallAttempt, knownTokens bool) []byte {
	t.Helper()
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	fact := CallFact{RequestID: "req_shared", UserID: "usr_shared", KeyID: "key_shared", ModelID: "mdl_shared", ModelName: "Shared", Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now.Add(time.Second), ErrorCode: "process_interrupted", Attempts: attempts}
	if knownTokens {
		input, output := int64(20), int64(1)
		fact.UsageComplete, fact.InputTokens, fact.OutputTokens = true, &input, &output
	}
	raw, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateCallFact(fact); err != nil {
		t.Fatal(err)
	}
	return raw
}

func sharedTestAttempt(number int, noWork bool) CallAttempt {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	work, failure := "unknown", "permanent_failure"
	if noWork {
		work = "not_sent"
		failure = "connection_failure"
	}
	return CallAttempt{ID: "att_shared_" + strconv.Itoa(number), CredentialID: "crd_shared_" + strconv.Itoa(number), SnapshotID: sharedTestReservationSnapshot, ConnectionID: "con_shared_" + strconv.Itoa(number), ProviderModelID: "pmd_shared_" + strconv.Itoa(number), AttemptNumber: number,
		Status: "error", FailureClass: failure, WorkEvidence: work, NativeCompletionEvidence: "unknown", StartedAt: now, CompletedAt: now.Add(time.Second)}
}

func sharedTestDispatch(t *testing.T, number int, history ...CallAttempt) sharedAttemptIntent {
	t.Helper()
	current := sharedTestAttempt(number, false)
	attempts := append(append([]CallAttempt(nil), history...), current)
	return sharedAttemptIntent{Number: number, AttemptID: current.ID, CredentialID: current.CredentialID, SnapshotID: current.SnapshotID, ConnectionID: current.ConnectionID, ProviderModelID: current.ProviderModelID, Fallback: sharedTestPayload(t, attempts, false)}
}

func TestSharedAdmissionInterruptedRejectionFailsClosedAndRetriesOriginalIntent(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(sharedTestFaultSuffix(after), func(t *testing.T) {
			coordinator, spool, authority, request := sharedTestFixture(t)
			original := sharedTestCopy(coordinator.intent)
			var intents []sharedAdmissionIntent
			coordinator.authority = sharedRejectedAuthority{sharedEconomicAuthority: authority, err: eventqueue.ErrQuotaTokens, intents: &intents}
			fault := &sharedCommitFault{operation: "reject_admission", after: after}
			spool.fault = fault
			err := coordinator.Admit(t.Context())
			if !fault.used || !errors.Is(err, errSharedInjected) || !errors.Is(err, errSharedAdmissionUncertain) || errors.Is(err, errSharedAdmissionRejected) {
				t.Fatalf("interrupted rejection classification = %v, fault used = %v", err, fault.used)
			}
			if coordinator.admitted || authority.receipt != nil || authority.acceptCount != 0 || authority.rpm != 0 || authority.concurrency != 0 || authority.tokensHeld != 0 || authority.moneyHeld != "" {
				t.Fatal("interrupted rejection admitted or charged the executor")
			}
			if after {
				if spool.record != nil {
					t.Fatal("post-deletion fault restored the rejected reservation")
				}
			} else if spool.record == nil || !reflect.DeepEqual(spool.record.Intent, original) {
				t.Fatal("pre-deletion fault discarded or changed the original reservation")
			}
			if err := coordinator.Dispatch(t.Context(), sharedTestDispatch(t, 1), func(context.Context) error {
				t.Fatal("interrupted rejection dispatched transport")
				return nil
			}); !errors.Is(err, errSharedDispatchForbidden) {
				t.Fatalf("dispatch after interrupted rejection = %v", err)
			}
			if sharedGatewayRejectionMayBeRecorded(&sharedGatewayExecution{admissionAttempted: true}, &GatewayResult{}) {
				t.Fatal("interrupted rejection was eligible for direct call recording")
			}
			request.Fallback[0] = '!'
			*request.Bound.Tokens = 999
			request.Policies[0].Revision = "changed"
			if err := coordinator.Admit(t.Context()); !errors.Is(err, errSharedAdmissionRejected) || !errors.Is(err, eventqueue.ErrQuotaTokens) || errors.Is(err, errSharedAdmissionUncertain) {
				t.Fatalf("identical rejection retry = %v", err)
			}
			if spool.record != nil || coordinator.admitted || authority.receipt != nil || len(intents) != 2 || !reflect.DeepEqual(intents[0], original) || !reflect.DeepEqual(intents[1], original) {
				t.Fatal("rejection retry changed immutable intent or retained its reservation")
			}
		})
	}
}

func TestSharedAdmissionInterruptedReservationRetainsOriginalIntent(t *testing.T) {
	for _, operation := range []string{"reserve", "accept", "confirm_acceptance"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				c, spool, authority, request := sharedTestFixture(t)
				original := sharedTestCopy(request)
				fault := &sharedCommitFault{operation: operation, after: after}
				spool.fault, authority.fault = fault, fault
				if err := c.Admit(context.Background()); !errors.Is(err, errSharedInjected) {
					t.Fatalf("interrupted admission = %v", err)
				}
				if c.admitted {
					t.Fatal("an uncertain operation admitted the executor")
				}
				if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), func(context.Context) error { t.Fatal("dispatch before acceptance"); return nil }); !errors.Is(err, errSharedDispatchForbidden) {
					t.Fatalf("pre-acceptance dispatch = %v", err)
				}
				request.Fallback[0] = '!'
				*request.Bound.Tokens = 999
				*request.Bound.Money = "9000"
				request.Policies[0].Revision = "changed"
				request.Authorization.User.ID = "usr_changed"
				request.Authorization.PersonalGrants.Revision = "changed"
				request.Authorization.Key.LifecycleRevision = "changed"
				if err := c.Admit(context.Background()); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(spool.record.Intent.sharedAdmissionRequest, original) || authority.acceptCount != 1 || authority.rpm != 1 || authority.concurrency != 1 || authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" {
					t.Fatalf("retry changed original body or economic debit: local=%+v authority=%+v", spool.record.Intent.sharedAdmissionRequest, authority)
				}
			})
		}
	}
}

func TestSharedAdmissionUncertainDispatchNeverExecutesOrReplays(t *testing.T) {
	for _, operation := range []string{"mark_dispatch", "confirm_dispatch"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				c, spool, authority, request := sharedTestFixture(t)
				if err := c.Admit(context.Background()); err != nil {
					t.Fatal(err)
				}
				fault := &sharedCommitFault{operation: operation, after: after}
				spool.fault, authority.fault = fault, fault
				calls := 0
				execute := func(context.Context) error { calls++; return nil }
				attempt := sharedTestDispatch(t, 1)
				if err := c.Dispatch(context.Background(), attempt, execute); !errors.Is(err, errSharedInjected) {
					t.Fatalf("interrupted dispatch = %v", err)
				}
				if err := c.Reconcile(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := c.Dispatch(context.Background(), attempt, execute); !errors.Is(err, errSharedDispatchForbidden) {
					t.Fatalf("reconciled redispatch = %v", err)
				}
				if calls != 0 || authority.concurrency != 1 || authority.tokensHeld != 100 {
					t.Fatalf("uncertainty executed/reclaimed work: calls=%d authority=%+v", calls, authority)
				}
				// Reconstructing a transient object cannot adopt this durable request.
				restarted, err := newSharedAdmissionCoordinator(c.intent.Owner, request, spool, authority, sharedCoordinatorOptions{OperationTimeout: time.Second})
				if err != nil {
					t.Fatal(err)
				}
				if err := restarted.Admit(context.Background()); !errors.Is(err, errSharedAdmissionConflict) {
					t.Fatalf("reconstructed executor adopted receipt: %v", err)
				}
				if calls != 0 {
					t.Fatal("recovery executed inference")
				}
			})
		}
	}
}

func TestSharedAdmissionLocalPreparationCanRetryBeforeDispatchIntent(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(sharedTestFaultSuffix(after), func(t *testing.T) {
			c, spool, authority, _ := sharedTestFixture(t)
			if err := c.Admit(context.Background()); err != nil {
				t.Fatal(err)
			}
			spool.fault = &sharedCommitFault{operation: "prepare_attempt", after: after}
			attempt := sharedTestDispatch(t, 1)
			calls := 0
			execute := func(context.Context) error { calls++; return nil }
			if err := c.Dispatch(context.Background(), attempt, execute); !errors.Is(err, errSharedInjected) {
				t.Fatal(err)
			}
			if calls != 0 || len(authority.receipt.Dispatches) != 0 {
				t.Fatal("local preparation crossed the dispatch barrier")
			}
			if err := c.Dispatch(context.Background(), attempt, execute); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(authority.receipt.Dispatches) != 1 {
				t.Fatal("original prepared attempt did not execute once")
			}
			if err := c.Dispatch(context.Background(), attempt, execute); !errors.Is(err, errSharedDispatchForbidden) {
				t.Fatalf("duplicate dispatch = %v", err)
			}
		})
	}
}

func TestSharedAdmissionInterruptedCheckpointBlocksNativeFallback(t *testing.T) {
	for _, operation := range []string{"record_attempt", "checkpoint"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				c, spool, authority, _ := sharedTestFixture(t)
				if err := c.Admit(context.Background()); err != nil {
					t.Fatal(err)
				}
				calls := 0
				execute := func(context.Context) error { calls++; return nil }
				if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), execute); err != nil {
					t.Fatal(err)
				}
				fault := &sharedCommitFault{operation: operation, after: after}
				spool.fault, authority.fault = fault, fault
				completed := sharedTestAttempt(1, true)
				fallback := sharedTestPayload(t, []CallAttempt{completed}, false)
				zero, zeroMoney := int64(0), "0"
				recovery := eventqueue.QuotaSettlement{Tokens: &zero, Money: &zeroMoney, Currency: "USD"}
				if err := c.Checkpoint(context.Background(), 1, fallback, recovery); !errors.Is(err, errSharedInjected) {
					t.Fatal(err)
				}
				if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 2, completed), execute); !errors.Is(err, errSharedDispatchForbidden) {
					t.Fatalf("fallback before checkpoint acknowledgement = %v", err)
				}
				changed := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false)
				if err := c.Checkpoint(context.Background(), 1, changed, eventqueue.QuotaSettlement{}); !errors.Is(err, errSharedAdmissionConflict) {
					t.Fatalf("checkpoint replaced uncertain evidence: %v", err)
				}
				if err := c.Checkpoint(context.Background(), 1, fallback, recovery); err != nil {
					t.Fatal(err)
				}
				if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 2, completed), execute); err != nil {
					t.Fatal(err)
				}
				if calls != 2 || authority.acceptCount != 1 || authority.rpm != 1 || authority.concurrency != 1 {
					t.Fatalf("fallback changed logical admission: calls=%d authority=%+v", calls, authority)
				}
			})
		}
	}
}

func TestSharedAdmissionUnknownAttemptCannotAuthorizeNativeFallback(t *testing.T) {
	c, _, _, _ := sharedTestFixture(t)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := 0
	execute := func(context.Context) error { calls++; return nil }
	if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), execute); err != nil {
		t.Fatal(err)
	}
	unknown := sharedTestAttempt(1, false)
	if err := c.Checkpoint(context.Background(), 1, sharedTestPayload(t, []CallAttempt{unknown}, false), eventqueue.QuotaSettlement{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 2, unknown), execute); !errors.Is(err, errSharedDispatchForbidden) {
		t.Fatalf("unknown attempt allowed fallback = %v", err)
	}
	if calls != 1 {
		t.Fatal("unknown work was replayed")
	}
}

func TestSharedAdmissionTerminalFaultsKeepUnknownEconomicsAndReleaseClosedExecutor(t *testing.T) {
	for _, operation := range []string{"prepare_terminal", "settle", "make_ready"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				c, spool, authority, _ := sharedTestFixture(t)
				if err := c.Admit(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
				fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false)
				if err := c.Settle(context.Background(), fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedDispatchForbidden) {
					t.Fatalf("settled without executor closure: %v", err)
				}
				if authority.concurrency != 1 {
					t.Fatal("missing terminal/heartbeat-like observation released concurrency")
				}
				if err := c.CloseExecutor(); err != nil {
					t.Fatal(err)
				}
				fault := &sharedCommitFault{operation: operation, after: after}
				spool.fault, authority.fault = fault, fault
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
					t.Fatalf("interrupted settlement = %v", err)
				}
				if operation != "make_ready" && spool.record.Ready {
					t.Fatal("fact became ready before confirmed settlement")
				}
				if authority.receipt.Terminal != nil {
					if authority.concurrency != 0 || authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" {
						t.Fatalf("closure fabricated known economics: %+v", authority)
					}
				}
				if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); err != nil {
					t.Fatal(err)
				}
				if !spool.record.Ready || authority.settleCount != 1 || authority.concurrency != 0 || authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" || !authority.settleHadDeadline || authority.settleWasCanceled {
					t.Fatalf("terminal retry lost durability/context/unknowns: local=%+v authority=%+v", spool.record, authority)
				}
			})
		}
	}
}

func TestSharedAdmissionExactTerminalReadbackRecoversKnownTokensUnknownMoney(t *testing.T) {
	c, spool, authority, _ := sharedTestFixture(t)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, true)
	var parsed CallFact
	if err := json.Unmarshal(fact, &parsed); err != nil {
		t.Fatal(err)
	}
	actual := quotaCallSettlement(parsed)
	if actual.Tokens == nil || *actual.Tokens != 21 || actual.Money != nil {
		t.Fatalf("fixture settlement = %+v", actual)
	}
	if err := c.CloseExecutor(); err != nil {
		t.Fatal(err)
	}
	authority.fault = &sharedCommitFault{operation: "settle", after: true}
	if err := c.Settle(context.Background(), fact, actual, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
		t.Fatal(err)
	}
	if spool.record.Ready || authority.tokensUsed != 21 || authority.tokensHeld != 0 || authority.moneyHeld != "0.000000000000000003" || authority.concurrency != 0 {
		t.Fatalf("partial terminal projection = %+v", authority)
	}
	if err := c.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !spool.record.Ready || authority.settleCount != 1 {
		t.Fatal("readback failed to complete original terminal readiness")
	}
	if err := c.Settle(context.Background(), fact, actual, sharedUpstreamUnknown); err != nil {
		t.Fatal(err)
	}
	if authority.tokensUsed != 21 || authority.settleCount != 1 {
		t.Fatal("terminal replay double charged")
	}
	parsed.ErrorCode = "canceled"
	changed, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Settle(context.Background(), changed, actual, sharedUpstreamUnknown); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("changed terminal body replaced original: %v", err)
	}
}

func TestSharedAdmissionOwnerFenceAndFalseReadbackFailClosed(t *testing.T) {
	c, spool, authority, _ := sharedTestFixture(t)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	authority.owner.Fence++
	calls := 0
	if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), func(context.Context) error { calls++; return nil }); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("stale owner dispatch = %v", err)
	}
	if calls != 0 || authority.concurrency != 1 || authority.tokensHeld != 100 {
		t.Fatal("owner fence released/executed work")
	}
	authority.owner = c.intent.Owner
	spool.record.Intent.Policies[0].Revision = "tampered_without_digest"
	if err := c.Reconcile(context.Background()); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("false exact readback = %v", err)
	}
}

func TestSharedAdmissionBusyExecutorCannotCloseOrSettle(t *testing.T) {
	c, _, _, _ := sharedTestFixture(t)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(context.Background(), sharedTestDispatch(t, 1), func(context.Context) error {
		if err := c.CloseExecutor(); !errors.Is(err, errSharedExecutorBusy) {
			t.Fatalf("closed active executor: %v", err)
		}
		if err := c.Reconcile(context.Background()); !errors.Is(err, errSharedExecutorBusy) {
			t.Fatalf("reconciled active executor: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func sharedTestFaultSuffix(after bool) string {
	if after {
		return "_after_commit"
	}
	return "_before_commit"
}
