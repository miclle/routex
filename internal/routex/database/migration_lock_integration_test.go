package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Run only against the disposable integration database. No other integration
// package may share its server while this test owns the migration advisory lock.
func TestMigrationLockIntegration(t *testing.T) {
	if os.Getenv("ROUTEX_TEST_MIGRATION_LOCK") != "1" {
		t.Skip("set ROUTEX_TEST_MIGRATION_LOCK=1 for an exclusive real-database lock regression")
	}
	if os.Getenv("ROUTEX_TEST_POSTGRES_DSN") == "" && os.Getenv("ROUTEX_TEST_MYSQL_DSN") == "" {
		t.Fatal("the opted-in lock regression requires a dedicated real-database DSN")
	}
	for _, dialect := range []string{"postgres", "mysql"} {
		t.Run(dialect, func(t *testing.T) {
			dsn := os.Getenv("ROUTEX_TEST_" + strings.ToUpper(dialect) + "_DSN")
			if dsn == "" {
				t.Skip("set the dedicated real-database integration DSN")
			}
			db := migrationLockTestDB(t, dialect, dsn)
			query := "SELECT current_database()"
			if dialect == "mysql" {
				query = "SELECT DATABASE()"
			}
			var name string
			if err := db.Raw(query).Scan(&name).Error; err != nil || name != "routex_test" {
				t.Fatal("migration lock test requires the isolated routex_test database")
			}
			if err := Migrate(context.Background(), db); err != nil {
				t.Fatal("prepare current schema", err)
			}
			for _, cancelCaller := range []bool{false, true} {
				name := "wait_beyond_thirty_seconds"
				if cancelCaller {
					name = "short_caller_cancellation"
				}
				t.Run(name, func(t *testing.T) {
					testMigrationLockWait(t, db, dialect, dsn, cancelCaller)
				})
			}
		})
	}
}

func migrationLockTestDB(t *testing.T, dialect, dsn string) *gorm.DB {
	t.Helper()
	db, err := Open(context.Background(), dialect, dsn)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Error(err)
		}
	})
	return db.Session(&gorm.Session{Logger: logger.Discard})
}

