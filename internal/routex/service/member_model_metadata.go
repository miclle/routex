package service

import (
	"slices"

	"github.com/miclle/routex/internal/routex/database"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Hydrate only an already-authorized complete Model ID set. Selection and
// resource authority stay with the caller; this never loads a candidate catalog.
func readMemberModelMetadata(tx *gorm.DB, data *memberModelsData, modelIDs []string) error {
	if len(modelIDs) == 0 {
		return nil
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_id", modelIDs)).Where(database.ExactTextColumns(tx, clause.Column{Name: "model_id"}, clause.Column{Name: "current_model_id"})).Limit(1001).Find(&data.Names).Error; err != nil {
		return err
	}
	for _, name := range data.Names {
		if !publicModelName.MatchString(name.Name) {
			return apperrors.ErrInternal
		}
	}
	if len(data.Names) != len(data.Models) {
		return apperrors.ErrInternal
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_id", modelIDs)).Order("id").Limit(5001).Find(&data.Bindings).Error; err != nil {
		return err
	}
	if len(data.Bindings) > 5000 {
		return ErrModelCatalogOverflow
	}
	pmIDs := []string{}
	for _, b := range data.Bindings {
		if !slices.Contains(modelIDs, b.ModelID) {
			return apperrors.ErrInternal
		}
		pmIDs = append(pmIDs, b.ProviderModelID)
	}
	slices.Sort(pmIDs)
	pmIDs = slices.Compact(pmIDs)
	if len(pmIDs) == 0 {
		return nil
	}
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", pmIDs)).Limit(5001).Find(&data.ProviderModels).Error; err != nil {
		return err
	}
	if len(data.ProviderModels) != len(pmIDs) {
		return apperrors.ErrInternal
	}
	connectionIDs := []string{}
	for _, p := range data.ProviderModels {
		if !slices.Contains(pmIDs, p.ID) {
			return apperrors.ErrInternal
		}
		connectionIDs = append(connectionIDs, p.ConnectionID)
	}
	slices.Sort(connectionIDs)
	connectionIDs = slices.Compact(connectionIDs)
	if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "id", connectionIDs)).Limit(5001).Find(&data.Connections).Error; err != nil {
		return err
	}
	if len(data.Connections) != len(connectionIDs) {
		return apperrors.ErrInternal
	}
	var egressIDs []string
	defaults := false
	for _, connection := range data.Connections {
		if !slices.Contains(connectionIDs, connection.ID) {
			return apperrors.ErrInternal
		}
		if connection.EgressMode == "" || connection.EgressMode == "default" {
			defaults = true
		}
		if connection.EgressMode == "proxy" && connection.EgressID != nil {
			egressIDs = append(egressIDs, *connection.EgressID)
		}
	}
	if defaults {
		if err := modelCreationDB(tx).Select("ID", "ETag", "DefaultEgressID").Take(&data.EgressSetting, 1).Error; err != nil {
			return err
		}
		if data.EgressSetting.DefaultEgressID != nil {
			egressIDs = append(egressIDs, *data.EgressSetting.DefaultEgressID)
		}
	}
	slices.Sort(egressIDs)
	egressIDs = slices.Compact(egressIDs)
	if len(egressIDs) > 0 {
		if err := modelCreationDB(tx).Select("ID", "ETag", "SecretGeneration", "Kind", "Host", "Port", "Enabled", "CreatedAt").Where(memberModelsExactIDs(tx, "id", egressIDs)).Limit(5001).Find(&data.Egresses).Error; err != nil {
			return err
		}
		if len(data.Egresses) > 5000 {
			return ErrModelCatalogOverflow
		}
	}
	if err := modelCreationDB(tx).Select("id", "connection_id", "name", "priority", "enabled", "verification_status", "verified_at", "created_at", "storage_source", "coverage_revision", "verified_transport_generation").Where(memberModelsExactIDs(tx, "connection_id", connectionIDs)).Order("id").Limit(5001).Find(&data.Credentials).Error; err != nil {
		return err
	}
	if len(data.Credentials) > 5000 {
		return ErrModelCatalogOverflow
	}
	credentialIDs := []string{}
	for _, c := range data.Credentials {
		if !slices.Contains(connectionIDs, c.ConnectionID) {
			return apperrors.ErrInternal
		}
		credentialIDs = append(credentialIDs, c.ID)
	}
	if len(credentialIDs) > 0 {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "credential_id", credentialIDs)).Where(memberModelsExactIDs(tx, "provider_model_id", pmIDs)).Order("credential_id,provider_model_id").Limit(5001).Find(&data.Access).Error; err != nil {
			return err
		}
		if len(data.Access) > 5000 {
			return ErrModelCatalogOverflow
		}
	}
	if err := attachCredentialSources(modelCreationDB(tx), data.Credentials); err != nil {
		return err
	}
	projected, err := loadDeploymentCoverage(tx, data.Credentials, data.Connections, data.ProviderModels, data.Access, 5000)
	if err != nil {
		return err
	}
	data.Access = projected

	// Provider eligibility is internal authority, independent of directory reads.
	// Hydrate only Providers reached through this authorized bounded topology.
	ids := []string{}
	for _, c := range data.Connections {
		if !slices.Contains(connectionIDs, c.ID) {
			return apperrors.ErrInternal
		}
		ids = append(ids, c.ProviderID)
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	columns := []string{"id", "created_at", "enabled", "e_tag"}
	if data.ProvidersRead {
		columns = append(columns, "name")
	}
	if len(ids) > 0 {
		if err := modelCreationDB(tx).Select(columns).Where(memberModelsExactIDs(tx, "id", ids)).Limit(5001).Find(&data.Providers).Error; err != nil {
			return err
		}
	}
	if len(data.Providers) != len(ids) {
		return apperrors.ErrInternal
	}
	for _, provider := range data.Providers {
		if !slices.Contains(ids, provider.ID) || !connectionMetadataBirth(provider.CreatedAt) || provider.ETag == "" || data.ProvidersRead && !validCatalogLabel(provider.Name) {
			return apperrors.ErrInternal
		}
	}
	if data.PricesRead {
		if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "provider_model_id", pmIDs)).Limit(5001).Find(&data.Prices).Error; err != nil {
			return err
		}
		if len(data.Prices) > 5000 {
			return ErrModelCatalogOverflow
		}
		ids := []string{}
		for _, p := range data.Prices {
			ids = append(ids, p.ID)
		}
		if len(ids) > 0 {
			if err := modelCreationDB(tx).Where(memberModelsExactIDs(tx, "model_price_id", ids)).Where("metric IN ?", []string{"INPUT_TOKEN", "OUTPUT_TOKEN"}).Limit(5001).Find(&data.Rates).Error; err != nil {
				return err
			}
			if len(data.Rates) > 5000 {
				return ErrModelCatalogOverflow
			}
		}
	}
	return nil
}
