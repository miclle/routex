package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This adapter measures actual GORM statements/transaction options without a
// database. Journal reads use an isolated Bolt file, never an inferred birth.
type projectKeyObserverSQLState struct {
	owner                               entity.Project
	keys                                []entity.ProjectKey
	row                                 entity.ResourceLimit
	calendar                            entity.QuotaSetting
	queries, writes, commits, rollbacks int
	isolation                           driver.IsolationLevel
}
type projectKeyObserverSQLConnector struct{ state *projectKeyObserverSQLState }

func (c projectKeyObserverSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return projectKeyObserverSQLConnection(c), nil
}
func (c projectKeyObserverSQLConnector) Driver() driver.Driver {
	return projectKeyObserverSQLDriver(c)
}

type projectKeyObserverSQLDriver projectKeyObserverSQLConnector

func (c projectKeyObserverSQLDriver) Open(string) (driver.Conn, error) {
	return projectKeyObserverSQLConnection(c), nil
}

type projectKeyObserverSQLConnection projectKeyObserverSQLConnector

func (projectKeyObserverSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (projectKeyObserverSQLConnection) Close() error { return nil }
func (c projectKeyObserverSQLConnection) Begin() (driver.Tx, error) {
	return projectKeyObserverSQLTransaction(c), nil
}
func (c projectKeyObserverSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.isolation = opts.Isolation
	return projectKeyObserverSQLTransaction(c), nil
}

type projectKeyObserverSQLTransaction projectKeyObserverSQLConnector

func (t projectKeyObserverSQLTransaction) Commit() error   { t.state.commits++; return nil }
func (t projectKeyObserverSQLTransaction) Rollback() error { t.state.rollbacks++; return nil }
func (c projectKeyObserverSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries++
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1}})
	case strings.Contains(q, `FROM "projects"`):
		return effectiveSQLRows([]entity.Project{c.state.owner})
	case strings.Contains(q, `FROM "project_api_keys"`):
		if strings.Contains(q, "token_hash") || strings.Contains(q, "prefix") {
			return nil, errors.New("unexpected secret selection")
		}
		if strings.Contains(q, `"id",`) == false && strings.Contains(q, "id,project_id") == false {
			return nil, errors.New("unexpected root query")
		}
		if strings.Contains(q, "project_id =") {
			return effectiveSQLRows(c.state.keys)
		}
		return effectiveSQLRows(c.state.keys[:1])
	case strings.Contains(q, `FROM "resource_limits"`):
		return effectiveSQLRows([]entity.ResourceLimit{c.state.row})
	case strings.Contains(q, `FROM "quota_settings"`):
		return effectiveSQLRows([]entity.QuotaSetting{c.state.calendar})
	case strings.Contains(q, `FROM "pricing_settings"`):
		return effectiveSQLRows([]entity.PricingSetting{{ID: 1, PlatformCurrency: "USD"}})
	default:
		return nil, errors.New("unexpected observer query")
	}
}
func (c projectKeyObserverSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("observer without registered birth must not write")
}
func TestProjectKeyWarningObserverFixedQueryBudgetNoFabricatedBirth(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000, 10001} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, auth, owner, keys, row, _, calendar := projectKeyRuntimeFixture(t)
			root := keys[0]
			keys = make([]entity.ProjectKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("pky_%026d", i+1)
				auth.LimitRoots[keys[i].ID] = keys[i].ID
				auth.Quota.Created[limitAccount("key", keys[i].ID)] = keys[i].CreatedAt
			}
			state := &projectKeyObserverSQLState{owner: owner, keys: keys, row: row, calendar: calendar}
			pool := sql.OpenDB(projectKeyObserverSQLConnector{state})
			t.Cleanup(func() { _ = pool.Close() })
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			s.db = db
			queue, err := eventqueue.Open(filepath.Join(t.TempDir(), "usage.db"), 100, 1024)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = queue.Close() })
			if err = queue.EnableQuota("UTC", time.Now().Add(-time.Hour)); err != nil {
				t.Fatal(err)
			}
			s.recorder = &callRecorder{queue: queue}
			start := time.Now()
			err = s.observeMonthlyProjectKeyQuotaWarning(context.Background(), "key", root.ID)
			elapsed := time.Since(start)
			if n > 10000 {
				if !errors.Is(err, errQuotaNotificationIdentity) || state.queries != 4 || state.rollbacks != 1 {
					t.Fatal("overflow truncated/read further", err, state.queries)
				}
			} else if err != nil || state.queries != 7 || state.commits != 1 {
				t.Fatal("fixed admitted-owner query budget", err, state.queries, state.commits)
			}
			if state.writes != 0 || state.isolation != driver.IsolationLevel(sql.LevelReadCommitted) {
				t.Fatal("unknown birth wrote/observer did not request fresh locked reads")
			}
			t.Logf("observer %d rows: %d SQL queries, %s", n, state.queries, elapsed)
			if elapsed > 3*time.Second {
				t.Fatal("bounded observer exceeded refresh deadline")
			}
		})
	}
}
