package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func configuredReadinessTestGraph() ([]ModelCatalog, []entity.ProviderModel, []entity.ProviderConnection, []entity.Provider, map[string]routingSupplyState) {
	birth := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	provider := entity.Provider{ID: "prv_summary", Enabled: true, CreatedAt: birth}
	connection := entity.ProviderConnection{ID: "con_summary", ProviderID: provider.ID, Enabled: true, Protocol: entity.ProtocolOpenAIChat, CreatedAt: birth, TransportGeneration: "0"}
	pm := entity.ProviderModel{ID: "pmd_summary", ConnectionID: connection.ID, UpstreamName: "literal-UPSTREAM", CreatedAt: birth, CapabilityTransportGeneration: "0"}
	binding := entity.ModelProviderBinding{ID: "bnd_summary", ModelID: "mdl_summary", ProviderModelID: pm.ID, Weight: 100}
	models := []ModelCatalog{{Model: entity.Model{ID: binding.ModelID, Status: entity.ResourceActive}, Bindings: []BindingCatalog{modelBindingCatalog(binding, pm, connection, true)}, GrantedUserIDs: []string{"usr_retained"}}}
	states := map[string]routingSupplyState{pm.ID: {Enabled: true, Covered: true, SourceAvailable: true}}
	return models, []entity.ProviderModel{pm}, []entity.ProviderConnection{connection}, []entity.Provider{provider}, states
}

func TestAdminModelConfiguredReadinessUsesCompleteStoredSupply(t *testing.T) {
	for _, scenario := range []string{"ready", "provider_disabled", "connection_disabled", "provider_model_disabled", "zero_weight", "model_disabled", "model_archived", "no_enabled_credential", "verification_uncovered", "source_unavailable", "stale_transport"} {
		t.Run(scenario, func(t *testing.T) {
			models, pms, connections, providers, states := configuredReadinessTestGraph()
			state := states[pms[0].ID]
			switch scenario {
			case "provider_disabled":
				providers[0].Enabled = false
			case "connection_disabled":
				connections[0].Enabled = false
			case "provider_model_disabled":
				pms[0].Disabled = true
			case "zero_weight":
				models[0].Bindings[0].Binding.Weight = 0
			case "model_disabled":
				models[0].Model.Status = entity.ResourceDisabled
			case "model_archived":
				models[0].Model.Status = entity.ResourceArchived
			case "no_enabled_credential":
				state.Enabled = false
			case "verification_uncovered":
				state.Covered = false
			case "source_unavailable":
				state.SourceAvailable = false
			case "stale_transport":
				connections[0].TransportGeneration = "rev_01j00000000000000000000000"
			}
			states[pms[0].ID] = state
			before := models[0]
			projectModelConfiguredReadiness(models, pms, connections, providers, states)
			if models[0].ConfiguredReady == nil || *models[0].ConfiguredReady != (scenario == "ready") {
				t.Fatal("stored configuration summary ignored a current supply gate", scenario, models[0].ConfiguredReady)
			}
			models[0].ConfiguredReady = nil
			if !reflect.DeepEqual(models[0], before) || !models[0].Bindings[0].Ready || !models[0].Bindings[0].credentialReady {
				t.Fatal("read summary changed legacy binding/write eligibility or grants")
			}
		})
	}
}

