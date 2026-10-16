package eventqueue

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	berrors "go.etcd.io/bbolt/errors"
)

var (
	ErrLocalInspectionBound  = errors.New("local journal inspection exceeds a byte bound")
	ErrLocalInspectionClosed = errors.New("local journal inspection is closed")
)

// These bounds apply to raw evidence returned by one operation, not to total
// journal size or economic history. Oversized evidence is never truncated.
const (
	LocalInspectionMaxHeaderBytes = 64 << 10
	LocalInspectionMaxPageBytes   = 1 << 20
	LocalInspectionMaxPageEntries = 512
	LocalInspectionMaxKeyBytes    = 32 << 10
	LocalInspectionMaxValueBytes  = 1 << 20
)

// LocalInspection holds a read-only transaction and shared file lock until
// Close. The lock excludes ordinary bbolt writers on the same inode. It is not
// process-quiescence proof and cannot prevent external replacement of the path.
type LocalInspection struct {
	mu     sync.Mutex
	db     *bolt.DB
	tx     *bolt.Tx
	header LocalInspectionHeader
}

type LocalInspectionEntry struct {
	Key, Value []byte
}

type LocalInspectionBucket struct {
	Name     []byte
	Sequence uint64
}

// LocalInspectionQuotaMetadata reports recorded metadata only. CoverageStart
// does not establish complete coverage, valid receipts, or transferable holds.
type LocalInspectionQuotaMetadata struct {
	TimeZone      string
	CoverageStart int64
}

type LocalInspectionHeader struct {
	FormatVersion  uint8
	InstallationID string
	ClockHighWater *int64
	Quota          *LocalInspectionQuotaMetadata
	Metadata       []LocalInspectionEntry
	Buckets        []LocalInspectionBucket
	RawBytes       int
}

// LocalInspectionPage preserves exact opaque values. Exhausted applies only to
// this bucket in this snapshot; it does not assert semantic or transfer validity.
type LocalInspectionPage struct {
	Bucket    []byte
	Sequence  uint64
	Entries   []LocalInspectionEntry
	NextAfter []byte
	Exhausted bool
	RawBytes  int
}

