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
type warningSQLState struct {
	existing                   []entity.QuotaWarningObservation
	writes, commits, rollbacks int
	failInbox                  bool
}
type warningSQLConnector struct{ state *warningSQLState }

func (c warningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return warningSQLConnection(c), nil
}
func (c warningSQLConnector) Driver() driver.Driver { return warningSQLDriver(c) }

type warningSQLDriver warningSQLConnector

func (d warningSQLDriver) Open(string) (driver.Conn, error) { return warningSQLConnection(d), nil }

type warningSQLConnection warningSQLConnector

func (warningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared warning query")
}
func (warningSQLConnection) Close() error { return nil }
func (c warningSQLConnection) Begin() (driver.Tx, error) {
	return warningSQLTransaction{state: c.state, before: c.state.writes}, nil
}
func (c warningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "quota_warning_observations"`) {
		return nil, errors.New("unexpected warning read")
	}
	return effectiveSQLRows(c.state.existing)
}
func (c warningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `INSERT INTO "quota_warning_inboxes"`) {
		if c.state.failInbox {
			return nil, errors.New("controlled inbox insert failure")
		}
	} else if !strings.Contains(q, `INSERT INTO "quota_warning_observations"`) {
		return nil, errors.New("unexpected warning mutation")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type warningSQLTransaction struct {
	state  *warningSQLState
	before int
}

func (t warningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t warningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func warningSQLDatabase(t *testing.T, state *warningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(warningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestQuotaWarningObservationAndInboxAtomicReplay(t *testing.T) {
	row, created, usage := warningFixture()
	original := monthlyQuotaWarnings(row, created, usage, "USD")[0]
	original.ID = "qwo_original"
	for _, kind := range []string{"first", "inbox_failure", "replay", "identity_alias"} {
		t.Run(kind, func(t *testing.T) {
			state := &warningSQLState{failInbox: kind == "inbox_failure"}
			proposed := original
			proposed.ID = ""
			if kind == "replay" || kind == "identity_alias" {
				state.existing = []entity.QuotaWarningObservation{original}
				proposed.Settled = "85"
				proposed.AsOf = proposed.AsOf.AddDate(0, 0, 1)
			}
			if kind == "identity_alias" {
				state.existing[0].OwnerID = strings.ToUpper(original.OwnerID)
			}
			db := warningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error { return persistQuotaWarning(tx, proposed) })
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
