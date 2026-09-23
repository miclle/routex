package service

import (
	"context"
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
	ProviderModelID string    `json:"provider_model_id"`
	Protocol        string    `json:"protocol"`
	ETag            string    `json:"etag"`
	Configured      bool      `json:"configured"`
	MaxInputTokens  int64     `json:"max_input_tokens"`
	MaxOutputTokens int64     `json:"max_output_tokens"`
	Evidence        string    `json:"evidence"`
	UpdatedAt       time.Time `json:"updated_at"`
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
func (s *Service) GetReservationBound(ctx context.Context, actor, modelID string) (*ReservationBoundRecord, error) {
	if err := authorizeGovernance(s.authDB(ctx), actor, "providers.read"); err != nil {
		return nil, catalogError(err)
	}
	row, err := readReservationBound(s.authDB(ctx), modelID)
	if err != nil {
		return nil, catalogError(err)
	}
	return reservationBoundRecord(row), nil
}
func (s *Service) WriteReservationBound(ctx context.Context, actor, modelID, etag string, input ReservationBoundInput) (*ReservationBoundRecord, error) {
	input.Evidence = strings.TrimSpace(input.Evidence)
	input.Reason = strings.TrimSpace(input.Reason)
	if etag == "" || input.MaxInputTokens <= 0 || input.MaxInputTokens > limits.MaxInteger || input.MaxOutputTokens <= 0 || input.MaxOutputTokens > limits.MaxInteger || len(input.Evidence) == 0 || len(input.Evidence) > 2000 || len(input.Reason) == 0 || len(input.Reason) > 2000 {
		return nil, apperrors.ErrBadRequest
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	var result *ReservationBoundRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "providers.write"); err != nil {
			return err
		}
		row, err := readReservationBound(tx, modelID)
		if err != nil {
			return err
		}
		if row.ETag != etag {
			if row.PreviousETag == etag && row.ActorID == actor && row.Reason == input.Reason && row.Evidence == input.Evidence && row.MaxInputTokens == input.MaxInputTokens && row.MaxOutputTokens == input.MaxOutputTokens {
				result = reservationBoundRecord(row)
				return nil
			}
			return errLimitConflict
		}
		if !entity.SupportedNativeProtocol(row.Protocol) {
			return apperrors.ErrBadRequest
		}
		before := reservationBoundRecord(row)
		revision, err := id.NewPrefixed("bnd")
		if err != nil {
			return err
		}
		row.MaxInputTokens = input.MaxInputTokens
		row.MaxOutputTokens = input.MaxOutputTokens
		row.Evidence = input.Evidence
		row.PreviousETag = etag
		row.ETag = revision
		row.ActorID = actor
		row.Reason = input.Reason
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		result = reservationBoundRecord(row)
		return appendQuotaAudit(tx, actor, "quota.bound.update", "provider_model", modelID, before, struct {
			Bound  *ReservationBoundRecord `json:"bound"`
			Reason string                  `json:"reason"`
		}{result, input.Reason})
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if s.runtime != nil {
		s.runtime.deniedProviderModels.Store(modelID, s.runtime.epoch.Add(1))
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
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
