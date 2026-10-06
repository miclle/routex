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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Reuse exact admission/permission and runtime query fixtures. Only Provider
// transaction state is added here; these checks do not replace real drivers.
type providerMetadataSQLState struct {
	base                                  *connectionMetadataSQLState
	afterCommit                           func(*providerMetadataSQLState, *rolesSQLFixture)
	afterRuntimeRead                      func(*providerMetadataSQLState)
	afterCommitDone, afterRuntimeReadDone bool
}
type providerMetadataSQLConnector struct {
	base  connectionMetadataSQLConnector
	state *providerMetadataSQLState
}

func (c providerMetadataSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &providerMetadataSQLConnection{connectionMetadataSQLConnection: &connectionMetadataSQLConnection{roleDefinitionSQLConnection: &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.base.roles}, control: c.base.control}, state: c.base.state}, state: c.state}, nil
}
func (c providerMetadataSQLConnector) Driver() driver.Driver { return providerMetadataSQLDriver(c) }

type providerMetadataSQLDriver providerMetadataSQLConnector

func (d providerMetadataSQLDriver) Open(string) (driver.Conn, error) {
	return providerMetadataSQLConnector(d).Connect(context.Background())
}

type providerMetadataSQLConnection struct {
	*connectionMetadataSQLConnection
	state    *providerMetadataSQLState
	provider *entity.Provider
}

func (c *providerMetadataSQLConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *providerMetadataSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.connectionMetadataSQLConnection.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	row := c.state.base.provider
	c.provider = &row
	return providerMetadataSQLTransaction{tx, c, opts.ReadOnly}, nil
}

type providerMetadataSQLTransaction struct {
	driver.Tx
	c        *providerMetadataSQLConnection
	readOnly bool
}

