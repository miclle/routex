package database

import (
	"context"
	"strings"
	"testing"
)

func TestOpenHidesConnectionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, driver, dsn string
	}{
		{name: "unsupported driver", driver: "private-driver-marker", dsn: "private-dsn-marker"},
		{name: "invalid postgres URI", driver: "postgres", dsn: "postgres://private-password-marker%"},
		{name: "invalid mysql address", driver: "mysql", dsn: "private-password-marker@tcp("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := Open(context.Background(), tc.driver, tc.dsn)
			if err == nil || db != nil {
				t.Fatal("invalid connection configuration should fail")
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("initialization error exposed connection configuration")
			}
		})
	}
}

func TestOpenCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A canceled PostgreSQL ping never needs a reachable database.
	db, err := Open(ctx, "postgres", "host=127.0.0.1 port=1 user=test password=private-password-marker dbname=test sslmode=disable")
	if err == nil || db != nil || strings.Contains(err.Error(), "private-") {
		t.Fatal("canceled connection should return a sanitized failure")
	}
}
