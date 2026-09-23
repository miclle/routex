package eventqueue

import (
	"errors"
	"fmt"
	"path/filepath"
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
