package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func modelCreationPublicationFixture() (*Service, *modelCreationState) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	modelID := "mdl_one"
	connection := entity.ProviderConnection{TransportGeneration: "0", Enabled: true, ID: "con_one", ProviderID: "prv_one", Protocol: entity.ProtocolOpenAIChat, BaseURL: "https://example.test/v1", CreatedAt: now}
	pm := entity.ProviderModel{CapabilityTransportGeneration: "0", ID: "pmd_one", ConnectionID: connection.ID, UpstreamName: "upstream", ETag: "revision", CreatedAt: now}
	name := entity.ModelName{Name: "Public", ModelID: modelID, CurrentModelID: &modelID, CreatedAt: now}
	model := modelCreationModelProof{Model: entity.Model{ID: modelID, Status: entity.ResourceActive, CreatedAt: now}, Name: name, Topology: []modelCreationTopology{{Binding: entity.ModelProviderBinding{ID: "bnd_one", ModelID: modelID, ProviderModelID: pm.ID, Weight: 100, CreatedAt: now}, ProviderModel: pm, Connection: connection, EgressRevision: "egress"}}}
	provider := entity.Provider{ID: connection.ProviderID, CreatedAt: now, Enabled: true, ETag: "0"}
	state := &modelCreationState{Provider: provider, Connection: connection, EgressRevision: "egress", ProviderModels: []entity.ProviderModel{pm}, Models: []modelCreationModelProof{model}, Credentials: []modelCreationCredentialProof{{TransportCurrent: true, ID: "crd_one", Revision: "credential", CipherHash: "exact-cipher", CreatedAt: now, Enabled: true, VerificationStatus: "verified", Access: []string{pm.ID}}}}
	s := &Service{runtime: &gatewayRuntime{}}
	auth := &runtimeAuthorization{Providers: map[string]runtimeProviderProof{provider.ID: {Birth: now, Enabled: true, Revision: provider.ETag}}, ValidUntil: now.Add(time.Minute), SourceDigest: "same", Connections: map[string]runtimeConnectionProof{connection.ID: {TransportGeneration: "0", ProviderID: connection.ProviderID, Birth: now, Enabled: true}}, ConnectionRevisions: map[string]string{connection.ID: "egress"}, ProviderModels: map[string]bool{pm.ID: true}, ProviderModelRevisions: map[string]string{pm.ID: pm.ETag}, Credentials: map[string]bool{"crd_one": true}, CredentialRevisions: map[string]string{"crd_one": "credential"}, CredentialAccess: map[string]map[string]bool{"crd_one": {pm.ID: true}}, Models: map[string]bool{modelID: true}, ModelCreated: map[string]time.Time{modelID: now}, Names: map[string]entity.ModelName{name.Name: name}}
	route := runtimeRoute{Route: gatewayRoute{ProviderModelBirth: now, ProviderModelRevision: pm.ETag, CapabilityTransportGeneration: "0", ConnectionTransportGeneration: "0", CapabilitiesTransportCurrent: true, ConnectionBirth: now, ConnectionEnabled: true, Protocol: connection.Protocol, BindingID: "bnd_one", Weight: 100, ProviderModelID: pm.ID, ConnectionID: connection.ID, ProviderID: connection.ProviderID, ProviderBirth: now, ProviderEnabled: true, ProviderRevision: provider.ETag, UpstreamName: pm.UpstreamName, BaseURL: connection.BaseURL, EgressRevision: "egress"}, Credentials: []runtimeCredential{{ID: "crd_one", CipherHash: "exact-cipher", CreatedAt: now}}}
	s.runtime.auth.Store(auth)
	s.runtime.routes.Store(&runtimeRoutes{Digest: "same", Models: map[string][]runtimeRoute{modelID: {route}}})
	return s, state
}
func TestModelCreationBatchPublicationUsesExactPrivateProof(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Service, *modelCreationState)
	}{
		{"expired lease", func(s *Service, _ *modelCreationState) {
			s.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
		}},
		{"mixed snapshots", func(s *Service, _ *modelCreationState) { s.runtime.routes.Load().Digest = "other" }},
		{"wrong binding identity", func(s *Service, _ *modelCreationState) {
			s.runtime.routes.Load().Models["mdl_one"][0].Route.BindingID = "bnd_other"
		}},
		{"wrong weight", func(s *Service, _ *modelCreationState) { s.runtime.routes.Load().Models["mdl_one"][0].Route.Weight = 0 }},
		{"missing full topology", func(s *Service, _ *modelCreationState) { s.runtime.routes.Load().Models["mdl_one"] = nil }},
		{"wrong Model creation", func(s *Service, _ *modelCreationState) { s.runtime.auth.Load().ModelCreated["mdl_one"] = time.Now() }},
		{"inactive Model", func(s *Service, _ *modelCreationState) { s.runtime.auth.Load().Models["mdl_one"] = false }},
		{"renamed current identity", func(s *Service, _ *modelCreationState) { delete(s.runtime.auth.Load().Names, "Public") }},
		{"unpublished PM revision", func(s *Service, _ *modelCreationState) {
			s.runtime.auth.Load().ProviderModelRevisions["pmd_one"] = "other"
		}},
		{"unpublished transport", func(s *Service, _ *modelCreationState) { s.egressGeneration.Add(1) }},
		{"partial coverage", func(s *Service, _ *modelCreationState) {
			s.runtime.auth.Load().CredentialAccess["crd_one"]["pmd_one"] = false
		}},
		{"old auth and old route secret", func(s *Service, state *modelCreationState) { state.Credentials[0].CipherHash = "changed-cipher" }},
		{"refreshed auth retained old route secret", func(s *Service, state *modelCreationState) {
			state.Credentials[0].CipherHash = "changed-cipher"
			s.runtime.auth.Load().SourceDigest = "fresh"
			s.runtime.routes.Load().Digest = "fresh"
		}},
		{"wrong Credential creation", func(s *Service, _ *modelCreationState) {
			s.runtime.routes.Load().Models["mdl_one"][0].Credentials[0].CreatedAt = time.Now()
		}},
		{"disabled Credential", func(s *Service, _ *modelCreationState) { s.runtime.auth.Load().Credentials["crd_one"] = false }},
		{"Model tombstone", func(s *Service, _ *modelCreationState) { s.runtime.deniedModels.Store("mdl_one", uint64(1)) }},
		{"PM tombstone", func(s *Service, _ *modelCreationState) { s.runtime.deniedProviderModels.Store("pmd_one", uint64(1)) }},
		{"Credential tombstone", func(s *Service, _ *modelCreationState) { s.runtime.deniedCredentials.Store("crd_one", uint64(1)) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, state := modelCreationPublicationFixture()
			if !s.modelCreationRuntimeApplied(state) {
				t.Fatal("valid exact fixture not applied")
			}
			test.change(s, state)
			if s.modelCreationRuntimeApplied(state) {
				t.Fatal("stale private publication claimed applied")
			}
		})
	}
	s, state := modelCreationPublicationFixture()
	s.runtime.mu.Lock()
	if s.modelCreationRuntimeApplied(state) {
		t.Fatal("in-progress publication claimed coherent proof")
	}
	s.runtime.mu.Unlock()
	s.runtime.publication.Lock()
	if s.modelCreationRuntimeApplied(state) {
		t.Fatal("exclusive retirement claimed available publication")
	}
	s.runtime.publication.Unlock()
	// A0 backup is retained as configuration alongside a100 route, without claiming it can dispatch.
	zero := state.Models[0].Topology[0]
	zero.Binding.ID = "bnd_zero"
	zero.Binding.Weight = 0
	state.Models[0].Topology = append(state.Models[0].Topology, zero)
	route := s.runtime.routes.Load().Models["mdl_one"][0]
	route.Route.BindingID = "bnd_zero"
	route.Route.Weight = 0
	s.runtime.routes.Load().Models["mdl_one"] = append(s.runtime.routes.Load().Models["mdl_one"], route)
	if !s.modelCreationRuntimeApplied(state) {
		t.Fatal("saved0 backup was confused with dispatch eligibility")
	}
}
func TestModelCreationBatchNameTimesUseInstants(t *testing.T) {
	s, state := modelCreationPublicationFixture()
	name := s.runtime.auth.Load().Names["Public"]
	name.CreatedAt = name.CreatedAt.In(time.FixedZone("recorded", 3600))
	s.runtime.auth.Load().Names["Public"] = name
	if !s.modelCreationRuntimeApplied(state) {
		t.Fatal("same persisted instant changed current-name proof")
	}
}

