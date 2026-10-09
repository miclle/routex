package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type routingSQLFixture struct {
	failStamp   bool
	roles       *rolesSQLFixture
	model       entity.Model
	provider    entity.Provider
	connections []entity.ProviderConnection
	models      []entity.ProviderModel
	credentials []entity.ProviderCredential
	access      []entity.CredentialModelAccess
	prices      []entity.ModelPrice
	rates       []entity.PriceRate
	queries     []string
	options     []driver.TxOptions
	bindings    []entity.ModelProviderBinding
}
type routingConnector struct{ f *routingSQLFixture }

func (c routingConnector) Connect(context.Context) (driver.Conn, error) {
	return &routingConnection{rolesSQLConnection: &rolesSQLConnection{f: c.f.roles}, f: c.f}, nil
}
func (c routingConnector) Driver() driver.Driver { return routingDriver(c) }

type routingDriver routingConnector

func (c routingDriver) Open(string) (driver.Conn, error) {
	return routingConnector(c).Connect(context.Background())
}

type routingConnection struct {
	pendingModel *entity.Model
	pending      []entity.ModelProviderBinding
	*rolesSQLConnection
	f *routingSQLFixture
}

func (c *routingConnection) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	c.f.options = append(c.f.options, o)
	tx, err := c.rolesSQLConnection.BeginTx(ctx, o)
	c.pending = slices.Clone(c.f.bindings)
	model := c.f.model
	c.pendingModel = &model
	if err != nil {
		return nil, err
	}
	return routingTransaction{Tx: tx, c: c}, nil
}

type routingTransaction struct {
	driver.Tx
	c *routingConnection
}

