package service

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

func catalogPricesFixture(t *testing.T) (*Service, *memberModelsData, []MemberModelCatalogRecord) {
	t.Helper()
	svc, data, _ := memberModelsProjectionFixture(t)
	data.Prices = []entity.ModelPrice{{ID: "prc_catalog", ProviderModelID: "pmd_one"}}
	data.Rates = []entity.PriceRate{
		{ModelPriceID: "prc_catalog", Metric: pricing.Input, Tier: pricing.Base, Unit: pricing.Unit, Currency: "USD", Amount: "0", Enabled: true},
		{ModelPriceID: "prc_catalog", Metric: pricing.Output, Tier: pricing.Base, Unit: pricing.Unit, Currency: "CNY", Amount: "999999999999999999.123456789012345678", Enabled: true},
	}
	return svc, data, []MemberModelCatalogRecord{{ID: data.Models[0].ID, Name: data.Names[0].Name}}
}
func TestMemberCatalogPricesExactDecimalAndPublicBoundary(t *testing.T) {
	svc, data, items := catalogPricesFixture(t)
	before, _ := json.Marshal(data)
	svc.applyMemberCatalogPrices(items, data)
	if items[0].InputPrice.State != "priced" || items[0].InputPrice.Rate.Amount != "0" || items[0].OutputPrice.Rate.Amount != "999999999999999999.123456789012345678" || items[0].OutputPrice.Rate.Currency != "CNY" {
		t.Fatal(items)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"Provider One", "pmd_one", "con_one", "prv_one", "credential", "ciphertext", "provider_id", "binding_id", "etag"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("private topology entered public prices", forbidden)
		}
	}
	after, _ := json.Marshal(data)
	if !slices.Equal(before, after) {
		t.Fatal("price projection mutated its input")
	}
	data.Rates[1].Amount = "0"
	if items[0].OutputPrice.Rate.Amount != "999999999999999999.123456789012345678" {
		t.Fatal("earlier observation aliases mutable stored rates")
	}
}
func TestMemberCatalogPricesAuthorityAndPublicationFences(t *testing.T) {
	for _, kind := range []string{"permission", "nil_runtime", "nil_auth", "nil_routes", "expired", "digest", "birth", "denied", "busy", "no_ready", "no_price", "disabled", "invalid_amount"} {
		t.Run(kind, func(t *testing.T) {
			svc, data, items := catalogPricesFixture(t)
			want := "unavailable"
			switch kind {
			case "permission":
				data.PricesRead = false
				want = "unauthorized"
			case "nil_runtime":
				svc.runtime = nil
			case "nil_auth":
				svc.runtime.auth.Store(nil)
			case "nil_routes":
				svc.runtime.routes.Store(nil)
			case "expired":
				svc.runtime.auth.Load().ValidUntil = time.Now().Add(-time.Second)
			case "digest":
				svc.runtime.auth.Load().SourceDigest = "different"
			case "birth":
				data.Models[0].CreatedAt = data.Models[0].CreatedAt.Add(time.Second)
			case "denied":
				svc.InvalidateRuntimeModel(data.Models[0].ID)
			case "busy":
				svc.runtime.mu.Lock()
				defer svc.runtime.mu.Unlock()
			case "no_ready":
				svc.InvalidateRuntimeCredential("crd_one")
			case "no_price":
				data.Rates = nil
				want = "missing"
			case "disabled":
				for i := range data.Rates {
					data.Rates[i].Enabled = false
				}
				want = "disabled"
			case "invalid_amount":
				data.Rates[0].Amount = "1e3"
				want = "heterogeneous"
			}
			svc.applyMemberCatalogPrices(items, data)
			if items[0].InputPrice.State != want || (want == "disabled") != (items[0].InputPrice.Rate != nil) {
				t.Fatal(kind, items[0].InputPrice)
			}
		})
	}
}
func TestMemberCatalogPricesCompleteRouteSetAcrossProtocols(t *testing.T) {
	for _, kind := range []string{"equal", "amount", "currency", "disabled", "missing", "zero_weight", "not_ready"} {
		t.Run(kind, func(t *testing.T) {
			svc, data, items := catalogPricesFixture(t)
			// A separate native protocol is part of the same public logical Model price.
			published := &runtimeData{Models: data.Models, Names: data.Names, Providers: slices.Clone(data.Providers), Bindings: slices.Clone(data.Bindings), ProviderModels: slices.Clone(data.ProviderModels), Connections: slices.Clone(data.Connections), Credentials: slices.Clone(data.Credentials), Access: slices.Clone(data.Access), EgressSetting: data.EgressSetting}
			pm := published.ProviderModels[0]
			pm.ID = "pmd_second"
			pm.ConnectionID = "con_second"
			connection := published.Connections[0]
			connection.ID = "con_second"
			connection.Protocol = entity.ProtocolAnthropicMessages
			credential := published.Credentials[0]
			credential.ID = "crd_second"
			credential.ConnectionID = connection.ID
			binding := published.Bindings[0]
			binding.ID = "mpb_second"
			binding.ProviderModelID = pm.ID
			if kind == "zero_weight" {
				binding.Weight = 0
			}
			if kind == "not_ready" {
				credential.Enabled = false
			}
			published.ProviderModels = append(published.ProviderModels, pm)
			published.Connections = append(published.Connections, connection)
			published.Credentials = append(published.Credentials, credential)
			published.Bindings = append(published.Bindings, binding)
			published.Access = append(published.Access, entity.CredentialModelAccess{CredentialID: credential.ID, ProviderModelID: pm.ID})
			data.ProviderModels = published.ProviderModels
			data.Connections = published.Connections
			data.Credentials = published.Credentials
			data.Bindings = published.Bindings
			data.Access = published.Access
			auth := buildRuntimeAuthorization(published, time.Now().Add(time.Minute))
			auth.SourceDigest = "catalog"
			first := svc.runtime.routes.Load().Models[items[0].ID][0]
			second := first
			second.Route.BindingID, second.Route.ProviderModelID = binding.ID, pm.ID
			second.Route.ConnectionID, second.Route.Protocol, second.Route.Weight = connection.ID, connection.Protocol, binding.Weight
			second.Route.EgressRevision = auth.ConnectionRevisions[connection.ID]
			second.Credentials = []runtimeCredential{{ID: credential.ID}}
			routes := map[string][]runtimeRoute{items[0].ID: {first, second}}
			svc.runtime.auth.Store(auth)
			svc.runtime.routes.Store(&runtimeRoutes{Digest: "catalog", Models: routes})
			data.Prices = append(data.Prices, entity.ModelPrice{ID: "prc_second", ProviderModelID: pm.ID})
			rate := data.Rates[0]
			rate.ModelPriceID = "prc_second"
			want := "heterogeneous"
			switch kind {
			case "equal":
				want = "priced"
			case "amount":
				rate.Amount = "1"
			case "currency":
				rate.Currency = "CNY"
			case "disabled":
				rate.Enabled = false
			case "zero_weight", "not_ready":
				rate.Amount = "1"
				want = "priced"
			}
			if kind != "missing" {
				data.Rates = append(data.Rates, rate)
			}
			svc.applyMemberCatalogPrices(items, data)
			if items[0].InputPrice.State != want {
				t.Fatal(kind, items[0].InputPrice)
			}
			if want == "priced" && !reflect.DeepEqual(items[0].InputPrice.Rate, &MemberModelBaseRate{Amount: "0", Currency: "USD", Unit: pricing.Unit}) {
				t.Fatal(items)
			}
		})
	}
}
