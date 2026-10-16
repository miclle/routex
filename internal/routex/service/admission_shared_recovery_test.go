package service

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

func TestSharedRecoveryDurableTerminalAfterRestart(t *testing.T) {
	for _, after := range []bool{false, true} {
		t.Run(sharedTestFaultSuffix(after), func(t *testing.T) {
			ctx := context.Background()
			c, _, authority, _ := sharedTestFixture(t)
			adapter, journal, path, options := durableSharedSpoolFixture(t, c)
			c.spool = adapter
			key := c.key()
			if err := c.Admit(ctx); err != nil {
				t.Fatal(err)
			}
			calls := 0
			if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { calls++; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := c.CloseExecutor(); err != nil {
				t.Fatal(err)
			}
			fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false)
			authority.fault = &sharedCommitFault{operation: "settle", after: after}
			if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
				t.Fatalf("interrupted settlement: %v", err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			// Recovery receives no live coordinator or executor assertion.
			c = nil
			reopened, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(key).Identity, options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			adapter, err = newDurableSharedCallSpool(reopened)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				state, err := recoverSharedAdmission(ctx, key, adapter, authority, time.Second)
				if err != nil || state != sharedRecoveryReady {
					t.Fatalf("recovery = %s, %v", state, err)
				}
			}
			entries, err := adapter.ReadDelivery(ctx, 2)
			if err != nil || len(entries) != 1 || !bytes.Equal(entries[0].Fact, fact) {
				t.Fatalf("recovered delivery = %+v, %v", entries, err)
			}
			if calls != 1 || authority.acceptCount != 1 || authority.settleCount != 1 || authority.rpm != 1 || authority.concurrency != 0 ||
				authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" || authority.receipt.Terminal.Actual.Tokens != nil || authority.receipt.Terminal.Actual.Money != nil {
				t.Fatalf("recovery repeated execution or lost unknown holds: %+v, calls=%d", authority, calls)
			}
		})
	}
}

func TestSharedRecoveryWithoutTerminalNeverClosesExecutor(t *testing.T) {
	for _, stage := range []string{"accepted", "uncertain_dispatch", "checkpoint"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			c, spool, authority, _ := sharedTestFixture(t)
			if err := c.Admit(ctx); err != nil {
				t.Fatal(err)
			}
			if stage != "accepted" {
				if stage == "uncertain_dispatch" {
					authority.fault = &sharedCommitFault{operation: "mark_dispatch", after: true}
				}
				err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { return nil })
				if stage == "uncertain_dispatch" && !errors.Is(err, errSharedInjected) || stage == "checkpoint" && err != nil {
					t.Fatal(err)
				}
			}
			if stage == "checkpoint" {
				authority.fault = &sharedCommitFault{operation: "checkpoint", after: false}
				fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, true)}, false)
				if err := c.Checkpoint(ctx, 1, fact, zeroQuotaSettlement(c.intent.Bound)); !errors.Is(err, errSharedInjected) {
					t.Fatal(err)
				}
			}
			key := c.key()
			beforeConcurrency, beforeTokens, beforeMoney := authority.concurrency, authority.tokensHeld, authority.moneyHeld
			state, err := recoverSharedAdmission(ctx, key, spool, authority, time.Second)
			if err != nil || state != sharedRecoveryPending || spool.record.Ready || spool.record.Terminal != nil || authority.settleCount != 0 ||
				authority.concurrency != beforeConcurrency || authority.tokensHeld != beforeTokens || authority.moneyHeld != beforeMoney || authority.acceptCount != 1 {
				t.Fatalf("pending recovery invented closure: state=%s, err=%v, authority=%+v", state, err, authority)
			}
			if stage == "uncertain_dispatch" && len(spool.record.Dispatches) != 1 || stage == "checkpoint" && len(authority.receipt.Checkpoints) != 1 {
				t.Fatal("immutable historical acknowledgement was not reconciled")
			}
		})
	}
}

type sharedRecoveryDispatchFaultSpool struct{ sharedCallSpool }

