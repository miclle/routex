package service

import (
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestAdminModelConfiguredInputsIntersectExactReadyPositiveProtocolRoutes(t *testing.T) {
	for _, scenario := range []string{"both", "mixed", "zero", "disabled", "uncovered", "stale", "separate_protocol", "partial_total", "empty", "inactive"} {
		t.Run(scenario, func(t *testing.T) {
			models, pms, connections, providers, states := configuredReadinessTestGraph()
			pms[0].SupportsImageInput, pms[0].SupportsPDFInput = true, true
			other := pms[0]
			other.ID, other.SupportsPDFInput = "pmd_other", false
			binding := models[0].Bindings[0].Binding
			binding.ID, binding.ProviderModelID, binding.Weight = "bnd_other", other.ID, 50
			models[0].Bindings[0].Binding.Weight = 50
			connection := connections[0]
			expected := map[string][]string{entity.ProtocolOpenAIChat: {"image"}}
			switch scenario {
			case "both":
				other.SupportsPDFInput = true
				expected[entity.ProtocolOpenAIChat] = []string{"image", "pdf"}
			case "zero":
				binding.Weight = 0
				models[0].Bindings[0].Binding.Weight = 100
				expected[entity.ProtocolOpenAIChat] = []string{"image", "pdf"}
			case "disabled":
				other.Disabled = true
				expected[entity.ProtocolOpenAIChat] = []string{"image", "pdf"}
			case "uncovered", "stale":
				expected[entity.ProtocolOpenAIChat] = []string{"image", "pdf"}
			case "separate_protocol":
				connection.ID, connection.Protocol = "con_responses", entity.ProtocolOpenAIResponses
				other.ConnectionID = connection.ID
				binding.Weight, models[0].Bindings[0].Binding.Weight = 100, 100
				connections = append(connections, connection)
				expected[entity.ProtocolOpenAIChat], expected[entity.ProtocolOpenAIResponses] = []string{"image", "pdf"}, []string{"image"}
			case "partial_total":
				binding.Weight = 49
				expected = map[string][]string{}
			case "empty":
				pms[0].SupportsImageInput, pms[0].SupportsPDFInput, other.SupportsImageInput = false, false, false
				expected[entity.ProtocolOpenAIChat] = []string{}
			case "inactive":
				models[0].Model.Status = entity.ResourceDisabled
				expected = map[string][]string{}
			}
			pms = append(pms, other)
			models[0].Bindings = append(models[0].Bindings, modelBindingCatalog(binding, other, connection, true))
			states[other.ID] = states[pms[0].ID]
			if scenario == "uncovered" {
				states[other.ID] = routingSupplyState{Enabled: true, Covered: false, SourceAvailable: true}
			}
			if scenario == "stale" {
				pms[1].CapabilityTransportGeneration = "rev_01j00000000000000000000000"
			}
			projectModelConfiguredReadiness(models, pms, connections, providers, states)
			if !reflect.DeepEqual(models[0].ConfiguredInputCapabilities, expected) {
				t.Fatal("configured intersection diverged", scenario, models[0].ConfiguredInputCapabilities, expected)
			}
			if models[0].Bindings[0].Binding.Weight == 0 || !models[0].Bindings[0].Ready || len(models[0].GrantedUserIDs) != 1 {
				t.Fatal("read projection changed routing or access")
			}
		})
	}
	models, _, _, _, _ := configuredReadinessTestGraph()
	models[0].Bindings = nil
	projectModelConfiguredReadiness(models, nil, nil, nil, nil)
	if models[0].ConfiguredInputCapabilities == nil || len(models[0].ConfiguredInputCapabilities) != 0 {
		t.Fatal("complete no-route projection became unknown")
	}
}
