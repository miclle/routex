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

type keyRollingWarningSQLState struct {
	state                      []entity.PersonalKeyRollingQuotaWarningState
	writes, commits, rollbacks int
	failInbox                  bool
}
type keyRollingWarningSQLConnector struct{ state *keyRollingWarningSQLState }

func (c keyRollingWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return keyRollingWarningSQLConnection(c), nil
}
func (c keyRollingWarningSQLConnector) Driver() driver.Driver { return keyRollingWarningSQLDriver(c) }

type keyRollingWarningSQLDriver keyRollingWarningSQLConnector

func (d keyRollingWarningSQLDriver) Open(string) (driver.Conn, error) {
	return keyRollingWarningSQLConnection(d), nil
}

type keyRollingWarningSQLConnection keyRollingWarningSQLConnector

func (keyRollingWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared rolling query")
}
func (keyRollingWarningSQLConnection) Close() error { return nil }
func (c keyRollingWarningSQLConnection) Begin() (driver.Tx, error) {
	return keyRollingWarningSQLTransaction{c.state, c.state.writes}, nil
}
func (c keyRollingWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if !strings.Contains(q, `FROM "personal_key_rolling_quota_warning_states"`) {
		return nil, errors.New("unexpected rolling read")
	}
	return effectiveSQLRows(c.state.state)
}
func (c keyRollingWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if strings.Contains(q, `INSERT INTO "personal_key_rolling_quota_warning_inboxes"`) && c.state.failInbox {
		return nil, errors.New("controlled inbox failure")
	}
	if !strings.Contains(q, `"personal_key_rolling_quota_warning_`) {
		return nil, errors.New("unexpected rolling write")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type keyRollingWarningSQLTransaction struct {
	state  *keyRollingWarningSQLState
	before int
}

func (t keyRollingWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t keyRollingWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func keyRollingWarningSQLDatabase(t *testing.T, state *keyRollingWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(keyRollingWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, e := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if e != nil {
		t.Fatal(e)
	}
	return db
}
func TestPersonalKeyRollingWarningAtomicStateObservationInbox(t *testing.T) {
	row, root, owner, frame := keyRollingFixture()
	sample := personalKeyRollingWarningSamples(row, root, owner, frame)[0]
	for _, kind := range []string{"first", "inbox_failure", "publication_lost", "replay", "alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &keyRollingWarningSQLState{failInbox: kind == "inbox_failure"}
			if kind == "replay" || kind == "alias" {
				saved, _, e := advancePersonalKeyRollingWarning(entity.PersonalKeyRollingQuotaWarningState{}, sample)
				if e != nil {
					t.Fatal(e)
				}
				if kind == "alias" {
					saved.OwnerID = strings.ToUpper(saved.OwnerID)
				}
				state.state = []entity.PersonalKeyRollingQuotaWarningState{saved}
			}
			db := keyRollingWarningSQLDatabase(t, state)
			e := db.Transaction(func(tx *gorm.DB) error {
				if e := persistPersonalKeyRollingWarning(tx, sample); e != nil {
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

func TestPersonalKeyRollingWarningScanBoundAndOwnStoredColumns(t *testing.T) {
	db := keyRollingWarningSQLDatabase(t, &keyRollingWarningSQLState{})
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var rows []entity.ResourceLimit
		return personalKeyRollingQuotaWarningScanQuery(tx, quotaNotificationCursor{ID: "usr_cursor"}).Find(&rows)
	})
	for _, required := range []string{"LIMIT 32", "tokens5_h > 0", "tokens7_d > 0", "EXISTS", "scope_id >", "personal_key_rolling_quota_warning_states"} {
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
