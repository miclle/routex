package service

import (
	"context"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ProviderModelUpdate struct {
	Enabled            *bool
	SupportsImageInput *bool
	SupportsPDFInput   *bool
}

func (s *Service) SetProviderModelState(ctx context.Context, actor, modelID, etag string, enabled bool) (*entity.ProviderModel, error) {
	return s.UpdateProviderModel(ctx, actor, modelID, etag, ProviderModelUpdate{Enabled: &enabled})
}

func (s *Service) UpdateProviderModel(ctx context.Context, actor, modelID, etag string, input ProviderModelUpdate) (*entity.ProviderModel, error) {
	if etag == "" || len(etag) > 64 {
		return nil, apperrors.ErrBadRequest
	}
	if input.Enabled == nil && input.SupportsImageInput == nil && input.SupportsPDFInput == nil {
		return nil, apperrors.ErrBadRequest
	}
	var model entity.ProviderModel
	changed := false
	becameDisabled := false
	availabilityChanged := false
	capabilitiesChanged := false
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
		updates := map[string]any{}
		if input.Enabled != nil && model.Disabled != !*input.Enabled {
			model.Disabled = !*input.Enabled
			updates["disabled"] = model.Disabled
			becameDisabled = model.Disabled
			availabilityChanged = true
		}
		if input.SupportsImageInput != nil && model.SupportsImageInput != *input.SupportsImageInput {
			model.SupportsImageInput = *input.SupportsImageInput
			updates["supports_image_input"] = model.SupportsImageInput
			capabilitiesChanged = true
		}
		if input.SupportsPDFInput != nil && model.SupportsPDFInput != *input.SupportsPDFInput {
			model.SupportsPDFInput = *input.SupportsPDFInput
			updates["supports_pdf_input"] = model.SupportsPDFInput
			capabilitiesChanged = true
		}
		if len(updates) == 0 {
			return nil
		}
		revision, err := id.NewPrefixed("pms")
		if err != nil {
			return err
		}
		model.ETag = revision
		updates["e_tag"] = revision
		if err := tx.Model(&model).Updates(updates).Error; err != nil {
			return err
		}
		changed = true
		action := "provider_model.update"
		if availabilityChanged && !capabilitiesChanged {
			action = "provider_model.disable"
			if !model.Disabled {
				action = "provider_model.enable"
			}
		}
		return appendAudit(tx, actor, action, "provider_model", model.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if changed && becameDisabled && s.runtime != nil {
		s.runtime.deniedProviderModels.Store(modelID, s.runtime.epoch.Add(1))
	}
	return &model, s.RefreshRuntime(ctx)
}
