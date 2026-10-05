package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type effectiveSQLFixture struct {
	data        *memberEffectiveModelsData
	queries     []string
	options     driver.TxOptions
	permissions map[string]bool
	failTable   string
}
type effectiveSQLConnector struct{ fixture *effectiveSQLFixture }

func (c effectiveSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return effectiveSQLConnection(c), nil
}
func (c effectiveSQLConnector) Driver() driver.Driver { return effectiveSQLDriver(c) }

type effectiveSQLDriver effectiveSQLConnector

func (d effectiveSQLDriver) Open(string) (driver.Conn, error) { return effectiveSQLConnection(d), nil }

type effectiveSQLConnection effectiveSQLConnector

func (effectiveSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (effectiveSQLConnection) Close() error { return nil }
func (c effectiveSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c effectiveSQLConnection) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = opts
	return adminOverviewTransaction{}, nil
}
func effectiveSQLRows[T any](values []T) (driver.Rows, error) {
	var zero T
	parsed, err := schema.Parse(&zero, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		return nil, err
	}
	rows := &adminOverviewRows{columns: parsed.DBNames}
	for _, value := range values {
		row := []driver.Value{}
		for _, column := range parsed.DBNames {
			raw, _ := parsed.FieldsByDBName[column].ValueOf(context.Background(), reflect.ValueOf(value))
			converted, err := driver.DefaultParameterConverter.ConvertValue(raw)
			if err != nil {
				return nil, err
			}
			row = append(row, converted)
		}
		rows.values = append(rows.values, row)
	}
	return rows, nil
}
func (c effectiveSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, q)
	d := f.data.Metadata
	if f.failTable != "" && strings.Contains(q, f.failTable) {
		return nil, errors.New("controlled selected metadata failure")
	}
	switch {
	case strings.Contains(q, `FROM "users"`):
		if args[0].Value == "usr_reader" {
			return effectiveSQLRows([]entity.User{{ID: "usr_reader", Role: entity.RoleAdmin, CreatedAt: d.Subject.CreatedAt}})
		}
		return effectiveSQLRows([]entity.User{d.Subject})
	case strings.Contains(q, "role_permissions AS permission"):
		for _, a := range args {
			if permission, ok := a.Value.(string); ok && f.permissions[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_admin", PermissionRoleID: "rol_admin", Permission: permission}})
			}
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(q, "team_memberships AS member"):
		return effectiveSQLRows(f.data.Teams)
	case strings.Contains(q, `FROM "team_model_grants"`):
		return effectiveSQLRows(f.data.TeamGrants)
	case strings.Contains(q, `FROM "user_model_grants"`):
		return effectiveSQLRows(d.Grants)
	case strings.Contains(q, `FROM "models"`):
		return effectiveSQLRows(d.Models)
	case strings.Contains(q, `FROM "model_names"`):
		return effectiveSQLRows(d.Names)
	case strings.Contains(q, `FROM "model_provider_bindings"`):
		return effectiveSQLRows(d.Bindings)
	case strings.Contains(q, `FROM "provider_models"`):
		return effectiveSQLRows(d.ProviderModels)
	case strings.Contains(q, `FROM "provider_connections"`):
		return effectiveSQLRows(d.Connections)
	case strings.Contains(q, `FROM "egress_settings"`):
		return effectiveSQLRows([]entity.EgressSetting{d.EgressSetting})
	case strings.Contains(q, `FROM "egresses"`):
		return effectiveSQLRows(d.Egresses)
	case strings.Contains(q, `FROM "provider_credentials"`):
		return effectiveSQLRows(d.Credentials)
	case strings.Contains(q, `FROM "credential_model_accesses"`):
		return effectiveSQLRows(d.Access)
	case strings.Contains(q, `FROM "providers"`):
		return effectiveSQLRows(d.Providers)
	case strings.Contains(q, `FROM "model_prices"`):
		return effectiveSQLRows(d.Prices)
	case strings.Contains(q, `FROM "price_rates"`):
		return effectiveSQLRows(d.Rates)
	}
	return nil, fmt.Errorf("unexpected selected query %s", q)
}
func TestMemberEffectiveModelsMeasuredReadBudgetAndUnauthorizedTeamOmission(t *testing.T) {
	for _, name := range []string{"personal", "full_one", "full_twenty", "mixed_egress", "write_only", "team_only", "metadata_error", "overflow_models", "overflow_teams", "overflow_grants"} {
		t.Run(name, func(t *testing.T) {
			_, data := effectiveModelsFixture(t)
			if name == "mixed_egress" {
				id := "egr_one"
				data.Metadata.Connections[0].EgressMode = "proxy"
				data.Metadata.Connections[0].EgressID = &id
				data.Metadata.Egresses = []entity.Egress{{ID: id}}
			}
			// One authorized current Model with four protocols. Scaling rows never scales queries.
			if name == "full_twenty" || name == "overflow_models" {
				count := 20
				if name == "overflow_models" {
					count = 1001
				}
				m, n, g := data.Metadata.Models[0], data.Metadata.Names[0], data.Metadata.Grants[0]
				data.Metadata.Models = nil
				data.Metadata.Names = nil
				data.Metadata.Grants = nil
				data.TeamGrants = nil
				for i := range count {
					id := fmt.Sprintf("mdl_%04d", i)
					m.ID = id
					n.ModelID = id
					n.CurrentModelID = &id
					n.Name = fmt.Sprintf("controlled-%d", i)
					g.ModelID = id
					data.Metadata.Models = append(data.Metadata.Models, m)
					data.Metadata.Names = append(data.Metadata.Names, n)
					data.Metadata.Grants = append(data.Metadata.Grants, g)
					data.TeamGrants = append(data.TeamGrants, entity.TeamModelGrant{TeamID: "tea_one", ModelID: id})
				}
				// No ready claim without matching runtime; only batch shape is under test here.
				for i := range data.Metadata.Bindings {
					data.Metadata.Bindings[i].ModelID = data.Metadata.Models[0].ID
				}
			}
			if name == "overflow_teams" {
				data.Teams = make([]memberEffectiveTeam, 101)
			}
			if name == "overflow_grants" {
				data.TeamGrants = make([]entity.TeamModelGrant, 5001)
			}
			f := &effectiveSQLFixture{data: data, permissions: map[string]bool{"members.read": true, "teams.read_all": true, "providers.read": true, "prices.read": true}}
			if name == "personal" {
				f.permissions["teams.read_all"] = false
			}
			if name == "write_only" {
				f.permissions = map[string]bool{"members.models.write": true}
			}
			if name == "team_only" {
				f.permissions = map[string]bool{"teams.read_all": true}
			}
			if name == "metadata_error" {
				f.failTable = `FROM "provider_models"`
			}
			pool := sql.OpenDB(effectiveSQLConnector{f})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			s := &Service{db: db}
			result, err := s.MemberEffectiveModels(context.Background(), "usr_reader", data.Metadata.Subject.ID)
			if f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !f.options.ReadOnly {
				t.Fatal(f.options)
			}
			switch name {
			case "write_only", "team_only":
				if err != apperrors.ErrForbidden || result != nil || len(f.queries) != 2 {
					t.Fatal(err, len(f.queries))
				}
				return
			case "metadata_error":
				if err != apperrors.ErrInternal || result != nil {
					t.Fatal(err, result)
				}
				return
			case "overflow_models", "overflow_teams", "overflow_grants":
				if !errors.Is(err, ErrModelCatalogOverflow) || result != nil {
					t.Fatal(err, result)
				}
				return
			}
			if err != nil {
				t.Fatal(err, f.queries)
			}
			want := 21
			if name == "mixed_egress" {
				want = 22
			}
			if name == "personal" {
				want = 19
			}
			if len(f.queries) != want {
				t.Fatalf("fixed query budget got%d want%d: %v", len(f.queries), want, f.queries)
			}
			if name == "personal" {
				if result.TeamEnrichment != "not_authorized" || result.UnionCompleteness != "unknown" || len(result.Items[0].Sources) != 1 {
					t.Fatal(result)
				}
				for _, q := range f.queries {
					if strings.Contains(q, "team_") || strings.Contains(q, `FROM "teams"`) {
						t.Fatal("Team SQL without independent authority", q)
					}
				}
			}
		})
	}
}
func TestMemberEffectiveModelsExactJoinDoesNotMutateInitializedQuery(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			origin := db.Model(&entity.User{}).Where("id = ?", "usr_unrelated").Clauses(clause.Locking{Strength: "SHARE"})
			q := memberEffectiveTeamsQuery(origin, "usr_target")
			q.Statement.BuildClauses = []string{"SELECT", "FROM", "JOINS", "WHERE", "ORDER BY", "LIMIT"}
			callbacks.BuildQuerySQL(q)
			sql := q.Statement.SQL.String()
			if strings.Contains(sql, "usr_unrelated") || strings.Contains(sql, "users") || strings.Contains(sql, "SHARE") || !strings.Contains(sql, "JOIN teams AS team") || !reflect.DeepEqual(q.Statement.Vars[:3], []any{"usr_target", "active", "active"}) {
				t.Fatal(sql, q.Statement.Vars)
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") < 8 {
				t.Fatal("join or identities lost exactness", sql)
			}
		})
	}
}

func TestExistingMemberModelsSelectedHydrationRetainsReadOnlyContract(t *testing.T) {
	_, data := effectiveModelsFixture(t)
	f := &effectiveSQLFixture{data: data, permissions: map[string]bool{"members.read": true, "teams.read_all": true, "providers.read": true, "prices.read": true}}
	pool := sql.OpenDB(effectiveSQLConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{db: db}
	result, err := s.MemberModelsWorkspace(context.Background(), "usr_reader", data.Metadata.Subject.ID)
	if err != nil || result.CanEdit || len(result.PersonalModels) != 1 || len(result.AvailableModels) != 0 || result.PersonalModels[0].ID != "mdl_one" || len(f.queries) != 18 {
		t.Fatal(err, result, len(f.queries))
	}
	for _, q := range f.queries {
		if strings.Contains(q, "team_") || strings.Contains(q, "ciphertext") {
			t.Fatal("extraction enlarged existing Models reads", q)
		}
	}
	if !f.options.ReadOnly || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
		t.Fatal(f.options)
	}
}
