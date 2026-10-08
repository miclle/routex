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

type teamRollingWarningSQLState struct {
	state                      []entity.TeamRollingQuotaWarningState
	writes, commits, rollbacks int
	failInbox                  bool
}
type teamRollingWarningSQLConnector struct {
	state *teamRollingWarningSQLState
}

func (c teamRollingWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return teamRollingWarningSQLConnection(c), nil
}
func (c teamRollingWarningSQLConnector) Driver() driver.Driver {
	return teamRollingWarningSQLDriver(c)
}

type teamRollingWarningSQLDriver teamRollingWarningSQLConnector

func (d teamRollingWarningSQLDriver) Open(string) (driver.Conn, error) {
	return teamRollingWarningSQLConnection(d), nil
}

type teamRollingWarningSQLConnection teamRollingWarningSQLConnector

func (teamRollingWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared rolling query")
}
func (teamRollingWarningSQLConnection) Close() error { return nil }
func (c teamRollingWarningSQLConnection) Begin() (driver.Tx, error) {
	return teamRollingWarningSQLTransaction{c.state, c.state.writes}, nil
}
func (c teamRollingWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !strings.Contains(q, `FROM "team_rolling_quota_warning_states"`) {
		return nil, errors.New("unexpected rolling read")
	}
	return effectiveSQLRows(c.state.state)
}
func (c teamRollingWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if strings.Contains(q, `INSERT INTO "team_rolling_quota_warning_inboxes"`) && c.state.failInbox {
		return nil, errors.New("controlled inbox failure")
	}
	if !strings.Contains(q, `"team_rolling_quota_warning_`) {
		return nil, errors.New("unexpected rolling write")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type teamRollingWarningSQLTransaction struct {
	state  *teamRollingWarningSQLState
	before int
}

func (t teamRollingWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t teamRollingWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func teamRollingWarningSQLDatabase(t *testing.T, state *teamRollingWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(teamRollingWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	return db
}
func TestTeamRollingWarningAtomicStateObservationInbox(t *testing.T) {
	row, team, frame := teamRollingFixture()
	sample := teamRollingWarningSamples(row, team, frame)[0]
	for _, kind := range []string{"first", "inbox_failure", "publication_lost", "replay", "alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &teamRollingWarningSQLState{failInbox: kind == "inbox_failure"}
			if kind == "replay" || kind == "alias" {
				saved, _, e := advanceTeamRollingWarning(entity.TeamRollingQuotaWarningState{}, sample)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "alias" {
					saved.TeamID = strings.ToUpper(saved.TeamID)
				}
				state.state = []entity.TeamRollingQuotaWarningState{saved}
			}
			db := teamRollingWarningSQLDatabase(t, state)
			e := db.Transaction(func(tx *gorm.DB) error {
				if e := persistTeamRollingWarning(tx, sample, []teamQuotaWarningRecipient{{ID: "usr_original", CreatedAt: sample.Team.CreatedAt.Add(-time.Hour), MembershipID: "tmm_original"}}); e != nil {
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

func TestTeamRollingWarningScanBoundAndOwnStoredColumns(t *testing.T) {
	db := teamRollingWarningSQLDatabase(t, &teamRollingWarningSQLState{})
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []entity.ResourceLimit
		return teamRollingQuotaWarningScanQuery(tx, quotaNotificationCursor{ID: "usr_cursor"}).Find(&rows)
	})
	for _, required := range []string{"LIMIT 32", "tokens5_h > 0", "tokens7_d > 0", "EXISTS", "scope_id >", "team_rolling_quota_warning_states"} {
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
