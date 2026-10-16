package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// This worker owns an empty disposable schema and exercises only historical
// settlement. Test-only economic DDL does not qualify the frozen migration.
func TestSharedEconomicSettlementSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_SETTLEMENT_SQL") != "1" {
		t.Skip("dedicated shared economic settlement SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_SETTLEMENT_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	db := runtimeArchiveIntegrationDB(t, driver)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	db = db.WithContext(ctx)
	tables := []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicReceipt{}, &entity.AdmissionEconomicAttempt{},
		&entity.AdmissionEconomicAccount{}, &entity.AdmissionEconomicCounter{}, &entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicInvalidBound{}}
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("settlement worker requires a fresh, exclusively owned schema")
	}
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("settlement worker requires absent economic tables")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("predecessor unexpectedly created economic tables")
		}
		if err := db.Migrator().CreateTable(table); err != nil {
			t.Fatal("seven bounded test-only economic tables", err)
		}
	}
	store := &sharedEconomicSQLStore{db: db}
	ordinal := 0
	fixture := func(t *testing.T) sharedEconomicSettlementSQLFixture {
		t.Helper()
		ordinal++
		return newSharedEconomicSettlementSQLFixture(t, db, ordinal, time.Time{}, nil, nil)
	}

	t.Run("identical_concurrent_terminal_releases_executor_once_retains_unknown_holds", func(t *testing.T) {
		f := fixture(t)
		terminal := f.terminal(t, nil, sharedUpstreamUnknown)
		if terminal.Actual.Tokens != nil || terminal.Actual.Money != nil {
			t.Fatal("unknown terminal acquired favorable usage")
		}
		results := sharedEconomicSettlementSQLConcurrent(t, ctx, store, f.now(), terminal, terminal)
		for _, result := range results {
			if result.err != nil || result.receipt == nil || !reflect.DeepEqual(result.receipt.Terminal, &terminal) {
				t.Fatal("identical concurrent settlement", result.err)
			}
		}
		f.requireSettled(t, db, terminal)
		f.requireCounts(t, db, 1, 0)
		f.requireCounterAbsent(t, db, "active")
		for _, window := range f.windows() {
			f.requireCounter(t, db, window, eventqueue.QuotaCounter{TokensHeld: 100,
				MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		}
		if rows := f.state(t, db).contributions; len(rows) != 5 {
			t.Fatal("settlement duplicated terminal holds or retained active/executor contribution", len(rows))
		}
		before := f.snapshot(t, db)
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, f.now()); err != nil {
			t.Fatal("exact terminal retry", err)
		}
		f.requireSnapshot(t, db, before)
	})

	t.Run("terminal_conflict_preserves_original_bytes_and_return_copies", func(t *testing.T) {
		f := fixture(t)
		terminal := f.terminal(t, nil, sharedUpstreamUnknown)
		// Preserve the original submitted bytes independently of the returned
		// object and the caller-owned request slice used by this invocation.
		got, err := store.settleSharedEconomicSQL(ctx, sharedTestCopy(terminal), f.now())
		if err != nil || got == nil || got.Terminal == nil {
			t.Fatal("positive terminal", err)
		}
		before := f.snapshot(t, db)
		got.Terminal.Fact[0] = '!'
		got.Dispatches[0].AttemptDigest = "changed"
		changed := f.terminal(t, func(fact *CallFact) { fact.ErrorCode = "different_terminal_failure" }, sharedUpstreamUnknown)
		if changed.Digest == terminal.Digest {
			t.Fatal("conflicting terminal fixture did not change immutable body")
		}
		if receipt, err := store.settleSharedEconomicSQL(ctx, changed, f.now()); receipt != nil || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("conflicting terminal rewrote original", err)
		}
		f.requireSnapshot(t, db, before)
		again, err := store.settleSharedEconomicSQL(ctx, terminal, f.now())
		if err != nil || again == nil || !reflect.DeepEqual(again.Terminal, &terminal) || again.Dispatches[0].AttemptDigest != f.attempt.AttemptDigest {
			t.Fatal("returned receipt aliases persisted original", err)
		}
		f.requireSnapshot(t, db, before)
	})

	t.Run("invalid_actual_or_closure_cannot_release_executor", func(t *testing.T) {
		f := fixture(t)
		valid := f.terminal(t, nil, sharedUpstreamUnknown)
		before := f.snapshot(t, db)
		for _, change := range []func(*sharedTerminalIntent){
			func(value *sharedTerminalIntent) { zero := int64(0); value.Actual.Tokens = &zero },
			func(value *sharedTerminalIntent) { value.ExecutorClosed = false },
			func(value *sharedTerminalIntent) { value.Upstream = sharedUpstreamTerminal },
		} {
			value := valid
			change(&value)
			value.Digest = durableSharedSpoolDigest(t, value)
			if receipt, err := store.settleSharedEconomicSQL(ctx, value, f.now()); err == nil || receipt != nil {
				t.Fatal("unproven favorable Actual/closure settled original")
			}
			f.requireSnapshot(t, db, before)
		}
		f.requireCounts(t, db, 1, 1)
	})

	t.Run("full_overrun_debt_and_permanent_original_bound_invalidation", func(t *testing.T) {
		f := fixture(t)
		terminal := f.terminal(t, sharedEconomicSettlementSQLPricedOverrun, sharedUpstreamTerminal)
		if terminal.Actual.Tokens == nil || *terminal.Actual.Tokens != 1500000 || terminal.Actual.Money == nil || *terminal.Actual.Money != "3.8" || terminal.Actual.Currency != "USD" {
			t.Fatal("validated full overrun fixture lost exact economics")
		}
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, f.now()); err != nil {
			t.Fatal("full overrun settlement", err)
		}
		f.requireSettled(t, db, terminal)
		f.requireCounts(t, db, 1, 0)
		for _, window := range f.windows() {
			f.requireCounter(t, db, window, eventqueue.QuotaCounter{TokensUsed: 1500000,
				MoneyUsed: map[string]string{"USD": "3800000000000000000"}, MoneyHeld: map[string]string{}})
		}
		invalid := f.requireInvalidBound(t, db)
		if invalid.Revision != f.bound.Revision || invalid.RequestID != f.row.RequestID || invalid.RequestDigest != f.row.RequestDigest || invalid.RecordedAt != f.now().UnixNano() {
			t.Fatal("invalidation did not retain exact original revision/cause")
		}
		if err := f.expire(db, f.row.MonthEnd+int64(8*24*time.Hour)); err != nil {
			t.Fatal("expired debt projections", err)
		}
		f.requireCounts(t, db, 0, 0)
		if state := f.state(t, db); len(state.counters) != 0 || len(state.contributions) != 0 || len(state.invalid) != 1 || !reflect.DeepEqual(state.invalid[0], invalid) {
			t.Fatal("projection expiry restored invalidated bound or retained expired debt")
		}
		before := f.snapshot(t, db)
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, time.Unix(0, f.row.MonthEnd).Add(9*24*time.Hour)); err != nil {
			t.Fatal("historical terminal retry after debt expiry", err)
		}
		f.requireSnapshot(t, db, before)
	})

	t.Run("retained_missing_account_refuses_without_synthesis", func(t *testing.T) {
		f := fixture(t)
		terminal := f.terminal(t, nil, sharedUpstreamUnknown)
		before := f.snapshot(t, db)
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		for _, table := range []any{&entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicCounter{}, &entity.AdmissionEconomicAccount{}} {
			result := tx.Where("installation_id = ? AND account_digest = ?", f.account.InstallationID, f.account.AccountDigest).Delete(table)
			if result.Error != nil || result.RowsAffected == 0 {
				t.Fatal("test-only missing-account reconstruction", result.Error)
			}
		}
		if receipt, err := (&sharedEconomicSQLStore{db: tx}).settleSharedEconomicSQL(ctx, terminal, f.now()); receipt != nil || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("missing historical account synthesized settlement", err)
		}
		var count int64
		if err := tx.Model(&entity.AdmissionEconomicAccount{}).Where("installation_id = ? AND account_digest = ?", f.account.InstallationID, f.account.AccountDigest).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("historical account was recreated", count, err)
		}
		var retained entity.AdmissionEconomicReceipt
		if err := tx.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Take(&retained).Error; err != nil || retained.TerminalDigest != nil || retained.TerminalBody != nil {
			t.Fatal("missing-account refusal retained a false terminal", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		f.requireSnapshot(t, db, before)
	})

	t.Run("cancellation_and_outer_rollback_preserve_complete_original", func(t *testing.T) {
		f := fixture(t)
		terminal := f.terminal(t, nil, sharedUpstreamUnknown)
		before := f.snapshot(t, db)
		cancelled, stop := context.WithCancel(ctx)
		stop()
		if receipt, err := store.settleSharedEconomicSQL(cancelled, terminal, f.now()); receipt != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled settlement returned a receipt", err)
		}
		f.requireSnapshot(t, db, before)
		sharedEconomicSettlementSQLCancelledLock(t, ctx, db, f, terminal)
		f.requireSnapshot(t, db, before)
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		if receipt, err := (&sharedEconomicSQLStore{db: tx}).settleSharedEconomicSQL(ctx, terminal, f.now()); err != nil || receipt == nil {
			t.Fatal("positive nested settlement before rollback", err)
		}
		f.requireSettled(t, tx, terminal)
		if err := tx.Rollback().Error; err != nil {
			t.Fatal(err)
		}
		f.requireSnapshot(t, db, before)
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, f.now()); err != nil {
			t.Fatal("original remains settleable after cancellation and rollback", err)
		}
		f.requireSettled(t, db, terminal)
	})

	t.Run("historical_settlement_survives_current_owner_and_auth_revocation", func(t *testing.T) {
		owner := sharedEconomicHistorySQLOwner(t, db)
		facts, authorization := sharedAuthorizationSQLIntegrationFixture(t, db, "personal_key", 1901)
		if err := sharedAuthorizationSQLIntegrationValidate(t, db, authorization); err != nil {
			t.Fatal("positive current authentication control", err)
		}
		owners := &admissionOwnerStore{db: db}
		if err := db.Transaction(func(tx *gorm.DB) error {
			return owners.CheckCurrentOwner(tx, owner.Owner, owner.InstanceStartedAt)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
			t.Fatal("positive current owner control", err)
		}
		ordinal++
		at := time.Now().UTC().Add(time.Second)
		if at.Before(authorization.EvaluatedAt) {
			at = authorization.EvaluatedAt.Add(time.Second)
		}
		f := newSharedEconomicSettlementSQLFixture(t, db, ordinal, at, &owner, &authorization)
		terminal := f.terminal(t, nil, sharedUpstreamUnknown)
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.APIKey{}).Where("id = ?", facts.PersonalKey.ID).Update("status", entity.KeyRevoked))
		if err := sharedAuthorizationSQLIntegrationValidate(t, db, authorization); err == nil {
			t.Fatal("Key revocation retained current dispatch authority")
		}
		fence := admissionOwnerFence{FencingID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Owner: owner.Owner, InstanceStartedAt: owner.InstanceStartedAt}
		if _, err := owners.FenceOwner(ctx, fence); err != nil {
			t.Fatal("terminal original owner fence", err)
		}
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Update("revision", int64(3)))
		if err := db.Transaction(func(tx *gorm.DB) error {
			return owners.CheckCurrentOwner(tx, owner.Owner, owner.InstanceStartedAt)
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err == nil {
			t.Fatal("fenced owner retained current dispatch authority")
		}
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, f.now()); err != nil {
			t.Fatal("current revocation suppressed original economic settlement", err)
		}
		f.requireSettled(t, db, terminal)
		f.requireCounts(t, db, 1, 0)
		for _, window := range f.windows() {
			f.requireCounter(t, db, window, eventqueue.QuotaCounter{TokensHeld: 100,
				MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		}
	})

	t.Run("late_settlement_keeps_admission_month_and_original_rolling_windows", func(t *testing.T) {
		ordinal++
		at := time.Date(2026, time.October, 31, 23, 59, 30, 0, time.UTC)
		f := newSharedEconomicSettlementSQLFixture(t, db, ordinal, at, nil, nil)
		terminal := f.terminal(t, func(fact *CallFact) {
			input, output := int64(20), int64(1)
			fact.InputTokens, fact.OutputTokens, fact.UsageComplete = &input, &output, true
			fact.CompletedAt = at.Add(2 * time.Hour)
			fact.Attempts[0].CompletedAt = fact.CompletedAt
			fact.Attempts[0].FinalUsageKnown = true
			fact.Attempts[0].NativeCompletionEvidence = "incomplete"
		}, sharedUpstreamTerminal)
		later := time.Date(2026, time.November, 1, 2, 0, 0, 0, time.UTC)
		if _, err := store.settleSharedEconomicSQL(ctx, terminal, later); err != nil {
			t.Fatal("late original settlement", err)
		}
		f.requireSettled(t, db, terminal)
		f.requireCounts(t, db, 0, 0)
		f.requireCounterAbsent(t, db, "minute")
		f.requireCounterAbsent(t, db, fmt.Sprintf("month_%d", f.row.MonthStart))
		newMonth, _, err := eventqueue.QuotaMonthBounds(later.UnixNano(), "UTC")
		if err != nil {
			t.Fatal(err)
		}
		f.requireCounterAbsent(t, db, fmt.Sprintf("month_%d", newMonth))
		for _, window := range []string{"five_hours", "seven_days"} {
			f.requireCounter(t, db, window, eventqueue.QuotaCounter{TokensUsed: 21,
				MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		}
		state := f.state(t, db)
		if len(state.contributions) != 2 {
			t.Fatal("late settlement created expired/new-month projection", len(state.contributions))
		}
		for _, row := range state.contributions {
			want := at.Add(5 * time.Hour).UnixNano()
			if row.Window == "seven_days" {
				want = at.Add(7 * 24 * time.Hour).UnixNano()
			}
			if row.ExpiresAt == nil || *row.ExpiresAt != want {
				t.Fatal("late settlement restarted rolling expiry", row.Window)
			}
		}
	})
}

type sharedEconomicSettlementSQLFixture struct {
	sharedEconomicProjectionSQLFixture
	key      sharedAdmissionKey
	intent   sharedAdmissionIntent
	dispatch sharedAttemptIntent
	attempt  entity.AdmissionEconomicAttempt
}

func newSharedEconomicSettlementSQLFixture(t *testing.T, db *gorm.DB, ordinal int, at time.Time, owner *admissionOwnerRegistration, authorization *sharedCapturedAuthorization) sharedEconomicSettlementSQLFixture {
	t.Helper()
	base := newSharedEconomicProjectionSQLReceipt(t, ordinal, fmt.Sprintf("usr_settlement_%d", ordinal), sharedAuthorizationTestTime().Add(-24*time.Hour))
	intent, err := sharedEconomicReceiptIntent(base.row)
	if err != nil {
		t.Fatal(err)
	}
	intent.Owner.InstallationID = fmt.Sprintf("led_%026d", ordinal)
	if owner != nil {
		intent.Owner, base.row.OwnerStartedAt = owner.Owner, owner.InstanceStartedAt
		intent.OwnerStartedAt = owner.InstanceStartedAt
	}
	var initial CallFact
	if err := json.Unmarshal(intent.Fallback, &initial); err != nil {
		t.Fatal(err)
	}
	if authorization != nil {
		intent.Authorization = *authorization
		initial.UserID, initial.KeyID, initial.ProjectID, initial.TeamID, initial.TeamMembershipID = "", "", "", "", ""
		initial.ModelID = authorization.Model.ID
		if authorization.User != nil {
			initial.UserID = authorization.User.ID
			intent.Policies[0].Account, intent.Policies[0].CreatedAt = authorization.User.ID, authorization.User.CreatedAt
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
	if at.IsZero() {
		at = sharedAuthorizationTestTime().Add(time.Second)
	}
	initial.StartedAt, initial.CompletedAt = at, at.Add(time.Second)
	intent.Recovery = zeroQuotaSettlement(intent.Bound)
	intent.Fallback = sharedEconomicHistorySQLBody(t, initial)
	alignSharedTestReservation(t, &intent)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	base.row.InstallationID, base.row.SpoolID, base.row.IncarnationID = intent.Owner.InstallationID, intent.Owner.SpoolID, intent.Owner.IncarnationID
	base.row.ModeRevision, base.row.Fence = int64(intent.Owner.ModeRevision), int64(intent.Owner.Fence)
	base.row.AdmissionDigest, base.row.IntentBody, base.row.AdmittedAt = intent.Digest, sharedEconomicHistorySQLBody(t, intent), at.UnixNano()
	base.row.MonthStart, base.row.MonthEnd, err = eventqueue.QuotaMonthBounds(at.UnixNano(), intent.TimeZone)
	if err != nil {
		t.Fatal(err)
	}
	base.policy, base.bound = intent.Policies[0], intent.Bound
	base.account.InstallationID, base.account.Account, base.account.Birth = base.row.InstallationID, base.policy.Account, base.policy.CreatedAt
	base.account.AccountDigest = sharedEconomicIdentityDigest(base.account.Account)
	if _, err := sharedEconomicReceiptIntent(base.row); err != nil {
		t.Fatal("retained admission before test-only seeding", err)
	}
	f := sharedEconomicSettlementSQLFixture{sharedEconomicProjectionSQLFixture: base, intent: intent, key: sharedIntentKey(intent)}
	current := sharedTestAttempt(1, false)
	current.ID = fmt.Sprintf("att_settlement_%d", ordinal)
	current.StartedAt, current.CompletedAt = at, at.Add(time.Second)
	initial.Attempts = []CallAttempt{current}
	f.dispatch = sharedAttemptIntent{Key: f.key, Number: 1, AttemptID: current.ID, CredentialID: current.CredentialID, SnapshotID: current.SnapshotID,
		ConnectionID: current.ConnectionID, ProviderModelID: current.ProviderModelID, Fallback: sharedEconomicHistorySQLBody(t, initial)}
	f.dispatch.Digest = durableSharedSpoolDigest(t, f.dispatch)
	if err := validateSharedSpoolAttempt(&sharedSpoolRecord{Intent: intent}, f.dispatch); err != nil {
		t.Fatal("valid original dispatch fixture", err)
	}
	f.attempt = entity.AdmissionEconomicAttempt{InstallationID: base.row.InstallationID, RequestDigest: base.row.RequestDigest, RequestID: base.row.RequestID, Number: 1,
		AttemptID: current.ID, AttemptIdentityDigest: sharedEconomicIdentityDigest(current.ID), AttemptDigest: f.dispatch.Digest, DispatchBody: sharedEconomicHistorySQLBody(t, f.dispatch)}
	domain := entity.AdmissionEconomicDomain{InstallationID: base.row.InstallationID, FormatVersion: 1, Initialized: true,
		CoverageStart: 1, ClockHighWater: base.row.AdmittedAt, ReceiptCapacity: 100, ReceiptCount: 1}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, row := range []any{&domain, &f.row, &f.attempt} {
			if err := tx.Omit(clause.Associations).Create(row).Error; err != nil {
				return err
			}
		}
		accounts, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy}, f.row.AdmittedAt, true)
		if err == nil {
			f.account = accounts[0]
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
		t.Fatal("test-only accepted receipt/dispatch/account", err)
	}
	f.insert(t, db, f.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{Bound: f.bound}),
		f.projection(t, "executor", "active", nil), f.projection(t, "rpm", "minute", nil))
	return f
}

func (f sharedEconomicSettlementSQLFixture) now() time.Time {
	return time.Unix(0, f.row.AdmittedAt).Add(time.Second).UTC()
}

func (f sharedEconomicSettlementSQLFixture) windows() []string {
	return []string{"minute", "five_hours", "seven_days", fmt.Sprintf("month_%d", f.row.MonthStart)}
}

func (f sharedEconomicSettlementSQLFixture) terminal(t *testing.T, change func(*CallFact), closure sharedUpstreamClosure) sharedTerminalIntent {
	t.Helper()
	var fact CallFact
	if err := json.Unmarshal(f.dispatch.Fallback, &fact); err != nil {
		t.Fatal(err)
	}
	if change != nil {
		change(&fact)
	}
	if err := validateCallFact(fact); err != nil {
		t.Fatal("terminal fixture must satisfy actual call validation", err)
	}
	terminal := sharedTerminalIntent{Key: f.key, LastAttempt: 1, Fact: sharedEconomicHistorySQLBody(t, fact),
		Actual: quotaCallSettlement(fact), ExecutorClosed: true, Upstream: closure}
	terminal.Digest = durableSharedSpoolDigest(t, terminal)
	if err := validateSharedSpoolTerminal(&sharedSpoolRecord{Intent: f.intent, Attempts: []sharedAttemptIntent{f.dispatch}}, terminal); err != nil {
		t.Fatal("terminal fixture must match original dispatch/actual/closure", err)
	}
	return terminal
}

// Reuse the existing pricing fixture and finalizer, never write a favorable
// Actual directly. An observed incomplete native terminal is not completed.
func sharedEconomicSettlementSQLPricedOverrun(fact *CallFact) {
	priced := pricedFact()
	fact.InputTokens, fact.OutputTokens = priced.InputTokens, priced.OutputTokens
	fact.CacheReadTokens, fact.CacheWriteTokens, fact.UsageComplete = priced.CacheReadTokens, priced.CacheWriteTokens, true
	fact.PriceBasis = priced.PriceBasis
	fact.ProviderModelID = fact.Attempts[0].ProviderModelID
	fact.PriceBasis.Schedule.ProviderModelID = fact.ProviderModelID
	fact.PriceBasis.Currency.PlatformCurrency = "USD"
	fact.PriceBasis.Currency.Rates = map[string]string{"USD": "1"}
	fact.Attempts[0].FinalUsageKnown = true
	fact.Attempts[0].NativeCompletionEvidence = "incomplete"
	finalizeCallPricing(fact)
}

type sharedEconomicSettlementSQLState struct {
	projection sharedEconomicProjectionSQLState
	row        entity.AdmissionEconomicReceipt
	attempts   []entity.AdmissionEconomicAttempt
	domain     entity.AdmissionEconomicDomain
}

func (f sharedEconomicSettlementSQLFixture) snapshot(t *testing.T, db *gorm.DB) sharedEconomicSettlementSQLState {
	t.Helper()
	state := sharedEconomicSettlementSQLState{projection: f.state(t, db)}
	query := func() *gorm.DB {
		return db.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest)
	}
	if err := query().Take(&state.row).Error; err != nil {
		t.Fatal(err)
	}
	if err := query().Order("number").Find(&state.attempts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("installation_id = ?", f.row.InstallationID).Take(&state.domain).Error; err != nil {
		t.Fatal(err)
	}
	return state
}

func (f sharedEconomicSettlementSQLFixture) requireSnapshot(t *testing.T, db *gorm.DB, expected sharedEconomicSettlementSQLState) {
	t.Helper()
	if actual := f.snapshot(t, db); !reflect.DeepEqual(actual, expected) {
		t.Fatal("refused/replayed/rolled-back settlement changed original economic state")
	}
}

func (f sharedEconomicSettlementSQLFixture) requireSettled(t *testing.T, db *gorm.DB, terminal sharedTerminalIntent) {
	t.Helper()
	actual := f.snapshot(t, db)
	expected := f.row
	expected.TerminalDigest, expected.TerminalBody = &terminal.Digest, sharedEconomicHistorySQLBody(t, terminal)
	actual.row.OwnerStartedAt = actual.row.OwnerStartedAt.UTC()
	if !reflect.DeepEqual(actual.row, expected) || len(actual.attempts) != 1 || !reflect.DeepEqual(actual.attempts[0], f.attempt) {
		t.Fatal("settlement changed original receipt/dispatch or failed exact terminal persistence")
	}
}

func (f sharedEconomicSettlementSQLFixture) requireInvalidBound(t *testing.T, db *gorm.DB) entity.AdmissionEconomicInvalidBound {
	t.Helper()
	var row entity.AdmissionEconomicInvalidBound
	if err := db.Where("installation_id = ? AND revision_digest = ?", f.row.InstallationID, sharedEconomicIdentityDigest(f.bound.Revision)).Take(&row).Error; err != nil {
		t.Fatal("permanent original bound invalidation", err)
	}
	return row
}

type sharedEconomicSettlementSQLOutcome struct {
	receipt *sharedAuthorityReceipt
	err     error
}

func sharedEconomicSettlementSQLConcurrent(t *testing.T, parent context.Context, store *sharedEconomicSQLStore, now time.Time, values ...sharedTerminalIntent) []sharedEconomicSettlementSQLOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan sharedEconomicSettlementSQLOutcome, len(values))
	for _, value := range values {
		go func() {
			<-start
			receipt, err := store.settleSharedEconomicSQL(ctx, value, now)
			done <- sharedEconomicSettlementSQLOutcome{receipt: receipt, err: err}
		}()
	}
	close(start)
	results := make([]sharedEconomicSettlementSQLOutcome, 0, len(values))
	for len(results) < len(values) {
		select {
		case result := <-done:
			results = append(results, result)
		case <-ctx.Done():
			cancel()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			for len(results) < len(values) {
				select {
				case result := <-done:
					results = append(results, result)
				case <-timer.C:
					t.Fatal("concurrent settlements did not join after cancellation")
				}
			}
			t.Fatal("concurrent settlements exceeded bounded deadline")
		}
	}
	return results
}

// Hold only the account row, so Settle can enter its domain/request transaction
// before cancellation. No callback registry mutation or production hook is used.
func sharedEconomicSettlementSQLCancelledLock(t *testing.T, parent context.Context, db *gorm.DB, f sharedEconomicSettlementSQLFixture, terminal sharedTerminalIntent) {
	t.Helper()
	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	if _, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy}, f.row.AdmittedAt, false); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	started := make(chan struct{})
	done := make(chan sharedEconomicSettlementSQLOutcome, 1)
	go func() {
		close(started)
		receipt, err := (&sharedEconomicSQLStore{db: db}).settleSharedEconomicSQL(ctx, terminal, f.now())
		done <- sharedEconomicSettlementSQLOutcome{receipt: receipt, err: err}
	}()
	select {
	case <-started:
	case <-parent.Done():
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("settlement contender did not join before startup")
		}
		t.Fatal("parent cancelled before settlement contender started")
	}
	select {
	case result := <-done:
		t.Fatal("settlement escaped retained account lock", result.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case result := <-done:
		if result.receipt != nil || !errors.Is(result.err, context.Canceled) {
			t.Fatal("cancelled blocked settlement produced receipt", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled blocked settlement did not join")
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal("release cancellation control lock", err)
	}
}
