package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

// The candidate is deliberately unregistered. Run this one worker only against
// an explicitly owned, initially empty disposable routex_test database.
func TestAdmissionEconomicV119Integration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_ECONOMIC_SCHEMA") != "1" {
		t.Skip("dedicated economic schema worker is not enabled")
	}
	driver := os.Getenv("ROUTEX_TEST_ECONOMIC_SCHEMA_DRIVER")
	dsn := os.Getenv("ROUTEX_TEST_ECONOMIC_SCHEMA_DSN")
	if (driver != "postgres" && driver != "mysql") || dsn == "" || os.Getenv("ROUTEX_TEST_ECONOMIC_SCHEMA_OWNED") != "1" {
		t.Fatal("choose one explicit owned postgres/mysql database")
	}
	db := migrationLockTestDB(t, driver, dsn)
	nameQuery := "SELECT current_database()"
	if driver == "mysql" {
		nameQuery = "SELECT DATABASE()"
	}
	var name string
	if err := db.Raw(nameQuery).Scan(&name).Error; err != nil || name != "routex_test" {
		t.Fatal("dedicated routex_test database required", err, name)
	}
	tables, err := db.Migrator().GetTables()
	if err != nil || len(tables) != 0 {
		t.Fatal("economic worker requires an initially empty database", err, tables)
	}

	t.Run("empty_creation_and_repeat", func(t *testing.T) {
		for range 2 {
			if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
				t.Fatal(err)
			}
		}
		for _, model := range admissionEconomicModelsV119() {
			var count int64
			if err := db.Model(model).Count(&count).Error; err != nil || count != 0 {
				t.Fatal("migration must seed no domain, capacity, coverage or receipt", err, count)
			}
		}
	})
	t.Run("empty_partial_ddl_repair", func(t *testing.T) {
		model := &admissionEconomicCounterV119{}
		if err := db.Migrator().DropConstraint(model, "ck_economic_counter_body"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(model, "CounterBody"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
			t.Fatal("repair empty interrupted column", err)
		}
		if err := validateAdmissionEconomicColumnsV119(db, model, true); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("existing_data_upgrade", func(t *testing.T) {
		// The released prefix supplies real existing platform data, while V119
		// continues to own only its seven tables and never seeds its domain.
		if err := MigrateThrough(t.Context(), db, 118); err != nil {
			t.Fatal("released prefix", err)
		}
		var original limitInstallationV14
		if err := db.Take(&original, 1).Error; err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
				t.Fatal(err)
			}
		}
		var after limitInstallationV14
		if err := db.Take(&after, 1).Error; err != nil || after != original {
			t.Fatal("released installation facts changed", err)
		}
		var count int64
		if err := db.Model(&admissionEconomicDomainV119{}).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("existing data cannot manufacture shared coverage", err, count)
		}
	})
	fixture := economicSchemaFixtureV119(t, db)
	t.Run("populated_repeat_preserves_exact_history", func(t *testing.T) {
		if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		economicSchemaAssertFixtureV119(t, db, fixture)
	})
	t.Run("concurrent_candidate_startup", func(t *testing.T) {
		peer := migrationLockTestDB(t, driver, dsn)
		start := make(chan struct{})
		results := make(chan error, 2)
		var workers sync.WaitGroup
		for _, handle := range []*gorm.DB{db, peer} {
			workers.Go(func() { <-start; results <- applyEconomicCandidateV119(t.Context(), handle) })
		}
		close(start)
		workers.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatal("serialized repeated schema application", err)
			}
		}
		economicSchemaAssertFixtureV119(t, db, fixture)
	})
	t.Run("constraints", func(t *testing.T) { economicSchemaConstraintsV119(t, db, fixture) })
	t.Run("case_distinct_exact_identities", func(t *testing.T) { economicSchemaCaseIdentitiesV119(t, db, fixture) })
	t.Run("wrong_existing_index_rejected", func(t *testing.T) {
		model := &admissionEconomicReceiptV119{}
		if err := DropIndex(db, model, "uidx_economic_execution"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().CreateIndex(&economicWrongExecutionIndexV119{}, "uidx_economic_execution"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicCandidateV119(t.Context(), db); err == nil {
			t.Fatal("wrong named index shape accepted")
		}
		if err := DropIndex(db, model, "uidx_economic_execution"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicCandidateV119(t.Context(), db); err != nil {
			t.Fatal("repair missing index", err)
		}
	})
	t.Run("projection_expiry_retains_receipt_and_invalid_revision", func(t *testing.T) {
		if err := db.Where("installation_id = ? AND request_digest = ?", fixture.receipt.InstallationID, fixture.receipt.RequestDigest).Delete(&admissionEconomicContributionV119{}).Error; err != nil {
			t.Fatal(err)
		}
		var receipt admissionEconomicReceiptV119
		if err := db.Where("installation_id = ? AND request_digest = ?", fixture.receipt.InstallationID, fixture.receipt.RequestDigest).Take(&receipt).Error; err != nil || !bytes.Equal(receipt.IntentBody, fixture.receipt.IntentBody) {
			t.Fatal("receipt lost with projection", err)
		}
		var invalid admissionEconomicInvalidBoundV119
		if err := db.Where("installation_id = ? AND revision_digest = ?", fixture.invalid.InstallationID, fixture.invalid.RevisionDigest).Take(&invalid).Error; err != nil || invalid.Revision != fixture.invalid.Revision {
			t.Fatal("expiry restored invalid bound", err)
		}
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error {
			return tx.Where("installation_id = ? AND request_digest = ?", fixture.receipt.InstallationID, fixture.receipt.RequestDigest).Delete(&admissionEconomicReceiptV119{}).Error
		})
	})
	t.Run("nonempty_partial_ddl_fails_closed", func(t *testing.T) {
		model := &admissionEconomicCounterV119{}
		if err := db.Migrator().DropConstraint(model, "ck_economic_counter_body"); err != nil {
			t.Fatal(err)
		}
		if err := db.Migrator().DropColumn(model, "CounterBody"); err != nil {
			t.Fatal(err)
		}
		if err := applyEconomicCandidateV119(t.Context(), db); err == nil {
			t.Fatal("missing populated projection body was synthesized")
		}
		if db.Migrator().HasColumn(model, "CounterBody") {
			t.Fatal("failed repair wrote a new history body")
		}
	})
}

// Candidate-only equivalent of the existing runner's same-connection lock. It
// writes no numbered ledger row and is not production registration evidence.
func applyEconomicCandidateV119(ctx context.Context, db *gorm.DB) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) (result error) {
		conn = conn.Session(&gorm.Session{NewDB: true})
		release := "SELECT pg_advisory_unlock(724683901)"
		switch conn.Name() {
		case "postgres":
			if err := conn.Exec("SELECT pg_advisory_lock(724683901)").Error; err != nil {
				return err
			}
		case "mysql":
			var locked int64
			wait, err := migrationLockWaitSeconds(ctx)
			if err != nil {
				return err
			}
			if err := conn.Raw("SELECT GET_LOCK('routex_schema_migrations', ?)", wait).Scan(&locked).Error; err != nil {
				return err
			}
			if locked != 1 {
				return fmt.Errorf("economic candidate migration lock unavailable")
			}
			release = "SELECT RELEASE_LOCK('routex_schema_migrations')"
		default:
			return fmt.Errorf("unsupported economic schema driver")
		}
		defer func() {
			releaseCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := conn.WithContext(releaseCtx).Exec(release).Error; err != nil {
				result = errors.Join(result, err)
				if sqlConn, ok := conn.Statement.ConnPool.(*sql.Conn); ok {
					// Never pool a session that may still own the migration lock.
					_ = sqlConn.Raw(func(any) error { return driver.ErrBadConn })
				}
			}
		}()
		return admissionEconomicMigration(conn)
	})
}

