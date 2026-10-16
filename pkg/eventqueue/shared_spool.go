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
	"time"

	"github.com/miclle/routex/pkg/routeattempt"
	bolt "go.etcd.io/bbolt"
)

var (
	ErrSharedSpoolMode      = errors.New("shared spool mode or identity does not match")
	ErrSharedSpoolConflict  = errors.New("shared spool transition conflicts")
	ErrSharedSpoolDelivered = errors.New("shared spool record was delivered")
	ErrSharedSpoolHistory   = errors.New("shared spool delivered history is full")
	sharedSpoolMetaBucket   = []byte("shared-spool-meta-v1")
	sharedSpoolRecordBucket = []byte("shared-spool-records-v1")
	sharedSpoolReadyBucket  = []byte("shared-spool-ready-v1")
	sharedSpoolAckBucket    = []byte("shared-spool-delivered-v1")
)

// Body bytes include the consumer's serialized metadata and repeated opaque
// facts. The record bound includes their base64 JSON expansion and all stages.
// These are retained live-data bounds, not a filesystem size/quota guarantee.
const (
	SharedSpoolMaxDataBytes      = 128 << 10
	SharedSpoolMaxRecordBytes    = 2 << 20
	SharedSpoolMaxDeliveredBytes = 2 << 10
)

type SharedSpoolIdentity struct {
	InstallationID string
	SpoolID        string
	ModeRevision   uint64
}

type SharedSpoolOptions struct {
	Capacity          int
	MaxDataBytes      int
	DeliveredCapacity int
}

type SharedSpoolKey struct {
	Identity        SharedSpoolIdentity
	IncarnationID   string
	Fence           uint64
	ExecutionID     string
	RequestID       string
	AdmissionDigest string
}

// Digest is the consumer's canonical operation digest. Checksum protects the
// exact serialized bytes separately; this package does not parse business data.
type SharedSpoolBody struct {
	Digest   string
	Checksum string
	Data     []byte
}

type SharedSpoolAttempt struct {
	Number int
	Body   SharedSpoolBody
}

type SharedSpoolDispatch struct {
	Key           SharedSpoolKey
	Number        int
	AttemptDigest string
}

type SharedSpoolCheckpoint struct {
	Number        int
	AttemptDigest string
	Body          SharedSpoolBody
}

type SharedSpoolTerminal struct {
	LastAttempt int
	Body        SharedSpoolBody
}

// The trusted consumer supplies this only after confirming the exact shared
// authority terminal receipt. Persisting a receipt identifier locally is not
// independent SQL proof; no economic authority or signature is invented here.
type SharedSpoolTerminalReceipt struct {
	Key            SharedSpoolKey
	ReceiptID      string
	TerminalDigest string
}

type SharedSpoolRecord struct {
	Key         SharedSpoolKey
	Intent      SharedSpoolBody
	ReceiptID   string
	Attempts    []SharedSpoolAttempt
	Dispatches  []SharedSpoolDispatch
	Checkpoints []SharedSpoolCheckpoint
	Terminal    *SharedSpoolTerminal
	Ready       *SharedSpoolTerminalReceipt
}

type SharedSpoolDelivery struct {
	Receipt SharedSpoolTerminalReceipt
	Data    []byte
}

type sharedSpoolMetadata struct {
	Version  int
	Identity SharedSpoolIdentity
	Options  SharedSpoolOptions
}

type SharedSpool struct {
	db       *bolt.DB
	identity SharedSpoolIdentity
	options  SharedSpoolOptions
}

