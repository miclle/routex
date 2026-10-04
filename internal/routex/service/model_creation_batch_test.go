package service

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelCreationBatchStrictIntent(t *testing.T) {
	good := `{"request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":" reviewed creation ","items":[{"provider_model_id":"pmd_one","target":"new","name":"Exact/Model"},{"provider_model_id":"pmd_two","target":"existing","model_id":"mdl_existing"}]}`
	var input ModelCreationBatchInput
	if err := json.Unmarshal([]byte(good), &input); err != nil || input.Reason != "reviewed creation" {
		t.Fatal(input, err)
	}
	for _, bad := range []string{
		strings.Replace(good, `"reason":" reviewed creation "`, `"reason":null`, 1),
		strings.Replace(good, `"reason":" reviewed creation "`, `"reason":"x","reason":"x"`, 1),
		strings.Replace(good, `"items":`, `"grants":[],"items":`, 1),
		strings.Replace(good, `"target":"new"`, `"target":"new","weight":100`, 1),
		strings.Replace(good, `"target":"new"`, `"target":"new","model_id":""`, 1),
		strings.Replace(good, `"target":"existing"`, `"target":"existing","name":""`, 1),
		strings.Replace(good, "pmd_two", "pmd_one", 1),
		strings.Replace(good, "pmd_one", "PMD_one", 1),
		strings.Replace(good, "Exact/Model", " bad ", 1),
		strings.Replace(good, "Exact/Model", strings.Repeat("x", 129), 1),
		strings.Replace(good, "reviewed creation", strings.Repeat("x", 1025), 1),
		strings.Replace(good, " reviewed creation ", `x\n`, 1),
		strings.Replace(good, "e26c3a0b-cfee-4391-bafd-7858fbf997ee", "e26c3a0b-cfee-3391-bafd-7858fbf997ee", 1),
		strings.Replace(good, " reviewed creation ", `\ud800`, 1),
		good + good, "null", `{"request_id":"e26c3a0b-cfee-4391-bafd-7858fbf997ee","reason":"x","items":[]}`,
		strings.Replace(good, "reviewed creation", string([]byte{255}), 1),
	} {
		var rejected ModelCreationBatchInput
		if err := json.Unmarshal([]byte(bad), &rejected); err == nil {
			t.Fatalf("unsupported intent accepted: %q", bad)
		}
	}
	reversed := input
	reversed.Items = slices.Clone(input.Items)
	slices.Reverse(reversed.Items)
	reversed, err := normalizeModelCreationBatch(reversed)
	if err != nil {
		t.Fatal(err)
	}
	hash := modelCreationIntentHash("usr_actor", "con_one", strings.Repeat("a", 64), input)
	if hash != modelCreationIntentHash("usr_actor", "con_one", strings.Repeat("a", 64), reversed) {
		t.Fatal("paging order changed intent")
	}
	for _, hash2 := range []string{modelCreationIntentHash("usr_other", "con_one", strings.Repeat("a", 64), input), modelCreationIntentHash("usr_actor", "con_two", strings.Repeat("a", 64), input), modelCreationIntentHash("usr_actor", "con_one", strings.Repeat("b", 64), input)} {
		if hash == hash2 {
			t.Fatal("actor/path/review were not bound")
		}
	}
}
func TestModelCreationBatchPreservesConfiguredWeights(t *testing.T) {
	for _, test := range []struct {
		name     string
		weights  []int
		disabled bool
		provider string
		weight   int
		blocker  string
	}{
		{name: "first protocol", weight: 100},
		{name: "backup", weights: []int{100}, weight: 0},
		{name: "disabled configured hundred", weights: []int{100}, disabled: true, weight: 0},
		{name: "split configured hundred", weights: []int{40, 60}, weight: 0},
		{name: "all zero", weights: []int{0, 0}, weight: 0, blocker: "protocol_weights_invalid"},
		{name: "malformed", weights: []int{-1, 101}, weight: 0, blocker: "protocol_weights_invalid"},
		{name: "same supplier protocol", weights: []int{100}, provider: "prv_selected", weight: 0, blocker: "provider_protocol_duplicate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := modelCreationModelProof{Model: entity.Model{Status: entity.ResourceActive}}
			for _, weight := range test.weights {
				model.Topology = append(model.Topology, modelCreationTopology{Binding: entity.ModelProviderBinding{Weight: weight}, ProviderModel: entity.ProviderModel{Disabled: test.disabled}, Connection: entity.ProviderConnection{ProviderID: test.provider}})
			}
			before := personalHash(model)
			weight, blockers := modelCreationWeight(model, "prv_selected")
			if weight != test.weight || test.blocker != "" && !slices.Contains(blockers, test.blocker) || test.blocker == "" && len(blockers) != 0 || personalHash(model) != before {
				t.Fatal(weight, blockers, "saved weights mutated")
			}
		})
	}
}
func TestModelCreationBatchCoverageIsCompleteAndExact(t *testing.T) {
	credentials := []modelCreationCredentialProof{{ID: "crd_one", Enabled: true, VerificationStatus: "verified", Access: []string{"pmd_one"}}, {ID: "crd_two", Enabled: true, VerificationStatus: "verified", Access: []string{}}}
	if modelCreationReady(nil, "pmd_one") || modelCreationReady(credentials, "pmd_one") || modelCreationReady(credentials, "PMD_one") {
		t.Fatal("missing coverage became ready")
	}
	credentials[1].Enabled = false
	if !modelCreationReady(credentials, "pmd_one") {
		t.Fatal("disabled Credential blocked actual eligible coverage")
	}
}
func TestModelCreationBatchQueryStatementsStayIndependent(t *testing.T) {
	db, err := gorm.Open(projectQuotaScopeDialector{}, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	original := personalExact(db, "id", "mdl_one").Clauses(clause.Locking{Strength: "UPDATE"})
	query := personalExact(modelCreationDB(original), "id", "con_one")
	query.Statement.Clauses["WHERE"].Build(query.Statement)
	if len(query.Statement.Vars) != 1 || query.Statement.Vars[0] != "con_one" || strings.Contains(query.Statement.SQL.String(), "mdl_one") {
		t.Fatal("initialized Model query leaked into Connection capture", query.Statement.SQL.String(), query.Statement.Vars)
	}
	if original.Statement.ConnPool != query.Statement.ConnPool {
		t.Fatal("fresh query lost transaction connection")
	}
}
func TestModelCreationBatchPickerBounds(t *testing.T) {
	for _, f := range []ModelCreationFilter{{Limit: -1}, {Limit: 51}, {Query: strings.Repeat("x", 201)}, {Cursor: "CON_one"}, {Cursor: "mdl_other"}} {
		if _, _, err := normalizeModelCreationFilter(f, "con"); err == nil {
			t.Fatal("invalid bounded picker", f)
		}
	}
	f, pattern, err := normalizeModelCreationFilter(ModelCreationFilter{Query: "_%!"}, "con")
	if err != nil || f.Limit != 20 || pattern != "%!_!%!!%" {
		t.Fatal(f, pattern, err)
	}
}

func TestModelCreationBatchReviewBindsActorAndStableContext(t *testing.T) {
	context := &modelCreationState{Connection: entity.ProviderConnection{ID: "con_one"}, ReservedNames: []*entity.ModelName{nil}}
	first := modelCreationReviewHash("usr_one", personalHash(context))
	if first != modelCreationReviewHash("usr_one", personalHash(context)) || first == modelCreationReviewHash("usr_two", personalHash(context)) {
		t.Fatal("review was not stable and actor-bound")
	}
	context.ReservedNames[0] = &entity.ModelName{Name: "reserved", ModelID: "mdl_other"}
	if first == modelCreationReviewHash("usr_one", personalHash(context)) {
		t.Fatal("explicit reserved-name absence was omitted")
	}
}

func TestModelCreationBatchUnicodeEscapesStayExact(t *testing.T) {
	for _, raw := range []string{`"\ud800"`, `"\udfff"`, `"\ud800x"`, `"\ud800\u0041"`} {
		if modelCreationUnicode([]byte(raw)) {
			t.Fatal("unpaired JSON surrogate accepted", raw)
		}
	}
	for _, raw := range []string{`"\ud83d\ude00"`, `"\\ud800"`, `"Unicode 字"`} {
		if !modelCreationUnicode([]byte(raw)) {
			t.Fatal("valid user string rejected", raw)
		}
	}
}

func TestModelCreationBatchFiftyRowReceiptFitsPortableText(t *testing.T) {
	_, state := modelCreationPublicationFixture()
	items := []ModelCreationReceiptItem{}
	original := state.Models[0]
	state.Models = nil
	state.ProviderModels = nil
	for index := range 50 {
		suffix := fmt.Sprintf("%026d", index)
		modelID, pmID, bindingID := "mdl_"+suffix, "pmd_"+suffix, "bnd_"+suffix
		name := strings.Repeat("n", 100) + fmt.Sprintf("%02d", index)
		model := original
		model.Model.ID = modelID
		model.Name.Name = name
		model.Name.ModelID = modelID
		model.Name.CurrentModelID = &modelID
		topology := original.Topology[0]
		topology.Binding.ID = bindingID
		topology.Binding.ModelID = modelID
		topology.Binding.ProviderModelID = pmID
		topology.ProviderModel.ID = pmID
		model.Topology = []modelCreationTopology{topology}
		state.Models = append(state.Models, model)
		state.ProviderModels = append(state.ProviderModels, topology.ProviderModel)
		items = append(items, ModelCreationReceiptItem{ProviderModelID: pmID, ModelID: modelID, BindingID: bindingID, CreatedModel: true, Name: name, Protocol: entity.ProtocolOpenAIChat, Weight: 100})
	}
	raw, err := encodeModelCreationSnapshot("con_one", items, state)
	if err != nil || len(raw) >= 60*1024 {
		t.Fatal("supported50-row batch exceeds portable TEXT", len(raw), err)
	}
	receipt := entity.ModelCreationBatchReceipt{ConnectionID: "con_one", SnapshotJSON: raw}
	snapshot, err := readModelCreationSnapshot(receipt)
	if err != nil || len(snapshot.Items) != 50 || len(snapshot.Topology) != 50 || snapshot.StateHash != personalHash(state) {
		t.Fatal("receipt lost original complete configuration", err)
	}
	snapshot.Topology[0].Weight = 0
	corrupt, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	receipt.SnapshotJSON = string(corrupt)
	if _, err := readModelCreationSnapshot(receipt); err == nil {
		t.Fatal("malformed original protocol topology accepted")
	}
}
