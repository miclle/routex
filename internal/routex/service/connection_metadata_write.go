package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type connectionMetadataValues struct {
	Name string `json:"name"`
}
type connectionMetadataAudit struct {
	Before connectionMetadataValues `json:"before"`
	After  connectionMetadataValues `json:"after"`
	Reason string                   `json:"reason"`
}

func appendConnectionMetadataAudit(tx *gorm.DB, actorID, connectionID, before string, input ConnectionMetadataInput) error {
	raw, err := json.Marshal(connectionMetadataAudit{connectionMetadataValues{before}, connectionMetadataValues{input.Name}, input.Reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	text := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "connection.metadata.update", ResourceType: "connection", ResourceID: connectionID, DetailsJSON: &text}).Error
}
func (s *Service) WriteConnectionMetadata(ctx context.Context, actorID, connectionID, etag string, input ConnectionMetadataInput) (*ConnectionMetadataWriteResult, error) {
	if err := connectionMetadataIDs(actorID, connectionID); err != nil {
		return nil, err
	}
	if !validConnectionMetadataInput(input) || !validConnectionMetadataETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, _, row, record, err := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if err != nil {
			return err
		}
		if err := connectionMetadataReview(record, etag, input); err != nil {
			return err
		}
		// This is current-value reconciliation, not an operation receipt. Identity
		// and independent current write authority were required even for equality.
		if row.Name == input.Name {
			return nil
		}
		revision, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		result := connectionMetadataQuery(tx, connectionID).Updates(map[string]any{"name": input.Name, "ETag": revision})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return connectionMetadataUnavailable
		}
		if err := appendConnectionMetadataAudit(tx, actor.ID, row.ID, row.Name, input); err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, connectionMetadataUnavailable
	}
	var result ConnectionMetadataWriteResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		_, _, _, record, err := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if err != nil {
			return err
		}
		if err := connectionMetadataReview(record, etag, input); err != nil {
			return err
		}
		if record.Name != input.Name {
			return catalogConflict
		}
		data, err := s.loadRuntimeDataTx(tx)
		if err != nil {
			return err
		}
		digest, err := runtimeDigest(data)
		if err != nil {
			return connectionMetadataUnavailable
		}
		if !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return connectionMetadataUnavailable
		}
		result = ConnectionMetadataWriteResult{Connection: record, RuntimeApplied: true, Changed: changed}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		var app *apperrors.Error
		if errors.As(err, &app) {
			return nil, app
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apperrors.ErrNotFound
		}
		return nil, connectionMetadataUnavailable
	}
	return &result, nil
}

// The complete coherent source digest includes names and raw revisions even for
// Connections without routes or with disabled egress. Egress route eligibility
// maps cannot prove this configuration, and no new authorization map is needed.
func (s *Service) connectionMetadataRuntimeApplied(digest string, egressGeneration uint64) bool {
	runtime := s.runtime
	if runtime == nil || runtime.done == nil || digest == "" || !runtime.publication.TryRLock() {
		return false
	}
	defer runtime.publication.RUnlock()
	if !runtime.mu.TryLock() {
		return false
	}
	defer runtime.mu.Unlock()
	select {
	case <-runtime.done:
		return false
	default:
	}
	auth, routes := runtime.auth.Load(), runtime.routes.Load()
	return auth != nil && routes != nil && time.Now().Before(auth.ValidUntil) && auth.SourceDigest == digest && routes.Digest == digest && s.egressGeneration.Load() == egressGeneration
}
func connectionMetadataAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.ResourceType != "connection" || connectionMetadataIDs(row.ActorID, row.ResourceID) != nil || row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 || !utf8.ValidString(*row.DetailsJSON) || !modelCreationUnicode([]byte(*row.DetailsJSON)) {
		return nil, false
	}
	fields, err := decodeDefaultLimitObject([]byte(*row.DetailsJSON), []string{"before", "after", "reason"})
	if err != nil {
		return nil, false
	}
	for _, key := range []string{"before", "after"} {
		if _, err := decodeDefaultLimitObject(fields[key], []string{"name"}); err != nil {
			return nil, false
		}
	}
	var detail connectionMetadataAudit
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || !validCatalogLabel(detail.Before.Name) || !validConnectionMetadataInput(ConnectionMetadataInput{detail.After.Name, detail.Reason}) || detail.Before.Name == detail.After.Name {
		return nil, false
	}
	return detail, true
}