func (sharedRecoveryDispatchFaultSpool) ConfirmDispatch(context.Context, sharedDispatchReceipt) error {
	return errSharedInjected
}

func TestSharedRecoveryTerminalWithUnacknowledgedDispatch(t *testing.T) {
	ctx := context.Background()
	c, _, authority, _ := sharedTestFixture(t)
	adapter, journal, path, options := durableSharedSpoolFixture(t, c)
	c.spool = sharedRecoveryDispatchFaultSpool{adapter}
	key := c.key()
	if err := c.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error {
		t.Fatal("unacknowledged dispatch invoked transport")
		return nil
	}); !errors.Is(err, errSharedInjected) {
		t.Fatal(err)
	}
	if err := c.CloseExecutor(); err != nil {
		t.Fatal(err)
	}
	fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false)
	authority.fault = &sharedCommitFault{operation: "settle", after: false}
	if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	c = nil
	reopened, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(key).Identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	adapter, err = newDurableSharedCallSpool(reopened)
	if err != nil {
		t.Fatal(err)
	}
	before, err := adapter.Read(ctx, key)
	if err != nil || before.Terminal == nil || len(before.Dispatches) != 0 || len(authority.receipt.Dispatches) != 1 {
		t.Fatalf("original crash seam = %+v, %v", before, err)
	}
	// Preserve the real journal's post-terminal transition protection.
	if err := adapter.ConfirmDispatch(ctx, authority.receipt.Dispatches[0]); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("post-terminal dispatch acknowledgement = %v", err)
	}
	state, err := recoverSharedAdmission(ctx, key, adapter, authority, time.Second)
	if err != nil || state != sharedRecoveryReady {
		t.Fatalf("original terminal recovery = %s, %v", state, err)
	}
	after, err := adapter.Read(ctx, key)
	if err != nil || !after.Ready || len(after.Dispatches) != 0 || !sharedMatchesTerminal(before.Terminal, after.Terminal) || authority.settleCount != 1 || authority.tokensHeld != 100 {
		t.Fatalf("recovery changed immutable local history/holds: %+v, %v", after, err)
	}
}

func TestSharedRecoveryUnacknowledgedAcceptanceAndDispatch(t *testing.T) {
	for _, operation := range []string{"confirm_acceptance", "confirm_dispatch"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				ctx := context.Background()
				c, spool, authority, _ := sharedTestFixture(t)
				spool.fault = &sharedCommitFault{operation: "confirm_acceptance", after: false}
				if err := c.Admit(ctx); !errors.Is(err, errSharedInjected) {
					t.Fatal(err)
				}
				if operation == "confirm_dispatch" {
					if err := c.Admit(ctx); err != nil {
						t.Fatal(err)
					}
					authority.fault = &sharedCommitFault{operation: "mark_dispatch", after: true}
					if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error {
						t.Fatal("uncertain SQL dispatch invoked transport")
						return nil
					}); !errors.Is(err, errSharedInjected) {
						t.Fatal(err)
					}
				}
				spool.fault = &sharedCommitFault{operation: operation, after: after}
				if state, err := recoverSharedAdmission(ctx, c.key(), spool, authority, time.Second); state != "" || !errors.Is(err, errSharedInjected) {
					t.Fatalf("uncertain acknowledgement = %s, %v", state, err)
				}
				state, err := recoverSharedAdmission(ctx, c.key(), spool, authority, time.Second)
				if err != nil || state != sharedRecoveryPending || authority.acceptCount != 1 || authority.settleCount != 0 || authority.concurrency != 1 || spool.record.Terminal != nil || spool.record.Ready {
					t.Fatalf("historical acknowledgement gained executor authority: %s, %v", state, err)
				}
			})
		}
	}
}

