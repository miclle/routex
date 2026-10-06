package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This additive wrapper retains the existing exact actor/admission/permission
// fixture and models only Connection state, transaction rollback and publication.
// Real PostgreSQL/MySQL acceptance remains a separate gate.
type connectionMetadataSQLState struct {
	provider                       entity.Provider
	row                            entity.ProviderConnection
	aliasConnection, aliasProvider bool
	failTable                      string
	afterCommit                    func(*connectionMetadataSQLState, *rolesSQLFixture)
	afterCommitDone                bool
	afterRuntimeRead               func(*connectionMetadataSQLState)
	afterRuntimeReadDone           bool
}
type connectionMetadataSQLConnector struct {
	roles   *rolesSQLFixture
	control *roleDefinitionSQLControl
	state   *connectionMetadataSQLState
}

func (c connectionMetadataSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &connectionMetadataSQLConnection{roleDefinitionSQLConnection: &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.roles}, control: c.control}, state: c.state}, nil
}
func (c connectionMetadataSQLConnector) Driver() driver.Driver { return connectionMetadataSQLDriver(c) }

type connectionMetadataSQLDriver connectionMetadataSQLConnector

func (d connectionMetadataSQLDriver) Open(string) (driver.Conn, error) {
	return connectionMetadataSQLConnector(d).Connect(context.Background())
}

type connectionMetadataSQLConnection struct {
	*roleDefinitionSQLConnection
	state *connectionMetadataSQLState
	row   *entity.ProviderConnection
}

func (c *connectionMetadataSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *connectionMetadataSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.roleDefinitionSQLConnection.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	row := c.state.row
	c.row = &row
	return connectionMetadataSQLTransaction{Tx: tx, c: c, readOnly: opts.ReadOnly}, nil
}

type connectionMetadataSQLTransaction struct {
	driver.Tx
	c        *connectionMetadataSQLConnection
	readOnly bool
}

