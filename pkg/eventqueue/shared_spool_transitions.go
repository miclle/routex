package eventqueue

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/miclle/routex/pkg/routeattempt"
	bolt "go.etcd.io/bbolt"
)

func (s *SharedSpool) Reserve(ctx context.Context, key SharedSpoolKey, body SharedSpoolBody) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.validKey(key) || body.Digest != key.AdmissionDigest {
		return ErrInvalid
	}
	captured, err := s.capturedBody(body)
	if err != nil {
		return err
	}
	return s.update(ctx, func(tx *bolt.Tx) error {
		if raw := tx.Bucket(sharedSpoolAckBucket).Get([]byte(key.RequestID)); raw != nil {
			var delivered SharedSpoolTerminalReceipt
			if len(raw) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(raw, &delivered) != nil || !s.validReceipt(delivered) {
				return ErrInvalid
			}
			if delivered.Key != key {
				return ErrSharedSpoolConflict
			}
			return ErrSharedSpoolDelivered
		}
		bucket := tx.Bucket(sharedSpoolRecordBucket)
		if raw := bucket.Get([]byte(key.RequestID)); raw != nil {
			stored, err := s.decodeRecord(raw)
			if err != nil {
				return err
			}
			if stored.Key != key || !sameSharedBody(stored.Intent, captured) {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		if bucket.Stats().KeyN >= s.options.Capacity {
			return ErrFull
		}
		return s.putRecord(tx, &SharedSpoolRecord{Key: key, Intent: captured})
	})
}

func (s *SharedSpool) ReadRecord(ctx context.Context, key SharedSpoolKey) (*SharedSpoolRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.validKey(key) {
		return nil, ErrInvalid
	}
	var record *SharedSpoolRecord
	err := s.db.View(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		record, err = s.readRecord(tx, key)
		return err
	})
	return record, err
}

func (s *SharedSpool) ConfirmAcceptance(ctx context.Context, key SharedSpoolKey, receiptID string) error {
	if !validKey.MatchString(receiptID) {
		return ErrInvalid
	}
	return s.change(ctx, key, func(record *SharedSpoolRecord) error {
		if record.ReceiptID != "" && record.ReceiptID != receiptID {
			return ErrSharedSpoolConflict
		}
		record.ReceiptID = receiptID
		return nil
	})
}

