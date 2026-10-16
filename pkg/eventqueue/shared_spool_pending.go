package eventqueue

import (
	"bytes"
	"context"
	"encoding/json"

	bolt "go.etcd.io/bbolt"
)

const (
	SharedSpoolPendingMaxLimit     = 64
	SharedSpoolPendingMaxScanned   = 64
	SharedSpoolPendingMaxReadBytes = 8 << 20
)

// A cursor resumes strictly after a request key in this exact spool binding.
// It identifies enumeration progress, never authority to adopt its executions.
type SharedSpoolPendingCursor struct {
	Identity       SharedSpoolIdentity
	AfterRequestID string
}

type SharedSpoolPendingPage struct {
	Keys []SharedSpoolKey
	Next *SharedSpoolPendingCursor
}

// ReadPending enumerates original keys whose records are not delivery-ready.
// It changes no record and implies no execution, closure or economic authority.
// Each page examines at most 64 records and validates at most 8 MiB of raw
// record/index bytes. Ready records count toward both limits. An empty page
// with Next is valid progress; nil Next means this read reached the current end.
// Pages do not share a snapshot. Repeat a sweep to discover earlier insertions.
func (s *SharedSpool) ReadPending(ctx context.Context, after *SharedSpoolPendingCursor, limit int) (SharedSpoolPendingPage, error) {
	if ctx == nil {
		return SharedSpoolPendingPage{}, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return SharedSpoolPendingPage{}, err
	}
	if s == nil || s.db == nil || limit < 1 || limit > SharedSpoolPendingMaxLimit ||
		after != nil && (after.Identity != s.identity || !validKey.MatchString(after.AfterRequestID)) {
		return SharedSpoolPendingPage{}, ErrInvalid
	}
	page := SharedSpoolPendingPage{Keys: []SharedSpoolKey{}}
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		records, ready, delivered := tx.Bucket(sharedSpoolRecordBucket), tx.Bucket(sharedSpoolReadyBucket), tx.Bucket(sharedSpoolAckBucket)
		if records == nil || ready == nil || delivered == nil {
			return ErrInvalid
		}
		cursor := records.Cursor()
		key, raw := cursor.First()
		if after != nil {
			key, raw = cursor.Seek([]byte(after.AfterRequestID))
			if bytes.Equal(key, []byte(after.AfterRequestID)) {
				key, raw = cursor.Next()
			}
		}
		scanned, consumed := 0, 0
		last := ""
		for key != nil && scanned < SharedSpoolPendingMaxScanned && len(page.Keys) < limit {
			if err := ctx.Err(); err != nil {
				return err
			}
			index := ready.Get(key)
			if !validKey.Match(key) || len(raw) == 0 || len(raw) > SharedSpoolMaxRecordBytes ||
				len(index) > SharedSpoolMaxDeliveredBytes || ready.Bucket(key) != nil ||
				delivered.Get(key) != nil || delivered.Bucket(key) != nil {
				return ErrInvalid
			}
			size := len(key) + len(raw) + len(index)
			if size > SharedSpoolPendingMaxReadBytes-consumed {
				break
			}
			record, err := s.decodeRecord(raw)
			if err != nil || record.Key.RequestID != string(key) {
				return ErrInvalid
			}
			if record.Ready == nil {
				if index != nil {
					return ErrInvalid
				}
				page.Keys = append(page.Keys, record.Key)
			} else {
				var receipt SharedSpoolTerminalReceipt
				if len(index) == 0 || json.Unmarshal(index, &receipt) != nil || receipt != *record.Ready {
					return ErrInvalid
				}
			}
			scanned++
			consumed += size
			last = string(key)
			key, raw = cursor.Next()
		}
		if key != nil {
			// The single-record cap always fits the page budget. Never issue a
			// cursor that skips a deferred, unchecked or malformed record.
			if last == "" {
				return ErrInvalid
			}
			page.Next = &SharedSpoolPendingCursor{Identity: s.identity, AfterRequestID: last}
		}
		return ctx.Err()
	})
	if err != nil {
		return SharedSpoolPendingPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return SharedSpoolPendingPage{}, err
	}
	return page, nil
}
