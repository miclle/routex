package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type projectWarningRecipientSQLState struct {
	members      []projectQuotaWarningManagerIdentity
	users        []entity.User
	applications []entity.RegistrationApprovalApplication
	queries      []string
	userIDs      []string
	fail         string
	writes       int
}
type projectWarningRecipientSQLConnector struct {
	state *projectWarningRecipientSQLState
}

func (c projectWarningRecipientSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return projectWarningRecipientSQLConnection(c), nil
}
func (c projectWarningRecipientSQLConnector) Driver() driver.Driver {
	return projectWarningRecipientSQLDriver(c)
}

type projectWarningRecipientSQLDriver projectWarningRecipientSQLConnector

func (d projectWarningRecipientSQLDriver) Open(string) (driver.Conn, error) {
	return projectWarningRecipientSQLConnection(d), nil
}

type projectWarningRecipientSQLConnection projectWarningRecipientSQLConnector

func (projectWarningRecipientSQLConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected recipient prepare")
}
func (projectWarningRecipientSQLConnection) Close() error { return nil }
func (projectWarningRecipientSQLConnection) Begin() (driver.Tx, error) {
	return adminOverviewTransaction{}, nil
}
func (c projectWarningRecipientSQLConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.state.writes++
	return nil, errors.New("recipient resolution wrote state")
}
func (c projectWarningRecipientSQLConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.state.queries = append(c.state.queries, q)
	if c.state.fail != "" && strings.Contains(q, c.state.fail) {
		return nil, errors.New("controlled scoped recipient read failure")
	}
	switch {
	case strings.Contains(q, "FROM project_managers AS manager"):
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
		ids := []string{}
		for _, arg := range args {
			if id, ok := arg.Value.(string); ok {
				ids = append(ids, id)
			}
		}
		rows := []entity.RegistrationApprovalApplication{}
		for _, app := range c.state.applications {
			if slices.Contains(ids, app.ID) {
				rows = append(rows, app)
			}
		}
		return effectiveSQLRows(rows)
	default:
		return nil, errors.New("unexpected scoped recipient read")
	}
}
func projectWarningRecipientSQLDatabase(t *testing.T, state *projectWarningRecipientSQLState) *gorm.DB {
	t.Helper()
	pool := sql.OpenDB(projectWarningRecipientSQLConnector{state})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db
}
func TestProjectQuotaWarningBoundedCompleteRecipientResolution(t *testing.T) {
	for _, name := range []string{"approved", "empty", "pending", "rejected", "unknown_application", "user_alias", "management_alias", "wrong_project", "unsafe_manager", "zero_birth", "missing_user", "overflow", "duplicate", "failure"} {
		t.Run(name, func(t *testing.T) {
			s, auth, _, project, _, _ := projectWarningRuntimeFixture(t)
			u, apps := managedAdvisorySubject(entity.User{ID: "usr_exact", CreatedAt: project.CreatedAt})
			publishManagedAdvisory(auth, u, apps)
			app := apps[*u.ApprovalApplicationID]
			state := &projectWarningRecipientSQLState{members: []projectQuotaWarningManagerIdentity{{ManagerID: "pmg_exact", ManagerUserID: u.ID, ManagerProjectID: project.ID, UserID: u.ID, ProjectID: project.ID, ProjectStatus: entity.ResourceActive}}, users: []entity.User{u}, applications: []entity.RegistrationApprovalApplication{app}}
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
			case "management_alias":
				state.members[0].ManagerID = strings.ToUpper("pmg_exact")
				wantCount = 0
			case "wrong_project":
				state.members[0].ProjectID = "prj_other"
				wantError = true
			case "unsafe_manager":
				state.members[0].ManagerID = "manager unsafe"
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
			recipients, err := s.projectQuotaWarningRecipients(projectWarningRecipientSQLDatabase(t, state), auth, project.ID)
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
			if name == "approved" && (recipients[0].ID != u.ID || !recipients[0].CreatedAt.Equal(u.CreatedAt) || recipients[0].ManagerID != "pmg_exact") {
				t.Fatal("complete private recipient proof lost")
			}
			if (name == "empty" || name == "overflow" || name == "duplicate") && len(state.queries) != 1 {
				t.Fatal("invalid or empty scope queried directory")
			}
		})
	}
}

func TestProjectQuotaWarningMaximumRecipientsStayCompleteAndBounded(t *testing.T) {
	s, auth, _, project, _, _ := projectWarningRuntimeFixture(t)
	state := &projectWarningRecipientSQLState{}
	published := auth.ProjectCreationStates[project.ID]
	published.Managers = map[string]string{}
	published.EnabledManagers = map[string]bool{}
	for i := range quotaInboxManagerLimit {
		userID, managerID := fmt.Sprintf("usr_%020d", i), fmt.Sprintf("pmg_%020d", i)
		u, apps := managedAdvisorySubject(entity.User{ID: userID, CreatedAt: project.CreatedAt})
		app := apps[*u.ApprovalApplicationID]
		app.ID = fmt.Sprintf("raa_%026d", i)
		u.ApprovalApplicationID = &app.ID
		publishManagedAdvisory(auth, u, map[string]entity.RegistrationApprovalApplication{app.ID: app})
		published.Managers[userID], published.EnabledManagers[userID] = managerID, true
		state.members = append(state.members, projectQuotaWarningManagerIdentity{ManagerID: managerID, ManagerUserID: userID, ManagerProjectID: project.ID, UserID: userID, ProjectID: project.ID, ProjectStatus: entity.ResourceActive})
		state.users = append(state.users, u)
		state.applications = append(state.applications, app)
	}
	auth.ProjectCreationStates[project.ID] = published
	recipients, err := s.projectQuotaWarningRecipients(projectWarningRecipientSQLDatabase(t, state), auth, project.ID)
	if err != nil || len(recipients) != quotaInboxManagerLimit || len(state.queries) != 4 || state.writes != 0 {
		t.Fatal("complete manager set or fixed four-query admission budget lost", len(recipients), len(state.queries), err)
	}
	seen := map[string]bool{}
	for i, recipient := range recipients {
		if seen[recipient.ID] || recipient.ID != state.users[i].ID || recipient.ManagerID != published.Managers[recipient.ID] || !recipient.CreatedAt.Equal(state.users[i].CreatedAt) {
			t.Fatal("bounded complete recipient proof changed")
		}
		seen[recipient.ID] = true
	}
	if len(state.userIDs) != quotaInboxManagerLimit || !slices.Equal(state.userIDs, func() []string {
		ids := make([]string, len(state.users))
		for i, user := range state.users {
			ids[i] = user.ID
		}
		return ids
	}()) {
		t.Fatal("scoped identity query expanded or omitted management candidates")
	}
}
