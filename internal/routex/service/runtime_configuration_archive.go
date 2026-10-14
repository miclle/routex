package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const runtimeArchiveChunkBytes = 32 << 10
const runtimeArchiveMaxChunks = runtimeConfigurationMaxBytesV1 / runtimeArchiveChunkBytes
const runtimeArchiveEnvelopeBytes = 128 << 10

var runtimeArchiveID = regexp.MustCompile(`^rcv_[0-7][0-9a-hjkmnp-tv-z]{25}$`)
var errRuntimeArchive = errors.New("runtime configuration archive unavailable")

func newRuntimeArchiveIdentity() (string, time.Time, error) {
	identity, err := id.NewPrefixed("rcv")
	return identity, time.Now().UTC().Truncate(time.Microsecond), err
}

func runtimeArchiveHeaderV1(identity string, birth time.Time, document runtimeConfigurationV1, length int, digest string) entity.RuntimeConfigurationArchive {
	return entity.RuntimeConfigurationArchive{ID: identity, CodecVersion: 1, ContentDigest: digest, SourceDigest: document.SourceDigest, AuthorizationDigest: document.AuthorizationProjectionDigest, NetworkDigest: document.NetworkPolicyDigest, PricingDigest: configurationPricingDigestV1(document.PricingObservation), PlaintextBytes: length, ChunkCount: (length + runtimeArchiveChunkBytes - 1) / runtimeArchiveChunkBytes, CreatedAt: birth.UTC()}
}

func runtimeArchiveHeaderValid(h entity.RuntimeConfigurationArchive) bool {
	return runtimeArchiveID.MatchString(h.ID) && h.CodecVersion == 1 && runtimeRouteDigest.MatchString(h.ContentDigest) && runtimeRouteDigest.MatchString(h.SourceDigest) && runtimeRouteDigest.MatchString(h.AuthorizationDigest) && runtimeRouteDigest.MatchString(h.NetworkDigest) && runtimeRouteDigest.MatchString(h.PricingDigest) && h.PlaintextBytes > 0 && h.PlaintextBytes <= runtimeConfigurationMaxBytesV1 && h.ChunkCount == (h.PlaintextBytes+runtimeArchiveChunkBytes-1)/runtimeArchiveChunkBytes && h.ChunkCount <= runtimeArchiveMaxChunks && !h.CreatedAt.IsZero() && h.CreatedAt.Equal(h.CreatedAt.Truncate(time.Microsecond))
}

func runtimeArchiveByteDigest(raw []byte) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("routex.gateway-archive-chunk.v1\x00"))
	_, _ = hash.Write(raw)
	return hex.EncodeToString(hash.Sum(nil))
}