func (t connectionMetadataSQLTransaction) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.c.state.row = *t.c.row
	t.c.row = nil
	if t.readOnly && len(t.c.f.data.audits) > 0 && t.c.state.afterRuntimeRead != nil && !t.c.state.afterRuntimeReadDone {
		t.c.state.afterRuntimeReadDone = true
		t.c.state.afterRuntimeRead(t.c.state)
	}
	if t.c.state.afterCommit != nil && !t.c.state.afterCommitDone && len(t.c.f.data.audits) > 0 {
		t.c.state.afterCommitDone = true
		t.c.state.afterCommit(t.c.state, t.c.f)
	}
	return nil
}
func (t connectionMetadataSQLTransaction) Rollback() error { t.c.row = nil; return t.Tx.Rollback() }
func (c *connectionMetadataSQLConnection) currentConnection() entity.ProviderConnection {
	if c.row != nil {
		return *c.row
	}
	return c.state.row
}
func (c *connectionMetadataSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.state.failTable != "" && strings.Contains(q, c.state.failTable) {
		return nil, errors.New("controlled Connection read unavailable")
	}
	switch {
	case strings.Contains(q, `FROM "provider_connections"`):
		c.f.queries = append(c.f.queries, q)
		row := c.currentConnection()
		if c.state.aliasConnection && strings.Contains(q, "WHERE") {
			row.ID = "CON_TARGET"
		}
		return effectiveSQLRows([]entity.ProviderConnection{row})
	case strings.Contains(q, `FROM "providers"`):
		c.f.queries = append(c.f.queries, q)
		row := c.state.provider
		if c.state.aliasProvider && strings.Contains(q, "WHERE") {
			row.ID = "PRV_TARGET"
		}
		return effectiveSQLRows([]entity.Provider{row})
	case strings.Contains(q, `FROM "users"`) && !strings.Contains(q, "WHERE"):
		c.f.queries = append(c.f.queries, q)
		rows := []entity.User{}
		for _, u := range c.current().users {
			rows = append(rows, u)
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, `FROM "egress_settings"`):
		return effectiveSQLRows([]entity.EgressSetting{{ID: 1, ETag: "0"}})
	case strings.Contains(q, `FROM "pricing_settings"`):
		return effectiveSQLRows([]entity.PricingSetting{{ID: 1, ETag: "0", PlatformCurrency: "USD"}})
	case strings.Contains(q, `FROM "quota_settings"`):
		return effectiveSQLRows([]entity.QuotaSetting{{ID: 1, ETag: "0", TimeZone: "UTC"}})
	}
	for _, table := range []string{"resource_limits", "api_keys", "api_key_models", "user_model_grants", "models", "model_names", "provider_credentials", "provider_models", "credential_model_accesses", "model_provider_bindings", "egresses", "projects", "project_managers", "project_api_keys", "project_api_key_models", "project_model_grants", "sessions", "teams", "team_memberships", "team_model_grants", "pricing_exchange_rates", "model_prices", "price_rates", "reservation_bounds"} {
		if strings.Contains(q, `FROM "`+table+`"`) {
			c.f.queries = append(c.f.queries, q)
			return &adminOverviewRows{columns: []string{"id"}}, nil
		}
	}
	rows, err := c.roleDefinitionSQLConnection.QueryContext(ctx, q, args)
	if err != nil {
		return nil, fmt.Errorf("%w (query %s)", err, q)
	}
	return rows, nil
}
func (c *connectionMetadataSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `UPDATE "provider_connections"`) {
		c.f.writes = append(c.f.writes, q)
		if c.row == nil {
			return nil, errors.New("mutation outside transaction")
		}
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		for i, col := range strings.Split(set, ",") {
			switch strings.Trim(strings.Split(col, "=")[0], " \"`") {
			case "name":
				c.row.Name = args[i].Value.(string)
			case "e_tag":
				c.row.ETag = args[i].Value.(string)
			default:
				return nil, errors.New("unexpected Connection mutation")
			}
		}
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(q, `INSERT INTO "runtime_publications"`) {
		return nil, errors.New("offline operational publication persistence unavailable")
	}
	return c.roleDefinitionSQLConnection.ExecContext(ctx, q, args)
}
func connectionMetadataSQLService(t *testing.T) (*Service, *rolesSQLFixture, *roleDefinitionSQLControl, *connectionMetadataSQLState) {
	t.Helper()
	_, roles, control := roleDefinitionSQLService(t)
	actor, provider, row := connectionMetadataTestRows()
	roles.data.users[actor.ID] = actor
	state := &connectionMetadataSQLState{provider: provider, row: row}
	pool := sql.OpenDB(connectionMetadataSQLConnector{roles, control, state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db, runtime: &gatewayRuntime{done: make(chan struct{})}}, roles, control, state
}
func TestConnectionMetadataSQLIndependentAuthorityAndExactIdentity(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name    string
		prepare func(*rolesSQLFixture, *roleDefinitionSQLControl, *connectionMetadataSQLState)
		want    error
	}{
		{"read_denied", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *connectionMetadataSQLState) {
			f.deny["providers.read"] = true
		}, apperrors.ErrForbidden},
		{"actor_alias", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *connectionMetadataSQLState) {
			f.actorAlias = true
		}, apperrors.ErrUnauthorized},
		{"connection_alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, s *connectionMetadataSQLState) {
			s.aliasConnection = true
		}, apperrors.ErrNotFound},
		{"provider_alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, s *connectionMetadataSQLState) {
			s.aliasProvider = true
		}, apperrors.ErrNotFound},
		{"pending_actor", func(f *rolesSQLFixture, c *roleDefinitionSQLControl, _ *connectionMetadataSQLState) {
			roleDefinitionSQLPending(f, c)
		}, apperrors.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, f, c, state := connectionMetadataSQLService(t)
			test.prepare(f, c, state)
			got, err := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
			if got != nil || err != test.want || len(f.writes) != 0 {
				t.Fatal(got, err, f.writes)
			}
		})
	}
	t.Run("read_only_does_not_grant_write", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		f.deny["providers.write"] = true
		record, err := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		if err != nil || record.CanEdit {
			t.Fatal(record, err)
		}
		got, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
		if got != nil || err != apperrors.ErrForbidden || len(f.writes) != 0 {
			t.Fatal(got, err)
		}
	})
	t.Run("writer_requires_no_read_permission", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		f.deny["providers.read"] = true
		result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
		if err != nil || !result.RuntimeApplied || !result.Changed || result.Connection.Name != "Changed" || len(f.data.audits) != 1 {
			_, detailErr := s.loadRuntimeData(ctx)
			t.Fatal(result, err, detailErr, f.queries)
		}
		for _, query := range f.queries {
			if strings.Contains(query, "role_permissions AS permission") && strings.Contains(query, "providers.read") {
				t.Fatal("inline read prerequisite")
			}
		}
	})
}
func TestConnectionMetadataSQLAtomicRenameRetryAndSharedConflict(t *testing.T) {
	ctx := context.Background()
	t.Run("audit_failure_rolls_back", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		original := state.row
		record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		f.failAudit = true
		result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
		if result != nil || err != apperrors.ErrInternal || !reflect.DeepEqual(state.row, original) || len(f.data.audits) != 0 {
			t.Fatal(result, err, state.row)
		}
	})
	t.Run("success_then_exact_retry", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		original := state.row
		record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		input := ConnectionMetadataInput{"Changed", "Reviewed"}
		result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if err != nil || !result.RuntimeApplied || !result.Changed || state.row.ETag == original.ETag || state.row.EgressMode != original.EgressMode || state.row.BaseURL != original.BaseURL || state.row.Protocol != original.Protocol {
			_, detailErr := s.loadRuntimeData(ctx)
			t.Fatal(result, err, detailErr, f.queries)
		}
		result, err = s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if err != nil || result.Changed || len(f.data.audits) != 1 {
			t.Fatal("current retry duplicated audit", result, err)
		}
		for _, q := range f.writes {
			if strings.Contains(q, `UPDATE "provider_connections"`) && (strings.Contains(q, "base_url") || strings.Contains(q, "protocol") || strings.Contains(q, "egress_mode")) {
				t.Fatal("broad write", q)
			}
		}
		if _, ok := connectionMetadataAuditProjection(f.data.audits[0]); !ok {
			t.Fatal("committed audit not typed")
		}
	})
	t.Run("concurrent_egress_revision_conflicts", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		state.row.ETag = "rev_egress"
		state.row.EgressMode = "direct"
		result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
		if result != nil || err != catalogConflict || len(f.writes) != 0 {
			t.Fatal(result, err)
		}
	})
	t.Run("closed_publication_remains_uncertain", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
		close(s.runtime.done)
		result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
		if result != nil || err != connectionMetadataUnavailable || state.row.Name != "Changed" || len(f.data.audits) != 1 {
			t.Fatal(result, err)
		}
	})
	for _, fault := range []string{"actor_disabled", "actor_recreated", "connection_recreated", "provider_recreated", "write_revoked", "later_name"} {
		t.Run(fault, func(t *testing.T) {
			s, f, _, state := connectionMetadataSQLService(t)
			record, _ := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
			state.afterCommit = func(state *connectionMetadataSQLState, f *rolesSQLFixture) {
				switch fault {
				case "actor_disabled":
					u := f.data.users["usr_admin"]
					u.Disabled = true
					f.data.users[u.ID] = u
				case "actor_recreated":
					u := f.data.users["usr_admin"]
					u.CreatedAt = u.CreatedAt.Add(1000000)
					f.data.users[u.ID] = u
				case "connection_recreated":
					state.row.CreatedAt = state.row.CreatedAt.Add(1000000)
				case "provider_recreated":
					state.provider.CreatedAt = state.provider.CreatedAt.Add(1000000)
				case "write_revoked":
					f.deny["providers.write"] = true
				case "later_name":
					state.row.Name = "Concurrent"
				}
			}
			result, err := s.WriteConnectionMetadata(ctx, "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
			if result != nil || err == nil || len(f.data.audits) != 1 {
				t.Fatal("postcommit authority/identity lost", result, err)
			}
		})
	}
	t.Run("read_snapshot_is_repeatable_and_bounded", func(t *testing.T) {
		s, f, _, state := connectionMetadataSQLService(t)
		if _, err := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID); err != nil {
			t.Fatal(err)
		}
		if len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || len(f.queries) != 5 || slices.ContainsFunc(f.queries, func(q string) bool { return strings.Contains(q, "provider_credentials") }) {
			t.Fatal(f.transactions, f.queries)
		}
	})
}

