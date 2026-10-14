package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

var runtimeArchiveCleanupConflict = &apperrors.Error{Code: http.StatusConflict, Message: "archive cleanup review or intent conflicts"}
var runtimeArchiveCleanupProtected = &apperrors.Error{Code: http.StatusConflict, Message: "archive is protected by retained rollback history"}
var runtimeArchiveCleanupUnavailable = &apperrors.Error{Code: http.StatusServiceUnavailable, Message: "archive cleanup temporarily unavailable"}

type RuntimeArchiveCleanupSelection struct {
	ArchiveIDs []string `json:"archive_ids"`
}

type RuntimeArchiveCleanupInput struct {
	ArchiveIDs []string `json:"archive_ids"`
	RequestID  string   `json:"request_id"`
	Reason     string   `json:"reason"`
}

type RuntimeArchiveCleanupPreview struct {
	Scope                  string   `json:"scope"`
	ArchiveIDs             []string `json:"archive_ids"`
	RetainedCount          int64    `json:"retained_count"`
	EncryptedBytes         int64    `json:"encrypted_bytes"`
	SelectedCount          int64    `json:"selected_count"`
	SelectedEncryptedBytes int64    `json:"selected_encrypted_bytes"`
	VersionCap             int64    `json:"version_cap"`
	EncryptedByteCap       int64    `json:"encrypted_byte_cap"`
	ETag                   string   `json:"-"`
}

type RuntimeArchiveCleanupReceipt struct {
	Scope                 string    `json:"scope"`
	RequestID             string    `json:"request_id"`
	ArchiveIDs            []string  `json:"archive_ids"`
	DeletedCount          int64     `json:"deleted_count"`
	DeletedEncryptedBytes int64     `json:"deleted_encrypted_bytes"`
	CompletedAt           time.Time `json:"completed_at"`
}

type runtimeArchiveCleanupReview struct {
	ActorID    string
	ActorBirth time.Time
	Ledger     entity.RuntimeArchiveRetention
	Headers    []entity.RuntimeConfigurationArchive
	Chunks     []database.RuntimeArchiveChunkSummary
	Targets    []entity.RuntimeArchiveContent
}

func runtimeArchiveCleanupETag(review runtimeArchiveCleanupReview) string {
	// Entity JSON deliberately hides private facts. Build a non-secret review
	// projection explicitly rather than hashing the hidden entity serialization.
	headers := make([][]any, 0, len(review.Headers))
	for _, h := range review.Headers {
		headers = append(headers, []any{h.ID, h.CreatedAt.UTC(), h.CodecVersion, h.ContentDigest, h.SourceDigest, h.AuthorizationDigest, h.NetworkDigest, h.PricingDigest, h.PlaintextBytes, h.ChunkCount})
	}
	targets := make([][2]string, 0, len(review.Targets))
	for _, target := range review.Targets {
		targets = append(targets, [2]string{target.ContentDigest, target.ArchiveID})
	}
	return connectionMetadataHash([]any{"routex.archive-cleanup.v1", review.ActorID, review.ActorBirth.UTC(), review.Ledger.RetainedCount, review.Ledger.EncryptedBytes, headers, review.Chunks, targets})
}

func runtimeArchiveCleanupSelection(ids []string) ([]string, error) {
	if len(ids) < 1 || len(ids) > 20 {
		return nil, apperrors.ErrBadRequest
	}
	selected := slices.Clone(ids)
	slices.Sort(selected)
	for i, identity := range selected {
		if !runtimeArchiveID.MatchString(identity) || i > 0 && identity == selected[i-1] {
			return nil, apperrors.ErrBadRequest
		}
	}
	return selected, nil
}

func runtimeArchiveCleanupError(err error) error {
	for _, expected := range []error{apperrors.ErrBadRequest, apperrors.ErrUnauthorized, apperrors.ErrForbidden, apperrors.ErrNotFound, runtimeArchiveCleanupConflict, runtimeArchiveCleanupProtected} {
		if errors.Is(err, expected) {
			return expected
		}
	}
	return runtimeArchiveCleanupUnavailable
}