// All immutable header and chunk fields form AAD. Ciphertext is deliberately
// excluded so envelope rewrap does not change source identity.
func runtimeArchiveChunkGeneration(h entity.RuntimeConfigurationArchive, c entity.RuntimeConfigurationArchiveChunk) string {
	tuple := struct {
		Domain, ID, Birth, Content, Source, Authorization, Network, Pricing string
		Codec, Bytes, Count                                                 int
		ChunkID                                                             string
		Ordinal, ChunkBytes                                                 int
		ChunkDigest                                                         string
	}{
		"routex.gateway-archive-aad.v1", h.ID, h.CreatedAt.UTC().Format(time.RFC3339Nano), h.ContentDigest, h.SourceDigest, h.AuthorizationDigest, h.NetworkDigest, h.PricingDigest, h.CodecVersion, h.PlaintextBytes, h.ChunkCount, c.ID, c.Ordinal, c.PlaintextBytes, c.PlaintextDigest,
	}
	raw, _ := json.Marshal(tuple)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func runtimeArchiveChunkReference(h entity.RuntimeConfigurationArchive, c entity.RuntimeConfigurationArchiveChunk) (string, error) {
	if !runtimeArchiveHeaderValid(h) || c.ArchiveID != h.ID || c.Ordinal < 0 || c.Ordinal >= h.ChunkCount || c.ID != fmt.Sprintf("%s-%03d", h.ID, c.Ordinal) || !runtimeRouteDigest.MatchString(c.PlaintextDigest) {
		return "", errRuntimeArchive
	}
	expected := runtimeArchiveChunkBytes
	if c.Ordinal == h.ChunkCount-1 {
		expected = h.PlaintextBytes - c.Ordinal*runtimeArchiveChunkBytes
	}
	if c.PlaintextBytes != expected || c.SecretGeneration != runtimeArchiveChunkGeneration(h, c) {
		return "", errRuntimeArchive
	}
	return "runtime-config:" + c.ID + ":" + c.SecretGeneration, nil
}

func runtimeArchiveHeadersEqual(a, b entity.RuntimeConfigurationArchive) bool {
	return a.ID == b.ID && a.CodecVersion == b.CodecVersion && a.ContentDigest == b.ContentDigest && a.SourceDigest == b.SourceDigest && a.AuthorizationDigest == b.AuthorizationDigest && a.NetworkDigest == b.NetworkDigest && a.PricingDigest == b.PricingDigest && a.PlaintextBytes == b.PlaintextBytes && a.ChunkCount == b.ChunkCount && a.CreatedAt.Equal(b.CreatedAt)
}

// storeRuntimeConfigurationArchiveV1 is an internal durable facts operation. The
// caller owns a stable identity/birth for identical retries; this operation
// neither proves coherent capture nor installs/activates any historical facts.
func (s *Service) storeRuntimeConfigurationArchiveV1(ctx context.Context, identity string, birth time.Time, document runtimeConfigurationV1) error {
	_, _, err := s.storeRuntimeArchiveIntentV1(ctx, identity, birth, document, nil)
	if err != nil {
		return errRuntimeArchive
	}
	return nil
}

// Exact-identity foundation calls preserve released private retry semantics.
// Only the coherent-capture production path deduplicates canonical content.
func (s *Service) storeRuntimeArchiveIntentV1(ctx context.Context, identity string, birth time.Time, document runtimeConfigurationV1, capture *runtimeConfigurationCaptureV1) (string, bool, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.db == nil {
		return "", false, errRuntimeArchive
	}
	raw, digest, err := encodeRuntimeConfigurationV1(ctx, document)
	if err != nil {
		return "", false, errRuntimeArchive
	}
	defer clear(raw)
	if capture != nil && (!runtimeArchiveCaptureProvenanceValidV1(capture) || !bytes.Equal(raw, capture.canonical) || digest != capture.contentDigest) {
		return "", false, errRuntimeArchive
	}
	header := runtimeArchiveHeaderV1(identity, birth, document, len(raw), digest)
	if !runtimeArchiveHeaderValid(header) {
		return "", false, errRuntimeArchive
	}
	epoch := s.secretEpoch()
	chunks := make([]entity.RuntimeConfigurationArchiveChunk, 0, header.ChunkCount)
	for ordinal, start := 0, 0; start < len(raw); ordinal, start = ordinal+1, start+runtimeArchiveChunkBytes {
		if ctx.Err() != nil {
			return "", false, errRuntimeArchive
		}
		end := min(start+runtimeArchiveChunkBytes, len(raw))
		c := entity.RuntimeConfigurationArchiveChunk{ID: fmt.Sprintf("%s-%03d", identity, ordinal), ArchiveID: identity, Ordinal: ordinal, PlaintextBytes: end - start, PlaintextDigest: runtimeArchiveByteDigest(raw[start:end])}
		c.SecretGeneration = runtimeArchiveChunkGeneration(header, c)
		reference, e := runtimeArchiveChunkReference(header, c)
		if e != nil {
			return "", false, errRuntimeArchive
		}
		c.AuthCiphertext, e = s.sealSecret(reference, string(raw[start:end]))
		if e != nil || len(c.AuthCiphertext) == 0 || len(c.AuthCiphertext) > runtimeArchiveEnvelopeBytes {
			return "", false, errRuntimeArchive
		}
		chunks = append(chunks, c)
	}
	storedID, deduplicated := identity, false
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var e error
		if capture == nil {
			var existing entity.RuntimeConfigurationArchive
			e = personalExact(tx, "id", identity).Take(&existing).Error
			if e == nil {
				if !runtimeArchiveHeadersEqual(header, existing) {
					return errRuntimeArchive
				}
				_, e = s.readRuntimeArchiveTx(ctx, tx, existing)
				return e
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
		}
		// Each guard takes the same singleton lock through commit. A cutover after
		// preparation rejects the entire write, without resealing stale intent.
		for _, c := range chunks {
			reference, e := runtimeArchiveChunkReference(header, c)
			if e != nil {
				return e
			}
			if e = s.guardSecretWrite(tx, epoch, reference, c.AuthCiphertext); e != nil {
				return e
			}
		}
		ledger, e := lockRuntimeArchiveRetention(tx)
		if e != nil {
			return e
		}
		if capture != nil {
			var target entity.RuntimeArchiveContent
			e = personalExact(tx, "content_digest", digest).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&target).Error
			if e == nil {
				if target.ContentDigest != digest || !runtimeArchiveID.MatchString(target.ArchiveID) {
					return errRuntimeArchive
				}
				var retained entity.RuntimeConfigurationArchive
				if e = personalExact(tx, "id", target.ArchiveID).Take(&retained).Error; e != nil || retained.ID != target.ArchiveID || retained.ContentDigest != digest {
					return errRuntimeArchive
				}
				facts, e := s.readRuntimeArchiveTx(ctx, tx, retained)
				if e != nil {
					return e
				}
				retainedRaw, retainedDigest, e := encodeRuntimeConfigurationV1(ctx, facts.Document)
				if e != nil {
					return e
				}
				defer clear(retainedRaw)
				if retainedDigest != digest || !bytes.Equal(retainedRaw, raw) {
					return errRuntimeArchive
				}
				storedID, deduplicated = retained.ID, true
				return nil
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
		}
		encryptedBytes := int64(0)
		for _, c := range chunks {
			encryptedBytes += int64(len(c.AuthCiphertext))
		}
		if e = runtimeArchiveRetentionAdmission(ledger, encryptedBytes); e != nil {
			return e
		}
		if e = tx.Create(&header).Error; e != nil {
			return e
		}
		if e = tx.Omit("Archive").CreateInBatches(chunks, 16).Error; e != nil {
			return e
		}
		target := entity.RuntimeArchiveContent{ContentDigest: digest, ArchiveID: identity}
		if e = tx.Omit("Archive").Clauses(clause.OnConflict{DoNothing: true}).Create(&target).Error; e != nil {
			return e
		}
		if capture != nil {
			provenance := entity.RuntimeArchiveCapture{ArchiveID: identity, SnapshotID: capture.snapshotID, RoutesPublishedAt: capture.routesPublishedAt.UTC().Truncate(time.Microsecond), CapturedAt: capture.capturedAt.UTC().Truncate(time.Microsecond)}
			if e = tx.Omit("Archive").Create(&provenance).Error; e != nil {
				return e
			}
		}
		return updateRuntimeArchiveRetention(tx, ledger.RetainedCount+1, ledger.EncryptedBytes+encryptedBytes)
	})
	if err != nil || ctx.Err() != nil {
		if err == nil {
			err = errRuntimeArchive
		}
		return "", false, err
	}
	return storedID, deduplicated, nil
}

