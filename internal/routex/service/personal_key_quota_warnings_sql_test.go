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
type personalKeyObserverSQLState struct {
	owner                               entity.User
	apps                                []entity.RegistrationApprovalApplication
	keys                                []entity.APIKey
	row                                 entity.ResourceLimit
	calendar                            entity.QuotaSetting
	queries, writes, commits, rollbacks int
	isolation                           driver.IsolationLevel
}
type personalKeyObserverSQLConnector struct{ state *personalKeyObserverSQLState }

func (c personalKeyObserverSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return personalKeyObserverSQLConnection(c), nil
}
func (c personalKeyObserverSQLConnector) Driver() driver.Driver {
	return personalKeyObserverSQLDriver(c)
}

type personalKeyObserverSQLDriver personalKeyObserverSQLConnector

func (c personalKeyObserverSQLDriver) Open(string) (driver.Conn, error) {
	return personalKeyObserverSQLConnection(c), nil
}

type personalKeyObserverSQLConnection personalKeyObserverSQLConnector

func (personalKeyObserverSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (personalKeyObserverSQLConnection) Close() error { return nil }
func (c personalKeyObserverSQLConnection) Begin() (driver.Tx, error) {
	return personalKeyObserverSQLTransaction(c), nil
}
func (c personalKeyObserverSQLConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.isolation = opts.Isolation
	return personalKeyObserverSQLTransaction(c), nil
}

type personalKeyObserverSQLTransaction personalKeyObserverSQLConnector

func (t personalKeyObserverSQLTransaction) Commit() error   { t.state.commits++; return nil }
func (t personalKeyObserverSQLTransaction) Rollback() error { t.state.rollbacks++; return nil }
func (c personalKeyObserverSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries++
	switch {
	case strings.Contains(q, `FROM "governance_settings"`):
		return effectiveSQLRows([]entity.GovernanceSetting{{ID: 1}})
	case strings.Contains(q, `FROM "users"`):
		if strings.Contains(q, "password_hash") || !strings.Contains(q, "approval_application_id") {
			return nil, errors.New("incomplete admission or secret owner projection")
		}
		return effectiveSQLRows([]entity.User{c.state.owner})
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		return effectiveSQLRows(c.state.apps)
	case strings.Contains(q, `FROM "api_keys"`):
		if strings.Contains(q, "token_hash") || strings.Contains(q, "prefix") {
			return nil, errors.New("unexpected secret selection")
		}
		if strings.Contains(q, `"id",`) == false && strings.Contains(q, "id,user_id") == false {
			return nil, errors.New("unexpected root query")
		}
		if strings.Contains(q, "user_id =") {
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
func (c personalKeyObserverSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("observer without registered birth must not write")
}
func TestPersonalKeyWarningObserverFixedQueryBudgetNoFabricatedBirth(t *testing.T) {
	for _, n := range []int{500, 501, 1000, 10000, 10001} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, auth, owner, apps, keys, row, _, calendar := personalKeyRuntimeFixture(t)
			root := keys[0]
			keys = make([]entity.APIKey, n)
			for i := range keys {
				keys[i] = root
				keys[i].ID = fmt.Sprintf("key_%026d", i+1)
				auth.LimitRoots[keys[i].ID] = keys[i].ID
				auth.Quota.Created[limitAccount("key", keys[i].ID)] = keys[i].CreatedAt
			}
			auth.PersonalKeyStates = runtimeMemberKeyStates(keys)
			state := &personalKeyObserverSQLState{owner: owner, keys: keys, row: row, calendar: calendar}
			for _, a := range apps {
				state.apps = append(state.apps, a)
			}
			pool := sql.OpenDB(personalKeyObserverSQLConnector{state})
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
			err = s.observeMonthlyPersonalKeyQuotaWarning(context.Background(), "key", root.ID)
			elapsed := time.Since(start)
			if n > 10000 {
				if !errors.Is(err, errQuotaNotificationIdentity) || state.queries != 5 || state.rollbacks != 1 {
					t.Fatal("overflow truncated/read further", err, state.queries)
				}
			} else if err != nil || state.queries != 8 || state.commits != 1 {
				t.Fatal("fixed admitted-owner query budget", err, state.queries, state.commits)
			}
			if state.writes != 0 || state.isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("unknown birth wrote/inconsistent snapshot")
			}
			t.Logf("observer %d rows: %d SQL queries, %s", n, state.queries, elapsed)
			if elapsed > 3*time.Second {
				t.Fatal("bounded observer exceeded refresh deadline")
			}
		})
	}
}