func testMigrationLockWait(t *testing.T, observer *gorm.DB, dialect, dsn string, cancelCaller bool) {
	t.Helper()
	pool, err := observer.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	holder, err := pool.Conn(ctx)
	stop()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := holder.Close(); err != nil {
			t.Error(err)
		}
	}()
	acquire, release := "SELECT pg_advisory_lock(724683901)", "SELECT pg_advisory_unlock(724683901)"
	if dialect == "mysql" {
		acquire, release = "SELECT GET_LOCK('routex_schema_migrations', 0)", "SELECT RELEASE_LOCK('routex_schema_migrations')"
	}
	ctx, stop = context.WithTimeout(context.Background(), 5*time.Second)
	if dialect == "mysql" {
		var got sql.NullInt64
		err = holder.QueryRowContext(ctx, acquire).Scan(&got)
		if err == nil && (!got.Valid || got.Int64 != 1) {
			err = errors.New("test migration lock unavailable")
		}
	} else {
		_, err = holder.ExecContext(ctx, acquire)
	}
	stop()
	if err != nil {
		t.Fatal(err)
	}
	held := true
	unlock := func() bool {
		if held {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var released bool
			err := holder.QueryRowContext(ctx, release).Scan(&released)
			if err != nil || !released {
				t.Error("release test migration lock", err)
			}
			held = false
			return err == nil && released
		}
		return true
	}
	defer unlock()
	identityQuery := "SELECT pg_backend_pid()"
	if dialect == "mysql" {
		identityQuery = "SELECT CONNECTION_ID()"
	}
	ctx, stop = context.WithTimeout(context.Background(), 5*time.Second)
	var holderID int64
	err = holder.QueryRowContext(ctx, identityQuery).Scan(&holderID)
	stop()
	if err != nil || holderID <= 0 {
		t.Fatal("observe lock holder connection", err)
	}
	var before []migrationLedger
	if err := observer.Order("version").Find(&before).Error; err != nil {
		t.Fatal(err)
	}

	// This independent handle owns its callback registry before its only worker
	// starts. Observe the actual dedicated connection without changing its query.
	db := migrationLockTestDB(t, dialect, dsn)
	type started struct {
		id  int64
		err error
	}
	startedQuery := make(chan started, 1)
	callback := func(tx *gorm.DB) {
		sqlText := tx.Statement.SQL.String()
		if !strings.HasPrefix(sqlText, "SELECT GET_LOCK(") && sqlText != "SELECT pg_advisory_lock(724683901)" {
			return
		}
		var id int64
		err := tx.Statement.ConnPool.QueryRowContext(tx.Statement.Context, identityQuery).Scan(&id)
		startedQuery <- started{id, err}
		if err != nil {
			_ = tx.AddError(err)
		}
	}
	if err := db.Callback().Raw().Before("gorm:raw").Register("migration-lock:observe", callback); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().Before("gorm:row").Register("migration-lock:observe", callback); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	joined := false
	defer func() {
		cancel()
		unlock()
		if !joined {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("migration worker did not join")
			}
		}
	}()
	start := time.Now()
	go func() { done <- Migrate(ctx, db) }()
	var waiting started
	select {
	case waiting = <-startedQuery:
		if waiting.err != nil || waiting.id <= 0 || waiting.id == holderID {
			t.Fatal("observe migration connection", waiting.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("migration lock query did not start")
	}
	query := "SELECT COUNT(*) FROM pg_stat_activity WHERE pid = ? AND wait_event_type = 'Lock' AND wait_event = 'advisory'"
	if dialect == "mysql" {
		query = "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID = ? AND COMMAND IN ('Query', 'Execute') AND INFO LIKE 'SELECT GET_LOCK(%'"
	}
	ctxObserve, stopObserve := context.WithTimeout(context.Background(), 5*time.Second)
	defer stopObserve()
	for {
		var count int64
		if err := observer.WithContext(ctxObserve).Raw(query, waiting.id).Scan(&count).Error; err != nil {
			t.Fatal("observe server lock wait", err)
		}
		if count == 1 {
			if dialect == "mysql" {
				var row struct {
					Command string
					State   sql.NullString
				}
				result := observer.WithContext(ctxObserve).Raw("SELECT COMMAND, STATE FROM information_schema.PROCESSLIST WHERE ID = ?", waiting.id).Scan(&row)
				if result.Error != nil || result.RowsAffected != 1 || row.Command != "Query" && row.Command != "Execute" {
					t.Fatal("observe exact MySQL lock waiter command", result.Error)
				}
				state := "other"
				if row.State.Valid && (row.State.String == "User lock" || row.State.String == "Waiting for user-level lock") {
					state = "user_lock"
				}
				t.Logf("observed MySQL lock waiter command=%s state=%s", strings.ToLower(row.Command), state)
			}
			break
		}
		select {
		case err := <-done:
			joined = true
			t.Fatal("migration returned before observed lock wait", err)
		case <-ctxObserve.Done():
			t.Fatal("server did not observe blocked migration")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if cancelCaller {
		cancel()
		select {
		case err := <-done:
			joined = true
			if !errors.Is(err, context.Canceled) {
				t.Fatal("migration did not preserve caller cancellation", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("canceled caller still waiting on migration lock")
		}
		if !unlock() {
			t.Fatal("holder did not confirm lock release")
		}
		// Driver cancellation can return before its asynchronous physical cleanup.
		// Join the exact server session, then test acquisition once without retry.
		query := "SELECT COUNT(*) FROM pg_stat_activity WHERE pid = ?"
		if dialect == "mysql" {
			query = "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID = ?"
		}
		ctxCleanup, stopCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCleanup()
		for {
			var count int64
			if err := observer.WithContext(ctxCleanup).Raw(query, waiting.id).Scan(&count).Error; err != nil {
				t.Fatal("observe canceled migration backend cleanup", err)
			}
			if count == 0 {
				break
			}
			select {
			case <-ctxCleanup.Done():
				t.Fatal("canceled migration backend did not close")
			case <-time.After(10 * time.Millisecond):
			}
		}
	} else {
		select {
		case err := <-done:
			joined = true
			t.Fatal("migration returned before lock release beyond thirty seconds", err)
		case <-time.After(31 * time.Second):
		}
		if !unlock() {
			t.Fatal("holder did not confirm lock release")
		}
		select {
		case err := <-done:
			joined = true
			if err != nil || time.Since(start) >= time.Minute {
				t.Fatal("migration did not complete within original minute", err)
			}
		case <-time.After(time.Until(start.Add(time.Minute))):
			t.Fatal("migration exceeded original minute")
		}
	}
	// A separate session must acquire immediately after completion/backend join;
	// neither a pooled connection nor an interrupted backend may retain the lock.
	ctx, stop = context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	probe, err := pool.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := probe.Close(); err != nil {
			t.Error(err)
		}
	}()
	var free bool
	if dialect == "mysql" {
		var got sql.NullInt64
		err = probe.QueryRowContext(ctx, acquire).Scan(&got)
		free = got.Valid && got.Int64 == 1
	} else {
		err = probe.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(724683901)").Scan(&free)
	}
	if err != nil || !free {
		t.Fatal("migration retained session lock", err)
	}
	var released bool
	if err := probe.QueryRowContext(ctx, release).Scan(&released); err != nil || !released {
		t.Fatal("probe did not confirm lock release", err)
	}
	var after []migrationLedger
	if err := observer.Order("version").Find(&after).Error; err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("lock wait/repeat/cancellation rewrote migration history", err)
	}
}
