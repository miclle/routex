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

// This opt-in source owns a fresh disposable schema per driver. Test-only mode
// and economic initialization establish no production cutover or coverage proof.
func TestSharedAccountUsageSQLIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_SHARED_USAGE_SQL") != "1" {
		t.Skip("dedicated shared account usage SQL worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_SHARED_USAGE_SQL_DRIVER")
	if driver != "postgres" && driver != "mysql" {
		t.Fatal("choose one dedicated postgres or mysql worker")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	db := runtimeArchiveIntegrationDB(t, driver).WithContext(ctx)
	tables := []any{&entity.AdmissionEconomicDomain{}, &entity.AdmissionEconomicReceipt{}, &entity.AdmissionEconomicAttempt{},
		&entity.AdmissionEconomicAccount{}, &entity.AdmissionEconomicCounter{}, &entity.AdmissionEconomicContribution{}, &entity.AdmissionEconomicInvalidBound{}}
	if db.Migrator().HasTable("schema_migrations") {
		t.Fatal("usage worker requires a fresh exclusively owned schema")
	}
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("usage worker requires absent economic tables")
		}
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal("current predecessor schema", err)
	}
	// Bounded test-only creation; the separate frozen migration matrix owns DDL
	// qualification. No released migration is changed by this worker.
	for _, table := range tables {
		if db.Migrator().HasTable(table) {
			t.Fatal("predecessor unexpectedly created economic tables")
		}
		if err := db.Migrator().CreateTable(table); err != nil {
			t.Fatal("test-only economic table", err)
		}
	}
	target := sharedUsageReadTestTarget()
	base := sharedEconomicHistoryTestReceipt(t)
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		var binding entity.LimitInstallation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&binding, 1).Error; err != nil {
			return err
		}
		if binding.Initialized {
			return errors.New("fresh worker unexpectedly has an initialized installation")
		}
		if err := tx.Model(&entity.LimitInstallation{}).Where("id = ?", 1).
			Updates(map[string]any{"journal_id": target.InstallationID, "initialized": true}).Error; err != nil {
			return err
		}
		var mode entity.AdmissionMode
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Take(&mode, 1).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			mode = entity.AdmissionMode{ID: 1, InstallationID: target.InstallationID, Mode: "shared", Revision: int64(target.ModeRevision)}
			if err := tx.Omit(clause.Associations).Create(&mode).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if err := tx.Model(&mode).Updates(map[string]any{"installation_id": target.InstallationID, "mode": "shared", "revision": int64(target.ModeRevision)}).Error; err != nil {
				return err
			}
		}
		domain := entity.AdmissionEconomicDomain{InstallationID: target.InstallationID, FormatVersion: 1, Initialized: true,
			CoverageStart: base.AdmittedAt - int64(24*time.Hour), ClockHighWater: base.AdmittedAt, ReceiptCapacity: 10000}
		return tx.Create(&domain).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
		t.Fatal("explicit test-only initialized authority", err)
	}
	ordinal := 50000
	fixture := func(t *testing.T, unknown bool) sharedEconomicProjectionSQLFixture {
		t.Helper()
		ordinal++
		return newSharedEconomicProjectionSQLFixture(t, db, ordinal, unknown)
	}
	sample := func(source *gorm.DB, account string, now time.Time, requested sharedUsageReadTarget) (*sharedAccountUsageProof, error) {
		return sharedUsageReadSQLSampleTransaction(source, requested, account, now)
	}
	readDomain := func(t *testing.T) entity.AdmissionEconomicDomain {
		t.Helper()
		var domain entity.AdmissionEconomicDomain
		if err := db.Take(&domain, "installation_id = ?", target.InstallationID).Error; err != nil {
			t.Fatal(err)
		}
		return domain
	}

	t.Run("original_expiry_unknown_active_and_monotonic_time", func(t *testing.T) {
		f := fixture(t, true)
		rpm, executor := f.projection(t, "rpm", "minute", nil), f.projection(t, "executor", "active", nil)
		active := f.projection(t, "active_quota", "active", &eventqueue.QuotaCounterContribution{})
		terminal := f.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Settled: true})
		f.insert(t, db, rpm, executor, active, terminal)
		invalid := entity.AdmissionEconomicInvalidBound{InstallationID: f.row.InstallationID, RevisionDigest: sharedEconomicIdentityDigest("invalid_usage_bound"),
			Revision: "invalid_usage_bound", RequestDigest: f.row.RequestDigest, RequestID: f.row.RequestID, RecordedAt: f.row.AdmittedAt}
		if err := db.Create(&invalid).Error; err != nil {
			t.Fatal(err)
		}
		before := f.state(t, db)
		proof, err := sample(db, f.account.Account, time.Unix(0, *rpm.ExpiresAt-1), target)
		if err != nil || proof == nil || proof.Recorded == nil || proof.Recorded.RPM != 1 || proof.Recorded.Concurrency != 1 || !proof.Recorded.Birth.Equal(f.account.Birth) || proof.AsOf.UnixNano() != *rpm.ExpiresAt-1 {
			t.Fatal("pre-cutoff exact registered proof", proof, err)
		}
		f.requireState(t, db, before)
		proof, err = sample(db, f.account.Account, time.Unix(0, *rpm.ExpiresAt), target)
		if err != nil || proof == nil || proof.Recorded == nil || proof.Recorded.RPM != 0 || proof.Recorded.Concurrency != 1 || !proof.Recorded.Birth.Equal(f.account.Birth) {
			t.Fatal("exact original cutoff did not expire only RPM", proof, err)
		}
		f.requireCounterAbsent(t, db, "minute")
		f.requireCounter(t, db, "active", eventqueue.QuotaCounter{TokensUnknown: 1, MoneyUnknown: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}})
		after := f.state(t, db)
		if len(after.contributions) != 2 || len(after.invalid) != 1 || !reflect.DeepEqual(after.invalid[0], invalid) {
			t.Fatal("sampling lost active/executor or retained invalid-bound facts")
		}
		regressed, err := sample(db, f.account.Account, time.Unix(0, base.AdmittedAt), target)
		if err != nil || regressed == nil || !regressed.AsOf.Equal(proof.AsOf) || !reflect.DeepEqual(regressed.Recorded, proof.Recorded) {
			t.Fatal("regressing caller clock altered proof", regressed, err)
		}
		f.requireState(t, db, after)
		f.requireReceipt(t, db)
		if domain := readDomain(t); domain.ClockHighWater != *rpm.ExpiresAt {
			t.Fatal("sample did not retain monotonic domain clock", domain)
		}
	})

	t.Run("missing_case_distinct_and_registered_zero", func(t *testing.T) {
		f := fixture(t, false)
		var before int64
		if err := db.Model(&entity.AdmissionEconomicAccount{}).Count(&before).Error; err != nil {
			t.Fatal(err)
		}
		now := time.Unix(0, readDomain(t).ClockHighWater+1)
		for _, account := range []string{"usr_usage_missing", strings.ToUpper(f.account.Account)} {
			proof, err := sample(db, account, now, target)
			if err != nil || proof == nil || proof.Recorded != nil || proof.Account != account || !proof.AsOf.Equal(now) {
				t.Fatal("absence became zero or case alias", proof, err)
			}
		}
		var count int64
		if err := db.Model(&entity.AdmissionEconomicAccount{}).Count(&count).Error; err != nil || count != before {
			t.Fatal("missing sample created an account", count, before, err)
		}
		proof, err := sample(db, f.account.Account, now, target)
		if err != nil || proof == nil || proof.Recorded == nil || proof.Recorded.RPM != 0 || proof.Recorded.Concurrency != 0 || !proof.Recorded.Birth.Equal(f.account.Birth) {
			t.Fatal("registered zero became absence", proof, err)
		}
		f.requireReceipt(t, db)
	})

	t.Run("actual_transaction_and_current_authority", func(t *testing.T) {
		f := fixture(t, false)
		now := time.Unix(0, readDomain(t).ClockHighWater+1)
		if proof, err := sampleSharedAccountUsageSQL(db, target, f.account.Account, now); proof != nil || !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatal("plain database handle was accepted as a transaction", proof, err)
		}
		before, domainBefore := f.state(t, db), readDomain(t)
		for _, revision := range []uint64{1, 3} {
			stale := target
			stale.ModeRevision = revision
			if proof, err := sample(db, f.account.Account, now, stale); proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("different current revision was accepted", proof, err)
			}
		}
		for _, change := range []map[string]any{{"mode": "local"}, {"revision": int64(3)}} {
			err := db.Transaction(func(tx *gorm.DB) error {
				if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
					return err
				}
				if _, _, err := lockAdmissionOwnerDomain(sharedAuthorizationDB(tx)); err != nil {
					return err
				}
				if err := tx.Model(&entity.AdmissionMode{}).Where("id = ?", 1).Updates(change).Error; err != nil {
					return err
				}
				proof, err := sampleSharedAccountUsageSQL(tx, target, f.account.Account, now)
				if proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
					t.Errorf("current authority change was accepted: %+v, %v", proof, err)
					return errors.New("unexpected current authority proof")
				}
				return err // Roll back the test-only current authority mutation.
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if !errors.Is(err, errSharedAdmissionConflict) {
				t.Fatal("authority control did not roll back", err)
			}
		}
		f.requireState(t, db, before)
		if after := readDomain(t); !reflect.DeepEqual(after, domainBefore) {
			t.Fatal("rejected authority advanced the domain clock")
		}
	})

	t.Run("later_corrupt_expiry_rolls_back_prior_subtraction", func(t *testing.T) {
		f := fixture(t, false)
		rpm := f.projection(t, "rpm", "minute", nil)
		tokens, money := int64(9), "0.000000000000000001"
		terminal := f.projection(t, "terminal_quota", "minute", &eventqueue.QuotaCounterContribution{Bound: f.bound, Settled: true,
			Actual: eventqueue.QuotaSettlement{Tokens: &tokens, Money: &money, Currency: "USD"}})
		f.insert(t, db, rpm, terminal)
		sharedEconomicHistorySQLChanged(t, db.Model(&entity.AdmissionEconomicContribution{}).
			Where("installation_id = ? AND request_digest = ? AND kind = ?", terminal.InstallationID, terminal.RequestDigest, terminal.Kind).
			Update("contribution_body", append([]byte(" "), terminal.ContributionBody...)))
		before, domainBefore := f.state(t, db), readDomain(t)
		if proof, err := sample(db, f.account.Account, time.Unix(0, domainBefore.ClockHighWater+1), target); proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("corrupt later expiry returned partial proof", proof, err)
		}
		// rpm sorts before terminal_quota at the same expiry. Its earlier
		// subtraction must roll back when the later original body is corrupt.
		f.requireState(t, db, before)
		f.requireReceipt(t, db)
		if after := readDomain(t); !reflect.DeepEqual(after, domainBefore) {
			t.Fatal("failed expiry advanced the domain clock")
		}
	})

	t.Run("expiry_work_overflow_refuses_without_mutation", func(t *testing.T) {
		f := fixture(t, false)
		generatedAt := time.Now()
		receipts, projections := sharedUsageSQLOverflowFixtures(t, sharedEconomicExpiryBatch+1, f.account.Account, f.account.Birth)
		t.Logf("generated %d exact overflow admissions/projections in %s", len(receipts), time.Since(generatedAt))
		insertedAt := time.Now()
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
			t.Fatal("4097-row bounded overflow fixture", err)
		}
		t.Logf("inserted exact overflow fixtures in %s", time.Since(insertedAt))
		before, domainBefore := f.state(t, db), readDomain(t)
		if proof, err := sample(db, f.account.Account, time.Unix(0, domainBefore.ClockHighWater+1), target); proof != nil || !errors.Is(err, errSharedEconomicUnavailable) {
			t.Fatal("expiry overflow returned success or partial proof", proof, err)
		}
		f.requireState(t, db, before)
		f.requireReceipt(t, db)
		if after := readDomain(t); !reflect.DeepEqual(after, domainBefore) {
			t.Fatal("overflow advanced the domain clock")
		}
	})

	t.Run("retained_account_lock_and_cancelled_wait", func(t *testing.T) {
		f := fixture(t, false)
		before := f.state(t, db)
		tx := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if tx.Error != nil {
			t.Fatal(tx.Error)
		}
		defer func() { _ = tx.Rollback().Error }()
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			t.Fatal(err)
		}
		if proof, err := sampleSharedAccountUsageSQL(tx, target, f.account.Account, time.Unix(0, readDomain(t).ClockHighWater)); err != nil || proof == nil {
			t.Fatal("sample inside retained owner transaction", proof, err)
		}
		waitCtx, stop := context.WithTimeout(ctx, 350*time.Millisecond)
		defer stop()
		// A second SQL connection takes only this account lock, proving that
		// the helper retains it until its caller commits or rolls back.
		err := db.WithContext(waitCtx).Connection(func(conn *gorm.DB) error {
			return conn.Transaction(func(other *gorm.DB) error {
				var row entity.AdmissionEconomicAccount
				return sharedEconomicAccountQuery(other, target.InstallationID, f.account.AccountDigest).
					Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("second connection escaped the retained account lock", err)
		}
		if err := tx.Rollback().Error; err != nil {
			t.Fatal("release original caller transaction", err)
		}
		f.requireState(t, db, before)
		proof, err := sample(db, f.account.Account, time.Unix(0, readDomain(t).ClockHighWater), target)
		if err != nil || proof == nil || proof.Recorded == nil {
			t.Fatal("fresh explicit retry after lock cancellation", proof, err)
		}
	})

	t.Run("cancelled_helper_wait_and_uninitialized_domain", func(t *testing.T) {
		f := fixture(t, false)
		before, domainBefore := f.state(t, db), readDomain(t)
		held := db.Begin(&sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if held.Error != nil {
			t.Fatal(held.Error)
		}
		defer func() { _ = held.Rollback().Error }()
		if _, err := lockSharedEconomicDomain(held, target.InstallationID); err != nil {
			t.Fatal("test owner domain lock", err)
		}
		waitCtx, stop := context.WithTimeout(ctx, 350*time.Millisecond)
		defer stop()
		if proof, err := sample(db.WithContext(waitCtx), f.account.Account, time.Unix(0, domainBefore.ClockHighWater+1), target); proof != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("cancelled helper lock wait returned absence or partial proof", proof, err)
		}
		if err := held.Rollback().Error; err != nil {
			t.Fatal("release domain owner", err)
		}
		f.requireState(t, db, before)
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
				return err
			}
			if _, _, err := lockAdmissionOwnerDomain(sharedAuthorizationDB(tx)); err != nil {
				return err
			}
			if _, err := lockSharedEconomicDomain(tx, target.InstallationID); err != nil {
				return err
			}
			if err := tx.Model(&entity.AdmissionEconomicDomain{}).Where("installation_id = ?", target.InstallationID).
				Updates(map[string]any{"initialized": false, "coverage_start": int64(0), "clock_high_water": int64(0), "receipt_count": int64(0)}).Error; err != nil {
				return err
			}
			proof, err := sampleSharedAccountUsageSQL(tx, target, f.account.Account, time.Unix(0, domainBefore.ClockHighWater+1))
			if proof != nil || !errors.Is(err, errSharedAdmissionConflict) {
				t.Errorf("uninitialized domain returned usage: %+v, %v", proof, err)
				return errors.New("unexpected uninitialized usage proof")
			}
			return err
		}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if !errors.Is(err, errSharedAdmissionConflict) {
			t.Fatal("uninitialized domain control did not roll back", err)
		}
		f.requireState(t, db, before)
		if after := readDomain(t); !reflect.DeepEqual(after, domainBefore) {
			t.Fatal("cancellation/uninitialized read changed domain")
		}
	})
}

