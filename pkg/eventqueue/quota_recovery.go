package eventqueue

import (
	"encoding/binary"
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

// recoverQuota rebuilds projections from receipts. In-flight requests cannot be
// proven undispatched after a crash, so their conservative holds survive while
// the independent concurrency leases are released by initLimits.
func (q *Queue) recoverQuota(tx *bolt.Tx) error {
	if string(tx.Bucket(limitMetaBucket).Get([]byte("version"))) == "1" {
		for _, name := range quotaBuckets {
			if tx.Bucket(name) != nil {
				return ErrInvalid
			}
		}
		return nil
	}
	metadata, err := readQuotaMetadata(tx)
	if err != nil {
		return err
	}
	for _, name := range quotaBuckets {
		if tx.Bucket(name) == nil {
			return ErrInvalid
		}
	}
	clock := tx.Bucket(limitMetaBucket).Get([]byte("clock"))
	if len(clock) != 8 {
		return ErrInvalid
	}
	instant := int64(binary.BigEndian.Uint64(clock))
	if instant < metadata.CoverageStart {
		return ErrInvalid
	}
	for _, name := range [][]byte{quotaCounterBucket, quotaExpiryBucket} {
		if err := tx.DeleteBucket(name); err != nil {
			return err
		}
		if _, err := tx.CreateBucket(name); err != nil {
			return err
		}
	}
	if err := tx.Bucket(quotaAccountBucket).ForEach(func(key, value []byte) error {
		var created int64
		if !validKey.Match(key) || json.Unmarshal(value, &created) != nil || created < 0 || created > instant {
			return ErrInvalid
		}
		return nil
	}); err != nil {
		return err
	}
	if err := tx.Bucket(quotaInvalidBoundBucket).ForEach(func(key, value []byte) error {
		if !validKey.Match(key) || !validKey.Match(value) {
			return ErrInvalid
		}
		return nil
	}); err != nil {
		return err
	}
	count := uint64(0)
	interrupted := []QuotaReceipt{}
	if err := tx.Bucket(quotaEntryBucket).ForEach(func(key, value []byte) error {
		entry, err := readQuotaReceipt(tx, string(key))
		if err != nil {
			return err
		}
		start, end, err := quotaMonth(entry.AdmittedAt, metadata.TimeZone)
		if err != nil || entry.AdmittedAt < metadata.CoverageStart || entry.AdmittedAt > instant || entry.MonthStart != start || entry.MonthEnd != end {
			return ErrInvalid
		}
		for _, account := range entry.Accounts {
			if tx.Bucket(quotaAccountBucket).Get([]byte(account.Account)) == nil {
				return ErrInvalid
			}
		}
		wasActive := entry.State == "active"
		if wasActive {
			fallback := tx.Bucket(pendingBucket).Get(key)
			if len(fallback) == 0 || tx.Bucket(readyBucket).Get(key) != nil {
				return ErrInvalid
			}
			entry.State = "interrupted"
			entry.SettlementDigest = quotaSettlementDigest(entry.Actual, fallback)
			interrupted = append(interrupted, entry)
		}
		if !wasActive {
			if tx.Bucket(pendingBucket).Get(key) != nil {
				return ErrInvalid
			}
			if payload := tx.Bucket(readyBucket).Get(key); payload != nil && quotaSettlementDigest(entry.Actual, payload) != entry.SettlementDigest {
				return ErrInvalid
			}
		}
		count++
		if count > uint64(q.quotaCapacity) {
			return ErrInvalid
		}
		return projectTerminalQuota(tx, entry, instant)
	}); err != nil {
		return err
	}
	for _, entry := range interrupted {
		if err := storeQuotaReceipt(tx, entry); err != nil {
			return err
		}
	}
	if err := tx.Bucket(quotaEntryBucket).SetSequence(count); err != nil {
		return err
	}
	return pruneQuota(tx, instant)
}
