package eventqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func localTransferTestEditReceipt(tx *bolt.Tx, edit func(*QuotaReceipt)) error {
	bucket := tx.Bucket([]byte("quota-entries-v2"))
	var receipt QuotaReceipt
	if err := json.Unmarshal(bucket.Get([]byte("call_original")), &receipt); err != nil {
		return err
	}
	edit(&receipt)
	return localTransferTestPutJSON(bucket, []byte(receipt.RequestID), receipt)
}

func localTransferTestDeleteRPM(tx *bolt.Tx) error {
	if err := tx.Bucket([]byte("rpm-v1")).Delete(localTransferTestRPMKey(localTransferTestTime.UnixNano(), "call_original", "usr_original")); err != nil {
		return err
	}
	if err := tx.Bucket([]byte("rpm-v1")).SetSequence(0); err != nil {
		return err
	}
	return tx.Bucket([]byte("rpm-counts-v1")).Delete([]byte("usr_original"))
}

func TestLocalTransferValidationCorruptionReturnsNoPartialEvidence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bolt.Tx) error
	}{
		{"noncanonical_quota_metadata", func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("limit-meta-v1"))
			return bucket.Put([]byte("quota"), append([]byte(" "), bucket.Get([]byte("quota"))...))
		}},
		{"noncanonical_birth", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-accounts-v2")).Put([]byte("usr_original"), []byte("00"))
		}},
		{"negative_birth", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-accounts-v2")).Put([]byte("usr_original"), []byte("-1"))
		}},
		{"missing_birth", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-accounts-v2")).Delete([]byte("usr_original"))
		}},
		{"future_birth", func(tx *bolt.Tx) error {
			return localTransferTestPutJSON(tx.Bucket([]byte("quota-accounts-v2")), []byte("usr_original"), localTransferTestTime.Add(time.Hour).UnixNano())
		}},
		{"birth_after_original_admission", func(tx *bolt.Tx) error {
			return localTransferTestPutJSON(tx.Bucket([]byte("quota-accounts-v2")), []byte("usr_original"), localTransferTestTime.Add(time.Second).UnixNano())
		}},
		{"original_month_changed", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { r.MonthStart++ })
		}},
		{"receipt_sequence", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-entries-v2")).SetSequence(2)
		}},
		{"duplicate_receipt_field", func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("quota-entries-v2"))
			raw := strings.Replace(string(bucket.Get([]byte("call_original"))), `"state":"complete"`, `"state":"complete","STATE":"complete"`, 1)
			return bucket.Put([]byte("call_original"), []byte(raw))
		}},
		{"unknown_receipt_field", func(tx *bolt.Tx) error {
			bucket := tx.Bucket([]byte("quota-entries-v2"))
			raw := bucket.Get([]byte("call_original"))
			return bucket.Put([]byte("call_original"), append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unexpected":true}`)...))
		}},
		{"terminal_recovery", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { value := int64(0); r.Recovery.Tokens = &value })
		}},
		{"terminal_digest", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { r.SettlementDigest = "missing" })
		}},
		{"duplicate_receipt_account", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { r.Accounts = append(r.Accounts, r.Accounts[0]) })
		}},
		{"stored_counter_mismatch", func(tx *bolt.Tx) error {
			counter := localTransferTestCounter()
			counter.TokensUsed = 8
			return localTransferTestPutJSON(tx.Bucket([]byte("quota-counters-v2")), counterKey("usr_original", "minute"), counter)
		}},
		{"noncanonical_counter_units", func(tx *bolt.Tx) error {
			counter := localTransferTestCounter()
			counter.MoneyHeld["USD"] = "02000000000000000000"
			return localTransferTestPutJSON(tx.Bucket([]byte("quota-counters-v2")), counterKey("usr_original", "minute"), counter)
		}},
		{"removed_ref_with_retained_counter", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-expiry-v2")).Delete(expiryKey(localTransferTestTime.Add(time.Minute).UnixNano(), 'c', "usr_original\x00minute\x00call_original"))
		}},
		{"missing_unexpired_ref_and_counter", func(tx *bolt.Tx) error {
			if err := tx.Bucket([]byte("quota-expiry-v2")).Delete(expiryKey(localTransferTestTime.Add(5*time.Hour).UnixNano(), 'c', "usr_original\x00five_hours\x00call_original")); err != nil {
				return err
			}
			return tx.Bucket([]byte("quota-counters-v2")).Delete(counterKey("usr_original", "five_hours"))
		}},
		{"missing_retirement_ref", func(tx *bolt.Tx) error {
			r := localTransferTestReceipt()
			return tx.Bucket([]byte("quota-expiry-v2")).Delete(expiryKey(r.MonthEnd, 'r', r.RequestID))
		}},
		{"wrong_expiry_instant", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-expiry-v2")).Put(expiryKey(localTransferTestTime.Add(5*time.Hour).UnixNano()+1, 'c', "usr_original\x00five_hours\x00call_original"), []byte{})
		}},
		{"orphan_expiry_ref", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-expiry-v2")).Put(expiryKey(localTransferTestTime.Add(5*time.Hour).UnixNano(), 'c', "usr_original\x00five_hours\x00call_missing"), []byte{})
		}},
		{"expiry_ref_value", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-expiry-v2")).Put(expiryKey(localTransferTestTime.Add(time.Minute).UnixNano(), 'c', "usr_original\x00minute\x00call_original"), []byte("unexpected"))
		}},
		{"counter_without_original_ref", func(tx *bolt.Tx) error {
			return localTransferTestPutJSON(tx.Bucket([]byte("quota-counters-v2")), counterKey("usr_original", "active"), localTransferTestCounter())
		}},
		{"rpm_sequence", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("rpm-v1")).SetSequence(2)
		}},
		{"rpm_count", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("rpm-counts-v1")).Put([]byte("usr_original"), localTransferTestUint(2))
		}},
		{"rpm_account_not_suffix", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(localTransferTestTime.UnixNano(), "call_original", "usr_original"), []byte("usr_other"))
		}},
		{"missing_unexpired_rpm_and_count", func(tx *bolt.Tx) error {
			if err := tx.Bucket([]byte("limit-meta-v1")).Put([]byte("clock"), localTransferTestUint(localTransferTestTime.Add(30*time.Second).UnixNano())); err != nil {
				return err
			}
			return localTransferTestDeleteRPM(tx)
		}},
		{"orphan_active_count", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("active-counts-v1")).Put([]byte("usr_original"), localTransferTestUint(1))
		}},
		{"invalid_marker_revision", func(tx *bolt.Tx) error {
			return tx.Bucket([]byte("quota-invalid-bounds-v2")).Put([]byte("invalid revision"), []byte("call_original"))
		}},
		{"overrun_not_recorded", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { value := int64(5); r.Bound.Tokens = &value })
		}},
		{"overrun_without_sticky_marker", func(tx *bolt.Tx) error {
			return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { value := int64(5); r.Bound.Tokens = &value; r.Overrun = true })
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inspection, _ := localTransferTestInspection(t, test.mutate)
			result, err := localTransferTestValidate(inspection, localTransferTestBounds())
			if result != nil || !errors.Is(err, ErrInvalid) {
				t.Fatal("corruption accepted or partial evidence exposed", result, err)
			}
		})
	}
}

