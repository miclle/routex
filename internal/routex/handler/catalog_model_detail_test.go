package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func TestAdminModelAndPriceDetailsRejectScopeExpansionBeforeService(t *testing.T) {
	ctrl := New(nil)
	router := fox.New()
	router.RenderErrorFunc = renderAPIError
	router.GET("/models/:model_id", ctrl.GetAdminModel)
	router.GET("/provider-models/:provider_model_id/price", ctrl.GetPrice)
	for _, path := range []string{"/models/mdl_exact", "/provider-models/pmd_exact/price"} {
		for _, query := range []string{"q=one", "cursor=other", "limit=1", "provider_model_id=other", "model_id=other", "provider_id=other", "user_id=private", "price=true", "q=one&q=two", "q=bad%zz"} {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
			if response.Code != http.StatusBadRequest {
				t.Fatal("query expanded exact detail read", path, query, response.Code)
			}
		}
	}
}

func TestAdminModelDetailPreservesExistingBoundedDTO(t *testing.T) {
	modelID := "mdl_exact"
	expires := time.Date(2026, 10, 4, 2, 0, 0, 0, time.UTC)
	row := service.ModelCatalog{Model: entity.Model{ID: modelID, Status: entity.ResourceActive}, Name: "current-name", Names: []entity.ModelName{{Name: "current-name", ModelID: modelID, CurrentModelID: &modelID}, {Name: "old-name", ModelID: modelID, ExpiresAt: &expires}}, Bindings: []service.BindingCatalog{{Binding: entity.ModelProviderBinding{ID: "bnd_exact", ModelID: modelID, ProviderModelID: "pmd_exact", Weight: 0}, ProviderID: "prv_exact", ConnectionID: "con_exact", UpstreamName: "upstream", Protocol: entity.ProtocolOpenAIChat, Ready: false}}, GrantedUserIDs: []string{"usr_granted"}}
	response := modelResponse(row)
	if response.ID != modelID || len(response.Names) != 2 || !response.Names[0].IsCurrent || response.Names[1].IsCurrent || !response.Names[1].ExpiresAt.Equal(expires) || response.Bindings[0].Weight != 0 || response.Bindings[0].Ready || !slices.Equal(response.GrantedUserIDs, row.GrantedUserIDs) {
		t.Fatal("detail projection changed existing Model semantics", response)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !reflect.DeepEqual(keys, []string{"bindings", "config_updated_at", "configured_ready", "created_at", "granted_user_ids", "id", "name", "names", "status"}) {
		t.Fatal("detail exposed uncontracted fields", keys)
	}
	if response.CreatedAt != nil || response.ConfigUpdatedAt != nil || string(object["created_at"]) != "null" || string(object["config_updated_at"]) != "null" {
		t.Fatal("legacy missing Model birth/configuration dates must remain unknown", response)
	}
	if response.ConfiguredReady != nil || string(object["configured_ready"]) != "null" {
		t.Fatal("unavailable or restricted configuration summary must remain unknown")
	}
	var bindings []map[string]json.RawMessage
	if err := json.Unmarshal(object["bindings"], &bindings); err != nil {
		t.Fatal(err)
	}
	keys = keys[:0]
	for key := range bindings[0] {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if !reflect.DeepEqual(keys, []string{"connection_id", "id", "protocol", "provider_id", "provider_model_id", "ready", "upstream_name", "weight"}) {
		t.Fatal("Model route exposed secret or unrelated price fields", keys)
	}
	empty := modelResponse(service.ModelCatalog{Model: entity.Model{ID: modelID}, GrantedUserIDs: []string{}})
	if empty.Names == nil || empty.Bindings == nil || empty.GrantedUserIDs == nil {
		t.Fatal("empty Model detail invented null collections")
	}
}

func TestAdminModelConfiguredReadinessDTOThreeStates(t *testing.T) {
	ready, unavailable := true, false
	for _, scenario := range []struct {
		name  string
		value *bool
		wire  string
	}{{"unknown", nil, "null"}, {"ineligible", &unavailable, "false"}, {"eligible", &ready, "true"}} {
		t.Run(scenario.name, func(t *testing.T) {
			row := service.ModelCatalog{Model: entity.Model{ID: "mdl_exact"}, ConfiguredReady: scenario.value, Bindings: []service.BindingCatalog{{Binding: entity.ModelProviderBinding{ID: "bnd_exact"}, Ready: true}}}
			response := modelResponse(row)
			raw, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				t.Fatal(err)
			}
			if string(object["configured_ready"]) != scenario.wire || !response.Bindings[0].Ready || response.ConfiguredReady != scenario.value {
				t.Fatal("nullable Model summary changed legacy route readiness or lost explicit state")
			}
		})
	}
}
