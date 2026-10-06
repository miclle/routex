package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMemberOverviewRolesCursorAndRetainedBounds(t *testing.T) {
	for _, input := range []MemberOverviewRolesFilter{{Limit: -1}, {Limit: 51}, {Cursor: "bad"}, {Cursor: memberOverviewCursor("usr_other", "rol_own")}, {Cursor: memberOverviewCursor("usr_self", "usr_other")}, {Cursor: strings.Repeat("x", 129)}} {
		if _, _, err := normalizeMemberOverviewRolesFilter("usr_self", input); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("invalid cursor or limit accepted", input, err)
		}
	}
	filter, after, err := normalizeMemberOverviewRolesFilter("usr_self", MemberOverviewRolesFilter{})
	if err != nil || filter.Limit != 10 || after != "" {
		t.Fatal(filter, after, err)
	}
	rows := make([]entity.UserRole, memberRolesReadBudget)
	for i := range rows {
		rows[i] = entity.UserRole{UserID: "usr_self", RoleID: fmt.Sprintf("rol_self_%05d", memberRolesReadBudget-i)}
	}
	ids, next, err := memberOverviewRolePageIDs("usr_self", "", 50, rows)
	if err != nil || len(ids) != 50 || ids[0] != "rol_self_00001" || next == nil {
		t.Fatal("retained 10000 set/page changed", err)
	}
	_, after, err = normalizeMemberOverviewRolesFilter("usr_self", MemberOverviewRolesFilter{Cursor: *next, Limit: 50})
	ids, last, err2 := memberOverviewRolePageIDs("usr_self", after, 50, rows)
	if err != nil || err2 != nil || ids[0] != "rol_self_00051" || last == nil {
		t.Fatal("exact ordered cursor changed", err, err2)
	}
	for name, bad := range map[string][]entity.UserRole{"overflow": append(rows, entity.UserRole{UserID: "usr_self", RoleID: "rol_overflow"}), "owner_alias": {{UserID: "USR_SELF", RoleID: "rol_own"}}, "duplicate": {{UserID: "usr_self", RoleID: "rol_own"}, {UserID: "usr_self", RoleID: "rol_own"}}, "intrinsic": {{UserID: "usr_self", RoleID: "rol_admin"}}, "bad_id": {{UserID: "usr_self", RoleID: "ROL_own"}}} {
		if _, _, err := memberOverviewRolePageIDs("usr_self", "", 10, bad); err == nil {
			t.Fatal("unproven set accepted", name)
		}
	}
}

func TestMemberOverviewRolesLabelsAreOnlyExplicitDisplayFacts(t *testing.T) {
	ids := []string{"rol_custom", "rol_finance"}
	rows := []entity.Role{{ID: "rol_finance", Name: "Finance", Builtin: true}, {ID: "rol_custom", Name: ""}}
	result, err := projectMemberOverviewRoleLabels(ids, rows)
	if err != nil || result[0].Name != nil || *result[1].Name != "Finance" || result[1].AssignmentKind != RoleAssignmentExplicit {
		t.Fatal(result, err)
	}
	for _, name := range []string{" ", " untrimmed", "bad\nname", strings.Repeat("x", 101), string([]byte{0xff})} {
		got, err := projectMemberOverviewRoleLabels([]string{"rol_custom"}, []entity.Role{{ID: "rol_custom", Name: name}})
		if err != nil || got[0].Name != nil {
			t.Fatal("bad legacy name invented", err)
		}
	}
	for name, bad := range map[string][]entity.Role{"missing": {}, "alias": {{ID: "ROL_custom"}, {ID: "rol_finance", Builtin: true}}, "duplicate": {rows[0], rows[0], rows[1]}, "unknown_builtin": {{ID: "rol_custom", Builtin: true}, rows[0]}, "reserved_false": {rows[1], {ID: "rol_finance", Builtin: false}}} {
		if _, err := projectMemberOverviewRoleLabels(ids, bad); err == nil {
			t.Fatal("unproven metadata accepted", name)
		}
	}
	encoded, _ := json.Marshal(MemberOverviewRolesPage{ActorUserID: "usr_self", IdentityRole: "member", Roles: result})
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(encoded, &fields)
	if len(fields) != 5 {
		t.Fatal("page leaked extra private/permission facts", string(encoded))
	}
	var items []map[string]json.RawMessage
	_ = json.Unmarshal(fields["roles"], &items)
	for _, item := range items {
		if len(item) != 4 {
			t.Fatal("label leaked extra facts", item)
		}
	}
}