func (t providerMetadataSQLTransaction) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	t.c.state.base.provider = *t.c.provider
	t.c.provider = nil
	if len(t.c.f.data.audits) > 0 && t.readOnly && t.c.state.afterRuntimeRead != nil && !t.c.state.afterRuntimeReadDone {
		t.c.state.afterRuntimeReadDone = true
		t.c.state.afterRuntimeRead(t.c.state)
	}
	if len(t.c.f.data.audits) > 0 && t.c.state.afterCommit != nil && !t.c.state.afterCommitDone {
		t.c.state.afterCommitDone = true
		t.c.state.afterCommit(t.c.state, t.c.f)
	}
	return nil
}
func (t providerMetadataSQLTransaction) Rollback() error { t.c.provider = nil; return t.Tx.Rollback() }
func (c *providerMetadataSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `FROM "providers"`) {
		if c.state.base.failTable == "providers" {
			return nil, errors.New("controlled Provider read unavailable")
		}
		c.f.queries = append(c.f.queries, q)
		row := c.state.base.provider
		if c.provider != nil {
			row = *c.provider
		}
		if c.state.base.aliasProvider && strings.Contains(q, "WHERE") {
			row.ID = "PRV_TARGET"
		}
		return effectiveSQLRows([]entity.Provider{row})
	}
	return c.connectionMetadataSQLConnection.QueryContext(ctx, q, args)
}
func (c *providerMetadataSQLConnection) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, `UPDATE "providers"`) {
		c.f.writes = append(c.f.writes, q)
		if c.provider == nil {
			return nil, errors.New("mutation outside transaction")
		}
		set := strings.Split(strings.Split(q, " SET ")[1], " WHERE ")[0]
		if strings.Trim(strings.Split(set, "=")[0], " \"`") != "name" || strings.Contains(set, ",") {
			return nil, errors.New("unexpected Provider mutation")
		}
		c.provider.Name = args[0].Value.(string)
		return driver.RowsAffected(1), nil
	}
	return c.connectionMetadataSQLConnection.ExecContext(ctx, q, args)
}
func providerMetadataSQLService(t *testing.T) (*Service, *rolesSQLFixture, *roleDefinitionSQLControl, *providerMetadataSQLState) {
	t.Helper()
	_, roles, control, base := connectionMetadataSQLService(t)
	state := &providerMetadataSQLState{base: base}
	pool := sql.OpenDB(providerMetadataSQLConnector{connectionMetadataSQLConnector{roles, control, base}, state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db, runtime: &gatewayRuntime{done: make(chan struct{})}}, roles, control, state
}
func TestProviderMetadataSQLIndependentAuthorityAndExactIdentity(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name    string
		prepare func(*rolesSQLFixture, *roleDefinitionSQLControl, *providerMetadataSQLState)
		want    error
	}{
		{"read_denied", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			f.deny["providers.read"] = true
		}, apperrors.ErrForbidden},
		{"actor_alias", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			f.actorAlias = true
		}, apperrors.ErrUnauthorized},
		{"provider_alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, s *providerMetadataSQLState) {
			s.base.aliasProvider = true
		}, apperrors.ErrNotFound},
		{"pending_actor", func(f *rolesSQLFixture, c *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			roleDefinitionSQLPending(f, c)
		}, apperrors.ErrUnauthorized},
		{"disabled_actor", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState) {
			u := f.data.users["usr_admin"]
			u.Disabled = true
			f.data.users[u.ID] = u
		}, apperrors.ErrUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, f, c, state := providerMetadataSQLService(t)
			test.prepare(f, c, state)
			got, err := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
			if got != nil || err != test.want || len(f.writes) != 0 {
				t.Fatal(got, err, f.writes)
			}
		})
	}
	t.Run("reader_cannot_write", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		f.deny["providers.write"] = true
		record, err := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		if err != nil || record.CanEdit {
			t.Fatal(record, err)
		}
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
		if got != nil || err != apperrors.ErrForbidden || len(f.writes) != 0 {
			t.Fatal(got, err)
		}
	})
	t.Run("writer_needs_no_read", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		record, err := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		if err != nil {
			t.Fatal(err)
		}
		f.deny["providers.read"] = true
		result, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
		if err != nil || result == nil || !result.Changed || !result.RuntimeApplied {
			t.Fatal(result, err)
		}
		if _, err := s.GetProviderMetadata(ctx, "usr_admin", record.ID); err != apperrors.ErrForbidden {
			t.Fatal(err)
		}
	})
	t.Run("one_bounded_read_snapshot", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		if _, err := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID); err != nil {
			t.Fatal(err)
		}
		if len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || len(f.queries) != 4 || slices.ContainsFunc(f.queries, func(q string) bool { return strings.Contains(q, "provider_connections") }) {
			t.Fatal(f.transactions, f.queries)
		}
	})
}
func TestProviderMetadataSQLAtomicCurrentOnlyRetry(t *testing.T) {
	ctx := context.Background()
	t.Run("audit_rollback", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		original := state.base.provider
		record, _ := s.GetProviderMetadata(ctx, "usr_admin", original.ID)
		f.failAudit = true
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
		if got != nil || err != apperrors.ErrInternal || !reflect.DeepEqual(original, state.base.provider) || len(f.data.audits) != 0 {
			t.Fatal(got, err, state.base.provider)
		}
	})
	t.Run("name_only_success_exact_retry_no_audit", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		originalProvider, originalConnection := state.base.provider, state.base.row
		record, _ := s.GetProviderMetadata(ctx, "usr_admin", originalProvider.ID)
		input := ProviderMetadataInput{"\ufeffChanged\ufeff", "\ufeffReviewed\ufeff"}
		result, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if err != nil || result == nil || !result.Changed || !result.RuntimeApplied || result.Provider.Name != input.Name {
			t.Fatal(result, err)
		}
		expected := originalProvider
		expected.Name = input.Name
		if !reflect.DeepEqual(expected, state.base.provider) || !reflect.DeepEqual(originalConnection, state.base.row) || len(f.data.audits) != 1 {
			t.Fatal(state.base.provider, state.base.row)
		}
		result, err = s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if err != nil || result.Changed || len(f.data.audits) != 1 {
			t.Fatal(result, err)
		}
		if _, ok := providerMetadataAuditProjection(f.data.audits[0]); !ok {
			t.Fatal("invalid typed audit")
		}
	})
	t.Run("concurrent_name_conflict_no_write", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		record, _ := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		state.base.provider.Name = "Concurrent"
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
		if got != nil || err != catalogConflict || len(f.writes) != 0 {
			t.Fatal(got, err)
		}
	})
	t.Run("closed_runtime_commit_remains_uncertain", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		record, _ := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		close(s.runtime.done)
		input := ProviderMetadataInput{"Changed", "Reviewed"}
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if got != nil || err != providerMetadataUnavailable || state.base.provider.Name != input.Name || len(f.data.audits) != 1 {
			t.Fatal(got, err)
		}
		got, err = s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if got != nil || err != providerMetadataUnavailable || len(f.data.audits) != 1 {
			t.Fatal(got, err)
		}
		s.runtime = &gatewayRuntime{done: make(chan struct{})}
		got, err = s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, input)
		if err != nil || got == nil || got.Changed || !got.RuntimeApplied || len(f.data.audits) != 1 {
			t.Fatal(got, err)
		}
	})
	for _, fault := range []string{"disabled", "actor_birth", "provider_birth", "write_revoked", "later_name"} {
		t.Run(fault, func(t *testing.T) {
			s, f, _, state := providerMetadataSQLService(t)
			record, _ := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
			state.afterCommit = func(s *providerMetadataSQLState, f *rolesSQLFixture) {
				switch fault {
				case "disabled":
					u := f.data.users["usr_admin"]
					u.Disabled = true
					f.data.users[u.ID] = u
				case "actor_birth":
					u := f.data.users["usr_admin"]
					u.CreatedAt = u.CreatedAt.Add(time.Millisecond)
					f.data.users[u.ID] = u
				case "provider_birth":
					s.base.provider.CreatedAt = s.base.provider.CreatedAt.Add(time.Millisecond)
				case "write_revoked":
					f.deny["providers.write"] = true
				case "later_name":
					s.base.provider.Name = "Concurrent"
				}
			}
			got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
			if got != nil || err == nil || len(f.data.audits) != 1 {
				t.Fatal("postcommit identity/authority lost", got, err)
			}
		})
	}
	t.Run("later_child_source_unconfirmed", func(t *testing.T) {
		s, f, _, state := providerMetadataSQLService(t)
		record, _ := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
		state.afterRuntimeRead = func(state *providerMetadataSQLState) { state.base.row.ETag = "rev_concurrent" }
		got, err := s.WriteProviderMetadata(ctx, "usr_admin", record.ID, record.ETag, ProviderMetadataInput{"Changed", "Reviewed"})
		if got != nil || err != providerMetadataUnavailable || len(f.data.audits) != 1 {
			t.Fatal(got, err)
		}
	})
}
func TestProviderMetadataSQLUnavailableReads(t *testing.T) {
	for _, table := range []string{"users", "role_permissions", "providers"} {
		t.Run(table, func(t *testing.T) {
			s, f, _, state := providerMetadataSQLService(t)
			state.base.failTable = table
			record, err := s.GetProviderMetadata(context.Background(), "usr_admin", state.base.provider.ID)
			if record != nil || err != providerMetadataUnavailable || len(f.writes) != 0 {
				t.Fatal(record, err)
			}
		})
	}
	s, f, _, state := providerMetadataSQLService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	record, err := s.GetProviderMetadata(ctx, "usr_admin", state.base.provider.ID)
	if record != nil || err != providerMetadataUnavailable || len(f.writes) != 0 {
		t.Fatal(record, err)
	}
}