// OpenLocalInspection opens an existing regular local v1/v2 journal without
// creating files/directories, recovering, rebuilding, pruning or writing. Lock
// acquisition has a one-second ceiling and observes context cancellation between
// short nonblocking attempts. No background opener survives cancellation.
func OpenLocalInspection(ctx context.Context, path string) (*LocalInspection, error) {
	if ctx == nil || path == "" {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	original, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !original.Mode().IsRegular() || original.Size() == 0 {
		return nil, ErrInvalid
	}
	openFile := func(name string, flags int, mode os.FileMode) (*os.File, error) {
		if flags != os.O_RDONLY {
			return nil, ErrInvalid
		}
		file, openErr := os.OpenFile(name, flags, mode)
		if openErr != nil {
			return nil, openErr
		}
		actual, statErr := file.Stat()
		if statErr != nil || !actual.Mode().IsRegular() || actual.Size() == 0 || !os.SameFile(original, actual) {
			_ = file.Close()
			if statErr != nil {
				return nil, statErr
			}
			return nil, ErrInvalid
		}
		return file, nil
	}
	lockContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	var db *bolt.DB
	for {
		if err := lockContext.Err(); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, berrors.ErrTimeout
		}
		db, err = bolt.Open(path, 0, &bolt.Options{ReadOnly: true, Timeout: time.Nanosecond, OpenFile: openFile})
		if !errors.Is(err, berrors.ErrTimeout) {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-lockContext.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
	if err != nil {
		return nil, err
	}
	inspection := &LocalInspection{db: db}
	if err := ctx.Err(); err != nil {
		_ = db.Close()
		return nil, err
	}
	inspection.tx, err = db.Begin(false)
	if err == nil {
		inspection.header, err = localInspectionHeader(ctx, inspection.tx)
	}
	if err != nil {
		_ = inspection.Close()
		return nil, err
	}
	return inspection, nil
}

func (inspection *LocalInspection) Close() error {
	inspection.mu.Lock()
	defer inspection.mu.Unlock()
	if inspection.db == nil {
		return nil
	}
	var err error
	if inspection.tx != nil {
		err = inspection.tx.Rollback()
	}
	err = errors.Join(err, inspection.db.Close())
	inspection.tx, inspection.db = nil, nil
	return err
}

// Header returns detached metadata and bucket sequences from the retained
// transaction. Missing binding/clock remain absent instead of being initialized.
func (inspection *LocalInspection) Header(ctx context.Context) (LocalInspectionHeader, error) {
	if ctx == nil {
		return LocalInspectionHeader{}, ErrInvalid
	}
	inspection.mu.Lock()
	defer inspection.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return LocalInspectionHeader{}, err
	}
	if inspection.tx == nil {
		return LocalInspectionHeader{}, ErrLocalInspectionClosed
	}
	header := inspection.header
	header.Metadata = cloneLocalInspectionEntries(header.Metadata)
	header.Buckets = append([]LocalInspectionBucket(nil), header.Buckets...)
	for index := range header.Buckets {
		header.Buckets[index].Name = bytes.Clone(header.Buckets[index].Name)
	}
	if header.ClockHighWater != nil {
		value := *header.ClockHighWater
		header.ClockHighWater = &value
	}
	if header.Quota != nil {
		value := *header.Quota
		header.Quota = &value
	}
	return header, nil
}

// ReadPage reads exclusively after a raw key within one whitelisted bucket.
// RawBytes includes bucket name, its eight-byte sequence and all key/value bytes.
// Cancellation, nested buckets and oversized individual entries return no page.
func (inspection *LocalInspection) ReadPage(ctx context.Context, name, after []byte, maxEntries, maxBytes int) (LocalInspectionPage, error) {
	if ctx == nil {
		return LocalInspectionPage{}, ErrInvalid
	}
	inspection.mu.Lock()
	defer inspection.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return LocalInspectionPage{}, err
	}
	if inspection.tx == nil {
		return LocalInspectionPage{}, ErrLocalInspectionClosed
	}
	if maxEntries < 1 || maxEntries > LocalInspectionMaxPageEntries || maxBytes < 1 || maxBytes > LocalInspectionMaxPageBytes || len(after) > LocalInspectionMaxKeyBytes {
		return LocalInspectionPage{}, ErrInvalid
	}
	bucket := inspection.tx.Bucket(name)
	if bucket == nil || len(name) > LocalInspectionMaxKeyBytes {
		return LocalInspectionPage{}, ErrInvalid
	}
	page := LocalInspectionPage{Bucket: bytes.Clone(name), Sequence: bucket.Sequence(), RawBytes: len(name) + 8}
	if page.RawBytes > maxBytes {
		return LocalInspectionPage{}, ErrLocalInspectionBound
	}
	cursor := bucket.Cursor()
	key, value := cursor.First()
	if len(after) != 0 {
		key, value = cursor.Seek(after)
		if bytes.Equal(key, after) {
			key, value = cursor.Next()
		}
	}
	for key != nil && len(page.Entries) < maxEntries {
		if err := ctx.Err(); err != nil {
			return LocalInspectionPage{}, err
		}
		if value == nil {
			return LocalInspectionPage{}, ErrInvalid
		}
		if len(key) > LocalInspectionMaxKeyBytes || len(value) > LocalInspectionMaxValueBytes {
			return LocalInspectionPage{}, ErrLocalInspectionBound
		}
		entryBytes := len(key) + len(value)
		if entryBytes > maxBytes-page.RawBytes {
			if len(page.Entries) == 0 {
				return LocalInspectionPage{}, ErrLocalInspectionBound
			}
			break
		}
		page.Entries = append(page.Entries, LocalInspectionEntry{Key: bytes.Clone(key), Value: bytes.Clone(value)})
		page.RawBytes += entryBytes
		page.NextAfter = bytes.Clone(key)
		key, value = cursor.Next()
	}
	if err := ctx.Err(); err != nil {
		return LocalInspectionPage{}, err
	}
	page.Exhausted = key == nil
	return page, nil
}

