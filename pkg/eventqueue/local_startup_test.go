package eventqueue

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

const localStartupTestIdentity = "led_startup_test"

type localStartupObservedContext struct {
	context.Context
	checks     atomic.Int64
	beforeOpen chan struct{}
}

type localStartupCancelAfterOpenContext struct {
	context.Context
	cancel context.CancelFunc
	checks atomic.Int64
}

func (ctx *localStartupCancelAfterOpenContext) Err() error {
	if ctx.checks.Add(1) == 4 {
		ctx.cancel()
	}
	return ctx.Context.Err()
}

func (ctx *localStartupObservedContext) Err() error {
	err := ctx.Context.Err()
	if ctx.checks.Add(1) == 2 {
		close(ctx.beforeOpen)
	}
	return err
}

func localStartupTestBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestOpenBoundLocalFreshBindAndInterruptedAcknowledgement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh", "queue.db")
	for _, established := range []bool{false, false, true} {
		queue, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, established)
		if err != nil {
			t.Fatal("fresh or exact empty bind retry", established, err)
		}
		if err := queue.BindInstallation(localStartupTestIdentity, true); err != nil {
			t.Fatal("bound identity must already be durable", err)
		}
		if err := queue.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenBoundLocalEstablishedRetainsOriginalRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	queue, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Reserve("original_request", []byte("original fallback")); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	queue, err = OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = queue.Close() }()
	entries, err := queue.Read(8)
	if err != nil || len(entries) != 1 || entries[0].ID != "original_request" || string(entries[0].Payload) != "original fallback" {
		t.Fatal("bound startup changed legacy pending recovery", entries, err)
	}
}

func TestOpenBoundLocalRejectsHistoryBeforeMutation(t *testing.T) {
	for _, test := range []struct {
		name        string
		identity    string
		established bool
		mutate      func(*testing.T, *Queue)
	}{
		{name: "foreign", identity: "led_foreign", established: true},
		{name: "uninitialized_pending", identity: localStartupTestIdentity, mutate: func(t *testing.T, q *Queue) {
			if err := q.Reserve("retained_request", []byte("retained fallback")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "uninitialized_accounting", identity: localStartupTestIdentity, mutate: func(t *testing.T, q *Queue) {
			if err := q.EnableQuota("UTC", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "uninitialized_removed_history_sequence", identity: localStartupTestIdentity, mutate: func(t *testing.T, q *Queue) {
			if err := q.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(readyBucket).SetSequence(1) }); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "uninitialized_unknown_bucket", identity: localStartupTestIdentity, mutate: func(t *testing.T, q *Queue) {
			if err := q.db.Update(func(tx *bolt.Tx) error { _, err := tx.CreateBucket([]byte("unknown-history")); return err }); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.db")
			queue, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, false)
			if err != nil {
				t.Fatal(err)
			}
			if test.mutate != nil {
				test.mutate(t, queue)
			}
			if err := queue.Close(); err != nil {
				t.Fatal(err)
			}
			before := localStartupTestBytes(t, path)
			got, err := OpenBoundLocal(t.Context(), path, 8, 1024, test.identity, test.established)
			if got != nil || !errors.Is(err, ErrInvalid) || !bytes.Equal(before, localStartupTestBytes(t, path)) {
				t.Fatal("rejected startup mutated retained file", err)
			}
		})
	}
}

func TestOpenBoundLocalMissingEstablishedDoesNotCreateParents(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "absent")
	path := filepath.Join(parent, "queue.db")
	if q, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true); q != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal("missing established history admitted", err)
	}
	if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected established startup created parents", err)
	}
}

func TestOpenBoundLocalSharedFileCannotUseLocalRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared.db")
	identity := SharedSpoolIdentity{InstallationID: "led_shared_test", ModeRevision: 2, SpoolID: "spool_test"}
	spool, err := OpenSharedSpool(t.Context(), path, identity, SharedSpoolOptions{Capacity: 2, MaxDataBytes: SharedSpoolMaxDataBytes, DeliveredCapacity: 4})
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	before := localStartupTestBytes(t, path)
	q, err := OpenBoundLocal(t.Context(), path, 8, 1024, identity.InstallationID, true)
	if q != nil || !errors.Is(err, ErrSharedSpoolMode) || !bytes.Equal(before, localStartupTestBytes(t, path)) {
		t.Fatal("local startup changed shared spool", err)
	}
}

