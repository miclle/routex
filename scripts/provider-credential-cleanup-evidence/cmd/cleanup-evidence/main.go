// Command cleanup-evidence is a private PostgreSQL acceptance capability.
// It neither authorizes cleanup nor makes a remote request.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	cleanupevidence "github.com/miclle/routex/scripts/provider-credential-cleanup-evidence"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var BuildSource string
var BuildRunID string
var BuildPhase string
var BuildTrustedHostUID string
var BuildDriver = "postgres"

var errRejected = cleanupevidence.ErrRejected

const rejected = "{\"result\":\"cleanup_evidence_rejected\",\"runtime_acceptance\":false}\n"

func main() { os.Exit(run(os.Args[1:], os.Stdout)) }

func reject(out io.Writer) int {
	_, _ = io.WriteString(out, rejected)
	return 1
}

func run(args []string, out io.Writer) (code int) {
	code = 1
	defer func() {
		if recover() != nil {
			code = reject(out)
		}
	}()
	if len(args) != 0 {
		return reject(out)
	}
	b, err := compiledBindings(os.Getuid(), os.Geteuid())
	if err != nil || !cleanConnectionEnvironment(os.Environ()) {
		return reject(out)
	}
	raw, err := stdinBounded()
	if err != nil {
		return reject(out)
	}
	defer clear(raw)
	input, err := decodeInput(raw, b)
	clear(raw)
	if err != nil {
		return reject(out)
	}
	expected := input.expectation(b)
	dsn, err := consumeConnection(&input, b)
	if err != nil {
		return reject(out)
	}
	// Read work and rollback share the three-second caller context. Pool Close
	// has no context argument; closure and context are checked before success.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dialector := postgres.Open(dsn)
	if b.driver == "mysql" {
		dialector = gormmysql.New(gormmysql.Config{DSN: dsn, SkipInitializeWithVersion: true})
	}
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true, Logger: logger.Discard, DisableAutomaticPing: true})
	dsn = ""
	if err != nil {
		// An initializer may return a partially created local pool. Close only
		// that returned handle; no success or speculative new connection follows.
		if db != nil {
			if pool, poolErr := db.DB(); poolErr == nil {
				_ = pool.Close()
			}
		}
		return reject(out)
	}
	pool, err := db.DB()
	if err != nil {
		return reject(out)
	}
	closed := false
	defer func() {
		if !closed {
			closed = true
			_ = pool.Close()
		}
	}()
	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(0)
	value, queryErr := cleanupevidence.Project(ctx, db, expected)
	if queryErr == nil {
		queryErr = verifyNamespace(ctx, db, b)
	}
	return complete(ctx, out, value, queryErr, func() error {
		// Set before the fallible close so panic/unknown closure cannot cause
		// a second automatic close attempt or a successful receipt.
		closed = true
		return pool.Close()
	})
}

func cleanConnectionEnvironment(values []string) bool {
	// pgx also understands libpq environment defaults. This fixed disposable
	// capability accepts none of those overrides, including service/pass files.
	for _, value := range values {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(name, "PG") {
			return false
		}
	}
	return true
}

func verifyNamespace(ctx context.Context, db *gorm.DB, b bindings) (err error) {
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return errRejected
	}
	defer func() {
		if recover() != nil {
			err = errRejected
		}
		if tx.Rollback().Error != nil || ctx.Err() != nil {
			err = errRejected
		}
	}()
	var identity struct {
		DatabaseName string
		DatabaseUser string
	}
	// GORM has no current database/user expression API. These fixed reads have
	// no caller-provided SQL or identifiers; MySQL includes the login host in CURRENT_USER.
	query := "SELECT current_database() AS database_name, current_user AS database_user"
	if b.driver == "mysql" {
		query = "SELECT DATABASE() AS database_name, SUBSTRING_INDEX(CURRENT_USER(), '@', 1) AS database_user"
	}
	result := tx.Raw(query).Scan(&identity)
	if result.Error != nil || result.RowsAffected != 1 || identity.DatabaseName != b.database() || identity.DatabaseUser != b.user() {
		return errRejected
	}
	return nil
}

func complete(ctx context.Context, out io.Writer, value cleanupevidence.Evidence, queryErr error, closePool func() error) (code int) {
	code = 1
	defer func() {
		if recover() != nil {
			code = reject(out)
		}
	}()
	closeErr := closePool()
	if queryErr != nil || closeErr != nil || ctx.Err() != nil {
		return reject(out)
	}
	encoded, err := json.Marshal(struct {
		Result            string                   `json:"result"`
		Capability        string                   `json:"database_capability"`
		RuntimeAcceptance bool                     `json:"runtime_acceptance"`
		Evidence          cleanupevidence.Evidence `json:"evidence"`
	}{"verified_published_cleanup_evidence", BuildDriver + "_private_controlled", false, value})
	if err != nil || len(encoded) > 16384 {
		return reject(out)
	}
	encoded = append(encoded, '\n')
	n, err := out.Write(encoded)
	if err != nil || n != len(encoded) || ctx.Err() != nil {
		return 1
	}
	return 0
}
