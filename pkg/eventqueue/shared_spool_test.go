package eventqueue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/routeattempt"
	bolt "go.etcd.io/bbolt"
)

func sharedJournalFixture(t *testing.T, capacity, delivered int) (*SharedSpool, string, SharedSpoolIdentity, SharedSpoolOptions) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "shared.db")
	identity := SharedSpoolIdentity{InstallationID: "installation_shared", SpoolID: "spool_shared", ModeRevision: 2}
	options := SharedSpoolOptions{Capacity: capacity, MaxDataBytes: SharedSpoolMaxDataBytes, DeliveredCapacity: delivered}
	spool, err := OpenSharedSpool(context.Background(), path, identity, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = spool.Close() })
	return spool, path, identity, options
}

func sharedJournalBody(data []byte) SharedSpoolBody {
	digest := sha256.Sum256(data)
	return SharedSpoolBody{Digest: hex.EncodeToString(digest[:]), Data: data}
}

func sharedJournalKey(identity SharedSpoolIdentity, requestID string, body SharedSpoolBody) SharedSpoolKey {
	return SharedSpoolKey{Identity: identity, IncarnationID: "incarnation_shared", Fence: 3, ExecutionID: "execution_shared", RequestID: requestID, AdmissionDigest: body.Digest}
}

func sharedJournalReady(t *testing.T, spool *SharedSpool, key SharedSpoolKey, body SharedSpoolBody) SharedSpoolTerminalReceipt {
	t.Helper()
	ctx := context.Background()
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_shared"); err != nil {
		t.Fatal(err)
	}
	terminal := sharedJournalBody([]byte("original_terminal:" + key.RequestID))
	if err := spool.PrepareTerminal(ctx, key, SharedSpoolTerminal{Body: terminal}); err != nil {
		t.Fatal(err)
	}
	receipt := SharedSpoolTerminalReceipt{Key: key, ReceiptID: "receipt_shared", TerminalDigest: terminal.Digest}
	if err := spool.MakeReady(ctx, receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestSharedSpoolRejectAdmissionReleasesOnlyUntouchedReservation(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 1, 4)
	ctx := context.Background()
	body := sharedJournalBody([]byte("rejected_admission"))
	key := sharedJournalKey(identity, "req_rejected", body)
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.RejectAdmission(ctx, key); err != nil {
		t.Fatal("rejecting untouched reservation", err)
	}
	if _, err := spool.ReadRecord(ctx, key); !errors.Is(err, ErrMissing) {
		t.Fatalf("rejected reservation retained: %v", err)
	}

	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal("released slot was not reusable", err)
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_rejected"); err != nil {
		t.Fatal(err)
	}
	if err := spool.RejectAdmission(ctx, key); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatalf("accepted reservation was released: %v", err)
	}
}

func sharedJournalBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestSharedSpoolRejectsLocalAndForeignBindingsWithoutFileMutation(t *testing.T) {
	t.Run("local_to_shared", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "local.db")
		local, err := Open(path, 2, 1024)
		if err != nil {
			t.Fatal(err)
		}
		if err := local.Reserve("req_local", []byte("pending_local")); err != nil {
			t.Fatal(err)
		}
		if err := local.Close(); err != nil {
			t.Fatal(err)
		}
		before := sharedJournalBytes(t, path)
		_, err = OpenSharedSpool(context.Background(), path, SharedSpoolIdentity{InstallationID: "installation_shared", SpoolID: "spool_shared", ModeRevision: 2}, SharedSpoolOptions{Capacity: 2, MaxDataBytes: 1024, DeliveredCapacity: 4})
		if !errors.Is(err, ErrSharedSpoolMode) || !bytes.Equal(before, sharedJournalBytes(t, path)) {
			t.Fatalf("local journal changed during rejection: %v", err)
		}
	})
	t.Run("shared_to_local", func(t *testing.T) {
		spool, path, identity, _ := sharedJournalFixture(t, 2, 4)
		body := sharedJournalBody([]byte("pending_shared"))
		if err := spool.Reserve(context.Background(), sharedJournalKey(identity, "req_shared", body), body); err != nil {
			t.Fatal(err)
		}
		if err := spool.Close(); err != nil {
			t.Fatal(err)
		}
		before := sharedJournalBytes(t, path)
		_, err := Open(path, 2, 1024)
		if !errors.Is(err, ErrSharedSpoolMode) || !bytes.Equal(before, sharedJournalBytes(t, path)) {
			t.Fatalf("shared journal changed during rejection: %v", err)
		}
	})
	t.Run("partial_shared_marker_missing", func(t *testing.T) {
		spool, path, identity, options := sharedJournalFixture(t, 2, 4)
		if err := spool.db.Update(func(tx *bolt.Tx) error { return tx.DeleteBucket(sharedSpoolMetaBucket) }); err != nil {
			t.Fatal(err)
		}
		if err := spool.Close(); err != nil {
			t.Fatal(err)
		}
		before := sharedJournalBytes(t, path)
		if _, err := Open(path, 2, 1024); !errors.Is(err, ErrSharedSpoolMode) {
			t.Fatalf("partial shared journal downgraded = %v", err)
		}
		if !bytes.Equal(before, sharedJournalBytes(t, path)) {
			t.Fatal("local rejection changed a partial shared journal")
		}
		if _, err := OpenSharedSpool(context.Background(), path, identity, options); !errors.Is(err, ErrSharedSpoolMode) {
			t.Fatalf("partial journal was repaired = %v", err)
		}
		if !bytes.Equal(before, sharedJournalBytes(t, path)) {
			t.Fatal("shared rejection repaired a partial journal")
		}
	})
	for _, change := range []string{"installation", "spool", "mode", "capacity", "data_limit", "delivered_capacity"} {
		t.Run(change, func(t *testing.T) {
			spool, path, identity, options := sharedJournalFixture(t, 2, 4)
			if err := spool.Close(); err != nil {
				t.Fatal(err)
			}
			before := sharedJournalBytes(t, path)
			switch change {
			case "installation":
				identity.InstallationID = "installation_foreign"
			case "spool":
				identity.SpoolID = "spool_foreign"
			case "mode":
				identity.ModeRevision++
			case "capacity":
				options.Capacity++
			case "data_limit":
				options.MaxDataBytes--
			case "delivered_capacity":
				options.DeliveredCapacity++
			}
			_, err := OpenSharedSpool(context.Background(), path, identity, options)
			if !errors.Is(err, ErrSharedSpoolMode) || !bytes.Equal(before, sharedJournalBytes(t, path)) {
				t.Fatalf("foreign binding changed journal: %v", err)
			}
		})
	}
}