func TestMemberOverviewRolesSQLIsExactIndexedAndBounded(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
		if err != nil {
			t.Fatal(err)
		}
		q := memberOverviewRoleAssignmentsQuery(db, "usr_self")
		q.Statement.Clauses["WHERE"].Build(q.Statement)
		q.Statement.Clauses["LIMIT"].Build(q.Statement)
		query := q.Statement.SQL.String()
		if !strings.Contains(query, "user_id") || !strings.Contains(query, "10001") && !strings.Contains(query, "LIMIT ?") || len(q.Statement.Vars) < 2 || q.Statement.Vars[0] != "usr_self" || q.Statement.Vars[1] != "usr_self" {
			t.Fatal("candidate/exact ownership or cap missing", query, q.Statement.Vars)
		}
		if dialect.Name() == "mysql" && !strings.Contains(query, "BINARY") {
			t.Fatal("collation guard absent", query)
		}
	}
}

type overviewRoleFixture struct {
	actor       entity.User
	assignments []entity.UserRole
	roles       []entity.Role
	apps        []entity.RegistrationApprovalApplication
	queries     []string
	options     driver.TxOptions
	deadline    bool
	writes      int
	fail        string
}
type overviewRoleConnector struct{ fixture *overviewRoleFixture }

func (c overviewRoleConnector) Connect(context.Context) (driver.Conn, error) {
	return overviewRoleConnection(c), nil
}
func (c overviewRoleConnector) Driver() driver.Driver { return overviewRoleDriver(c) }

type overviewRoleDriver overviewRoleConnector

func (c overviewRoleDriver) Open(string) (driver.Conn, error) {
	return overviewRoleConnection(c), nil
}

type overviewRoleConnection overviewRoleConnector

func (c overviewRoleConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepared statement")
}
func (c overviewRoleConnection) Close() error { return nil }
func (c overviewRoleConnection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c overviewRoleConnection) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.fixture.options = opts
	deadline, ok := ctx.Deadline()
	c.fixture.deadline = ok && time.Until(deadline) <= 5*time.Second
	return adminOverviewTransaction{}, nil
}
func (c overviewRoleConnection) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.fixture.writes++
	return nil, errors.New("unexpected write")
}
func (c overviewRoleConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	f := c.fixture
	f.queries = append(f.queries, query)
	if f.fail != "" && strings.Contains(query, f.fail) {
		return nil, errors.New("controlled read outage")
	}
	switch {
	case strings.Contains(query, `FROM "users"`):
		return effectiveSQLRows([]entity.User{f.actor})
	case strings.Contains(query, `FROM "registration_approval_applications"`):
		return effectiveSQLRows(f.apps)
	case strings.Contains(query, `FROM "user_roles"`):
		return effectiveSQLRows(f.assignments)
	case strings.Contains(query, `FROM "roles"`):
		rows := []entity.Role{}
		for _, role := range f.roles {
			for _, arg := range args {
				if arg.Value == role.ID {
					rows = append(rows, role)
					break
				}
			}
		}
		return effectiveSQLRows(rows)
	}
	return nil, errors.New("unexpected directory/permission query")
}
func overviewRoleFixtureService(t *testing.T, f *overviewRoleFixture) *Service {
	t.Helper()
	pool := sql.OpenDB(overviewRoleConnector{f})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}
}
func TestMemberOverviewRolesSnapshotReadsAndAdmission(t *testing.T) {
	birth := time.Now().UTC().Add(-time.Hour)
	base := func() *overviewRoleFixture {
		return &overviewRoleFixture{actor: entity.User{ID: "usr_self", Role: entity.RoleMember, CreatedAt: birth}, assignments: []entity.UserRole{{UserID: "usr_self", RoleID: "rol_finance"}}, roles: []entity.Role{{ID: "rol_finance", Name: "Finance", Builtin: true}}}
	}
	for _, name := range []string{"unmanaged", "approved", "pending", "rejected", "missing", "wrong_birth", "wrong_user", "wrong_link", "no_birth", "disabled", "offboarded", "alias", "unknown_identity", "outage"} {
		t.Run(name, func(t *testing.T) {
			f := base()
			id := "raa_01j00000000000000000000000"
			if name != "unmanaged" {
				f.actor.ApprovalApplicationID = &id
				f.apps = []entity.RegistrationApprovalApplication{approvedMemberListApplication(f.actor, id)}
			}
			switch name {
			case "pending":
				f.apps[0].State = "pending"
				f.apps[0].DecidedAt = nil
				f.apps[0].DecisionActorID = nil
				f.apps[0].DecisionReason = nil
			case "rejected":
				f.apps[0].State = "rejected"
			case "missing":
				f.apps = nil
			case "wrong_birth":
				f.apps[0].UserCreatedAt = birth.Add(time.Millisecond)
			case "wrong_user":
				f.apps[0].UserID = "usr_other"
			case "wrong_link":
				bad := "raa_INVALID"
				f.actor.ApprovalApplicationID = &bad
			case "no_birth":
				f.actor.CreatedAt = time.Time{}
			case "disabled":
				f.actor.Disabled = true
			case "offboarded":
				f.actor.OffboardedAt = &birth
			case "alias":
				f.actor.ID = "USR_SELF"
			case "unknown_identity":
				f.actor.Role = "other"
			case "outage":
				f.fail = "registration_approval_applications"
			}
			page, err := overviewRoleFixtureService(t, f).MemberOverviewRoles(context.Background(), "usr_self", MemberOverviewRolesFilter{})
			positive := name == "unmanaged" || name == "approved"
			if positive != (err == nil) || !positive && page != nil {
				t.Fatal("admission boundary changed", name, page, err)
			}
			if positive {
				want := 3
				if name == "approved" {
					want = 4
				}
				if len(f.queries) != want || len(page.Roles) != 1 || page.IdentityRole != "member" {
					t.Fatal("query budget/labels changed", len(f.queries), page)
				}
			}
			if !f.options.ReadOnly || f.options.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) || f.writes != 0 || !f.deadline {
				t.Fatal("snapshot/deadline/write boundary changed", f.options)
			}
			actorReads := 0
			for _, q := range f.queries {
				if strings.Contains(q, `FROM "users"`) {
					actorReads++
					if !strings.Contains(q, "SELECT *") {
						t.Fatal("incomplete actor admission projection", q)
					}
				}
				if strings.Contains(q, "role_permissions") || strings.Contains(q, "teams") {
					t.Fatal("directory/permission read", q)
				}
			}
			if actorReads != 1 {
				t.Fatal("actor proof reread", actorReads)
			}
		})
	}
}
func TestMemberOverviewRolesRetainedQueryBudget(t *testing.T) {
	for _, count := range []int{0, 500, 501, 1000, 10000, 10001} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := &overviewRoleFixture{actor: entity.User{ID: "usr_self", Role: entity.RoleMember, CreatedAt: time.Now().UTC()}}
			for i := 0; i < count; i++ {
				id := fmt.Sprintf("rol_bound_%05d", i)
				f.assignments = append(f.assignments, entity.UserRole{UserID: "usr_self", RoleID: id})
				f.roles = append(f.roles, entity.Role{ID: id, Name: "Own label"})
			}
			page, err := overviewRoleFixtureService(t, f).MemberOverviewRoles(context.Background(), "usr_self", MemberOverviewRolesFilter{Limit: 50})
			if count > memberRolesReadBudget {
				if !errors.Is(err, memberRolesOverflow) || len(f.queries) != 2 || page != nil {
					t.Fatal("overflow partially exposed", err, len(f.queries))
				}
				return
			}
			want := 3
			if count == 0 {
				want = 2
			}
			if err != nil || len(f.queries) != want || len(page.Roles) != min(count, 50) {
				t.Fatal("bounded metadata queries changed", count, err, len(f.queries))
			}
			for _, q := range f.queries {
				if strings.Contains(q, `FROM "roles"`) && (strings.Contains(q, "definition_revision") || strings.Contains(q, "description") || !strings.Contains(q, "LIMIT")) {
					t.Fatal("role projection expanded", q)
				}
			}
		})
	}
}