func TestAdminModelConfiguredReadinessKeepsIncompleteProjectionUnknown(t *testing.T) {
	for _, scenario := range []string{"missing_pm", "pm_alias", "connection_alias", "provider_alias", "binding_owner_alias", "projected_parent_alias", "upstream_alias", "protocol_alias", "negative_weight", "excess_weight", "missing_state", "duplicate_pm", "duplicate_connection", "duplicate_provider", "duplicate_model", "duplicate_binding", "foreign_state", "foreign_provider", "unknown_model_status", "legacy_birth_unknown", "legacy_generation_unknown", "model_overflow", "binding_overflow"} {
		t.Run(scenario, func(t *testing.T) {
			models, pms, connections, providers, states := configuredReadinessTestGraph()
			switch scenario {
			case "missing_pm":
				pms = nil
			case "pm_alias":
				pms[0].ID = "pmd_SUMMARY"
			case "connection_alias":
				connections[0].ID = "con_SUMMARY"
			case "provider_alias":
				providers[0].ID = "prv_SUMMARY"
			case "binding_owner_alias":
				models[0].Bindings[0].Binding.ModelID = "mdl_SUMMARY"
			case "projected_parent_alias":
				models[0].Bindings[0].ProviderID = "prv_SUMMARY"
			case "upstream_alias":
				models[0].Bindings[0].UpstreamName = "literal-upstream"
			case "protocol_alias":
				models[0].Bindings[0].Protocol = "OPENAI_CHAT"
			case "negative_weight":
				models[0].Bindings[0].Binding.Weight = -1
			case "excess_weight":
				models[0].Bindings[0].Binding.Weight = 101
			case "missing_state":
				states = nil
			case "duplicate_pm":
				pms = append(pms, pms[0])
			case "duplicate_connection":
				connections = append(connections, connections[0])
			case "duplicate_provider":
				providers = append(providers, providers[0])
			case "duplicate_model":
				models = append(models, models[0])
			case "duplicate_binding":
				models[0].Bindings = append(models[0].Bindings, models[0].Bindings[0])
			case "foreign_state":
				states["pmd_unrelated"] = routingSupplyState{}
			case "foreign_provider":
				providers = append(providers, entity.Provider{ID: "prv_unrelated"})
			case "unknown_model_status":
				models[0].Model.Status = "unknown"
			case "legacy_birth_unknown":
				connections[0].CreatedAt = time.Time{}
			case "legacy_generation_unknown":
				pms[0].CapabilityTransportGeneration = ""
			case "model_overflow":
				models = make([]ModelCatalog, modelConfiguredReadinessLimit+1)
			case "binding_overflow":
				models[0].Bindings = make([]BindingCatalog, modelConfiguredReadinessLimit+1)
			}
			stale := true
			for i := range models {
				models[i].ConfiguredReady = &stale
			}
			projectModelConfiguredReadiness(models, pms, connections, providers, states)
			for _, model := range models {
				if model.ConfiguredReady != nil {
					t.Fatal("incomplete, aliased or overflowing data became a known readiness fact", scenario)
				}
			}
		})
	}
}

func TestAdminModelConfiguredReadinessPositiveAlternativeAndEmpty(t *testing.T) {
	models, pms, connections, providers, states := configuredReadinessTestGraph()
	other := pms[0]
	other.ID = "pmd_alternative"
	pms[0].Disabled = true
	pms = append(pms, other)
	binding := models[0].Bindings[0].Binding
	binding.ID, binding.ProviderModelID, binding.Weight = "bnd_alternative", other.ID, 1
	models[0].Bindings = append(models[0].Bindings, modelBindingCatalog(binding, other, connections[0], true))
	states[other.ID] = states[pms[0].ID]
	projectModelConfiguredReadiness(models, pms, connections, providers, states)
	if models[0].ConfiguredReady == nil || !*models[0].ConfiguredReady {
		t.Fatal("one disabled route hid a separate configured eligible positive route")
	}
	models[0].Bindings = nil
	projectModelConfiguredReadiness(models, nil, nil, nil, nil)
	if models[0].ConfiguredReady == nil || *models[0].ConfiguredReady {
		t.Fatal("authorized complete empty binding set lost known false")
	}
}

type configuredReadinessSQLFixture struct {
	roles               *rolesSQLFixture
	state               *providerBindingsSQLState
	credentialReads     int
	failCredentials     bool
	overflowCredentials bool
}

type configuredReadinessConnector struct {
	f *configuredReadinessSQLFixture
}

func (c configuredReadinessConnector) Connect(context.Context) (driver.Conn, error) {
	return &configuredReadinessSQLConnection{providerBindingsConnection: &providerBindingsConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.roles}, state: c.f.state}, f: c.f}, nil
}
func (c configuredReadinessConnector) Driver() driver.Driver { return configuredReadinessDriver(c) }

type configuredReadinessDriver configuredReadinessConnector

func (c configuredReadinessDriver) Open(string) (driver.Conn, error) {
	return configuredReadinessConnector(c).Connect(context.Background())
}

type configuredReadinessSQLConnection struct {
	*providerBindingsConnection
	f *configuredReadinessSQLFixture
}

