package eventqueue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

func localInspectionFileHash(t *testing.T, path string) [32]byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(raw)
}

func localInspectionFixture(t *testing.T, quota bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "local.db")
	queue, err := Open(path, 16, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if quota {
		if err := queue.EnableQuota("UTC", quotaTestTime); err != nil {
			t.Fatal(err)
		}
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func localInspectionMutate(t *testing.T, path string, change func(*bolt.Tx) error) {
	t.Helper()
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(change); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestLocalInspectionPreservesActiveReceiptsAndExactEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.db")
	queue, err := Open(path, 16, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.BindInstallation("led_original", false); err != nil {
		t.Fatal(err)
	}
	if err := queue.EnableQuota("UTC", quotaTestTime); err != nil {
		t.Fatal(err)
	}
	policy := quotaPolicy("key_original")
	policy.RPM, policy.Concurrency = limitPtr(5), limitPtr(5)
	known := QuotaBound{Tokens: limitPtr(10), Money: moneyPtr("1.25"), Currency: "USD", Revision: "bound_known"}
	unknown := QuotaBound{Currency: "USD", Revision: "bound_unknown"}
	requireReserve(t, queue, "known", []QuotaLimit{policy}, known, quotaTestTime.Add(time.Minute))
	requireReserve(t, queue, "unknown", []QuotaLimit{policy}, unknown, quotaTestTime.Add(2*time.Minute))
	usage := requireUsage(t, queue, policy.Account, quotaTestTime.Add(2*time.Minute))
	if usage.Active.TokensHeld != 10 || usage.Active.TokensUnknown != 1 || usage.Active.MoneyHeld["USD"] != "1.25" || usage.Active.MoneyUnknown != 1 {
		t.Fatalf("fixture lacks distinct known and unknown holds: %+v", usage.Active)
	}
	want := map[string][]LocalInspectionEntry{}
	sequences := map[string]uint64{}
	if err := queue.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(pendingBucket).SetSequence(37)
	}); err != nil {
		t.Fatal(err)
	}
	if err := queue.db.View(func(tx *bolt.Tx) error {
		return tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
			sequences[string(name)] = bucket.Sequence()
			want[string(name)] = []LocalInspectionEntry{}
			return bucket.ForEach(func(key, value []byte) error {
				want[string(name)] = append(want[string(name)], LocalInspectionEntry{Key: bytes.Clone(key), Value: bytes.Clone(value)})
				return nil
			})
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	before := localInspectionFileHash(t, path)
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inspection.Close() })
	header, err := inspection.Header(context.Background())
	if err != nil || header.FormatVersion != 2 || header.InstallationID != "led_original" || header.Quota == nil || header.Quota.TimeZone != "UTC" || header.Quota.CoverageStart != quotaTestTime.UnixNano() || header.ClockHighWater == nil || *header.ClockHighWater != quotaTestTime.Add(2*time.Minute).UnixNano() {
		t.Fatalf("recorded header was changed: %+v, %v", header, err)
	}
	got := map[string][]LocalInspectionEntry{}
	for _, bucket := range header.Buckets {
		if bucket.Sequence != sequences[string(bucket.Name)] {
			t.Fatalf("sequence changed for %s", bucket.Name)
		}
		got[string(bucket.Name)] = []LocalInspectionEntry{}
		var after []byte
		for {
			page, err := inspection.ReadPage(context.Background(), bucket.Name, after, 1, LocalInspectionMaxPageBytes)
			if err != nil || page.Sequence != bucket.Sequence {
				t.Fatalf("read exact evidence: %+v, %v", page, err)
			}
			got[string(bucket.Name)] = append(got[string(bucket.Name)], page.Entries...)
			if page.Exhausted {
				break
			}
			after = page.NextAfter
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("raw local evidence differs from the closed original")
	}
	for _, entry := range got[string(quotaEntryBucket)] {
		var receipt QuotaReceipt
		if err := json.Unmarshal(entry.Value, &receipt); err != nil || receipt.State != "active" || receipt.SettlementDigest != "" {
			t.Fatalf("inspection recovered an active receipt: %+v, %v", receipt, err)
		}
	}
	if len(got[string(pendingBucket)]) != 2 || len(got[string(readyBucket)]) != 0 {
		t.Fatal("inspection promoted pending call facts")
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if after := localInspectionFileHash(t, path); after != before {
		t.Fatal("inspection changed the whole journal file")
	}
}

func TestLocalInspectionDetachedHeaderAndPages(t *testing.T) {
	path := localInspectionFixture(t, true)
	localInspectionMutate(t, path, func(tx *bolt.Tx) error {
		return tx.Bucket(pendingBucket).Put([]byte("original"), []byte("opaque evidence"))
	})
	before := localInspectionFileHash(t, path)
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inspection.Close() })
	header, err := inspection.Header(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	originalHeader, err := inspection.Header(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range header.Metadata {
		entry.Key[0] ^= 1
		entry.Value[0] ^= 1
	}
	for index := range header.Buckets {
		header.Buckets[index].Name[0] ^= 1
		header.Buckets[index].Sequence++
	}
	*header.ClockHighWater = 1
	header.Quota.TimeZone, header.Quota.CoverageStart = "changed", 1
	currentHeader, err := inspection.Header(context.Background())
	if err != nil || !reflect.DeepEqual(currentHeader, originalHeader) {
		t.Fatal("caller mutation changed the retained header")
	}
	page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 2, 1024)
	if err != nil || len(page.Entries) != 1 {
		t.Fatalf("first detached page: %+v, %v", page, err)
	}
	page.Bucket[0] ^= 1
	page.NextAfter[0] ^= 1
	page.Entries[0].Key[0] ^= 1
	page.Entries[0].Value[0] ^= 1
	current, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 2, 1024)
	if err != nil || string(current.Entries[0].Key) != "original" || string(current.Entries[0].Value) != "opaque evidence" || string(current.NextAfter) != "original" {
		t.Fatalf("caller mutation changed journal/page bytes: %+v, %v", current, err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if localInspectionFileHash(t, path) != before {
		t.Fatal("detached reads modified the file")
	}
	if _, err := inspection.Header(context.Background()); !errors.Is(err, ErrLocalInspectionClosed) {
		t.Fatalf("closed header: %v", err)
	}
	if _, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 1, 1024); !errors.Is(err, ErrLocalInspectionClosed) {
		t.Fatalf("closed page: %v", err)
	}
}

func TestLocalInspectionVersionOneRetainsMissingFacts(t *testing.T) {
	path := localInspectionFixture(t, false)
	before := localInspectionFileHash(t, path)
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := inspection.Header(context.Background())
	if err != nil || header.FormatVersion != 1 || header.InstallationID != "" || header.ClockHighWater != nil || header.Quota != nil || len(header.Buckets) != 7 {
		t.Fatalf("missing v1 facts were invented: %+v, %v", header, err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if localInspectionFileHash(t, path) != before {
		t.Fatal("version-one inspection initialized metadata")
	}
}

func TestLocalInspectionMissingNonregularAndMalformedFiles(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "absent-parent", "local.db")
	if _, err := OpenLocalInspection(context.Background(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing file: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created a parent directory")
	}
	if _, err := OpenLocalInspection(context.Background(), root); !errors.Is(err, ErrInvalid) {
		t.Fatalf("directory accepted: %v", err)
	}
	for _, raw := range [][]byte{nil, []byte("not a bbolt database")} {
		path := filepath.Join(t.TempDir(), "invalid.db")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		before := localInspectionFileHash(t, path)
		if inspection, err := OpenLocalInspection(context.Background(), path); err == nil {
			_ = inspection.Close()
			t.Fatal("invalid database accepted")
		}
		if localInspectionFileHash(t, path) != before {
			t.Fatal("invalid database was initialized or repaired")
		}
	}
	path := localInspectionFixture(t, false)
	link := filepath.Join(root, "symlink.db")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLocalInspection(context.Background(), link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink accepted: %v", err)
	}
}

func TestLocalInspectionLockLifetimeAndCancellation(t *testing.T) {
	path := localInspectionFixture(t, false)
	before := localInspectionFileHash(t, path)
	writer, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	for _, cancelOnly := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
		var cancelTimer *time.Timer
		if cancelOnly {
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
			cancelTimer = time.AfterFunc(25*time.Millisecond, cancel)
		}
		started := time.Now()
		inspection, err := OpenLocalInspection(ctx, path)
		cancel()
		if cancelTimer != nil {
			cancelTimer.Stop()
		}
		if inspection != nil || (!cancelOnly && !errors.Is(err, context.DeadlineExceeded)) || (cancelOnly && !errors.Is(err, context.Canceled)) {
			t.Fatalf("lock cancellation: inspection=%v error=%v", inspection, err)
		}
		if time.Since(started) > time.Second {
			t.Fatal("context-bounded lock waited beyond its ceiling")
		}
	}
	if inspection, err := OpenLocalInspection(context.Background(), path); inspection != nil || !errors.Is(err, berrors.ErrTimeout) {
		t.Fatalf("fixed lock ceiling: inspection=%v error=%v", inspection, err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := bolt.Open(path, 0600, &bolt.Options{Timeout: 25 * time.Millisecond}); !errors.Is(err, berrors.ErrTimeout) {
		if other != nil {
			_ = other.Close()
		}
		t.Fatalf("inspection did not exclude a writer: %v", err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal("close was not idempotent")
	}
	writer, err = bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal("close did not release its transaction and file lock", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if localInspectionFileHash(t, path) != before {
		t.Fatal("lock attempts changed the file")
	}
}

func TestLocalInspectionRejectsNamespacesAndHeaderShapesWithoutRepair(t *testing.T) {
	cases := []struct {
		name   string
		quota  bool
		change func(*bolt.Tx) error
	}{
		{"unknown_bucket", false, func(tx *bolt.Tx) error { _, err := tx.CreateBucket([]byte("future-v3")); return err }},
		{"mixed_shared_bucket", false, func(tx *bolt.Tx) error { _, err := tx.CreateBucket(sharedSpoolMetaBucket); return err }},
		{"missing_local_bucket", false, func(tx *bolt.Tx) error { return tx.DeleteBucket(readyBucket) }},
		{"unknown_version", false, func(tx *bolt.Tx) error { return tx.Bucket(limitMetaBucket).Put([]byte("version"), []byte("3")) }},
		{"v1_quota_bucket", false, func(tx *bolt.Tx) error { _, err := tx.CreateBucket(quotaEntryBucket); return err }},
		{"v1_quota_metadata", false, func(tx *bolt.Tx) error {
			return tx.Bucket(limitMetaBucket).Put([]byte("quota"), []byte(`{"TimeZone":"UTC","CoverageStart":1}`))
		}},
		{"missing_v2_bucket", true, func(tx *bolt.Tx) error { return tx.DeleteBucket(quotaCounterBucket) }},
		{"missing_v2_metadata", true, func(tx *bolt.Tx) error { return tx.Bucket(limitMetaBucket).Delete([]byte("quota")) }},
		{"unknown_metadata", false, func(tx *bolt.Tx) error { return tx.Bucket(limitMetaBucket).Put([]byte("future"), []byte("1")) }},
		{"nested_metadata", false, func(tx *bolt.Tx) error {
			_, err := tx.Bucket(limitMetaBucket).CreateBucket([]byte("nested"))
			return err
		}},
		{"malformed_clock", true, func(tx *bolt.Tx) error { return tx.Bucket(limitMetaBucket).Put([]byte("clock"), []byte("broken")) }},
		{"missing_v2_clock", true, func(tx *bolt.Tx) error { return tx.Bucket(limitMetaBucket).Delete([]byte("clock")) }},
		{"clock_before_coverage", true, func(tx *bolt.Tx) error {
			raw := make([]byte, 8)
			binary.BigEndian.PutUint64(raw, 1)
			return tx.Bucket(limitMetaBucket).Put([]byte("clock"), raw)
		}},
		{"future_quota_shape", true, func(tx *bolt.Tx) error {
			return tx.Bucket(limitMetaBucket).Put([]byte("quota"), []byte(`{"TimeZone":"UTC","CoverageStart":1,"Future":true}`))
		}},
		{"invalid_calendar", true, func(tx *bolt.Tx) error {
			return tx.Bucket(limitMetaBucket).Put([]byte("quota"), []byte(`{"TimeZone":"Invalid/Calendar","CoverageStart":1}`))
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := localInspectionFixture(t, test.quota)
			localInspectionMutate(t, path, test.change)
			before := localInspectionFileHash(t, path)
			if inspection, err := OpenLocalInspection(context.Background(), path); inspection != nil || !errors.Is(err, ErrInvalid) {
				if inspection != nil {
					_ = inspection.Close()
				}
				t.Fatalf("namespace/header accepted: %v", err)
			}
			if localInspectionFileHash(t, path) != before {
				t.Fatal("rejected journal was repaired or mutated")
			}
			// Failed inspection must also relinquish the original file lock.
			localInspectionMutate(t, path, func(*bolt.Tx) error { return nil })
		})
	}
	t.Run("shared_only", func(t *testing.T) {
		spool, path, _, _ := sharedJournalFixture(t, 2, 2)
		if err := spool.Close(); err != nil {
			t.Fatal(err)
		}
		before := localInspectionFileHash(t, path)
		if inspection, err := OpenLocalInspection(context.Background(), path); inspection != nil || !errors.Is(err, ErrInvalid) {
			t.Fatalf("shared-only accepted: %v", err)
		}
		if localInspectionFileHash(t, path) != before {
			t.Fatal("shared-only journal was modified")
		}
	})
}

func TestLocalInspectionBoundedPagesAndOpaqueSemanticCorruption(t *testing.T) {
	path := localInspectionFixture(t, true)
	localInspectionMutate(t, path, func(tx *bolt.Tx) error {
		bucket := tx.Bucket(pendingBucket)
		if err := bucket.Put([]byte("a"), []byte("first")); err != nil {
			return err
		}
		if err := bucket.Put([]byte("b"), []byte("second")); err != nil {
			return err
		}
		if err := tx.Bucket(quotaEntryBucket).Put([]byte("opaque"), []byte("not a valid semantic receipt")); err != nil {
			return err
		}
		_, err := bucket.CreateBucket([]byte("z_nested"))
		return err
	})
	before := localInspectionFileHash(t, path)
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = inspection.Close() })
	for _, input := range []struct{ entries, bytes int }{{0, 1024}, {513, 1024}, {1, 0}, {1, LocalInspectionMaxPageBytes + 1}} {
		if page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, input.entries, input.bytes); !errors.Is(err, ErrInvalid) || len(page.Entries) != 0 {
			t.Fatalf("invalid page limits: %+v, %v", page, err)
		}
	}
	for _, budget := range []int{1, len(pendingBucket) + 8 + len("a") + len("first") - 1} {
		if page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 2, budget); !errors.Is(err, ErrLocalInspectionBound) || len(page.Entries) != 0 {
			t.Fatalf("page must not truncate an individual entry: %+v, %v", page, err)
		}
	}
	budget := len(pendingBucket) + 8 + len("a") + len("first")
	page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 2, budget)
	if err != nil || len(page.Entries) != 1 || string(page.NextAfter) != "a" || page.Exhausted || page.RawBytes != budget {
		t.Fatalf("bounded page did not stop before the second entry: %+v, %v", page, err)
	}
	page, err = inspection.ReadPage(context.Background(), pendingBucket, []byte("aa"), 1, 1024)
	if err != nil || len(page.Entries) != 1 || string(page.Entries[0].Key) != "b" {
		t.Fatalf("exclusive nonexisting cursor: %+v, %v", page, err)
	}
	if page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 3, 1024); !errors.Is(err, ErrInvalid) || len(page.Entries) != 0 || len(page.NextAfter) != 0 {
		t.Fatalf("nested-bucket error returned partial data: %+v, %v", page, err)
	}
	page, err = inspection.ReadPage(context.Background(), quotaEntryBucket, nil, 1, 1024)
	if err != nil || string(page.Entries[0].Value) != "not a valid semantic receipt" || !page.Exhausted {
		t.Fatalf("inspection inferred or rewrote semantic receipt facts: %+v, %v", page, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if page, err := inspection.ReadPage(ctx, pendingBucket, nil, 2, 1024); !errors.Is(err, context.Canceled) || len(page.Entries) != 0 {
		t.Fatalf("canceled page returned data: %+v, %v", page, err)
	}
	if header, err := inspection.Header(ctx); !errors.Is(err, context.Canceled) || len(header.Metadata) != 0 {
		t.Fatalf("canceled header returned data: %+v, %v", header, err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if localInspectionFileHash(t, path) != before {
		t.Fatal("bounds/cancellation/semantic-corruption inspection mutated the file")
	}
}

func TestLocalInspectionOversizedHeaderAndValue(t *testing.T) {
	for _, header := range []bool{false, true} {
		path := localInspectionFixture(t, true)
		localInspectionMutate(t, path, func(tx *bolt.Tx) error {
			if header {
				return tx.Bucket(limitMetaBucket).Put([]byte("quota"), bytes.Repeat([]byte("x"), LocalInspectionMaxHeaderBytes+1))
			}
			return tx.Bucket(pendingBucket).Put([]byte("large"), bytes.Repeat([]byte("x"), LocalInspectionMaxValueBytes+1))
		})
		before := localInspectionFileHash(t, path)
		inspection, err := OpenLocalInspection(context.Background(), path)
		if header {
			if inspection != nil || !errors.Is(err, ErrLocalInspectionBound) {
				t.Fatalf("oversized header accepted: %v", err)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if page, err := inspection.ReadPage(context.Background(), pendingBucket, nil, 1, LocalInspectionMaxPageBytes); !errors.Is(err, ErrLocalInspectionBound) || len(page.Entries) != 0 {
				t.Fatalf("oversized value returned partial evidence: %+v, %v", page, err)
			}
			if err := inspection.Close(); err != nil {
				t.Fatal(err)
			}
		}
		if localInspectionFileHash(t, path) != before {
			t.Fatal("oversized evidence changed the file")
		}
	}
}
