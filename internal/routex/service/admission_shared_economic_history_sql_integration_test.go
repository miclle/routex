package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This opt-in, single-driver worker owns a fresh disposable database. It tests
// dormant history operations, not Accept, economic cutover or migration DDL.
func TestSharedEconomicHistorySQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_HISTORY_SQL") != "1" {
		t.Skip("dedicated shared economic history SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_HISTORY_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("history worker requires a fresh, exclusively owned schema")
	}
	for _, row := range []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicReceipt{}, &entity.AdmissionEconomicAttempt{}} {
		if db.Migrator().HasTable(row) {
			t.Fatal("history worker requires absent economic tables")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	// Explicitly bounded test-only DDL. The separate schema worker qualifies the
	// frozen economic migration; this fixture makes no migration acceptance claim.
	for _, row := range []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicReceipt{}, &entity.AdmissionEconomicAttempt{}} {
		if db.Migrator().HasTable(row) {
			t.Fatal("predecessor unexpectedly created economic tables")
		}
		if err := db.Migrator().CreateTable(row); err != nil {
			t.Fatal("test-only history table", err)
		}
	}
	owner := sharedEconomicHistorySQLOwner(t, db)
	domain := entity.AdmissionEconomicDomain{InstallationID: owner.Owner.InstallationID, FormatVersion: 1,
		Initialized: true, CoverageStart: 1, ClockHighWater: 9000000000000000000, ReceiptCapacity: 1000}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatal("explicit test-only initialized domain", err)
	}
	store := &sharedEconomicSQLStore{db: db}
	ordinal := 0
	fixture := func(t *testing.T, count int) sharedEconomicHistorySQLFixture {
		t.Helper()
		ordinal++
		return newSharedEconomicHistorySQLFixture(t, db, owner, ordinal, count, nil)
	}

	t.Run("identical_concurrent_checkpoint_and_return_copy", func(t *testing.T) {
		f := fixture(t, 2)
		results := sharedEconomicHistorySQLConcurrent(t, ctx, store, f.checkpoints[1], f.checkpoints[1])
		if results[0] != nil || results[1] != nil {
			t.Fatal("identical concurrent checkpoint", results)
		}
		f.requireCheckpoint(t, db, f.checkpoints[1])
		first, err := store.Read(ctx, f.key)
		if err != nil || first == nil || len(first.Dispatches) != 2 || len(first.Checkpoints) != 2 || first.Terminal != nil {
			t.Fatal("retained dispatch/checkpoint", err)
		}
		first.Dispatches[0].AttemptDigest = "changed"
		first.Checkpoints[0].Fallback[0] = '!'
		if first.Checkpoints[0].Recovery.Tokens == nil || first.Checkpoints[0].Recovery.Money == nil {
			t.Fatal("prior no-work checkpoint must retain explicit zero recovery")
		}
		*first.Checkpoints[0].Recovery.Tokens = 999
		*first.Checkpoints[0].Recovery.Money = "999"
		again, err := store.Read(ctx, f.key)
		if err != nil || !reflect.DeepEqual(again.Checkpoints, f.checkpoints) || again.Dispatches[0].AttemptDigest != f.attempts[0].AttemptDigest {
			t.Fatal("returned history aliases persisted bytes", err)
		}
		f.requireOriginal(t, db)
	})

	t.Run("conflicting_concurrent_checkpoint_is_once_only", func(t *testing.T) {
		f := fixture(t, 1)
		changed := f.checkpoints[0]
		var fact CallFact
		if err := json.Unmarshal(changed.Fallback, &fact); err != nil {
			t.Fatal(err)
		}
		fact.ErrorCode = "different_observed_failure"
		changed.Fallback = sharedEconomicHistorySQLBody(t, fact)
		changed.Digest = durableSharedSpoolDigest(t, changed)
		results := sharedEconomicHistorySQLConcurrent(t, ctx, store, f.checkpoints[0], changed)
		winner := -1
		for i, err := range results {
			if err == nil {
				if winner != -1 {
					t.Fatal("two different checkpoint bodies committed")
				}
				winner = i
			} else if !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("conflicting checkpoint did not report conflict", err)
			}
		}
		if winner == -1 {
			t.Fatal("neither concurrent checkpoint committed")
		}
		values := []sharedAttemptCheckpoint{f.checkpoints[0], changed}
		f.requireCheckpoint(t, db, values[winner])
		if err := store.Checkpoint(ctx, values[winner]); err != nil {
			t.Fatal("exact winner replay", err)
		}
		if err := store.Checkpoint(ctx, values[1-winner]); !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("loser rewrote original checkpoint", err)
		}
		f.requireCheckpoint(t, db, values[winner])
		f.requireOriginal(t, db)
	})

	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run("history_survives_"+kind+"_revocation", func(t *testing.T) {
			ordinal++
			facts, authorization := sharedAuthorizationSQLIntegrationFixture(t, db, kind, 1600+ordinal)
			f := newSharedEconomicHistorySQLFixture(t, db, owner, ordinal, 1, &authorization)
			if err := sharedAuthorizationSQLIntegrationValidate(t, db, authorization); err != nil {
				t.Fatal("positive current authority before revocation", err)
			}
			var result *gorm.DB
			switch kind {
			case "personal_key":
				result = db.Model(&entity.APIKey{}).Where("id = ?", facts.PersonalKey.ID).Update("status", entity.KeyRevoked)
			case "project_key":
				result = db.Model(&entity.ProjectKey{}).Where("id = ?", facts.ProjectKey.ID).Update("status", entity.KeyRevoked)
			case "personal_session":
				result = db.Where("id = ?", facts.Session.ID).Delete(&entity.Session{})
			case "team_session":
				result = db.Model(&entity.TeamMembership{}).Where("id = ?", facts.Membership.ID).Update("status", entity.ResourceDisabled)
			}
			if result.Error != nil || result.RowsAffected != 1 {
				t.Fatal("exact current revocation", result.Error, result.RowsAffected)
			}
			if err := sharedAuthorizationSQLIntegrationValidate(t, db, authorization); err == nil {
				t.Fatal("revocation did not remove current dispatch authority")
			}
			if err := store.Checkpoint(ctx, f.checkpoints[0]); err != nil {
				t.Fatal("revocation suppressed original attempt history", err)
			}
			if _, err := store.Read(ctx, f.key); err != nil {
				t.Fatal("revocation suppressed exact receipt", err)
			}
			f.requireCheckpoint(t, db, f.checkpoints[0])
			f.requireOriginal(t, db)
		})
	}

	t.Run("history_survives_owner_fence_and_mode_revision", func(t *testing.T) {
		f := fixture(t, 1)
		owners := &admissionOwnerStore{db: db}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return owners.CheckCurrentOwner(tx, owner.Owner, owner.InstanceStartedAt)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
			t.Fatal("positive current owner", err)
		}
		fence := admissionOwnerFence{FencingID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Owner: owner.Owner, InstanceStartedAt: owner.InstanceStartedAt}
		if _, err := owners.FenceOwner(ctx, fence); err != nil {
			t.Fatal("terminal owner fence", err)
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return owners.CheckCurrentOwner(tx, owner.Owner, owner.InstanceStartedAt)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err == nil {
			t.Fatal("fenced owner retained current authority")
		}
		result := db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("revision", int64(3))
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatal("test-only mode revision change", result.Error)
		}
		if err := store.Checkpoint(ctx, f.checkpoints[0]); err != nil {
			t.Fatal("fencing/mode change suppressed original checkpoint", err)
		}
		if err := store.Checkpoint(ctx, f.checkpoints[0]); err != nil {
			t.Fatal("fencing/mode change suppressed exact retry", err)
		}
		if _, err := store.Read(ctx, f.key); err != nil {
			t.Fatal("fencing/mode change suppressed original receipt", err)
		}
		f.requireOriginal(t, db)
	})

	for _, test := range []struct {
		name   string
		change func(*testing.T, *gorm.DB, sharedEconomicHistorySQLFixture)
	}{
		{"missing_first_attempt", func(t *testing.T, tx *gorm.DB, f sharedEconomicHistorySQLFixture) {
			sharedEconomicHistorySQLChanged(t, tx.Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 1).Delete(&entity.AdmissionEconomicAttempt{}))
		}},
		{"missing_predecessor_checkpoint", func(t *testing.T, tx *gorm.DB, f sharedEconomicHistorySQLFixture) {
			sharedEconomicHistorySQLChanged(t, tx.Model(&entity.AdmissionEconomicAttempt{}).Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 1).Updates(map[string]any{"checkpoint_digest": nil, "checkpoint_body": nil}))
		}},
		{"noncanonical_dispatch", func(t *testing.T, tx *gorm.DB, f sharedEconomicHistorySQLFixture) {
			sharedEconomicHistorySQLChanged(t, tx.Model(&entity.AdmissionEconomicAttempt{}).Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 2).Update("dispatch_body", append([]byte(" "), f.attempts[1].DispatchBody...)))
		}},
		{"checkpoint_digest_mismatch", func(t *testing.T, tx *gorm.DB, f sharedEconomicHistorySQLFixture) {
			sharedEconomicHistorySQLChanged(t, tx.Model(&entity.AdmissionEconomicAttempt{}).Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 1).Update("checkpoint_digest", strings.Repeat("f", 64)))
		}},
		{"request_case_alias", func(t *testing.T, tx *gorm.DB, f sharedEconomicHistorySQLFixture) {
			sharedEconomicHistorySQLChanged(t, tx.Model(&entity.AdmissionEconomicReceipt{}).Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Update("request_id", strings.ToUpper(f.row.RequestID)))
		}},
	} {
		t.Run("corruption_"+test.name, func(t *testing.T) {
			f := fixture(t, 2)
			if _, err := store.Read(ctx, f.key); err != nil {
				t.Fatal("positive ordered history before corruption", err)
			}
			tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer func() { _ = tx.Rollback().Error }()
			test.change(t, tx, f)
			corrupt := &sharedEconomicSQLStore{db: tx}
			if got, err := corrupt.Read(ctx, f.key); got != nil || !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("corrupt history returned authority receipt", err)
			}
			if err := corrupt.Checkpoint(ctx, f.checkpoints[1]); !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("checkpoint repaired or ignored corrupt history", err)
			}
			if err := tx.Rollback().Error; err != nil {
				t.Fatal(err)
			}
			if _, err := store.Read(ctx, f.key); err != nil {
				t.Fatal("rollback did not preserve original history", err)
			}
			f.requireOriginal(t, db)
		})
	}

	t.Run("terminal_rejects_new_checkpoint_but_preserves_exact_retry", func(t *testing.T) {
		f := fixture(t, 1)
		terminal := sharedTerminalIntent{Key: f.key, LastAttempt: 1, Fact: f.checkpoints[0].Fallback, Actual: eventqueue.QuotaSettlement{}, ExecutorClosed: true, Upstream: sharedUpstreamUnknown}
		terminal.Digest = durableSharedSpoolDigest(t, terminal)
		body := sharedEconomicHistorySQLBody(t, terminal)
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionEconomicReceipt{}).Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Updates(map[string]any{"terminal_digest": terminal.Digest, "terminal_body": body}))
		if err := store.Checkpoint(ctx, f.checkpoints[0]); !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("new checkpoint changed terminal history", err)
		}
		var retained entity.AdmissionEconomicAttempt
		if err := db.Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 1).Take(&retained).Error; err != nil || retained.CheckpointDigest != nil || retained.CheckpointBody != nil {
			t.Fatal("terminal rejection wrote a checkpoint", err)
		}
		// Seed an already checkpointed terminal to exercise historical retry;
		// no Settle/economic effects are simulated by this dormant store test.
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionEconomicAttempt{}).Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, 1).Updates(map[string]any{"checkpoint_digest": f.checkpoints[0].Digest, "checkpoint_body": sharedEconomicHistorySQLBody(t, f.checkpoints[0])}))
		if err := store.Checkpoint(ctx, f.checkpoints[0]); err != nil {
			t.Fatal("terminal lost exact checkpoint retry", err)
		}
		got, err := store.Read(ctx, f.key)
		if err != nil || got == nil || !reflect.DeepEqual(got.Terminal, &terminal) {
			t.Fatal("original terminal receipt changed", err)
		}
		got.Terminal.Fact[0] = '!'
		again, err := store.Read(ctx, f.key)
		if err != nil || !reflect.DeepEqual(again.Terminal, &terminal) {
			t.Fatal("terminal response aliases storage", err)
		}
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		corrupt := terminal
		corrupt.LastAttempt = 0
		corrupt.Digest = durableSharedSpoolDigest(t, corrupt)
		sharedEconomicHistorySQLChanged(t, tx.Model(&entity.AdmissionEconomicReceipt{}).Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Updates(map[string]any{"terminal_digest": corrupt.Digest, "terminal_body": sharedEconomicHistorySQLBody(t, corrupt)}))
		if got, err := (&sharedEconomicSQLStore{db: tx}).Read(ctx, f.key); got != nil || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("canonical but inconsistent terminal accepted", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		again, err = store.Read(ctx, f.key)
		if err != nil || !reflect.DeepEqual(again.Terminal, &terminal) {
			t.Fatal("terminal corruption rollback changed original", err)
		}
	})

	t.Run("rollback_and_cancelled_wait_do_not_checkpoint", func(t *testing.T) {
		f := fixture(t, 1)
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		if err := (&sharedEconomicSQLStore{db: tx}).Checkpoint(ctx, f.checkpoints[0]); err != nil {
			t.Fatal("checkpoint inside caller rollback transaction", err)
		}
		f.requireCheckpoint(t, tx, f.checkpoints[0])
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		f.requireNoCheckpoint(t, db)
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if err := store.Checkpoint(cancelled, f.checkpoints[0]); !errors.Is(err, context.Canceled) {
			t.Fatal("pre-cancelled checkpoint", err)
		}
		if got, err := store.Read(cancelled, f.key); got != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("pre-cancelled read", err)
		}
		// Hold the actual first lock and expire a competing Checkpoint. Deadline
		// failure is not missing evidence and cannot leave a committed checkpoint.
		held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		var domain entity.AdmissionEconomicDomain
		if err := held.Clauses(clause.Locking{Strength: "UPDATE"}).Where("installation_id = ?", f.row.InstallationID).Take(&domain).Error; err != nil {
			t.Fatal(err)
		}
		waitCtx, stopWait := context.WithTimeout(ctx, 750*time.Millisecond)
		defer stopWait()
		result := make(chan error, 1)
		go func() { result <- store.Checkpoint(waitCtx, f.checkpoints[0]) }()
		select {
		case err := <-result:
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, eventqueue.ErrMissing) {
				t.Fatal("lock wait cancellation became success or absence", err)
			}
		case <-time.After(5 * time.Second):
			stopWait()
			_ = held.Rollback().Error
			select {
			case <-result:
			case <-time.After(5 * time.Second):
				t.Fatal("cancelled checkpoint did not join after release")
			}
			t.Fatal("checkpoint exceeded bounded lock deadline")
		}
		if err := held.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		f.requireNoCheckpoint(t, db)
		f.requireOriginal(t, db)
		if err := store.Checkpoint(ctx, f.checkpoints[0]); err != nil {
			t.Fatal("fresh explicit retry after cancelled transaction", err)
		}
	})

	t.Run("missing_and_changed_key_do_not_create_history", func(t *testing.T) {
		f := fixture(t, 1)
		missing := f.key
		missing.RequestID = "req_never_accepted"
		if got, err := store.Read(ctx, missing); got != nil || !errors.Is(err, eventqueue.ErrMissing) {
			t.Fatal("absent receipt not explicit", err)
		}
		missingCheckpoint := f.checkpoints[0]
		missingCheckpoint.Key = missing
		missingCheckpoint.Digest = durableSharedSpoolDigest(t, missingCheckpoint)
		if err := store.Checkpoint(ctx, missingCheckpoint); !errors.Is(err, eventqueue.ErrMissing) {
			t.Fatal("checkpoint fabricated absent accepted history", err)
		}
		changed := f.key
		changed.ExecutionID = "execution_other"
		if got, err := store.Read(ctx, changed); got != nil || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("changed original execution admitted", err)
		}
		checkpoint := f.checkpoints[0]
		checkpoint.Key = changed
		checkpoint.Digest = durableSharedSpoolDigest(t, checkpoint)
		if err := store.Checkpoint(ctx, checkpoint); !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("changed execution rewrote history", err)
		}
		f.requireNoCheckpoint(t, db)
		f.requireOriginal(t, db)
	})
}

