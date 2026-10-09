package service

import (
	"database/sql"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type databaseTransportRoute struct {
	ModelID    string
	Route      gatewayRoute
	Credential entity.ProviderCredential
}

// Hydrate only topology for the caller's already-authorized logical Models.
// Stored stamps are compared byte-exactly in Go, including MySQL deployments.
func readDatabaseGatewayRoutes(db *gorm.DB, modelIDs []string) ([]databaseTransportRoute, error) {
	var rows []databaseTransportRoute
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		rows, e = readDatabaseGatewayRoutesTx(tx, modelIDs)
		return e
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return rows, err
}

func readDatabaseGatewayRoutesTx(db *gorm.DB, modelIDs []string) ([]databaseTransportRoute, error) {
	type row struct {
		ModelID                                                           string
		ProviderRecordID, ProviderModelConnectionID, BoundProviderModelID string
		Route                                                             gatewayRoute `gorm:"embedded"`
	}
	var rows []row
	err := db.Table("model_provider_bindings AS b").Select("b.model_id, b.provider_model_id AS bound_provider_model_id, p.connection_id AS provider_model_connection_id, pr.id AS provider_record_id, b.id AS binding_id, b.weight, p.id AS provider_model_id, p.created_at AS provider_model_birth, p.e_tag AS provider_model_revision, p.capability_transport_generation, p.upstream_name, p.disabled, p.supports_image_input, p.supports_pdf_input, c.id AS connection_id, c.created_at AS connection_birth, c.transport_generation AS connection_transport_generation, c.enabled AS connection_enabled, c.provider_id, c.name AS connection_name, c.base_url, c.protocol, c.adapter, c.api_version, pr.name AS provider_name, pr.created_at AS provider_birth, pr.enabled AS provider_enabled, pr.e_tag AS provider_revision").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Joins("JOIN provider_connections c ON c.id = p.connection_id").Joins("JOIN providers pr ON pr.id = c.provider_id").Where(memberModelsExactIDs(db, "model_id", modelIDs)).Order("b.id").Limit(5001).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	if len(rows) > 5000 {
		return nil, runtimeUnavailable
	}
	selected := map[string][]string{}
	for _, row := range rows {
		selected[row.Route.ConnectionID] = append(selected[row.Route.ConnectionID], row.Route.ProviderModelID)
	}
	currentConnections := map[string]entity.ProviderConnection{}
	credentials := map[string][]entity.ProviderCredential{}
	coverage := map[string]map[string]map[string]bool{}
	for id, ids := range selected {
		cs, covered, e := connectionCredentialCoverage(db, id, ids)
		if e != nil {
			return nil, e
		}
		credentials[id] = cs
		coverage[id] = covered
		var current entity.ProviderConnection
		if e := personalExact(modelCreationDB(db), "id", id).Take(&current).Error; e != nil {
			return nil, e
		}
		currentConnections[id] = current
	}
	result := make([]databaseTransportRoute, 0, len(rows))
	for _, row := range rows {
		r := row.Route
		c := entity.ProviderConnection{ID: r.ConnectionID, ProviderID: r.ProviderID, CreatedAt: r.ConnectionBirth, TransportGeneration: r.ConnectionTransportGeneration, BaseURL: r.BaseURL, Protocol: r.Protocol, Adapter: r.Adapter, APIVersion: r.APIVersion}
		pm := entity.ProviderModel{ID: r.ProviderModelID, ConnectionID: r.ConnectionID, CreatedAt: r.ProviderModelBirth, CapabilityTransportGeneration: r.CapabilityTransportGeneration}
		current := currentConnections[r.ConnectionID]
		sameParent := row.ProviderRecordID == r.ProviderID && row.ProviderModelConnectionID == r.ConnectionID && row.BoundProviderModelID == r.ProviderModelID && current.ID == r.ConnectionID && current.ProviderID == r.ProviderID && current.CreatedAt.Equal(r.ConnectionBirth) && current.TransportGeneration == r.ConnectionTransportGeneration && sameConnectionTransport(connectionTransportTuple(current), connectionTransportTuple(c))
		r.Adapter = entity.ConnectionAdapter(c)
		r.ConnectionEnabled = r.ConnectionEnabled && current.Enabled && sameParent
		r.CapabilitiesTransportCurrent = sameParent && capabilityTransportCurrent(pm, c)
		out := databaseTransportRoute{ModelID: row.ModelID, Route: r}
		for _, cred := range credentials[c.ID] {
			if cred.Enabled && verifiedTransportCurrent(cred, c) && coverage[c.ID][cred.ID][pm.ID] {
				out.Credential = cred
				break
			}
		}
		result = append(result, out)
	}
	return result, nil
}
