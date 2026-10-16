package service

import (
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

// This worker qualifies dormant projection operations on one fresh, exclusively
// owned database. It does not activate shared admission or qualify migration DDL.
func TestSharedEconomicProjectionSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_PROJECTION_SQL") != "1" {
		t.Skip("dedicated shared economic projection SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_ECONOMIC_PROJECTION_SQL_DRIVER")
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
		t.Fatal("projection worker requires a fresh, exclusively owned schema")
	}
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("projection worker requires absent economic tables")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	// Seven bounded test-only entities; the separate frozen-schema worker owns
	// empty/upgrade/repeat/concurrent migration and constraint qualification.
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("predecessor unexpectedly created economic tables")
		}
		if err := db.Migrator().CreateTable(table); err != nil {
			t.Fatal("test-only projection table", err)
		}
	}
	base := sharedEconomicHistoryTestReceipt(t)
	domain := entity.AdmissionEconomicDomain{InstallationID: base.InstallationID, FormatVersion: 1,
		Initialized: true, CoverageStart: 1, ClockHighWater: 9000000000000000000, ReceiptCapacity: 10000}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatal("explicit test-only initialized domain", err)
	}
	ordinal := 0
	fixture := func(t *testing.T) sharedEconomicProjectionSQLFixture {
		t.Helper()
		ordinal++
		return newSharedEconomicProjectionSQLFixture(t, db, ordinal, false)
	}

	t.Run("exact_account_birth_case_and_order", func(t *testing.T) {
		f := fixture(t)
		alias := f.policy
		alias.Account = strings.ToUpper(alias.Account)
		if err := db.Transaction(func(tx *gorm.DB) error {
			if _, err := lockSharedEconomicDomain(tx, f.row.InstallationID); err != nil {
				return err
			}
			rows, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy, alias}, f.row.AdmittedAt, true)
			if err != nil {
				return err
			}
			if len(rows) != 2 || rows[0].Account != alias.Account || rows[1].Account != f.account.Account || rows[0].AccountDigest == rows[1].AccountDigest {
				return errors.New("case-distinct accounts aliased or lock order changed")
			}
			return nil
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
			t.Fatal("exact case-distinct account control", err)
		}
		before := f.state(t, db)
		reborn := f.policy
		reborn.CreatedAt = reborn.CreatedAt.Add(time.Microsecond)
		for _, policies := range [][]eventqueue.QuotaLimit{{reborn}, {f.policy, f.policy}} {
			err := db.Transaction(func(tx *gorm.DB) error {
				if _, err := lockSharedEconomicDomain(tx, f.row.InstallationID); err != nil {
					return err
				}
				_, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, policies, f.row.AdmittedAt, false)
				return err
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err == nil {
				t.Fatal("reborn or duplicated account accepted")
			}
			f.requireState(t, db, before)
		}
		missing := f.policy
		missing.Account = "usr_projection_missing"
		err := db.Transaction(func(tx *gorm.DB) error {
			if _, err := lockSharedEconomicDomain(tx, f.row.InstallationID); err != nil {
				return err
			}
			_, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{missing}, f.row.AdmittedAt, false)
			return err
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err == nil {
			t.Fatal("retained-only lookup synthesized a missing historical account")
		}
		var count int64
		if err := db.Model(&entity.AdmissionEconomicAccount{}).Where("installation_id = ? AND account_digest = ?", f.row.InstallationID, sharedEconomicIdentityDigest(missing.Account)).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("missing historical account was persisted", count, err)
		}
		f.requireState(t, db, before)
	})

	t.Run("active_quota_executor_and_rpm_are_independent", func(t *testing.T) {
		f := fixture(t)
		active := f.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{Bound: f.bound})
		rpm := f.projection(t, "rpm", "minute", nil)
		executor := f.projection(t, "executor", "active", nil)
		f.insert(t, db, active, rpm, executor)
		f.requireCounts(t, db, 1, 1)
		f.requireCounter(t, db, "active", eventqueue.QuotaCounter{TokensHeld: 100, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		f.remove(t, db, active)
		f.requireCounts(t, db, 1, 1)
		f.requireCounterAbsent(t, db, "active")
		f.remove(t, db, executor)
		f.requireCounts(t, db, 1, 0)
		f.remove(t, db, rpm)
		f.requireCounts(t, db, 0, 0)
		f.requireReceipt(t, db)
	})

	t.Run("zero_add_and_subtract_preserve_nonempty_counter", func(t *testing.T) {
		f := fixture(t)
		window := fmt.Sprintf("month_%d", f.row.MonthStart)
		known := int64(7)
		amount := "0.000000000000000003"
		nonzero := f.projection(t, "terminal_quota", window, &eventqueue.QuotaCounterContribution{Bound: f.bound, Settled: true,
			Actual: eventqueue.QuotaSettlement{Tokens: &known, Money: &amount, Currency: "USD"}})
		f.insert(t, db, nonzero)
		original := f.counterRow(t, db, window)
		zeroFixture := newSharedEconomicProjectionSQLReceipt(t, 10001, f.account.Account, f.account.Birth)
		if err := db.Create(&zeroFixture.row).Error; err != nil {
			t.Fatal(err)
		}
		zero := int64(0)
		zeroMoney := "0"
		zeroRow := zeroFixture.projection(t, "terminal_quota", window, &eventqueue.QuotaCounterContribution{Bound: zeroFixture.bound, Settled: true,
			Actual: eventqueue.QuotaSettlement{Tokens: &zero, Money: &zeroMoney, Currency: "USD"}})
		zeroFixture.insert(t, db, zeroRow)
		if got := f.counterRow(t, db, window); !reflect.DeepEqual(got, original) {
			t.Fatal("known-zero addition rewrote nonempty counter")
		}
		zeroFixture.remove(t, db, zeroRow)
		if got := f.counterRow(t, db, window); !reflect.DeepEqual(got, original) {
			t.Fatal("known-zero subtraction rewrote nonempty counter")
		}
		f.requireCounter(t, db, window, eventqueue.QuotaCounter{TokensUsed: 7, MoneyUsed: map[string]string{"USD": "3"}, MoneyHeld: map[string]string{}})
		f.requireReceipt(t, db)
		zeroFixture.requireReceipt(t, db)
	})

	t.Run("terminal_dimensions_keep_original_windows", func(t *testing.T) {
		f := fixture(t)
		money := "0.000000000000000005"
		windows := []struct {
			name string
			end  int64
		}{{"minute", f.row.AdmittedAt + int64(time.Minute)}, {"five_hours", f.row.AdmittedAt + int64(5*time.Hour)},
			{"seven_days", f.row.AdmittedAt + int64(7*24*time.Hour)}, {fmt.Sprintf("month_%d", f.row.MonthStart), f.row.MonthEnd}}
		for _, window := range windows {
			row := f.projection(t, "terminal_quota", window.name, &eventqueue.QuotaCounterContribution{Bound: f.bound, Settled: true,
				Actual: eventqueue.QuotaSettlement{Money: &money, Currency: "USD"}})
			if row.ExpiresAt == nil || *row.ExpiresAt != window.end {
				t.Fatal("settlement moved the original admission window", window.name)
			}
			f.insert(t, db, row)
			f.requireCounter(t, db, window.name, eventqueue.QuotaCounter{TokensHeld: 100, MoneyUsed: map[string]string{"USD": "5"}, MoneyHeld: map[string]string{}})
		}
		g := fixture(t)
		tokens := int64(11)
		row := g.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Bound: g.bound, Settled: true,
			Actual: eventqueue.QuotaSettlement{Tokens: &tokens}})
		g.insert(t, db, row)
		g.requireCounter(t, db, "minute", eventqueue.QuotaCounter{TokensUsed: 11, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		// An unbounded unknown remains an independent unknown count, not zero.
		ordinal++
		u := newSharedEconomicProjectionSQLFixture(t, db, ordinal, true)
		unknown := u.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Settled: true})
		u.insert(t, db, unknown)
		u.requireCounter(t, db, "minute", eventqueue.QuotaCounter{TokensUnknown: 1, MoneyUnknown: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}})
		unknownActive := u.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{})
		unknownExecutor := u.projection(t, "executor", "active", nil)
		u.insert(t, db, unknownActive, unknownExecutor)
		if err := u.expire(db, u.row.MonthEnd+int64(24*time.Hour)); err != nil {
			t.Fatal("unknown terminal expiry", err)
		}
		u.requireCounterAbsent(t, db, "minute")
		u.requireCounter(t, db, "active", eventqueue.QuotaCounter{TokensUnknown: 1, MoneyUnknown: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}})
		u.requireCounts(t, db, 0, 1)
	})

	t.Run("expiry_once_retains_history_invalid_bound_and_active", func(t *testing.T) {
		f := fixture(t)
		zero, money := int64(0), "0"
		terminal := f.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Bound: f.bound, Settled: true,
			Actual: eventqueue.QuotaSettlement{Tokens: &zero, Money: &money, Currency: "USD"}})
		active := f.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{Bound: f.bound})
		executor, rpm := f.projection(t, "executor", "active", nil), f.projection(t, "rpm", "minute", nil)
		f.insert(t, db, active, executor, rpm, terminal)
		invalid := entity.AdmissionEconomicInvalidBound{InstallationID: f.row.InstallationID, RevisionDigest: sharedEconomicIdentityDigest(f.bound.Revision),
			Revision: f.bound.Revision, RequestDigest: f.row.RequestDigest, RequestID: f.row.RequestID, RecordedAt: f.row.AdmittedAt}
		if err := db.Create(&invalid).Error; err != nil {
			t.Fatal(err)
		}
		beforeExpiry := f.state(t, db)
		if err := f.expire(db, *rpm.ExpiresAt-1); err != nil {
			t.Fatal(err)
		}
		f.requireState(t, db, beforeExpiry)
		if err := f.expire(db, *rpm.ExpiresAt); err != nil {
			t.Fatal("exact expiry", err)
		}
		f.requireCounts(t, db, 0, 1)
		f.requireCounterAbsent(t, db, "minute")
		f.requireCounter(t, db, "active", eventqueue.QuotaCounter{TokensHeld: 100, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
		afterExpiry := f.state(t, db)
		if len(afterExpiry.contributions) != 2 || len(afterExpiry.invalid) != 1 || !reflect.DeepEqual(afterExpiry.invalid[0], invalid) {
			t.Fatal("expiry removed active/executor or retained invalid-bound evidence")
		}
		for _, at := range []int64{*rpm.ExpiresAt, f.row.MonthEnd + int64(24*time.Hour)} {
			if err := f.expire(db, at); err != nil {
				t.Fatal("repeated expiry", err)
			}
			f.requireState(t, db, afterExpiry)
		}
		f.requireReceipt(t, db)
	})

	for _, fault := range []string{"corrupt_subtraction_body", "counter_underflow"} {
		t.Run(fault+"_rolls_back_entire_expiry", func(t *testing.T) {
			f := fixture(t)
			known, amount := int64(9), "0.000000000000000001"
			rpm := f.projection(t, "rpm", "minute", nil)
			terminal := f.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Bound: f.bound, Settled: true,
				Actual: eventqueue.QuotaSettlement{Tokens: &known, Money: &amount, Currency: "USD"}})
			f.insert(t, db, rpm, terminal)
			if fault == "corrupt_subtraction_body" {
				sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionEconomicContribution{}).
					Where("installation_id = ? AND request_digest = ? AND kind = ?", terminal.InstallationID, terminal.RequestDigest, terminal.Kind).
					Update("contribution_body", append([]byte(" "), terminal.ContributionBody...)))
			} else {
				bad := eventqueue.QuotaCounter{TokensUsed: 8, MoneyUsed: map[string]string{"USD": "1"}, MoneyHeld: map[string]string{}}
				sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionEconomicCounter{}).
					Where("installation_id = ? AND account_digest = ? AND window_name = ?", f.account.InstallationID, f.account.AccountDigest, "minute").
					Update("counter_body", sharedEconomicHistorySQLBody(t, bad)))
			}
			before := f.state(t, db)
			if err := f.expire(db, *rpm.ExpiresAt); err == nil {
				t.Fatal("corruption/underflow permitted expiry")
			}
			// rpm sorts before terminal_quota at the same expiry; its earlier
			// subtraction/deletion must roll back with the later bad row.
			f.requireState(t, db, before)
			f.requireReceipt(t, db)
		})
	}

	t.Run("expiry_batch_overflow_refuses_without_mutation", func(t *testing.T) {
		f := fixture(t)
		var receipts []entity.AdmissionEconomicReceipt
		var projections []entity.AdmissionEconomicContribution
		for i := 0; i <= sharedEconomicExpiryBatch; i++ {
			g := newSharedEconomicProjectionSQLReceipt(t, 20000+i, f.account.Account, f.account.Birth)
			receipts = append(receipts, g.row)
			projections = append(projections, g.projection(t, "rpm", "minute", nil))
		}
		if err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.CreateInBatches(&receipts, 64).Error; err != nil {
				return err
			}
			if err := tx.CreateInBatches(&projections, 128).Error; err != nil {
				return err
			}
			return tx.Model(&entity.AdmissionEconomicAccount{}).Where("installation_id = ? AND account_digest = ?", f.account.InstallationID, f.account.AccountDigest).
				Update("rpm_count", int64(len(projections))).Error
		}); err != nil {
			t.Fatal("bounded overflow fixture", err)
		}
		before := f.state(t, db)
		var retainedBefore int64
		if err := db.Model(&entity.AdmissionEconomicReceipt{}).Where("installation_id = ?", f.row.InstallationID).Count(&retainedBefore).Error; err != nil {
			t.Fatal(err)
		}
		if err := f.expire(db, *projections[0].ExpiresAt); !errors.Is(err, errSharedEconomicUnavailable) {
			t.Fatal("expiry batch overflow did not fail closed", err)
		}
		f.requireState(t, db, before)
		var count int64
		if err := db.Model(&entity.AdmissionEconomicReceipt{}).Where("installation_id = ?", f.row.InstallationID).Count(&count).Error; err != nil || count != retainedBefore {
			t.Fatal("overflow lost retained receipts", count, err)
		}
		f.requireReceipt(t, db)
	})

	t.Run("two_connections_retain_account_and_counter_locks", func(t *testing.T) {
		f := fixture(t)
		active := f.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{Bound: f.bound})
		f.insert(t, db, active)
		for _, resource := range []string{"account", "counter"} {
			sharedEconomicProjectionSQLLockControl(t, ctx, db, f, resource)
		}
		f.requireCounts(t, db, 0, 0)
		f.requireCounter(t, db, "active", eventqueue.QuotaCounter{TokensHeld: 100, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "3"}})
	})
}

