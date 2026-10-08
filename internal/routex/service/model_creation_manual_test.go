package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestModelCreationManualSelectorExactUnion(t *testing.T) {
	for _, raw := range []string{
		`{"upstream_name":"not/discovered","target":"new","name":"Public"}`,
		`{"upstream_name":"Exact%_Name","target":"existing","model_id":"mdl_target"}`,
		`{"provider_model_id":"pmd_existing","target":"new","name":"Public"}`,
	} {
		var item ModelCreationItem
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			t.Fatal(err)
		}
		if _, err := normalizeModelCreationItems([]ModelCreationItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	for _, raw := range []string{
		`{"upstream_name":"a","provider_model_id":"pmd_existing","target":"new","name":"Public"}`,
		`{"upstream_name":"a","provider_model_id":"","target":"new","name":"Public"}`,
		`{"upstream_name":null,"target":"new","name":"Public"}`,
		`{"upstream_name":" a","target":"new","name":"Public"}`,
		`{"upstream_name":"a\n","target":"new","name":"Public"}`,
		`{"upstream_name":"a","upstream_name":"b","target":"new","name":"Public"}`,
		`{"upstream_name":"a","target":"existing","model_id":"mdl_target","name":"Public"}`,
		`{"upstream_name":"a","target":"new","name":"Public","credential_ready":true}`,
	} {
		var item ModelCreationItem
		if json.Unmarshal([]byte(raw), &item) == nil {
			t.Fatal("invalid selector accepted", raw)
		}
	}
	a := ModelCreationItem{UpstreamName: "Exact", Target: "new", Name: "Public"}
	b := ModelCreationItem{ProviderModelID: "pmd_other", Target: "new", Name: "Other"}
	got, err := normalizeModelCreationItems([]ModelCreationItem{b, a})
	if err != nil || got[0] != a || !modelCreationHasManual(got) {
		t.Fatal(got, err)
	}
	if _, err := normalizeModelCreationItems([]ModelCreationItem{a, a}); err == nil {
		t.Fatal("duplicate manual source")
	}
	if !validManualModelName(entity.ProviderConnection{Protocol: entity.ProtocolOpenAIChat}, "native/name") || validManualModelName(entity.ProviderConnection{Protocol: entity.ProtocolGeminiGenerateContent}, "native/name") || validManualModelName(entity.ProviderConnection{Protocol: entity.ProtocolOpenAIChat, Adapter: entity.AdapterAzureOpenAIClassic}, "native/name") {
		t.Fatal("native path rules lost")
	}
}

func TestModelCreationManualProjectionCannotInventCoverageOrReuseCollision(t *testing.T) {
	for _, mode := range []string{"new", "collision", "failure"} {
		t.Run(mode, func(t *testing.T) {
			f := &creationDefaultsSQL{}
			if mode == "collision" {
				f.pms = []entity.ProviderModel{{ID: "pmd_existing", ConnectionID: "con_one", UpstreamName: "Exact%_Name"}}
			}
			if mode == "failure" {
				f.fail = "provider_models"
			}
			pool := sql.OpenDB(creationDefaultsConnector{f})
			t.Cleanup(func() {
				if err := pool.Close(); err != nil {
					t.Error(err)
				}
			})
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			got, err := modelCreationManualProviderModel(db, entity.ProviderConnection{ID: "con_one", Protocol: entity.ProtocolOpenAIChat}, "Exact%_Name")
			if mode == "new" {
				if err != nil || !reflect.DeepEqual(got, entity.ProviderModel{ConnectionID: "con_one", UpstreamName: "Exact%_Name"}) {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("collision/unknown became new")
			}
			if len(f.queries) != 1 || !strings.Contains(f.queries[0], "LIMIT $3") || !strings.Contains(f.queries[0], "upstream_name") || !strings.Contains(f.queries[0], "connection_id") || strings.Contains(f.queries[0], "LIKE") {
				t.Fatal("unbounded/inexact collision query", f.queries)
			}
			before := len(f.queries)
			if _, err := modelCreationManualProviderModel(db, entity.ProviderConnection{ID: "con_one", Protocol: entity.ProtocolGeminiGenerateContent}, "unsafe/path"); err == nil || len(f.queries) != before {
				t.Fatal("unsafe native name queried")
			}
		})
	}
}

func TestModelCreationManualRequiresIndependentCurrentWrite(t *testing.T) {
	svc, f := routingSQLService(t, 0)
	f.roles.deny["providers.write"] = true
	input := ModelCreationBatchInput{RequestID: "12345678-1234-4234-8234-123456789abc", Reason: "Explicit manual configuration", Items: []ModelCreationItem{{UpstreamName: "not-discovered", Target: "new", Name: "Public"}}}
	_, err := svc.CreateModelBatch(context.Background(), "usr_admin", "con_one", strings.Repeat("a", 64), input)
	if !errors.Is(err, apperrors.ErrForbidden) || len(f.roles.writes) != 0 || len(f.queries) != 0 {
		t.Fatal("manual write crossed independent authority", err, f.queries, f.roles.writes)
	}
}

func TestModelCreationManualPublicationDoesNotProveCredentialEligibility(t *testing.T) {
	svc, state := modelCreationPublicationFixture()
	state.Credentials[0].Access = nil
	svc.runtime.auth.Load().CredentialAccess["crd_one"] = map[string]bool{}
	svc.runtime.routes.Load().Models["mdl_one"][0].Credentials = nil
	if modelCreationReady(state.Credentials, "pmd_one") || !svc.modelCreationRuntimeApplied(state) {
		t.Fatal("configured unavailable route confused with coverage")
	}
	svc.runtime.routes.Load().Models["mdl_one"][0].Credentials = []runtimeCredential{{ID: "crd_one", CreatedAt: state.Credentials[0].CreatedAt, CipherHash: state.Credentials[0].CipherHash}}
	if svc.modelCreationRuntimeApplied(state) {
		t.Fatal("unproven credential became published eligibility")
	}
}

func TestModelCreationManualReceiptPreservesExactSourceAndLegacyWire(t *testing.T) {
	_, state := modelCreationPublicationFixture()
	row := ModelCreationReceiptItem{ManualUpstreamName: "not/discovered", ProviderModelID: "pmd_one", ModelID: "mdl_one", BindingID: "bnd_one", CreatedModel: true, Name: "Public", Protocol: entity.ProtocolOpenAIChat, Weight: 100}
	raw, err := encodeModelCreationSnapshot("con_one", []ModelCreationReceiptItem{row}, state)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := readModelCreationSnapshot(entity.ModelCreationBatchReceipt{ConnectionID: "con_one", SnapshotJSON: raw})
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0] != row {
		t.Fatal(snapshot, err)
	}
	before := modelCreationIntentHash("usr_actor", "con_one", strings.Repeat("a", 64), ModelCreationBatchInput{Items: []ModelCreationItem{{UpstreamName: "not/discovered", Target: "new", Name: "Public"}}})
	changed := modelCreationIntentHash("usr_actor", "con_one", strings.Repeat("a", 64), ModelCreationBatchInput{Items: []ModelCreationItem{{UpstreamName: "Not/discovered", Target: "new", Name: "Public"}}})
	if before == changed {
		t.Fatal("manual spelling omitted from intent")
	}
	for _, name := range []string{" padded ", "bad\n", strings.Repeat("x", 256)} {
		row.ManualUpstreamName = name
		encoded, err := json.Marshal(modelCreationSnapshot{ConnectionID: "con_one", Items: []ModelCreationReceiptItem{row}, StateHash: strings.Repeat("a", 64), Topology: snapshot.Topology})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := readModelCreationSnapshot(entity.ModelCreationBatchReceipt{ConnectionID: "con_one", SnapshotJSON: string(encoded)}); err == nil {
			t.Fatal("unsafe retained source accepted")
		}
	}
	row.ManualUpstreamName = ""
	legacy, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), "manual_upstream_name") {
		t.Fatal("legacy receipt changed")
	}
}

