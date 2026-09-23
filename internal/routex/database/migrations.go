package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Migrate applies immutable, numbered schema steps. A dedicated connection owns
// the advisory lock because MySQL DDL commits transactions implicitly.
func Migrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db handle is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) (migrationErr error) {
		// Connection hands us an initialized GORM statement. Start fresh query
		// sessions on that SAME sql.Conn so predicates from one ledger query
		// cannot leak into subsequent versions or poison lock release.
		conn = conn.Session(&gorm.Session{NewDB: true})
		dialect := conn.Name()
		var unlock string
		switch dialect {
		case "postgres":
			if err := conn.Exec("SELECT pg_advisory_lock(724683901)").Error; err != nil {
				return fmt.Errorf("lock migrations: %w", err)
			}
			unlock = "SELECT pg_advisory_unlock(724683901)"
		case "mysql":
			var locked sql.NullInt64
			if err := conn.Raw("SELECT GET_LOCK('routex_schema_migrations', 30)").Scan(&locked).Error; err != nil {
				return fmt.Errorf("lock migrations: %w", err)
			}
			if !locked.Valid || locked.Int64 != 1 {
				return fmt.Errorf("migration lock unavailable")
			}
			unlock = "SELECT RELEASE_LOCK('routex_schema_migrations')"
		default:
			return fmt.Errorf("unsupported migration driver: %s", dialect)
		}
		defer func() {
			// The original request may have expired; the connection must not be
			// returned to its pool with the session-level lock still held.
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer releaseCancel()
			if err := conn.WithContext(releaseCtx).Exec(unlock).Error; err != nil {
				migrationErr = errors.Join(migrationErr, fmt.Errorf("unlock migrations: %w", err))
				if sqlConn, ok := conn.Statement.ConnPool.(*sql.Conn); ok {
					// ErrBadConn tells database/sql to discard this physical
					// connection instead of pooling a potentially held lock.
					_ = sqlConn.Raw(func(any) error { return driver.ErrBadConn })
				}
			}
		}()
		if err := migrateTables(conn, &migrationLedger{}); err != nil {
			return err
		}
		steps := migrationSteps(dialect)
		var unsupported int64
		if err := conn.Table("schema_migrations").Where("version < 1 OR version > ?", len(steps)).Count(&unsupported).Error; err != nil {
			return err
		}
		if unsupported != 0 {
			return fmt.Errorf("database schema version is not supported by this binary")
		}
		for version, apply := range steps {
			var count int64
			if err := conn.Table("schema_migrations").Where("version = ?", version+1).Count(&count).Error; err != nil {
				return err
			}
			if count != 0 {
				continue
			}
			if err := apply(conn); err != nil {
				return fmt.Errorf("migration %d: %w", version+1, err)
			}
			if err := conn.Create(&migrationLedger{Version: version + 1, AppliedAt: time.Now().UTC().Format(time.RFC3339Nano)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// Released versions 1–4 retain their original SQL for reproducible upgrades.
// New versions use frozen models and GORM Migrator APIs.
func migrationSteps(dialect string) []func(*gorm.DB) error {
	legacy := legacyMigrationSQL(dialect)
	steps := make([]func(*gorm.DB) error, 0, len(legacy)+3)
	for _, statements := range legacy {
		steps = append(steps, func(db *gorm.DB) error {
			for _, statement := range statements {
				if err := db.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		})
	}
	return append(steps, callMigration, governanceMigration, runtimeMigration, resourcesMigration, projectKeyMigration, offboardingMigration, pricingMigration, projectRequestMigration, callPricingMigration, resourceLimitMigration, providerModelStateMigration, mfaMigration, siteMigration, egressMigration, quotaMigration, smtpMigration)
}

func legacyMigrationSQL(dialect string) [][]string {
	emailType := "VARCHAR(254)"
	timestamp, sequence, seed := "TIMESTAMPTZ", "BIGSERIAL", "INSERT INTO installations (id, initialized) VALUES (1, FALSE) ON CONFLICT (id) DO NOTHING"
	if dialect == "mysql" {
		timestamp, sequence = "DATETIME(6)", "BIGINT UNSIGNED AUTO_INCREMENT"
		// Keep email identity byte-exact after normalization, matching PostgreSQL;
		// MySQL's default accent-insensitive collation would alias distinct users.
		emailType = "VARCHAR(254) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
		seed = "INSERT IGNORE INTO installations (id, initialized) VALUES (1, FALSE)"
	}
	sessionIndexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions (expires_at)",
	}
	extraSessionIndexes := ""
	if dialect == "mysql" {
		extraSessionIndexes = ", INDEX idx_sessions_user_id (user_id), INDEX idx_sessions_expires_at (expires_at)"
		sessionIndexes = nil
	}
	steps := [][]string{
		{fmt.Sprintf("CREATE TABLE IF NOT EXISTS examples (id %s PRIMARY KEY, created_at %s, updated_at %s, deleted_at %s, title VARCHAR(255) NOT NULL, body TEXT)", sequence, timestamp, timestamp, timestamp)},
		{
			fmt.Sprintf("CREATE TABLE IF NOT EXISTS users (id VARCHAR(30) PRIMARY KEY, email %s NOT NULL UNIQUE, name VARCHAR(100) NOT NULL, password_hash VARCHAR(60) NOT NULL, role VARCHAR(20) NOT NULL, disabled BOOLEAN NOT NULL DEFAULT FALSE, created_at %s NOT NULL, updated_at %s NOT NULL)", emailType, timestamp, timestamp),
			fmt.Sprintf("CREATE TABLE IF NOT EXISTS sessions (id VARCHAR(30) PRIMARY KEY, user_id VARCHAR(30) NOT NULL, token_hash VARCHAR(64) NOT NULL UNIQUE, expires_at %s NOT NULL, created_at %s NOT NULL, CONSTRAINT fk_sessions_user FOREIGN KEY (user_id) REFERENCES users(id)%s)", timestamp, timestamp, extraSessionIndexes),
			"CREATE TABLE IF NOT EXISTS installations (id INTEGER PRIMARY KEY, initialized BOOLEAN NOT NULL DEFAULT FALSE)",
			seed,
		},
	}
	steps[1] = append(steps[1], sessionIndexes...)
	return append(steps, catalogMigration(dialect), apiKeyMigration(dialect))
}
