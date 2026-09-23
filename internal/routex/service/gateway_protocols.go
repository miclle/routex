package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	"sort"
	"time"
)

// gatewayProtocols reports only protocol identities, never upstream topology.
func (s *Service) gatewayProtocols(ctx context.Context, modelIDs []string) (map[string][]string, error) {
	result := make(map[string][]string, len(modelIDs))
	for _, modelID := range modelIDs {
		result[modelID] = []string{}
	}
	if len(modelIDs) == 0 {
		return result, nil
	}
	if s.runtime != nil {
		auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
		if auth == nil || !time.Now().Before(auth.ValidUntil) {
			return nil, runtimeUnavailable
		}
		if routes == nil {
			return result, nil
		}
		for _, modelID := range modelIDs {
			totals, available := map[string]int{}, map[string]bool{}
			for _, candidate := range routes.Models[modelID] {
				route := candidate.Route
				totals[route.Protocol] += route.Weight
				if route.Weight <= 0 || !auth.ProviderModels[route.ProviderModelID] || runtimeDenied(&s.runtime.deniedProviderModels, route.ProviderModelID) {
					continue
				}
				for _, credential := range candidate.Credentials {
					if auth.Credentials[credential.ID] && auth.CredentialAccess[credential.ID][route.ProviderModelID] && !runtimeDenied(&s.runtime.deniedCredentials, credential.ID) {
						available[route.Protocol] = true
					}
				}
			}
			for protocol, total := range totals {
				if entity.SupportedNativeProtocol(protocol) && total == 100 && available[protocol] {
					result[modelID] = append(result[modelID], protocol)
				}
			}
			sort.Strings(result[modelID])
		}
		return result, nil
	}
	var rows []struct {
		ModelID, BindingID, Protocol, CredentialID string
		Weight                                     int
		Disabled                                   bool
	}
	err := s.authDB(ctx).Table("model_provider_bindings b").Select("b.model_id, b.id AS binding_id, b.weight, p.disabled, c.protocol, k.id AS credential_id").Joins("JOIN provider_models p ON p.id = b.provider_model_id").Joins("JOIN provider_connections c ON c.id = p.connection_id").Joins("LEFT JOIN credential_model_accesses a ON a.provider_model_id = p.id").Joins("LEFT JOIN provider_credentials k ON k.id = a.credential_id AND k.connection_id = c.id AND k.enabled = ? AND k.verification_status = ?", true, "verified").Where("b.model_id IN ?", modelIDs).Scan(&rows).Error
	if err != nil {
		return nil, runtimeUnavailable
	}
	type group struct {
		total     int
		available bool
		bindings  map[string]bool
	}
	groups := map[string]map[string]*group{}
	for _, row := range rows {
		if groups[row.ModelID] == nil {
			groups[row.ModelID] = map[string]*group{}
		}
		item := groups[row.ModelID][row.Protocol]
		if item == nil {
			item = &group{bindings: map[string]bool{}}
			groups[row.ModelID][row.Protocol] = item
		}
		if !item.bindings[row.BindingID] {
			item.total += row.Weight
			item.bindings[row.BindingID] = true
		}
		item.available = item.available || (row.Weight > 0 && !row.Disabled && row.CredentialID != "")
	}
	for modelID, protocols := range groups {
		for protocol, item := range protocols {
			if entity.SupportedNativeProtocol(protocol) && item.total == 100 && item.available {
				result[modelID] = append(result[modelID], protocol)
			}
		}
		sort.Strings(result[modelID])
	}
	return result, nil
}
