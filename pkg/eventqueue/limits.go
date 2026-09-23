package eventqueue

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"time"

	bolt "go.etcd.io/bbolt"
)

var ErrRateLimit = errors.New("request rate limit exceeded")
var ErrConcurrency = errors.New("concurrency limit exceeded")
var rpmBucket = []byte("rpm-v1")
var leaseBucket = []byte("leases-v1")
var rpmCountBucket = []byte("rpm-counts-v1")
var activeCountBucket = []byte("active-counts-v1")
var limitMetaBucket = []byte("limit-meta-v1")

const limitHistoryCapacity = 200000

type Limit struct {
	Account          string
	RPM, Concurrency *int64
}

// BindInstallation rejects missing/foreign established ledgers. Initialization
// is the only permitted empty binding; SQL marks it established before listening.
func (q *Queue) BindInstallation(identity string, established bool) error {
	if !validKey.MatchString(identity) {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(limitMetaBucket)
		old := bucket.Get([]byte("installation"))
		if len(old) == 0 {
			if established {
				return ErrInvalid
			}
			return bucket.Put([]byte("installation"), []byte(identity))
		}
		if string(old) != identity {
			return ErrInvalid
		}
		return nil
	})
}
func initLimits(tx *bolt.Tx) error {
	if metadata := tx.Bucket(limitMetaBucket); metadata != nil {
		if version := metadata.Get([]byte("version")); string(version) != "1" && string(version) != "2" {
			return ErrInvalid
		}
		if clock := metadata.Get([]byte("clock")); clock != nil && len(clock) != 8 {
			return ErrInvalid
		}
		for _, name := range [][]byte{rpmBucket, leaseBucket, pendingBucket, readyBucket} {
			if tx.Bucket(name) == nil {
				return ErrInvalid
			}
		}
	}

	for _, name := range [][]byte{rpmBucket, leaseBucket, limitMetaBucket, rpmCountBucket, activeCountBucket} {
		if _, err := tx.CreateBucketIfNotExists(name); err != nil {
			return err
		}
	}
	if tx.Bucket(limitMetaBucket).Get([]byte("version")) == nil {
		if err := tx.Bucket(limitMetaBucket).Put([]byte("version"), []byte("1")); err != nil {
			return err
		}
	}
	// Rebuild indexes from durable events once at startup, validating each entry.
	for _, name := range [][]byte{leaseBucket, rpmCountBucket, activeCountBucket} {
		if err := tx.DeleteBucket(name); err != nil {
			return err
		}
		if _, err := tx.CreateBucket(name); err != nil {
			return err
		}
	}
	total := uint64(0)
	if err := tx.Bucket(rpmBucket).ForEach(func(key, value []byte) error {
		if len(key) < 9 || !validKey.Match(value) {
			return ErrInvalid
		}
		total++
		return changeCount(tx.Bucket(rpmCountBucket), value, 1)
	}); err != nil {
		return err
	}
	if total > limitHistoryCapacity {
		return ErrInvalid
	}
	return tx.Bucket(rpmBucket).SetSequence(total)
}
func countValue(bucket *bolt.Bucket, key []byte) (int64, error) {
	raw := bucket.Get(key)
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, ErrInvalid
	}
	value := int64(binary.BigEndian.Uint64(raw))
	if value < 0 {
		return 0, ErrInvalid
	}
	return value, nil
}
func changeCount(bucket *bolt.Bucket, key []byte, delta int64) error {
	value, err := countValue(bucket, key)
	if err != nil {
		return err
	}
	value += delta
	if value < 0 {
		return ErrInvalid
	}
	if value == 0 {
		return bucket.Delete(key)
	}
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, uint64(value))
	return bucket.Put(key, raw)
}
func pruneLimits(tx *bolt.Tx, instant int64) error {
	events := tx.Bucket(rpmBucket)
	cursor := events.Cursor()
	cutoff := instant - int64(time.Minute)
	removed := uint64(0)
	for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
		if len(key) < 9 || !validKey.Match(value) {
			return ErrInvalid
		}
		if int64(binary.BigEndian.Uint64(key[:8])) > cutoff {
			break
		}
		if err := changeCount(tx.Bucket(rpmCountBucket), value, -1); err != nil {
			return err
		}
		if err := cursor.Delete(); err != nil {
			return err
		}
		removed++
	}
	if removed > events.Sequence() {
		return ErrInvalid
	}
	return events.SetSequence(events.Sequence() - removed)
}
func releaseLimits(tx *bolt.Tx, id []byte) error {
	leases := tx.Bucket(leaseBucket)
	raw := leases.Get(id)
	if raw == nil {
		return nil
	}
	var accounts []string
	if json.Unmarshal(raw, &accounts) != nil {
		return ErrInvalid
	}
	for _, account := range accounts {
		if err := changeCount(tx.Bucket(activeCountBucket), []byte(account), -1); err != nil {
			return err
		}
	}
	return leases.Delete(id)
}
func logicalLimitTime(tx *bolt.Tx, now time.Time) (int64, error) {
	value := now.UnixNano()
	if value <= 0 {
		return 0, ErrInvalid
	}
	bucket := tx.Bucket(limitMetaBucket)
	previous := bucket.Get([]byte("clock"))
	if len(previous) != 0 {
		if len(previous) != 8 {
			return 0, ErrInvalid
		}
		old := int64(binary.BigEndian.Uint64(previous))
		if value < old {
			value = old
		}
	}
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, uint64(value))
	return value, bucket.Put([]byte("clock"), raw)
}