type economicSchemaStateV119 struct {
	domain       admissionEconomicDomainV119
	receipt      admissionEconomicReceiptV119
	attempt      admissionEconomicAttemptV119
	account      admissionEconomicAccountV119
	counter      admissionEconomicCounterV119
	contribution admissionEconomicContributionV119
	invalid      admissionEconomicInvalidBoundV119
}

func economicDigestV119(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func economicSchemaFixtureV119(t *testing.T, db *gorm.DB) economicSchemaStateV119 {
	t.Helper()
	born := time.Date(2026, 10, 1, 0, 0, 0, 123456000, time.UTC)
	instant := born.Add(time.Hour).UnixNano()
	installation := "led_01jjjjjjjjjjjjjjjjjjjjjjjj"
	terminalDigest, checkpointDigest := economicDigestV119("terminal"), economicDigestV119("checkpoint")
	state := economicSchemaStateV119{
		domain:  admissionEconomicDomainV119{InstallationID: installation, FormatVersion: 1, Initialized: true, CoverageStart: born.UnixNano(), ClockHighWater: instant, ReceiptCapacity: 16, ReceiptCount: 1},
		receipt: admissionEconomicReceiptV119{InstallationID: installation, RequestDigest: economicDigestV119("Request_A"), RequestID: "Request_A", ExecutionID: "Execution_A", ExecutionDigest: economicDigestV119("Execution_A"), ReceiptID: "Receipt_A", ReceiptDigest: economicDigestV119("Receipt_A"), SpoolID: "spool_A", IncarnationID: "incarnation_A", ModeRevision: 1, Fence: 1, OwnerStartedAt: born, AdmissionDigest: economicDigestV119("intent"), IntentBody: []byte(`{"original":"admission"}`), AdmittedAt: instant, MonthStart: born.Truncate(time.Second).UnixNano(), MonthEnd: born.AddDate(0, 1, 0).UnixNano(), TerminalDigest: &terminalDigest, TerminalBody: []byte(`{"original":"terminal"}`)},
		account: admissionEconomicAccountV119{InstallationID: installation, AccountDigest: economicDigestV119("user_A"), Account: "user_A", Birth: born, RPMCount: 1, ExecutorCount: 0},
	}
	state.attempt = admissionEconomicAttemptV119{InstallationID: installation, RequestDigest: state.receipt.RequestDigest, RequestID: state.receipt.RequestID, Number: 1, AttemptID: "Attempt_A", AttemptIdentityDigest: economicDigestV119("Attempt_A"), AttemptDigest: economicDigestV119("dispatch"), DispatchBody: []byte(`{"original":"dispatch"}`), CheckpointDigest: &checkpointDigest, CheckpointBody: []byte(`{"original":"checkpoint"}`)}
	state.counter = admissionEconomicCounterV119{InstallationID: installation, AccountDigest: state.account.AccountDigest, Window: "minute", CounterBody: []byte(`{"tokens_used":0,"tokens_held":1,"tokens_unknown":0,"money_used":{},"money_held":{"USD":"1000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},"money_unknown":0}`)}
	expires := instant + int64(time.Minute)
	state.contribution = admissionEconomicContributionV119{InstallationID: installation, RequestDigest: state.receipt.RequestDigest, RequestID: state.receipt.RequestID, AccountDigest: state.account.AccountDigest, Kind: "terminal_quota", Window: "minute", ExpiresAt: &expires, ContributionBody: []byte(`{"bound":{},"actual":{},"settled":true}`)}
	state.invalid = admissionEconomicInvalidBoundV119{InstallationID: installation, RevisionDigest: economicDigestV119("bound_A"), Revision: "bound_A", RequestDigest: state.receipt.RequestDigest, RequestID: state.receipt.RequestID, RecordedAt: instant}
	for _, model := range []any{&state.domain, &state.receipt, &state.account, &state.attempt, &state.counter, &state.contribution, &state.invalid} {
		if err := db.Omit("Domain", "Receipt", "AccountRecord").Create(model).Error; err != nil {
			t.Fatal("explicit synthetic schema fixture", err)
		}
	}
	return state
}

func economicSchemaAssertFixtureV119(t *testing.T, db *gorm.DB, state economicSchemaStateV119) {
	t.Helper()
	for _, check := range []struct {
		model    any
		where    string
		args     []any
		expected any
	}{
		{&admissionEconomicReceiptV119{}, "installation_id = ? AND request_digest = ?", []any{state.receipt.InstallationID, state.receipt.RequestDigest}, state.receipt},
		{&admissionEconomicAttemptV119{}, "installation_id = ? AND request_digest = ? AND number = ?", []any{state.attempt.InstallationID, state.attempt.RequestDigest, state.attempt.Number}, state.attempt},
		{&admissionEconomicAccountV119{}, "installation_id = ? AND account_digest = ?", []any{state.account.InstallationID, state.account.AccountDigest}, state.account},
		{&admissionEconomicCounterV119{}, "installation_id = ? AND account_digest = ? AND window_name = ?", []any{state.counter.InstallationID, state.counter.AccountDigest, state.counter.Window}, state.counter},
	} {
		if err := db.Where(check.where, check.args...).Take(check.model).Error; err != nil {
			t.Fatal(err)
		}
		// Serialize neither private history nor huge money into failure output.
		if !economicSchemaEqualV119(check.model, check.expected) {
			t.Fatal("exact retained schema fixture changed")
		}
	}
}

func economicSchemaRejectV119(t *testing.T, db *gorm.DB, write func(*gorm.DB) error) {
	t.Helper()
	if err := db.Transaction(write); err == nil {
		t.Fatal("invalid schema write was accepted")
	}
}

type economicWrongExecutionIndexV119 struct {
	InstallationID string `gorm:"size:30;uniqueIndex:uidx_economic_execution,priority:1"`
	RequestDigest  string `gorm:"size:64;uniqueIndex:uidx_economic_execution,priority:2"`
}

func (economicWrongExecutionIndexV119) TableName() string { return "admission_economic_receipts" }

func economicSchemaEqualV119(actual any, expected any) bool {
	switch row := actual.(type) {
	case *admissionEconomicReceiptV119:
		row.OwnerStartedAt = row.OwnerStartedAt.UTC()
	case *admissionEconomicAccountV119:
		row.Birth = row.Birth.UTC()
	}
	return reflect.DeepEqual(reflect.ValueOf(actual).Elem().Interface(), expected)
}

func economicSchemaConstraintsV119(t *testing.T, db *gorm.DB, state economicSchemaStateV119) {
	t.Helper()
	domainWhere := map[string]any{"installation_id": state.domain.InstallationID}
	receiptWhere := map[string]any{"installation_id": state.receipt.InstallationID, "request_digest": state.receipt.RequestDigest}
	attemptWhere := map[string]any{"installation_id": state.attempt.InstallationID, "request_digest": state.attempt.RequestDigest, "number": state.attempt.Number}
	accountWhere := map[string]any{"installation_id": state.account.InstallationID, "account_digest": state.account.AccountDigest}
	contributionWhere := map[string]any{"installation_id": state.contribution.InstallationID, "request_digest": state.contribution.RequestDigest, "account_digest": state.contribution.AccountDigest, "kind": state.contribution.Kind, "window_name": state.contribution.Window}
	invalidWhere := map[string]any{"installation_id": state.invalid.InstallationID, "revision_digest": state.invalid.RevisionDigest}
	tests := []struct {
		name   string
		model  any
		where  map[string]any
		values map[string]any
	}{
		{"format_version", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"format_version": 2}},
		{"zero_capacity", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"receipt_capacity": 0}},
		{"negative_receipt_count", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"receipt_count": -1}},
		{"capacity_exhaustion", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"receipt_count": 17}},
		{"clock_before_coverage", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"clock_high_water": state.domain.CoverageStart - 1}},
		{"missing_initialized_coverage", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"coverage_start": 0}},
		{"uninitialized_with_facts", &admissionEconomicDomainV119{}, domainWhere, map[string]any{"initialized": false}},
		{"negative_rpm", &admissionEconomicAccountV119{}, accountWhere, map[string]any{"rpm_count": -1}},
		{"negative_executor", &admissionEconomicAccountV119{}, accountWhere, map[string]any{"executor_count": -1}},
		{"zero_owner_revision", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"mode_revision": 0}},
		{"zero_owner_fence", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"fence": 0}},
		{"empty_intent", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"intent_body": []byte{}}},
		{"oversized_intent", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"intent_body": make([]byte, 131073)}},
		{"terminal_digest_without_body", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"terminal_body": nil}},
		{"terminal_body_without_digest", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"terminal_digest": nil}},
		{"empty_terminal", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"terminal_body": []byte{}}},
		{"invalid_month", &admissionEconomicReceiptV119{}, receiptWhere, map[string]any{"month_end": state.receipt.AdmittedAt}},
		{"fifth_attempt", &admissionEconomicAttemptV119{}, attemptWhere, map[string]any{"number": 5}},
		{"empty_dispatch", &admissionEconomicAttemptV119{}, attemptWhere, map[string]any{"dispatch_body": []byte{}}},
		{"checkpoint_digest_without_body", &admissionEconomicAttemptV119{}, attemptWhere, map[string]any{"checkpoint_body": nil}},
		{"checkpoint_body_without_digest", &admissionEconomicAttemptV119{}, attemptWhere, map[string]any{"checkpoint_digest": nil}},
		{"oversized_checkpoint", &admissionEconomicAttemptV119{}, attemptWhere, map[string]any{"checkpoint_body": make([]byte, 131073)}},
		{"negative_expiry", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"expires_at": -1}},
		{"terminal_without_expiry", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"expires_at": nil}},
		{"active_hold_with_expiry", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"kind": "active_quota"}},
		{"executor_with_expiry", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"kind": "executor"}},
		{"upper_case_kind", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"kind": "TERMINAL_QUOTA"}},
		{"padded_kind", &admissionEconomicContributionV119{}, contributionWhere, map[string]any{"kind": "terminal_quota "}},
		{"zero_invalid_bound_time", &admissionEconomicInvalidBoundV119{}, invalidWhere, map[string]any{"recorded_at": 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Model(test.model).Where(test.where).Updates(test.values).Error })
		})
	}
	t.Run("required_account_birth", func(t *testing.T) {
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error {
			return tx.Model(&admissionEconomicAccountV119{}).Where(accountWhere).Update("birth", nil).Error
		})
	})
	t.Run("restrict_domain_history_delete", func(t *testing.T) {
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Where(domainWhere).Delete(&admissionEconomicDomainV119{}).Error })
	})
	t.Run("restrict_account_projection_delete", func(t *testing.T) {
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Where(accountWhere).Delete(&admissionEconomicAccountV119{}).Error })
	})
	t.Run("missing_receipt_reference", func(t *testing.T) {
		row := state.attempt
		row.RequestID = "MissingRequest"
		row.RequestDigest = economicDigestV119(row.RequestID)
		row.AttemptID = "MissingAttempt"
		row.AttemptIdentityDigest = economicDigestV119(row.AttemptID)
		economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit("Receipt").Create(&row).Error })
	})
	economicSchemaAssertFixtureV119(t, db, state)
}

