package service

import (
	"context"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) SetProviderModelState(ctx context.Context, actor, modelID, etag string, enabled bool) (*entity.ProviderModel, error) {
	if etag == "" || len(etag) > 64 {
		return nil, apperrors.ErrBadRequest
	}
	var model entity.ProviderModel
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "providers.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model, "id = ?", modelID).Error; err != nil {
			return err
		}
		if model.ETag != etag {
			return catalogConflict
		}
		if model.Disabled == !enabled {
			return nil
		}
		revision, err := id.NewPrefixed("pms")
		if err != nil {
			return err
		}
		model.Disabled, model.ETag = !enabled, revision
		if err := tx.Model(&model).Select("Disabled", "ETag").Updates(model).Error; err != nil {
			return err
		}
		action := "provider_model.disable"
		if enabled {
			action = "provider_model.enable"
		}
		return appendAudit(tx, actor, action, "provider_model", model.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if !enabled && s.runtime != nil {
		s.runtime.deniedProviderModels.Store(modelID, s.runtime.epoch.Add(1))
	}
	return &model, s.RefreshRuntime(ctx)
}
