package service

import (
	"context"
	"sort"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

const (
	inputCapabilityImage = "image"
	inputCapabilityPDF   = "pdf"
)

type gatewayModelMetadata struct {
	Protocols         []string
	InputCapabilities map[string][]string
}

type inputCapabilityIntersection struct {
	initialized bool
	image       bool
	pdf         bool
}

func (c *inputCapabilityIntersection) include(route gatewayRoute) {
	if !c.initialized {
		c.initialized = true
		c.image = route.SupportsImageInput
		c.pdf = route.SupportsPDFInput
		return
	}
	c.image = c.image && route.SupportsImageInput
	c.pdf = c.pdf && route.SupportsPDFInput
}

func (c inputCapabilityIntersection) values() []string {
	result := []string{}
	if c.image {
		result = append(result, inputCapabilityImage)
	}
	if c.pdf {
		result = append(result, inputCapabilityPDF)
	}
	return result
}

// gatewayModelMetadata reports effective protocols and their safe input
// capabilities without exposing provider topology. A capability is advertised
// only when every currently ready positive-weight route for that protocol has it.
func (s *Service) gatewayModelMetadata(ctx context.Context, modelIDs []string) (map[string]gatewayModelMetadata, error) {
	result := make(map[string]gatewayModelMetadata, len(modelIDs))
	for _, modelID := range modelIDs {
		result[modelID] = gatewayModelMetadata{Protocols: []string{}, InputCapabilities: map[string][]string{}}
	}
	if len(modelIDs) == 0 {
		return result, nil
	}
	if s.runtime != nil {
		return s.runtimeGatewayModelMetadata(modelIDs, result)
	}
	return s.databaseGatewayModelMetadata(ctx, modelIDs, result)
}

func (s *Service) runtimeGatewayModelMetadata(modelIDs []string, result map[string]gatewayModelMetadata) (map[string]gatewayModelMetadata, error) {
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) {
		return nil, runtimeUnavailable
	}
	if routes == nil {
		return result, nil
	}
	for _, modelID := range modelIDs {
		totals := map[string]int{}
		available := map[string]bool{}
		capabilities := map[string]*inputCapabilityIntersection{}
		for _, candidate := range routes.Models[modelID] {
			route := candidate.Route
			totals[route.Protocol] += route.Weight
			if !s.runtimeRouteReadyForDiscovery(auth, candidate) {
				continue
			}
			available[route.Protocol] = true
			intersection := capabilities[route.Protocol]
			if intersection == nil {
				intersection = &inputCapabilityIntersection{}
				capabilities[route.Protocol] = intersection
			}
			intersection.include(route)
		}
		item := result[modelID]
		for protocol, total := range totals {
			if !entity.SupportedNativeProtocol(protocol) || total != 100 || !available[protocol] {
				continue
			}
			item.Protocols = append(item.Protocols, protocol)
			item.InputCapabilities[protocol] = capabilities[protocol].values()
		}
		sort.Strings(item.Protocols)
		result[modelID] = item
	}
	return result, nil
}

func (s *Service) runtimeRouteReadyForDiscovery(auth *runtimeAuthorization, candidate runtimeRoute) bool {
	route := candidate.Route
	if !s.runtimeConnectionAllowed(auth, route) || route.Weight <= 0 || !auth.ProviderModels[route.ProviderModelID] || runtimeDenied(&s.runtime.deniedProviderModels, route.ProviderModelID) {
		return false
	}
	for _, credential := range candidate.Credentials {
		if auth.Credentials[credential.ID] && auth.CredentialAccess[credential.ID][route.ProviderModelID] && !runtimeDenied(&s.runtime.deniedCredentials, credential.ID) {
			return true
		}
	}
	return false
}

type databaseGatewayRoute struct {
	Route gatewayRoute
	Ready bool
}

type databaseGatewayProtocol struct {
	Total  int
	Routes map[string]*databaseGatewayRoute
}

func (s *Service) databaseGatewayModelMetadata(ctx context.Context, modelIDs []string, result map[string]gatewayModelMetadata) (map[string]gatewayModelMetadata, error) {
	var rows []struct {
		ModelID, BindingID, Protocol, CredentialID string
		Weight                                     int
		Disabled                                   bool
		ConnectionEnabled                          bool
		ProviderEnabled                            bool
		SupportsImageInput                         bool
		SupportsPDFInput                           bool
	}
	err := s.authDB(ctx).Table("model_provider_bindings b").
		Select("b.model_id, b.id AS binding_id, b.weight, p.disabled, c.enabled AS connection_enabled, pr.enabled AS provider_enabled, p.supports_image_input, p.supports_pdf_input, c.protocol, k.id AS credential_id").
		Joins("JOIN provider_models p ON p.id = b.provider_model_id").
		Joins("JOIN provider_connections c ON c.id = p.connection_id").
		Joins("JOIN providers pr ON pr.id = c.provider_id").
		Joins("LEFT JOIN credential_model_accesses a ON a.provider_model_id = p.id").
		Joins("LEFT JOIN provider_credentials k ON k.id = a.credential_id AND k.connection_id = c.id AND k.enabled = ? AND k.verification_status = ?", true, "verified").
		Where("b.model_id IN ?", modelIDs).
		Scan(&rows).Error
	if err != nil {
		return nil, runtimeUnavailable
	}
	groups := map[string]map[string]*databaseGatewayProtocol{}
	for _, row := range rows {
		if groups[row.ModelID] == nil {
			groups[row.ModelID] = map[string]*databaseGatewayProtocol{}
		}
		item := groups[row.ModelID][row.Protocol]
		if item == nil {
			item = &databaseGatewayProtocol{Routes: map[string]*databaseGatewayRoute{}}
			groups[row.ModelID][row.Protocol] = item
		}
		route := item.Routes[row.BindingID]
		if route == nil {
			route = &databaseGatewayRoute{Route: gatewayRoute{
				Protocol:           row.Protocol,
				Weight:             row.Weight,
				Disabled:           row.Disabled,
				SupportsImageInput: row.SupportsImageInput,
				SupportsPDFInput:   row.SupportsPDFInput,
			}}
			item.Routes[row.BindingID] = route
			item.Total += row.Weight
		}
		route.Ready = route.Ready || row.ProviderEnabled && row.ConnectionEnabled && row.CredentialID != ""
	}
	for modelID, protocols := range groups {
		item := result[modelID]
		for protocol, group := range protocols {
			available := false
			intersection := inputCapabilityIntersection{}
			for _, route := range group.Routes {
				if route.Route.Weight <= 0 || route.Route.Disabled || !route.Ready {
					continue
				}
				available = true
				intersection.include(route.Route)
			}
			if !entity.SupportedNativeProtocol(protocol) || group.Total != 100 || !available {
				continue
			}
			item.Protocols = append(item.Protocols, protocol)
			item.InputCapabilities[protocol] = intersection.values()
		}
		sort.Strings(item.Protocols)
		result[modelID] = item
	}
	return result, nil
}
