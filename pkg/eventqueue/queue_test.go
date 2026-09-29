package eventqueue

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestQueueDurabilityAndAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.db")
	q, err := Open(path, 2, 128)
	if err != nil {
		t.Fatal(err)
	}
	if err = q.Reserve("req_inflight", []byte("interrupted unknown usage")); err != nil {
		t.Fatal(err)
	}
	if err = q.Reserve("req_complete", []byte("fallback")); err != nil {
		t.Fatal(err)
	}
	if err = q.Complete("req_complete", []byte("known usage")); err != nil {
		t.Fatal(err)
	}
	if err = q.Complete("req_complete", []byte("must not overwrite")); err != nil {
		t.Fatal(err)
	}
	if err = q.Reserve("req_full", []byte("overflow")); !errors.Is(err, ErrFull) {
		t.Fatalf("full admission: %v", err)
	}
	entries, err := q.Read(2)
	if err != nil || len(entries) != 1 {
		t.Fatalf("only completed facts may drain: %v", err)
	}
	if err = q.Close(); err != nil {
		t.Fatal(err)
	}
	q, err = Open(path, 2, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := q.Close(); err != nil {
			t.Error(err)
		}
	}()
	entries, err = q.Read(2)
	if err != nil || len(entries) != 2 {
		t.Fatalf("crash admissions not recovered: %v", err)
	}
	payloads := map[string]string{}
	for _, entry := range entries {
		payloads[entry.ID] = string(entry.Payload)
	}
	if payloads["req_inflight"] != "interrupted unknown usage" || payloads["req_complete"] != "known usage" {
		t.Fatalf("wrong recovered facts: %v", payloads)
	}
	if err = q.Ack("req_complete"); err != nil {
		t.Fatal(err)
	}
	if err = q.Reserve("req_new", []byte("next")); err != nil {
		t.Fatal(err)
	}
}

func TestQueueConcurrentCapacityAndExclusiveLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.db")
	q, err := Open(path, 8, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	if other, err := Open(path, 8, 32); err == nil {
		_ = other.Close()
		t.Fatal("second process owner acquired queue")
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 32)
	for i := range 32 {
		wg.Go(func() { outcomes <- q.Reserve(fmt.Sprintf("req_%d", i), []byte("pending")) })
	}
	wg.Wait()
	close(outcomes)
	admitted := 0
	for err := range outcomes {
		if err == nil {
			admitted++
		} else if !errors.Is(err, ErrFull) {
			t.Fatal(err)
		}
	}
	if admitted != 8 {
		t.Fatalf("admitted %d, expected8", admitted)
	}
	if err := q.Complete("missing", []byte("result")); !errors.Is(err, ErrMissing) {
		t.Fatal("missing admission accepted")
	}
	if err := q.Reserve("../escape", []byte("x")); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid ID accepted")
	}
}

func TestUpdatePendingPreservesAdmissionAndRecoversLatestFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facts.db")
	q, err := Open(path, 2, 128)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.EnableQuota("UTC", quotaTestTime); err != nil {
		t.Fatal(err)
	}
	policy := quotaPolicy("key_update")
	policy.RPM = limitPtr(2)
	policy.Concurrency = limitPtr(1)
	policy.Tokens5H = limitPtr(20)
	if err := q.ReserveWithQuota("req_update", []byte("first fallback"), []QuotaLimit{policy}, tokenBound(10), quotaTestTime); err != nil {
		t.Fatal(err)
	}
	receiptBefore, err := q.QuotaReceipt("req_update")
	if err != nil {
		t.Fatal(err)
	}
	usageBefore := requireUsage(t, q, policy.Account, quotaTestTime)
	rpmBefore, activeBefore, err := q.AccountUsage(policy.Account, quotaTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.UpdatePending("req_update", []byte("latest fallback")); err != nil {
		t.Fatal(err)
	}
	receiptAfter, err := q.QuotaReceipt("req_update")
	if err != nil {
		t.Fatal(err)
	}
	usageAfter := requireUsage(t, q, policy.Account, quotaTestTime)
	rpmAfter, activeAfter, err := q.AccountUsage(policy.Account, quotaTestTime)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receiptAfter, receiptBefore) || !reflect.DeepEqual(usageAfter, usageBefore) || rpmAfter != rpmBefore || activeAfter != activeBefore {
		t.Fatalf("fallback update changed admission state: receipt=%t usage=%t rpm=%d/%d active=%d/%d", reflect.DeepEqual(receiptAfter, receiptBefore), reflect.DeepEqual(usageAfter, usageBefore), rpmBefore, rpmAfter, activeBefore, activeAfter)
	}
	if err := q.UpdatePending("missing", []byte("fallback")); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing update error = %v, want %v", err, ErrMissing)
	}
	if err := q.UpdatePending("req_update", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid update error = %v, want %v", err, ErrInvalid)
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	q, err = Open(path, 2, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = q.Close() }()
	entries, err := q.Read(2)
	if err != nil || len(entries) != 1 || entries[0].ID != "req_update" || string(entries[0].Payload) != "latest fallback" {
		t.Fatalf("recovered entries = %+v, error = %v", entries, err)
	}
	if err := q.UpdatePending("req_update", []byte("too late")); !errors.Is(err, ErrMissing) {
		t.Fatalf("ready update error = %v, want %v", err, ErrMissing)
	}
}