func TestSharedSpoolExclusiveOpenAndCanceledCreation(t *testing.T) {
	_, path, identity, options := sharedJournalFixture(t, 2, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if second, err := OpenSharedSpool(ctx, path, identity, options); err == nil {
		_ = second.Close()
		t.Fatal("two processes/handles acquired one spool")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	newPath := filepath.Join(t.TempDir(), "never-created", "shared.db")
	if _, err := OpenSharedSpool(canceled, newPath, identity, options); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(newPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled open created a journal")
	}
}

func TestSharedSpoolReopenRetainsPendingEvidenceAndCannotRecoverDispatchPermit(t *testing.T) {
	spool, path, identity, options := sharedJournalFixture(t, 2, 4)
	ctx := context.Background()
	body := sharedJournalBody([]byte("original_admission"))
	key := sharedJournalKey(identity, "req_shared", body)
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_shared"); err != nil {
		t.Fatal(err)
	}
	attempt := sharedJournalBody([]byte("interrupted_exact_attempt"))
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: 1, Body: attempt}); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmDispatch(ctx, SharedSpoolDispatch{Key: key, Number: 1, AttemptDigest: attempt.Digest}); err != nil {
		t.Fatal(err)
	}
	before, err := spool.ReadRecord(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSharedSpool(ctx, path, identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	after, err := reopened.ReadRecord(ctx, key)
	if err != nil || !reflect.DeepEqual(before, after) || after.Ready != nil || after.Terminal != nil {
		t.Fatalf("restart rewrote pending evidence: %+v %v", after, err)
	}
	ready, err := reopened.ReadReady(ctx, 2)
	if err != nil || len(ready) != 0 {
		t.Fatalf("restart promoted pending facts: %+v %v", ready, err)
	}
	newOwner := key
	newOwner.IncarnationID, newOwner.Fence = "incarnation_new", key.Fence+1
	if _, err := reopened.ReadRecord(ctx, newOwner); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatalf("new owner adopted old execution: %v", err)
	}
	if err := reopened.Reserve(ctx, newOwner, body); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatalf("restarted executor replaced original identity: %v", err)
	}
}

