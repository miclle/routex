package service

import "time"

// Configuration application is an exact current process observation. It does
// not attest an earlier command, native completion, or fleet convergence.
func (s *Service) modelWeightRuntimeApplied(st *modelWeightState) bool {
	if s.runtime == nil || st == nil || !st.executable(st.Rows) || !s.runtime.publication.TryRLock() {
		return false
	}
	defer s.runtime.publication.RUnlock()
	if !s.runtime.mu.TryLock() {
		return false
	}
	defer s.runtime.mu.Unlock()
	generation := s.runtime.epoch.Load()
	auth, routes := s.runtime.auth.Load(), s.runtime.routes.Load()
	if auth == nil || routes == nil || auth.publicationEpoch != generation || auth.SourceDigest == "" || auth.SourceDigest != routes.Digest || !time.Now().Before(auth.ValidUntil) || !auth.Models[st.Model.ID] || !auth.ModelCreated[st.Model.ID].Equal(st.Model.CreatedAt) || runtimeDenied(&s.runtime.deniedModels, st.Model.ID) {
		return false
	}

	hash := st.currentEligibilityHash()
	if hash == "" || auth.ModelEligibilityHashes[st.Model.ID] != hash {
		return false
	}
	actual := map[string]runtimeRoute{}
	for _, route := range routes.Models[st.Model.ID] {
		if _, exists := actual[route.Route.BindingID]; exists {
			return false
		}
		actual[route.Route.BindingID] = route
	}
	if len(actual) != len(st.Rows) {
		return false
	}
	pms := map[string]int{}
	cs := map[string]int{}
	providers := map[string]runtimeProviderProof{}
	for _, p := range st.Providers {
		providers[p.ID] = runtimeProviderProof{Birth: p.CreatedAt.UTC(), Enabled: p.Enabled, Revision: p.ETag}
	}
	for i, pm := range st.Models {
		pms[pm.ID] = i
	}
	for i, c := range st.Connections {
		cs[c.ID] = i
	}
	for _, row := range st.Rows {
		candidate, exists := actual[row.BindingID]
		pmIndex, pok := pms[row.ProviderModelID]
		cIndex, cok := cs[row.ConnectionID]
		if !exists || !pok || !cok {
			return false
		}
		route, pm, c := candidate.Route, st.Models[pmIndex], st.Connections[cIndex]
		provider, providerOK := providers[c.ProviderID]
		if !providerOK || auth.Providers[c.ProviderID] != provider || !route.ProviderBirth.Equal(provider.Birth) || route.ProviderEnabled != provider.Enabled || route.ProviderRevision != provider.Revision || runtimeDenied(&s.runtime.deniedProviders, c.ProviderID) {
			return false
		}
		revision := st.EgressRevisions[c.ID]
		if revision == "" || route.EgressRevision != revision || auth.ConnectionRevisions[c.ID] != revision || route.EgressGeneration != s.egressGeneration.Load() || route.Weight != row.Weight || route.ProviderModelID != pm.ID || route.ConnectionID != c.ID || route.ProviderID != c.ProviderID || route.Protocol != c.Protocol || route.UpstreamName != pm.UpstreamName || route.BaseURL != c.BaseURL || route.Disabled != pm.Disabled || route.SupportsImageInput != pm.SupportsImageInput || route.SupportsPDFInput != pm.SupportsPDFInput || !route.ConnectionBirth.Equal(c.CreatedAt) || route.ConnectionEnabled != c.Enabled || auth.ProviderModelRevisions[pm.ID] != pm.ETag || auth.ProviderModels[pm.ID] != (!pm.Disabled && capabilityTransportCurrent(pm, c)) || route.ConnectionTransportGeneration != c.TransportGeneration || route.ProviderModelRevision != pm.ETag || route.CapabilitiesTransportCurrent != capabilityTransportCurrent(pm, c) || runtimeDenied(&s.runtime.deniedProviderModels, pm.ID) {
			return false
		}
		supply, ok := st.Supplies[pm.ID]
		if !ok {
			return false
		}
		expected := map[string]modelCreationCredentialProof{}
		for _, credential := range supply.Credentials {
			if credential.Enabled && credential.TransportCurrent && credential.VerificationStatus == "verified" && containsModelCreationAccess(credential.Access, pm.ID) {
				expected[credential.ID] = credential
			}
			if auth.CredentialRevisions[credential.ID] != credential.Revision || auth.Credentials[credential.ID] != (credential.Enabled && credential.TransportCurrent && credential.VerificationStatus == "verified") || auth.CredentialAccess[credential.ID][pm.ID] != containsModelCreationAccess(credential.Access, pm.ID) || runtimeDenied(&s.runtime.deniedCredentials, credential.ID) {
				return false
			}
		}
		if len(expected) != len(candidate.Credentials) {
			return false
		}
		for _, credential := range candidate.Credentials {
			proof, ok := expected[credential.ID]
			if !ok || !credential.CreatedAt.Equal(proof.CreatedAt) || credential.CipherHash != proof.CipherHash {
				return false
			}
		}
		if row.Weight > 0 && !s.runtimeRouteReadyForDiscovery(auth, candidate) {
			return false
		}
	}
	return s.runtime.auth.Load() == auth && s.runtime.routes.Load() == routes && s.runtime.epoch.Load() == generation && time.Now().Before(auth.ValidUntil)
}
