package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type rollingWarningSQLState struct {
	state                      []entity.PersonalRollingQuotaWarningState
	writes, commits, rollbacks int
	failInbox                  bool
}
type rollingWarningSQLConnector struct{ state *rollingWarningSQLState }

func (c rollingWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return rollingWarningSQLConnection(c), nil
}
func (c rollingWarningSQLConnector) Driver() driver.Driver { return rollingWarningSQLDriver(c) }

type rollingWarningSQLDriver rollingWarningSQLConnector

func (d rollingWarningSQLDriver) Open(string) (driver.Conn, error) {
	return rollingWarningSQLConnection(d), nil
}

type rollingWarningSQLConnection rollingWarningSQLConnector

func (rollingWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared rolling query")
}
func (rollingWarningSQLConnection) Close() error { return nil }
func (c rollingWarningSQLConnection) Begin() (driver.Tx, error) {
	return rollingWarningSQLTransaction{c.state, c.state.writes}, nil
}
func (c rollingWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !strings.Contains(q, `FROM "personal_rolling_quota_warning_states"`) {
		return nil, errors.New("unexpected rolling read")
	}
	return effectiveSQLRows(c.state.state)
}
func (c rollingWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if strings.Contains(q, `INSERT INTO "personal_rolling_quota_warning_inboxes"`) && c.state.failInbox {
		return nil, errors.New("controlled inbox failure")
	}
	if !strings.Contains(q, `"personal_rolling_quota_warning_`) {
		return nil, errors.New("unexpected rolling write")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type rollingWarningSQLTransaction struct {
	state  *rollingWarningSQLState
	before int
}

func (t rollingWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t rollingWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func rollingWarningSQLDatabase(t *testing.T, state *rollingWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(rollingWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	return db
}
func TestPersonalRollingWarningAtomicStateObservationInbox(t *testing.T) {
	r, b, p := rollingWarningFixture()
	sample := personalRollingWarningSamples(r, b, p)[0]
	for _, kind := range []string{"first", "inbox_failure", "publication_lost", "replay", "alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &rollingWarningSQLState{failInbox: kind == "inbox_failure"}
			if kind == "replay" || kind == "alias" {
				saved, _, e := advancePersonalRollingWarning(entity.PersonalRollingQuotaWarningState{}, sample)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "alias" {
					saved.OwnerID = strings.ToUpper(saved.OwnerID)
				}
				state.state = []entity.PersonalRollingQuotaWarningState{saved}
			}
			db := rollingWarningSQLDatabase(t, state)
			e := db.Transaction(func(tx *gorm.DB) error {
				if e := persistPersonalRollingWarning(tx, sample); e != nil {
					return e
				}
				if kind == "publication_lost" {
					return runtimeUnavailable
				}
				return nil
			})
			switch kind {
			case "first":
				if e != nil || state.writes != 3 || state.commits != 1 {
					t.Fatal("triple did not commit", e, state)
				}
			case "replay":
				if e != nil || state.writes != 1 || state.commits != 1 {
					t.Fatal("duplicate notice written", e, state)
				}
			default:
				if e == nil || state.writes != 0 || state.rollbacks != 1 {
					t.Fatal("partial state/observation/recipient committed", e, state)
				}
			}
		})
	}
}

func TestPersonalRollingWarningScanBoundAndOwnStoredColumns(t *testing.T) {
	db := rollingWarningSQLDatabase(t, &rollingWarningSQLState{})
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []entity.ResourceLimit
		return personalRollingWarningScanQuery(tx, quotaNotificationCursor{ID: "usr_cursor"}).Find(&rows)
	})
	for _, required := range []string{"LIMIT 32", "tokens5_h > 0", "tokens7_d > 0", "EXISTS", "scope_id >", "personal_rolling_quota_warning_states"} {
		if !strings.Contains(sql, required) {
			t.Fatal("bounded stored-cap scan lost", required)
		}
	}
	parsed := &gorm.Statement{DB: db}
	if e := parsed.Parse(&entity.ResourceLimit{}); e != nil {
		t.Fatal(e)
	}
	if parsed.Schema.FieldsByName["Tokens5H"].DBName != "tokens5_h" || parsed.Schema.FieldsByName["Tokens7D"].DBName != "tokens7_d" {
		t.Fatal("real GORM column mapping changed")
	}
}
