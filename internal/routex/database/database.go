// Package database centralizes GORM connection setup and schema migration.
package database

import (
	"context"
	"errors"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/miclle/routex/pkg/gormlog"
)

// Open connects to the configured database and verifies the connection.
// Supported drivers are "postgres" and "mysql". Initialization errors never
// expose driver error strings, which can contain connection credentials.
func Open(ctx context.Context, driver, dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, errors.New("database dsn is required")
	}

	var dialector gorm.Dialector
	switch driver {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres", "":
		dialector = postgres.Open(dsn)
	default:
		return nil, errors.New("unsupported database driver")
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		TranslateError:       true,
		DisableAutomaticPing: true,
		Logger:               logger.Discard,
	})
	if err != nil {
		closeInitializationPool(db)
		return nil, errors.New("connect to database failed")
	}
	sqlDB, err := db.DB()
	if err != nil {
		closeInitializationPool(db)
		return nil, errors.New("access database pool failed")
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, errors.New("ping database failed")
	}
	// Restore operational logging only after connection validation succeeded.
	db.Logger = gormlog.New(0)
	return db, nil
}

func closeInitializationPool(db *gorm.DB) {
	if db == nil {
		return
	}
	if pool, err := db.DB(); err == nil {
		_ = pool.Close()
	}
}
