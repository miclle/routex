package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type credentialStatisticsSQLState struct {
	credential     entity.ProviderCredential
	rows           []credentialStatisticsAttempt
	fail           bool
	readOnly       bool
	isolation      driver.IsolationLevel
	attemptQueries int
	attemptArgs    []driver.NamedValue
}
type credentialStatisticsSQLConnector struct {
	provider providerMetadataSQLConnector
	stats    *credentialStatisticsSQLState
}

func (c credentialStatisticsSQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	base, err := c.provider.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &credentialStatisticsSQLConnection{providerMetadataSQLConnection: base.(*providerMetadataSQLConnection), stats: c.stats}, nil
}
func (c credentialStatisticsSQLConnector) Driver() driver.Driver {
	return credentialStatisticsSQLDriver(c)
}

type credentialStatisticsSQLDriver credentialStatisticsSQLConnector

func (d credentialStatisticsSQLDriver) Open(string) (driver.Conn, error) {
	return credentialStatisticsSQLConnector(d).Connect(context.Background())
}

type credentialStatisticsSQLConnection struct {
	*providerMetadataSQLConnection
	stats *credentialStatisticsSQLState
}

func (c *credentialStatisticsSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.stats.readOnly, c.stats.isolation = opts.ReadOnly, opts.Isolation
	return c.providerMetadataSQLConnection.BeginTx(ctx, opts)
}
func (c *credentialStatisticsSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `FROM "provider_credentials"`) {
		c.f.queries = append(c.f.queries, q)
		return effectiveSQLRows([]entity.ProviderCredential{c.stats.credential})
	}
	if strings.Contains(q, `FROM "call_attempts"`) {
		c.f.queries = append(c.f.queries, q)
		c.stats.attemptQueries++
		c.stats.attemptArgs = append([]driver.NamedValue(nil), args...)
		if c.stats.fail {
			return nil, errors.New("private database failure")
		}
		return effectiveSQLRows(c.stats.rows)
	}
	return c.providerMetadataSQLConnection.QueryContext(ctx, q, args)
}
func (c *credentialStatisticsSQLConnection) ExecContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	c.f.writes = append(c.f.writes, q)
	return nil, errors.New("statistics must remain read-only")
}
func credentialStatisticsSQLService(t *testing.T) (*Service, *rolesSQLFixture, *roleDefinitionSQLControl, *providerMetadataSQLState, *credentialStatisticsSQLState) {
	t.Helper()
	_, roles, control, provider := providerMetadataSQLService(t)
	stats := &credentialStatisticsSQLState{credential: entity.ProviderCredential{ID: "crd_target", ConnectionID: provider.base.row.ID, CreatedAt: provider.base.row.CreatedAt}}
	pool := sql.OpenDB(credentialStatisticsSQLConnector{provider: providerMetadataSQLConnector{base: connectionMetadataSQLConnector{roles: roles, control: control, state: provider.base}, state: provider}, stats: stats})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, roles, control, provider, stats
}

func TestCredentialAttemptStatisticsSQLAuthorityAndBoundedRead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*rolesSQLFixture, *roleDefinitionSQLControl, *providerMetadataSQLState, *credentialStatisticsSQLState)
		want    error
	}{
		{"read only does not require write", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			f.deny["providers.write"] = true
		}, nil},
		{"write cannot substitute read", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			f.deny["providers.read"] = true
		}, apperrors.ErrForbidden},
		{"actor alias", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			f.actorAlias = true
		}, apperrors.ErrUnauthorized},
		{"disabled actor", func(f *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			u := f.data.users["usr_admin"]
			u.Disabled = true
			f.data.users[u.ID] = u
		}, apperrors.ErrUnauthorized},
		{"pending actor", func(f *rolesSQLFixture, c *roleDefinitionSQLControl, _ *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			roleDefinitionSQLPending(f, c)
		}, apperrors.ErrUnauthorized},
		{"Provider alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, p *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			p.base.aliasProvider = true
		}, apperrors.ErrNotFound},
		{"Credential alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, s *credentialStatisticsSQLState) {
			s.credential.ID = "CRD_TARGET"
		}, apperrors.ErrNotFound},
		{"Connection alias", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, p *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			p.base.aliasConnection = true
		}, apperrors.ErrNotFound},
		{"foreign Provider", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, p *providerMetadataSQLState, _ *credentialStatisticsSQLState) {
			p.base.row.ProviderID = "prv_foreign"
		}, apperrors.ErrNotFound},
		{"Credential birth unknown", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, s *credentialStatisticsSQLState) {
			s.credential.CreatedAt = time.Time{}
		}, credentialAttemptStatisticsUnavailable},
		{"database error sanitized", func(_ *rolesSQLFixture, _ *roleDefinitionSQLControl, _ *providerMetadataSQLState, s *credentialStatisticsSQLState) {
			s.fail = true
		}, credentialAttemptStatisticsUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, roles, control, provider, stats := credentialStatisticsSQLService(t)
			tc.prepare(roles, control, provider, stats)
			got, err := s.GetCredentialAttemptStatistics(context.Background(), "usr_admin", provider.base.provider.ID, []string{"crd_target"})
			if err != tc.want || (got == nil) != (tc.want != nil) || len(roles.writes) != 0 {
				t.Fatal(got, err, roles.writes)
			}
			if !stats.readOnly || stats.isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("coherent read-only snapshot required")
			}
			if got != nil {
				if len(got.Items) != 1 || got.Items[0].FailureStreak.State != "no_records" || got.Items[0].ConnectionID != provider.base.row.ID || !got.RecordedOnly {
					t.Fatal(got)
				}
				if stats.attemptQueries != 1 || len(stats.attemptArgs) != 3 ||
					stats.attemptArgs[0].Ordinal != 1 || stats.attemptArgs[0].Value != "crd_target" ||
					stats.attemptArgs[1].Ordinal != 2 || stats.attemptArgs[1].Value != "crd_target" ||
					stats.attemptArgs[2].Ordinal != 3 || stats.attemptArgs[2].Value != int64(101) {
					t.Fatal("exact Credential predicates and parameterized sentinel bound required", stats.attemptQueries, stats.attemptArgs)
				}
				for _, q := range roles.queries {
					if !strings.Contains(q, `FROM "call_attempts"`) {
						continue
					}
					if !strings.HasSuffix(q, `LIMIT $3`) || !strings.Contains(q, `WHERE credential_id = $1 AND "credential_id" = $2`) || !strings.Contains(q, `"completed_at" DESC`) || !strings.Contains(q, `"id" COLLATE "C" DESC`) || strings.Contains(q, "snapshot_id") || strings.Contains(q, `"completed_at" <=`) {
						t.Fatal(q)
					}
				}
			}
		})
	}
	s, roles, _, p, stats := credentialStatisticsSQLService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := s.GetCredentialAttemptStatistics(ctx, "usr_admin", p.base.provider.ID, []string{"crd_target"}); got != nil || err != credentialAttemptStatisticsUnavailable || stats.attemptQueries != 0 || len(roles.writes) != 0 {
		t.Fatal(got, err)
	}
}