func sharedEconomicHistorySQLBody(t *testing.T, value any) []byte {
	t.Helper()
	body, err := sharedEconomicCanonicalBody(value)
	if err != nil {
		t.Fatal("bounded canonical test body", err)
	}
	return body
}

func sharedEconomicHistorySQLChanged(t *testing.T, result *gorm.DB) {
	t.Helper()
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatal("exact fixture mutation", result.Error, result.RowsAffected)
	}
}

func sharedEconomicHistorySQLOwner(t *testing.T, db *gorm.DB) admissionOwnerRegistration {
	t.Helper()
	in := admissionOwnerFixture()
	in.InstanceStartedAt = time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	sharedEconomicHistorySQLChanged(t, db.Model(&entity.LimitInstallation{}).Where("id = ?", 1).Updates(map[string]any{"initialized": true, "journal_id": in.Owner.InstallationID}))
	mode := entity.AdmissionMode{ID: 1, InstallationID: in.Owner.InstallationID, Mode: "shared", Revision: 2}
	if err := db.Create(&mode).Error; err != nil {
		t.Fatal("shared mode is test-only", err)
	}
	instance := entity.SystemInstance{ID: in.Owner.IncarnationID, LeaseToken: "lck_01jjjjjjjjjjjjjjjjjjjjjjjj", HeartbeatRevision: 1, Role: "combined",
		StartedAt: in.InstanceStartedAt, LastHeartbeatAt: in.InstanceStartedAt, LeaseExpiresAt: in.InstanceStartedAt.Add(time.Second)}
	if err := db.Create(&instance).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&admissionOwnerStore{db: db}).RegisterOwner(t.Context(), in); err != nil {
		t.Fatal("original current owner registration", err)
	}
	return in
}

