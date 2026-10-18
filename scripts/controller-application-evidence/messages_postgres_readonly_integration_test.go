//go:build integration && routex_messages_readonly

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

var messagesPostgresControlRejected = errors.New("Messages PostgreSQL read-only control rejected")

func messagesPostgresTypedReadOnly(err error) bool {
	var typed *pgconn.PgError
	return err != nil && errors.As(err, &typed) && typed != nil && typed.Code == "25006"
}
func messagesPostgresCheckedRollback(step func() error, rollback func() error) (err error) {
	defer func() {
		if rollback() != nil {
			err = messagesPostgresControlRejected
		}
	}()
	return step()
}
func messagesPostgresRefusedWrite(ctx context.Context, db *gorm.DB, owner, keyID string, observe func(*gorm.DB) error) error {
	if db == nil || db.Error != nil || ctx.Err() != nil || observe == nil || (owner != "personal" && owner != "project") {
		return messagesPostgresControlRejected
	}
	prefix, table := "key", "api_keys"
	if owner == "project" {
		prefix, table = "pky", "project_api_keys"
	}
	if !validID(keyID, prefix) {
		return messagesPostgresControlRejected
	}
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return messagesPostgresControlRejected
	}
	err := messagesPostgresCheckedRollback(func() error {
		// GORM has no portable API for server transaction settings. This test is
		// deliberately PostgreSQL-only; inspect fixed settings without secrets.
		var settings struct {
			ReadOnly  string `gorm:"column:read_only"`
			Isolation string `gorm:"column:isolation"`
		}
		if tx.Raw("SELECT current_setting('transaction_read_only') AS read_only, current_setting('transaction_isolation') AS isolation").Scan(&settings).Error != nil || settings.ReadOnly != "on" || settings.Isolation != "repeatable read" {
			return messagesPostgresControlRejected
		}
		if observe(tx) != nil || ctx.Err() != nil {
			return messagesPostgresControlRejected
		}
		// The exact same canonical ID cannot satisfy equality and inequality. Even
		// an unexpected write acceptance cannot modify any existing Key. PostgreSQL
		// must nevertheless reject the UPDATE statement itself with typed 25006.
		writeErr := tx.Table(table).Where("id = ?", keyID).Where("id <> ?", keyID).UpdateColumn("status", "active").Error
		if !messagesPostgresTypedReadOnly(writeErr) || ctx.Err() != nil {
			return messagesPostgresControlRejected
		}
		return nil
	}, func() error { return tx.Rollback().Error })
	if err != nil || ctx.Err() != nil {
		return messagesPostgresControlRejected
	}
	return nil
}
func messagesPostgresRequireNativeStage(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || os.Getuid() <= 0 || os.Getuid() != os.Geteuid() {
		t.Fatal("requires fresh owned Linux non-root native input stage")
	}
}
func messagesPostgresOpenTest(t *testing.T, driver, dsn string) (*gorm.DB, *sql.DB) {
	t.Helper()
	db, err := openEvidenceDatabase(driver, dsn)
	if err != nil {
		if db != nil {
			if pool, e := db.DB(); e == nil {
				_ = pool.Close()
			}
		}
		t.Fatal("fixed PostgreSQL connection rejected")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("fixed PostgreSQL pool rejected")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = pool.Close() })
	return db, pool
}
func TestPostgresMessagesOnceExactProjectionReadOnlyIntegration(t *testing.T) {
	messagesPostgresRequireNativeStage(t)
	raw, err := privateRead("/fixture-evidence-postgres-messages-once-test/config.json", 8192, true)
	if err != nil {
		t.Fatal("protected PostgreSQL Messages once input rejected")
	}
	defer clear(raw)
	var c messagesOnceConfig
	if !decodeMessagesOnce(raw, &c) || (c.DatabaseDriver != "" && c.DatabaseDriver != "postgres") {
		t.Fatal("PostgreSQL Messages once expectations rejected")
	}
	dsn, ok := prepareMessagesOnceConnection(&c)
	if !ok || !validMessagesOnceExpectations(c) {
		t.Fatal("fixed PostgreSQL Messages once connection rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, pool := messagesPostgresOpenTest(t, c.DatabaseDriver, dsn)
	dsn = ""
	var before []messagesOnceProjection
	err = messagesPostgresRefusedWrite(ctx, db, c.OwnerKind, c.KeyID, func(tx *gorm.DB) error {
		var readErr error
		before, readErr = queryMessagesOnce(tx, c)
		if readErr != nil || !validMessagesOnceProjection(c, before) {
			return messagesPostgresControlRejected
		}
		return nil
	})
	if err != nil {
		t.Fatal("PostgreSQL Messages once typed refusal or rollback unconfirmed")
	}
	// The refused PostgreSQL transaction is aborted; re-reading inside it would
	// only produce 25P02. A new checked read-only transaction observes the proof
	// after confirmed rollback, using the maintained reader and unchanged config.
	after, err := readMessagesOnce(ctx, db, c)
	if err != nil || !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		t.Fatal("exact PostgreSQL Messages once projection changed or unknown")
	}
	if pool.Close() != nil || ctx.Err() != nil {
		t.Fatal("PostgreSQL Messages once pool closure or deadline unknown")
	}
	receipt, err := json.Marshal(struct {
		Result       string                   `json:"result"`
		Driver       string                   `json:"database_driver"`
		SourceCommit string                   `json:"source_commit"`
		RunID        string                   `json:"run_id"`
		Owner        string                   `json:"owner_kind"`
		SQLState     string                   `json:"sqlstate"`
		Rollback     bool                     `json:"rollback_confirmed"`
		Before       []messagesOnceProjection `json:"before"`
		After        []messagesOnceProjection `json:"after"`
	}{"verified_postgres_readonly_messages_once", "postgres", c.SourceCommit, c.RunID, c.OwnerKind, "25006", true, before, after})
	if err != nil || len(receipt) > 16384 {
		t.Fatal("bounded PostgreSQL Messages once receipt rejected")
	}
	t.Logf("postgres_messages_once_receipt=%s", receipt)
}
func TestPostgresMessagesPairExactProjectionReadOnlyIntegration(t *testing.T) {
	messagesPostgresRequireNativeStage(t)
	raw, err := privateRead("/fixture-evidence-postgres-messages-pair-test/config.json", 16384, true)
	if err != nil {
		t.Fatal("protected PostgreSQL Messages pair input rejected")
	}
	defer clear(raw)
	var c messagesPairConfig
	if !decodeMessagesPair(raw, &c) || (c.DatabaseDriver != "" && c.DatabaseDriver != "postgres") {
		t.Fatal("PostgreSQL Messages pair expectations rejected")
	}
	dsn, ok := prepareMessagesPairConnection(&c)
	if !ok || !validMessagesPairExpectations(c) {
		t.Fatal("fixed PostgreSQL Messages pair connection rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, pool := messagesPostgresOpenTest(t, c.DatabaseDriver, dsn)
	dsn = ""
	var before []messagesPairProjection
	err = messagesPostgresRefusedWrite(ctx, db, c.OwnerKind, c.KeyID, func(tx *gorm.DB) error {
		readErr := messagesPairSelection(tx, c).Scan(&before).Error
		if readErr != nil || !validMessagesPairProjection(c, before) {
			return messagesPostgresControlRejected
		}
		return nil
	})
	if err != nil {
		t.Fatal("PostgreSQL Messages pair typed refusal or rollback unconfirmed")
	}
	after, err := readMessagesPair(ctx, db, c)
	// SQL scan order is not an immutable fact. Use the retained first proof,
	// rather than lexical RequestID order, to align both complete projections.
	for _, rows := range [][]messagesPairProjection{before, after} {
		if len(rows) == 2 && rows[0].RequestID != c.FirstAccepted.Proof.RequestID {
			rows[0], rows[1] = rows[1], rows[0]
		}
	}
	if err != nil || !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		t.Fatal("exact PostgreSQL Messages pair projection changed or unknown")
	}
	if pool.Close() != nil || ctx.Err() != nil {
		t.Fatal("PostgreSQL Messages pair pool closure or deadline unknown")
	}
	receipt, err := json.Marshal(struct {
		Result       string                   `json:"result"`
		Driver       string                   `json:"database_driver"`
		SourceCommit string                   `json:"source_commit"`
		RunID        string                   `json:"run_id"`
		Owner        string                   `json:"owner_kind"`
		SQLState     string                   `json:"sqlstate"`
		Rollback     bool                     `json:"rollback_confirmed"`
		Before       []messagesPairProjection `json:"before"`
		After        []messagesPairProjection `json:"after"`
	}{"verified_postgres_readonly_messages_pair", "postgres", c.SourceCommit, c.RunID, c.OwnerKind, "25006", true, before, after})
	if err != nil || len(receipt) > 32768 {
		t.Fatal("bounded PostgreSQL Messages pair receipt rejected")
	}
	t.Logf("postgres_messages_pair_receipt=%s", receipt)
}
func TestMessagesPostgresControlRequiresTyped25006(t *testing.T) {
	good := &pgconn.PgError{Code: "25006", Message: "SYNTHETIC_PRIVATE_ERROR"}
	translated := (&postgres.Dialector{}).Translate(good)
	if translated != good || !messagesPostgresTypedReadOnly(translated) || !messagesPostgresTypedReadOnly(fmt.Errorf("wrapped: %w", good)) {
		t.Fatal("pinned typed PostgreSQL refusal changed")
	}
	for _, bad := range []error{nil, errors.New("SQLSTATE 25006 SYNTHETIC_PRIVATE_ERROR"), &pgconn.PgError{Code: "25P02"}, &pgconn.PgError{Code: "08006"}, context.Canceled, context.DeadlineExceeded} {
		if messagesPostgresTypedReadOnly(bad) {
			t.Fatal("unrelated refusal or connection error accepted")
		}
	}
}
func TestMessagesPostgresControlRollbackAlwaysRunsAndUnknownFails(t *testing.T) {
	for _, stepErr := range []error{nil, messagesPostgresControlRejected} {
		for _, rollbackErr := range []error{nil, messagesPostgresControlRejected} {
			calls := 0
			err := messagesPostgresCheckedRollback(func() error { return stepErr }, func() error { calls++; return rollbackErr })
			if calls != 1 || (err == nil) != (stepErr == nil && rollbackErr == nil) {
				t.Fatal("rollback skipped or unknown accepted")
			}
		}
	}
	calls := 0
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic silently accepted")
			}
		}()
		_ = messagesPostgresCheckedRollback(func() error { panic("synthetic step") }, func() error { calls++; return nil })
	}()
	if calls != 1 {
		t.Fatal("panic skipped rollback")
	}
}
