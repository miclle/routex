package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func providerBindingsTestGraph() ([]entity.ProviderConnection, []entity.ProviderModel, []entity.ModelProviderBinding, []entity.Model, []entity.ModelName) {
	id := "mdl_a"
	return []entity.ProviderConnection{{ID: "con_a", ProviderID: "prv_target"}}, []entity.ProviderModel{{ID: "pmd_empty", ConnectionID: "con_a"}, {ID: "pmd_a", ConnectionID: "con_a", Disabled: true}}, []entity.ModelProviderBinding{{ID: "bnd_a", ProviderModelID: "pmd_a", ModelID: id, Weight: 0}, {ID: "bnd_b", ProviderModelID: "pmd_a", ModelID: "mdl_b", Weight: 100}}, []entity.Model{{ID: id, Status: "disabled"}, {ID: "mdl_b", Status: "active"}}, []entity.ModelName{{Name: "current-a", ModelID: id, CurrentModelID: &id}}
}
func TestProviderModelBindingsCompleteStoredFactsAndWire(t *testing.T) {
	c, p, b, m, n := providerBindingsTestGraph()
	got, err := projectProviderModelBindings("prv_target", c, p, b, m, n)
	if err != nil || len(got.Items) != 2 || got.Items[0].BindingCount != 2 || *got.Items[0].Models[0].Name != "current-a" || got.Items[0].Models[1].Name != nil || got.Items[1].BindingCount != 0 || got.Items[1].Models == nil {
		t.Fatal("stored zero/disabled/unknown/empty facts changed", got, err)
	}
	encoded, _ := json.Marshal(got)
	var page map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &page)
	if len(page) != 2 {
		t.Fatal("page leaked fields", string(encoded))
	}
	var items []map[string]json.RawMessage
	_ = json.Unmarshal(page["items"], &items)
	for _, r := range items {
		if len(r) != 4 {
			t.Fatal("row leaked fields", r)
		}
		var models []map[string]json.RawMessage
		_ = json.Unmarshal(r["models"], &models)
		for _, v := range models {
			if len(v) != 2 {
				t.Fatal("model leaked fields", v)
			}
		}
	}
	empty, err := projectProviderModelBindings("prv_target", nil, nil, nil, nil, nil)
	raw, _ := json.Marshal(empty)
	if err != nil || string(raw) != `{"provider_id":"prv_target","items":[]}` {
		t.Fatal(string(raw), err)
	}
}
func TestProviderModelBindingsRejectIncompleteAliasedDuplicateGraph(t *testing.T) {
	for _, which := range []string{"connection_alias", "connection_duplicate", "connection_parent", "connection_missing", "pm_alias", "pm_duplicate", "pm_parent", "pm_missing", "binding_id", "binding_duplicate", "binding_pair_duplicate", "binding_pm_alias", "binding_pm_orphan", "binding_model_id", "binding_weight", "model_missing", "model_duplicate", "model_alias", "name_alias", "name_orphan", "name_duplicate", "name_not_current"} {
		t.Run(which, func(t *testing.T) {
			c, p, b, m, n := providerBindingsTestGraph()
			switch which {
			case "connection_alias":
				c[0].ID = "CON_a"
			case "connection_duplicate":
				c = append(c, c[0])
			case "connection_parent":
				c[0].ProviderID = "PRV_target"
			case "connection_missing":
				c = nil
			case "pm_alias":
				p[1].ID = "PMD_a"
			case "pm_duplicate":
				p = append(p, p[1])
			case "pm_parent":
				p[1].ConnectionID = "con_a "
			case "pm_missing":
				p = p[:1]
			case "binding_id":
				b[0].ID = "bnd_a "
			case "binding_duplicate":
				b = append(b, b[0])
			case "binding_pair_duplicate":
				v := b[0]
				v.ID = "bnd_other"
				b = append(b, v)
			case "binding_pm_alias":
				b[0].ProviderModelID = "PMD_a"
			case "binding_pm_orphan":
				b[0].ProviderModelID = "pmd_missing"
			case "binding_model_id":
				b[0].ModelID = "mdl_a "
			case "binding_weight":
				b[0].Weight = 101
			case "model_missing":
				m = m[:1]
			case "model_duplicate":
				m = append(m, m[0])
			case "model_alias":
				m[0].ID = "MDL_a"
			case "name_alias":
				v := "MDL_a"
				n[0].CurrentModelID = &v
			case "name_orphan":
				v := "mdl_other"
				n[0].CurrentModelID = &v
				n[0].ModelID = v
			case "name_duplicate":
				n = append(n, n[0])
			case "name_not_current":
				n[0].CurrentModelID = nil
			}
			if got, err := projectProviderModelBindings("prv_target", c, p, b, m, n); err != providerModelBindingsUnavailable || got != nil {
				t.Fatal("corrupt graph became complete projection", got, err)
			}
		})
	}
	for _, label := range []string{"old alias with spaces", "bad\nname", strings.Repeat("x", 129), string([]byte{255})} {
		c, p, b, m, n := providerBindingsTestGraph()
		n[0].Name = label
		got, err := projectProviderModelBindings("prv_target", c, p, b, m, n)
		if err != nil || got.Items[0].Models[0].Name != nil {
			t.Fatal("invented recorded name", label, err)
		}
	}
}
func TestProviderModelBindingsMaximumCompleteAndOverflow(t *testing.T) {
	c, p, _, _, _ := providerBindingsTestGraph()
	p = p[1:]
	b := make([]entity.ModelProviderBinding, providerModelBindingsLimit)
	m := make([]entity.Model, providerModelBindingsLimit)
	for i := range b {
		model := fmt.Sprintf("mdl_%05d", i)
		b[i] = entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_%05d", i), ProviderModelID: "pmd_a", ModelID: model}
		m[i] = entity.Model{ID: model}
	}
	got, err := projectProviderModelBindings("prv_target", c, p, b, m, nil)
	if err != nil || got.Items[0].BindingCount != 10000 || len(got.Items[0].Models) != 10000 || got.Items[0].Models[9999].ID != "mdl_09999" {
		t.Fatal("supported complete bound lost", err)
	}
	for _, length := range []int{10001, 20000} {
		if err := providerBindingsBound(length); err != providerModelBindingsOverflow {
			t.Fatal("truncation accepted", length)
		}
	}
	if _, err := projectProviderModelBindings("prv_target", c, p, append(b, b[0]), m, nil); err != providerModelBindingsOverflow {
		t.Fatal("overflow concealed", err)
	}
	if !reflect.DeepEqual(p, []entity.ProviderModel{{ID: "pmd_a", ConnectionID: "con_a", Disabled: true}}) {
		t.Fatal("projection mutated input")
	}
}