func TestSharedRecoveryRetainsOrderedFallbackHistory(t *testing.T) {
	ctx := context.Background()
	c, _, authority, _ := sharedTestFixture(t)
	adapter, journal, path, options := durableSharedSpoolFixture(t, c)
	c.spool = adapter
	key := c.key()
	if err := c.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	calls := 0
	execute := func(context.Context) error { calls++; return nil }
	if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), execute); err != nil {
		t.Fatal(err)
	}
	first := sharedTestAttempt(1, true)
	if err := c.Checkpoint(ctx, 1, sharedTestPayload(t, []CallAttempt{first}, false), zeroQuotaSettlement(c.intent.Bound)); err != nil {
		t.Fatal(err)
	}
	if err := c.Dispatch(ctx, sharedTestDispatch(t, 2, first), execute); err != nil {
		t.Fatal(err)
	}
	if err := c.CloseExecutor(); err != nil {
		t.Fatal(err)
	}
	fact := sharedTestPayload(t, []CallAttempt{first, sharedTestAttempt(2, false)}, false)
	authority.fault = &sharedCommitFault{operation: "settle", after: false}
	if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
		t.Fatal(err)
	}
	before, err := adapter.Read(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	c = nil
	reopened, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(key).Identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	adapter, err = newDurableSharedCallSpool(reopened)
	if err != nil {
		t.Fatal(err)
	}
	state, err := recoverSharedAdmission(ctx, key, adapter, authority, time.Second)
	if err != nil || state != sharedRecoveryReady {
		t.Fatalf("fallback recovery = %s, %v", state, err)
	}
	after, err := adapter.Read(ctx, key)
	if err != nil || !after.Ready || !reflect.DeepEqual(before.Attempts, after.Attempts) || !reflect.DeepEqual(before.Checkpoints, after.Checkpoints) ||
		!bytes.Equal(after.Terminal.Fact, fact) || calls != 2 || authority.acceptCount != 1 || authority.rpm != 1 || authority.settleCount != 1 {
		t.Fatalf("fallback recovery changed prefix or repeated effects: %+v, %v", after, err)
	}
}

func TestSharedRecoveryMissingReceiptPreservesOriginal(t *testing.T) {
	c, spool, authority, _ := sharedTestFixture(t)
	if err := spool.Reserve(context.Background(), c.intent); err != nil {
		t.Fatal(err)
	}
	before := sharedTestCopy(spool.record)
	state, err := recoverSharedAdmission(context.Background(), c.key(), spool, authority, time.Second)
	if !errors.Is(err, eventqueue.ErrMissing) || state != "" || !reflect.DeepEqual(before, spool.record) || authority.acceptCount != 0 || authority.settleCount != 0 {
		t.Fatalf("missing receipt recreated/discarded intent: %s, %v", state, err)
	}
}

func TestSharedRecoveryUncertainHistoricalWritesAreReplayable(t *testing.T) {
	for _, operation := range []string{"confirm_acceptance", "checkpoint", "settle", "make_ready"} {
		for _, after := range []bool{false, true} {
			t.Run(operation+sharedTestFaultSuffix(after), func(t *testing.T) {
				ctx := context.Background()
				c, spool, authority, _ := sharedTestFixture(t)
				if err := c.Admit(ctx); err != nil {
					t.Fatal(err)
				}
				if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { return nil }); err != nil {
					t.Fatal(err)
				}
				fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, true)}, false)
				if err := c.Checkpoint(ctx, 1, fact, zeroQuotaSettlement(c.intent.Bound)); err != nil {
					t.Fatal(err)
				}
				// Only the local original checkpoint and terminal survive the
				// corresponding uncertain SQL writes in this fault fixture.
				authority.receipt.Checkpoints = nil
				if err := c.CloseExecutor(); err != nil {
					t.Fatal(err)
				}
				authority.fault = &sharedCommitFault{operation: "settle", after: false}
				zero := int64(0)
				if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{Tokens: &zero}, sharedUpstreamNoWork); !errors.Is(err, errSharedInjected) {
					t.Fatal(err)
				}
				original := sharedTestCopy(spool.record.Terminal)
				fault := &sharedCommitFault{operation: operation, after: after}
				spool.fault, authority.fault = fault, fault
				state, err := recoverSharedAdmission(ctx, c.key(), spool, authority, time.Second)
				if !errors.Is(err, errSharedInjected) || state != "" || !reflect.DeepEqual(original, spool.record.Terminal) {
					t.Fatalf("uncertain recovery changed original terminal: %s, %v", state, err)
				}
				state, err = recoverSharedAdmission(ctx, c.key(), spool, authority, time.Second)
				if err != nil || state != sharedRecoveryReady || !spool.record.Ready || authority.settleCount != 1 || authority.acceptCount != 1 {
					t.Fatalf("exact recovery retry = %s, %v, %+v", state, err, authority)
				}
			})
		}
	}
}

