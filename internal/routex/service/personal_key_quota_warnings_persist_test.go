package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"database/sql"
	"database/sql/driver"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This database/sql adapter exercises the real GORM transaction and INSERT
// paths without opening a database or manufacturing journal/native evidence.
type personalKeyWarningSQLState struct {
	existing                   []entity.PersonalKeyQuotaWarningObservation
	writes, commits, rollbacks int
	failInbox                  bool
}
type personalKeyWarningSQLConnector struct{ state *personalKeyWarningSQLState }

func (c personalKeyWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return personalKeyWarningSQLConnection(c), nil
}
func (c personalKeyWarningSQLConnector) Driver() driver.Driver { return personalKeyWarningSQLDriver(c) }

type personalKeyWarningSQLDriver personalKeyWarningSQLConnector

func (d personalKeyWarningSQLDriver) Open(string) (driver.Conn, error) {
	return personalKeyWarningSQLConnection(d), nil
}

type personalKeyWarningSQLConnection personalKeyWarningSQLConnector

func (personalKeyWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared warning query")
}
func (personalKeyWarningSQLConnection) Close() error { return nil }
func (c personalKeyWarningSQLConnection) Begin() (driver.Tx, error) {
	return personalKeyWarningSQLTransaction{state: c.state, before: c.state.writes}, nil
}
func (c personalKeyWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "personal_key_quota_warning_observations"`) && !strings.Contains(q, `FROM "personal_key_quota_warning_inboxes"`) {
		return nil, errors.New("unexpected warning read")
	}
	return effectiveSQLRows(c.state.existing)
}
func (c personalKeyWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `INSERT INTO "personal_key_quota_warning_inboxes"`) {
		if c.state.failInbox {
			return nil, errors.New("controlled inbox insert failure")
		}
	} else if !strings.Contains(q, `INSERT INTO "personal_key_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning mutation")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type personalKeyWarningSQLTransaction struct {
	state  *personalKeyWarningSQLState
	before int
}

func (t personalKeyWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t personalKeyWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func personalKeyWarningSQLDatabase(t *testing.T, state *personalKeyWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(personalKeyWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestPersonalKeyQuotaWarningObservationAndInboxAtomicReplay(t *testing.T) {
	row, root, owner, frame := personalKeyWarningFixture()
	original := personalKeyMonthlyWarnings(row, root, owner, frame, "USD")[0]
	original.ID = "kwo_original"
	for _, kind := range []string{"first", "inbox_failure", "replay", "identity_alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &personalKeyWarningSQLState{failInbox: kind == "inbox_failure"}
			proposed := original
			proposed.ID = ""
			if kind == "replay" || kind == "identity_alias" {
				state.existing = []entity.PersonalKeyQuotaWarningObservation{original}
				proposed.Settled = "85"
				proposed.AsOf = proposed.AsOf.AddDate(0, 0, 1)
			}
			if kind == "identity_alias" {
				state.existing[0].OwnerID = strings.ToUpper(original.OwnerID)
			}
			db := personalKeyWarningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error { return persistPersonalKeyQuotaWarning(tx, proposed) })
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

type personalKeyChainSQLState struct {
	keys            []entity.APIKey
	queries, writes int
}
type personalKeyChainSQLConnector struct{ state *personalKeyChainSQLState }

func (c personalKeyChainSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return personalKeyChainSQLConnection(c), nil
}
func (c personalKeyChainSQLConnector) Driver() driver.Driver { return personalKeyChainSQLDriver(c) }

type personalKeyChainSQLDriver personalKeyChainSQLConnector

func (d personalKeyChainSQLDriver) Open(string) (driver.Conn, error) {
	return personalKeyChainSQLConnection(d), nil
}

type personalKeyChainSQLConnection personalKeyChainSQLConnector

func (personalKeyChainSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (personalKeyChainSQLConnection) Close() error { return nil }
func (personalKeyChainSQLConnection) Begin() (driver.Tx, error) {
	return nil, errors.New("unexpected transaction")
}
func (c personalKeyChainSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "api_keys"`) || strings.Contains(q, "token_hash") || strings.Contains(q, "prefix") {
		return nil, errors.New("unexpected private chain query")
	}
	c.state.queries++
	return effectiveSQLRows(c.state.keys)
}
func (c personalKeyChainSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("unexpected chain mutation")
}
func personalKeyChainSQLDatabase(t *testing.T, state *personalKeyChainSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(personalKeyChainSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
