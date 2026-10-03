package service

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelBindingAvailabilityPreservesCredentialActivation(t *testing.T) {
	connection := entity.ProviderConnection{ID: "con_supply", ProviderID: "prv_supply", Protocol: entity.ProtocolOpenAIResponses}
	for _, disabled := range []bool{false, true} {
		for _, covered := range []bool{false, true} {
			for _, weight := range []int{0, 100} {
				t.Run(fmt.Sprintf("disabled=%t/covered=%t/weight=%d", disabled, covered, weight), func(t *testing.T) {
					binding := entity.ModelProviderBinding{ID: "bnd_supply", ModelID: "mdl_supply", ProviderModelID: "pmd_supply", Weight: weight}
					model := entity.ProviderModel{ID: binding.ProviderModelID, ConnectionID: connection.ID, UpstreamName: "supply-upstream", Disabled: disabled}
					catalog := modelBindingCatalog(binding, model, connection, covered)
					if catalog.Ready != (!disabled && covered) {
						t.Fatal("disabled supply or missing credential coverage advertised availability", catalog)
					}
					if catalog.credentialReady != covered {
						t.Fatal("temporary availability changed the credential activation requirement", catalog)
					}
					if !reflect.DeepEqual(catalog.Binding, binding) || catalog.ProviderID != connection.ProviderID || catalog.ConnectionID != connection.ID || catalog.Protocol != connection.Protocol || catalog.UpstreamName != model.UpstreamName {
						t.Fatal("availability projection changed configured route facts", catalog)
					}
				})
			}
		}
	}
}