func TestSharedSpoolOrderedCASAndIndependentBytes(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 2, 4)
	ctx := context.Background()
	original := []byte("original_admission")
	body := sharedJournalBody(original)
	key := sharedJournalKey(identity, "req_shared", body)
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	original[0] = '!'
	read, err := spool.ReadRecord(ctx, key)
	if err != nil || string(read.Intent.Data) != "original_admission" {
		t.Fatalf("input alias changed journal: %+v %v", read, err)
	}
	read.Intent.Data[0] = '?'
	read, err = spool.ReadRecord(ctx, key)
	if err != nil || string(read.Intent.Data) != "original_admission" {
		t.Fatal("read alias changed journal")
	}
	if err := spool.Reserve(ctx, key, sharedJournalBody([]byte("original_admission"))); err != nil {
		t.Fatal(err)
	}
	before := read
	attempt := sharedJournalBody([]byte("attempt_one"))
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: 1, Body: attempt}); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatalf("attempt before acceptance = %v", err)
	}
	unchanged, err := spool.ReadRecord(ctx, key)
	if err != nil || !reflect.DeepEqual(before, unchanged) {
		t.Fatal("failed transition partially committed")
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_shared"); err != nil {
		t.Fatal(err)
	}
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: 2, Body: attempt}); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatal("skipped attempt order")
	}
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: 1, Body: attempt}); err != nil {
		t.Fatal(err)
	}
	checkpoint := sharedJournalBody([]byte("original_checkpoint"))
	if err := spool.RecordAttempt(ctx, key, SharedSpoolCheckpoint{Number: 1, AttemptDigest: attempt.Digest, Body: checkpoint}); err != nil {
		t.Fatal(err)
	}
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: 2, Body: sharedJournalBody([]byte("attempt_two"))}); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatal("checkpoint without dispatch receipt authorized another attempt")
	}
	if err := spool.RecordAttempt(ctx, key, SharedSpoolCheckpoint{Number: 1, AttemptDigest: attempt.Digest, Body: sharedJournalBody([]byte("replacement_checkpoint"))}); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatal("checkpoint replaced immutable evidence")
	}
	if err := spool.ConfirmDispatch(ctx, SharedSpoolDispatch{Key: key, Number: 1, AttemptDigest: attempt.Digest}); err != nil {
		t.Fatal(err)
	}
	terminal := sharedJournalBody([]byte("original_terminal"))
	if err := spool.PrepareTerminal(ctx, key, SharedSpoolTerminal{LastAttempt: 1, Body: terminal}); err != nil {
		t.Fatal(err)
	}
	wrong := SharedSpoolTerminalReceipt{Key: key, ReceiptID: "receipt_other", TerminalDigest: terminal.Digest}
	if err := spool.MakeReady(ctx, wrong); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatal("wrong authority receipt made ready")
	}
	wrong.ReceiptID, wrong.TerminalDigest = "receipt_shared", attempt.Digest
	if err := spool.MakeReady(ctx, wrong); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatal("wrong terminal digest made ready")
	}
	ready, err := spool.ReadReady(ctx, 2)
	if err != nil || len(ready) != 0 {
		t.Fatal("prepared terminal became delivery-ready")
	}
	correct := SharedSpoolTerminalReceipt{Key: key, ReceiptID: "receipt_shared", TerminalDigest: terminal.Digest}
	if err := spool.MakeReady(ctx, correct); err != nil {
		t.Fatal(err)
	}
	ready, err = spool.ReadReady(ctx, 2)
	if err != nil || len(ready) != 1 || !bytes.Equal(ready[0].Data, terminal.Data) {
		t.Fatalf("ready fact = %+v %v", ready, err)
	}
	ready[0].Data[0] = '!'
	ready, err = spool.ReadReady(ctx, 2)
	if err != nil || !bytes.Equal(ready[0].Data, terminal.Data) {
		t.Fatal("delivery alias changed original fact")
	}
}

func TestSharedSpoolCapacityContentionAndDeliveredHistoryFailClosed(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 1, 1)
	ctx := context.Background()
	body := sharedJournalBody([]byte("admission"))
	keys := []SharedSpoolKey{sharedJournalKey(identity, "req_one", body), sharedJournalKey(identity, "req_two", body)}
	errorsByKey := make([]error, 2)
	var workers sync.WaitGroup
	for index, key := range keys {
		workers.Add(1)
		go func() { defer workers.Done(); errorsByKey[index] = spool.Reserve(ctx, key, body) }()
	}
	workers.Wait()
	winner, loser := 0, 1
	if errorsByKey[0] != nil {
		winner, loser = 1, 0
	}
	if errorsByKey[winner] != nil || !errors.Is(errorsByKey[loser], ErrFull) {
		t.Fatalf("one-slot contention = %v", errorsByKey)
	}
	first := sharedJournalReady(t, spool, keys[winner], body)
	if err := spool.AckReady(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := spool.AckReady(ctx, first); err != nil {
		t.Fatal("matching repeated acknowledgement failed", err)
	}
	if err := spool.Reserve(ctx, first.Key, body); !errors.Is(err, ErrSharedSpoolDelivered) {
		t.Fatalf("delivered request was recreated = %v", err)
	}
	second := sharedJournalReady(t, spool, keys[loser], body)
	if err := spool.AckReady(ctx, second); !errors.Is(err, ErrSharedSpoolHistory) {
		t.Fatalf("history silently evicted = %v", err)
	}
	entries, err := spool.ReadReady(ctx, 1)
	if err != nil || len(entries) != 1 || entries[0].Receipt != second {
		t.Fatal("history exhaustion erased an undelivered fact")
	}
	if err := spool.AckReady(ctx, first); err != nil {
		t.Fatal("history exhaustion broke original acknowledgement idempotency", err)
	}
	changed := first
	changed.ReceiptID = "receipt_changed"
	if err := spool.AckReady(ctx, changed); !errors.Is(err, ErrSharedSpoolConflict) {
		t.Fatalf("changed delivered receipt = %v", err)
	}
	third := sharedJournalKey(identity, "req_three", body)
	if err := spool.Reserve(ctx, third, body); !errors.Is(err, ErrFull) {
		t.Fatal("full pending fact slot was reused")
	}
}