// OpenSharedSpool owns an exclusive synchronous bbolt handle. Reopening never
// promotes pending records, settles economics or restores inference permits.
// Existing local, foreign and partially initialized journals are rejected before
// any mutation; automatic migration, repair and mode downgrade are forbidden.
func OpenSharedSpool(ctx context.Context, path string, identity SharedSpoolIdentity, options SharedSpoolOptions) (*SharedSpool, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if path == "" || !validSharedIdentity(identity) || options.Capacity < 1 || options.Capacity > quotaHistoryCapacity ||
		options.MaxDataBytes < 1 || options.MaxDataBytes > SharedSpoolMaxDataBytes || options.DeliveredCapacity < 1 || options.DeliveredCapacity > quotaHistoryCapacity {
		return nil, ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	timeout := time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, context.DeadlineExceeded
		}
		if remaining < timeout {
			timeout = remaining
		}
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: timeout})
	if err != nil {
		return nil, err
	}
	spool := &SharedSpool{db: db, identity: identity, options: options}
	err = db.Update(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		metadata := sharedSpoolMetadata{Version: 1, Identity: identity, Options: options}
		bucket := tx.Bucket(sharedSpoolMetaBucket)
		if bucket == nil {
			count := 0
			if err := tx.ForEach(func(_ []byte, _ *bolt.Bucket) error { count++; return nil }); err != nil {
				return err
			}
			if count != 0 {
				return ErrSharedSpoolMode
			}
			for _, name := range sharedSpoolBuckets() {
				if _, err := tx.CreateBucket(name); err != nil {
					return err
				}
			}
			raw, err := json.Marshal(metadata)
			if err != nil {
				return err
			}
			if err := tx.Bucket(sharedSpoolMetaBucket).Put([]byte("binding"), raw); err != nil {
				return err
			}
		} else {
			var stored sharedSpoolMetadata
			binding := bucket.Get([]byte("binding"))
			if len(binding) == 0 || len(binding) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(binding, &stored) != nil {
				return ErrInvalid
			}
			if stored != metadata {
				return ErrSharedSpoolMode
			}
			if err := spool.validateJournal(tx); err != nil {
				return err
			}
		}
		return ctx.Err()
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return spool, nil
}

func (s *SharedSpool) Close() error                  { return s.db.Close() }
func (s *SharedSpool) Identity() SharedSpoolIdentity { return s.identity }

// Capacity is the immutable reservation bound reviewed when this spool was opened.
func (s *SharedSpool) Capacity() int { return s.options.Capacity }

func sharedSpoolBuckets() [][]byte {
	return [][]byte{sharedSpoolMetaBucket, sharedSpoolRecordBucket, sharedSpoolReadyBucket, sharedSpoolAckBucket}
}

func (s *SharedSpool) validateJournal(tx *bolt.Tx) error {
	if err := tx.ForEach(func(name []byte, _ *bolt.Bucket) error {
		for _, allowed := range sharedSpoolBuckets() {
			if bytes.Equal(name, allowed) {
				return nil
			}
		}
		return ErrSharedSpoolMode
	}); err != nil {
		return err
	}
	for _, name := range sharedSpoolBuckets() {
		if tx.Bucket(name) == nil {
			return ErrInvalid
		}
	}
	if tx.Bucket(sharedSpoolMetaBucket).Stats().KeyN != 1 {
		return ErrInvalid
	}
	records, ready, delivered := tx.Bucket(sharedSpoolRecordBucket), tx.Bucket(sharedSpoolReadyBucket), tx.Bucket(sharedSpoolAckBucket)
	if records.Stats().KeyN > s.options.Capacity || delivered.Stats().KeyN > s.options.DeliveredCapacity {
		return ErrFull
	}
	if err := records.ForEach(func(key, raw []byte) error {
		record, err := s.decodeRecord(raw)
		if err != nil || record.Key.RequestID != string(key) || delivered.Get(key) != nil {
			return ErrInvalid
		}
		index := ready.Get(key)
		if record.Ready == nil {
			if index != nil {
				return ErrInvalid
			}
			return nil
		}
		var receipt SharedSpoolTerminalReceipt
		if len(index) == 0 || len(index) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(index, &receipt) != nil || receipt != *record.Ready {
			return ErrInvalid
		}
		return nil
	}); err != nil {
		return err
	}
	if err := ready.ForEach(func(key, _ []byte) error {
		if records.Get(key) == nil {
			return ErrInvalid
		}
		return nil
	}); err != nil {
		return err
	}
	return delivered.ForEach(func(key, raw []byte) error {
		var receipt SharedSpoolTerminalReceipt
		if len(raw) > SharedSpoolMaxDeliveredBytes || json.Unmarshal(raw, &receipt) != nil || !s.validReceipt(receipt) || receipt.Key.RequestID != string(key) {
			return ErrInvalid
		}
		return nil
	})
}

func validSharedIdentity(identity SharedSpoolIdentity) bool {
	return validKey.MatchString(identity.InstallationID) && validKey.MatchString(identity.SpoolID) && identity.ModeRevision != 0
}