type sharedEconomicProjectionSQLFixture struct {
	row     entity.AdmissionEconomicReceipt
	account entity.AdmissionEconomicAccount
	policy  eventqueue.QuotaLimit
	bound   eventqueue.QuotaBound
}

// Historical fixtures retain captured authentication facts, but establish no
// current authorization. Exact admission bytes and births supply subtraction data.
func newSharedEconomicProjectionSQLReceipt(t *testing.T, ordinal int, account string, birth time.Time) sharedEconomicProjectionSQLFixture {
	t.Helper()
	row := sharedEconomicHistoryTestReceipt(t)
	intent, err := sharedEconomicReceiptIntent(row)
	if err != nil {
		t.Fatal(err)
	}
	intent.RequestID, intent.ExecutionID = fmt.Sprintf("req_projection_%d", ordinal), fmt.Sprintf("execution_projection_%d", ordinal)
	var fact CallFact
	if err := json.Unmarshal(intent.Fallback, &fact); err != nil {
		t.Fatal(err)
	}
	fact.RequestID = intent.RequestID
	intent.Fallback = sharedEconomicHistorySQLBody(t, fact)
	intent.Policies[0].Account, intent.Policies[0].CreatedAt = account, birth
	alignSharedTestReservation(t, &intent)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	row.RequestID, row.ExecutionID, row.ReceiptID = intent.RequestID, intent.ExecutionID, fmt.Sprintf("receipt_projection_%d", ordinal)
	row.RequestDigest, row.ExecutionDigest, row.ReceiptDigest = sharedEconomicIdentityDigest(row.RequestID), sharedEconomicIdentityDigest(row.ExecutionID), sharedEconomicIdentityDigest(row.ReceiptID)
	row.AdmissionDigest, row.IntentBody = intent.Digest, sharedEconomicHistorySQLBody(t, intent)
	if _, err := sharedEconomicReceiptIntent(row); err != nil {
		t.Fatal("valid retained admission fixture", err)
	}
	return sharedEconomicProjectionSQLFixture{row: row, policy: intent.Policies[0], bound: intent.Bound,
		account: entity.AdmissionEconomicAccount{InstallationID: row.InstallationID, AccountDigest: sharedEconomicIdentityDigest(account), Account: account, Birth: birth}}
}

