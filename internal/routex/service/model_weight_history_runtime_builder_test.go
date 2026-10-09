package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

// Use the production builders for both sides of the publication proof. No
// handcrafted route or independently substituted eligibility hash is admitted.
func modelWeightActualBuilderFixture(t *testing.T, connectionEnabled, secondDisabled bool) (*Service, *modelWeightState, *runtimeRoutes) {
	t.Helper()
	s, data, _ := runtimeFixture(t, "https://example.invalid/v1")
	t.Cleanup(s.upstream.CloseIdleConnections)
	t.Cleanup(func() { closeRuntimeClients(s.runtime.routes.Load().Models) })
	birth := time.Date(2026, 10, 9, 0, 0, 0, 123456000, time.UTC)
	data.Models[0].CreatedAt = birth
	data.Names[0].CreatedAt = birth
	data.Providers[0].CreatedAt = birth
	data.Connections[0].CreatedAt = birth
	data.Connections[0].Enabled = connectionEnabled
	data.Connections[0].EgressMode = "direct"
	data.ProviderModels[0].CreatedAt = birth
	data.Credentials[0].CreatedAt = birth
	data.Bindings[0].CreatedAt = birth
	second := data.ProviderModels[0]
	second.ID = "pmd_two"
	second.UpstreamName = "second-recorded-model"
	second.Disabled = secondDisabled
	data.ProviderModels = append(data.ProviderModels, second)
	data.Bindings = append(data.Bindings, entity.ModelProviderBinding{ID: "bnd_two", ModelID: data.Models[0].ID, ProviderModelID: second.ID, Weight: 0, CreatedAt: birth})
	data.Access = append(data.Access, entity.CredentialModelAccess{CredentialID: data.Credentials[0].ID, ProviderModelID: second.ID})

	models, err := s.buildRuntimeRoutes(data)
	if err != nil {
		t.Fatal(err)
	}
	closeRuntimeClients(s.runtime.routes.Load().Models)
	digest, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	auth := buildRuntimeAuthorization(data, time.Now().Add(runtimeAuthorizationLease))
	auth.SourceDigest = digest
	auth.publicationEpoch = s.runtime.epoch.Load()
	routes := &runtimeRoutes{ID: "cfg_builder", Digest: digest, Models: models}
	s.runtime.auth.Store(auth)
	s.runtime.routes.Store(routes)

	st := &modelWeightState{Model: data.Models[0], Models: data.ProviderModels, Connections: data.Connections, Providers: data.Providers, Credentials: data.Credentials, Names: data.Names, Egress: *data, Supplies: map[string]routingSupplyState{}, EgressReady: map[string]bool{}, EgressRevisions: map[string]string{}}
	c := data.Connections[0]
	_, revision, ready := runtimeEgressSelection(data, c)
	st.EgressRevisions[c.ID], st.EgressReady[c.ID] = revision, ready
	credential := data.Credentials[0]
	for i, pm := range data.ProviderModels {
		b := data.Bindings[i]
		st.Rows = append(st.Rows, ModelWeightRow{BindingID: b.ID, BindingCreatedAt: modelWeightBirth(b.CreatedAt), ProviderModelID: pm.ID, ProviderModelCreatedAt: modelWeightBirth(pm.CreatedAt), ConnectionID: c.ID, ConnectionCreatedAt: modelWeightBirth(c.CreatedAt), ProviderID: c.ProviderID, ProviderCreatedAt: modelWeightBirth(data.Providers[0].CreatedAt), Protocol: c.Protocol, Weight: b.Weight})
		st.Supplies[pm.ID] = routingSupplyState{Enabled: c.Enabled, Covered: true, SourceAvailable: true, Credentials: []modelCreationCredentialProof{{TransportCurrent: verifiedTransportCurrent(credential, c), ID: credential.ID, CreatedAt: credential.CreatedAt, Revision: credentialRuntimeRevision(credential), CipherHash: credentialSourceProof(credential), Enabled: credential.Enabled, VerificationStatus: credential.VerificationStatus, Access: []string{pm.ID}}}}
	}
	return s, st, routes
}

func TestModelWeightRuntimeActualBuilderConfiguredFacts(t *testing.T) {
	for _, test := range []struct {
		name              string
		connectionEnabled bool
		secondDisabled    bool
	}{{"enabled", true, false}, {"disabled_connection", false, false}, {"disabled_zero_weight", true, true}, {"disabled_zero_weight_connection_disabled", false, true}} {
		t.Run(test.name, func(t *testing.T) {
			_, st, routes := modelWeightActualBuilderFixture(t, test.connectionEnabled, test.secondDisabled)
			actual := routes.Models[st.Model.ID]
			if len(actual) != 2 || actual[0].Route.Weight != 100 || actual[1].Route.Weight != 0 {
				t.Fatal("actual builder lost the complete configured protocol")
			}
			for i, route := range actual {
				if route.Route.ConnectionEnabled != st.Connections[0].Enabled || route.Route.Disabled != st.Models[i].Disabled || !route.Route.ConnectionBirth.Equal(st.Connections[0].CreatedAt) {
					t.Fatal("actual route omitted captured configuration facts", i)
				}
			}
		})
	}
}

func TestModelWeightRuntimeActualBuilderCurrentApplication(t *testing.T) {
	for _, test := range []struct {
		name              string
		connectionEnabled bool
		secondDisabled    bool
		applied           bool
	}{{"enabled_two_routes", true, false, true}, {"disabled_zero_weight", true, true, true}, {"disabled_connection", false, false, false}, {"disabled_positive_route", true, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			s, st, routes := modelWeightActualBuilderFixture(t, test.connectionEnabled, test.secondDisabled)
			if st.currentEligibilityHash() != s.runtime.auth.Load().ModelEligibilityHashes[st.Model.ID] {
				t.Fatal("real authorization and current topology projections differ")
			}
			if test.name == "disabled_positive_route" {
				st.Models[0].Disabled = true
			}
			if applied := s.modelWeightRuntimeApplied(st); applied != test.applied {
				t.Fatal("real builder publication application differs", applied, test.applied)
			}
			if test.secondDisabled && s.runtimeRouteReadyForDiscovery(s.runtime.auth.Load(), routes.Models[st.Model.ID][1]) {
				t.Fatal("disabled zero-weight configuration became executable")
			}
		})
	}
}