func (s *SharedSpool) validKey(key SharedSpoolKey) bool {
	return key.Identity == s.identity && validKey.MatchString(key.IncarnationID) && key.Fence != 0 && validKey.MatchString(key.ExecutionID) &&
		validKey.MatchString(key.RequestID) && validSharedDigest(key.AdmissionDigest)
}

func validSharedDigest(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	raw, err := hex.DecodeString(digest)
	return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == digest
}

func (s *SharedSpool) validBody(body SharedSpoolBody) bool {
	if !validSharedDigest(body.Digest) || len(body.Data) == 0 || len(body.Data) > s.options.MaxDataBytes {
		return false
	}
	digest := sha256.Sum256(body.Data)
	return body.Checksum == hex.EncodeToString(digest[:])
}

func (s *SharedSpool) capturedBody(body SharedSpoolBody) (SharedSpoolBody, error) {
	if !validSharedDigest(body.Digest) || len(body.Data) == 0 || len(body.Data) > s.options.MaxDataBytes {
		return SharedSpoolBody{}, ErrInvalid
	}
	data := bytes.Clone(body.Data)
	digest := sha256.Sum256(data)
	checksum := hex.EncodeToString(digest[:])
	if body.Checksum != "" && body.Checksum != checksum {
		return SharedSpoolBody{}, ErrInvalid
	}
	return SharedSpoolBody{Digest: body.Digest, Checksum: checksum, Data: data}, nil
}

func (s *SharedSpool) validReceipt(receipt SharedSpoolTerminalReceipt) bool {
	return s.validKey(receipt.Key) && validKey.MatchString(receipt.ReceiptID) && validSharedDigest(receipt.TerminalDigest)
}

func (s *SharedSpool) decodeRecord(raw []byte) (*SharedSpoolRecord, error) {
	var record SharedSpoolRecord
	if len(raw) == 0 || len(raw) > SharedSpoolMaxRecordBytes || json.Unmarshal(raw, &record) != nil || !s.validKey(record.Key) ||
		!s.validBody(record.Intent) || record.Intent.Digest != record.Key.AdmissionDigest || len(record.Attempts) > routeattempt.MaxAttempts ||
		len(record.Dispatches) > len(record.Attempts) || len(record.Checkpoints) > len(record.Attempts) || record.ReceiptID != "" && !validKey.MatchString(record.ReceiptID) {
		return nil, ErrInvalid
	}
	if len(record.Attempts) > 0 && record.ReceiptID == "" {
		return nil, ErrInvalid
	}
	for i, attempt := range record.Attempts {
		if attempt.Number != i+1 || !s.validBody(attempt.Body) || i > 0 && (len(record.Dispatches) < i || len(record.Checkpoints) < i) {
			return nil, ErrInvalid
		}
	}
	for i, dispatch := range record.Dispatches {
		if dispatch.Key != record.Key || dispatch.Number != i+1 || dispatch.AttemptDigest != record.Attempts[i].Body.Digest {
			return nil, ErrInvalid
		}
	}
	for i, checkpoint := range record.Checkpoints {
		if checkpoint.Number != i+1 || checkpoint.AttemptDigest != record.Attempts[i].Body.Digest || !s.validBody(checkpoint.Body) {
			return nil, ErrInvalid
		}
	}
	if record.Terminal != nil && (record.ReceiptID == "" || record.Terminal.LastAttempt != len(record.Attempts) || !s.validBody(record.Terminal.Body)) {
		return nil, ErrInvalid
	}
	if record.Ready != nil && (record.Terminal == nil || !s.validReceipt(*record.Ready) || record.Ready.Key != record.Key ||
		record.Ready.ReceiptID != record.ReceiptID || record.Ready.TerminalDigest != record.Terminal.Body.Digest) {
		return nil, ErrInvalid
	}
	return &record, nil
}

// Each mutation checks cancellation before entering the transaction and again
// immediately before its synchronous commit. An in-progress fsync is not
// interruptible; consumers reconcile any uncertain return using exact read-back.
func (s *SharedSpool) update(ctx context.Context, change func(*bolt.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := change(tx); err != nil {
			return err
		}
		return ctx.Err()
	})
}