func newSharedEconomicProjectionSQLFixture(t *testing.T, db *gorm.DB, ordinal int, unknown bool) sharedEconomicProjectionSQLFixture {
	t.Helper()
	f := newSharedEconomicProjectionSQLReceipt(t, ordinal, fmt.Sprintf("usr_projection_%d", ordinal), sharedAuthorizationTestTime().Add(-24*time.Hour))
	if unknown {
		f.setInitialBound(t, eventqueue.QuotaBound{})
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockSharedEconomicDomain(tx, f.row.InstallationID); err != nil {
			return err
		}
		if err := tx.Omit(clause.Associations).Create(&f.row).Error; err != nil {
			return err
		}
		rows, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy}, f.row.AdmittedAt, true)
		if err == nil {
			f.account = rows[0]
		}
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
		t.Fatal("seed retained admission and exact account", err)
	}
	return f
}

func (f *sharedEconomicProjectionSQLFixture) setInitialBound(t *testing.T, bound eventqueue.QuotaBound) {
	t.Helper()
	intent, err := sharedEconomicReceiptIntent(f.row)
	if err != nil {
		t.Fatal(err)
	}
	intent.Bound, intent.Recovery = bound, zeroQuotaSettlement(bound)
	intent.Reservation = sharedTestReservation(t, intent.RequestID, intent.Authorization, bound.Tokens != nil, bound.Money != nil)
	intent.Digest = durableSharedSpoolDigest(t, intent)
	f.row.AdmissionDigest, f.row.IntentBody, f.bound = intent.Digest, sharedEconomicHistorySQLBody(t, intent), bound
}

