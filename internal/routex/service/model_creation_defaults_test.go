package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestModelCreationInitialTargetsPreserveExactReservationsAndWeights(t *testing.T) {
	current := entity.ProviderConnection{ID: "con_current", ProviderID: "prv_current", Protocol: entity.ProtocolOpenAIChat}
	old := entity.ProviderConnection{ID: "con_old", ProviderID: "prv_old", Protocol: entity.ProtocolOpenAIChat}
	mid := "mdl_old"
	names := []string{"fresh", "same", "alias"}
	reservations := []entity.ModelName{{Name: "same", ModelID: mid, CurrentModelID: &mid}, {Name: "alias", ModelID: mid}}
	models := []entity.Model{{ID: mid, Status: entity.ResourceActive}}
	bindings := []entity.ModelProviderBinding{{ID: "bnd_old", ModelID: mid, ProviderModelID: "pmd_old", Weight: 100}}
	pms := []entity.ProviderModel{{ID: "pmd_old", ConnectionID: old.ID}}
	before := personalHash([]any{reservations, models, bindings, pms, old})
	got, err := projectModelCreationDefaults(names, reservations, models, bindings, pms, []entity.ProviderConnection{old}, current)
	if err != nil || got["fresh"] == nil || got["fresh"].Target != "new" || got["fresh"].Name != "fresh" || got["same"] == nil || got["same"].Target != "existing" || got["same"].ModelID != mid || *got["same"].InitialWeight != 0 || got["alias"] != nil {
		t.Fatal(got, err)
	}
	if personalHash([]any{reservations, models, bindings, pms, old}) != before {
		t.Fatal("initial assistance changed retained topology")
	}
	old.Protocol = entity.ProtocolOpenAIResponses
	got, err = projectModelCreationDefaults(names, reservations, models, bindings, pms, []entity.ProviderConnection{old}, current)
	if err != nil || *got["same"].InitialWeight != 100 {
		t.Fatal("first protocol did not receive100", got, err)
	}
	old.Protocol = current.Protocol
	old.ProviderID = current.ProviderID
	got, err = projectModelCreationDefaults(names, reservations, models, bindings, pms, []entity.ProviderConnection{old}, current)
	if err != nil || got["same"] != nil {
		t.Fatal("same Provider/protocol target suggested", got, err)
	}
	old.ProviderID = "prv_old"
	bindings[0].Weight = 0
	got, err = projectModelCreationDefaults(names, reservations, models, bindings, pms, []entity.ProviderConnection{old}, current)
	if err != nil || got["same"] != nil {
		t.Fatal("invalid configured total suggested", got, err)
	}
	bindings[0].Weight = 100
	models[0].Status = entity.ResourceArchived
	got, err = projectModelCreationDefaults(names, reservations, models, bindings, pms, []entity.ProviderConnection{old}, current)
	if err != nil || got["same"] != nil {
		t.Fatal("inactive target suggested", got, err)
	}
}

func TestModelCreationInitialTargetsRejectIncompleteOrAliasedSnapshot(t *testing.T) {
	mid := "mdl_old"
	current := entity.ProviderConnection{ProviderID: "prv_current", Protocol: entity.ProtocolOpenAIChat}
	name := entity.ModelName{Name: "same", ModelID: mid, CurrentModelID: &mid}
	model := entity.Model{ID: mid, Status: entity.ResourceActive}
	binding := entity.ModelProviderBinding{ID: "bnd_old", ModelID: mid, ProviderModelID: "pmd_old", Weight: 100}
	pm := entity.ProviderModel{ID: "pmd_old", ConnectionID: "con_old"}
	c := entity.ProviderConnection{ID: "con_old", ProviderID: "prv_old", Protocol: current.Protocol}
	for _, kind := range []string{"alias-name", "duplicate-name", "wrong-owner", "duplicate-model", "missing-model", "foreign-binding", "duplicate-binding", "missing-pm", "alias-pm", "missing-connection", "alias-connection", "over-bound"} {
		t.Run(kind, func(t *testing.T) {
			ns := []entity.ModelName{name}
			ms := []entity.Model{model}
			bs := []entity.ModelProviderBinding{binding}
			ps := []entity.ProviderModel{pm}
			cs := []entity.ProviderConnection{c}
			switch kind {
			case "alias-name":
				ns[0].Name = "SAME"
			case "duplicate-name":
				ns = append(ns, name)
			case "wrong-owner":
				other := "mdl_other"
				ns[0].CurrentModelID = &other
			case "duplicate-model":
				ms = append(ms, model)
			case "missing-model":
				ms = nil
			case "foreign-binding":
				bs[0].ModelID = "mdl_other"
			case "duplicate-binding":
				bs = append(bs, binding)
			case "missing-pm":
				ps = nil
			case "alias-pm":
				ps[0].ID = "PMD_old"
			case "missing-connection":
				cs = nil
			case "alias-connection":
				cs[0].ID = "CON_old"
			case "over-bound":
				for i := 0; i < 201; i++ {
					copy := binding
					copy.ID = fmt.Sprintf("bnd_extra_%03d", i)
					bs = append(bs, copy)
				}
			}
			if _, err := projectModelCreationDefaults([]string{"same"}, ns, ms, bs, ps, cs, current); err == nil {
				t.Fatal("corrupt snapshot became a target")
			}
		})
	}
	for _, names := range [][]string{{"same", "same"}, {" name "}, {strings.Repeat("a", 129)}} {
		if _, err := projectModelCreationDefaults(names, nil, nil, nil, nil, nil, current); err == nil {
			t.Fatal("invalid names accepted")
		}
	}
	got, err := projectModelCreationDefaults([]string{"SAME"}, nil, nil, nil, nil, nil, current)
	if err != nil || !reflect.DeepEqual(got["SAME"], &ModelCreationInitialTarget{Target: "new", Name: "SAME"}) {
		t.Fatal("exact spelling normalized", got, err)
	}
	copy := slices.Clone([]string{"a", "b"})
	_, _ = projectModelCreationDefaults(copy, nil, nil, nil, nil, nil, current)
	if !reflect.DeepEqual(copy, []string{"a", "b"}) {
		t.Fatal("caller input mutated")
	}
}

