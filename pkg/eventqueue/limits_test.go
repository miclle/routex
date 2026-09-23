package eventqueue

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func limitPtr(value int64) *int64 { return &value }
func TestAtomicAggregateAndChildLimits(t *testing.T) {
	queue, err := Open(filepath.Join(t.TempDir(), "calls.db"), 100, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for index := range 30 {
		wg.Go(func() {
			<-start
			err := queue.ReserveWithLimits(fmt.Sprintf("req_%d", index), []byte("{}"), []Limit{{Account: "project_one", RPM: limitPtr(3)}, {Account: "key_one", RPM: limitPtr(2)}}, now)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrRateLimit) {
				t.Error(err)
			}
		})
	}
	close(start)
	wg.Wait()
	if accepted.Load() != 2 {
		t.Fatalf("accepted %d", accepted.Load())
	}
	for _, account := range []string{"project_one", "key_one"} {
		rpm, active, err := queue.AccountUsage(account, now)
		if err != nil || rpm != 2 || active != 2 {
			t.Fatalf("partial debit %s %d %d %v", account, rpm, active, err)
		}
	}
	if err := queue.ReserveWithLimits("req_other", []byte("{}"), []Limit{{Account: "project_other", RPM: limitPtr(1)}}, now); err != nil {
		t.Fatal("unrelated account blocked", err)
	}
	if err := queue.ReserveWithLimits("req_zero", []byte("{}"), []Limit{{Account: "project_zero", RPM: limitPtr(0)}}, now); !errors.Is(err, ErrRateLimit) {
		t.Fatal("zero did not block")
	}
	rpm, _, _ := queue.AccountUsage("project_zero", now)
	if rpm != 0 {
		t.Fatal("rejection consumed quota")
	}
}
func TestConcurrencyReleaseRestartAndRollingRPM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.db")
	queue, err := Open(path, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	policy := []Limit{{Account: "key_family", Concurrency: limitPtr(1), RPM: limitPtr(3)}}
	if err := queue.BindInstallation("led_test", false); err != nil {
		t.Fatal(err)
	}
	if err := queue.ReserveWithLimits("req_first", []byte("{}"), policy, now); err != nil {
		t.Fatal(err)
	}
	if err := queue.ReserveWithLimits("req_blocked", []byte("{}"), policy, now); !errors.Is(err, ErrConcurrency) {
		t.Fatal("concurrency overshoot", err)
	}
	if err := queue.Complete("req_first", []byte(`{"final":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete("req_first", []byte(`{"duplicate":true}`)); err != nil {
		t.Fatal(err)
	}
	if err := queue.Ack("req_first"); err != nil {
		t.Fatal(err)
	}
	if rpm, active, err := queue.AccountUsage("key_family", now); err != nil || rpm != 1 || active != 0 {
		t.Fatalf("ack erased RPM or double released %d %d %v", rpm, active, err)
	}
	if err := queue.ReserveWithLimits("req_interrupted", []byte("{}"), policy, now); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	queue, err = Open(path, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	if err := queue.BindInstallation("led_test", true); err != nil {
		t.Fatal(err)
	}
	if rpm, active, err := queue.AccountUsage("key_family", now.Add(-time.Hour)); err != nil || rpm != 2 || active != 0 {
		t.Fatalf("restart/clock rollback %d %d %v", rpm, active, err)
	}
	if err := queue.ReserveWithLimits("req_after_restart", []byte("{}"), policy, now); err != nil {
		t.Fatal(err)
	}
	if err := queue.Complete("req_after_restart", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if err := queue.ReserveWithLimits("req_rpm_blocked", []byte("{}"), policy, now.Add(time.Minute-time.Nanosecond)); !errors.Is(err, ErrRateLimit) {
		t.Fatal("RPM reset too early", err)
	}
	if err := queue.ReserveWithLimits("req_boundary", []byte("{}"), policy, now.Add(time.Minute)); err != nil {
		t.Fatal("rolling boundary failed", err)
	}
	if rpm, active, err := queue.AccountUsage("key_family", now.Add(time.Minute)); err != nil || rpm != 1 || active != 1 {
		t.Fatalf("incorrect rolling counters %d %d %v", rpm, active, err)
	}
}
func TestJournalBindingRejectsMissingOrForeignHistory(t *testing.T) {
	queue, err := Open(filepath.Join(t.TempDir(), "calls.db"), 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	if !errors.Is(queue.BindInstallation("led_established", true), ErrInvalid) {
		t.Fatal("missing ledger silently reset")
	}
	if err := queue.BindInstallation("led_original", false); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(queue.BindInstallation("led_foreign", false), ErrInvalid) {
		t.Fatal("foreign ledger accepted")
	}
}

func TestMissingRetainedLedgerBucketFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.db")
	queue, err := Open(path, 10, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.BindInstallation("led_initialized", false); err != nil {
		t.Fatal(err)
	}
	if err := queue.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket(rpmBucket) }); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, 10, 100)
	if reopened != nil {
		_ = reopened.Close()
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("missing ledger bucket silently reset usage", err)
	}
}
