//go:build integration

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Execute only in a separate owned PostgreSQL test namespace. The fixed private
// file must contain the reviewed fixture config and exact existing request pair;
// it is never discovered from the current app, history, argv or environment.
func TestPostgresEnforcesReadOnlyAndExactProjection(t *testing.T) {
	raw, err := privateRead("/fixture-evidence-test/config.json", 8192, false)
	if err != nil {
		t.Fatal("private isolated PostgreSQL test config missing")
	}
	defer clear(raw)
	var c config
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if !validJSONObject(raw) || d.Decode(&c) != nil || !validConfig(c) {
		t.Fatal("invalid private test config")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	dsn := "host=postgres port=5432 user=routex password=" + c.DatabasePassword + " dbname=routex sslmode=disable connect_timeout=2"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("isolated PostgreSQL connection failed")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("isolated database pool unavailable")
	}
	defer func() { _ = pool.Close() }()
	tx := db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		t.Fatal("read-only transaction failed")
	}
	rows, err := query(tx, c)
	if err != nil || !validProjection(c, rows) {
		_ = tx.Rollback()
		t.Fatal("exact durable projection rejected")
	}
	// This deliberately touches zero rows in an isolated test namespace. PostgreSQL
	// must reject the write operation itself in the read-only transaction, proving
	// that read-only behavior is server-enforced, not a success label in source.
	writeErr := tx.Table("api_keys").Where("id = ?", "nonexistent-isolated-readonly-probe").Update("status", "active").Error
	var state interface{ SQLState() string }
	if writeErr == nil || !errors.As(writeErr, &state) || state.SQLState() != "25006" {
		_ = tx.Rollback()
		t.Fatal("database accepted a write inside read-only transaction")
	}
	_ = tx.Rollback()
}
