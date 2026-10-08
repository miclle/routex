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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type projectRollingWarningSQLState struct {
	state                      []entity.ProjectRollingQuotaWarningState
	writes, commits, rollbacks int
	failInbox                  bool
}
type projectRollingWarningSQLConnector struct {
	state *projectRollingWarningSQLState
}

func (c projectRollingWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return projectRollingWarningSQLConnection(c), nil
}
func (c projectRollingWarningSQLConnector) Driver() driver.Driver {
	return projectRollingWarningSQLDriver(c)
}

type projectRollingWarningSQLDriver projectRollingWarningSQLConnector

func (d projectRollingWarningSQLDriver) Open(string) (driver.Conn, error) {
	return projectRollingWarningSQLConnection(d), nil
}

type projectRollingWarningSQLConnection projectRollingWarningSQLConnector

func (projectRollingWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared rolling query")
}
func (projectRollingWarningSQLConnection) Close() error { return nil }
func (c projectRollingWarningSQLConnection) Begin() (driver.Tx, error) {
	return projectRollingWarningSQLTransaction{c.state, c.state.writes}, nil
}
func (c projectRollingWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !strings.Contains(q, `FROM "project_rolling_quota_warning_states"`) {
		return nil, errors.New("unexpected rolling read")
	}
	return effectiveSQLRows(c.state.state)
}
func (c projectRollingWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if strings.Contains(q, `INSERT INTO "project_rolling_quota_warning_inboxes"`) && c.state.failInbox {
		return nil, errors.New("controlled inbox failure")
	}
	if !strings.Contains(q, `"project_rolling_quota_warning_`) {
		return nil, errors.New("unexpected rolling write")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type projectRollingWarningSQLTransaction struct {
	state  *projectRollingWarningSQLState
	before int
}

func (t projectRollingWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t projectRollingWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func projectRollingWarningSQLDatabase(t *testing.T, state *projectRollingWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(projectRollingWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	return db
}
func TestProjectRollingWarningAtomicStateObservationInbox(t *testing.T) {
	row, project, frame := projectRollingFixture()
	sample := projectRollingWarningSamples(row, project, frame)[0]
	for _, kind := range []string{"first", "inbox_failure", "publication_lost", "replay", "alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &projectRollingWarningSQLState{failInbox: kind == "inbox_failure"}
			if kind == "replay" || kind == "alias" {
				saved, _, e := advanceProjectRollingWarning(entity.ProjectRollingQuotaWarningState{}, sample)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "alias" {
					saved.ProjectID = strings.ToUpper(saved.ProjectID)
				}
				state.state = []entity.ProjectRollingQuotaWarningState{saved}
			}
			db := projectRollingWarningSQLDatabase(t, state)
			e := db.Transaction(func(tx *gorm.DB) error {
				if e := persistProjectRollingWarning(tx, sample, []projectQuotaWarningRecipient{{ID: "usr_original", CreatedAt: sample.Project.CreatedAt.Add(-time.Hour), ManagerID: "pmg_original"}}); e != nil {
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

func TestProjectRollingWarningScanBoundAndOwnStoredColumns(t *testing.T) {
	db := projectRollingWarningSQLDatabase(t, &projectRollingWarningSQLState{})
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []entity.ResourceLimit
		return projectRollingQuotaWarningScanQuery(tx, quotaNotificationCursor{ID: "usr_cursor"}).Find(&rows)
	})
	for _, required := range []string{"LIMIT 32", "tokens5_h > 0", "tokens7_d > 0", "EXISTS", "scope_id >", "project_rolling_quota_warning_states"} {
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