// readRuntimeConfigurationArchiveV1 returns private detached facts only. There
// is no network read, historical secret recovery, authorization or publication.
func (s *Service) readRuntimeConfigurationArchiveV1(ctx context.Context, identity string) (*runtimeConfigurationFactsV1, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.db == nil || !runtimeArchiveID.MatchString(identity) {
		return nil, errRuntimeArchive
	}
	var facts *runtimeConfigurationFactsV1
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var header entity.RuntimeConfigurationArchive
		if e := personalExact(tx, "id", identity).Take(&header).Error; e != nil || header.ID != identity {
			return errRuntimeArchive
		}
		var e error
		facts, e = s.readRuntimeArchiveTx(ctx, tx, header)
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil || ctx.Err() != nil {
		return nil, errRuntimeArchive
	}
	return facts, nil
}

func (s *Service) readRuntimeArchiveTx(ctx context.Context, tx *gorm.DB, header entity.RuntimeConfigurationArchive) (*runtimeConfigurationFactsV1, error) {
	if !runtimeArchiveHeaderValid(header) {
		return nil, errRuntimeArchive
	}
	// Count every retained row before selecting bounded ciphertext. An extra
	// malformed envelope must not disappear behind the allocation predicate.
	var count int64
	if err := personalExact(tx.Model(&entity.RuntimeConfigurationArchiveChunk{}), "archive_id", header.ID).Count(&count).Error; err != nil || count != int64(header.ChunkCount) {
		return nil, errRuntimeArchive
	}
	var chunks []entity.RuntimeConfigurationArchiveChunk
	// Only size-bounded ciphertext is allocated. Both this set and the complete
	// metadata count belong to the same read transaction.
	query := personalExact(tx, "archive_id", header.ID).Where("OCTET_LENGTH(auth_ciphertext) BETWEEN ? AND ?", 1, runtimeArchiveEnvelopeBytes).Order("ordinal ASC").Limit(runtimeArchiveMaxChunks + 1)
	if err := query.Find(&chunks).Error; err != nil || len(chunks) != header.ChunkCount {
		return nil, errRuntimeArchive
	}
	return reconstructRuntimeArchiveChunksV1(ctx, header, chunks, s.openSecret)
}

