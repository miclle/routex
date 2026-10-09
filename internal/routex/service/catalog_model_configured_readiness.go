package service

import (
	"errors"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

// Optional projection bounds do not truncate the existing catalogue response.
const modelConfiguredReadinessLimit = 1000

func modelConfiguredReadinessBounded(models []ModelCatalog) bool {
	if len(models) > modelConfiguredReadinessLimit {
		return false
	}
	bindings := 0
	for _, model := range models {
		bindings += len(model.Bindings)
		if bindings > modelConfiguredReadinessLimit {
			return false
		}
	}
	return true
}

// This optional summary is projected only after independent Provider read authority.
// Existing weight-write credential eligibility and legacy binding.ready are untouched.
func enrichModelConfiguredReadiness(tx *gorm.DB, actorID string, models []ModelCatalog) error {
	for i := range models {
		models[i].ConfiguredReady = nil
	}
	actor, err := exactEnabledActor(modelCreationDB(tx), actorID)
	if err != nil {
		return err
	}
	allowed, err := exactGovernancePermission(modelCreationDB(tx), actor, "providers.read")
	if err != nil || !allowed {
		return err
	}
	return loadModelConfiguredReadiness(tx, models)
}

// Query only exact retained dependencies in page-wide bounded batches. Projection
// overflow/incompleteness stays Unknown; real read errors still fail the request.
func loadModelConfiguredReadiness(tx *gorm.DB, models []ModelCatalog) error {
	for i := range models {
		models[i].ConfiguredReady = nil
	}
	if !modelConfiguredReadinessBounded(models) {
		return nil
	}
	ids := []string{}
	seen := map[string]bool{}
	for i := range models {
		for _, binding := range models[i].Bindings {
			id := binding.Binding.ProviderModelID
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	if len(ids) > modelConfiguredReadinessLimit {
		return nil
	}
	if len(ids) == 0 {
		projectModelConfiguredReadiness(models, nil, nil, nil, nil)
		return nil
	}
	var pms []entity.ProviderModel
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Limit(modelConfiguredReadinessLimit + 1).Find(&pms).Error; err != nil {
		return err
	}
	connectionIDs := []string{}
	seen = map[string]bool{}
	for _, pm := range pms {
		if !seen[pm.ConnectionID] {
			seen[pm.ConnectionID] = true
			connectionIDs = append(connectionIDs, pm.ConnectionID)
		}
	}
	var connections []entity.ProviderConnection
	if len(connectionIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Limit(modelConfiguredReadinessLimit + 1).Find(&connections).Error; err != nil {
			return err
		}
	}
	providerIDs := []string{}
	seen = map[string]bool{}
	for _, connection := range connections {
		if !seen[connection.ProviderID] {
			seen[connection.ProviderID] = true
			providerIDs = append(providerIDs, connection.ProviderID)
		}
	}
	var providers []entity.Provider
	if len(providerIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", providerIDs)).Limit(modelConfiguredReadinessLimit + 1).Find(&providers).Error; err != nil {
			return err
		}
	}
	if len(pms) > modelConfiguredReadinessLimit || len(connections) > modelConfiguredReadinessLimit || len(providers) > modelConfiguredReadinessLimit {
		return nil
	}
	states, err := routingSupplyStates(tx, connections, pms)
	if errors.Is(err, connectionMetadataUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	projectModelConfiguredReadiness(models, pms, connections, providers, states)
	return nil
}

// Validate the entire exact dependency projection before publishing any boolean.
// No absent or collating-alias row can become a known false or a positive route.
func projectModelConfiguredReadiness(models []ModelCatalog, pms []entity.ProviderModel, connections []entity.ProviderConnection, providers []entity.Provider, states map[string]routingSupplyState) {
	for i := range models {
		models[i].ConfiguredReady = nil
	}
	if !modelConfiguredReadinessBounded(models) || len(pms) > modelConfiguredReadinessLimit || len(connections) > modelConfiguredReadinessLimit || len(providers) > modelConfiguredReadinessLimit || len(states) > modelConfiguredReadinessLimit {
		return
	}
	pmRows := map[string]entity.ProviderModel{}
	connectionRows := map[string]entity.ProviderConnection{}
	providerRows := map[string]entity.Provider{}
	for _, pm := range pms {
		if pm.ID == "" || pmRows[pm.ID].ID != "" {
			return
		}
		pmRows[pm.ID] = pm
	}
	for _, connection := range connections {
		if connection.ID == "" || connectionRows[connection.ID].ID != "" {
			return
		}
		connectionRows[connection.ID] = connection
	}
	for _, provider := range providers {
		if provider.ID == "" || providerRows[provider.ID].ID != "" {
			return
		}
		providerRows[provider.ID] = provider
	}
	usedPMs, usedConnections, usedProviders := map[string]bool{}, map[string]bool{}, map[string]bool{}
	modelIDs, bindingIDs := map[string]bool{}, map[string]bool{}
	values := make([]bool, len(models))
	for i, model := range models {
		if model.Model.ID == "" || modelIDs[model.Model.ID] ||
			(model.Model.Status != entity.ResourceActive && model.Model.Status != entity.ResourceDisabled && model.Model.Status != entity.ResourceArchived) {
			return
		}
		modelIDs[model.Model.ID] = true
		for _, binding := range model.Bindings {
			pm, foundPM := pmRows[binding.Binding.ProviderModelID]
			connection, foundConnection := connectionRows[pm.ConnectionID]
			provider, foundProvider := providerRows[connection.ProviderID]
			state, foundState := states[pm.ID]
			if binding.Binding.ID == "" || bindingIDs[binding.Binding.ID] || binding.Binding.ModelID != model.Model.ID ||
				!foundPM || !foundConnection || !foundProvider || !foundState ||
				!connectionMetadataBirth(pm.CreatedAt) || !connectionMetadataBirth(connection.CreatedAt) ||
				!validTransportGeneration(pm.CapabilityTransportGeneration) || !validTransportGeneration(connection.TransportGeneration) ||
				binding.ConnectionID != connection.ID || binding.ProviderID != provider.ID ||
				binding.Protocol != connection.Protocol || binding.UpstreamName != pm.UpstreamName ||
				binding.Binding.Weight < 0 || binding.Binding.Weight > 100 {
				return
			}
			bindingIDs[binding.Binding.ID] = true
			usedPMs[pm.ID], usedConnections[connection.ID], usedProviders[provider.ID] = true, true, true
			if model.Model.Status == entity.ResourceActive && binding.Binding.Weight > 0 &&
				provider.Enabled && connection.Enabled && !pm.Disabled && capabilityTransportCurrent(pm, connection) &&
				state.Enabled && state.Covered && state.SourceAvailable {
				values[i] = true
			}
		}
	}
	if len(usedPMs) != len(pmRows) || len(usedConnections) != len(connectionRows) || len(usedProviders) != len(providerRows) || len(states) != len(pmRows) {
		return
	}
	for i := range models {
		value := values[i]
		models[i].ConfiguredReady = &value
	}
}