func TestModelCreationPublicationTransportContinuity(t *testing.T) {
	for _, kind := range []string{"connection generation", "capability stamp", "credential stamp", "captured route generation", "captured capability flags", "generation ABA"} {
		t.Run(kind, func(t *testing.T) {
			s, state := modelCreationPublicationFixture()
			if !s.modelCreationRuntimeApplied(state) {
				t.Fatal("current legacy proof rejected")
			}
			switch kind {
			case "connection generation":
				state.Connection.TransportGeneration = transportTestA
			case "capability stamp":
				state.ProviderModels[0].CapabilityTransportGeneration = transportTestA
			case "credential stamp":
				state.Credentials[0].TransportCurrent = false
			case "captured route generation":
				s.runtime.routes.Load().Models["mdl_one"][0].Route.ConnectionTransportGeneration = transportTestA
			case "captured capability flags":
				s.runtime.routes.Load().Models["mdl_one"][0].Route.CapabilitiesTransportCurrent = false
			case "generation ABA":
				proof := s.runtime.auth.Load().Connections[state.Connection.ID]
				proof.TransportGeneration = transportTestB
				s.runtime.auth.Load().Connections[state.Connection.ID] = proof
			}
			if s.modelCreationRuntimeApplied(state) {
				t.Fatal("stale guided publication applied", kind)
			}
		})
	}
}