func localInspectionHeader(ctx context.Context, tx *bolt.Tx) (LocalInspectionHeader, error) {
	header := LocalInspectionHeader{}
	metadata := tx.Bucket(limitMetaBucket)
	if metadata == nil {
		return header, ErrInvalid
	}
	version := metadata.Get([]byte("version"))
	if bytes.Equal(version, []byte("1")) {
		header.FormatVersion = 1
	} else if bytes.Equal(version, []byte("2")) {
		header.FormatVersion = 2
	} else {
		return header, ErrInvalid
	}
	// Freeze the recognized v1/v2 namespace; future runtime bucket additions
	// must not silently expand this historical inspection format.
	allowed := [][]byte{[]byte("pending-v1"), []byte("ready-v1"), []byte("rpm-v1"), []byte("leases-v1"), []byte("rpm-counts-v1"), []byte("active-counts-v1"), []byte("limit-meta-v1")}
	if header.FormatVersion == 2 {
		allowed = append(allowed, []byte("quota-entries-v2"), []byte("quota-counters-v2"), []byte("quota-expiry-v2"), []byte("quota-accounts-v2"), []byte("quota-invalid-bounds-v2"))
	}
	err := tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		known := false
		for _, candidate := range allowed {
			known = known || bytes.Equal(candidate, name)
		}
		if !known {
			return ErrInvalid
		}
		header.RawBytes += len(name) + 8
		header.Buckets = append(header.Buckets, LocalInspectionBucket{Name: bytes.Clone(name), Sequence: bucket.Sequence()})
		return nil
	})
	if err != nil || len(header.Buckets) != len(allowed) {
		if err != nil {
			return LocalInspectionHeader{}, err
		}
		return LocalInspectionHeader{}, ErrInvalid
	}
	err = metadata.ForEach(func(key, value []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if value == nil {
			return ErrInvalid
		}
		if len(key)+len(value) > LocalInspectionMaxHeaderBytes-header.RawBytes {
			return ErrLocalInspectionBound
		}
		header.RawBytes += len(key) + len(value)
		switch string(key) {
		case "version":
		case "installation":
			if !validKey.Match(value) {
				return ErrInvalid
			}
			header.InstallationID = string(value)
		case "clock":
			if len(value) != 8 {
				return ErrInvalid
			}
			clock := binary.BigEndian.Uint64(value)
			if clock == 0 || clock > math.MaxInt64 {
				return ErrInvalid
			}
			instant := int64(clock)
			header.ClockHighWater = &instant
		case "quota":
			if header.FormatVersion != 2 {
				return ErrInvalid
			}
			quota := LocalInspectionQuotaMetadata{}
			decoder := json.NewDecoder(bytes.NewReader(value))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&quota) != nil || quota.CoverageStart <= 0 || quota.TimeZone == "" {
				return ErrInvalid
			}
			if _, err := time.LoadLocation(quota.TimeZone); err != nil {
				return ErrInvalid
			}
			if decoder.Decode(new(any)) != io.EOF {
				return ErrInvalid
			}
			header.Quota = &quota
		default:
			return ErrInvalid
		}
		header.Metadata = append(header.Metadata, LocalInspectionEntry{Key: bytes.Clone(key), Value: bytes.Clone(value)})
		return nil
	})
	if err != nil {
		return LocalInspectionHeader{}, err
	}
	if header.FormatVersion == 2 && (header.Quota == nil || header.ClockHighWater == nil || *header.ClockHighWater < header.Quota.CoverageStart) {
		return LocalInspectionHeader{}, ErrInvalid
	}
	return header, nil
}

func cloneLocalInspectionEntries(entries []LocalInspectionEntry) []LocalInspectionEntry {
	copy := make([]LocalInspectionEntry, len(entries))
	for index, entry := range entries {
		copy[index] = LocalInspectionEntry{Key: bytes.Clone(entry.Key), Value: bytes.Clone(entry.Value)}
	}
	return copy
}
