//go:build integration && routex_mysql_readonly

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

	mysqldriver "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// This explicit isolated test is excluded from ordinary integration. Supply a
// fresh exact native pair in an owned MySQL namespace; no discovery or seed runs.
var mysqlPairRefused = errors.New("private MySQL pair proof rejected")

func mysqlPairReadOnlyRefusal(err error) bool {
	var typed *mysqldriver.MySQLError
	return err != nil && errors.As(err, &typed) && typed != nil &&
		typed.Number == 1792 && typed.SQLState == [5]byte{'2', '5', '0', '0', '6'}
}

// Every post-BEGIN path uses this same closure. Even unexpected UPDATE success,
// projection refusal or panic invokes rollback; unknown rollback is never proof.
func mysqlPairWithRollback(step func() error, rollback func() error) (err error) {
	defer func() {
		if rollback() != nil {
			err = mysqlPairRefused
		}
	}()
	return step()
}

func mysqlPairSnapshot(ctx context.Context, db *gorm.DB, c config) ([]projection, error) {
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return nil, mysqlPairRefused
	}
	var rows []projection
	err := mysqlPairWithRollback(func() error {
		var readErr error
		rows, readErr = query(tx, c)
		if readErr != nil || !validProjection(c, rows) || ctx.Err() != nil {
			return mysqlPairRefused
		}
		return nil
	}, func() error { return tx.Rollback().Error })
	if err != nil || ctx.Err() != nil {
		return nil, mysqlPairRefused
	}
	return rows, nil
}

func mysqlPairProbe(ctx context.Context, db *gorm.DB, c config) ([]projection, error) {
	if !validExpectations(c) {
		return nil, mysqlPairRefused
	}
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return nil, mysqlPairRefused
	}
	var before []projection
	err := mysqlPairWithRollback(func() error {
		var readErr error
		before, readErr = query(tx, c)
		if readErr != nil || !validProjection(c, before) || ctx.Err() != nil {
			return mysqlPairRefused
		}
		writeTable := "api_keys"
		if c.OwnerKind == "project" {
			writeTable = "project_api_keys"
		}
		// Both IDs are distinct canonical same-owner Key IDs validated above.
		// Their conjunction cannot match a row. No real Key status can change,
		// even if the database unexpectedly accepts this read-only statement.
		writeErr := tx.Table(writeTable).Where("id = ?", c.KeyID).
			Where("id = ?", c.PredecessorID).UpdateColumn("status", "active").Error
		if !mysqlPairReadOnlyRefusal(writeErr) || ctx.Err() != nil {
			return mysqlPairRefused
		}
		return nil
	}, func() error { return tx.Rollback().Error })
	if err != nil || ctx.Err() != nil {
		return nil, mysqlPairRefused
	}
	return before, nil
}

func TestMySQLExactNativePairReadOnlyRefusalIntegration(t *testing.T) {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		t.Fatal("requires fresh owned Linux non-root native input stage")
	}
	raw, err := privateRead("/fixture-evidence-mysql-test/config.json", 8192, true)
	if err != nil {
		t.Fatal("protected exact MySQL pair input rejected")
	}
	defer clear(raw)
	var c config
	if !decodeConfig(raw, &c) || c.DatabaseDriver != "mysql" {
		t.Fatal("exact MySQL pair expectations rejected")
	}
	dsn, ok := prepareConnection(&c)
	if !ok || !validExpectations(c) {
		t.Fatal("fixed MySQL connection boundary rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	db, err := openEvidenceDatabase(c.DatabaseDriver, dsn)
	dsn = ""
	if err != nil {
		t.Fatal("fixed MySQL fixture connection rejected")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("fixed MySQL fixture pool rejected")
	}
	poolClosed := false
	defer func() {
		if !poolClosed {
			_ = pool.Close()
		}
	}()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	before, err := mysqlPairProbe(ctx, db, c)
	if err != nil {
		t.Fatal("MySQL typed refusal or rollback not confirmed")
	}
	// A distinct transaction observes the same actual immutable pair after
	// confirmed rollback. Source cannot assume zero-match UPDATE is refused.
	after, err := mysqlPairSnapshot(ctx, db, c)
	if err != nil || !reflect.DeepEqual(before, after) || ctx.Err() != nil {
		t.Fatal("exact MySQL native pair changed or became unknown")
	}
	closeErr := pool.Close()
	poolClosed = true
	if closeErr != nil || ctx.Err() != nil {
		t.Fatal("MySQL pool closure or proof deadline became unknown")
	}
	owner := c.OwnerKind
	if owner == "" {
		owner = "personal"
	}
	receipt, err := json.Marshal(struct {
		Result   string       `json:"result"`
		Driver   string       `json:"database_driver"`
		Owner    string       `json:"owner_kind"`
		Code     uint16       `json:"mysql_error"`
		State    string       `json:"sqlstate"`
		Rollback bool         `json:"rollback_confirmed"`
		Before   []projection `json:"before"`
		After    []projection `json:"after"`
	}{"verified_mysql_readonly_exact_pair", "mysql", owner, 1792, "25006", true, before, after})
	if err != nil || len(receipt) > 16384 {
		t.Fatal("bounded MySQL pair receipt rejected")
	}
	// Content-free canonical identity/birth/native facts only; never c, DSN,
	// credentials, error messages, request/answer text or secret hashes.
	t.Logf("mysql_pair_receipt=%s", receipt)
}

func TestMySQLReadOnlyPairErrorRequiresTyped1792AndExactState(t *testing.T) {
	good := &mysqldriver.MySQLError{Number: 1792, SQLState: [5]byte{'2', '5', '0', '0', '6'}, Message: "SYNTHETIC_PRIVATE_ERROR"}
	translator := gormmysql.Dialector{}
	translated := translator.Translate(good)
	if translated != good || !mysqlPairReadOnlyRefusal(translated) {
		t.Fatal("pinned GORM translation changed typed MySQL read-only refusal")
	}
	if !mysqlPairReadOnlyRefusal(good) || !mysqlPairReadOnlyRefusal(fmt.Errorf("private wrapper: %w", good)) {
		t.Fatal("exact typed MySQL refusal was not recognized")
	}
	for _, bad := range []error{nil, errors.New("1792 SQLSTATE25006 SYNTHETIC_PRIVATE_ERROR"),
		&mysqldriver.MySQLError{Number: 1045, SQLState: good.SQLState},
		&mysqldriver.MySQLError{Number: 1792, SQLState: [5]byte{'H', 'Y', '0', '0', '0'}},
		context.Canceled, context.DeadlineExceeded} {
		if mysqlPairReadOnlyRefusal(bad) {
			t.Fatal("unrelated or missing error accepted as read-only proof")
		}
	}
}

func TestMySQLReadOnlyPairRollbackAlwaysRunsAndUnknownNeverPasses(t *testing.T) {
	for _, stepError := range []error{nil, mysqlPairRefused} {
		for _, rollbackError := range []error{nil, mysqlPairRefused} {
			calls := 0
			err := mysqlPairWithRollback(func() error { return stepError }, func() error { calls++; return rollbackError })
			if calls != 1 || (err == nil) != (stepError == nil && rollbackError == nil) {
				t.Fatal("post-BEGIN path skipped rollback or unknown closure passed")
			}
		}
	}
	rolledBack, recovered := false, false
	func() {
		defer func() { recovered = recover() != nil }()
		_ = mysqlPairWithRollback(func() error { panic("synthetic controlled panic") }, func() error { rolledBack = true; return nil })
	}()
	if !rolledBack || !recovered {
		t.Fatal("panic path skipped rollback")
	}
}