func TestLocalTransferValidationDrainedRequirement(t *testing.T) {
	for _, name := range []string{"pending-v1", "ready-v1", "leases-v1", "active_receipt"} {
		t.Run(name, func(t *testing.T) {
			inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
				if name == "active_receipt" {
					return localTransferTestEditReceipt(tx, func(r *QuotaReceipt) { r.State = "active"; r.Actual = QuotaSettlement{}; r.SettlementDigest = "" })
				}
				return tx.Bucket([]byte(name)).Put([]byte("call_active"), []byte("undrained"))
			})
			result, err := localTransferTestValidate(inspection, localTransferTestBounds())
			if result != nil || !errors.Is(err, ErrLocalTransferNotDrained) {
				t.Fatal("undrained snapshot accepted", result, err)
			}
		})
	}
}

func TestLocalTransferValidationPrunedExpiredRefsAndRPMRemainValid(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("quota-expiry-v2")).Delete(expiryKey(localTransferTestTime.Add(time.Minute).UnixNano(), 'c', "usr_original\x00minute\x00call_original")); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("quota-counters-v2")).Delete(counterKey("usr_original", "minute")); err != nil {
			return err
		}
		return localTransferTestDeleteRPM(tx)
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || len(result.StoredQuota) != 3 || len(result.LiveQuota) != 3 || len(result.RPM) != 0 {
		t.Fatal("valid already-pruned expired projections rejected", err)
	}
}

func TestLocalTransferValidationCompleteMultipageCensusAndAccountBound(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		for index := range 600 {
			if err := tx.Bucket([]byte("quota-accounts-v2")).Put([]byte(fmt.Sprintf("usr_retained_%04d", index)), []byte("0")); err != nil {
				return err
			}
		}
		return nil
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || len(result.AccountBirths) != 601 || result.RecordsRead != 617 {
		t.Fatal("multipage census lost original accounts", err)
	}
	bounds := localTransferTestBounds()
	bounds.Accounts = 600
	if partial, err := localTransferTestValidate(inspection, bounds); partial != nil || !errors.Is(err, ErrLocalTransferBound) {
		t.Fatal("account bound exposed partial census", partial, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if digest, err := localTransferRawDigest(ctx, result.Raw); digest != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled digest reported complete evidence", digest, err)
	}
}

func TestLocalTransferValidationLegacyRPMDoesNotInventQuotaBirth(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		// A pre-v2 call ID can be reused after Ack; its older retained RPM
		// event must not be attributed to the newer quota receipt.
		if err := tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(localTransferTestTime.Add(-time.Second).UnixNano(), "call_original", "usr_original"), []byte("usr_original")); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(localTransferTestTime.Add(-2*time.Second).UnixNano(), "call_legacy", "usr_legacy"), []byte("usr_legacy")); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-v1")).SetSequence(3); err != nil {
			return err
		}
		if err := tx.Bucket([]byte("rpm-counts-v1")).Put([]byte("usr_original"), localTransferTestUint(2)); err != nil {
			return err
		}
		return tx.Bucket([]byte("rpm-counts-v1")).Put([]byte("usr_legacy"), localTransferTestUint(1))
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || len(result.RPM) != 2 || result.RPM[0].Account != "usr_legacy" || result.RPM[0].Stored != 1 || len(result.AccountBirths) != 1 {
		t.Fatal("legacy RPM invented receipt or account provenance", err)
	}
}