func TestConnectionMetadataSQLUnavailableReadAndCanceledContext(t *testing.T) {
	for _, table := range []string{"users", "role_permissions", "provider_connections", "providers"} {
		t.Run(table, func(t *testing.T) {
			s, f, _, state := connectionMetadataSQLService(t)
			state.failTable = table
			record, err := s.GetConnectionMetadata(context.Background(), "usr_admin", state.row.ID)
			if record != nil || err != connectionMetadataUnavailable || len(f.writes) != 0 {
				t.Fatal(record, err, f.writes)
			}
		})
	}
	s, f, _, state := connectionMetadataSQLService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record, err := s.GetConnectionMetadata(ctx, "usr_admin", state.row.ID)
	if record != nil || err != connectionMetadataUnavailable || len(f.writes) != 0 {
		t.Fatal(record, err)
	}
}

func TestConnectionMetadataSQLRejectsSourceChangedBetweenPublishAndConfirmation(t *testing.T) {
	s, f, _, state := connectionMetadataSQLService(t)
	record, err := s.GetConnectionMetadata(context.Background(), "usr_admin", state.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	state.afterRuntimeRead = func(state *connectionMetadataSQLState) {
		state.row.ETag = "rev_concurrent"
		state.row.EgressMode = "direct"
	}
	result, err := s.WriteConnectionMetadata(context.Background(), "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{"Changed", "Reviewed"})
	if result != nil || err != connectionMetadataUnavailable || state.row.Name != "Changed" || !state.afterRuntimeReadDone || len(f.data.audits) != 1 {
		t.Fatal("mixed source confirmation accepted", result, err, state.row)
	}
}

func TestConnectionMetadataSQLUsesGORMConnectionRevisionColumn(t *testing.T) {
	s, f, _, state := connectionMetadataSQLService(t)
	statement := &gorm.Statement{DB: s.db}
	if err := statement.Parse(&entity.ProviderConnection{}); err != nil {
		t.Fatal(err)
	}
	field := statement.Schema.LookUpField("ETag")
	if field == nil || field.DBName != "e_tag" {
		t.Fatal("unexpected released GORM revision column", field)
	}
	record, err := s.GetConnectionMetadata(context.Background(), "usr_admin", state.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.WriteConnectionMetadata(context.Background(), "usr_admin", record.ID, record.ETag, ConnectionMetadataInput{Name: "Column checked", Reason: "Review real schema mapping"})
	if err != nil || result == nil || !result.Changed || !result.RuntimeApplied || state.row.ETag == "0" {
		t.Fatal("rename did not use the released revision column", result, err)
	}
	updates := 0
	for _, q := range f.writes {
		if !strings.HasPrefix(q, `UPDATE "provider_connections"`) {
			continue
		}
		updates++
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		columns := []string{}
		for _, assignment := range strings.Split(set, ",") {
			columns = append(columns, strings.Trim(strings.Split(assignment, "=")[0], " \"`"))
		}
		slices.Sort(columns)
		if !reflect.DeepEqual(columns, []string{field.DBName, "name"}) {
			t.Fatal("rename updated an unknown or unrelated column", q)
		}
	}
	if updates != 1 {
		t.Fatal("expected one exact configuration update", f.writes)
	}
}
