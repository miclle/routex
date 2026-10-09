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

type ProviderStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}
type ProviderStatusRecord struct {
	ProviderMetadataRecord
	Enabled bool `json:"enabled"`
}
type ProviderStatusWriteResult struct {
	Provider       ProviderStatusRecord `json:"provider"`
	RuntimeApplied bool                 `json:"runtime_applied"`
	Changed        bool                 `json:"changed"`
}

func (input *ProviderStatusInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var next ProviderStatusInput
	if string(fields["enabled"]) == "null" || json.Unmarshal(fields["enabled"], &next.Enabled) != nil || string(fields["reason"]) == "null" || json.Unmarshal(fields["reason"], &next.Reason) != nil || !validProviderStatusInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func validProviderStatusInput(input ProviderStatusInput) bool {
	return validProviderMetadataInput(ProviderMetadataInput{Name: "status", Reason: input.Reason})
}
func providerStatusRecord(metadata ProviderMetadataRecord, row entity.Provider) ProviderStatusRecord {
	record := ProviderStatusRecord{ProviderMetadataRecord: metadata, Enabled: row.Enabled}
	// Reuse the exact identity prefix; the review is purpose-scoped and binds all
	// shared metadata/status revision, without expanding their public wire.
	review := connectionMetadataHash(struct {
		Version string
		Record  ProviderStatusRecord
	}{"provider.status.review.v1", record})
	record.ETag = metadata.ETag[:64] + "." + review
	return record
}
func providerStatusReview(record ProviderStatusRecord, etag string, enabled bool) error {
	if !validConnectionMetadataETag(etag) {
		return apperrors.ErrBadRequest
	}
	if record.ETag[:64] != etag[:64] || record.Enabled != enabled && record.ETag != etag {
		return catalogConflict
	}
	return nil
}
func (s *Service) GetProviderStatus(ctx context.Context, actorID, providerID string) (*ProviderStatusRecord, error) {
	if err := providerMetadataIDs(actorID, providerID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result ProviderStatusRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, row, metadata, err := providerMetadataSnapshot(tx, actorID, providerID, false, false)
		if err != nil {
			return err
		}
		result = providerStatusRecord(metadata, row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, providerStatusError(err)
	}
	return &result, nil
}
func (s *Service) WriteProviderStatus(ctx context.Context, actorID, providerID, etag string, input ProviderStatusInput) (*ProviderStatusWriteResult, error) {
	if err := providerMetadataIDs(actorID, providerID); err != nil {
		return nil, err
	}
	if !validProviderStatusInput(input) || !validConnectionMetadataETag(etag) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Serialize both directions with runtime publication and local dispatch
	// admission. No remote verification/discovery/inference runs under this gate.
	release := s.pinPersonalKeyMutation()
	defer release()
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		actor, row, metadata, err := providerMetadataSnapshot(tx, actorID, providerID, true, true)
		if err != nil {
			return err
		}
		record := providerStatusRecord(metadata, row)
		if err := providerStatusReview(record, etag, input.Enabled); err != nil {
			return err
		}
		if row.Enabled == input.Enabled {
			return nil
		}
		revision, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		// Map updates deliberately persist false, rather than GORM struct zero omission.
		update := providerMetadataQuery(tx, providerID).Updates(map[string]any{"enabled": input.Enabled, "ETag": revision})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return providerMetadataUnavailable
		}
		auditID, err := id.NewPrefixed("aud")
		if err != nil {
			return err
		}
		raw, err := json.Marshal(providerStatusAudit{Before: row.Enabled, After: input.Enabled, Reason: input.Reason})
		if err != nil {
			return err
		}
		detail := string(raw)
		if err := tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor.ID, Action: "provider.status.update", ResourceType: "provider", ResourceID: row.ID, DetailsJSON: &detail}).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err == nil && !input.Enabled && s.runtime != nil {
		s.runtime.deniedProviders.Store(providerID, s.runtime.epoch.Add(1))
	}
	release()
	if err != nil {
		return nil, providerStatusError(err)
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, providerMetadataUnavailable
	}
	var result ProviderStatusWriteResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		_, row, metadata, err := providerMetadataSnapshot(tx, actorID, providerID, true, true)
		if err != nil {
			return err
		}
		record := providerStatusRecord(metadata, row)
		if err := providerStatusReview(record, etag, input.Enabled); err != nil {
			return err
		}
		if row.Enabled != input.Enabled {
			return catalogConflict
		}
		data, err := s.loadRuntimeDataTx(tx)
		if err != nil {
			return err
		}
		digest, err := runtimeDigest(data)
		if err != nil {
			return err
		}
		if !s.providerStatusRuntimeApplied(digest, data.EgressGeneration, row) {
			return providerMetadataUnavailable
		}
		result = ProviderStatusWriteResult{Provider: record, RuntimeApplied: true, Changed: changed}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		// The status transaction already committed. A later denial or conflicting
		// current state cannot prove that the original intent had no effect.
		return nil, providerMetadataUnavailable
	}
	return &result, nil
}

type providerStatusAudit struct {
	Before bool   `json:"before"`
	After  bool   `json:"after"`
	Reason string `json:"reason"`
}

func providerStatusAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON != nil && (len(*row.DetailsJSON) > 64*1024 || !utf8.ValidString(*row.DetailsJSON) || !modelCreationUnicode([]byte(*row.DetailsJSON))) {
		return nil, false
	}
	if row.ResourceType != "provider" || providerMetadataIDs(row.ActorID, row.ResourceID) != nil || row.DetailsJSON == nil {
		return nil, false
	}
	fields, err := decodeDefaultLimitObject([]byte(*row.DetailsJSON), []string{"before", "after", "reason"})
	if err != nil {
		return nil, false
	}
	for _, key := range []string{"before", "after"} {
		if string(fields[key]) != "true" && string(fields[key]) != "false" {
			return nil, false
		}
	}
	var detail providerStatusAudit
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || detail.Before == detail.After || !validProviderStatusInput(ProviderStatusInput{Reason: detail.Reason}) {
		return nil, false
	}
	return detail, true
}

func providerStatusError(err error) error {
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return providerMetadataUnavailable
}
