package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
)

func durableSharedSpoolFixture(t *testing.T, c *sharedAdmissionCoordinator) (*durableSharedCallSpool, *eventqueue.SharedSpool, string, eventqueue.SharedSpoolOptions) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared.db")
	options := eventqueue.SharedSpoolOptions{Capacity: 2, MaxDataBytes: eventqueue.SharedSpoolMaxDataBytes, DeliveredCapacity: 4}
	journal, err := eventqueue.OpenSharedSpool(context.Background(), path, sharedJournalKey(c.key()).Identity, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	adapter, err := newDurableSharedCallSpool(journal)
	if err != nil {
		t.Fatal(err)
	}
	return adapter, journal, path, options
}

func durableSharedSpoolDigest(t *testing.T, value any) string {
	t.Helper()
	digest, err := sharedDigest(value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestDurableSharedSpoolCoordinatorTerminalRestartAndDeliveryPreserveUnknownEconomics(t *testing.T) {
	c, _, authority, _ := sharedTestFixture(t)
	adapter, journal, path, options := durableSharedSpoolFixture(t, c)
	c.spool = adapter
	ctx := context.Background()
	if err := c.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	calls := 0
	if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	fact := sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false)
	if err := c.CloseExecutor(); err != nil {
		t.Fatal(err)
	}
	authority.fault = &sharedCommitFault{operation: "settle", after: true}
	if err := c.Settle(ctx, fact, eventqueue.QuotaSettlement{}, sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) {
		t.Fatalf("uncertain SQL settlement = %v", err)
	}
	before, err := adapter.Read(ctx, c.key())
	if err != nil || before.Terminal == nil || before.Ready {
		t.Fatalf("uncertain local state = %+v %v", before, err)
	}
	if entries, err := adapter.ReadDelivery(ctx, 2); err != nil || len(entries) != 0 {
		t.Fatal("unconfirmed terminal was deliverable")
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(c.key()).Identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	adapter, err = newDurableSharedCallSpool(reopened)
	if err != nil {
		t.Fatal(err)
	}
	c.spool = adapter
	after, err := adapter.Read(ctx, c.key())
	if err != nil || after.Ready || !sharedMatchesTerminal(after.Terminal, before.Terminal) {
		t.Fatalf("reopen changed original terminal evidence: %+v %v", after, err)
	}
	// This is storage-only completion by the already closed original executor;
	// no restart reconstructs an inference permit or triggers another transport.
	if err := c.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err := adapter.ReadDelivery(ctx, 2)
	if err != nil || len(entries) != 1 || !bytes.Equal(entries[0].Fact, fact) {
		t.Fatalf("confirmed final delivery = %+v %v", entries, err)
	}
	entries[0].Fact[0] = '!'
	entries, err = adapter.ReadDelivery(ctx, 2)
	if err != nil || !bytes.Equal(entries[0].Fact, fact) {
		t.Fatal("delivery exposed mutable stored fact bytes")
	}
	if err := adapter.AckDelivery(ctx, entries[0].Receipt); err != nil {
		t.Fatal(err)
	}
	if err := adapter.AckDelivery(ctx, entries[0].Receipt); err != nil {
		t.Fatal(err)
	}
	if pending, err := adapter.ReadDelivery(ctx, 2); err != nil || len(pending) != 0 {
		t.Fatal("acknowledged fact remained ready")
	}
	receipt, err := authority.Read(ctx, c.key())
	if err != nil || receipt.Terminal == nil || receipt.Terminal.Actual.Tokens != nil || receipt.Terminal.Actual.Money != nil ||
		authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" || authority.concurrency != 0 || authority.settleCount != 1 || calls != 1 {
		t.Fatalf("delivery/reopen changed economics or repeated inference: receipt=%+v authority=%+v calls=%d err=%v", receipt, authority, calls, err)
	}
	if err := adapter.Reserve(ctx, c.intent); !errors.Is(err, eventqueue.ErrSharedSpoolDelivered) {
		t.Fatalf("delivered admission was recreated = %v", err)
	}
}

func TestDurableSharedSpoolUncertainDispatchReopenCannotAdoptOrExecute(t *testing.T) {
	c, _, authority, request := sharedTestFixture(t)
	adapter, journal, path, options := durableSharedSpoolFixture(t, c)
	c.spool = adapter
	ctx := context.Background()
	if err := c.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	authority.fault = &sharedCommitFault{operation: "mark_dispatch", after: true}
	calls := 0
	if err := c.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { calls++; return nil }); !errors.Is(err, errSharedInjected) {
		t.Fatal(err)
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(c.key()).Identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	adapter, err = newDurableSharedCallSpool(reopened)
	if err != nil {
		t.Fatal(err)
	}
	local, err := adapter.Read(ctx, c.key())
	if err != nil || len(local.Attempts) != 1 || local.Ready || local.Terminal != nil {
		t.Fatalf("uncertain dispatch record = %+v %v", local, err)
	}
	owner := c.intent.Owner
	owner.IncarnationID, owner.Fence = "incarnation_restarted", owner.Fence+1
	authority.owner = owner
	restarted, err := newSharedAdmissionCoordinator(owner, request, adapter, authority, sharedCoordinatorOptions{OperationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.Admit(ctx); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("restarted executor adopted old intent = %v", err)
	}
	if err := restarted.Dispatch(ctx, sharedTestDispatch(t, 1), func(context.Context) error { calls++; return nil }); !errors.Is(err, errSharedDispatchForbidden) {
		t.Fatal(err)
	}
	if calls != 0 || authority.tokensHeld != 100 || authority.moneyHeld != "0.000000000000000003" || authority.concurrency != 1 {
		t.Fatal("reopen/fence implicitly executed or settled old work")
	}
}

func TestDurableSharedSpoolRejectsAlteredEvidenceAndUnconfirmedReadiness(t *testing.T) {
	c, _, _, _ := sharedTestFixture(t)
	adapter, _, _, _ := durableSharedSpoolFixture(t, c)
	c.spool = adapter
	ctx := context.Background()
	if err := c.Admit(ctx); err != nil {
		t.Fatal(err)
	}
	attempt := sharedTestDispatch(t, 1)
	attempt.Key = c.key()
	attempt.Digest = durableSharedSpoolDigest(t, attempt)
	wrong := sharedTestCopy(attempt)
	wrong.CredentialID = "crd_wrong"
	wrong.Digest = durableSharedSpoolDigest(t, wrong)
	if err := adapter.PrepareAttempt(ctx, wrong); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("altered Credential evidence = %v", err)
	}
	wrong = sharedTestCopy(attempt)
	wrong.Digest = "incorrect_digest"
	if err := adapter.PrepareAttempt(ctx, wrong); !errors.Is(err, errSharedAdmissionInvalid) {
		t.Fatalf("invalid digest = %v", err)
	}
	local, err := adapter.Read(ctx, c.key())
	if err != nil || len(local.Attempts) != 0 {
		t.Fatal("invalid evidence partially committed")
	}
	if err := adapter.PrepareAttempt(ctx, attempt); err != nil {
		t.Fatal(err)
	}
	terminal := sharedTerminalIntent{Key: c.key(), LastAttempt: 1, Fact: sharedTestPayload(t, []CallAttempt{sharedTestAttempt(1, false)}, false), ExecutorClosed: true, Upstream: sharedUpstreamUnknown}
	terminal.Digest = durableSharedSpoolDigest(t, terminal)
	if err := adapter.MakeReady(ctx, terminal, "receipt_shared"); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("uncaptured terminal made ready = %v", err)
	}
	if err := adapter.PrepareTerminal(ctx, terminal); err != nil {
		t.Fatal(err)
	}
	if err := adapter.MakeReady(ctx, terminal, "receipt_wrong"); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("wrong authority identity made ready = %v", err)
	}
	zero := int64(0)
	wrongTerminal := sharedTestCopy(terminal)
	wrongTerminal.Actual.Tokens = &zero
	wrongTerminal.Digest = durableSharedSpoolDigest(t, wrongTerminal)
	if err := adapter.PrepareTerminal(ctx, wrongTerminal); !errors.Is(err, errSharedAdmissionConflict) {
		t.Fatalf("unknown work became zero = %v", err)
	}
	if ready, err := adapter.ReadDelivery(ctx, 2); err != nil || len(ready) != 0 {
		t.Fatal("invalid receipt/economics made a fact ready")
	}
}