// ReserveWithLimits checks every account and persists RPM/leases with the call
// fallback in one transaction. Every successful admission consumes RPM even if
// a later dial, upstream response or stream fails.
func (q *Queue) ReserveWithLimits(id string, payload []byte, limits []Limit, now time.Time) error {
	if !q.valid(id, payload) || len(limits) > 8 {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		if string(tx.Bucket(limitMetaBucket).Get([]byte("version"))) == "2" {
			return ErrQuotaRequired
		}
		return q.reserveWithLimits(tx, id, payload, limits, now)
	})
}
func (q *Queue) reserveWithLimits(tx *bolt.Tx, id string, payload []byte, limits []Limit, now time.Time) error {
	pending, ready := tx.Bucket(pendingBucket), tx.Bucket(readyBucket)
	if pending.Get([]byte(id)) != nil || ready.Get([]byte(id)) != nil {
		return ErrInvalid
	}
	if pending.Stats().KeyN+ready.Stats().KeyN >= q.capacity {
		return ErrFull
	}
	instant, err := logicalLimitTime(tx, now)
	if err != nil {
		return err
	}
	if err := pruneLimits(tx, instant); err != nil {
		return err
	}
	events := tx.Bucket(rpmBucket)
	if events.Sequence()+uint64(len(limits)) > limitHistoryCapacity {
		return ErrFull
	}
	leases := tx.Bucket(leaseBucket)
	seen := map[string]bool{}
	accounts := []string{}
	for _, limit := range limits {
		if !validKey.MatchString(limit.Account) || seen[limit.Account] {
			return ErrInvalid
		}
		seen[limit.Account] = true
		rpm, err := countValue(tx.Bucket(rpmCountBucket), []byte(limit.Account))
		if err != nil {
			return err
		}
		active, err := countValue(tx.Bucket(activeCountBucket), []byte(limit.Account))
		if err != nil {
			return err
		}
		if limit.RPM != nil && (*limit.RPM < 0 || rpm >= *limit.RPM) {
			return ErrRateLimit
		}
		if limit.Concurrency != nil && (*limit.Concurrency < 0 || active >= *limit.Concurrency) {
			return ErrConcurrency
		}
		accounts = append(accounts, limit.Account)
	}
	for _, account := range accounts {
		if err := changeCount(tx.Bucket(rpmCountBucket), []byte(account), 1); err != nil {
			return err
		}
		if err := changeCount(tx.Bucket(activeCountBucket), []byte(account), 1); err != nil {
			return err
		}
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(instant))
		key = append(key, []byte(id+":"+account)...)
		if err := events.Put(key, []byte(account)); err != nil {
			return err
		}
	}
	if err := events.SetSequence(events.Sequence() + uint64(len(accounts))); err != nil {
		return err
	}
	encoded, err := json.Marshal(accounts)
	if err != nil {
		return err
	}
	if err := leases.Put([]byte(id), encoded); err != nil {
		return err
	}
	return pending.Put([]byte(id), payload)
}

// AccountUsage reads local enforcement state, independent of SQL report lag.
func (q *Queue) AccountUsage(account string, now time.Time) (rpm, active int64, err error) {
	err = q.db.Update(func(tx *bolt.Tx) error {
		instant, err := logicalLimitTime(tx, now)
		if err != nil {
			return err
		}
		if err := pruneLimits(tx, instant); err != nil {
			return err
		}
		rpm, err = countValue(tx.Bucket(rpmCountBucket), []byte(account))
		if err != nil {
			return err
		}
		active, err = countValue(tx.Bucket(activeCountBucket), []byte(account))
		return err
	})
	return
}