type sharedRecoveryReadFault struct {
	sharedRecoveryAuthority
	alter func(*sharedAuthorityReceipt)
}

func (a sharedRecoveryReadFault) Read(ctx context.Context, key sharedAdmissionKey) (*sharedAuthorityReceipt, error) {
	receipt, err := a.sharedRecoveryAuthority.Read(ctx, key)
	if err == nil {
		a.alter(receipt)
	}
	return receipt, err
}

func TestSharedRecoveryRejectsInconsistentHistoryBeforeWrites(t *testing.T) {
	for name, alter := range map[string]func(*sharedAuthorityReceipt){
		"key":        func(r *sharedAuthorityReceipt) { r.Key.ExecutionID = "another_execution" },
		"receipt":    func(r *sharedAuthorityReceipt) { r.ReceiptID = "another_receipt" },
		"dispatch":   func(r *sharedAuthorityReceipt) { r.Dispatches[0].AttemptDigest = "changed" },
		"sequence":   func(r *sharedAuthorityReceipt) { r.Dispatches[0].Number++ },
		"missing":    func(r *sharedAuthorityReceipt) { r.Dispatches = nil },
		"extra":      func(r *sharedAuthorityReceipt) { r.Dispatches = append(r.Dispatches, r.Dispatches[0]) },
		"checkpoint": func(r *sharedAuthorityReceipt) { r.Checkpoints = []sharedAttemptCheckpoint{{Number: 1}} },
		"terminal":   func(r *sharedAuthorityReceipt) { r.Terminal = &sharedTerminalIntent{Key: r.Key, ExecutorClosed: true} },
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			c, spool, authority, _ := sharedTestFixture(t)
			if err := c.Admit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { return nil }); err != nil {
				t.Fatal(err)
			}
			before := sharedTestCopy(spool.record)
			fault := &sharedCommitFault{operation: "confirm_acceptance", after: false}
			spool.fault = fault
			state, err := recoverSharedAdmission(ctx, c.key(), spool, sharedRecoveryReadFault{authority, alter}, time.Second)
			if !errors.Is(err, errSharedAdmissionConflict) || state != "" || fault.used || !reflect.DeepEqual(before, spool.record) || authority.settleCount != 0 {
				t.Fatalf("inconsistent history performed a write: %s, %v", state, err)
			}
		})
	}
}

func TestSharedRecoveryCancellationAndBounds(t *testing.T) {
	c, spool, authority, _ := sharedTestFixture(t)
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	before := sharedTestCopy(spool.record)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if state, err := recoverSharedAdmission(ctx, c.key(), spool, authority, time.Second); state != "" || !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, spool.record) {
		t.Fatalf("cancelled recovery = %s, %v", state, err)
	}
	for _, timeout := range []time.Duration{-time.Second, 0, 31 * time.Second} {
		if _, err := recoverSharedAdmission(context.Background(), c.key(), spool, authority, timeout); !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatalf("invalid timeout %s = %v", timeout, err)
		}
	}
	//lint:ignore SA1012 This deliberately verifies rejection of a nil context.
	//nolint:staticcheck // Intentionally exercise nil-context rejection.
	if _, err := recoverSharedAdmission(nil, c.key(), spool, authority, time.Second); !errors.Is(err, errSharedAdmissionInvalid) {
		t.Fatal(err)
	}
}