func economicSchemaCaseIdentitiesV119(t *testing.T, db *gorm.DB, state economicSchemaStateV119) {
	t.Helper()
	receipt := state.receipt
	receipt.RequestID = "request_a"
	receipt.RequestDigest = economicDigestV119(receipt.RequestID)
	receipt.ExecutionID = "execution_a"
	receipt.ExecutionDigest = economicDigestV119(receipt.ExecutionID)
	receipt.ReceiptID = "receipt_a"
	receipt.ReceiptDigest = economicDigestV119(receipt.ReceiptID)
	if err := db.Omit("Domain").Create(&receipt).Error; err != nil {
		t.Fatal("case-distinct request/execution/receipt identities collided", err)
	}
	attempt := state.attempt
	attempt.RequestID = receipt.RequestID
	attempt.RequestDigest = receipt.RequestDigest
	attempt.AttemptID = "attempt_a"
	attempt.AttemptIdentityDigest = economicDigestV119(attempt.AttemptID)
	if err := db.Omit("Receipt").Create(&attempt).Error; err != nil {
		t.Fatal("case-distinct attempt identities collided", err)
	}
	account := state.account
	account.Account = "user_a"
	account.AccountDigest = economicDigestV119(account.Account)
	if err := db.Omit("Domain").Create(&account).Error; err != nil {
		t.Fatal("case-distinct account identities collided", err)
	}
	invalid := state.invalid
	invalid.Revision = "bound_a"
	invalid.RevisionDigest = economicDigestV119(invalid.Revision)
	if err := db.Omit("Receipt").Create(&invalid).Error; err != nil {
		t.Fatal("case-distinct revision identities collided", err)
	}
	for _, original := range []admissionEconomicReceiptV119{state.receipt, receipt} {
		var actual admissionEconomicReceiptV119
		if err := db.Where("installation_id = ? AND request_digest = ?", original.InstallationID, original.RequestDigest).Take(&actual).Error; err != nil || actual.RequestID != original.RequestID || actual.ExecutionID != original.ExecutionID || actual.ReceiptID != original.ReceiptID {
			t.Fatal("exact original request bytes lost", err)
		}
	}
	duplicate := receipt
	duplicate.RequestID = "different_request"
	duplicate.RequestDigest = economicDigestV119(duplicate.RequestID)
	duplicate.ReceiptID = "different_receipt"
	duplicate.ReceiptDigest = economicDigestV119(duplicate.ReceiptID)
	economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit("Domain").Create(&duplicate).Error })
	duplicate = receipt
	duplicate.RequestID = "another_request"
	duplicate.RequestDigest = economicDigestV119(duplicate.RequestID)
	duplicate.ExecutionID = "different_execution"
	duplicate.ExecutionDigest = economicDigestV119(duplicate.ExecutionID)
	economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit("Domain").Create(&duplicate).Error })
	duplicateAttempt := attempt
	duplicateAttempt.Number = 2
	economicSchemaRejectV119(t, db, func(tx *gorm.DB) error { return tx.Omit("Receipt").Create(&duplicateAttempt).Error })
}
