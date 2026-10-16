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
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

var localTransferTestTime = time.Date(2026, time.October, 15, 0, 0, 0, 0, time.UTC)

func localTransferTestBounds() LocalTransferBounds {
	return LocalTransferBounds{Bytes: 8 << 20, Records: 1000, Accounts: 1000, References: 1000}
}

func localTransferTestReceipt() QuotaReceipt {
	tokens := int64(20)
	actual := int64(7)
	money := "2"
	start, end, _ := QuotaMonthBounds(localTransferTestTime.UnixNano(), "UTC")
	return QuotaReceipt{
		RequestID: "call_original", AdmittedAt: localTransferTestTime.UnixNano(), MonthStart: start, MonthEnd: end,
		Accounts: []quotaAccount{{Account: "usr_original", Revision: "rev_policy"}},
		Bound:    QuotaBound{Tokens: &tokens, Money: &money, Currency: "USD", Revision: "rev_bound"},
		State:    "complete", Actual: QuotaSettlement{Tokens: &actual}, SettlementDigest: strings.Repeat("a", 64),
	}
}

func localTransferTestCounter() QuotaCounter {
	return QuotaCounter{TokensUsed: 7, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{"USD": "2000000000000000000"}}
}

func localTransferTestPutJSON(bucket *bolt.Bucket, key []byte, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return bucket.Put(key, raw)
}

func localTransferTestUint(value int64) []byte {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, uint64(value))
	return raw
}

func localTransferTestRPMKey(instant int64, request, account string) []byte {
	return append(localTransferTestUint(instant), []byte(request+":"+account)...)
}

// Build original on-disk facts directly. No Queue.Open/recovery/pruning repairs
// the fixture before the inspector or validator observes its raw bytes.
func localTransferTestInspection(t *testing.T, mutate func(*bolt.Tx) error) (*LocalInspection, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "original.db")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{"pending-v1", "ready-v1", "rpm-v1", "leases-v1", "rpm-counts-v1", "active-counts-v1", "limit-meta-v1", "quota-entries-v2", "quota-counters-v2", "quota-expiry-v2", "quota-accounts-v2", "quota-invalid-bounds-v2"} {
			if _, err := tx.CreateBucket([]byte(name)); err != nil {
				return err
			}
		}
		metadata := tx.Bucket([]byte("limit-meta-v1"))
		for key, value := range map[string][]byte{"version": []byte("2"), "installation": []byte("led_original"), "clock": localTransferTestUint(localTransferTestTime.Add(10 * time.Minute).UnixNano())} {
			if err := metadata.Put([]byte(key), value); err != nil {
				return err
			}
		}
		if err := localTransferTestPutJSON(metadata, []byte("quota"), quotaMetadata{TimeZone: "UTC", CoverageStart: localTransferTestTime.UnixNano()}); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("quota-accounts-v2")).Put([]byte("usr_original"), []byte("0")); err != nil {
			return err
		}
		receipt := localTransferTestReceipt()
		if err := localTransferTestPutJSON(tx.Bucket([]byte("quota-entries-v2")), []byte(receipt.RequestID), receipt); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("quota-entries-v2")).SetSequence(1); err != nil {
			return err
		}
		// Deliberately retained expired minute refs/counters: CompleteQuota
		// advances the clock without pruning these earlier projections.
		for window, end := range quotaWindowEnds(receipt) {
			if err := localTransferTestPutJSON(tx.Bucket([]byte("quota-counters-v2")), counterKey("usr_original", window), localTransferTestCounter()); err != nil {
				return err
			}
			if err := tx.Bucket([]byte("quota-expiry-v2")).Put(expiryKey(end, 'c', "usr_original\x00"+window+"\x00call_original"), []byte{}); err != nil {
				return err
			}
		}
		latest := max(receipt.MonthEnd, receipt.AdmittedAt+int64(7*24*time.Hour))
		if err := tx.Bucket([]byte("quota-expiry-v2")).Put(expiryKey(latest, 'r', receipt.RequestID), []byte{}); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(receipt.AdmittedAt, receipt.RequestID, "usr_original"), []byte("usr_original")); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-v1")).SetSequence(1); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-counts-v1")).Put([]byte("usr_original"), localTransferTestUint(1)); err != nil {
			return err
		}
		if mutate != nil {
			return mutate(tx)
		}
		return nil
	})
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatal(errors.Join(err, closeErr))
	}
	inspection, err := OpenLocalInspection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := inspection.Close(); err != nil {
			t.Error(err)
		}
	})
	return inspection, path
}