func TestLocalTransferValidationOverrunKeepsOriginalInvalidation(t *testing.T) {
	inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
		if err := localTransferTestEditReceipt(tx, func(r *QuotaReceipt) {
			value := int64(5)
			r.Bound.Tokens, r.Overrun = &value, true
		}); err != nil {
			return err
		}
		return tx.Bucket([]byte("quota-invalid-bounds-v2")).Put([]byte("rev_bound"), []byte("call_original"))
	})
	result, err := localTransferTestValidate(inspection, localTransferTestBounds())
	if err != nil || len(result.LiveQuota) != 3 || result.LiveQuota[0].Counter.TokensUsed != 7 {
		t.Fatal("accepted historical overrun lost usage or invalidation", err)
	}
}

func TestLocalTransferValidationMissingReceiptRespectsOriginalEconomicRetirement(t *testing.T) {
	admitted := localTransferTestTime.Add(time.Second)
	monthEnd := time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)
	lateAdmission := localTransferTestTime.AddDate(0, 0, 15).Add(time.Second)
	sevenDayEnd := lateAdmission.Add(7 * 24 * time.Hour)
	shanghaiEnd := monthEnd.Add(-8 * time.Hour)
	for _, test := range []struct {
		name     string
		zone     string
		admitted time.Time
		clock    time.Time
		valid    bool
	}{
		{"expired_rpm_still_requires_receipt", "UTC", admitted, localTransferTestTime.Add(10 * time.Minute), false},
		{"before_original_month_end", "UTC", admitted, monthEnd.Add(-time.Nanosecond), false},
		{"at_original_month_end", "UTC", admitted, monthEnd, true},
		{"month_end_before_seven_day_retirement", "UTC", lateAdmission, monthEnd, false},
		{"before_seven_day_retirement", "UTC", lateAdmission, sevenDayEnd.Add(-time.Nanosecond), false},
		{"at_seven_day_retirement", "UTC", lateAdmission, sevenDayEnd, true},
		{"before_original_zone_month_end", "Asia/Shanghai", admitted, shanghaiEnd.Add(-time.Nanosecond), false},
		{"at_original_zone_month_end", "Asia/Shanghai", admitted, shanghaiEnd, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			inspection, _ := localTransferTestInspection(t, func(tx *bolt.Tx) error {
				// Remove the original receipt, all c/r refs and counters together;
				// coherent RPM counts alone must not hide required history.
				for _, name := range []string{"quota-entries-v2", "quota-expiry-v2", "quota-counters-v2", "rpm-v1"} {
					if err := tx.DeleteBucket([]byte(name)); err != nil {
						return err
					}
					if _, err := tx.CreateBucket([]byte(name)); err != nil {
						return err
					}
				}
				metadata := tx.Bucket([]byte("limit-meta-v1"))
				if err := metadata.Put([]byte("clock"), localTransferTestUint(test.clock.UnixNano())); err != nil {
					return err
				}
				if err := localTransferTestPutJSON(metadata, []byte("quota"), quotaMetadata{TimeZone: test.zone, CoverageStart: localTransferTestTime.UnixNano()}); err != nil {
					return err
				}
				if err := tx.Bucket([]byte("rpm-v1")).Put(localTransferTestRPMKey(test.admitted.UnixNano(), "call_original", "usr_original"), []byte("usr_original")); err != nil {
					return err
				}
				return tx.Bucket([]byte("rpm-v1")).SetSequence(1)
			})
			result, err := ValidateLocalTransferSnapshot(context.Background(), inspection, localTransferTestBounds(), test.clock.UnixNano())
			if !test.valid {
				if result != nil || !errors.Is(err, ErrInvalid) {
					t.Fatal("required economic history was concealed by expired RPM", result, err)
				}
				return
			}
			if err != nil || len(result.RPM) != 1 || result.RPM[0].Stored != 1 || result.RPM[0].Live != 0 || result.UnverifiedSettlementDigests != 0 || result.TransferState != LocalTransferIncomplete {
				t.Fatal("fully retired original history was treated as present or invalid", result, err)
			}
		})
	}
}