func reconstructRuntimeArchiveChunksV1(ctx context.Context, header entity.RuntimeConfigurationArchive, chunks []entity.RuntimeConfigurationArchiveChunk, open func(string, string) (string, error)) (*runtimeConfigurationFactsV1, error) {
	if ctx == nil || ctx.Err() != nil || open == nil || !runtimeArchiveHeaderValid(header) || len(chunks) != header.ChunkCount {
		return nil, errRuntimeArchive
	}
	raw := make([]byte, 0, header.PlaintextBytes)
	defer clear(raw)
	for ordinal, c := range chunks {
		if ctx.Err() != nil || c.Ordinal != ordinal || len(c.AuthCiphertext) == 0 || len(c.AuthCiphertext) > runtimeArchiveEnvelopeBytes {
			return nil, errRuntimeArchive
		}
		reference, err := runtimeArchiveChunkReference(header, c)
		if err != nil {
			return nil, errRuntimeArchive
		}
		plaintext, err := open(reference, c.AuthCiphertext)
		if err != nil || len(plaintext) != c.PlaintextBytes || runtimeArchiveByteDigest([]byte(plaintext)) != c.PlaintextDigest {
			return nil, errRuntimeArchive
		}
		raw = append(raw, plaintext...)
	}
	if len(raw) != header.PlaintextBytes {
		return nil, errRuntimeArchive
	}
	facts, err := reconstructRuntimeConfigurationV1(ctx, raw, header.ContentDigest)
	if err != nil || facts == nil {
		return nil, errRuntimeArchive
	}
	expected := runtimeArchiveHeaderV1(header.ID, header.CreatedAt, facts.Document, len(raw), header.ContentDigest)
	if !runtimeArchiveHeadersEqual(header, expected) {
		return nil, errRuntimeArchive
	}
	return facts, nil
}
