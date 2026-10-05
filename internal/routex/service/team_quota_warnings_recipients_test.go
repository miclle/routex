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

type teamWarningRecipientSQLState struct {
	members      []quotaTeamIdentity
	users        []entity.User
	applications []entity.RegistrationApprovalApplication
	queries      []string
	userIDs      []string
	fail         string
	writes       int
}
type teamWarningRecipientSQLConnector struct{ state *teamWarningRecipientSQLState }

func (c teamWarningRecipientSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return teamWarningRecipientSQLConnection(c), nil
}
func (c teamWarningRecipientSQLConnector) Driver() driver.Driver {
	return teamWarningRecipientSQLDriver(c)
}

type teamWarningRecipientSQLDriver teamWarningRecipientSQLConnector

func (d teamWarningRecipientSQLDriver) Open(string) (driver.Conn, error) {
	return teamWarningRecipientSQLConnection(d), nil
}

type teamWarningRecipientSQLConnection teamWarningRecipientSQLConnector

func (teamWarningRecipientSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected recipient prepare")
}
func (teamWarningRecipientSQLConnection) Close() error { return nil }
func (teamWarningRecipientSQLConnection) Begin() (driver.Tx, error) {
	return adminOverviewTransaction{}, nil
}
func (c teamWarningRecipientSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("recipient resolution wrote state")
}
func (c teamWarningRecipientSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries = append(c.state.queries, q)
	if c.state.fail != "" && strings.Contains(q, c.state.fail) {
		return nil, errors.New("controlled scoped recipient read failure")
	}
	switch {
	case strings.Contains(q, "FROM team_memberships AS member"):
		return effectiveSQLRows(c.state.members)
	case strings.Contains(q, `FROM "users"`):
		if !strings.Contains(q, " IN ") || !strings.Contains(q, "LIMIT") {
			return nil, errors.New("unbounded recipient directory query")
		}
		for _, arg := range args {
			if s, ok := arg.Value.(string); ok {
				c.state.userIDs = append(c.state.userIDs, s)
			}
		}
		return effectiveSQLRows(c.state.users)
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		return effectiveSQLRows(c.state.applications)
	default:
		return nil, errors.New("unexpected scoped recipient read")
	}
}
func teamWarningRecipientSQLDatabase(t *testing.T, state *teamWarningRecipientSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(teamWarningRecipientSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestTeamQuotaWarningBoundedCompleteRecipientResolution(t *testing.T) {
	for _, name := range []string{"approved", "empty", "pending", "rejected", "unknown_application", "user_alias", "membership_alias", "wrong_team", "zero_birth", "missing_user", "overflow", "duplicate", "failure"} {
		t.Run(name, func(t *testing.T) {
			s, auth, _, team, _, _ := teamWarningRuntimeFixture(t)
			u, apps := managedAdvisorySubject(entity.User{ID: "usr_exact", CreatedAt: team.CreatedAt})
			publishManagedAdvisory(auth, u, apps)
			app := apps[*u.ApprovalApplicationID]
			state := &teamWarningRecipientSQLState{members: []quotaTeamIdentity{{MembershipID: "tmb_exact", MembershipUserID: u.ID, MembershipTeamID: team.ID, UserID: u.ID, TeamID: team.ID, TeamStatus: entity.ResourceActive, MembershipStatus: entity.ResourceActive, Role: entity.TeamMember}}, users: []entity.User{u}, applications: []entity.RegistrationApprovalApplication{app}}
			wantCount, wantError := 1, false
			switch name {
			case "empty":
				state.members = nil
				wantCount = 0
			case "pending":
				state.applications[0].State = "pending"
				state.applications[0].DecidedAt = nil
				state.applications[0].DecisionActorID = nil
				state.applications[0].DecisionReason = nil
				wantCount = 0
			case "rejected":
				state.applications[0].State = "rejected"
				wantCount = 0
			case "unknown_application":
				state.applications = nil
				wantCount = 0
			case "user_alias":
				state.users[0].ID = strings.ToUpper(u.ID)
				wantError = true
			case "membership_alias":
				state.members[0].MembershipID = strings.ToUpper("tmb_exact")
				wantCount = 0
			case "wrong_team":
				state.members[0].TeamID = "tem_other"
				wantError = true
			case "zero_birth":
				state.users[0].CreatedAt = entity.User{}.CreatedAt
				wantError = true
			case "missing_user":
				state.users = nil
				wantError = true
			case "overflow":
				for len(state.members) <= quotaInboxManagerLimit {
					state.members = append(state.members, state.members[0])
				}
				wantError = true
			case "duplicate":
				state.members = append(state.members, state.members[0])
				wantError = true
			case "failure":
				state.fail = "registration_approval_applications"
				wantError = true
			}
			recipients, err := s.teamQuotaWarningRecipients(teamWarningRecipientSQLDatabase(t, state), auth, team.ID)
			if (err != nil) != wantError || !wantError && len(recipients) != wantCount {
				t.Fatal("scoped recipient resolution", name, recipients, err)
			}
			if state.writes != 0 {
				t.Fatal("recipient read mutated identity")
			}
			for _, id := range state.userIDs {
				if id != u.ID {
					t.Fatal("queried unrelated recipient", id)
				}
			}
			if name == "approved" && (recipients[0].ID != u.ID || !recipients[0].CreatedAt.Equal(u.CreatedAt) || recipients[0].MembershipID != "tmb_exact") {
				t.Fatal("complete private recipient proof lost")
			}
			if (name == "empty" || name == "overflow" || name == "duplicate") && len(state.queries) != 1 {
				t.Fatal("invalid or empty scope queried directory")
			}
		})
	}
}
