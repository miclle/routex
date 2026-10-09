package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

type providerMetadataValues struct {
	Name string `json:"name"`
}
type providerMetadataAudit struct {
	Before providerMetadataValues `json:"before"`
	After  providerMetadataValues `json:"after"`
	Reason string                 `json:"reason"`
}

func appendProviderMetadataAudit(tx *gorm.DB, actorID, providerID, before string, input ProviderMetadataInput) error {
	raw, err := json.Marshal(providerMetadataAudit{providerMetadataValues{before}, providerMetadataValues{input.Name}, input.Reason})
	if err != nil {
		return apperrors.ErrInternal
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	text := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "provider.metadata.update", ResourceType: "provider", ResourceID: providerID, DetailsJSON: &text}).Error
}
func (s *Service) WriteProviderMetadata(ctx context.Context, actorID, providerID, etag string, input ProviderMetadataInput) (*ProviderMetadataWriteResult, error) {
	if err := providerMetadataIDs(actorID, providerID); err != nil {
		return nil, err
	}
	if !validProviderMetadataInput(input) || !validConnectionMetadataETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, row, record, err := providerMetadataSnapshot(tx, actorID, providerID, true, true)
		if err != nil {
			return err
		}
		if err := providerMetadataReview(record, etag, input); err != nil {
			return err
		}
		if row.Name == input.Name {
			return nil
		}
		revision, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		result := providerMetadataQuery(tx, providerID).Updates(map[string]any{"Name": input.Name, "ETag": revision})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return providerMetadataUnavailable
		}
		if err := appendProviderMetadataAudit(tx, actor.ID, row.ID, row.Name, input); err != nil {
			return err
		}
		changed = true
		return nil
	})
	release()
	if err != nil {
		return nil, catalogError(err)
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, providerMetadataUnavailable
	}
	var result ProviderMetadataWriteResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		_, _, record, err := providerMetadataSnapshot(tx, actorID, providerID, true, true)
		if err != nil {
			return err
		}
		if err := providerMetadataReview(record, etag, input); err != nil {
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
		if err != nil || !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return providerMetadataUnavailable
		}
		result = ProviderMetadataWriteResult{record, true, changed}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, providerMetadataReadError(err)
	}
	return &result, nil
}
func providerMetadataAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.ResourceType != "provider" || providerMetadataIDs(row.ActorID, row.ResourceID) != nil || row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 || !utf8.ValidString(*row.DetailsJSON) || !modelCreationUnicode([]byte(*row.DetailsJSON)) {
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
	var detail providerMetadataAudit
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || !validCatalogLabel(detail.Before.Name) || !validProviderMetadataInput(ProviderMetadataInput{detail.After.Name, detail.Reason}) || detail.Before.Name == detail.After.Name {
		return nil, false
	}
	return detail, true
}
