package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"database/sql"
	"database/sql/driver"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This database/sql adapter exercises the real GORM transaction and INSERT
// paths without opening a database or manufacturing journal/native evidence.
type projectKeyWarningSQLState struct {
	existing                   []entity.ProjectKeyQuotaWarningObservation
	writes, commits, rollbacks int
	failInbox                  bool
}
type projectKeyWarningSQLConnector struct{ state *projectKeyWarningSQLState }

func (c projectKeyWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return projectKeyWarningSQLConnection(c), nil
}
func (c projectKeyWarningSQLConnector) Driver() driver.Driver { return projectKeyWarningSQLDriver(c) }

type projectKeyWarningSQLDriver projectKeyWarningSQLConnector

func (d projectKeyWarningSQLDriver) Open(string) (driver.Conn, error) {
	return projectKeyWarningSQLConnection(d), nil
}

type projectKeyWarningSQLConnection projectKeyWarningSQLConnector

func (projectKeyWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared warning query")
}
func (projectKeyWarningSQLConnection) Close() error { return nil }
func (c projectKeyWarningSQLConnection) Begin() (driver.Tx, error) {
	return projectKeyWarningSQLTransaction{state: c.state, before: c.state.writes}, nil
}
func (c projectKeyWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "project_key_quota_warning_observations"`) && !strings.Contains(q, `FROM "project_key_quota_warning_inboxes"`) {
		return nil, errors.New("unexpected warning read")
	}
	return effectiveSQLRows(c.state.existing)
}
func (c projectKeyWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.HasPrefix(q, "SAVEPOINT ") || strings.HasPrefix(q, "ROLLBACK TO SAVEPOINT ") {
		return driver.RowsAffected(0), nil
	}
	if strings.Contains(q, `INSERT INTO "project_key_quota_warning_inboxes"`) {
		if c.state.failInbox {
			return nil, errors.New("controlled inbox insert failure")
		}
	} else if !strings.Contains(q, `INSERT INTO "project_key_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning mutation")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type projectKeyWarningSQLTransaction struct {
	state  *projectKeyWarningSQLState
	before int
}

func (t projectKeyWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t projectKeyWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func projectKeyWarningSQLDatabase(t *testing.T, state *projectKeyWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(projectKeyWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestProjectKeyQuotaWarningAtomicReplayNoRecipientExpansion(t *testing.T) {
	row, root, owner, frame := projectKeyWarningFixture()
	original := projectKeyMonthlyWarnings(row, root, owner, frame, "USD")[0]
	original.ID = "jwo_original"
	for _, kind := range []string{"first", "inbox_failure", "replay", "identity_alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &projectKeyWarningSQLState{failInbox: kind == "inbox_failure"}
			proposed := original
			proposed.ID = ""
			if kind == "replay" || kind == "identity_alias" {
				state.existing = []entity.ProjectKeyQuotaWarningObservation{original}
				proposed.Settled = "85"
				proposed.AsOf = proposed.AsOf.AddDate(0, 0, 1)
			}
			if kind == "identity_alias" {
				state.existing[0].ProjectID = strings.ToUpper(original.ProjectID)
			}
			db := projectKeyWarningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error {
				return persistProjectKeyQuotaWarning(tx, proposed, []projectQuotaWarningRecipient{{ID: "usr_original", CreatedAt: owner.CreatedAt}})
			})
			switch kind {
			case "first":
				if err != nil || state.writes != 2 || state.commits != 1 {
					t.Fatal("observation/recipient pair did not commit", err, state)
				}
			case "inbox_failure":
				if err == nil || state.writes != 0 || state.rollbacks != 1 {
					t.Fatal("partial immutable snapshot committed", err, state)
				}
			case "replay":
				if err != nil || state.writes != 0 || state.existing[0].Settled != original.Settled || !state.existing[0].AsOf.Equal(original.AsOf) {
					t.Fatal("replay rewrote original snapshot/recipient", err, state)
				}
			case "identity_alias":
				if !errors.Is(err, errQuotaNotificationIdentity) || state.writes != 0 {
					t.Fatal("collated alias borrowed snapshot", err, state)
				}
			}
		})
	}
}

type projectKeyChainSQLState struct {
	keys            []entity.ProjectKey
	queries, writes int
}
type projectKeyChainSQLConnector struct{ state *projectKeyChainSQLState }

func (c projectKeyChainSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return projectKeyChainSQLConnection(c), nil
}
func (c projectKeyChainSQLConnector) Driver() driver.Driver { return projectKeyChainSQLDriver(c) }

type projectKeyChainSQLDriver projectKeyChainSQLConnector

func (d projectKeyChainSQLDriver) Open(string) (driver.Conn, error) {
	return projectKeyChainSQLConnection(d), nil
}

type projectKeyChainSQLConnection projectKeyChainSQLConnector

func (projectKeyChainSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (projectKeyChainSQLConnection) Close() error { return nil }
func (projectKeyChainSQLConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c projectKeyChainSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "project_api_keys"`) || strings.Contains(q, "token_hash") || strings.Contains(q, "prefix") || strings.Contains(q, "creator_id") {
		return nil, errors.New("unexpected private chain query")
	}
	c.state.queries++
	return effectiveSQLRows(c.state.keys)
}
func (c projectKeyChainSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("unexpected chain mutation")
}
func projectKeyChainSQLDatabase(t *testing.T, state *projectKeyChainSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(projectKeyChainSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestProjectKeyQuotaWarningCompleteRecipientsAndNoReplayExpansion(t *testing.T) {
	row, root, project, frame := projectKeyWarningFixture()
	v := projectKeyMonthlyWarnings(row, root, project, frame, "USD")[0]
	for _, name := range []string{"empty", "maximum", "overflow", "duplicate", "missing_birth", "future_birth", "late_replay"} {
		t.Run(name, func(t *testing.T) {
			n := 1000
			if name == "empty" {
				n = 0
			}
			if name == "overflow" {
				n = 1001
			}
			recipients := make([]projectQuotaWarningRecipient, n)
			for i := range recipients {
				recipients[i] = projectQuotaWarningRecipient{ID: fmt.Sprintf("usr_%026d", i), CreatedAt: project.CreatedAt}
			}
			switch name {
			case "duplicate":
				recipients[1] = recipients[0]
			case "missing_birth":
				recipients[0].CreatedAt = time.Time{}
			case "future_birth":
				recipients[0].CreatedAt = v.AsOf.Add(time.Millisecond)
			}
			state := &projectKeyWarningSQLState{}
			if name == "late_replay" {
				old := v
				old.ID = "jwo_original"
				state.existing = []entity.ProjectKeyQuotaWarningObservation{old}
				recipients = []projectQuotaWarningRecipient{{ID: "usr_late", CreatedAt: project.CreatedAt}}
			}
			db := projectKeyWarningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error { return persistProjectKeyQuotaWarning(tx, v, recipients) })
			if name == "maximum" {
				if err != nil || state.writes != 5 {
					t.Fatal("complete thousand managers not batched atomically", err, state.writes)
				}
			} else if name == "empty" || name == "late_replay" {
				if err != nil || state.writes != 0 {
					t.Fatal("empty/replay changed original recipients", err, state.writes)
				}
			} else if !errors.Is(err, errQuotaNotificationIdentity) || state.writes != 0 {
				t.Fatal("invalid recipient set truncated or persisted", err, state.writes)
			}
		})
	}
}