type creationDefaultsSQL struct {
	queries     []string
	fail        string
	options     driver.TxOptions
	names       []entity.ModelName
	models      []entity.Model
	bindings    []entity.ModelProviderBinding
	pms         []entity.ProviderModel
	connections []entity.ProviderConnection
}
type creationDefaultsConnector struct{ f *creationDefaultsSQL }

func (c creationDefaultsConnector) Connect(context.Context) (driver.Conn, error) {
	return creationDefaultsConnection(c), nil
}
func (c creationDefaultsConnector) Driver() driver.Driver { return creationDefaultsDriver(c) }

type creationDefaultsDriver creationDefaultsConnector

func (d creationDefaultsDriver) Open(string) (driver.Conn, error) {
	return creationDefaultsConnection(d), nil
}

type creationDefaultsConnection creationDefaultsConnector

func (creationDefaultsConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (creationDefaultsConnection) Close() error { return nil }
func (c creationDefaultsConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c creationDefaultsConnection) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.f.options = opts
	return adminOverviewTransaction{}, nil
}
func (creationDefaultsConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	return nil, errors.New("assistance wrote state")
}
func (c creationDefaultsConnection) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	c.f.queries = append(c.f.queries, q)
	if c.f.fail != "" && strings.Contains(q, c.f.fail) {
		return nil, errors.New("controlled assistance batch failure")
	}
	switch {
	case strings.Contains(q, `FROM "model_names"`):
		return effectiveSQLRows(c.f.names)
	case strings.Contains(q, `FROM "models"`):
		return effectiveSQLRows(c.f.models)
	case strings.Contains(q, `FROM "model_provider_bindings"`):
		return effectiveSQLRows(c.f.bindings)
	case strings.Contains(q, `FROM "provider_models"`):
		return effectiveSQLRows(c.f.pms)
	case strings.Contains(q, `FROM "provider_connections"`):
		return effectiveSQLRows(c.f.connections)
	}
	return nil, errors.New("unexpected assistance query")
}
func TestModelCreationInitialTargetsUseFiveBoundedPageBatches(t *testing.T) {
	for _, fail := range []string{"", "model_names", "models", "model_provider_bindings", "provider_models", "provider_connections"} {
		t.Run("failure-"+fail, func(t *testing.T) {
			f := &creationDefaultsSQL{fail: fail}
			rows := []ModelCreationProviderModel{}
			for i := 0; i < 50; i++ {
				mid := fmt.Sprintf("mdl_%02d", i)
				native := fmt.Sprintf("native-%02d", i)
				f.names = append(f.names, entity.ModelName{Name: native, ModelID: mid, CurrentModelID: &mid})
				f.models = append(f.models, entity.Model{ID: mid, Status: entity.ResourceActive})
				f.bindings = append(f.bindings, entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_%02d", i), ModelID: mid, ProviderModelID: "pmd_original", Weight: 100})
				rows = append(rows, ModelCreationProviderModel{ID: fmt.Sprintf("pmd_%02d", i), UpstreamName: native, Selectable: true})
			}
			f.pms = []entity.ProviderModel{{ID: "pmd_original", ConnectionID: "con_original"}}
			f.connections = []entity.ProviderConnection{{ID: "con_original", ProviderID: "prv_original", Protocol: entity.ProtocolOpenAIChat}}
			conn := sql.OpenDB(creationDefaultsConnector{f})
			defer func() {
				if err := conn.Close(); err != nil {
					t.Error(err)
				}
			}()
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: conn}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			err = db.Transaction(func(tx *gorm.DB) error {
				return applyModelCreationDefaults(tx, entity.ProviderConnection{ProviderID: "prv_current", Protocol: entity.ProtocolOpenAIChat}, rows)
			}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
			if fail != "" {
				if err == nil {
					t.Fatal("failed read became known defaults")
				}
				for _, row := range rows {
					if row.InitialTarget != nil {
						t.Fatal("partial facts escaped")
					}
				}
				return
			}
			if err != nil || len(f.queries) != 5 || !f.options.ReadOnly || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal(len(f.queries), f.options, err)
			}
			for _, q := range f.queries {
				if !strings.Contains(q, "LIMIT ") {
					t.Fatal("unbounded query")
				}
			}
			for _, row := range rows {
				if row.InitialTarget == nil || row.InitialTarget.Target != "existing" || *row.InitialTarget.InitialWeight != 0 {
					t.Fatal("off-page default missing", row)
				}
			}
		})
	}
}