// Governance precedes the secret-policy and retention locks. Current Session
// and independent permissions are required even for an identical saved retry.
func (s *Service) runtimeArchiveCleanupActor(tx *gorm.DB, auth *Authentication) (entity.User, error) {
	if err := lockGovernance(tx); err != nil {
		return entity.User{}, err
	}
	actor, err := s.runtimePublicationActor(tx, auth)
	if err != nil {
		return actor, err
	}
	for _, permission := range []string{"system.read", "system.write"} {
		allowed, e := exactGovernancePermissionForAdmittedActor(tx, actor, permission)
		if e != nil {
			return actor, e
		}
		if !allowed {
			return actor, apperrors.ErrForbidden
		}
	}
	return actor, nil
}

func runtimeArchiveCleanupRead(tx *gorm.DB, actor entity.User, selected []string) (*RuntimeArchiveCleanupPreview, runtimeArchiveCleanupReview, error) {
	review := runtimeArchiveCleanupReview{ActorID: actor.ID, ActorBirth: actor.CreatedAt}
	// Serialize with every archive write and root-envelope accounting update.
	if _, err := lockedSecretPolicy(tx); err != nil {
		return nil, review, err
	}
	ledger, err := lockRuntimeArchiveRetention(tx)
	if err != nil {
		return nil, review, err
	}
	review.Ledger = ledger
	var protected int64
	if err := tx.Model(&entity.RuntimeArchiveRollback{}).Where("archive_id IN ?", selected).Count(&protected).Error; err != nil {
		return nil, review, err
	}
	// Both preview and confirmation hold the retention lock. The restrictive FK
	// is a final integrity guard, not the first user-visible protection signal.
	if protected != 0 {
		return nil, review, runtimeArchiveCleanupProtected
	}
	if err = tx.Where("id IN ?", selected).Order("id ASC").Find(&review.Headers).Error; err != nil {
		return nil, review, err
	}
	if len(review.Headers) != len(selected) {
		return nil, review, apperrors.ErrNotFound
	}
	for i, header := range review.Headers {
		if header.ID != selected[i] || !runtimeArchiveHeaderValid(header) {
			return nil, review, errRuntimeArchive
		}
	}
	review.Chunks, err = database.RuntimeArchiveChunkSummaries(tx, selected)
	if err != nil {
		return nil, review, err
	}
	slices.SortFunc(review.Chunks, func(a, b database.RuntimeArchiveChunkSummary) int {
		return bytes.Compare([]byte(a.ArchiveID), []byte(b.ArchiveID))
	})
	if len(review.Chunks) != len(selected) {
		return nil, review, errRuntimeArchive
	}
	var total int64
	for i, chunks := range review.Chunks {
		if chunks.ArchiveID != selected[i] || chunks.ChunkCount != int64(review.Headers[i].ChunkCount) || chunks.EncryptedBytes < 1 || chunks.EncryptedBytes > math.MaxInt64-total {
			return nil, review, errRuntimeArchive
		}
		total += chunks.EncryptedBytes
	}
	if ledger.RetainedCount < int64(len(selected)) || ledger.EncryptedBytes < total {
		return nil, review, errRuntimeArchive
	}
	if err = tx.Where("archive_id IN ?", selected).Order("content_digest ASC").Find(&review.Targets).Error; err != nil {
		return nil, review, err
	}
	for _, target := range review.Targets {
		index, found := slices.BinarySearch(selected, target.ArchiveID)
		if !found || target.ContentDigest != review.Headers[index].ContentDigest {
			return nil, review, errRuntimeArchive
		}
	}
	preview := &RuntimeArchiveCleanupPreview{Scope: "archive_storage_only", ArchiveIDs: slices.Clone(selected), RetainedCount: ledger.RetainedCount, EncryptedBytes: ledger.EncryptedBytes, SelectedCount: int64(len(selected)), SelectedEncryptedBytes: total, VersionCap: runtimeArchiveRetainedVersions, EncryptedByteCap: runtimeArchiveRetainedEncryptedBytes, ETag: runtimeArchiveCleanupETag(review)}
	return preview, review, nil
}