func (tx routingTransaction) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	tx.c.f.bindings = slices.Clone(tx.c.pending)
	tx.c.f.model = *tx.c.pendingModel
	return nil
}
func (c *routingConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `UPDATE "models" SET "config_updated_at"`) {
		if c.f.failStamp {
			return nil, errors.New("controlled configuration stamp failure")
		}
		if c.pendingModel == nil || len(args) != 2 || args[1].Value != c.pendingModel.ID {
			return nil, errors.New("unexpected configuration stamp target")
		}
		stamp, ok := args[0].Value.(time.Time)
		if !ok {
			return nil, errors.New("configuration stamp lost recorded date")
		}
		c.pendingModel.ConfigUpdatedAt = &stamp
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(q, `INSERT INTO "model_provider_bindings"`) {
		c.f.roles.writes = append(c.f.roles.writes, q)
		c.pending = append(c.pending, entity.ModelProviderBinding{ID: args[0].Value.(string), ModelID: args[1].Value.(string), ProviderModelID: args[2].Value.(string), Weight: int(args[3].Value.(int64)), CreatedAt: args[4].Value.(time.Time)})
		return driver.RowsAffected(1), nil
	}
	return c.rolesSQLConnection.ExecContext(ctx, q, args)
}
func (c *routingConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.f
	if strings.HasPrefix(q, "SELECT count(*)") {
		return effectiveSQLRows([]struct{ Count int64 }{{Count: 1}})
	}
	// These source fixtures exercise actual GORM projections and bounded batches; real collation
	// filtering/serialization is also asserted by the existing registered catalog lifecycle.
	if strings.Contains(q, "FROM provider_models AS pm") {
		f.queries = append(f.queries, q)
		if strings.Contains(q, "DISTINCT p.id") {
			return effectiveSQLRows([]ModelRoutingProvider{{ID: f.provider.ID, Name: f.provider.Name}})
		}
		if len(c.pending) > 0 {
			return effectiveSQLRows([]entity.ProviderModel{})
		}
		return effectiveSQLRows(f.models)
	}
	for _, table := range []string{"models", "model_names", "user_model_grants", "provider_models", "model_provider_bindings", "providers", "provider_connections", "provider_credentials", "credential_model_accesses", "model_prices", "price_rates"} {
		if strings.Contains(q, `FROM "`+table+`"`) {
			f.queries = append(f.queries, q)
			switch table {
			case "model_names":
				return effectiveSQLRows([]entity.ModelName{{Name: "Public", ModelID: f.model.ID, CurrentModelID: &f.model.ID}})
			case "user_model_grants":
				return effectiveSQLRows([]entity.UserModelGrant{})
			case "provider_models":
				return effectiveSQLRows(f.models)
			case "models":
				if c.pendingModel != nil {
					return effectiveSQLRows([]entity.Model{*c.pendingModel})
				}
				return effectiveSQLRows([]entity.Model{f.model})
			case "model_provider_bindings":
				return effectiveSQLRows(c.pending)
			case "providers":
				return effectiveSQLRows([]entity.Provider{f.provider})
			case "provider_connections":
				return effectiveSQLRows(f.connections)
			case "provider_credentials":
				return effectiveSQLRows(f.credentials)
			case "credential_model_accesses":
				return effectiveSQLRows(f.access)
			case "model_prices":
				return effectiveSQLRows(f.prices)
			case "price_rates":
				return effectiveSQLRows(f.rates)
			}
		}
	}
	if strings.Contains(q, "role_permissions AS permission") {
		actor := c.current().users["usr_admin"]
		if actor.Role == entity.RoleMember {
			permission := ""
			for _, v := range rolesSQLStrings(args) {
				if slices.Contains(AvailablePermissions, v) {
					permission = v
				}
			}
			if f.roles.deny[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{})
			}
			return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_custom", PermissionRoleID: "rol_custom", Permission: permission, AssignmentRoleID: "rol_custom", AssignmentUserID: actor.ID}})
		}
	}
	return c.rolesSQLConnection.QueryContext(ctx, q, args)
}
func routingSQLService(t *testing.T, n int) (*Service, *routingSQLFixture) {
	t.Helper()
	actor, _, _ := connectionMetadataTestRows()
	f := &routingSQLFixture{roles: &rolesSQLFixture{data: rolesSQLData{users: map[string]entity.User{actor.ID: actor}}, deny: map[string]bool{}}, model: entity.Model{ID: "mdl_target", Status: entity.ResourceActive}, provider: entity.Provider{ID: "prv_one", Name: "Literal % supplier", Enabled: true, ETag: "0", CreatedAt: time.Now().UTC()}, connections: []entity.ProviderConnection{{ID: "con_one", ProviderID: "prv_one", Name: "Recorded connection", Protocol: "openai_chat", Enabled: true}}, credentials: []entity.ProviderCredential{{ID: "crd_one", ConnectionID: "con_one", Enabled: true, VerificationStatus: "verified", Ciphertext: "retained-ciphertext", CreatedAt: time.Now().UTC()}}}
	for i := 0; i < n; i++ {
		id := "pmd_" + strings.Repeat("a", i+1)
		f.models = append(f.models, entity.ProviderModel{ID: id, ConnectionID: "con_one", UpstreamName: "Literal%_model"})
		f.access = append(f.access, entity.CredentialModelAccess{CredentialID: "crd_one", ProviderModelID: id})
	}
	pool := sql.OpenDB(routingConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func TestModelRoutingCandidateBoundedBatchFactsAndOptionalPrices(t *testing.T) {
	for _, n := range []int{1, 20} {
		t.Run(strings.Repeat("a", n), func(t *testing.T) {
			s, f := routingSQLService(t, n)
			f.prices = []entity.ModelPrice{{ID: "prc_one", ProviderModelID: f.models[0].ID}}
			f.rates = []entity.PriceRate{{ID: "rate_one", ModelPriceID: "prc_one", Tier: "base", Metric: "INPUT_TOKEN", Unit: "1M_TOKEN", Currency: "USD", Amount: "0.123456789123456789", Enabled: true}}
			page, err := s.ListModelRoutingCandidates(context.Background(), "usr_admin", "mdl_target", "openai_chat", "prv_one", ModelCreationFilter{Query: "%_", Limit: 20})
			if err != nil || len(page.Items) != n {
				t.Fatal("bounded actual read failed", err)
			}
			row := page.Items[0]
			if !row.Selectable || !row.VerificationCovered || !row.ConfiguredAvailable || row.ConnectionName != "Recorded connection" || row.Prices == nil || row.Prices.Input[0].Amount != f.rates[0].Amount {
				t.Fatal("recorded facts/decimal strings lost")
			}
			if len(f.queries) != 9 || len(f.options) != 1 || !f.options[0].ReadOnly || f.options[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || len(f.roles.writes) != 0 {
				t.Fatal("N+1, write or snapshot contract changed", len(f.queries), f.options)
			}
			joined := strings.Join(f.queries, " ")
			if !strings.Contains(joined, "NOT EXISTS") || !strings.Contains(joined, "LIMIT") || !strings.Contains(joined, "ESCAPE '!'") {
				t.Fatal("duplicate/literal/page predicate missing")
			}
		})
	}
}
func TestModelRoutingIndependentAuthorityAndUnknownPrices(t *testing.T) {
	for _, permission := range []string{"models.read_all", "providers.read", "prices.read"} {
		t.Run(permission, func(t *testing.T) {
			s, f := routingSQLService(t, 1)
			f.roles.deny[permission] = true
			got, err := s.ListModelRoutingCandidates(context.Background(), "usr_admin", "mdl_target", "openai_chat", "prv_one", ModelCreationFilter{})
			if permission != "prices.read" {
				if !errors.Is(err, apperrors.ErrForbidden) || got != nil || len(f.queries) != 0 {
					t.Fatal("denied read exposed private supply", err, len(f.queries))
				}
				return
			}
			if err != nil || got.Items[0].Prices != nil {
				t.Fatal("denied prices fabricated facts", err)
			}
			for _, q := range f.queries {
				if strings.Contains(q, "model_prices") || strings.Contains(q, "price_rates") {
					t.Fatal("denied price read queried catalogue")
				}
			}
		})
	}
}
func TestModelRoutingConfiguredAvailabilityIsNotCoverage(t *testing.T) {
	for _, kind := range []string{"unverified", "disabled_credential", "disabled_connection", "disabled_provider", "disabled_model", "missing_material"} {
		t.Run(kind, func(t *testing.T) {
			s, f := routingSQLService(t, 1)
			switch kind {
			case "unverified":
				f.credentials[0].VerificationStatus = "pending"
			case "disabled_credential":
				f.credentials[0].Enabled = false
			case "disabled_provider":
				f.provider.Enabled = false
			case "disabled_connection":
				f.connections[0].Enabled = false
			case "disabled_model":
				f.models[0].Disabled = true
			case "missing_material":
				f.credentials[0].Ciphertext = ""
			}
			got, err := s.ListModelRoutingCandidates(context.Background(), "usr_admin", "mdl_target", "openai_chat", "prv_one", ModelCreationFilter{})
			if err != nil || got.Items[0].Selectable {
				t.Fatal("ineligible recorded candidate selectable", err)
			}
			if kind == "unverified" && (!got.Items[0].ConfiguredAvailable || got.Items[0].VerificationCovered) {
				t.Fatal("configuration inferred verification")
			}
			if kind == "missing_material" && (!got.Items[0].ConfiguredAvailable || !got.Items[0].VerificationCovered) {
				t.Fatal("source invalidity overwrote separate recorded facts")
			}
		})
	}
}
func TestModelRoutingOverflowAliasesAndMalformedBeforeLookup(t *testing.T) {
	for _, kind := range []string{"actor_alias", "provider_alias", "connection_alias", "model_alias", "credential_overflow", "price_overflow"} {
		t.Run(kind, func(t *testing.T) {
			s, f := routingSQLService(t, 1)
			switch kind {
			case "actor_alias":
				f.roles.actorAlias = true
			case "provider_alias":
				f.provider.ID = "PRV_one"
			case "connection_alias":
				f.models[0].ConnectionID = "CON_one"
			case "model_alias":
				f.model.ID = "MDL_target"
			case "credential_overflow":
				for len(f.credentials) < 1001 {
					f.credentials = append(f.credentials, f.credentials[0])
				}
			case "price_overflow":
				for len(f.prices) < 52 {
					f.prices = append(f.prices, entity.ModelPrice{ID: "prc_one", ProviderModelID: f.models[0].ID})
				}
			}
			got, err := s.ListModelRoutingCandidates(context.Background(), "usr_admin", "mdl_target", "openai_chat", "prv_one", ModelCreationFilter{})
			if err == nil || got != nil {
				t.Fatal("alias/overflow exposed candidate")
			}
		})
	}
	for _, protocol := range []string{"OPENAI_CHAT", "openai_chat\n", "unknown"} {
		if _, err := (&Service{}).ListModelRoutingProviders(context.Background(), "usr_admin", "mdl_target", protocol, ModelCreationFilter{}); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("invalid protocol reached DB")
		}
	}
}
func TestModelRoutingReviewFencesRecordedMaterialAndTopology(t *testing.T) {
	s, f := routingSQLService(t, 1)
	_ = s
	base := routingSupplyState{Enabled: true, Covered: true, SourceAvailable: true, Credentials: []modelCreationCredentialProof{{ID: "crd_one", CipherHash: "source-one"}}}
	first := routingCandidate(f.model, nil, f.models[0], f.connections[0], f.provider, base)
	for _, kind := range []string{"target", "birth", "protocol", "model", "source", "binding"} {
		t.Run(kind, func(t *testing.T) {
			m, c, p, state := f.model, f.connections[0], f.models[0], base
			state.Credentials = slices.Clone(base.Credentials)
			bindings := []entity.ModelProviderBinding{}
			switch kind {
			case "target":
				m.Status = entity.ResourceDisabled
			case "birth":
				c.CreatedAt = time.Now()
			case "protocol":
				c.Protocol = "openai_responses"
			case "model":
				p.ETag = "changed"
			case "source":
				state.Credentials[0].CipherHash = "changed"
			case "binding":
				bindings = append(bindings, entity.ModelProviderBinding{ID: "bnd_one", ModelID: m.ID, ProviderModelID: p.ID, Weight: 0})
			}
			if routingCandidate(m, bindings, p, c, f.provider, state).ReviewETag == first.ReviewETag {
				t.Fatal("review did not fence current source/topology")
			}
		})
	}
	for _, dialect := range []string{"pg", "my"} {
		t.Run(dialect, func(t *testing.T) {
			d := postgres.New(postgres.Config{DSN: "host=localhost port=1 user=test dbname=test sslmode=disable"})
			if dialect == "my" {
				d = mysql.New(mysql.Config{DSN: "unused:unused@tcp(localhost:1)/unused", SkipInitializeWithVersion: true})
			}
			db, err := gorm.Open(d, &gorm.Config{DisableAutomaticPing: true, DryRun: true})
			if err != nil {
				t.Fatal(err)
			}
			q := routingCandidateQuery(db, "mdl_Exact", "openai_chat").Select("pm.*").Limit(51).Find(&[]entity.ProviderModel{}).Statement
			if !strings.Contains(q.SQL.String(), "NOT EXISTS") || !strings.Contains(q.SQL.String(), "bound_connection") || !strings.Contains(q.SQL.String(), "LIMIT") {
				t.Fatal("exact duplicate provider predicate lost")
			}
		})
	}
}

func TestModelRoutingReviewedInsertCurrentAuthorityAndAtomicAudit(t *testing.T) {
	for _, kind := range []string{"ok", "audit_failure", "stamp_failure", "write_denied", "read_denied", "provider_denied", "stale_review", "wrong_protocol", "invalid_source", "disabled_target"} {
		t.Run(kind, func(t *testing.T) {
			s, f := routingSQLService(t, 1)
			page, err := s.ListModelRoutingCandidates(context.Background(), "usr_admin", "mdl_target", "openai_chat", "prv_one", ModelCreationFilter{})
			if err != nil {
				t.Fatal(err)
			}
			review, protocol := page.Items[0].ReviewETag, "openai_chat"
			switch kind {
			case "audit_failure":
				f.roles.failAudit = true
			case "stamp_failure":
				f.failStamp = true
			case "write_denied":
				f.roles.deny["models.write"] = true
			case "read_denied":
				f.roles.deny["models.read_all"] = true
			case "provider_denied":
				f.roles.deny["providers.read"] = true
			case "stale_review":
				f.connections[0].Name = "Changed"
			case "wrong_protocol":
				protocol = "openai_responses"
			case "invalid_source":
				f.credentials[0].Ciphertext = ""
			case "disabled_target":
				f.model.Status = entity.ResourceDisabled
			}
			beforeModel := f.model
			f.options = nil
			got, err := s.AddReviewedModelBinding(context.Background(), "usr_admin", "mdl_target", f.models[0].ID, protocol, review)
			if len(f.options) == 0 || f.options[0].Isolation != driver.IsolationLevel(sql.LevelReadCommitted) || f.options[0].ReadOnly {
				t.Fatal("insertion did not serialize fresh current proof")
			}
			if kind != "ok" {
				if err == nil || got != nil || len(f.bindings) != 0 || len(f.roles.data.audits) != 0 || !reflect.DeepEqual(f.model, beforeModel) {
					t.Fatal("rejected/audit-failed insertion committed", kind, err)
				}
				return
			}
			if err != nil || got == nil || len(f.bindings) != 1 || f.bindings[0].Weight != 0 || len(f.roles.data.audits) != 1 || f.roles.data.audits[0].ResourceID != "mdl_target" {
				t.Fatal("exact zero insertion/audit not committed", err)
			}
			if f.model.ConfigUpdatedAt == nil || f.model.ConfigUpdatedAt.IsZero() || f.model.ConfigUpdatedAt.Location() != time.UTC || f.model.ConfigUpdatedAt.Nanosecond()%1000 != 0 {
				t.Fatal("successful insertion did not record portable configuration time")
			}
			expected := beforeModel
			expected.ConfigUpdatedAt = f.model.ConfigUpdatedAt
			if !reflect.DeepEqual(f.model, expected) {
				t.Fatal("configuration stamp changed Model identity/lifecycle/birth")
			}
			committedModel := f.model
			if _, err = s.AddReviewedModelBinding(context.Background(), "usr_admin", "mdl_target", f.models[0].ID, protocol, review); !errors.Is(err, catalogConflict) || len(f.bindings) != 1 || len(f.roles.data.audits) != 1 || !reflect.DeepEqual(f.model, committedModel) {
				t.Fatal("duplicate Provider/retry fabricated success", err)
			}
		})
	}
}
