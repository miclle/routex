package eventqueue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
)

var (
	ErrLocalTransferBound      = errors.New("local transfer validation exceeds an explicit total bound")
	ErrLocalTransferNotDrained = errors.New("local transfer validation requires a drained journal")
)

// LocalTransferBounds has no defaults. Bytes bounds all returned inspection
// bytes, including repeated page framing and header metadata. Records bounds
// bucket entries; accounts bounds distinct economic/RPM identities; references
// bounds receipt-account, expiry, RPM and invalid-bound relationships.
type LocalTransferBounds struct {
	Bytes      int64
	Records    int
	Accounts   int
	References int
}

// LocalTransferBucket retains original keys/values, not re-encoded receipts.
type LocalTransferBucket struct {
	Name     []byte
	Sequence uint64
	Entries  []LocalInspectionEntry
}

type LocalTransferAccountBirth struct {
	Account string
	// Zero is recorded unknown prehistory, never a new birth at cutover.
	RecordedUnixNano int64
}

type LocalTransferQuotaProjection struct {
	Account, Window string
	Counter         QuotaCounter
}

type LocalTransferRPMProjection struct {
	Account      string
	Stored, Live int64
}

type LocalTransferUnproven string

type LocalTransferState string

// A successful consistency check still leaves the actual transfer incomplete.
const LocalTransferIncomplete LocalTransferState = "incomplete"

const (
	LocalTransferHistoricalProvenance LocalTransferUnproven = "historical_provenance"
	LocalTransferExecutorClosure      LocalTransferUnproven = "executor_closure"
	LocalTransferBindingAuthority     LocalTransferUnproven = "installation_binding_authority"
	LocalTransferSQLCalendar          LocalTransferUnproven = "historical_sql_calendar"
	LocalTransferSharedCoordinates    LocalTransferUnproven = "shared_receipt_coordinates"
	LocalTransferIngestionAndCutover  LocalTransferUnproven = "ingestion_and_cutover"
)

// LocalTransferValidation is a complete, bounded census and consistency check
// of a drained original local-v2 snapshot. It is never a transfer certificate.
// Raw digest consistency cannot authenticate the original history or establish
// deployment exclusion, executor closure, SQL identity, or shared receipt IDs.
type LocalTransferValidation struct {
	TransferState   LocalTransferState
	Header          LocalInspectionHeader
	Raw             []LocalTransferBucket
	RawSHA256       string
	BytesRead       int64
	RecordsRead     int
	ReferencesRead  int
	CutoverUnixNano int64
	AccountBirths   []LocalTransferAccountBirth
	// StoredQuota includes expired contributions still referenced by the journal.
	// LiveQuota independently excludes those expired at the requested cutover.
	StoredQuota []LocalTransferQuotaProjection
	LiveQuota   []LocalTransferQuotaProjection
	RPM         []LocalTransferRPMProjection
	// Ack may remove terminal payloads; receipt digest syntax is checked but its
	// historical payload cannot be reconstructed or its authenticity inferred.
	UnverifiedSettlementDigests int
	Unproven                    []LocalTransferUnproven
}

// ValidateLocalTransferSnapshot uses only Header and ReadPage on the same
// retained read-only inspection transaction. The caller opens/closes it and
// separately establishes deployment exclusion. No Open, recovery, prune,
// write, migration, shared conversion or runtime activation occurs here.
// Failure returns no partial census or projection, including cancellation.
func ValidateLocalTransferSnapshot(ctx context.Context, inspection *LocalInspection, bounds LocalTransferBounds, cutoverUnixNano int64) (*LocalTransferValidation, error) {
	if ctx == nil || inspection == nil || bounds.Bytes < 1 || bounds.Records < 1 || bounds.Accounts < 1 || bounds.References < 1 || cutoverUnixNano < 1 {
		return nil, ErrInvalid
	}
	header, err := inspection.Header(ctx)
	if err != nil {
		return nil, err
	}
	if header.FormatVersion != 2 || header.Quota == nil || header.ClockHighWater == nil || cutoverUnixNano < *header.ClockHighWater {
		return nil, ErrInvalid
	}
	result := &LocalTransferValidation{Header: header, BytesRead: int64(header.RawBytes), CutoverUnixNano: cutoverUnixNano}
	if result.BytesRead > bounds.Bytes {
		return nil, ErrLocalTransferBound
	}
	for _, bucket := range header.Buckets {
		raw := LocalTransferBucket{Name: bytes.Clone(bucket.Name), Sequence: bucket.Sequence}
		var after []byte
		for {
			remainingBytes := bounds.Bytes - result.BytesRead
			if remainingBytes < int64(len(bucket.Name)+8) {
				return nil, ErrLocalTransferBound
			}
			pageBytes := int64(LocalInspectionMaxPageBytes)
			if remainingBytes < pageBytes {
				pageBytes = remainingBytes
			}
			pageEntries := min(LocalInspectionMaxPageEntries, max(1, bounds.Records-result.RecordsRead))
			page, err := inspection.ReadPage(ctx, bucket.Name, after, pageEntries, int(pageBytes))
			if err != nil {
				if errors.Is(err, ErrLocalInspectionBound) {
					return nil, errors.Join(ErrLocalTransferBound, err)
				}
				return nil, err
			}
			if page.Sequence != bucket.Sequence || !bytes.Equal(page.Bucket, bucket.Name) || !page.Exhausted && len(page.Entries) == 0 {
				return nil, ErrInvalid
			}
			if len(page.Entries) > bounds.Records-result.RecordsRead {
				return nil, ErrLocalTransferBound
			}
			result.BytesRead += int64(page.RawBytes)
			result.RecordsRead += len(page.Entries)
			raw.Entries = append(raw.Entries, page.Entries...)
			if page.Exhausted {
				break
			}
			after = page.NextAfter
		}
		result.Raw = append(result.Raw, raw)
	}
	if err := validateLocalTransferSemantics(ctx, result, bounds); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.RawSHA256, err = localTransferRawDigest(ctx, result.Raw)
	if err != nil {
		return nil, err
	}
	result.TransferState = LocalTransferIncomplete
	result.Unproven = []LocalTransferUnproven{LocalTransferHistoricalProvenance, LocalTransferExecutorClosure, LocalTransferBindingAuthority, LocalTransferSQLCalendar, LocalTransferSharedCoordinates, LocalTransferIngestionAndCutover}
	return result, nil
}

// The length-framed digest covers the complete sorted namespace, bucket
// sequences, and exact raw entries once. Page boundaries do not change it.
func localTransferRawDigest(ctx context.Context, buckets []LocalTransferBucket) (string, error) {
	digest := sha256.New()
	_, _ = digest.Write([]byte("routex-original-local-v2-census\x00"))
	localTransferDigestUint(digest, uint64(len(buckets)))
	for _, bucket := range buckets {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		localTransferDigestBytes(digest, bucket.Name)
		localTransferDigestUint(digest, bucket.Sequence)
		localTransferDigestUint(digest, uint64(len(bucket.Entries)))
		for _, entry := range bucket.Entries {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			localTransferDigestBytes(digest, entry.Key)
			localTransferDigestBytes(digest, entry.Value)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func localTransferDigestBytes(digest hash.Hash, value []byte) {
	localTransferDigestUint(digest, uint64(len(value)))
	_, _ = digest.Write(value)
}

func localTransferDigestUint(digest hash.Hash, value uint64) {
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], value)
	_, _ = digest.Write(raw[:])
}