// Derive distinct receipts from one validated immutable scaffold. Every derived
// admission still receives the real projection constructor's canonical-body,
// digest, bound, birth, and complete retained-identity checks. Rebuilding the
// unchanged coordinator and price basis 4,097 times tests no overflow behavior.
func sharedUsageSQLOverflowFixtures(t *testing.T, count int, account string, birth time.Time) ([]entity.AdmissionEconomicReceipt, []entity.AdmissionEconomicContribution) {
	t.Helper()
	template := newSharedEconomicProjectionSQLReceipt(t, 60000, account, birth)
	original, err := sharedEconomicReceiptIntent(template.row)
	if err != nil {
		t.Fatal("validated overflow admission template", err)
	}
	var fallback CallFact
	if err := json.Unmarshal(original.Fallback, &fallback); err != nil {
		t.Fatal("overflow fallback template", err)
	}
	receipts := make([]entity.AdmissionEconomicReceipt, 0, count)
	projections := make([]entity.AdmissionEconomicContribution, 0, count)
	for i := range count {
		ordinal := 60000 + i
		intent := original
		manifest := *original.Reservation
		intent.Reservation = &manifest
		intent.RequestID, intent.ExecutionID = fmt.Sprintf("req_projection_%d", ordinal), fmt.Sprintf("execution_projection_%d", ordinal)
		fact := fallback
		fact.RequestID = intent.RequestID
		intent.Fallback = sharedEconomicHistorySQLBody(t, fact)
		alignSharedTestReservation(t, &intent)
		intent.Digest = durableSharedSpoolDigest(t, intent)
		row := template.row
		row.RequestID, row.ExecutionID, row.ReceiptID = intent.RequestID, intent.ExecutionID, fmt.Sprintf("receipt_projection_%d", ordinal)
		row.RequestDigest, row.ExecutionDigest, row.ReceiptDigest = sharedEconomicIdentityDigest(row.RequestID), sharedEconomicIdentityDigest(row.ExecutionID), sharedEconomicIdentityDigest(row.ReceiptID)
		row.AdmissionDigest, row.IntentBody = intent.Digest, sharedEconomicHistorySQLBody(t, intent)
		projection, err := newSharedEconomicProjection(row, template.account, "rpm", "minute", nil)
		if err != nil {
			t.Fatal("exact derived overflow admission/projection", ordinal, err)
		}
		receipts = append(receipts, row)
		projections = append(projections, projection)
	}
	return receipts, projections
}

func sharedUsageReadSQLSampleTransaction(db *gorm.DB, target sharedUsageReadTarget, account string, now time.Time) (*sharedAccountUsageProof, error) {
	var proof *sharedAccountUsageProof
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(sharedAuthorizationDB(tx)); err != nil {
			return err
		}
		var err error
		proof, err = sampleSharedAccountUsageSQL(tx, target, account, now)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	return proof, nil
}
