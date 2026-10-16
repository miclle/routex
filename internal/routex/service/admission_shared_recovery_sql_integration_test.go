package service

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
)

// Faults wrap the production SQL transaction. An after fault loses only its
// acknowledgement; a before fault never calls the SQL settlement operation.
type sharedRecoverySQLSettlementFault struct {
	sharedEconomicAuthority
	after bool
	used  bool
}

func (s *sharedRecoverySQLSettlementFault) Settle(ctx context.Context, terminal sharedTerminalIntent) (*sharedAuthorityReceipt, error) {
	if s.used {
		return s.sharedEconomicAuthority.Settle(ctx, terminal)
	}
	s.used = true
	if s.after {
		if _, err := s.sharedEconomicAuthority.Settle(ctx, terminal); err != nil {
			return nil, err
		}
	}
	return nil, errSharedInjected
}

// Called only by the exclusively owned real SQL Accept worker after its actual
// authenticated runtime fixture and seven test-only tables have been prepared.
func sharedRecoverySQLIntegration(t *testing.T, ctx context.Context, db *gorm.DB, fixture func(*testing.T, string, map[string]any) sharedEconomicAcceptSQLFixture) {
	for _, test := range []struct {
		name                  string
		missingDispatchAck    bool
		afterSettlementCommit bool
		revokeKey             bool
		fallback              bool
		pending               bool
		missingSQLReceipt     bool
	}{
		{name: "terminal_missing_dispatch_ack_no_transport", missingDispatchAck: true},
		{name: "terminal_uncertain_sql_settlement_before_commit"},
		{name: "terminal_uncertain_sql_settlement_after_commit", afterSettlementCommit: true},
		{name: "revoked_key_historical_only_recovery", revokeKey: true},
		{name: "ordered_fallback_prefix_after_restart", fallback: true},
		{name: "pending_without_terminal_retains_executor_and_unknown_holds", pending: true},
		{name: "missing_sql_receipt_preserves_local_reservation", missingSQLReceipt: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := fixture(t, "personal_key", nil)
			path := filepath.Join(t.TempDir(), "shared-recovery.db")
			options := eventqueue.SharedSpoolOptions{Capacity: 2, MaxDataBytes: eventqueue.SharedSpoolMaxDataBytes, DeliveredCapacity: 4}
			journal, err := eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(sharedIntentKey(f.intent)).Identity, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = journal.Close() })
			adapter, err := newDurableSharedCallSpool(journal)
			if err != nil {
				t.Fatal(err)
			}
			fault := &sharedRecoverySQLSettlementFault{sharedEconomicAuthority: f.store, after: test.afterSettlementCommit}
			var originalSpool sharedCallSpool = adapter
			if test.missingDispatchAck {
				originalSpool = sharedRecoveryDispatchFaultSpool{adapter}
			}
			coordinator, err := newSharedAdmissionCoordinator(f.intent.Owner, f.intent.sharedAdmissionRequest, originalSpool, fault, sharedCoordinatorOptions{OperationTimeout: 5 * time.Second})
			if err != nil {
				t.Fatal("coherent real request coordinator", err)
			}
			f.intent = sharedTestCopy(coordinator.intent)
			key := coordinator.key()
			calls := 0
			if test.missingSQLReceipt {
				if err := adapter.Reserve(ctx, f.intent); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := coordinator.Admit(ctx); err != nil {
					t.Fatal("actual SQL acceptance", err)
				}
				f.requireAdmission(t, db)
				first := f.attempt(t, 1, nil)
				dispatchErr := coordinator.Dispatch(ctx, first, func(context.Context) error { calls++; return nil })
				if test.missingDispatchAck {
					if !errors.Is(dispatchErr, errSharedInjected) || calls != 0 {
						t.Fatal("unacknowledged dispatch invoked transport", dispatchErr, calls)
					}
				} else if dispatchErr != nil {
					t.Fatal("actual SQL dispatch barrier", dispatchErr)
				}
				last := first
				if test.fallback {
					fact, err := sharedCallPayload(key.RequestID, first.Fallback)
					if err != nil {
						t.Fatal(err)
					}
					fact.Attempts[0].WorkEvidence, fact.Attempts[0].FailureClass = "not_sent", "connection_failure"
					if err := coordinator.Checkpoint(ctx, 1, sharedEconomicHistorySQLBody(t, fact), sharedCheckpointRecovery(fact, f.intent.Bound)); err != nil {
						t.Fatal("exact no-work checkpoint", err)
					}
					last = f.attempt(t, 2, fact.Attempts)
					if err := coordinator.Dispatch(ctx, last, func(context.Context) error { calls++; return nil }); err != nil {
						t.Fatal("ordered second original dispatch", err)
					}
				}
				if !test.pending {
					fact, err := sharedCallPayload(key.RequestID, last.Fallback)
					if err != nil {
						t.Fatal(err)
					}
					if err := coordinator.CloseExecutor(); err != nil {
						t.Fatal(err)
					}
					beforeSettlement := sharedEconomicAcceptSQLSnapshot(t, db)
					if err := coordinator.Settle(ctx, last.Fallback, quotaCallSettlement(fact), sharedUpstreamUnknown); !errors.Is(err, errSharedInjected) || !fault.used {
						t.Fatal("production SQL settlement crash seam", err)
					}
					retained, err := f.store.Read(ctx, key)
					if err != nil || retained == nil || (retained.Terminal != nil) != test.afterSettlementCommit ||
						test.afterSettlementCommit && !sharedMatchesTerminal(retained.Terminal, coordinator.terminal) {
						t.Fatal("actual SQL state did not match settlement fault seam", err)
					}
					if !test.afterSettlementCommit {
						sharedEconomicAcceptSQLRequireSnapshot(t, db, beforeSettlement)
					}
				}
			}
			before, err := adapter.Read(ctx, key)
			if err != nil || before.Ready {
				t.Fatal("original durable pending record", err)
			}
			if !test.pending && !test.missingSQLReceipt && before.Terminal == nil || test.pending && before.Terminal != nil ||
				test.missingDispatchAck && len(before.Dispatches) != 0 {
				t.Fatal("durable original does not match the selected crash seam")
			}
			if entries, err := adapter.ReadDelivery(ctx, 2); err != nil || len(entries) != 0 {
				t.Fatal("unconfirmed local terminal became deliverable", err)
			}
			sqlBefore := sharedEconomicAcceptSQLSnapshot(t, db)
			if err := journal.Close(); err != nil {
				t.Fatal(err)
			}
			if test.revokeKey {
				sharedEconomicHistorySQLChanged(t, db.Model(&entity.APIKey{}).Where("id = ?", f.intent.Authorization.Key.ID).Update("status", entity.KeyRevoked))
			}
			// No original coordinator or live authority enters restart recovery.
			coordinator, fault, f.store = nil, nil, nil
			historical := &sharedEconomicSQLStore{db: db}
			journal, err = eventqueue.OpenSharedSpool(ctx, path, sharedJournalKey(key).Identity, options)
			if err != nil {
				t.Fatal("real file reopen", err)
			}
			adapter, err = newDurableSharedCallSpool(journal)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := adapter.Read(ctx, key)
			if err != nil || !reflect.DeepEqual(before, reopened) {
				t.Fatal("reopen rewrote immutable local originals", err)
			}
			state, recoverErr := recoverSharedAdmission(ctx, key, adapter, historical, 5*time.Second)
			if test.missingSQLReceipt {
				if !errors.Is(recoverErr, eventqueue.ErrMissing) || state != "" {
					t.Fatal("missing SQL acceptance was recreated", state, recoverErr)
				}
				sharedEconomicAcceptSQLRequireSnapshot(t, db, sqlBefore)
				after, err := adapter.Read(ctx, key)
				if err != nil || !reflect.DeepEqual(before, after) || calls != 0 {
					t.Fatal("missing history changed local reservation", err)
				}
				return
			}
			if test.pending {
				if recoverErr != nil || state != sharedRecoveryPending {
					t.Fatal("nonterminal restart recovery", state, recoverErr)
				}
				sharedEconomicAcceptSQLRequireSnapshot(t, db, sqlBefore)
				f.requireAdmission(t, db)
			} else {
				if recoverErr != nil || state != sharedRecoveryReady {
					t.Fatal("historical-only terminal recovery", state, recoverErr)
				}
				if test.afterSettlementCommit {
					sharedEconomicAcceptSQLRequireSnapshot(t, db, sqlBefore)
				}
				sharedRecoverySQLRequireTerminal(t, db, before)
			}
			after, err := adapter.Read(ctx, key)
			if err != nil || !reflect.DeepEqual(before.Intent, after.Intent) || !reflect.DeepEqual(before.Attempts, after.Attempts) ||
				!reflect.DeepEqual(before.Dispatches, after.Dispatches) || !reflect.DeepEqual(before.Checkpoints, after.Checkpoints) ||
				!reflect.DeepEqual(before.Terminal, after.Terminal) || after.Ready != !test.pending {
				t.Fatal("recovery changed original local history", err)
			}
			entries, err := adapter.ReadDelivery(ctx, 2)
			if err != nil || test.pending && len(entries) != 0 || !test.pending && (len(entries) != 1 || !bytes.Equal(entries[0].Fact, before.Terminal.Fact)) {
				t.Fatal("restart delivery did not preserve confirmed original fact", err)
			}
			if test.missingDispatchAck && (calls != 0 || len(after.Dispatches) != 0) || test.fallback && calls != 2 ||
				!test.missingDispatchAck && !test.fallback && calls != 1 {
				t.Fatal("recovery repeated transport or fabricated local dispatch acknowledgement", calls)
			}
			settled := sharedEconomicAcceptSQLSnapshot(t, db)
			for range 2 {
				replayed, err := recoverSharedAdmission(ctx, key, adapter, historical, 5*time.Second)
				if err != nil || replayed != state {
					t.Fatal("exact restart recovery replay", replayed, err)
				}
				sharedEconomicAcceptSQLRequireSnapshot(t, db, settled)
				current, err := adapter.Read(ctx, key)
				if err != nil || !reflect.DeepEqual(after, current) {
					t.Fatal("idempotent readiness changed local bytes", err)
				}
			}
		})
	}
}

