package database

import (
	"context"
	"testing"
)

func TestOpenRejectsMissingDSN(t *testing.T) {
	if _, err := Open(context.Background(), "postgres", ""); err == nil {
		t.Fatal("Open should reject an empty DSN")
	}
}

func TestOpenRejectsUnsupportedDriver(t *testing.T) {
	if _, err := Open(context.Background(), "sqlite", "file:test.db"); err == nil {
		t.Fatal("Open should reject unsupported driver")
	}
}

func TestMigrateRejectsNilDB(t *testing.T) {
	if err := Migrate(context.Background(), nil); err == nil {
		t.Fatal("Migrate should reject nil db")
	}
}