func TestDurableSharedSpoolCanceledAndExpiredOperationsLeaveFileUnchanged(t *testing.T) {
	c, _, _, _ := sharedTestFixture(t)
	adapter, _, path, _ := durableSharedSpoolFixture(t, c)
	c.spool = adapter
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	attempt := sharedTestDispatch(t, 1)
	attempt.Key = c.key()
	attempt.Digest = durableSharedSpoolDigest(t, attempt)
	terminal := sharedTerminalIntent{Key: c.key(), LastAttempt: 1, Fact: attempt.Fallback, ExecutorClosed: true, Upstream: sharedUpstreamUnknown}
	terminal.Digest = durableSharedSpoolDigest(t, terminal)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	operations := []func(context.Context) error{
		func(ctx context.Context) error { return adapter.Reserve(ctx, c.intent) },
		func(ctx context.Context) error { _, err := adapter.Read(ctx, c.key()); return err },
		func(ctx context.Context) error { return adapter.ConfirmAcceptance(ctx, c.key(), "receipt_shared") },
		func(ctx context.Context) error { return adapter.PrepareAttempt(ctx, attempt) },
		func(ctx context.Context) error {
			return adapter.ConfirmDispatch(ctx, sharedDispatchReceipt{Key: c.key(), Number: 1, AttemptDigest: attempt.Digest})
		},
		func(ctx context.Context) error {
			return adapter.RecordAttempt(ctx, sharedAttemptCheckpoint{Key: c.key(), Number: 1, AttemptDigest: attempt.Digest})
		},
		func(ctx context.Context) error { return adapter.PrepareTerminal(ctx, terminal) },
		func(ctx context.Context) error { return adapter.MakeReady(ctx, terminal, "receipt_shared") },
		func(ctx context.Context) error { _, err := adapter.ReadDelivery(ctx, 2); return err },
		func(ctx context.Context) error {
			return adapter.AckDelivery(ctx, eventqueue.SharedSpoolTerminalReceipt{Key: sharedJournalKey(c.key()), ReceiptID: "receipt_shared", TerminalDigest: terminal.Digest})
		},
	}
	for _, expired := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if expired {
			ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			defer cancel()
		}
		for index, operation := range operations {
			if err := operation(ctx); !errors.Is(err, ctx.Err()) {
				t.Fatalf("operation %d ignored context: %v", index, err)
			}
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("canceled/expired adapter call modified durable bytes")
	}
}

