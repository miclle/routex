package service

import (
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// A saved zero-weight route proves configuration publication, never inference eligibility.
func (s *Service) modelCreationRuntimeApplied(state *modelCreationState) bool {
	if s.runtime == nil || state == nil || !s.runtime.publication.TryRLock() {
		return false
	}
	defer s.runtime.publication.RUnlock()
	if !s.runtime.mu.TryLock() {
		return false
	}
	defer s.runtime.mu.Unlock()
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || routes == nil || !time.Now().Before(auth.ValidUntil) || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest || auth.ConnectionRevisions[state.Connection.ID] != state.EgressRevision {
		return false
	}
	providerProof, providerOK := auth.Providers[state.Provider.ID]
	if !providerOK || !state.Provider.Enabled || providerProof.Enabled != state.Provider.Enabled || !providerProof.Birth.Equal(state.Provider.CreatedAt) || providerProof.Revision != state.Provider.ETag || runtimeDenied(&s.runtime.deniedProviders, state.Provider.ID) {
		return false
	}
	for _, pm := range state.ProviderModels {
		if !auth.ProviderModels[pm.ID] || auth.ProviderModelRevisions[pm.ID] != pm.ETag || runtimeDenied(&s.runtime.deniedProviderModels, pm.ID) {
			return false
		}
	}
	for _, credential := range state.Credentials {
		if auth.Credentials[credential.ID] != (credential.Enabled && credential.VerificationStatus == "verified") || auth.CredentialRevisions[credential.ID] != credential.Revision || runtimeDenied(&s.runtime.deniedCredentials, credential.ID) {
			return false
		}
		for _, pm := range state.ProviderModels {
			if auth.CredentialAccess[credential.ID][pm.ID] != containsModelCreationAccess(credential.Access, pm.ID) {
				return false
			}
		}
	}
	for _, model := range state.Models {
		if !auth.Models[model.Model.ID] || !auth.ModelCreated[model.Model.ID].Equal(model.Model.CreatedAt) || runtimeDenied(&s.runtime.deniedModels, model.Model.ID) {
			return false
		}
		name, exists := auth.Names[model.Name.Name]
		if !exists || !modelCreationNameEqual(name, model.Name) {
			return false
		}
		if len(model.Topology) == 0 {
			return false
		}
		protocol := model.Topology[0].Connection.Protocol
		actual := map[string]runtimeRoute{}
		for _, route := range routes.Models[model.Model.ID] {
			if route.Route.Protocol == protocol {
				actual[route.Route.BindingID] = route
			}
		}
		if len(actual) != len(model.Topology) {
			return false
		}
		for _, row := range model.Topology {
			route, exists := actual[row.Binding.ID]
			r := route.Route
			if !exists || (!s.runtimeProviderMatches(auth, r) || r.Weight > 0 && !s.runtimeProviderAllowed(auth, r)) || r.Weight != row.Binding.Weight || r.ProviderModelID != row.ProviderModel.ID || r.ConnectionID != row.Connection.ID || r.ProviderID != row.Connection.ProviderID || r.UpstreamName != row.ProviderModel.UpstreamName || r.BaseURL != row.Connection.BaseURL || r.SupportsImageInput != row.ProviderModel.SupportsImageInput || r.SupportsPDFInput != row.ProviderModel.SupportsPDFInput || r.EgressRevision != row.EgressRevision || r.EgressGeneration != s.egressGeneration.Load() || auth.ConnectionRevisions[row.Connection.ID] != row.EgressRevision || auth.ProviderModelRevisions[row.ProviderModel.ID] != row.ProviderModel.ETag || auth.ProviderModels[row.ProviderModel.ID] == row.ProviderModel.Disabled || runtimeDenied(&s.runtime.deniedProviderModels, row.ProviderModel.ID) {
				return false
			}
			if row.Connection.ID == state.Connection.ID {
				expected := map[string]modelCreationCredentialProof{}
				for _, credential := range state.Credentials {
					if credential.Enabled && credential.VerificationStatus == "verified" && containsModelCreationAccess(credential.Access, row.ProviderModel.ID) {
						expected[credential.ID] = credential
					}
				}
				if len(route.Credentials) != len(expected) {
					return false
				}
				for _, credential := range route.Credentials {
					proof, exists := expected[credential.ID]
					if !exists || !credential.CreatedAt.Equal(proof.CreatedAt) || credential.CipherHash != proof.CipherHash {
						return false
					}
				}
			}
		}
	}
	return true
}
func containsModelCreationAccess(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func modelCreationNameEqual(a, b entity.ModelName) bool {
	if a.Name != b.Name || a.ModelID != b.ModelID || !a.CreatedAt.Equal(b.CreatedAt) || (a.CurrentModelID == nil) != (b.CurrentModelID == nil) || (a.ExpiresAt == nil) != (b.ExpiresAt == nil) {
		return false
	}
	if a.CurrentModelID != nil && *a.CurrentModelID != *b.CurrentModelID {
		return false
	}
	return a.ExpiresAt == nil || a.ExpiresAt.Equal(*b.ExpiresAt)
}