type sharedEconomicHistorySQLFixture struct {
	row         entity.AdmissionEconomicReceipt
	key         sharedAdmissionKey
	attempts    []entity.AdmissionEconomicAttempt
	checkpoints []sharedAttemptCheckpoint
}

func newSharedEconomicHistorySQLFixture(t *testing.T, db *gorm.DB, owner admissionOwnerRegistration, ordinal, count int, authorization *sharedCapturedAuthorization) sharedEconomicHistorySQLFixture {
	t.Helper()
	row := sharedEconomicHistoryTestReceipt(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	intent.Owner, row.OwnerStartedAt = owner.Owner, owner.InstanceStartedAt
	intent.OwnerStartedAt = owner.InstanceStartedAt
	intent.RequestID, intent.ExecutionID = fmt.Sprintf("req_history_%d", ordinal), fmt.Sprintf("execution_history_%d", ordinal)
	var initial CallFact
	if err := json.Unmarshal(intent.Fallback, &initial); err != nil {
		t.Fatal(err)
	}
	initial.RequestID = intent.RequestID
	if authorization != nil {
		intent.Authorization = *authorization
		initial.UserID, initial.KeyID, initial.ProjectID, initial.TeamID, initial.TeamMembershipID = "", "", "", "", ""
		initial.ModelID = authorization.Model.ID
		if authorization.User != nil {
			initial.UserID = authorization.User.ID
		}
		if authorization.Key != nil {
			initial.KeyID = authorization.Key.ID
		}
		if authorization.Project != nil {
			initial.ProjectID = authorization.Project.ID
		}
		if authorization.Team != nil {
			initial.TeamID, initial.TeamMembershipID = authorization.Team.ID, authorization.Team.MembershipID
		}
	}
	intent.Fallback = sharedEconomicHistorySQLBody(t, initial)
	alignSharedTestReservation(t, &intent)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	row.InstallationID, row.SpoolID, row.IncarnationID = owner.Owner.InstallationID, owner.Owner.SpoolID, owner.Owner.IncarnationID
	row.ModeRevision, row.Fence = int64(owner.Owner.ModeRevision), int64(owner.Owner.Fence)
	row.RequestID, row.ExecutionID, row.ReceiptID = intent.RequestID, intent.ExecutionID, fmt.Sprintf("receipt_history_%d", ordinal)
	row.RequestDigest, row.ExecutionDigest, row.ReceiptDigest = sharedEconomicIdentityDigest(row.RequestID), sharedEconomicIdentityDigest(row.ExecutionID), sharedEconomicIdentityDigest(row.ReceiptID)
	row.AdmissionDigest, row.IntentBody = intent.Digest, sharedEconomicHistorySQLBody(t, intent)
	at := time.Now().UTC().Add(time.Second)
	if at.Before(intent.Authorization.EvaluatedAt) {
		at = intent.Authorization.EvaluatedAt.Add(time.Second)
	}
	row.AdmittedAt = at.UnixNano()
	row.MonthStart, row.MonthEnd, err = eventqueue.QuotaMonthBounds(row.AdmittedAt, intent.TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sharedEconomicReceiptIntent(row); err != nil {
		t.Fatal("fixture is not valid accepted history", err)
	}
	f := sharedEconomicHistorySQLFixture{row: row, key: sharedIntentKey(intent)}
	var completed []CallAttempt
	for number := 1; number <= count; number++ {
		current := sharedTestAttempt(number, false)
		current.ID = fmt.Sprintf("att_history_%d_%d", ordinal, number)
		dispatchFact := initial
		dispatchFact.Attempts = append(append([]CallAttempt(nil), completed...), current)
		attempt := sharedAttemptIntent{Key: f.key, Number: number, AttemptID: current.ID, CredentialID: current.CredentialID, SnapshotID: current.SnapshotID,
			ConnectionID: current.ConnectionID, ProviderModelID: current.ProviderModelID, Fallback: sharedEconomicHistorySQLBody(t, dispatchFact)}
		attempt.Digest = durableSharedSpoolDigest(t, attempt)
		retained := entity.AdmissionEconomicAttempt{InstallationID: row.InstallationID, RequestDigest: row.RequestDigest, RequestID: row.RequestID, Number: int64(number),
			AttemptID: attempt.AttemptID, AttemptIdentityDigest: sharedEconomicIdentityDigest(attempt.AttemptID), AttemptDigest: attempt.Digest, DispatchBody: sharedEconomicHistorySQLBody(t, attempt)}
		if number < count {
			current.WorkEvidence, current.FailureClass = "not_sent", "connection_failure"
		}
		completed = append(completed, current)
		checkpointFact := initial
		checkpointFact.Attempts = append([]CallAttempt(nil), completed...)
		checkpoint := sharedAttemptCheckpoint{Key: f.key, Number: number, AttemptDigest: attempt.Digest, Fallback: sharedEconomicHistorySQLBody(t, checkpointFact), Recovery: sharedCheckpointRecovery(checkpointFact, intent.Bound)}
		checkpoint.Digest = durableSharedSpoolDigest(t, checkpoint)
		if number < count {
			retained.CheckpointDigest = &checkpoint.Digest
			retained.CheckpointBody = sharedEconomicHistorySQLBody(t, checkpoint)
		}
		f.attempts, f.checkpoints = append(f.attempts, retained), append(f.checkpoints, checkpoint)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		for i := range f.attempts {
			if err := tx.Create(&f.attempts[i]).Error; err != nil {
				return err
			}
		}
		return tx.Model(&entity.AdmissionEconomicDomain{}).Where("installation_id = ?", row.InstallationID).Update("receipt_count", gorm.Expr("receipt_count + 1")).Error
	}); err != nil {
		t.Fatal("seed original retained history", err)
	}
	return f
}

func (f sharedEconomicHistorySQLFixture) requireOriginal(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row entity.AdmissionEconomicReceipt
	if err := db.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Take(&row).Error; err != nil {
		t.Fatal(err)
	}
	// Normalize only driver timestamp location; never relax exact instants or bytes.
	row.OwnerStartedAt, row.Domain = row.OwnerStartedAt.UTC(), nil
	if !reflect.DeepEqual(row, f.row) {
		t.Fatal("checkpoint changed immutable original receipt")
	}
	var attempts []entity.AdmissionEconomicAttempt
	if err := db.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Order("number").Find(&attempts).Error; err != nil || len(attempts) != len(f.attempts) {
		t.Fatal("original dispatch census changed", err)
	}
	for i := range attempts {
		attempts[i].CheckpointDigest, attempts[i].CheckpointBody, attempts[i].Receipt = nil, nil, nil
		expected := f.attempts[i]
		expected.CheckpointDigest, expected.CheckpointBody = nil, nil
		if !reflect.DeepEqual(attempts[i], expected) {
			t.Fatal("checkpoint changed immutable dispatch bytes")
		}
	}
}

func (f sharedEconomicHistorySQLFixture) requireNoCheckpoint(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row entity.AdmissionEconomicAttempt
	if err := db.Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, len(f.attempts)).Take(&row).Error; err != nil || row.CheckpointDigest != nil || row.CheckpointBody != nil {
		t.Fatal("failed transaction left checkpoint evidence", err)
	}
}

