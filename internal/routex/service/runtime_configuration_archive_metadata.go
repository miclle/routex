package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

type RuntimeArchiveMetadataFilter struct {
	Cursor string
	Limit  int
}
type RuntimeArchiveMetadataRecord struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	CodecVersion  int       `json:"codec_version"`
	ContentDigest string    `json:"content_digest"`
	DocumentBytes int       `json:"document_bytes"`
}
type RuntimeArchiveMetadataPage struct {
	Scope      string                         `json:"scope"`
	ObservedAt time.Time                      `json:"observed_at"`
	Items      []RuntimeArchiveMetadataRecord `json:"items"`
	NextCursor *string                        `json:"next_cursor"`
}

func runtimeArchiveMetadataCursor(actorID, last string) string {
	scope := personalHash([]string{"routex.runtime-archive-metadata.v1", actorID})
	return base64.RawURLEncoding.EncodeToString([]byte(last + "|" + scope))
}
func validateRuntimeArchiveMetadataFilter(actorID string, f RuntimeArchiveMetadataFilter) (RuntimeArchiveMetadataFilter, string, error) {
	if f.Limit == 0 {
		f.Limit = 20
	}
	if f.Limit < 1 || f.Limit > 100 || len(f.Cursor) > 200 {
		return f, "", apperrors.ErrBadRequest
	}
	last := ""
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		parts := strings.Split(string(raw), "|")
		if err != nil || len(parts) != 2 || !runtimeArchiveID.MatchString(parts[0]) || f.Cursor != runtimeArchiveMetadataCursor(actorID, parts[0]) {
			return f, "", apperrors.ErrBadRequest
		}
		last = parts[0]
	}
	return f, last, nil
}

// ListRuntimeArchiveMetadata reads retained headers only. Metadata does not
// authenticate chunks, reconstruct source or establish publication/activation.
func (s *Service) ListRuntimeArchiveMetadata(ctx context.Context, actorID string, f RuntimeArchiveMetadataFilter) (*RuntimeArchiveMetadataPage, error) {
	f, last, err := validateRuntimeArchiveMetadataFilter(actorID, f)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	page := &RuntimeArchiveMetadataPage{Scope: "archive_metadata_only", ObservedAt: time.Now().UTC(), Items: []RuntimeArchiveMetadataRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		actor, e := exactEnabledActor(tx, actorID)
		if e != nil {
			return e
		}
		allowed, e := exactGovernancePermissionForAdmittedActor(tx, actor, "system.read")
		if e != nil {
			return e
		}
		if !allowed {
			return apperrors.ErrForbidden
		}
		var rows []entity.RuntimeConfigurationArchive
		q := tx.Model(&entity.RuntimeConfigurationArchive{})
		if last != "" {
			q = q.Where("id < ?", last)
		}
		if e = q.Order("id DESC").Limit(f.Limit + 1).Find(&rows).Error; e != nil {
			return e
		}
		prior := last
		for _, row := range rows {
			if !runtimeArchiveHeaderValid(row) || prior != "" && row.ID >= prior {
				return runtimeUnavailable
			}
			prior = row.ID
		}
		if len(rows) > f.Limit {
			cursor := runtimeArchiveMetadataCursor(actorID, rows[f.Limit-1].ID)
			page.NextCursor = &cursor
			rows = rows[:f.Limit]
		}
		for _, row := range rows {
			page.Items = append(page.Items, RuntimeArchiveMetadataRecord{ID: row.ID, CreatedAt: row.CreatedAt.UTC(), CodecVersion: row.CodecVersion, ContentDigest: row.ContentDigest, DocumentBytes: row.PlaintextBytes})
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	if ctx.Err() != nil {
		return nil, runtimeUnavailable
	}
	return page, nil
}
