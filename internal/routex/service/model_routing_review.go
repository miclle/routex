package service

import (
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// ModelRoutingCandidateReview keeps the candidate proof and configured routing
// preview in one read snapshot. It exposes neither grants nor private credentials.
type ModelRoutingCandidateReview struct {
	ModelID   string                        `json:"model_id"`
	ModelName string                        `json:"model_name"`
	Candidate ModelRoutingCandidate         `json:"candidate"`
	Routes    []ModelRoutingConfiguredRoute `json:"routes"`
}

type ModelRoutingConfiguredRoute struct {
	ID              string `json:"id"`
	ProviderID      string `json:"provider_id"`
	ProviderName    string `json:"provider_name"`
	ConnectionID    string `json:"connection_id"`
	ConnectionName  string `json:"connection_name"`
	ProviderModelID string `json:"provider_model_id"`
	UpstreamName    string `json:"upstream_name"`
	Protocol        string `json:"protocol"`
	Weight          int    `json:"weight"`
}

func captureModelRoutingReview(tx *gorm.DB, model entity.Model, bindings []entity.ModelProviderBinding, review *ModelRoutingCandidateReview) error {
	var name entity.ModelName
	if err := personalExact(personalExact(tx, "model_id", model.ID), "current_model_id", model.ID).Take(&name).Error; err != nil {
		return err
	}
	if name.ModelID != model.ID || name.CurrentModelID == nil || *name.CurrentModelID != model.ID {
		return apperrors.ErrNotFound
	}
	pmIDs := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		pmIDs = append(pmIDs, binding.ProviderModelID)
	}
	pms := []entity.ProviderModel{}
	connections := []entity.ProviderConnection{}
	providers := []entity.Provider{}
	if len(pmIDs) > 0 {
		if err := tx.Where(memberModelsExactIDs(tx, "id", pmIDs)).Limit(201).Find(&pms).Error; err != nil {
			return err
		}
		conIDs := make([]string, 0, len(pms))
		for _, pm := range pms {
			conIDs = append(conIDs, pm.ConnectionID)
		}
		if len(conIDs) > 0 {
			if err := tx.Where(memberModelsExactIDs(tx, "id", conIDs)).Limit(201).Find(&connections).Error; err != nil {
				return err
			}
		}
		providerIDs := make([]string, 0, len(connections))
		for _, connection := range connections {
			providerIDs = append(providerIDs, connection.ProviderID)
		}
		if len(providerIDs) > 0 {
			if err := tx.Where(memberModelsExactIDs(tx, "id", providerIDs)).Limit(201).Find(&providers).Error; err != nil {
				return err
			}
		}
	}
	pmByID := map[string]entity.ProviderModel{}
	conByID := map[string]entity.ProviderConnection{}
	providerByID := map[string]entity.Provider{}
	for _, pm := range pms {
		pmByID[pm.ID] = pm
	}
	for _, connection := range connections {
		conByID[connection.ID] = connection
	}
	for _, provider := range providers {
		providerByID[provider.ID] = provider
	}
	review.ModelID, review.ModelName = model.ID, name.Name
	review.Routes = make([]ModelRoutingConfiguredRoute, 0, len(bindings))
	for _, binding := range bindings {
		pm, pmOK := pmByID[binding.ProviderModelID]
		connection, conOK := conByID[pm.ConnectionID]
		provider, providerOK := providerByID[connection.ProviderID]
		if !pmOK || !conOK || !providerOK || binding.ModelID != model.ID || !modelCreationID(binding.ID, "bnd") || !validRoutingProtocol(connection.Protocol) || binding.Weight < 0 || binding.Weight > 100 {
			return connectionMetadataUnavailable
		}
		review.Routes = append(review.Routes, ModelRoutingConfiguredRoute{ID: binding.ID, ProviderID: provider.ID, ProviderName: provider.Name, ConnectionID: connection.ID, ConnectionName: connection.Name, ProviderModelID: pm.ID, UpstreamName: pm.UpstreamName, Protocol: connection.Protocol, Weight: binding.Weight})
	}
	return nil
}