func (s *Service) PreviewRuntimeArchiveCleanup(ctx context.Context, auth *Authentication, input RuntimeArchiveCleanupSelection) (*RuntimeArchiveCleanupPreview, error) {
	selected, err := runtimeArchiveCleanupSelection(input.ArchiveIDs)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var preview *RuntimeArchiveCleanupPreview
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := s.runtimeArchiveCleanupActor(tx, auth)
		if e != nil {
			return e
		}
		preview, _, e = runtimeArchiveCleanupRead(tx, actor, selected)
		if e != nil {
			return e
		}
		// Archive reads may cross expiry while authority rows remain locked.
		// A review must still belong to a current Session when it is returned.
		_, e = s.runtimePublicationActor(tx, auth)
		return e
	})
	if err != nil {
		return nil, runtimeArchiveCleanupError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeArchiveCleanupUnavailable
	}
	return preview, nil
}

func runtimeArchiveCleanupReceipt(row entity.RuntimeArchiveCleanup, selected []string) (*RuntimeArchiveCleanupReceipt, error) {
	var ids []string
	if len(row.ArchiveIDsJSON) < 34 || len(row.ArchiveIDsJSON) > 661 {
		return nil, errRuntimeArchive
	}
	if json.Unmarshal([]byte(row.ArchiveIDsJSON), &ids) != nil || !slices.Equal(ids, selected) || row.DeletedCount != int64(len(selected)) || row.DeletedBytes < 1 || !runtimeInstallationMicrosecond(row.CompletedAt) || row.CompletedAt.Before(row.ActorCreatedAt) {
		return nil, errRuntimeArchive
	}
	return &RuntimeArchiveCleanupReceipt{Scope: "archive_storage_only", RequestID: row.RequestID, ArchiveIDs: slices.Clone(ids), DeletedCount: row.DeletedCount, DeletedEncryptedBytes: row.DeletedBytes, CompletedAt: row.CompletedAt}, nil
}

// Cleanup atomically records the exact intent, audit and actual-byte release.
// Retained rollback references are checked during review and confirmation;
// the restrictive V116 foreign key also prevents out-of-band deletion.
func (s *Service) CleanupRuntimeArchives(ctx context.Context, auth *Authentication, etag string, input RuntimeArchiveCleanupInput) (*RuntimeArchiveCleanupReceipt, error) {
	selected, err := runtimeArchiveCleanupSelection(input.ArchiveIDs)
	if err != nil || !runtimePublicationRequestID.MatchString(input.RequestID) || !runtimePublicationReason(input.Reason) || !runtimeRouteDigest.MatchString(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var receipt *RuntimeArchiveCleanupReceipt
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := s.runtimeArchiveCleanupActor(tx, auth)
		if e != nil {
			return e
		}
		var saved entity.RuntimeArchiveCleanup
		e = personalExact(tx, "request_id", input.RequestID).Take(&saved).Error
		if e == nil {
			if saved.RequestID != input.RequestID || saved.ActorID != actor.ID || !saved.ActorCreatedAt.Equal(actor.CreatedAt) || saved.ReviewETag != etag || saved.Reason != input.Reason {
				return runtimeArchiveCleanupConflict
			}
			receipt, e = runtimeArchiveCleanupReceipt(saved, selected)
			if e != nil {
				return e
			}
			// The same transaction already owns governance and Session locks;
			// recheck time-dependent expiry immediately before receipt commit.
			_, e = s.runtimePublicationActor(tx, auth)
			return e
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		preview, review, e := runtimeArchiveCleanupRead(tx, actor, selected)
		if e != nil {
			return e
		}
		if preview.ETag != etag {
			return runtimeArchiveCleanupConflict
		}
		if e = s.runtimeArchiveCleanupRebind(ctx, tx, selected, review); e != nil {
			return e
		}
		if e = tx.Where("archive_id IN ?", selected).Delete(&entity.RuntimeArchiveCapture{}).Error; e != nil {
			return e
		}
		deleted := tx.Where("archive_id IN ?", selected).Delete(&entity.RuntimeConfigurationArchiveChunk{})
		var expectedChunks int64
		for _, chunk := range review.Chunks {
			expectedChunks += chunk.ChunkCount
		}
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != expectedChunks {
			return errRuntimeArchive
		}
		deleted = tx.Where("id IN ?", selected).Delete(&entity.RuntimeConfigurationArchive{})
		if deleted.Error != nil {
			return deleted.Error
		}
		if deleted.RowsAffected != int64(len(selected)) {
			return errRuntimeArchive
		}
		if e = updateRuntimeArchiveRetention(tx, review.Ledger.RetainedCount-preview.SelectedCount, review.Ledger.EncryptedBytes-preview.SelectedEncryptedBytes); e != nil {
			return e
		}
		raw, _ := json.Marshal(selected)
		row := entity.RuntimeArchiveCleanup{RequestID: input.RequestID, ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt, ReviewETag: etag, Reason: input.Reason, ArchiveIDsJSON: string(raw), DeletedCount: preview.SelectedCount, DeletedBytes: preview.SelectedEncryptedBytes, CompletedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if e = tx.Create(&row).Error; e != nil {
			return e
		}
		auditID, e := id.NewPrefixed("aud")
		if e != nil {
			return e
		}
		details, _ := json.Marshal(struct {
			RequestID             string   `json:"request_id"`
			ArchiveIDs            []string `json:"archive_ids"`
			DeletedEncryptedBytes int64    `json:"deleted_encrypted_bytes"`
			Reason                string   `json:"reason"`
		}{row.RequestID, selected, row.DeletedBytes, row.Reason})
		text := string(details)
		// Audit resources use canonical 30-byte IDs. The UUID intent and full
		// multi-archive selection remain in the details and durable receipt.
		if e = tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor.ID, Action: "runtime.archive.cleanup", ResourceType: "runtime_configuration_archive", ResourceID: selected[0], DetailsJSON: &text}).Error; e != nil {
			return e
		}
		receipt, e = runtimeArchiveCleanupReceipt(row, selected)
		if e != nil {
			return e
		}
		// Reconstruction and audit may cross Session expiry even while all
		// authority rows remain locked. Expiry must roll back the whole cleanup.
		_, e = s.runtimePublicationActor(tx, auth)
		return e
	})
	if err != nil {
		return nil, runtimeArchiveCleanupError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeArchiveCleanupUnavailable
	}
	return receipt, nil
}

