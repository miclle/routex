package database

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMigrationLockWaitUsesRemainingDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		left time.Duration
		want int
	}{
		{"overall budget", time.Minute, 60},
		{"cap", 2 * time.Minute, 60},
		{"beyond old timeout", 45 * time.Second, 45},
		{"round up", 1500 * time.Millisecond, 2},
		{"short caller", 500 * time.Millisecond, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), tc.left)
			defer cancel()
			if got, err := migrationLockWaitSeconds(ctx); err != nil || got != tc.want {
				t.Fatalf("wait seconds = %d, error = %v; want %d", got, err, tc.want)
			}
		})
	}
	if got, err := migrationLockWaitSeconds(context.Background()); err != nil || got != 60 {
		t.Fatal("unbounded input must retain finite server wait", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := migrationLockWaitSeconds(ctx); got != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled caller must not acquire a lock", got, err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if got, err := migrationLockWaitSeconds(ctx); got != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("expired caller must not acquire a lock", got, err)
	}
}
