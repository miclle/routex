package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
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

type providerBindingsSQLState struct {
	applications  []entity.RegistrationApprovalApplication
	provider      entity.Provider
	connections   []entity.ProviderConnection
	supply        []entity.ProviderModel
	bindings      []entity.ModelProviderBinding
	models        []entity.Model
	names         []entity.ModelName
	queries       []string
	deadline      bool
	fail          string
	afterBindings func()
}

func (s providerBindingsSQLState) clone() providerBindingsSQLState {
	s.applications = slices.Clone(s.applications)
	s.connections = slices.Clone(s.connections)
	s.supply = slices.Clone(s.supply)
	s.bindings = slices.Clone(s.bindings)
	s.models = slices.Clone(s.models)
	s.names = slices.Clone(s.names)
	return s
}

type providerBindingsConnector struct {
	roles *rolesSQLFixture
	state *providerBindingsSQLState
}

func (c providerBindingsConnector) Connect(context.Context) (driver.Conn, error) {
	return &providerBindingsConnection{rolesSQLConnection: &rolesSQLConnection{f: c.roles}, state: c.state}, nil
}
func (c providerBindingsConnector) Driver() driver.Driver { return providerBindingsDriver(c) }

type providerBindingsDriver providerBindingsConnector

func (d providerBindingsDriver) Open(string) (driver.Conn, error) {
	return providerBindingsConnector(d).Connect(context.Background())
}

type providerBindingsConnection struct {
	*rolesSQLConnection
	state    *providerBindingsSQLState
	snapshot providerBindingsSQLState
}

