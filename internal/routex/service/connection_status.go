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

type ConnectionStatusInput struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}
type ConnectionStatusRecord struct {
	ConnectionMetadataRecord
	Enabled bool `json:"enabled"`
}
type ConnectionStatusWriteResult struct {
	Connection     ConnectionStatusRecord `json:"connection"`
	RuntimeApplied bool                   `json:"runtime_applied"`
	Changed        bool                   `json:"changed"`
}

func (input *ConnectionStatusInput) UnmarshalJSON(raw []byte) error {
	if len(raw) > 64*1024 || !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return apperrors.ErrBadRequest
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"enabled", "reason"})
	if err != nil {
		return err
	}
	var next ConnectionStatusInput
	if string(fields["enabled"]) == "null" || json.Unmarshal(fields["enabled"], &next.Enabled) != nil || string(fields["reason"]) == "null" || json.Unmarshal(fields["reason"], &next.Reason) != nil || !validConnectionStatusInput(next) {
		return apperrors.ErrBadRequest
	}
	*input = next
	return nil
}
func validConnectionStatusInput(input ConnectionStatusInput) bool {
	return validConnectionMetadataInput(ConnectionMetadataInput{Name: "status", Reason: input.Reason})
}
func connectionStatusRecord(metadata ConnectionMetadataRecord, row entity.ProviderConnection) ConnectionStatusRecord {
	record := ConnectionStatusRecord{ConnectionMetadataRecord: metadata, Enabled: row.Enabled}
	// Reuse the exact identity prefix; the review is purpose-scoped and binds all
	// shared metadata/egress revisions, without expanding their public wire.
	review := connectionMetadataHash(struct {
		Version string
		Record  ConnectionStatusRecord
	}{"connection.status.review.v1", record})
	record.ETag = metadata.ETag[:64] + "." + review
	return record
}
func connectionStatusReview(record ConnectionStatusRecord, etag string, enabled bool) error {
	if !validConnectionMetadataETag(etag) {
		return apperrors.ErrBadRequest
	}
	if record.ETag[:64] != etag[:64] || record.Enabled != enabled && record.ETag != etag {
		return catalogConflict
	}
	return nil
}
func (s *Service) GetConnectionStatus(ctx context.Context, actorID, connectionID string) (*ConnectionStatusRecord, error) {
	if err := connectionMetadataIDs(actorID, connectionID); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result ConnectionStatusRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		_, _, row, metadata, err := connectionMetadataSnapshot(tx, actorID, connectionID, false, false)
		if err != nil {
			return err
		}
		result = connectionStatusRecord(metadata, row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, connectionStatusError(err)
	}
	return &result, nil
}
func (s *Service) WriteConnectionStatus(ctx context.Context, actorID, connectionID, etag string, input ConnectionStatusInput) (*ConnectionStatusWriteResult, error) {
	if err := connectionMetadataIDs(actorID, connectionID); err != nil {
		return nil, err
	}
	if !validConnectionStatusInput(input) || !validConnectionMetadataETag(etag) {
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
		actor, _, row, metadata, err := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if err != nil {
			return err
		}
		record := connectionStatusRecord(metadata, row)
		if err := connectionStatusReview(record, etag, input.Enabled); err != nil {
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
		update := connectionMetadataQuery(tx, connectionID).Updates(map[string]any{"enabled": input.Enabled, "ETag": revision})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return connectionMetadataUnavailable
		}
		auditID, err := id.NewPrefixed("aud")
		if err != nil {
			return err
		}
		raw, err := json.Marshal(connectionStatusAudit{Before: row.Enabled, After: input.Enabled, Reason: input.Reason})
		if err != nil {
			return err
		}
		detail := string(raw)
		if err := tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor.ID, Action: "connection.status.update", ResourceType: "connection", ResourceID: row.ID, DetailsJSON: &detail}).Error; err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err == nil && !input.Enabled && s.runtime != nil {
		s.runtime.deniedConnections.Store(connectionID, s.runtime.epoch.Add(1))
	}
	release()
	if err != nil {
		return nil, connectionStatusError(err)
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, connectionMetadataUnavailable
	}
	var result ConnectionStatusWriteResult
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		_, _, row, metadata, err := connectionMetadataSnapshot(tx, actorID, connectionID, true, true)
		if err != nil {
			return err
		}
		record := connectionStatusRecord(metadata, row)
		if err := connectionStatusReview(record, etag, input.Enabled); err != nil {
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
		if !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return connectionMetadataUnavailable
		}
		result = ConnectionStatusWriteResult{Connection: record, RuntimeApplied: true, Changed: changed}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, connectionStatusError(err)
	}
	return &result, nil
}

type connectionStatusAudit struct {
	Before bool   `json:"before"`
	After  bool   `json:"after"`
	Reason string `json:"reason"`
}

func connectionStatusAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON != nil && (len(*row.DetailsJSON) > 64*1024 || !utf8.ValidString(*row.DetailsJSON) || !modelCreationUnicode([]byte(*row.DetailsJSON))) {
		return nil, false
	}
	if row.ResourceType != "connection" || connectionMetadataIDs(row.ActorID, row.ResourceID) != nil || row.DetailsJSON == nil {
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
	var detail connectionStatusAudit
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || detail.Before == detail.After || !validConnectionStatusInput(ConnectionStatusInput{Reason: detail.Reason}) {
		return nil, false
	}
	return detail, true
}

func connectionStatusError(err error) error {
	var app *apperrors.Error
	if errors.As(err, &app) {
		return app
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.ErrNotFound
	}
	return connectionMetadataUnavailable
}
