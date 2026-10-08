package service

import (
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

// Assistance uses only the current bounded picker page. Retained aliases remain
// reservations, and the independent final preview decides whether a write is valid.
func applyModelCreationDefaults(tx *gorm.DB, connection entity.ProviderConnection, rows []ModelCreationProviderModel) error {
	if len(rows) > 50 {
		return apperrors.ErrInternal
	}
	names := []string{}
	for _, row := range rows {
		if row.Selectable && publicModelName.MatchString(row.UpstreamName) {
			names = append(names, row.UpstreamName)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 {
		return nil
	}
	var reservations []entity.ModelName
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "name", names)).Limit(len(names) + 1).Find(&reservations).Error; err != nil {
		return err
	}
	modelIDs := []string{}
	for _, name := range reservations {
		if name.CurrentModelID != nil {
			modelIDs = append(modelIDs, name.ModelID)
		}
	}
	slices.Sort(modelIDs)
	modelIDs = slices.Compact(modelIDs)
	var models []entity.Model
	var bindings []entity.ModelProviderBinding
	var providerModels []entity.ProviderModel
	var connections []entity.ProviderConnection
	if len(modelIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", modelIDs)).Limit(len(modelIDs) + 1).Find(&models).Error; err != nil {
			return err
		}
		// Existing target reads support 200 bindings per Model. Hydrate once for the
		// complete page instead of calling the per-Model reader from a row loop.
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_id", modelIDs)).Limit(10001).Find(&bindings).Error; err != nil {
			return err
		}
		if len(bindings) > 10000 {
			return apperrors.ErrInternal
		}
		ids := []string{}
		for _, binding := range bindings {
			ids = append(ids, binding.ProviderModelID)
		}
		slices.Sort(ids)
		ids = slices.Compact(ids)
		if len(ids) > 0 {
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", ids)).Limit(len(ids) + 1).Find(&providerModels).Error; err != nil {
				return err
			}
			connectionIDs := []string{}
			for _, pm := range providerModels {
				connectionIDs = append(connectionIDs, pm.ConnectionID)
			}
			slices.Sort(connectionIDs)
			connectionIDs = slices.Compact(connectionIDs)
			if len(connectionIDs) > 0 {
				if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Limit(len(connectionIDs) + 1).Find(&connections).Error; err != nil {
					return err
				}
			}
		}
	}
	choices, err := projectModelCreationDefaults(names, reservations, models, bindings, providerModels, connections, connection)
	if err != nil {
		return err
	}
	for i := range rows {
		if rows[i].Selectable {
			rows[i].InitialTarget = choices[rows[i].UpstreamName]
		}
	}
	return nil
}

func projectModelCreationDefaults(names []string, reservations []entity.ModelName, models []entity.Model, bindings []entity.ModelProviderBinding, pms []entity.ProviderModel, connections []entity.ProviderConnection, current entity.ProviderConnection) (map[string]*ModelCreationInitialTarget, error) {
	if len(names) > 50 || len(reservations) > len(names) || len(bindings) > 10000 {
		return nil, apperrors.ErrInternal
	}
	reserved := map[string]entity.ModelName{}
	wanted := map[string]bool{}
	for _, name := range names {
		if !publicModelName.MatchString(name) || wanted[name] {
			return nil, apperrors.ErrInternal
		}
		wanted[name] = true
	}
	selectedModels := map[string]bool{}
	for _, row := range reservations {
		if !wanted[row.Name] {
			return nil, apperrors.ErrInternal
		}
		if _, exists := reserved[row.Name]; exists {
			return nil, apperrors.ErrInternal
		}
		if row.CurrentModelID != nil {
			if *row.CurrentModelID != row.ModelID || !validAdminModelTarget(row.ModelID) || selectedModels[row.ModelID] {
				return nil, apperrors.ErrInternal
			}
			selectedModels[row.ModelID] = true
		}
		reserved[row.Name] = row
	}
	proofs := map[string]modelCreationModelProof{}
	for _, row := range models {
		if !selectedModels[row.ID] {
			return nil, apperrors.ErrInternal
		}
		if _, exists := proofs[row.ID]; exists {
			return nil, apperrors.ErrInternal
		}
		proofs[row.ID] = modelCreationModelProof{Model: row}
	}
	if len(proofs) != len(selectedModels) {
		return nil, apperrors.ErrInternal
	}
	wantedPMs := map[string]bool{}
	bindingIDs := map[string]bool{}
	counts := map[string]int{}
	for _, row := range bindings {
		if !selectedModels[row.ModelID] || bindingIDs[row.ID] {
			return nil, apperrors.ErrInternal
		}
		bindingIDs[row.ID] = true
		counts[row.ModelID]++
		if counts[row.ModelID] > 200 {
			return nil, apperrors.ErrInternal
		}
		wantedPMs[row.ProviderModelID] = true
	}
	pmMap := map[string]entity.ProviderModel{}
	wantedConnections := map[string]bool{}
	for _, row := range pms {
		if !wantedPMs[row.ID] {
			return nil, apperrors.ErrInternal
		}
		if _, exists := pmMap[row.ID]; exists {
			return nil, apperrors.ErrInternal
		}
		pmMap[row.ID] = row
		wantedConnections[row.ConnectionID] = true
	}
	if len(pmMap) != len(wantedPMs) {
		return nil, apperrors.ErrInternal
	}
	connectionMap := map[string]entity.ProviderConnection{}
	for _, row := range connections {
		if !wantedConnections[row.ID] {
			return nil, apperrors.ErrInternal
		}
		if _, exists := connectionMap[row.ID]; exists {
			return nil, apperrors.ErrInternal
		}
		connectionMap[row.ID] = row
	}
	if len(connectionMap) != len(wantedConnections) {
		return nil, apperrors.ErrInternal
	}
	for _, binding := range bindings {
		pm := pmMap[binding.ProviderModelID]
		c := connectionMap[pm.ConnectionID]
		if c.Protocol == current.Protocol {
			proof := proofs[binding.ModelID]
			proof.Topology = append(proof.Topology, modelCreationTopology{Binding: binding, ProviderModel: pm, Connection: c})
			proofs[binding.ModelID] = proof
		}
	}
	result := map[string]*ModelCreationInitialTarget{}
	for _, name := range names {
		row, exists := reserved[name]
		if !exists {
			result[name] = &ModelCreationInitialTarget{Target: "new", Name: name}
			continue
		}
		if row.CurrentModelID == nil {
			continue
		}
		proof := proofs[row.ModelID]
		weight, blockers := modelCreationWeight(proof, current.ProviderID)
		if len(blockers) == 0 {
			result[name] = &ModelCreationInitialTarget{Target: "existing", Name: name, ModelID: row.ModelID, InitialWeight: &weight}
		}
	}
	return result, nil
}
