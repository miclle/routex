package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
)

func TestAdminModelRecordedMetadataDTORequiresBothNullableFields(t *testing.T) {
	birth := time.Date(2026, 10, 1, 8, 0, 0, 123000000, time.FixedZone("fixture", 8*60*60))
	for _, scenario := range []string{"unknown", "legacy_birth", "recorded"} {
		t.Run(scenario, func(t *testing.T) {
			model := entity.Model{ID: "mdl_recorded", Status: entity.ResourceActive}
			if scenario != "unknown" {
				model.CreatedAt = birth
			}
			if scenario == "recorded" {
				model.ConfigUpdatedAt = &birth
			}
			response := modelResponse(service.ModelCatalog{Model: model, GrantedUserIDs: []string{"usr_retained"}})
			raw, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"created_at", "config_updated_at"} {
				value, present := wire[field]
				if !present {
					t.Fatal("current backend omitted mandatory nullable recorded field", field)
				}
				known := scenario == "recorded" || scenario == "legacy_birth" && field == "created_at"
				if !known {
					if string(value) != "null" {
						t.Fatal("unknown metadata received a fabricated timestamp", field)
					}
					continue
				}
				var stamp time.Time
				if err := json.Unmarshal(value, &stamp); err != nil || !stamp.Equal(birth) || stamp.Location() != time.UTC {
					t.Fatal("recorded instant lost UTC wire precision", field, err)
				}
			}
			if len(response.GrantedUserIDs) != 1 || response.GrantedUserIDs[0] != "usr_retained" {
				t.Fatal("recorded metadata changed retained Personal grantees")
			}
		})
	}
}
