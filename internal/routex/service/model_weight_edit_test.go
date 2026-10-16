package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelWeightEditClosedCompleteInput(t *testing.T) {
	good := `{"weights":[{"binding_id":"bnd_exact","weight":100}]}`
	var input ModelWeightEditInput
	if err := json.Unmarshal([]byte(good), &input); err != nil || len(input.Weights) != 1 || input.Weights[0].BindingID != "bnd_exact" || input.Weights[0].Weight != 100 {
		t.Fatal(input, err)
	}
	for _, raw := range []string{
		`null`, `{}`, `{"weights":null}`, `{"weights":[]}`, `{"weights":[],"weights":[]}`,
		strings.Replace(good, `"weight":100`, `"weight":100,"weight":0`, 1),
		strings.Replace(good, `"weight":100`, `"weight":null`, 1),
		strings.Replace(good, `"weight":100`, `"weight":1.5`, 1),
		strings.Replace(good, `"weight":100`, `"weight":101`, 1),
		strings.Replace(good, `"weight":100`, `"weight":100,"secret":"hidden"`, 1),
		strings.Replace(good, `"binding_id":"bnd_exact"`, `"binding_id":"BND_exact"`, 1),
		good + `{}`, strings.Replace(good, `}]}`, `}],"actor":"usr_other"}`, 1),
	} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("malformed reviewed weight input accepted", raw)
		}
	}
}

func TestModelWeightEditProofBindsActorCurrentWeightsAndSource(t *testing.T) {
	actor := entity.User{ID: "usr_exact", CreatedAt: time.Now().UTC()}
	state := weightTestState()
	modelID := state.Model.ID
	state.Names = []entity.ModelName{{ModelID: modelID, CurrentModelID: &modelID, Name: "recorded-model", CreatedAt: state.Model.CreatedAt}}
	if state.currentEligibilityHash() == "" {
		t.Fatal("fixture must include the current Model name for source eligibility")
	}
	token := modelWeightEditETag(actor, true, state)
	if len(token) != 64 || token != modelWeightEditETag(actor, true, state) {
		t.Fatal("unstable current review")
	}
	other := actor
	other.ID = "usr_other"
	if token == modelWeightEditETag(other, true, state) || token == modelWeightEditETag(actor, false, state) {
		t.Fatal("review borrowed authority")
	}
	state.Rows[0].Weight++
	if token == modelWeightEditETag(actor, true, state) {
		t.Fatal("review ignored concurrent same-topology weight change")
	}
	state.Rows[0].Weight--
	state.SourceDenialProof = "changed-source-denial"
	if token == modelWeightEditETag(actor, true, state) {
		t.Fatal("review ignored permanent source denial")
	}
	state.SourceDenialProof = ""
	state.Connections[0].TransportGeneration = "1"
	if token == modelWeightEditETag(actor, true, state) {
		t.Fatal("review ignored source transport revision")
	}
}
