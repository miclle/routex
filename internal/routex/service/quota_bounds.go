package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
)

type ReservationBoundInput struct {
	MaxInputTokens  int64  `json:"max_input_tokens"`
	MaxOutputTokens int64  `json:"max_output_tokens"`
	Evidence        string `json:"evidence"`
	Reason          string `json:"reason"`
}
type ReservationBoundRecord struct {
	ProviderModelID  string    `json:"provider_model_id"`
	Protocol         string    `json:"protocol"`
	ETag             string    `json:"etag"`
	Revision         string    `json:"revision"`
	TransportCurrent bool      `json:"transport_current"`
	Configured       bool      `json:"configured"`
	MaxInputTokens   int64     `json:"max_input_tokens"`
	MaxOutputTokens  int64     `json:"max_output_tokens"`
	Evidence         string    `json:"evidence"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func readReservationBound(db *gorm.DB, modelID string) (entity.ReservationBound, error) {
	var model entity.ProviderModel
	if err := db.First(&model, "id = ?", modelID).Error; err != nil {
		return entity.ReservationBound{}, err
	}
	var connection entity.ProviderConnection
	if err := db.First(&connection, "id = ?", model.ConnectionID).Error; err != nil {
		return entity.ReservationBound{}, err
	}
	row := entity.ReservationBound{ProviderModelID: modelID, Protocol: connection.Protocol, ETag: "0"}
	err := db.First(&row, "provider_model_id = ?", modelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return row, err
}
func reservationBoundRecord(row entity.ReservationBound) *ReservationBoundRecord {
	return &ReservationBoundRecord{ProviderModelID: row.ProviderModelID, Protocol: row.Protocol, ETag: row.ETag, Configured: row.ETag != "0", MaxInputTokens: row.MaxInputTokens, MaxOutputTokens: row.MaxOutputTokens, Evidence: row.Evidence, UpdatedAt: row.UpdatedAt}
}
func reservationBoundReview(actor entity.User, p entity.Provider, c entity.ProviderConnection, pm entity.ProviderModel, row entity.ReservationBound) *ReservationBoundRecord {
	result := reservationBoundRecord(row)
	result.Revision = row.ETag
	result.TransportCurrent = row.ETag != "0" && capacityTransportCurrent(row, pm, c)
	result.ETag = connectionMetadataHash(struct {
		Version, CapabilityReview, Generation string
		Bound                                 entity.ReservationBound
	}{"capacity.transport.review.v1", capabilityReviewETag(actor, p, c, pm), row.TransportGeneration, row})
	return result
}
func (s *Service) GetReservationBound(ctx context.Context, actor, modelID string) (*ReservationBoundRecord, error) {
	var result *ReservationBoundRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		a, p, c, pm, e := transportSubject(tx, actor, modelID, "providers.read", false)
		if e != nil {
			return e
		}
		row, e := readReservationBound(tx, modelID)
		if e != nil {
			return e
		}
		result = reservationBoundReview(a, p, c, pm, row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, catalogError(err)
}
func (s *Service) WriteReservationBound(ctx context.Context, actor, modelID, etag string, input ReservationBoundInput) (*ReservationBoundRecord, error) {
	input.Evidence = strings.TrimSpace(input.Evidence)
	input.Reason = strings.TrimSpace(input.Reason)
	if !validMemberRoleDigest(etag) || input.MaxInputTokens <= 0 || input.MaxInputTokens > limits.MaxInteger || input.MaxOutputTokens <= 0 || input.MaxOutputTokens > limits.MaxInteger || len(input.Evidence) == 0 || len(input.Evidence) > 2000 || len(input.Reason) == 0 || len(input.Reason) > 2000 {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	release := s.pinPersonalKeyMutation()
	defer release()
	var target providerModelCommittedTarget
	var saved entity.ReservationBound
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		a, p, c, pm, e := transportSubject(tx, actor, modelID, "providers.write", true)
		if e != nil {
			return e
		}
		row, e := readReservationBound(tx, modelID)
		if e != nil {
			return e
		}
		before := reservationBoundReview(a, p, c, pm, row)
		if before.ETag != etag {
			return errLimitConflict
		}
		if !entity.SupportedNativeProtocol(c.Protocol) {
			return apperrors.ErrBadRequest
		}
		revision, e := id.NewPrefixed("bnd")
		if e != nil {
			return e
		}
		row.MaxInputTokens = input.MaxInputTokens
		row.MaxOutputTokens = input.MaxOutputTokens
		row.Evidence = input.Evidence
		row.PreviousETag = row.ETag
		row.ETag = revision
		row.ActorID = actor
		row.Reason = input.Reason
		row.Protocol = c.Protocol
		row.TransportGeneration = c.TransportGeneration
		if e := tx.Save(&row).Error; e != nil {
			return e
		}
		after := reservationBoundReview(a, p, c, pm, row)
		if e := appendQuotaAudit(tx, actor, "quota.bound.update", "provider_model", modelID, before, struct {
			Bound  *ReservationBoundRecord `json:"bound"`
			Reason string                  `json:"reason"`
		}{after, input.Reason}); e != nil {
			return e
		}
		saved = row
		target = providerModelCommittedTarget{a, p, c, pm}
		return nil
	})
	if (err == nil || target.Model.ID != "") && s.runtime != nil {
		s.runtime.deniedProviderModels.Store(modelID, s.runtime.epoch.Add(1))
	}
	release()
	if err != nil {
		if target.Model.ID != "" {
			return nil, connectionMetadataUnavailable
		}
		return nil, catalogError(err)
	}
	if s.RefreshRuntime(ctx) != nil {
		return nil, connectionMetadataUnavailable
	}
	var result *ReservationBoundRecord
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if e := lockGovernance(tx); e != nil {
			return e
		}
		a, p, c, pm, e := transportSubject(tx, actor, modelID, "providers.write", true)
		if e != nil {
			return e
		}
		if !target.matches(a, p, c, pm) {
			return connectionMetadataUnavailable
		}
		row, e := readReservationBound(tx, modelID)
		if e != nil {
			return e
		}
		if row.ETag != saved.ETag || row.TransportGeneration != saved.TransportGeneration || row.Protocol != saved.Protocol || row.ActorID != saved.ActorID || row.Reason != saved.Reason || row.Evidence != saved.Evidence || row.MaxInputTokens != saved.MaxInputTokens || row.MaxOutputTokens != saved.MaxOutputTokens {
			return connectionMetadataUnavailable
		}
		data, e := s.loadRuntimeDataTx(tx)
		if e != nil {
			return e
		}
		digest, e := runtimeDigest(data)
		if e != nil {
			return e
		}
		if !s.connectionMetadataRuntimeApplied(digest, data.EgressGeneration) {
			return connectionMetadataUnavailable
		}
		result = reservationBoundReview(a, p, c, pm, row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, connectionMetadataUnavailable
	}
	return result, nil
}
func appendQuotaAudit(tx *gorm.DB, actor, action, resource, resourceID string, before, after any) error {
	raw, err := json.Marshal(struct {
		Before any `json:"before"`
		After  any `json:"after"`
	}{before, after})
	if err != nil || len(raw) > 12<<10 {
		return apperrors.ErrInternal
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	encoded := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: action, ResourceType: resource, ResourceID: resourceID, DetailsJSON: &encoded}).Error
}