func (s *Service) runtimeArchiveCleanupRebind(ctx context.Context, tx *gorm.DB, selected []string, review runtimeArchiveCleanupReview) error {
	for _, target := range review.Targets {
		var successor entity.RuntimeConfigurationArchive
		err := personalExact(tx, "content_digest", target.ContentDigest).Where("id NOT IN ?", selected).Order("id ASC").Take(&successor).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err = personalExact(tx, "content_digest", target.ContentDigest).Delete(&entity.RuntimeArchiveContent{}).Error; err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		index, found := slices.BinarySearch(selected, target.ArchiveID)
		if !found || successor.ContentDigest != target.ContentDigest || !runtimeArchiveHeaderValid(successor) {
			return errRuntimeArchive
		}
		original, err := s.readRuntimeArchiveTx(ctx, tx, review.Headers[index])
		if err != nil {
			return err
		}
		remaining, err := s.readRuntimeArchiveTx(ctx, tx, successor)
		if err != nil {
			return err
		}
		if err = runtimeArchiveCleanupSameContent(ctx, original, remaining, target.ContentDigest); err != nil {
			return err
		}
		result := personalExact(tx.Model(&entity.RuntimeArchiveContent{}), "content_digest", target.ContentDigest).Update("archive_id", successor.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errRuntimeArchive
		}
	}
	return nil
}

func runtimeArchiveCleanupSameContent(ctx context.Context, original, remaining *runtimeConfigurationFactsV1, digest string) error {
	a, ad, err := encodeRuntimeConfigurationV1(ctx, original.Document)
	if err != nil {
		return err
	}
	defer clear(a)
	b, bd, err := encodeRuntimeConfigurationV1(ctx, remaining.Document)
	if err != nil {
		return err
	}
	defer clear(b)
	if ad != digest || bd != digest || !bytes.Equal(a, b) {
		return errRuntimeArchive
	}
	return nil
}
