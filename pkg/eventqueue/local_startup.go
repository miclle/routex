package eventqueue

import (
	"context"
	"errors"
	"os"

	bolt "go.etcd.io/bbolt"
)

type localQueueStartup struct {
	ctx         context.Context
	identity    string
	established bool
	absent      bool
	fileInfo    os.FileInfo
}

// OpenBoundLocal validates mode and identity before any recovery mutation, on
// the same exclusively locked file handle used for recovery and binding. The
// caller must independently retain its persisted local-mode SQL startup locks.
// This operation is not live-cutover fencing or shared admission activation.
func OpenBoundLocal(ctx context.Context, path string, capacity, maxPayload int, identity string, established bool) (*Queue, error) {
	return openBoundLocal(ctx, path, capacity, maxPayload, identity, established, nil)
}

// The private seam permits deterministic source qualification of changes after
// Lstat. Production callers always pass nil and have no filesystem hook.
func openBoundLocal(ctx context.Context, path string, capacity, maxPayload int, identity string, established bool, beforeOpen func()) (*Queue, error) {
	if ctx == nil || path == "" || !validKey.MatchString(identity) || capacity < 1 || maxPayload < 1 {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	absent := errors.Is(err, os.ErrNotExist)
	if err != nil && !absent {
		return nil, err
	}
	if absent && established || !absent && (!info.Mode().IsRegular() || info.Size() == 0) {
		return nil, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if beforeOpen != nil {
		beforeOpen()
	}
	return openQueue(path, capacity, maxPayload, &localQueueStartup{ctx: ctx, identity: identity, established: established, absent: absent, fileInfo: info})
}

// bbolt normally opens with O_CREATE and initializes an empty file before its
// transaction callback. Check the retained inode before allowing bbolt access;
// existing paths never create/truncate, and fresh paths use exclusive creation.
func (startup *localQueueStartup) openFile(path string, flags int, mode os.FileMode) (*os.File, error) {
	if err := startup.ctx.Err(); err != nil {
		return nil, err
	}
	flags &^= os.O_TRUNC
	if startup.absent {
		return os.OpenFile(path, flags|os.O_CREATE|os.O_EXCL, mode)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !current.Mode().IsRegular() || current.Size() == 0 || !os.SameFile(startup.fileInfo, current) {
		return nil, ErrInvalid
	}
	file, err := os.OpenFile(path, flags&^os.O_CREATE, mode)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() == 0 || !os.SameFile(startup.fileInfo, opened) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalid
	}
	current, err = os.Lstat(path)
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		_ = file.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrInvalid
	}
	if err := startup.ctx.Err(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateLocalQueueStartup(tx *bolt.Tx, startup localQueueStartup) error {
	if err := startup.ctx.Err(); err != nil {
		return err
	}
	for _, name := range sharedSpoolBuckets() {
		if tx.Bucket(name) != nil {
			return ErrSharedSpoolMode
		}
	}
	if startup.absent {
		// A file appearing between Lstat and the exclusive open must not be
		// mistaken for the fresh file this operation was authorized to create.
		if key, _ := tx.Cursor().First(); key != nil {
			return ErrInvalid
		}
		return nil
	}
	metadata := tx.Bucket(limitMetaBucket)
	if metadata == nil || string(metadata.Get([]byte("installation"))) != startup.identity {
		return ErrInvalid
	}
	version := string(metadata.Get([]byte("version")))
	if version != "1" && version != "2" {
		return ErrInvalid
	}
	for _, name := range [][]byte{pendingBucket, readyBucket, rpmBucket, leaseBucket} {
		if tx.Bucket(name) == nil {
			return ErrInvalid
		}
	}
	if startup.established {
		return nil
	}
	// An interrupted first bind may retry only its exact empty format1 journal.
	// A matching ID alone cannot turn retained economic history into freshness.
	if version != "1" || metadata.Sequence() != 0 {
		return ErrInvalid
	}
	for _, name := range [][]byte{rpmCountBucket, activeCountBucket} {
		if tx.Bucket(name) == nil {
			return ErrInvalid
		}
	}
	if err := metadata.ForEach(func(key, value []byte) error {
		if value == nil || string(key) != "version" && string(key) != "installation" {
			return ErrInvalid
		}
		return nil
	}); err != nil {
		return err
	}
	return tx.ForEach(func(name []byte, bucket *bolt.Bucket) error {
		if string(name) == string(limitMetaBucket) {
			return nil
		}
		known := false
		for _, expected := range [][]byte{pendingBucket, readyBucket, rpmBucket, leaseBucket, rpmCountBucket, activeCountBucket} {
			known = known || string(name) == string(expected)
		}
		key, _ := bucket.Cursor().First()
		if !known || key != nil || bucket.Sequence() != 0 {
			return ErrInvalid
		}
		return nil
	})
}