func localTransferTestValidate(inspection *LocalInspection, bounds LocalTransferBounds) (*LocalTransferValidation, error) {
	return ValidateLocalTransferSnapshot(context.Background(), inspection, bounds, localTransferTestTime.Add(10*time.Minute).UnixNano())
}

func TestLocalTransferValidationRetainedCountersDifferFromCutover(t *testing.T) {
	inspection, path := localTransferTestInspection(t, nil)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Raw) != 12 || result.RecordsRead != 17 || result.ReferencesRead != 7 || len(result.StoredQuota) != 4 || len(result.LiveQuota) != 3 || len(result.RPM) != 1 || result.RPM[0].Stored != 1 || result.RPM[0].Live != 0 {
		t.Fatalf("incorrect complete census or expiry separation: %+v", result)
	}
	for _, row := range result.StoredQuota {
		if row.Counter.TokensUsed != 7 || row.Counter.MoneyHeld["USD"] != "2000000000000000000" || row.Counter.MoneyUnknown != 0 {
			t.Fatalf("lost original independent settlement/hold: %+v", row)
		}
	}
	for _, row := range result.LiveQuota {
		if row.Window == "minute" {
			t.Fatal("expired minute was projected as live")
		}
	}
	if len(result.AccountBirths) != 1 || result.AccountBirths[0].RecordedUnixNano != 0 || result.UnverifiedSettlementDigests != 1 || result.TransferState != LocalTransferIncomplete || len(result.Unproven) != 6 {
		t.Fatalf("invented birth/provenance/payload/transfer authority: %+v", result)
	}
	again, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || result.RawSHA256 != again.RawSHA256 || len(result.RawSHA256) != 64 {
		t.Fatal("unstable complete original-byte digest", err)
	}
	// Returned evidence and projections are detached from the inspection.
	result.Raw[0].Name[0] = 'x'
	result.StoredQuota[0].Counter.MoneyHeld["USD"] = "1"
	third, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || third.RawSHA256 != again.RawSHA256 || third.StoredQuota[0].Counter.MoneyHeld["USD"] != "2000000000000000000" {
		t.Fatal("returned values changed inspection evidence", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("read-only validation changed the source file", err)
	}
}