func (c *providerBindingsConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	deadline, ok := ctx.Deadline()
	c.state.deadline = ok && time.Until(deadline) <= 5*time.Second
	c.snapshot = c.state.clone()
	return c.rolesSQLConnection.BeginTx(ctx, opts)
}
func (c *providerBindingsConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, table := range []string{"providers", "provider_connections", "provider_models", "model_provider_bindings", "models", "model_names"} {
		if strings.Contains(q, `FROM "`+table+`"`) {
			c.state.queries = append(c.state.queries, q)
			if c.state.fail == table {
				return nil, errors.New("controlled binding read failure")
			}
			s := c.snapshot
			switch table {
			case "providers":
				if s.provider.ID == "" {
					return effectiveSQLRows([]entity.Provider{})
				}
				return effectiveSQLRows([]entity.Provider{s.provider})
			case "provider_connections":
				return effectiveSQLRows(s.connections)
			case "provider_models":
				return effectiveSQLRows(s.supply)
			case "model_provider_bindings":
				if c.state.afterBindings != nil {
					c.state.afterBindings()
				}
				return effectiveSQLRows(s.bindings)
			case "models":
				return effectiveSQLRows(s.models)
			case "model_names":
				return effectiveSQLRows(s.names)
			}
		}
	}
	if strings.Contains(q, `FROM "registration_approval_applications"`) {
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows(c.snapshot.applications)
	}
	if strings.Contains(q, "role_permissions AS permission") {
		actor := c.current().users["usr_admin"]
		if actor.Role == entity.RoleMember {
			c.f.queries = append(c.f.queries, q)
			permission := ""
			for _, v := range rolesSQLStrings(args) {
				if slices.Contains(AvailablePermissions, v) {
					permission = v
				}
			}
			if c.f.deny[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{})
			}
			return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_custom", PermissionRoleID: "rol_custom", Permission: permission, AssignmentRoleID: "rol_custom", AssignmentUserID: actor.ID}})
		}
	}
	return c.rolesSQLConnection.QueryContext(ctx, q, args)
}
func providerBindingsSQLService(t *testing.T) (*Service, *rolesSQLFixture, *providerBindingsSQLState) {
	t.Helper()
	actor, _, _ := connectionMetadataTestRows()
	roles := &rolesSQLFixture{data: rolesSQLData{users: map[string]entity.User{actor.ID: actor}}, deny: map[string]bool{}}
	c, p, b, m, n := providerBindingsTestGraph()
	state := &providerBindingsSQLState{provider: entity.Provider{ID: "prv_target"}, connections: c, supply: p, bindings: b, models: m, names: n}
	pool := sql.OpenDB(providerBindingsConnector{roles, state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, roles, state
}
func TestProviderModelBindingsSQLSnapshotBudgetAndNoWrites(t *testing.T) {
	s, f, state := providerBindingsSQLService(t)
	state.afterBindings = func() { state.names[0].Name = "new-generation"; state.models = nil }
	got, err := s.GetProviderModelBindings(context.Background(), "usr_admin", "prv_target")
	if err != nil || *got.Items[0].Models[0].Name != "current-a" {
		t.Fatal("mixed read generation", got, err)
	}
	if len(state.queries) != 6 || len(f.queries) != 3 || len(f.writes) != 0 || len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || !state.deadline {
		t.Fatal("query/transaction/write budget changed", len(state.queries), len(f.queries), f.transactions, state.deadline)
	}
	for i, q := range state.queries {
		if i != 0 && !strings.Contains(q, "LIMIT") {
			t.Fatal("unbounded data read", q)
		}
	}
	if strings.Contains(strings.Join(state.queries, " "), "credentials") {
		t.Fatal("readiness or secret directory queried")
	}
}
func TestProviderModelBindingsSQLAuthorityIdentityAndOutages(t *testing.T) {
	for _, which := range []string{"provider_read_denied", "model_read_denied", "actor_alias", "disabled", "offboarded", "unknown_role", "zero_birth", "invalid_actor", "invalid_target", "provider_absent", "provider_alias", "connection_parent_alias", "pm_parent_alias", "binding_pm_alias", "model_missing", "names_outage", "cancelled"} {
		t.Run(which, func(t *testing.T) {
			s, f, state := providerBindingsSQLService(t)
			ctx := context.Background()
			actor, target := "usr_admin", "prv_target"
			want := error(providerModelBindingsUnavailable)
			switch which {
			case "provider_read_denied":
				f.deny["providers.read"] = true
				want = apperrors.ErrForbidden
			case "model_read_denied":
				f.deny["models.read_all"] = true
				want = apperrors.ErrForbidden
			case "actor_alias":
				f.actorAlias = true
				want = apperrors.ErrUnauthorized
			case "disabled":
				v := f.data.users[actor]
				v.Disabled = true
				f.data.users[actor] = v
				want = apperrors.ErrUnauthorized
			case "offboarded":
				v := f.data.users[actor]
				now := time.Now()
				v.OffboardedAt = &now
				f.data.users[actor] = v
				want = apperrors.ErrUnauthorized
			case "unknown_role":
				v := f.data.users[actor]
				v.Role = "unknown"
				f.data.users[actor] = v
				want = apperrors.ErrUnauthorized
			case "zero_birth":
				v := f.data.users[actor]
				v.CreatedAt = time.Time{}
				f.data.users[actor] = v
				want = apperrors.ErrUnauthorized
			case "invalid_actor":
				actor = "USR_admin"
				want = apperrors.ErrUnauthorized
			case "invalid_target":
				target = "prv_target "
				want = apperrors.ErrBadRequest
			case "provider_absent":
				state.provider.ID = ""
				want = apperrors.ErrNotFound
			case "provider_alias":
				state.provider.ID = "PRV_target"
				want = apperrors.ErrNotFound
			case "connection_parent_alias":
				state.connections[0].ProviderID = "PRV_target"
			case "pm_parent_alias":
				state.supply[1].ConnectionID = "con_a "
			case "binding_pm_alias":
				state.bindings[0].ProviderModelID = "PMD_a"
			case "model_missing":
				state.models = nil
			case "names_outage":
				state.fail = "model_names"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			got, err := s.GetProviderModelBindings(ctx, actor, target)
			if !errors.Is(err, want) || got != nil || len(f.writes) != 0 {
				t.Fatal("authority/error fail-closed changed", got, err, want)
			}
			if want == apperrors.ErrForbidden && len(state.queries) != 0 {
				t.Fatal("denied authority read private catalogue")
			}
		})
	}
}
func TestProviderModelBindingsSQLEmptyAndUnboundSkipDependentReads(t *testing.T) {
	for _, empty := range []bool{true, false} {
		s, _, state := providerBindingsSQLService(t)
		state.bindings = nil
		state.models = nil
		state.names = nil
		want := 4
		if empty {
			state.connections = nil
			state.supply = nil
			want = 2
		}
		got, err := s.GetProviderModelBindings(context.Background(), "usr_admin", "prv_target")
		if err != nil || len(state.queries) != want {
			t.Fatal("dependent empty read budget", len(state.queries), err)
		}
		for _, item := range got.Items {
			if item.BindingCount != 0 || item.Models == nil {
				t.Fatal("unbound became unknown or null", item)
			}
		}
	}
}

func TestProviderModelBindingsSQLManagedAdmissionBeforeCatalogue(t *testing.T) {
	for _, status := range []string{"approved", "pending", "rejected", "missing", "wrong_birth", "wrong_user"} {
		t.Run(status, func(t *testing.T) {
			s, f, state := providerBindingsSQLService(t)
			actor := f.data.users["usr_admin"]
			actor.Role = entity.RoleMember
			applicationID := "raa_01j00000000000000000000000"
			actor.ApprovalApplicationID = &applicationID
			f.data.users[actor.ID] = actor
			app := approvedMemberListApplication(actor, applicationID)
			switch status {
			case "pending":
				app.State = "pending"
				app.DecidedAt = nil
				app.DecisionActorID = nil
				app.DecisionReason = nil
			case "rejected":
				app.State = "rejected"
			case "wrong_birth":
				app.UserCreatedAt = app.UserCreatedAt.Add(time.Millisecond)
			case "wrong_user":
				app.UserID = "usr_other"
			}
			state.applications = []entity.RegistrationApprovalApplication{app}
			if status == "missing" {
				state.applications = nil
			}
			got, err := s.GetProviderModelBindings(context.Background(), actor.ID, "prv_target")
			if status == "approved" {
				if err != nil || got == nil || len(f.queries) != 4 {
					t.Fatal("current exact approved identity rejected", err, len(f.queries))
				}
			} else if err != apperrors.ErrUnauthorized || got != nil || len(state.queries) != 0 {
				t.Fatal("pending/foreign admission read catalogue", status, err)
			}
		})
	}
}
func TestProviderModelBindingsSQLMaximumBudgetAndOverflow(t *testing.T) {
	for _, count := range []int{10000, 10001} {
		s, _, state := providerBindingsSQLService(t)
		state.supply = state.supply[1:]
		state.bindings = nil
		state.models = nil
		state.names = nil
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("mdl_%05d", i)
			state.bindings = append(state.bindings, entity.ModelProviderBinding{ID: fmt.Sprintf("bnd_%05d", i), ProviderModelID: "pmd_a", ModelID: id})
			state.models = append(state.models, entity.Model{ID: id})
		}
		got, err := s.GetProviderModelBindings(context.Background(), "usr_admin", "prv_target")
		if count == 10000 {
			if err != nil || got.Items[0].BindingCount != count || len(state.queries) != 6 {
				t.Fatal("maximum supported complete/query-budget failed", err, len(state.queries))
			}
		} else if got != nil || err != providerModelBindingsOverflow || len(state.queries) != 4 {
			t.Fatal("overflow did not stop before dependent model reads", err, len(state.queries))
		}
	}
}
func TestProviderModelBindingsIndexedCandidateSQLBoundValues(t *testing.T) {
	pool := sql.OpenDB(providerBindingsConnector{})
	t.Cleanup(func() { _ = pool.Close() })
	for _, dialect := range []gorm.Dialector{postgres.New(postgres.Config{Conn: pool}), mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true})} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		values := []string{"pmd_Case", "pmd_case", "pmd_case ", "pmd_');DROP"}
		var rows []entity.ModelProviderBinding
		q := providerBindingsRows(db, "provider_model_id", values).Select("id", "model_id", "provider_model_id", "weight").Find(&rows)
		sql := q.Statement.SQL.String()
		if !strings.Contains(sql, "provider_model_id") || !strings.Contains(sql, " IN ") || !strings.Contains(sql, "ORDER BY") || !strings.Contains(sql, "LIMIT") || strings.Contains(sql, "DROP") || len(q.Statement.Vars) < len(values) || len(q.Statement.Vars) > len(values)+1 {
			t.Fatal("indexed bounded parameterization lost", sql, q.Statement.Vars)
		}
		for i, value := range values {
			if q.Statement.Vars[i] != value {
				t.Fatal("candidate identity normalized", q.Statement.Vars)
			}
		}
		if len(q.Statement.Vars) > len(values) && q.Statement.Vars[len(values)] != 10001 || len(q.Statement.Vars) == len(values) && !strings.Contains(sql, "LIMIT 10001") {
			t.Fatal("overflow sentinel absent", q.Statement.Vars)
		}
	}
}
