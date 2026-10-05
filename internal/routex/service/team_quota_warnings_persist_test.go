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

// This database/sql adapter exercises the real GORM transaction and INSERT
// paths without opening a database or manufacturing journal/native evidence.
type teamWarningSQLState struct {
	existing                   []entity.TeamQuotaWarningObservation
	writes, commits, rollbacks int
	failInbox                  bool
}
type teamWarningSQLConnector struct{ state *teamWarningSQLState }

func (c teamWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return teamWarningSQLConnection(c), nil
}
func (c teamWarningSQLConnector) Driver() driver.Driver { return teamWarningSQLDriver(c) }

type teamWarningSQLDriver teamWarningSQLConnector

func (d teamWarningSQLDriver) Open(string) (driver.Conn, error) {
	return teamWarningSQLConnection(d), nil
}

type teamWarningSQLConnection teamWarningSQLConnector

func (teamWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared warning query")
}
func (teamWarningSQLConnection) Close() error { return nil }
func (c teamWarningSQLConnection) Begin() (driver.Tx, error) {
	return teamWarningSQLTransaction{state: c.state, before: c.state.writes}, nil
}
func (c teamWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "team_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning read")
	}
	return effectiveSQLRows(c.state.existing)
}
func (c teamWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `INSERT INTO "team_quota_warning_inboxes"`) {
		if c.state.failInbox {
			return nil, errors.New("controlled inbox insert failure")
		}
	} else if !strings.Contains(q, `INSERT INTO "team_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning mutation")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type teamWarningSQLTransaction struct {
	state  *teamWarningSQLState
	before int
}

func (t teamWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t teamWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func teamWarningSQLDatabase(t *testing.T, state *teamWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(teamWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestTeamQuotaWarningObservationAndInboxAtomicReplay(t *testing.T) {
	row, created, usage := teamWarningFixture()
	original := monthlyTeamQuotaWarnings(row, created, usage, "USD")[0]
	original.ID = "two_original"
	recipients := []teamQuotaWarningRecipient{{ID: "usr_one", CreatedAt: created}, {ID: "usr_two", CreatedAt: created}}
	for _, kind := range []string{"first", "inbox_failure", "replay", "identity_alias", "new_recipient_replay", "recipient_birth_missing", "duplicate_recipient"} {
		t.Run(kind, func(t *testing.T) {
			state := &teamWarningSQLState{failInbox: kind == "inbox_failure"}
			proposed := original
			proposed.ID = ""
			proposedRecipients := append([]teamQuotaWarningRecipient(nil), recipients...)
			if kind == "recipient_birth_missing" {
				proposedRecipients[0].CreatedAt = time.Time{}
			}
			if kind == "duplicate_recipient" {
				proposedRecipients[1] = proposedRecipients[0]
			}
			if kind == "replay" || kind == "identity_alias" || kind == "new_recipient_replay" {
				state.existing = []entity.TeamQuotaWarningObservation{original}
				proposed.Settled = "85"
				proposed.AsOf = proposed.AsOf.AddDate(0, 0, 1)
			}
			if kind == "identity_alias" {
				state.existing[0].TeamID = strings.ToUpper(original.TeamID)
			}
			if kind == "new_recipient_replay" {
				proposedRecipients = append(proposedRecipients, teamQuotaWarningRecipient{ID: "usr_later", CreatedAt: created})
			}
			db := teamWarningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error { return persistTeamQuotaWarning(tx, proposed, proposedRecipients) })
			switch kind {
			case "first":
				if err != nil || state.writes != 3 || state.commits != 1 {
					t.Fatal("observation/recipient pair did not commit", err, state)
				}
			case "inbox_failure":
				if err == nil || state.writes != 0 || state.rollbacks != 1 {
					t.Fatal("partial immutable snapshot committed", err, state)
				}
			case "replay", "new_recipient_replay":
				if err != nil || state.writes != 0 || state.existing[0].Settled != original.Settled || !state.existing[0].AsOf.Equal(original.AsOf) {
					t.Fatal("replay rewrote original snapshot/recipient", err, state)
				}
			case "identity_alias", "recipient_birth_missing", "duplicate_recipient":
				if !errors.Is(err, errQuotaNotificationIdentity) || state.writes != 0 {
					t.Fatal("collated alias borrowed snapshot", err, state)
				}
			}
		})
	}
}