func TestLocalTransferValidationBoundsAndCancellationReturnNoEvidence(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, nil)
	baseline, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*LocalTransferBounds)
	}{
		{"bytes", func(b *LocalTransferBounds) { b.Bytes = baseline.BytesRead - 1 }},
		{"records", func(b *LocalTransferBounds) { b.Records = baseline.RecordsRead - 1 }},
		{"references", func(b *LocalTransferBounds) { b.References = baseline.ReferencesRead - 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bounds := localTransferTestBounds()
			test.change(&bounds)
			result, err := localTransferTestValidate(inspection, bounds)
			if result != nil || !errors.Is(err, ErrLocalTransferBound) {
				t.Fatal("bound failure exposed partial evidence", result, err)
			}
		})
	}
	exact := LocalTransferBounds{Bytes: baseline.BytesRead, Records: baseline.RecordsRead, Accounts: 1, References: baseline.ReferencesRead}
	if result, err := localTransferTestValidate(inspection, exact); err != nil || result.RawSHA256 != baseline.RawSHA256 {
		t.Fatal("inclusive exact total bounds rejected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result, err := ValidateLocalTransferSnapshot(ctx, inspection, exact, baseline.CutoverUnixNano); result != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation exposed evidence", result, err)
	}
	if result, err := ValidateLocalTransferSnapshot(context.Background(), inspection, LocalTransferBounds{}, baseline.CutoverUnixNano); result != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal("implicit unbounded defaults accepted", result, err)
	}
	if result, err := ValidateLocalTransferSnapshot(context.Background(), inspection, exact, *baseline.Header.ClockHighWater-1); result != nil || !errors.Is(err, ErrInvalid) {
		t.Fatal("cutover before recorded clock accepted", result, err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
	if result, err := localTransferTestValidate(inspection, exact); result != nil || !errors.Is(err, ErrLocalInspectionClosed) {
		t.Fatal("closed snapshot accepted", result, err)
	}
}

func TestLocalTransferValidationUnknownInterruptedHoldAndStickyMarker(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		receipt := localTransferTestReceipt()
		receipt.State = "interrupted"
		receipt.Bound = QuotaBound{}
		receipt.Actual = QuotaSettlement{}
		if err := localTransferTestPutJSON(tx.Bucket([]byte("quota-entries-v2")), []byte(receipt.RequestID), receipt); err != nil {
			return err
		}
		for window := range quotaWindowEnds(receipt) {
			counter := QuotaCounter{TokensUnknown: 1, MoneyUnknown: 1, MoneyUsed: map[string]string{}, MoneyHeld: map[string]string{}}
			if err := localTransferTestPutJSON(tx.Bucket([]byte("quota-counters-v2")), counterKey("usr_original", window), counter); err != nil {
				return err
			}
		}
		return tx.Bucket([]byte("quota-invalid-bounds-v2")).Put([]byte("rev_sticky"), []byte("call_pruned"))
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range result.LiveQuota {
		if row.Counter.TokensUnknown != 1 || row.Counter.MoneyUnknown != 1 || row.Counter.TokensUsed != 0 {
			t.Fatal("interrupted unknown dimensions were released", row)
		}
	}
	found := false
	for _, bucket := range result.Raw {
		if string(bucket.Name) == "quota-invalid-bounds-v2" {
			found = len(bucket.Entries) == 1 && string(bucket.Entries[0].Key) == "rev_sticky" && string(bucket.Entries[0].Value) == "call_pruned"
		}
	}
	if !found {
		t.Fatal("pruned receipt erased sticky invalidation")
	}
}

func TestLocalTransferValidationOriginalReceiptBytesAndUnknownBinding(t *testing.T) {
	var original []byte
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("limit-meta-v1")).Delete([]byte("installation")); err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		bucket := tx.Bucket([]byte("quota-entries-v2"))
		if err := json.Unmarshal(bucket.Get([]byte("call_original")), &fields); err != nil {
			return err
		}
		delete(fields, "recovery") // Legacy optional absence is not invented evidence.
		var err error
		original, err = json.Marshal(fields)
		if err != nil {
			return err
		}
		return bucket.Put([]byte("call_original"), original)
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || result.Header.InstallationID != "" || result.TransferState != LocalTransferIncomplete {
		t.Fatal("missing binding was manufactured or treated as transfer proof", err)
	}
	for _, bucket := range result.Raw {
		if string(bucket.Name) == "quota-entries-v2" && !bytes.Equal(bucket.Entries[0].Value, original) {
			t.Fatal("original optional-field receipt was normalized")
		}
	}
}

func TestLocalTransferValidationKnownZeroIsNotAnUnknownHold(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		zero, money := int64(0), "0"
		receipt := localTransferTestReceipt()
		receipt.Actual = QuotaSettlement{Tokens: &zero, Money: &money, Currency: "USD"}
		if err := localTransferTestPutJSON(tx.Bucket([]byte("quota-entries-v2")), []byte(receipt.RequestID), receipt); err != nil {
			return err
		}
		for window := range quotaWindowEnds(receipt) {
			if err := tx.Bucket([]byte("quota-counters-v2")).Delete(counterKey("usr_original", window)); err != nil {
				return err
			}
		}
		return nil
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || len(result.StoredQuota) != 0 || len(result.LiveQuota) != 0 || result.UnverifiedSettlementDigests != 1 {
		t.Fatal("proven independent zero became a hold or digest proof", err)
	}
}