func TestOpenBoundLocalCancellationClosesAcquiredHandle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.db")
	queue, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	observed := &localStartupObservedContext{Context: ctx, beforeOpen: make(chan struct{})}
	result := make(chan error, 1)
	go func() {
		got, err := OpenBoundLocal(observed, path, 8, 1024, localStartupTestIdentity, true)
		if got != nil {
			_ = got.Close()
			result <- errors.New("cancelled open retained a queue")
			return
		}
		result <- err
	}()
	<-observed.beforeOpen
	cancel()
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled lock/open", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled file open exceeded its bound")
	}
	queue, err = OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true)
	if err != nil {
		t.Fatal("cancelled operation leaked its file lock", err)
	}
	_ = queue.Close()
	before := localStartupTestBytes(t, path)
	afterOpen, cancelAfterOpen := context.WithCancel(t.Context())
	defer cancelAfterOpen()
	check := &localStartupCancelAfterOpenContext{Context: afterOpen, cancel: cancelAfterOpen}
	queue, err = OpenBoundLocal(check, path, 8, 1024, localStartupTestIdentity, true)
	if queue != nil || !errors.Is(err, context.Canceled) || !bytes.Equal(before, localStartupTestBytes(t, path)) {
		t.Fatal("cancelled acquired handle changed local history", err)
	}
	queue, err = OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true)
	if err != nil {
		t.Fatal("cancellation after file acquisition leaked its handle", err)
	}
	_ = queue.Close()
}

func TestOpenBoundLocalRejectsFileRacesBeforeBoltInitialization(t *testing.T) {
	t.Run("existing_zero_file_remains_zero", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.db")
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
		q, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true)
		if q != nil || !errors.Is(err, ErrInvalid) || len(localStartupTestBytes(t, path)) != 0 {
			t.Fatal("bbolt initialized existing empty history", err)
		}
	})
	for _, change := range []string{"disappeared", "parent_disappeared", "replaced_zero", "replaced_foreign"} {
		t.Run(change, func(t *testing.T) {
			parent := filepath.Join(t.TempDir(), "existing")
			path := filepath.Join(parent, "queue.db")
			q, err := OpenBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, false)
			if err != nil {
				t.Fatal(err)
			}
			_ = q.Close()
			var replacement []byte
			q, err = openBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, true, func() {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "parent_disappeared":
					if err := os.Remove(parent); err != nil {
						t.Fatal(err)
					}
				case "replaced_zero":
					if err := os.WriteFile(path, nil, 0600); err != nil {
						t.Fatal(err)
					}
				case "replaced_foreign":
					foreign, err := OpenBoundLocal(t.Context(), path, 8, 1024, "led_foreign", false)
					if err != nil {
						t.Fatal(err)
					}
					if err := foreign.Reserve("foreign_original", []byte("foreign original fallback")); err != nil {
						t.Fatal(err)
					}
					_ = foreign.Close()
				}
				if change == "replaced_zero" || change == "replaced_foreign" {
					replacement = localStartupTestBytes(t, path)
				}
			})
			if q != nil || err == nil {
				t.Fatal("same-path race admitted an established queue", err)
			}
			if change == "replaced_zero" || change == "replaced_foreign" {
				if !bytes.Equal(replacement, localStartupTestBytes(t, path)) {
					t.Fatal("bbolt initialized or recovered a replacement file")
				}
			} else if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("established disappeared path was recreated", err)
			}
			if change == "parent_disappeared" {
				if _, err := os.Lstat(parent); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("established disappeared parents were recreated", err)
				}
			}
		})
	}
	t.Run("absent_path_collision_remains_untouched", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "collision.db")
		q, err := openBoundLocal(t.Context(), path, 8, 1024, localStartupTestIdentity, false, func() {
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
		})
		if q != nil || !errors.Is(err, os.ErrExist) || len(localStartupTestBytes(t, path)) != 0 {
			t.Fatal("exclusive creation initialized a colliding path", err)
		}
	})
}