func TestMemberOverviewRolesReadFailureNeverReturnsPartialLabels(t *testing.T) {
	for _, name := range []string{"assignment_outage", "metadata_outage", "missing", "alias", "duplicate", "owner_alias", "unknown_builtin", "reserved_false", "canceled"} {
		t.Run(name, func(t *testing.T) {
			f := &overviewRoleFixture{actor: entity.User{ID: "usr_self", Role: entity.RoleMember, CreatedAt: time.Now().UTC()}, assignments: []entity.UserRole{{UserID: "usr_self", RoleID: "rol_finance"}}, roles: []entity.Role{{ID: "rol_finance", Name: "Finance", Builtin: true}}}
			switch name {
			case "assignment_outage":
				f.fail = `FROM "user_roles"`
			case "metadata_outage":
				f.fail = `FROM "roles"`
			case "missing":
				f.roles = nil
			case "alias":
				f.roles[0].ID = "ROL_finance"
			case "duplicate":
				f.roles = append(f.roles, f.roles[0])
			case "owner_alias":
				f.assignments[0].UserID = "USR_SELF"
			case "unknown_builtin":
				f.assignments[0].RoleID = "rol_unknown"
				f.roles[0].ID = "rol_unknown"
			case "reserved_false":
				f.roles[0].Builtin = false
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "canceled" {
				cancel()
			}
			page, err := overviewRoleFixtureService(t, f).MemberOverviewRoles(ctx, "usr_self", MemberOverviewRolesFilter{})
			if err == nil || page != nil || f.writes != 0 {
				t.Fatal("partial or stale private labels returned", name, page, err)
			}
		})
	}
}