func TestLocalTransferValidationCoverageBoundaryKeepsLegacyAmbiguity(t *testing.T) {
	for _, test := range []struct {
		name string
		now  time.Time
	}{
		{"equal_clock", localTransferTestTime},
		{"clock_rollback", localTransferTestTime.Add(-time.Hour)},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
				metadata := tx.Bucket([]byte("limit-meta-v1"))
				if err := metadata.Put([]byte("clock"), localTransferTestUint(localTransferTestTime.UnixNano())); err != nil {
					return err
				}
				// EnableQuota uses this clamped instant without incrementing it.
				instant, err := logicalLimitTime(tx, test.now)
				if err != nil {
					return err
				}
				if instant != localTransferTestTime.UnixNano() {
					return errors.New("fixture did not retain the legacy clock boundary")
				}
				for _, request := range []string{"call_boundary_legacy", "call_original"} {
					// One ID has no quota receipt, and one shares a later
					// receipt ID while its legacy account remains different.
					if err := tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(instant, request, "usr_legacy"), []byte("usr_legacy")); err != nil {
						return err
					}
				}
				if err := tx.Bucket([]byte("rpm-v1")).SetSequence(3); err != nil {
					return err
				}
				return tx.Bucket([]byte("rpm-counts-v1")).Put([]byte("usr_legacy"), localTransferTestUint(2))
			})
			result, err := localTransferTestValidate(inspection, localTransferTestBounds())
			if err != nil {
				t.Fatal(err)
			}
			if len(result.AccountBirths) != 1 || len(result.RPM) != 2 || result.RPM[0].Account != "usr_legacy" || result.RPM[0].Stored != 2 || result.RPM[0].Live != 0 || result.TransferState != LocalTransferIncomplete {
				t.Fatal("boundary ambiguity manufactured quota attribution", result)
			}
			found := 0
			for _, bucket := range result.Raw {
				if string(bucket.Name) != "rpm-v1" {
					continue
				}
				for _, entry := range bucket.Entries {
					if string(entry.Value) == "usr_legacy" {
						if !bytes.Equal(entry.Key[:8], localTransferTestUint(localTransferTestTime.UnixNano())) {
							t.Fatal("original boundary timestamp was rewritten")
						}
						found++
					}
				}
			}
			if found != 2 {
				t.Fatal("original ambiguous RPM events were discarded")
			}
		})
	}
}

func TestLocalTransferValidationStickyMarkerSurvivesRequestIDReuse(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		// The original August overrun receipt is now beyond every original
		// economic window. Its sticky marker remains after ID reuse in October.
		if err := localTransferTestPutJSON(tx.Bucket([]byte("limit-meta-v1")), []byte("quota"), quotaMetadata{TimeZone: "UTC", CoverageStart: localTransferTestTime.AddDate(0, -2, 0).UnixNano()}); err != nil {
			return err
		}
		if err := localTransferTestEditReceipt(tx, func(receipt *QuotaReceipt) { receipt.Bound.Revision = "rev_new_bound" }); err != nil {
			return err
		}
		return tx.Bucket([]byte("quota-invalid-bounds-v2")).Put([]byte("rev_old_bound"), []byte("call_original"))
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil {
		t.Fatal(err)
	}
	marker, receipt := false, false
	for _, bucket := range result.Raw {
		switch string(bucket.Name) {
		case "quota-invalid-bounds-v2":
			marker = len(bucket.Entries) == 1 && string(bucket.Entries[0].Key) == "rev_old_bound" && string(bucket.Entries[0].Value) == "call_original"
		case "quota-entries-v2":
			var original QuotaReceipt
			if err := json.Unmarshal(bucket.Entries[0].Value, &original); err != nil {
				t.Fatal(err)
			}
			receipt = original.RequestID == "call_original" && original.Bound.Revision == "rev_new_bound" && !original.Overrun
		}
	}
	if !marker || !receipt || result.TransferState != LocalTransferIncomplete {
		t.Fatal("sticky marker was rebound to a newer request incarnation")
	}
}
