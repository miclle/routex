package eventqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/miclle/routex/pkg/routeattempt"
	bolt "go.etcd.io/bbolt"
)

func sharedPendingReserve(t *testing.T, spool *SharedSpool, request string) SharedSpoolKey {
	t.Helper()
	body := sharedJournalBody([]byte("original:" + request))
	key := sharedJournalKey(spool.Identity(), request, body)
	key.ExecutionID = "execution_" + request
	if err := spool.Reserve(context.Background(), key, body); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestSharedSpoolPendingReopenPreservesExactEvidenceAndReadOnlyPages(t *testing.T) {
	spool, path, identity, options := sharedJournalFixture(t, 8, 8)
	ctx := context.Background()
	first := sharedPendingReserve(t, spool, "req_a")
	body := sharedJournalBody([]byte("ready_body"))
	ready := sharedJournalReady(t, spool, sharedJournalKey(identity, "req_b", body), body)
	second := sharedPendingReserve(t, spool, "req_c")
	if err := spool.ConfirmAcceptance(ctx, second, "receipt_c"); err != nil {
		t.Fatal(err)
	}
	attempt := sharedJournalBody([]byte("original_unknown_dispatch"))
	if err := spool.PrepareAttempt(ctx, second, SharedSpoolAttempt{Number: 1, Body: attempt}); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmDispatch(ctx, SharedSpoolDispatch{Key: second, Number: 1, AttemptDigest: attempt.Digest}); err != nil {
		t.Fatal(err)
	}
	third := sharedPendingReserve(t, spool, "req_d")
	if err := spool.ConfirmAcceptance(ctx, third, "receipt_d"); err != nil {
		t.Fatal(err)
	}
	if err := spool.PrepareTerminal(ctx, third, SharedSpoolTerminal{Body: sharedJournalBody([]byte("prepared_only"))}); err != nil {
		t.Fatal(err)
	}
	deliveredBody := sharedJournalBody([]byte("delivered_body"))
	delivered := sharedJournalReady(t, spool, sharedJournalKey(identity, "req_e", deliveredBody), deliveredBody)
	if err := spool.AckReady(ctx, delivered); err != nil {
		t.Fatal(err)
	}
	before, err := spool.ReadRecord(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	page, err := spool.ReadPending(ctx, nil, 1)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{first}) || page.Next == nil || page.Next.AfterRequestID != first.RequestID {
		t.Fatal("first pending page lost its original key", page, err)
	}
	if err := spool.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSharedSpool(ctx, path, identity, options)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	fileBefore := sharedJournalBytes(t, path)
	page, err = reopened.ReadPending(ctx, page.Next, 64)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{second, third}) || page.Next != nil {
		t.Fatal("reopen pagination lost pending stages or included ready/delivered keys", page, err)
	}
	after, err := reopened.ReadRecord(ctx, second)
	if err != nil || !reflect.DeepEqual(before, after) || after.Ready != nil || after.Terminal != nil {
		t.Fatal("enumeration changed unknown dispatch evidence", after, err)
	}
	readyEntries, err := reopened.ReadReady(ctx, 1)
	if err != nil || len(readyEntries) != 1 || readyEntries[0].Receipt != ready {
		t.Fatal("pending read changed existing delivery", readyEntries, err)
	}
	if !bytes.Equal(fileBefore, sharedJournalBytes(t, path)) {
		t.Fatal("pending enumeration wrote the journal")
	}
}

func TestSharedSpoolPendingReadyOnlyPagesRespectScanBound(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 70, 70)
	for i := range 65 {
		body := sharedJournalBody([]byte(fmt.Sprintf("ready_%03d", i)))
		sharedJournalReady(t, spool, sharedJournalKey(identity, fmt.Sprintf("req_%03d", i), body), body)
	}
	pending := sharedPendingReserve(t, spool, "req_zzz")
	page, err := spool.ReadPending(context.Background(), nil, 64)
	if err != nil || len(page.Keys) != 0 || page.Next == nil || page.Next.AfterRequestID != "req_063" {
		t.Fatal("ready rows bypassed scan bound or prevented progress", page, err)
	}
	page, err = spool.ReadPending(context.Background(), page.Next, 64)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{pending}) || page.Next != nil {
		t.Fatal("ready-only cursor skipped pending key", page, err)
	}
}