func TestSharedSpoolTotalRepeatedFactBoundsAndCancellationRollback(t *testing.T) {
	spool, path, identity, _ := sharedJournalFixture(t, 1, 2)
	ctx := context.Background()
	data := bytes.Repeat([]byte("x"), SharedSpoolMaxDataBytes)
	body := sharedJournalBody(data)
	key := sharedJournalKey(identity, "req_bounds", body)
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_bounds"); err != nil {
		t.Fatal(err)
	}
	for number := 1; number <= routeattempt.MaxAttempts; number++ {
		if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: number, Body: body}); err != nil {
			t.Fatal(err)
		}
		if err := spool.ConfirmDispatch(ctx, SharedSpoolDispatch{Key: key, Number: number, AttemptDigest: body.Digest}); err != nil {
			t.Fatal(err)
		}
		if err := spool.RecordAttempt(ctx, key, SharedSpoolCheckpoint{Number: number, AttemptDigest: body.Digest, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := spool.PrepareTerminal(ctx, key, SharedSpoolTerminal{LastAttempt: routeattempt.MaxAttempts, Body: body}); err != nil {
		t.Fatal(err)
	}
	if err := spool.db.View(func(tx *bolt.Tx) error {
		length := len(tx.Bucket(sharedSpoolRecordBucket).Get([]byte(key.RequestID)))
		if length > SharedSpoolMaxRecordBytes || length < (2+2*routeattempt.MaxAttempts)*SharedSpoolMaxDataBytes {
			t.Fatalf("total repeated-data bound = %d", length)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := sharedJournalBytes(t, path)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := spool.MakeReady(canceled, SharedSpoolTerminalReceipt{Key: key, ReceiptID: "receipt_bounds", TerminalDigest: body.Digest}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, sharedJournalBytes(t, path)) {
		t.Fatal("canceled operation mutated journal")
	}
	inside, stop := context.WithCancel(ctx)
	if err := spool.update(inside, func(tx *bolt.Tx) error {
		if err := tx.Bucket(sharedSpoolReadyBucket).Put([]byte("invalid_partial"), []byte("must_rollback")); err != nil {
			return err
		}
		stop()
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation before commit = %v", err)
	}
	if !bytes.Equal(before, sharedJournalBytes(t, path)) {
		t.Fatal("cancellation committed partial transaction")
	}
	if err := spool.PrepareAttempt(ctx, key, SharedSpoolAttempt{Number: routeattempt.MaxAttempts + 1, Body: body}); !errors.Is(err, ErrInvalid) {
		t.Fatal("attempt bound was extended")
	}
	oversized := sharedJournalBody(bytes.Repeat([]byte("x"), SharedSpoolMaxDataBytes+1))
	if err := spool.PrepareTerminal(ctx, key, SharedSpoolTerminal{LastAttempt: routeattempt.MaxAttempts, Body: oversized}); !errors.Is(err, ErrInvalid) {
		t.Fatal("serialized body bound was bypassed")
	}
}

func TestSharedSpoolCorruptBodyReopenRejectsWithoutRepair(t *testing.T) {
	spool, path, identity, options := sharedJournalFixture(t, 2, 4)
	body := sharedJournalBody([]byte("original"))
	key := sharedJournalKey(identity, "req_corrupt", body)
	if err := spool.Reserve(context.Background(), key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(sharedSpoolRecordBucket)
		var record SharedSpoolRecord
		if err := json.Unmarshal(bucket.Get([]byte(key.RequestID)), &record); err != nil {
			return err
		}
		record.Intent.Data[0] = '!'
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		return bucket.Put([]byte(key.RequestID), raw)
	}); err != nil {
		t.Fatal(err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	before := sharedJournalBytes(t, path)
	if _, err := OpenSharedSpool(context.Background(), path, identity, options); !errors.Is(err, ErrInvalid) {
		t.Fatalf("corrupt body reopened = %v", err)
	}
	if !bytes.Equal(before, sharedJournalBytes(t, path)) {
		t.Fatal("corrupt-body rejection repaired/mutated journal")
	}
}