func (f sharedEconomicHistorySQLFixture) requireCheckpoint(t *testing.T, db *gorm.DB, checkpoint sharedAttemptCheckpoint) {
	t.Helper()
	var row entity.AdmissionEconomicAttempt
	if err := db.Where("installation_id = ? AND request_digest = ? AND number = ?", f.row.InstallationID, f.row.RequestDigest, checkpoint.Number).Take(&row).Error; err != nil || row.CheckpointDigest == nil || *row.CheckpointDigest != checkpoint.Digest || !bytes.Equal(row.CheckpointBody, sharedEconomicHistorySQLBody(t, checkpoint)) {
		t.Fatal("persisted checkpoint is not exact original canonical bytes", err)
	}
}

func sharedEconomicHistorySQLConcurrent(t *testing.T, parent context.Context, store *sharedEconomicSQLStore, values ...sharedAttemptCheckpoint) []error {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	type outcome struct {
		index int
		err   error
	}
	start, done := make(chan struct{}), make(chan outcome, len(values))
	for i, value := range values {
		go func() { <-start; done <- outcome{i, store.Checkpoint(ctx, value)} }()
	}
	close(start)
	results := make([]error, len(values))
	joined := 0
	for joined < len(values) {
		select {
		case result := <-done:
			results[result.index] = result.err
			joined++
		case <-ctx.Done():
			cancel()
			deadline := time.NewTimer(5 * time.Second)
			defer deadline.Stop()
			for joined < len(values) {
				select {
				case <-done:
					joined++
				case <-deadline.C:
					t.Fatal("concurrent SQL checkpoints did not join after cancellation")
				}
			}
			t.Fatal("concurrent SQL checkpoints exceeded bounded deadline")
		}
	}
	return results
}
