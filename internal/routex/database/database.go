// Package database centralizes GORM connection setup and schema migration.
package database

import (
	"context"
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/miclle/routex/pkg/gormlog"
)

// Open connects to the configured database and verifies the connection.
// Supported drivers are "postgres" and "mysql".
func Open(ctx context.Context, driver, dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("database dsn is required")
	}

	var dialector gorm.Dialector
	switch driver {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres", "":
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported driver: %s (supported: postgres, mysql)", driver)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		TranslateError: true,
		Logger:         gormlog.New(0),
	})
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access sql db: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
