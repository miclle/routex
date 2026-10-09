package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type ProviderModelUpdate struct {
	Enabled              *bool
	SupportsImageInput   *bool
	SupportsPDFInput     *bool
	CapabilityReviewETag *string
}

func validProviderModelUpdate(input ProviderModelUpdate) bool {
	capability := input.SupportsImageInput != nil || input.SupportsPDFInput != nil || input.CapabilityReviewETag != nil
	if capability {
		return input.SupportsImageInput != nil && input.SupportsPDFInput != nil && input.CapabilityReviewETag != nil && validMemberRoleDigest(*input.CapabilityReviewETag)
	}
	return input.Enabled != nil
}
func (s *Service) SetProviderModelState(ctx context.Context, actor, modelID, etag string, enabled bool) (*entity.ProviderModel, error) {
	return s.UpdateProviderModel(ctx, actor, modelID, etag, ProviderModelUpdate{Enabled: &enabled})
}

type providerModelCommittedTarget struct {
	Actor      entity.User
	Provider   entity.Provider
	Connection entity.ProviderConnection
	Model      entity.ProviderModel
}

func (t providerModelCommittedTarget) matches(a entity.User, p entity.Provider, c entity.ProviderConnection, pm entity.ProviderModel) bool {
	return t.Actor.ID == a.ID && t.Actor.CreatedAt.Equal(a.CreatedAt) && t.Provider.ID == p.ID && t.Provider.CreatedAt.Equal(p.CreatedAt) && t.Connection.ID == c.ID && t.Connection.ProviderID == c.ProviderID && t.Connection.CreatedAt.Equal(c.CreatedAt) && t.Connection.TransportGeneration == c.TransportGeneration && sameConnectionTransport(connectionTransportTuple(t.Connection), connectionTransportTuple(c)) && t.Model.ID == pm.ID && t.Model.ConnectionID == pm.ConnectionID && t.Model.CreatedAt.Equal(pm.CreatedAt) && t.Model.ETag == pm.ETag && t.Model.Disabled == pm.Disabled && t.Model.SupportsImageInput == pm.SupportsImageInput && t.Model.SupportsPDFInput == pm.SupportsPDFInput && t.Model.CapabilityTransportGeneration == pm.CapabilityTransportGeneration
}
func (s *Service) UpdateProviderModel(ctx context.Context, actor, modelID, etag string, input ProviderModelUpdate) (*entity.ProviderModel, error) {
	if etag == "" || len(etag) > 64 || !validProviderModelUpdate(input) {
		return nil, apperrors.ErrBadRequest
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release := s.pinPersonalKeyMutation()
	defer release()
	var target providerModelCommittedTarget
	changed := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		a, p, c, pm, err := transportSubject(tx, actor, modelID, "providers.write", true)
		if err != nil {
			return err
		}
		if pm.ETag != etag {
			return catalogConflict
		}
		before := pm
		updates := map[string]any{}
		if input.CapabilityReviewETag != nil {
			if capabilityReviewETag(a, p, c, pm) != *input.CapabilityReviewETag {
				return catalogConflict
			}
			if pm.SupportsImageInput != *input.SupportsImageInput || pm.SupportsPDFInput != *input.SupportsPDFInput || pm.CapabilityTransportGeneration != c.TransportGeneration {
				pm.SupportsImageInput = *input.SupportsImageInput
				pm.SupportsPDFInput = *input.SupportsPDFInput
				pm.CapabilityTransportGeneration = c.TransportGeneration
				updates["supports_image_input"] = pm.SupportsImageInput
				updates["supports_pdf_input"] = pm.SupportsPDFInput
				updates["capability_transport_generation"] = c.TransportGeneration
			}
		}
		if input.Enabled != nil && pm.Disabled != !*input.Enabled {
			pm.Disabled = !*input.Enabled
			updates["disabled"] = pm.Disabled
		}
		if len(updates) > 0 {
			revision, err := id.NewPrefixed("pms")
			if err != nil {
				return err
			}
			pm.ETag = revision
			updates["e_tag"] = revision
			r := memberRolesExact(memberRolesDB(tx).Model(&entity.ProviderModel{}), "id", modelID).Updates(updates)
			if r.Error != nil {
				return r.Error
			}
			if r.RowsAffected != 1 {
				return connectionMetadataUnavailable
			}
			if input.CapabilityReviewETag != nil {
				if err := appendCapabilityTransportAudit(tx, actor, before, pm); err != nil {
					return err
				}
			} else {
				action := "provider_model.enable"
				if pm.Disabled {
					action = "provider_model.disable"
				}
				if err := appendAudit(tx, actor, action, "provider_model", modelID); err != nil {
					return err
				}
			}
			changed = true
		}
		target = providerModelCommittedTarget{a, p, c, pm}
		return nil
	})
	if (err == nil || target.Model.ID != "") && changed && s.runtime != nil {
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
	var result entity.ProviderModel
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		a, p, c, pm, err := transportSubject(tx, actor, modelID, "providers.write", true)
		if err != nil {
			return err
		}
		if !target.matches(a, p, c, pm) {
			return connectionMetadataUnavailable
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
		pm.CapabilitiesTransportCurrent = capabilityTransportCurrent(pm, c)
		pm.CapabilityReviewETag = capabilityReviewETag(a, p, c, pm)
		result = pm
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, connectionMetadataUnavailable
	}
	return &result, nil
}