func (f sharedEconomicProjectionSQLFixture) projection(t *testing.T, kind, window string, quota *eventqueue.QuotaCounterContribution) entity.AdmissionEconomicContribution {
	t.Helper()
	row, err := newSharedEconomicProjection(f.row, f.account, kind, window, quota)
	if err != nil {
		t.Fatal("projection from exact retained admission", err)
	}
	return row
}

func (f sharedEconomicProjectionSQLFixture) transaction(db *gorm.DB, work func(*gorm.DB, *entity.AdmissionEconomicAccount) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if _, err := lockSharedEconomicDomain(tx, f.row.InstallationID); err != nil {
			return err
		}
		accounts, err := lockSharedEconomicAccountsSQL(tx, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy}, f.row.AdmittedAt, false)
		if err != nil {
			return err
		}
		return work(tx, &accounts[0])
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func (f sharedEconomicProjectionSQLFixture) insert(t *testing.T, db *gorm.DB, rows ...entity.AdmissionEconomicContribution) {
	t.Helper()
	if err := f.transaction(db, func(tx *gorm.DB, account *entity.AdmissionEconomicAccount) error {
		for _, row := range rows {
			if err := insertSharedEconomicProjectionSQL(tx, account, row); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal("insert independent projections", err)
	}
}

func (f sharedEconomicProjectionSQLFixture) remove(t *testing.T, db *gorm.DB, row entity.AdmissionEconomicContribution) {
	t.Helper()
	if err := f.transaction(db, func(tx *gorm.DB, account *entity.AdmissionEconomicAccount) error {
		return deleteSharedEconomicProjectionSQL(tx, account, row)
	}); err != nil {
		t.Fatal("remove exact projection", err)
	}
}

func (f sharedEconomicProjectionSQLFixture) expire(db *gorm.DB, instant int64) error {
	return f.transaction(db, func(tx *gorm.DB, account *entity.AdmissionEconomicAccount) error {
		return expireSharedEconomicProjectionsSQL(tx, account, instant)
	})
}

type sharedEconomicProjectionSQLState struct {
	account       entity.AdmissionEconomicAccount
	counters      []entity.AdmissionEconomicCounter
	contributions []entity.AdmissionEconomicContribution
	invalid       []entity.AdmissionEconomicInvalidBound
}

func (f sharedEconomicProjectionSQLFixture) state(t *testing.T, db *gorm.DB) sharedEconomicProjectionSQLState {
	t.Helper()
	var state sharedEconomicProjectionSQLState
	query := func() *gorm.DB {
		return db.Where("installation_id = ? AND account_digest = ?", f.account.InstallationID, f.account.AccountDigest)
	}
	if err := query().Take(&state.account).Error; err != nil {
		t.Fatal(err)
	}
	if err := query().Order("window_name").Find(&state.counters).Error; err != nil {
		t.Fatal(err)
	}
	if err := query().Order("request_digest, kind, window_name").Find(&state.contributions).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Order("revision_digest").Find(&state.invalid).Error; err != nil {
		t.Fatal(err)
	}
	return state
}

func (f sharedEconomicProjectionSQLFixture) requireState(t *testing.T, db *gorm.DB, expected sharedEconomicProjectionSQLState) {
	t.Helper()
	if actual := f.state(t, db); !reflect.DeepEqual(actual, expected) {
		t.Fatal("economic projection changed across refused/repeated operation")
	}
}

func (f sharedEconomicProjectionSQLFixture) requireCounts(t *testing.T, db *gorm.DB, rpm, executors int64) {
	t.Helper()
	row := f.state(t, db).account
	if row.RPMCount != rpm || row.ExecutorCount != executors {
		t.Fatal("independent RPM/executor counts", row.RPMCount, row.ExecutorCount)
	}
}

func (f sharedEconomicProjectionSQLFixture) counterRow(t *testing.T, db *gorm.DB, window string) entity.AdmissionEconomicCounter {
	t.Helper()
	var row entity.AdmissionEconomicCounter
	if err := db.Where("installation_id = ? AND account_digest = ? AND window_name = ?", f.account.InstallationID, f.account.AccountDigest, window).Take(&row).Error; err != nil {
		t.Fatal("persisted counter", err)
	}
	return row
}

func (f sharedEconomicProjectionSQLFixture) requireCounter(t *testing.T, db *gorm.DB, window string, expected eventqueue.QuotaCounter) {
	t.Helper()
	row := f.counterRow(t, db, window)
	var actual eventqueue.QuotaCounter
	if err := json.Unmarshal(row.CounterBody, &actual); err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatal("persisted independent token/money units", actual, err)
	}
}

func (f sharedEconomicProjectionSQLFixture) requireCounterAbsent(t *testing.T, db *gorm.DB, window string) {
	t.Helper()
	var count int64
	if err := db.Model(&entity.AdmissionEconomicCounter{}).Where("installation_id = ? AND account_digest = ? AND window_name = ?", f.account.InstallationID, f.account.AccountDigest, window).Count(&count).Error; err != nil || count != 0 {
		t.Fatal("empty projection retained a counter", count, err)
	}
}

func (f sharedEconomicProjectionSQLFixture) requireReceipt(t *testing.T, db *gorm.DB) {
	t.Helper()
	var row entity.AdmissionEconomicReceipt
	if err := db.Where("installation_id = ? AND request_digest = ?", f.row.InstallationID, f.row.RequestDigest).Take(&row).Error; err != nil {
		t.Fatal("retained receipt", err)
	}
	row.OwnerStartedAt = row.OwnerStartedAt.UTC()
	if !reflect.DeepEqual(row, f.row) {
		t.Fatal("projection expiry changed immutable receipt")
	}
}

// The contender executes on a second pooled SQL connection. The first
// transaction intentionally avoids the domain lock, so only the exact row lock
// under test can exclude it. Every started contender has a bounded direct join.
func sharedEconomicProjectionSQLLockControl(t *testing.T, parent context.Context, db *gorm.DB, f sharedEconomicProjectionSQLFixture, resource string) {
	t.Helper()
	tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() { _ = tx.Rollback().Error }()
	lock := func(q *gorm.DB) error {
		if resource == "account" {
			_, err := lockSharedEconomicAccountsSQL(q, f.row.InstallationID, []eventqueue.QuotaLimit{f.policy}, f.row.AdmittedAt, false)
			return err
		}
		_, _, _, err := readSharedEconomicCounterSQL(q, f.account, "active")
		return err
	}
	if err := lock(tx); err != nil {
		t.Fatal("owner exact row lock", resource, err)
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		done <- db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
			return conn.Transaction(func(other *gorm.DB) error {
				close(started)
				return lock(other)
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		})
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatal("second SQL connection failed before observation", resource, err)
	case <-ctx.Done():
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("second SQL connection did not join")
		}
		t.Fatal("second SQL connection was not acquired")
	}
	select {
	case err := <-done:
		t.Fatal("contender escaped retained row lock", resource, err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := tx.Rollback().Error; err != nil {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("contender did not join after owner rollback failure")
		}
		t.Fatal("release exact row owner", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("released row lock did not permit contender", resource, err)
		}
	case <-ctx.Done():
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("contender did not join after cancellation")
		}
		t.Fatal("released row lock exceeded bounded deadline")
	}
}
