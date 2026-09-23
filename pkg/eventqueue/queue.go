// Package eventqueue provides a bounded, durable local journal. It stores opaque
// event payloads; relational business data remains in the primary GORM database.
package eventqueue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	ErrFull       = errors.New("event queue is full")
	ErrInvalid    = errors.New("invalid event queue entry")
	ErrMissing    = errors.New("event queue reservation is missing")
	pendingBucket = []byte("pending-v1")
	readyBucket   = []byte("ready-v1")
	validKey      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type Queue struct {
	db                   *bolt.DB
	capacity, maxPayload int
	quotaCapacity        int
}
type Entry struct {
	ID      string
	Payload []byte
}

// Open takes an exclusive process lock. Any admissions left pending by a former
// process become replayable fallback facts; callers supply that fallback when
// reserving a slot. Default synchronous commits must never be disabled.
func Open(path string, capacity, maxPayload int) (*Queue, error) {
	if path == "" || capacity < 1 || maxPayload < 1 {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	q := &Queue{db: db, capacity: capacity, maxPayload: maxPayload, quotaCapacity: quotaHistoryCapacity}
	err = db.Update(func(tx *bolt.Tx) error {
		if err := initLimits(tx); err != nil {
			return err
		}
		pending, err := tx.CreateBucketIfNotExists(pendingBucket)
		if err != nil {
			return err
		}
		ready, err := tx.CreateBucketIfNotExists(readyBucket)
		if err != nil {
			return err
		}
		if err := q.recoverQuota(tx); err != nil {
			return err
		}
		keys := [][]byte{}
		if err := pending.ForEach(func(k, v []byte) error {
			if !validKey.Match(k) || len(v) > maxPayload {
				return ErrInvalid
			}
			if ready.Get(k) == nil {
				if err := ready.Put(k, v); err != nil {
					return err
				}
			}
			keys = append(keys, bytes.Clone(k))
			return nil
		}); err != nil {
			return err
		}
		for _, key := range keys {
			if err := pending.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return q, nil
}
func (q *Queue) Close() error { return q.db.Close() }
func (q *Queue) valid(id string, payload []byte) bool {
	return validKey.MatchString(id) && len(payload) > 0 && len(payload) <= q.maxPayload
}

// Reserve fsyncs a fallback fact before a caller dispatches an upstream request.
// A full or unwritable queue rejects admission rather than accepting lost facts.
func (q *Queue) Reserve(id string, fallback []byte) error {
	if !q.valid(id, fallback) {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		pending, ready := tx.Bucket(pendingBucket), tx.Bucket(readyBucket)
		key := []byte(id)
		if bucket := tx.Bucket(quotaEntryBucket); bucket != nil && bucket.Get(key) != nil {
			return ErrQuotaConflict
		}
		if pending.Get(key) != nil || ready.Get(key) != nil {
			return ErrInvalid
		}
		if pending.Stats().KeyN+ready.Stats().KeyN >= q.capacity {
			return ErrFull
		}
		return pending.Put(key, fallback)
	})
}

// Complete atomically replaces a reservation with the final fact. Repeated
// completion cannot overwrite a fact already accepted for delivery.
func (q *Queue) Complete(id string, payload []byte) error {
	if !q.valid(id, payload) {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error {
		if bucket := tx.Bucket(quotaEntryBucket); bucket != nil && bucket.Get([]byte(id)) != nil {
			return ErrQuotaRequired
		}
		return q.complete(tx, id, payload)
	})
}
func (q *Queue) complete(tx *bolt.Tx, id string, payload []byte) error {
	pending, ready := tx.Bucket(pendingBucket), tx.Bucket(readyBucket)
	key := []byte(id)
	if ready.Get(key) != nil {
		return nil
	}
	if pending.Get(key) == nil {
		return ErrMissing
	}
	if err := ready.Put(key, payload); err != nil {
		return err
	}
	if err := releaseLimits(tx, key); err != nil {
		return err
	}
	return pending.Delete(key)
}

// Read returns independent copies without holding a transaction during delivery.
func (q *Queue) Read(limit int) ([]Entry, error) {
	if limit < 1 || limit > q.capacity {
		return nil, ErrInvalid
	}
	entries := []Entry{}
	err := q.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(readyBucket).Cursor()
		for key, value := cursor.First(); key != nil && len(entries) < limit; key, value = cursor.Next() {
			entries = append(entries, Entry{ID: string(key), Payload: bytes.Clone(value)})
		}
		return nil
	})
	return entries, err
}

// Ack follows successful idempotent primary-database delivery. A crash between
// delivery and acknowledgement causes replay, never silent loss.
func (q *Queue) Ack(id string) error {
	if !validKey.MatchString(id) {
		return ErrInvalid
	}
	return q.db.Update(func(tx *bolt.Tx) error { return tx.Bucket(readyBucket).Delete([]byte(id)) })
}

func (q *Queue) Depth() (int, error) {
	count := 0
	err := q.db.View(func(tx *bolt.Tx) error {
		count = tx.Bucket(pendingBucket).Stats().KeyN + tx.Bucket(readyBucket).Stats().KeyN
		return nil
	})
	return count, err
}