func sharedPendingMaximumRecord(t *testing.T, spool *SharedSpool, request string) SharedSpoolKey {
	t.Helper()
	ctx := context.Background()
	body := sharedJournalBody(bytes.Repeat([]byte("x"), SharedSpoolMaxDataBytes))
	key := sharedJournalKey(spool.Identity(), request, body)
	if err := spool.Reserve(ctx, key, body); err != nil {
		t.Fatal(err)
	}
	if err := spool.ConfirmAcceptance(ctx, key, "receipt_maximum"); err != nil {
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
	return key
}

func TestSharedSpoolPendingRawByteBudgetDoesNotSkipDeferredRecord(t *testing.T) {
	spool, _, _, _ := sharedJournalFixture(t, 8, 8)
	keys := []SharedSpoolKey{}
	for i := range 6 {
		keys = append(keys, sharedPendingMaximumRecord(t, spool, fmt.Sprintf("req_%02d", i)))
	}
	page, err := spool.ReadPending(context.Background(), nil, 64)
	if err != nil || len(page.Keys) != 4 || !reflect.DeepEqual(page.Keys, keys[:4]) || page.Next == nil || page.Next.AfterRequestID != keys[3].RequestID {
		t.Fatal("maximum records bypassed byte budget or lost progress", page, err)
	}
	if err := spool.db.View(func(tx *bolt.Tx) error {
		consumed := 0
		for i, key := range keys[:5] {
			consumed += len(key.RequestID) + len(tx.Bucket(sharedSpoolRecordBucket).Get([]byte(key.RequestID)))
			if i == 3 && consumed > SharedSpoolPendingMaxReadBytes || i == 4 && consumed <= SharedSpoolPendingMaxReadBytes {
				t.Fatal("fixture does not straddle the original raw byte budget", consumed)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page, err = spool.ReadPending(context.Background(), page.Next, 64)
	if err != nil || !reflect.DeepEqual(page.Keys, keys[4:]) || page.Next != nil {
		t.Fatal("byte-limited page skipped its deferred original key", page, err)
	}
}

type sharedPendingCanceledContext struct {
	context.Context
	checks int
}

func (ctx *sharedPendingCanceledContext) Err() error {
	ctx.checks++
	if ctx.checks >= 4 {
		return context.Canceled
	}
	return nil
}

func TestSharedSpoolPendingCursorLimitsAndCancellation(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 2, 2)
	sharedPendingReserve(t, spool, "req_a")
	sharedPendingReserve(t, spool, "req_b")
	for _, limit := range []int{-1, 0, 65} {
		if page, err := spool.ReadPending(context.Background(), nil, limit); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(page, SharedSpoolPendingPage{}) {
			t.Fatal("invalid limit returned a partial page", limit, page, err)
		}
	}
	for _, edit := range []func(*SharedSpoolPendingCursor){
		func(c *SharedSpoolPendingCursor) { c.Identity.InstallationID = "foreign" },
		func(c *SharedSpoolPendingCursor) { c.Identity.SpoolID = "foreign" },
		func(c *SharedSpoolPendingCursor) { c.Identity.ModeRevision++ },
		func(c *SharedSpoolPendingCursor) { c.AfterRequestID = "" },
		func(c *SharedSpoolPendingCursor) { c.AfterRequestID = "invalid/key" },
		func(c *SharedSpoolPendingCursor) { c.AfterRequestID = string(bytes.Repeat([]byte("x"), 65)) },
	} {
		cursor := SharedSpoolPendingCursor{Identity: identity, AfterRequestID: "req_a"}
		edit(&cursor)
		if page, err := spool.ReadPending(context.Background(), &cursor, 64); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(page, SharedSpoolPendingPage{}) {
			t.Fatal("foreign or malformed cursor accepted", cursor, page, err)
		}
	}
	if page, err := spool.ReadPending(context.Background(), &SharedSpoolPendingCursor{Identity: identity, AfterRequestID: "req_z"}, 64); err != nil || len(page.Keys) != 0 || page.Next != nil {
		t.Fatal("after-end cursor did not finish", page, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, ctx := range []context.Context{canceled, expired, &sharedPendingCanceledContext{Context: context.Background()}} {
		if page, err := spool.ReadPending(ctx, nil, 64); err == nil || !reflect.DeepEqual(page, SharedSpoolPendingPage{}) {
			t.Fatal("canceled read exposed a partial page", page, err)
		}
	}
	//lint:ignore SA1012 This deliberately verifies rejection of a nil context.
	//nolint:staticcheck // Intentionally exercise nil-context rejection.
	if _, err := spool.ReadPending(nil, nil, 64); !errors.Is(err, ErrInvalid) {
		t.Fatal("nil context accepted", err)
	}
}

func TestSharedSpoolPendingPagesRequireRepeatSweepsForEarlierInsertions(t *testing.T) {
	spool, _, identity, _ := sharedJournalFixture(t, 8, 8)
	first := sharedPendingReserve(t, spool, "req_b")
	last := sharedPendingReserve(t, spool, "req_d")
	page, err := spool.ReadPending(context.Background(), nil, 1)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{first}) || page.Next == nil {
		t.Fatal(page, err)
	}
	earlier := sharedPendingReserve(t, spool, "req_a")
	middle := sharedPendingReserve(t, spool, "req_c")
	body := sharedJournalBody([]byte("original:req_b"))
	receipt := sharedJournalReady(t, spool, first, body)
	if err := spool.AckReady(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	// The exact cursor remains usable after its record is acknowledged/deleted.
	page, err = spool.ReadPending(context.Background(), page.Next, 64)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{middle, last}) || page.Next != nil {
		t.Fatal("exclusive seek lost the next surviving key", page, err)
	}
	page, err = spool.ReadPending(context.Background(), nil, 64)
	if err != nil || !reflect.DeepEqual(page.Keys, []SharedSpoolKey{earlier, middle, last}) || page.Next != nil || page.Keys[0].Identity != identity {
		t.Fatal("repeat sweep did not discover earlier insertion", page, err)
	}
}

func TestSharedSpoolPendingCorruptionReturnsNoPartialKeys(t *testing.T) {
	for _, kind := range []string{"record_json", "record_checksum", "record_key", "record_oversize", "pending_ready_index", "ready_missing_index", "ready_changed_index", "ready_oversize_index", "duplicate_delivered"} {
		t.Run(kind, func(t *testing.T) {
			spool, path, identity, _ := sharedJournalFixture(t, 4, 4)
			sharedPendingReserve(t, spool, "req_a")
			key := sharedPendingReserve(t, spool, "req_b")
			if kind == "ready_missing_index" || kind == "ready_changed_index" || kind == "ready_oversize_index" || kind == "duplicate_delivered" {
				sharedJournalReady(t, spool, key, sharedJournalBody([]byte("original:req_b")))
			}
			if err := spool.db.Update(func(tx *bolt.Tx) error {
				records, ready := tx.Bucket(sharedSpoolRecordBucket), tx.Bucket(sharedSpoolReadyBucket)
				request := []byte(key.RequestID)
				switch kind {
				case "record_json":
					return records.Put(request, []byte("{"))
				case "record_oversize":
					return records.Put(request, bytes.Repeat([]byte("x"), SharedSpoolMaxRecordBytes+1))
				case "pending_ready_index":
					return ready.Put(request, []byte("{}"))
				case "ready_missing_index":
					return ready.Delete(request)
				case "ready_changed_index":
					return ready.Put(request, []byte("{}"))
				case "ready_oversize_index":
					return ready.Put(request, bytes.Repeat([]byte("x"), SharedSpoolMaxDeliveredBytes+1))
				case "duplicate_delivered":
					return tx.Bucket(sharedSpoolAckBucket).Put(request, bytes.Clone(ready.Get(request)))
				default:
					var record SharedSpoolRecord
					if err := json.Unmarshal(records.Get(request), &record); err != nil {
						return err
					}
					if kind == "record_checksum" {
						record.Intent.Data[0] = '!'
					} else {
						record.Key.RequestID = "req_other"
					}
					raw, err := json.Marshal(record)
					if err != nil {
						return err
					}
					return records.Put(request, raw)
				}
			}); err != nil {
				t.Fatal(err)
			}
			before := sharedJournalBytes(t, path)
			if page, err := spool.ReadPending(context.Background(), nil, 64); !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(page, SharedSpoolPendingPage{}) {
				t.Fatal("corrupt suffix exposed earlier valid keys", page, err)
			}
			if !bytes.Equal(before, sharedJournalBytes(t, path)) || spool.Identity() != identity {
				t.Fatal("corrupt read repaired or rebound the journal")
			}
		})
	}
}