func TestModelCreationManualMaximumASCIIReceiptFitsExistingPortableBound(t *testing.T) {
	_, state := modelCreationPublicationFixture()
	original := state.Models[0]
	state.Models = nil
	state.ProviderModels = nil
	items := make([]ModelCreationReceiptItem, 0, 50)
	for i := range 50 {
		suffix := fmt.Sprintf("%026d", i)
		mid, pid, bid := "mdl_"+suffix, "pmd_"+suffix, "bnd_"+suffix
		name := strings.Repeat("n", 126) + fmt.Sprintf("%02d", i)
		native := strings.Repeat("u", 253) + fmt.Sprintf("%02d", i)
		model := original
		model.Model.ID = mid
		model.Name = entity.ModelName{Name: name, ModelID: mid, CurrentModelID: &mid}
		topology := original.Topology[0]
		topology.Binding.ID = bid
		topology.Binding.ModelID = mid
		topology.Binding.ProviderModelID = pid
		topology.ProviderModel.ID = pid
		topology.ProviderModel.UpstreamName = native
		model.Topology = []modelCreationTopology{topology}
		state.Models = append(state.Models, model)
		state.ProviderModels = append(state.ProviderModels, topology.ProviderModel)
		items = append(items, ModelCreationReceiptItem{ManualUpstreamName: native, ProviderModelID: pid, ModelID: mid, BindingID: bid, CreatedModel: true, Name: name, Protocol: entity.ProtocolOpenAIChat, Weight: 100})
	}
	raw, err := encodeModelCreationSnapshot("con_one", items, state)
	if err != nil || len(raw) >= 60*1024 {
		t.Fatal("manual fifty-row receipt exceeds existing bound", len(raw), err)
	}
	snapshot, err := readModelCreationSnapshot(entity.ModelCreationBatchReceipt{ConnectionID: "con_one", SnapshotJSON: raw})
	if err != nil || !reflect.DeepEqual(snapshot.Items, items) || len(snapshot.Topology) != 50 {
		t.Fatal("manual exact sources lost", err)
	}
}
