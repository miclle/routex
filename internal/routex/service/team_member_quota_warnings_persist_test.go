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
type memberWarningSQLState struct {
	existing                   []entity.TeamMemberQuotaWarningObservation
	writes, commits, rollbacks int
	failInbox                  bool
}
type memberWarningSQLConnector struct{ state *memberWarningSQLState }

func (c memberWarningSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return memberWarningSQLConnection(c), nil
}
func (c memberWarningSQLConnector) Driver() driver.Driver { return memberWarningSQLDriver(c) }

type memberWarningSQLDriver memberWarningSQLConnector

func (d memberWarningSQLDriver) Open(string) (driver.Conn, error) {
	return memberWarningSQLConnection(d), nil
}

type memberWarningSQLConnection memberWarningSQLConnector

func (memberWarningSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared warning query")
}
func (memberWarningSQLConnection) Close() error { return nil }
func (c memberWarningSQLConnection) Begin() (driver.Tx, error) {
	return memberWarningSQLTransaction{state: c.state, before: c.state.writes}, nil
}
func (c memberWarningSQLConnection) QueryContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.Contains(q, `FROM "team_member_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning read")
	}
	return effectiveSQLRows(c.state.existing)
}
func (c memberWarningSQLConnection) ExecContext(ctx context.Context, q string, _ []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.Contains(q, `INSERT INTO "team_member_quota_warning_inboxes"`) {
		if c.state.failInbox {
			return nil, errors.New("controlled inbox insert failure")
		}
	} else if !strings.Contains(q, `INSERT INTO "team_member_quota_warning_observations"`) {
		return nil, errors.New("unexpected warning mutation")
	}
	c.state.writes++
	return driver.RowsAffected(1), nil
}

type memberWarningSQLTransaction struct {
	state  *memberWarningSQLState
	before int
}

func (t memberWarningSQLTransaction) Commit() error { t.state.commits++; return nil }
func (t memberWarningSQLTransaction) Rollback() error {
	t.state.rollbacks++
	t.state.writes = t.before
	return nil
}
func memberWarningSQLDatabase(t *testing.T, state *memberWarningSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(memberWarningSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestTeamMemberQuotaWarningObservationAndInboxAtomicReplay(t *testing.T) {
	c, u := memberWarningFixture(t)
	original := monthlyTeamMemberQuotaWarnings(c, u)[0]
	original.ID = "mwo_original"
	for _, kind := range []string{"first", "inbox_failure", "replay", "renamed_replay", "identity_alias", "forged_self", "recipient_birth_missing", "old_user_birth"} {
		t.Run(kind, func(t *testing.T) {
			state := &memberWarningSQLState{failInbox: kind == "inbox_failure"}
			proposed := original
			proposed.ID = ""
			if kind == "replay" || kind == "renamed_replay" || kind == "identity_alias" || kind == "old_user_birth" {
				state.existing = []entity.TeamMemberQuotaWarningObservation{original}
				proposed.Settled = "85"
				proposed.AsOf = proposed.AsOf.AddDate(0, 0, 1)
			}
			switch kind {
			case "renamed_replay":
				proposed.TeamName = "New name"
			case "identity_alias":
				state.existing[0].MemberUserID = strings.ToUpper(original.MemberUserID)
			case "forged_self":
				proposed.MemberUserID = "usr_owner"
			case "recipient_birth_missing":
				proposed.UserCreatedAt = time.Time{}
			case "old_user_birth":
				proposed.UserCreatedAt = proposed.UserCreatedAt.Add(time.Millisecond)
			}
			db := memberWarningSQLDatabase(t, state)
			err := db.Transaction(func(tx *gorm.DB) error { return persistTeamMemberQuotaWarning(tx, proposed) })
			switch kind {
			case "first":
				if err != nil || state.writes != 2 || state.commits != 1 {
					t.Fatal("sole self observation/inbox pair did not commit", err, state)
				}
			case "inbox_failure":
				if err == nil || state.writes != 0 || state.rollbacks != 1 {
					t.Fatal("partial snapshot committed", err, state)
				}
			case "replay", "renamed_replay":
				if err != nil || state.writes != 0 || state.existing[0].Settled != original.Settled || state.existing[0].TeamName != original.TeamName || !state.existing[0].AsOf.Equal(original.AsOf) {
					t.Fatal("replay rewrote original self snapshot", err, state)
				}
			default:
				if !errors.Is(err, errQuotaNotificationIdentity) || state.writes != 0 {
					t.Fatal("substituted self/birth borrowed old snapshot", err, state)
				}
			}
		})
	}
}