func (c *configuredReadinessSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, `FROM "provider_credentials"`) {
		c.f.credentialReads++
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.f.failCredentials {
			return nil, errors.New("controlled configured-summary outage")
		}
		if !strings.Contains(query, "LIMIT") || !strings.Contains(query, "connection_id") {
			return nil, errors.New("unbounded configured-summary credential read")
		}
		rows := []entity.ProviderCredential{}
		if c.f.overflowCredentials {
			rows = make([]entity.ProviderCredential, 1001)
		}
		return effectiveSQLRows(rows)
	}
	return c.providerBindingsConnection.QueryContext(ctx, query, args)
}

func TestAdminModelConfiguredReadinessSQLIndependentAuthorityAndBatch(t *testing.T) {
	for _, scenario := range []string{"one", "twenty", "restricted", "models_denied", "actor_alias", "missing_parent", "parent_alias", "outage", "credential_overflow", "projection_overflow", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			models, pms, connections, providers, _ := configuredReadinessTestGraph()
			if scenario == "restricted" {
				stale := true
				models[0].ConfiguredReady = &stale
			}
			actor, _, _ := connectionMetadataTestRows()
			actor.Role = entity.RoleMember
			f := &configuredReadinessSQLFixture{roles: &rolesSQLFixture{data: rolesSQLData{users: map[string]entity.User{actor.ID: actor}}, deny: map[string]bool{}}, state: &providerBindingsSQLState{provider: providers[0], connections: connections, supply: pms}}
			ctx := context.Background()
			switch scenario {
			case "twenty":
				for i := 1; i < 20; i++ {
					item := models[0]
					item.Model.ID = fmt.Sprintf("mdl_%03d", i)
					item.Bindings = append([]BindingCatalog{}, item.Bindings...)
					item.Bindings[0].Binding.ModelID = item.Model.ID
					item.Bindings[0].Binding.ID = fmt.Sprintf("bnd_%03d", i)
					models = append(models, item)
				}
			case "restricted":
				f.roles.deny["providers.read"] = true
			case "models_denied":
				f.roles.deny["models.read_all"] = true
			case "actor_alias":
				f.roles.actorAlias = true
			case "missing_parent":
				f.state.provider.ID = ""
			case "parent_alias":
				f.state.provider.ID = "prv_SUMMARY"
			case "outage":
				f.failCredentials = true
			case "credential_overflow":
				f.overflowCredentials = true
			case "projection_overflow":
				models = make([]ModelCatalog, modelConfiguredReadinessLimit+1)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			pool := sql.OpenDB(configuredReadinessConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := exactCatalogPermission(tx, actor.ID, "models.read_all"); err != nil {
					return err
				}
				return enrichModelConfiguredReadiness(tx, actor.ID, models)
			}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
			wantError := scenario == "models_denied" || scenario == "actor_alias" || scenario == "outage" || scenario == "cancelled"
			if (err != nil) != wantError || len(f.roles.writes) != 0 {
				t.Fatal("independent authority/read failure contract changed", scenario, err)
			}
			wantUnknown := wantError || scenario == "restricted" || scenario == "missing_parent" || scenario == "parent_alias" || scenario == "credential_overflow" || scenario == "projection_overflow"
			for _, model := range models {
				if wantUnknown && model.ConfiguredReady != nil || !wantUnknown && (model.ConfiguredReady == nil || *model.ConfiguredReady) {
					t.Fatal("restricted/incomplete data manufactured readiness or empty credentials lost known false", scenario)
				}
			}
			if scenario == "restricted" || scenario == "models_denied" || scenario == "actor_alias" || scenario == "projection_overflow" || scenario == "cancelled" {
				if len(f.state.queries) != 0 || f.credentialReads != 0 {
					t.Fatal("dependent catalogue read before fresh authority/bounds", scenario)
				}
			} else if len(f.state.queries) != 3 || f.credentialReads != 1 {
				t.Fatal("configured summary added per-Model dependency queries", scenario, len(f.state.queries), f.credentialReads)
			}
			if scenario != "cancelled" && (len(f.roles.transactions) != 1 || !f.roles.transactions[0].ReadOnly || f.roles.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !f.state.deadline) {
				t.Fatal("configured summary lost bounded coherent read-only transaction")
			}
		})
	}
}