func sharedRecoverySQLRequireTerminal(t *testing.T, db *gorm.DB, original *sharedSpoolRecord) {
	t.Helper()
	if original.Terminal == nil || original.Terminal.Actual.Tokens != nil || original.Terminal.Actual.Money != nil ||
		original.Intent.Bound.Tokens == nil || original.Intent.Bound.Money == nil {
		t.Fatal("fixture must retain finite maximums with independently unknown terminal usage")
	}
	key := sharedIntentKey(original.Intent)
	var receipt entity.AdmissionEconomicReceipt
	if err := db.Where("installation_id = ? AND request_digest = ?", key.Owner.InstallationID, sharedEconomicIdentityDigest(key.RequestID)).Take(&receipt).Error; err != nil ||
		!bytes.Equal(receipt.IntentBody, sharedEconomicHistorySQLBody(t, original.Intent)) ||
		!bytes.Equal(receipt.TerminalBody, sharedEconomicHistorySQLBody(t, *original.Terminal)) {
		t.Fatal("SQL rewrote original admission or terminal", err)
	}
	var attempts []entity.AdmissionEconomicAttempt
	if err := db.Where("installation_id = ? AND request_digest = ?", key.Owner.InstallationID, receipt.RequestDigest).Order("number").Find(&attempts).Error; err != nil || len(attempts) != len(original.Attempts) {
		t.Fatal("SQL changed ordered attempt census", err)
	}
	for i, row := range attempts {
		if !bytes.Equal(row.DispatchBody, sharedEconomicHistorySQLBody(t, original.Attempts[i])) {
			t.Fatal("SQL changed original selected attempt bytes")
		}
		if i < len(original.Checkpoints) {
			if row.CheckpointDigest == nil || *row.CheckpointDigest != original.Checkpoints[i].Digest ||
				!bytes.Equal(row.CheckpointBody, sharedEconomicHistorySQLBody(t, original.Checkpoints[i])) {
				t.Fatal("SQL changed original ordered checkpoint bytes")
			}
		} else if row.CheckpointDigest != nil || row.CheckpointBody != nil {
			t.Fatal("SQL fabricated an attempt checkpoint")
		}
	}
	want, err := eventqueue.QuotaCounterDelta(eventqueue.QuotaCounter{MoneyHeld: map[string]string{}, MoneyUsed: map[string]string{}},
		eventqueue.QuotaCounterContribution{Bound: original.Intent.Bound, Actual: original.Terminal.Actual, Settled: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range original.Intent.Policies {
		var account entity.AdmissionEconomicAccount
		if err := db.Where("installation_id = ? AND account_digest = ?", key.Owner.InstallationID, sharedEconomicIdentityDigest(policy.Account)).Take(&account).Error; err != nil || account.ExecutorCount != 0 || account.RPMCount != 1 {
			t.Fatal("terminal failed to release only the original executor", err)
		}
		var active int64
		if err := db.Model(&entity.AdmissionEconomicContribution{}).Where("installation_id = ? AND account_digest = ? AND request_digest = ? AND kind IN ?", key.Owner.InstallationID, account.AccountDigest, receipt.RequestDigest, []string{"active_quota", "executor"}).Count(&active).Error; err != nil || active != 0 {
			t.Fatal("terminal retained duplicate active or executor projections", err)
		}
		var contributions []entity.AdmissionEconomicContribution
		if err := db.Where("installation_id = ? AND account_digest = ? AND request_digest = ? AND kind = ?", key.Owner.InstallationID, account.AccountDigest, receipt.RequestDigest, "terminal_quota").Find(&contributions).Error; err != nil || len(contributions) == 0 {
			t.Fatal("unknown terminal lost quota contributions", err)
		}
		for _, contribution := range contributions {
			var counter entity.AdmissionEconomicCounter
			if err := db.Where("installation_id = ? AND account_digest = ? AND window_name = ?", key.Owner.InstallationID, account.AccountDigest, contribution.Window).Take(&counter).Error; err != nil {
				t.Fatal(err)
			}
			var actual eventqueue.QuotaCounter
			if !sharedEconomicDecodeBody(counter.CounterBody, &actual) || !reflect.DeepEqual(actual, want) {
				t.Fatal("unknown usage lost its original conservative maximum")
			}
		}
	}
}
