package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type memberListAdmissionSQLFixture struct {
	handoverRows       []memberListHandover
	handoverFailure    bool
	actor              entity.User
	actorID            string
	users              []entity.User
	applications       []entity.RegistrationApprovalApplication
	permissions        map[string]bool
	memberships        []entity.TeamMembership
	teams              []entity.Team
	queries            []string
	options            driver.TxOptions
	writes             int
	applicationFailure bool
}
type memberListAdmissionConnector struct {
	fixture *memberListAdmissionSQLFixture
}

func (c memberListAdmissionConnector) Connect(context.Context) (driver.Conn, error) {
	return memberListAdmissionConnection(c), nil
}
func (c memberListAdmissionConnector) Driver() driver.Driver { return memberListAdmissionDriver(c) }

type memberListAdmissionDriver memberListAdmissionConnector

func (d memberListAdmissionDriver) Open(string) (driver.Conn, error) {
	return memberListAdmissionConnection(d), nil
}

type memberListAdmissionConnection memberListAdmissionConnector

func (memberListAdmissionConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (memberListAdmissionConnection) Close() error { return nil }
func (c memberListAdmissionConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c memberListAdmissionConnection) BeginTx(_ context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = options
	return adminOverviewTransaction{}, nil
}
func (c memberListAdmissionConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.fixture.writes++
	return nil, errors.New("read-only member list wrote state")
}

// Honor the actual SELECT list: returning omitted private link/birth columns
// here would hide an incomplete actor projection in a fake SQL driver.
func memberListAdmissionUserRows(query string, users []entity.User) (driver.Rows, error) {
	raw, err := effectiveSQLRows(users)
	if err != nil {
		return nil, err
	}
	rows := raw.(*adminOverviewRows)
	head, _, _ := strings.Cut(query, " FROM ")
	if strings.Contains(head, "SELECT *") {
		return rows, nil
	}
	out := &adminOverviewRows{}
	for index, name := range rows.columns {
		if !strings.Contains(head, `"`+name+`"`) {
			continue
		}
		out.columns = append(out.columns, name)
		for r := range rows.values {
			if len(out.values) <= r {
				out.values = append(out.values, []driver.Value{})
			}
			out.values[r] = append(out.values[r], rows.values[r][index])
		}
	}
	return out, nil
}
func (c memberListAdmissionConnection) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f := c.fixture
	f.queries = append(f.queries, q)
	switch {
	case strings.Contains(q, "offboarding_cases AS handover"):
		if f.handoverFailure {
			return nil, errors.New("controlled handover read outage")
		}
		return effectiveSQLRows(f.handoverRows)
	case strings.Contains(q, `FROM "users"`):
		for _, arg := range args {
			if arg.Value == f.actorID {
				return memberListAdmissionUserRows(q, []entity.User{f.actor})
			}
		}
		return memberListAdmissionUserRows(q, f.users)
	case strings.Contains(q, `FROM "registration_approval_applications"`):
		if f.applicationFailure {
			return nil, errors.New("controlled admission read outage")
		}
		rows := []entity.RegistrationApprovalApplication{}
		for _, app := range f.applications {
			for _, arg := range args {
				if arg.Value == app.ID {
					rows = append(rows, app)
					break
				}
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(q, "role_permissions AS permission"):
		for _, arg := range args {
			if permission, ok := arg.Value.(string); ok && f.permissions[permission] {
				return effectiveSQLRows([]exactPermissionIdentity{{RoleID: "rol_member", PermissionRoleID: "rol_member", Permission: permission}})
			}
		}
		return effectiveSQLRows([]exactPermissionIdentity{})
	case strings.Contains(q, `FROM "user_roles"`):
		return effectiveSQLRows([]entity.UserRole{})
	case strings.Contains(q, "api_keys AS retained_key"):
		return effectiveSQLRows([]memberListKeyCount{})
	case strings.Contains(q, `FROM "resource_limits"`):
		return effectiveSQLRows([]entity.ResourceLimit{})
	case strings.Contains(q, `FROM "pricing_settings"`):
		return effectiveSQLRows([]entity.PricingSetting{{ID: 1, PlatformCurrency: "USD"}})
	case strings.Contains(q, `FROM "quota_settings"`):
		return effectiveSQLRows([]entity.QuotaSetting{{ID: 1, TimeZone: "UTC", ETag: memberRoleBaseline}})
	case strings.Contains(q, `FROM "team_memberships"`):
		return effectiveSQLRows(f.memberships)
	case strings.Contains(q, `FROM "teams"`):
		return effectiveSQLRows(f.teams)
	}
	return nil, errors.New("unexpected member list SQL shape")
}
func memberListAdmissionService(t *testing.T, count int) (*Service, *memberListAdmissionSQLFixture) {
	t.Helper()
	created := time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC)
	f := &memberListAdmissionSQLFixture{actor: entity.User{ID: "usr_reader", Role: entity.RoleMember, CreatedAt: created}, actorID: "usr_reader", permissions: map[string]bool{"members.read": true}}
	for i := range count {
		f.users = append(f.users, entity.User{ID: fmt.Sprintf("usr_subject_%03d", i), Email: fmt.Sprintf("subject-%d@example.invalid", i), Name: "Retained subject", Role: entity.RoleMember, CreatedAt: created})
	}
	pool := sql.OpenDB(memberListAdmissionConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f
}
func approvedMemberListApplication(user entity.User, id string) entity.RegistrationApprovalApplication {
	now := user.CreatedAt.Add(time.Minute)
	actor, reason := "usr_reviewer", "Reviewed registration"
	return entity.RegistrationApprovalApplication{ID: id, UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: now, State: "approved", Revision: memberRoleBaseline, DecidedAt: &now, DecisionActorID: &actor, DecisionReason: &reason}
}
func TestMemberListAdmissionMaintainsOriginalMeasuredQueryBudgets(t *testing.T) {
	for _, count := range []int{1, 100} {
		for _, mode := range []string{"personal", "teams", "overflow"} {
			t.Run(fmt.Sprintf("%s_%d", mode, count), func(t *testing.T) {
				s, f := memberListAdmissionService(t, count)
				want := 10
				if mode != "personal" {
					f.permissions["teams.read_all"] = true
					f.memberships = []entity.TeamMembership{{ID: "tmm_one", UserID: f.users[0].ID, TeamID: "tea_one", Role: entity.TeamMember, Status: entity.ResourceActive}}
					f.teams = []entity.Team{{ID: "tea_one", Name: "Retained Team", Status: entity.ResourceActive}}
					want = 12
				}
				if mode == "overflow" {
					f.memberships = make([]entity.TeamMembership, memberListTeamBudget+1)
					want = 11
				}
				page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: count})
				if err != nil || page == nil || len(page.Members) != count || len(f.queries) != want {
					t.Fatal(err, page != nil, len(f.queries), want)
				}
				if !f.options.ReadOnly || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || f.writes != 0 {
					t.Fatal("list lost read-only coherent transaction", f.options, f.writes)
				}
				actorReads := 0
				for _, q := range f.queries {
					if strings.Contains(q, `FROM "users"`) && strings.Contains(q, `offboarded_at IS NULL`) {
						actorReads++
						if !strings.Contains(q, `"created_at"`) || !strings.Contains(q, `"approval_application_id"`) {
							t.Fatal("actor identity is incomplete", q)
						}
					}
					if mode == "personal" && (strings.Contains(q, "team_memberships") || strings.Contains(q, `FROM "teams"`)) {
						t.Fatal("no Team SQL without independent authority")
					}
				}
				if actorReads != 1 {
					t.Fatal("actor reloaded for independent permission checks", actorReads)
				}
				if mode == "overflow" && page.Members[0].Teams.Status != "overflow" {
					t.Fatal("overflow replaced with partial Team facts")
				}
				for _, row := range page.Members {
					if row.RegistrationApproval.Status != "not_required" || !row.RegistrationApproval.AdmissionEligible || row.Personal.RuntimeApplied {
						t.Fatal("unpublished or missing subject proof invented", row.RegistrationApproval, row.Personal.RuntimeApplied)
					}
				}
			})
		}
	}
}
func TestMemberListAdmissionHydratesActorOnceAndSubjectsInOneBatch(t *testing.T) {
	s, f := memberListAdmissionService(t, 100)
	id := "raa_01j00000000000000000000000"
	f.actor.ApprovalApplicationID = &id
	f.applications = []entity.RegistrationApprovalApplication{approvedMemberListApplication(f.actor, id)}
	for i := range f.users {
		id := fmt.Sprintf("raa_01j%023d", i+1)
		f.users[i].ApprovalApplicationID = &id
		app := approvedMemberListApplication(f.users[i], id)
		if i == 0 {
			app.State = "pending"
			app.DecidedAt = nil
			app.DecisionActorID = nil
			app.DecisionReason = nil
		}
		f.applications = append(f.applications, app)
	}
	page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: 100})
	if err != nil || page == nil || len(f.queries) != 12 {
		t.Fatal(err, page != nil, len(f.queries))
	}
	apps := 0
	for _, q := range f.queries {
		if strings.Contains(q, `FROM "registration_approval_applications"`) {
			apps++
		}
	}
	if apps != 2 {
		t.Fatal("actor proof repeated or subject hydration is per-row", apps)
	}
	if page.Members[0].RegistrationApproval.Status != "pending" || page.Members[0].RegistrationApproval.AdmissionEligible || page.Members[1].RegistrationApproval.Status != "approved" || !page.Members[1].RegistrationApproval.AdmissionEligible {
		t.Fatal("retained pending/admitted subject projection changed")
	}
	if f.writes != 0 || !f.options.ReadOnly {
		t.Fatal("admission read wrote state")
	}
}
func TestMemberListAdmissionRejectsUnprovenActorsBeforePermissionOrTargetReads(t *testing.T) {
	for _, name := range []string{"pending", "rejected", "missing", "wrong_birth", "wrong_user", "wrong_application_id", "invalid_link", "no_birth", "disabled", "offboarded", "case_alias", "space_alias", "outage", "read_denied"} {
		t.Run(name, func(t *testing.T) {
			s, f := memberListAdmissionService(t, 1)
			id := "raa_01j00000000000000000000000"
			f.actor.ApprovalApplicationID = &id
			app := approvedMemberListApplication(f.actor, id)
			f.applications = []entity.RegistrationApprovalApplication{app}
			want := apperrors.ErrUnauthorized
			switch name {
			case "pending":
				f.applications[0].State = "pending"
				f.applications[0].DecidedAt = nil
				f.applications[0].DecisionActorID = nil
				f.applications[0].DecisionReason = nil
			case "rejected":
				f.applications[0].State = "rejected"
			case "missing":
				f.applications = nil
			case "wrong_birth":
				f.applications[0].UserCreatedAt = f.actor.CreatedAt.Add(time.Microsecond)
			case "wrong_user":
				f.applications[0].UserID = "usr_foreign"
			case "wrong_application_id":
				f.applications[0].ID = strings.ToUpper(id)
			case "invalid_link":
				id += " "
				f.actor.ApprovalApplicationID = &id
			case "no_birth":
				f.actor.CreatedAt = time.Time{}
			case "disabled":
				f.actor.Disabled = true
			case "offboarded":
				f.actor.OffboardedAt = &app.CreatedAt
			case "case_alias":
				f.actorID = strings.ToUpper(f.actor.ID)
			case "space_alias":
				f.actorID = f.actor.ID + " "
			case "outage":
				f.applicationFailure = true
			case "read_denied":
				f.permissions["members.read"] = false
				want = apperrors.ErrForbidden
			}
			page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: 1})
			if page != nil || err == nil || name != "outage" && !errors.Is(err, want) {
				t.Fatal("unproven actor reached a page", name, err, page != nil)
			}
			for _, q := range f.queries {
				if name != "read_denied" && strings.Contains(q, "role_permissions AS permission") {
					t.Fatal("unproven admission reached permission lookup", name)
				}
				if strings.Contains(q, `FROM "user_roles"`) || strings.Contains(q, "api_keys AS retained_key") || strings.Contains(q, `FROM "resource_limits"`) || strings.Contains(q, `FROM "teams"`) {
					t.Fatal("denied actor read private target facts", name)
				}
			}
			if f.writes != 0 {
				t.Fatal("denied read wrote state")
			}
		})
	}
}

func TestMemberListHandoverReadFailureNeverBecomesNoPlan(t *testing.T) {
	s, f := memberListAdmissionService(t, 2)
	f.handoverRows = []memberListHandover{{UserID: f.users[0].ID}}
	page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: 2})
	if err != nil || page == nil || !page.Members[0].HandoverPlanRecorded || page.Members[1].HandoverPlanRecorded {
		t.Fatal("batch did not preserve a recorded case separately from admission", err)
	}
	if page.Members[0].User.Disabled || page.Members[0].User.OffboardedAt != nil {
		t.Fatal("recorded plan changed account state")
	}
	f.handoverFailure = true
	if page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: 2}); page != nil || err == nil {
		t.Fatal("failed case read became an authorized no-plan response", err)
	}
	f.handoverFailure = false
	f.handoverRows[0].UserID = strings.ToUpper(f.users[0].ID)
	if page, err := s.ListMemberSummaries(context.Background(), f.actorID, MemberFilter{Limit: 2}); page != nil || err == nil {
		t.Fatal("collation alias case was attributed to current subject", err)
	}
}
