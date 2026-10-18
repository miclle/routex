//go:build integration && routex_messages_mysql_readonly

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var messagesMySQLControlRejected = errors.New("Messages MySQL read-only control rejected")

func messagesMySQLTypedReadOnly(err error) bool {
	var typed *mysqldriver.MySQLError
	return err != nil && errors.As(err, &typed) && typed != nil && typed.Number == 1792 && typed.SQLState == [5]byte{'2', '5', '0', '0', '6'}
}
func messagesMySQLCheckedRollback(step func() error, rollback func() error) (err error) {
	defer func() {
		if rollback() != nil {
			err = messagesMySQLControlRejected
		}
	}()
	return step()
}
func messagesMySQLRefusedWrite(ctx context.Context, db *gorm.DB, owner, keyID string, observe func(*gorm.DB) error) error {
	if db == nil || db.Error != nil || ctx.Err() != nil || observe == nil || (owner != "personal" && owner != "project") {
		return messagesMySQLControlRejected
	}
	prefix, table := "key", "api_keys"
	if owner == "project" {
		prefix, table = "pky", "project_api_keys"
	}
	if !validID(keyID, prefix) {
		return messagesMySQLControlRejected
	}
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return messagesMySQLControlRejected
	}
	err := messagesMySQLCheckedRollback(func() error {
		// Explicit TxOptions above reach the pinned mysql driver's SET TRANSACTION
		// ISOLATION LEVEL REPEATABLE READ and START TRANSACTION READ ONLY. Do not
		// authenticate this current transaction with session/default @@ variables:
		// the actual zero-match UPDATE must receive typed server1792/25006.
		if observe(tx) != nil || ctx.Err() != nil {
			return messagesMySQLControlRejected
		}
		// The exact same canonical ID cannot satisfy equality and inequality. Even
		// an unexpected write acceptance cannot modify any existing Key. MySQL
		// must nevertheless reject the UPDATE statement itself with typed1792/25006.
		writeErr := tx.Table(table).Where("id = ?", keyID).Where("id <> ?", keyID).UpdateColumn("status", "active").Error
		if !messagesMySQLTypedReadOnly(writeErr) || ctx.Err() != nil {
			return messagesMySQLControlRejected
		}
		return nil
	}, func() error { return tx.Rollback().Error })
	if err != nil || ctx.Err() != nil {
		return messagesMySQLControlRejected
	}
	return nil
}
func messagesMySQLRequireNativeStage(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" || os.Getuid() <= 0 || os.Getuid() != os.Geteuid() {
		t.Fatal("requires fresh owned Linux non-root native input stage")
	}
}
func messagesMySQLOpenTest(t *testing.T, driver, dsn string) (*gorm.DB, *sql.DB) {
	t.Helper()
	db, err := openEvidenceDatabase(driver, dsn)
	if err != nil {
		if db != nil {
			if pool, e := db.DB(); e == nil {
				_ = pool.Close()
			}
		}
		t.Fatal("fixed MySQL connection rejected")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("fixed MySQL pool rejected")
	}
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	t.Cleanup(func() { _ = pool.Close() })
	return db, pool
}
func TestMySQLMessagesOnceExactProjectionReadOnlyIntegration(t *testing.T) {
	messagesMySQLRequireNativeStage(t)
	raw, err := privateRead("/fixture-evidence-mysql-messages-once-test/config.json", 8192, true)
	if err != nil {
		t.Fatal("protected MySQL Messages once input rejected")
	}
	defer clear(raw)
	var c messagesOnceConfig
	if !decodeMessagesOnce(raw, &c) || c.DatabaseDriver != "mysql" {
		t.Fatal("MySQL Messages once expectations rejected")
	}
	dsn, ok := prepareMessagesOnceConnection(&c)
	if !ok || !validMessagesOnceExpectations(c) {
		t.Fatal("fixed MySQL Messages once connection rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, pool := messagesMySQLOpenTest(t, c.DatabaseDriver, dsn)
	dsn = ""
	var before []messagesOnceProjection
	err = messagesMySQLRefusedWrite(ctx, db, c.OwnerKind, c.KeyID, func(tx *gorm.DB) error {
		var readErr error
		before, readErr = queryMessagesOnce(tx, c)
		if readErr != nil || !validMessagesOnceProjection(c, before) {
			return messagesMySQLControlRejected
		}
		return nil
	})
	if err != nil {
		t.Fatal("MySQL Messages once typed refusal or rollback unconfirmed")
	}
	// After confirmed rollback, a distinct checked read-only transaction reads
	// the entire unchanged proof. Do not reuse the refused transaction for the
	// after snapshot, even when MySQL permits further reads in that transaction.
	after, err := readMessagesOnce(ctx, db, c)
	if err != nil || !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		t.Fatal("exact MySQL Messages once projection changed or unknown")
	}
	if pool.Close() != nil || ctx.Err() != nil {
		t.Fatal("MySQL Messages once pool closure or deadline unknown")
	}
	receipt, err := json.Marshal(struct {
		Result       string                   `json:"result"`
		Driver       string                   `json:"database_driver"`
		SourceCommit string                   `json:"source_commit"`
		RunID        string                   `json:"run_id"`
		Owner        string                   `json:"owner_kind"`
		MySQLError   uint16                   `json:"mysql_error"`
		SQLState     string                   `json:"sqlstate"`
		Rollback     bool                     `json:"rollback_confirmed"`
		Before       []messagesOnceProjection `json:"before"`
		After        []messagesOnceProjection `json:"after"`
	}{"verified_mysql_readonly_messages_once", "mysql", c.SourceCommit, c.RunID, c.OwnerKind, 1792, "25006", true, before, after})
	if err != nil || len(receipt) > 16384 {
		t.Fatal("bounded MySQL Messages once receipt rejected")
	}
	t.Logf("mysql_messages_once_receipt=%s", receipt)
}
func TestMySQLMessagesPairExactProjectionReadOnlyIntegration(t *testing.T) {
	messagesMySQLRequireNativeStage(t)
	raw, err := privateRead("/fixture-evidence-mysql-messages-pair-test/config.json", 16384, true)
	if err != nil {
		t.Fatal("protected MySQL Messages pair input rejected")
	}
	defer clear(raw)
	var c messagesPairConfig
	if !decodeMessagesPair(raw, &c) || c.DatabaseDriver != "mysql" {
		t.Fatal("MySQL Messages pair expectations rejected")
	}
	dsn, ok := prepareMessagesPairConnection(&c)
	if !ok || !validMessagesPairExpectations(c) {
		t.Fatal("fixed MySQL Messages pair connection rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, pool := messagesMySQLOpenTest(t, c.DatabaseDriver, dsn)
	dsn = ""
	var before []messagesPairProjection
	err = messagesMySQLRefusedWrite(ctx, db, c.OwnerKind, c.KeyID, func(tx *gorm.DB) error {
		readErr := messagesPairSelection(tx, c).Scan(&before).Error
		if readErr != nil || !validMessagesPairProjection(c, before) {
			return messagesMySQLControlRejected
		}
		return nil
	})
	if err != nil {
		t.Fatal("MySQL Messages pair typed refusal or rollback unconfirmed")
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
		t.Fatal("exact MySQL Messages pair projection changed or unknown")
	}
	if pool.Close() != nil || ctx.Err() != nil {
		t.Fatal("MySQL Messages pair pool closure or deadline unknown")
	}
	receipt, err := json.Marshal(struct {
		Result       string                   `json:"result"`
		Driver       string                   `json:"database_driver"`
		SourceCommit string                   `json:"source_commit"`
		RunID        string                   `json:"run_id"`
		Owner        string                   `json:"owner_kind"`
		MySQLError   uint16                   `json:"mysql_error"`
		SQLState     string                   `json:"sqlstate"`
		Rollback     bool                     `json:"rollback_confirmed"`
		Before       []messagesPairProjection `json:"before"`
		After        []messagesPairProjection `json:"after"`
	}{"verified_mysql_readonly_messages_pair", "mysql", c.SourceCommit, c.RunID, c.OwnerKind, 1792, "25006", true, before, after})
	if err != nil || len(receipt) > 32768 {
		t.Fatal("bounded MySQL Messages pair receipt rejected")
	}
	t.Logf("mysql_messages_pair_receipt=%s", receipt)
}
func TestMessagesMySQLControlRequiresTyped1792And25006(t *testing.T) {
	good := &mysqldriver.MySQLError{Number: 1792, SQLState: [5]byte{'2', '5', '0', '0', '6'}, Message: "SYNTHETIC_PRIVATE_ERROR"}
	translated := (&gormmysql.Dialector{}).Translate(good)
	if translated != good || !messagesMySQLTypedReadOnly(translated) || !messagesMySQLTypedReadOnly(fmt.Errorf("wrapped: %w", good)) {
		t.Fatal("pinned typed MySQL refusal changed")
	}
	for _, bad := range []error{nil, driver.ErrBadConn, errors.New("1792 SQLSTATE 25006 SYNTHETIC_PRIVATE_ERROR"), &mysqldriver.MySQLError{Number: 1045, SQLState: good.SQLState}, &mysqldriver.MySQLError{Number: 1792, SQLState: [5]byte{'H', 'Y', '0', '0', '0'}}, context.Canceled, context.DeadlineExceeded} {
		if messagesMySQLTypedReadOnly(bad) {
			t.Fatal("unrelated refusal or connection error accepted")
		}
	}
}

func TestMessagesMySQLControlRollbackAlwaysRunsAndUnknownFails(t *testing.T) {
	for _, stepErr := range []error{nil, messagesMySQLControlRejected} {
		for _, rollbackErr := range []error{nil, messagesMySQLControlRejected} {
			calls := 0
			err := messagesMySQLCheckedRollback(func() error { return stepErr }, func() error { calls++; return rollbackErr })
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
		_ = messagesMySQLCheckedRollback(func() error { panic("synthetic step") }, func() error { calls++; return nil })
	}()
	if calls != 1 {
		t.Fatal("panic skipped rollback")
	}
}