func (s *SharedSpool) PrepareAttempt(ctx context.Context, key SharedSpoolKey, attempt SharedSpoolAttempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if attempt.Number < 1 || attempt.Number > routeattempt.MaxAttempts {
		return ErrInvalid
	}
	body, err := s.capturedBody(attempt.Body)
	if err != nil {
		return err
	}
	return s.change(ctx, key, func(record *SharedSpoolRecord) error {
		if attempt.Number <= len(record.Attempts) {
			if !sameSharedBody(record.Attempts[attempt.Number-1].Body, body) {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		if record.Terminal != nil || record.ReceiptID == "" || attempt.Number != len(record.Attempts)+1 ||
			attempt.Number > 1 && (len(record.Checkpoints) != attempt.Number-1 || len(record.Dispatches) != attempt.Number-1) {
			return ErrSharedSpoolConflict
		}
		record.Attempts = append(record.Attempts, SharedSpoolAttempt{Number: attempt.Number, Body: body})
		return nil
	})
}

func (s *SharedSpool) ConfirmDispatch(ctx context.Context, receipt SharedSpoolDispatch) error {
	if receipt.Number < 1 || receipt.Number > routeattempt.MaxAttempts || !validSharedDigest(receipt.AttemptDigest) {
		return ErrInvalid
	}
	return s.change(ctx, receipt.Key, func(record *SharedSpoolRecord) error {
		if receipt.Number > len(record.Attempts) || record.Attempts[receipt.Number-1].Body.Digest != receipt.AttemptDigest {
			return ErrSharedSpoolConflict
		}
		if receipt.Number <= len(record.Dispatches) {
			if record.Dispatches[receipt.Number-1] != receipt {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		if record.Terminal != nil || receipt.Number != len(record.Dispatches)+1 {
			return ErrSharedSpoolConflict
		}
		record.Dispatches = append(record.Dispatches, receipt)
		return nil
	})
}

func (s *SharedSpool) RecordAttempt(ctx context.Context, key SharedSpoolKey, checkpoint SharedSpoolCheckpoint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if checkpoint.Number < 1 || checkpoint.Number > routeattempt.MaxAttempts || !validSharedDigest(checkpoint.AttemptDigest) {
		return ErrInvalid
	}
	body, err := s.capturedBody(checkpoint.Body)
	if err != nil {
		return err
	}
	return s.change(ctx, key, func(record *SharedSpoolRecord) error {
		if checkpoint.Number > len(record.Attempts) || record.Attempts[checkpoint.Number-1].Body.Digest != checkpoint.AttemptDigest {
			return ErrSharedSpoolConflict
		}
		if checkpoint.Number <= len(record.Checkpoints) {
			stored := record.Checkpoints[checkpoint.Number-1]
			if stored.AttemptDigest != checkpoint.AttemptDigest || !sameSharedBody(stored.Body, body) {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		if record.Terminal != nil || checkpoint.Number != len(record.Checkpoints)+1 {
			return ErrSharedSpoolConflict
		}
		record.Checkpoints = append(record.Checkpoints, SharedSpoolCheckpoint{Number: checkpoint.Number, AttemptDigest: checkpoint.AttemptDigest, Body: body})
		return nil
	})
}

func (s *SharedSpool) PrepareTerminal(ctx context.Context, key SharedSpoolKey, terminal SharedSpoolTerminal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if terminal.LastAttempt < 0 || terminal.LastAttempt > routeattempt.MaxAttempts {
		return ErrInvalid
	}
	body, err := s.capturedBody(terminal.Body)
	if err != nil {
		return err
	}
	return s.change(ctx, key, func(record *SharedSpoolRecord) error {
		if record.ReceiptID == "" || terminal.LastAttempt != len(record.Attempts) {
			return ErrSharedSpoolConflict
		}
		if record.Terminal != nil {
			if record.Terminal.LastAttempt != terminal.LastAttempt || !sameSharedBody(record.Terminal.Body, body) {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		record.Terminal = &SharedSpoolTerminal{LastAttempt: terminal.LastAttempt, Body: body}
		return nil
	})
}

func (s *SharedSpool) MakeReady(ctx context.Context, receipt SharedSpoolTerminalReceipt) error {
	if !s.validReceipt(receipt) {
		return ErrInvalid
	}
	return s.change(ctx, receipt.Key, func(record *SharedSpoolRecord) error {
		if record.Terminal == nil || record.Terminal.Body.Digest != receipt.TerminalDigest || record.ReceiptID != receipt.ReceiptID {
			return ErrSharedSpoolConflict
		}
		if record.Ready != nil && *record.Ready != receipt {
			return ErrSharedSpoolConflict
		}
		copy := receipt
		record.Ready = &copy
		return nil
	})
}

func (s *SharedSpool) ReadReady(ctx context.Context, limit int) ([]SharedSpoolDelivery, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > s.options.Capacity {
		return nil, ErrInvalid
	}
	entries := []SharedSpoolDelivery{}
	err := s.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(sharedSpoolReadyBucket).Cursor()
		for key, raw := cursor.First(); key != nil && len(entries) < limit; key, raw = cursor.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}
			var receipt SharedSpoolTerminalReceipt
			if len(raw) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(raw, &receipt) != nil || !s.validReceipt(receipt) || receipt.Key.RequestID != string(key) {
				return ErrInvalid
			}
			record, err := s.readRecord(tx, receipt.Key)
			if err != nil {
				return err
			}
			if record.Ready == nil || *record.Ready != receipt || record.Terminal == nil {
				return ErrInvalid
			}
			entries = append(entries, SharedSpoolDelivery{Receipt: receipt, Data: bytes.Clone(record.Terminal.Body.Data)})
		}
		return nil
	})
	return entries, err
}

// AckReady follows successful idempotent primary call-fact delivery. A matching
// delivered receipt is permanently idempotent while bounded history remains.
// Exhaustion leaves the ready fact intact; no tombstone is silently evicted.
// This spool holds no economic counters or original shared SQL receipts.
func (s *SharedSpool) AckReady(ctx context.Context, receipt SharedSpoolTerminalReceipt) error {
	if !s.validReceipt(receipt) {
		return ErrInvalid
	}
	return s.update(ctx, func(tx *bolt.Tx) error {
		key := []byte(receipt.Key.RequestID)
		ack := tx.Bucket(sharedSpoolAckBucket)
		if raw := ack.Get(key); raw != nil {
			var stored SharedSpoolTerminalReceipt
			if len(raw) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(raw, &stored) != nil || !s.validReceipt(stored) {
				return ErrInvalid
			}
			if stored != receipt {
				return ErrSharedSpoolConflict
			}
			return nil
		}
		record, err := s.readRecord(tx, receipt.Key)
		if err != nil {
			return err
		}
		if record.Ready == nil || *record.Ready != receipt {
			return ErrSharedSpoolConflict
		}
		if ack.Stats().KeyN >= s.options.DeliveredCapacity {
			return ErrSharedSpoolHistory
		}
		raw, err := json.Marshal(receipt)
		if err != nil {
			return err
		}
		if len(raw) > SharedSpoolMaxDeliveredBytes {
			return ErrInvalid
		}
		if err := ack.Put(key, raw); err != nil {
			return err
		}
		if err := tx.Bucket(sharedSpoolReadyBucket).Delete(key); err != nil {
			return err
		}
		return tx.Bucket(sharedSpoolRecordBucket).Delete(key)
	})
}

func (s *SharedSpool) change(ctx context.Context, key SharedSpoolKey, transition func(*SharedSpoolRecord) error) error {
	if !s.validKey(key) {
		return ErrInvalid
	}
	return s.update(ctx, func(tx *bolt.Tx) error {
		record, err := s.readRecord(tx, key)
		if err != nil {
			return err
		}
		if err := transition(record); err != nil {
			return err
		}
		return s.putRecord(tx, record)
	})
}

func (s *SharedSpool) readRecord(tx *bolt.Tx, key SharedSpoolKey) (*SharedSpoolRecord, error) {
	raw := tx.Bucket(sharedSpoolRecordBucket).Get([]byte(key.RequestID))
	if raw == nil {
		if delivered := tx.Bucket(sharedSpoolAckBucket).Get([]byte(key.RequestID)); delivered != nil {
			var receipt SharedSpoolTerminalReceipt
			if len(delivered) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(delivered, &receipt) != nil || !s.validReceipt(receipt) {
				return nil, ErrInvalid
			}
			if receipt.Key != key {
				return nil, ErrSharedSpoolConflict
			}
			return nil, ErrSharedSpoolDelivered
		}
		return nil, ErrMissing
	}
	record, err := s.decodeRecord(raw)
	if err != nil {
		return nil, err
	}
	if record.Key != key {
		return nil, ErrSharedSpoolConflict
	}
	return record, nil
}

func (s *SharedSpool) putRecord(tx *bolt.Tx, record *SharedSpoolRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := s.decodeRecord(raw); err != nil {
		return err
	}
	key := []byte(record.Key.RequestID)
	if err := tx.Bucket(sharedSpoolRecordBucket).Put(key, raw); err != nil {
		return err
	}
	if record.Ready != nil {
		receipt, err := json.Marshal(record.Ready)
		if err != nil {
			return err
		}
		if len(receipt) > SharedSpoolMaxDeliveredBytes {
			return ErrInvalid
		}
		return tx.Bucket(sharedSpoolReadyBucket).Put(key, receipt)
	}
	return nil
}

func sameSharedBody(left, right SharedSpoolBody) bool {
	return left.Digest == right.Digest && left.Checksum == right.Checksum && bytes.Equal(left.Data, right.Data)
}