func TestDurableSharedSpoolRejectsInvalidCapturedAuthorizationWithoutMutation(t *testing.T) {
	c, _, _, _ := sharedTestFixture(t)
	adapter, _, path, _ := durableSharedSpoolFixture(t, c)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*sharedCapturedAuthorization)
	}{
		{"unknown_version", func(a *sharedCapturedAuthorization) { a.Version = 2 }},
		{"missing_user", func(a *sharedCapturedAuthorization) { a.User = nil }},
		{"foreign_user", func(a *sharedCapturedAuthorization) { a.User.ID = "usr_other" }},
		{"foreign_key", func(a *sharedCapturedAuthorization) { a.Key.ID = "key_other" }},
		{"foreign_model", func(a *sharedCapturedAuthorization) { a.Model.ID = "mdl_other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrong := sharedTestCopy(c.intent)
			tc.change(&wrong.Authorization)
			// Recompute the correct digest: semantic scope validation must reject
			// this body even when its bytes and digest agree.
			wrong.Digest = durableSharedSpoolDigest(t, wrong)
			if err := adapter.Reserve(context.Background(), wrong); !errors.Is(err, errSharedAdmissionInvalid) {
				t.Fatalf("invalid authorization reserved: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected authorization modified spool bytes")
			}
		})
	}
	if err := adapter.Reserve(context.Background(), c.intent); err != nil {
		t.Fatal(err)
	}
	stored, err := adapter.Read(context.Background(), c.key())
	if err != nil || !reflect.DeepEqual(stored.Intent.Authorization, c.intent.Authorization) {
		t.Fatalf("valid original authorization not retained: %v", err)
	}
}

func TestDurableSharedSpoolFourPrincipalAuthorizationSurvivesReopen(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			original, _, _, request := sharedTestFixture(t)
			proof, err := newSharedCapturedAuthorization(sharedAuthorizationTestFacts(kind), sharedAuthorizationTestTime())
			if err != nil {
				t.Fatal(err)
			}
			var fact CallFact
			if err := json.Unmarshal(request.Fallback, &fact); err != nil {
				t.Fatal(err)
			}
			fact.UserID, fact.KeyID, fact.ProjectID, fact.TeamID, fact.TeamMembershipID = "", "", "", "", ""
			if proof.User != nil {
				fact.UserID = proof.User.ID
			}
			if proof.Key != nil {
				fact.KeyID = proof.Key.ID
			}
			if proof.Project != nil {
				fact.ProjectID = proof.Project.ID
			}
			if proof.Team != nil {
				fact.TeamID, fact.TeamMembershipID = proof.Team.ID, proof.Team.MembershipID
			}
			request.Authorization = proof
			request.Reservation.Model = GatewayReservationIdentity{proof.Model.ID, proof.Model.CreatedAt}
			request.Fallback, err = json.Marshal(fact)
			if err != nil {
				t.Fatal(err)
			}
			c, err := newSharedAdmissionCoordinator(original.intent.Owner, request, original.spool, original.authority, sharedCoordinatorOptions{OperationTimeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			adapter, journal, path, options := durableSharedSpoolFixture(t, c)
			if err := adapter.Reserve(context.Background(), c.intent); err != nil {
				t.Fatal(err)
			}
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := eventqueue.OpenSharedSpool(context.Background(), path, sharedJournalKey(c.key()).Identity, options)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()
			adapter, err = newDurableSharedCallSpool(reopened)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := adapter.Read(context.Background(), c.key())
			if err != nil || !reflect.DeepEqual(stored.Intent.Authorization, proof) || stored.ReceiptID != "" || stored.Ready || len(stored.Attempts) != 0 {
				t.Fatalf("reopen altered original proof or invented authorization: %+v %v", stored, err)
			}
		})
	}
}
