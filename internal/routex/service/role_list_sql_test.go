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
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type roleListSQLControl struct {
	admission       *roleDefinitionSQLControl
	fail            string
	permissionAlias bool
	builtinCounts   []roleListBaseCount
	customCounts    []roleListCustomCount
}
type roleListSQLConnector struct {
	fixture *rolesSQLFixture
	control *roleListSQLControl
}

func (c roleListSQLConnector) Connect(context.Context) (driver.Conn, error) {
	return &roleListSQLConnection{roleDefinitionSQLConnection: &roleDefinitionSQLConnection{rolesSQLConnection: &rolesSQLConnection{f: c.fixture}, control: c.control.admission}, list: c.control}, nil
}
func (c roleListSQLConnector) Driver() driver.Driver { return roleListSQLDriver(c) }

type roleListSQLDriver roleListSQLConnector

func (d roleListSQLDriver) Open(string) (driver.Conn, error) {
	return roleListSQLConnector(d).Connect(context.Background())
}

type roleListSQLConnection struct {
	*roleDefinitionSQLConnection
	list *roleListSQLControl
}

func (c *roleListSQLConnection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.list.fail != "" && strings.Contains(query, c.list.fail) {
		c.f.queries = append(c.f.queries, query)
		return nil, errors.New("controlled role list read outage")
	}
	switch {
	case strings.Contains(query, "COUNT(*) AS member_count") && strings.Contains(query, `FROM "users"`):
		c.f.queries = append(c.f.queries, query)
		if c.list.builtinCounts != nil {
			return effectiveSQLRows(c.list.builtinCounts)
		}
		counts := map[string]int64{}
		for _, u := range c.current().users {
			if u.Role == entity.RoleAdmin || u.Role == entity.RoleMember {
				counts[u.Role]++
			}
		}
		rows := []roleListBaseCount{}
		for _, role := range []string{entity.RoleAdmin, entity.RoleMember} {
			if n := counts[role]; n > 0 {
				rows = append(rows, roleListBaseCount{role, n})
			}
		}
		return effectiveSQLRows(rows)
	case strings.Contains(query, "COUNT(*) AS member_count") && strings.Contains(query, `FROM "user_roles"`):
		c.f.queries = append(c.f.queries, query)
		if c.list.customCounts != nil {
			return effectiveSQLRows(c.list.customCounts)
		}
		counts := map[string]int64{}
		for _, binding := range c.current().assignments {
			user, userOK := c.current().users[binding.UserID]
			role, roleOK := c.current().roles[binding.RoleID]
			if userOK && roleOK && user.ID == binding.UserID && role.ID == binding.RoleID && !role.Builtin {
				counts[role.ID]++
			}
		}
		rows := []roleListCustomCount{}
		for id, n := range counts {
			rows = append(rows, roleListCustomCount{id, n})
		}
		return effectiveSQLRows(rows)
	case strings.Contains(query, `FROM "roles"`) && !strings.Contains(query, "WHERE"):
		c.f.queries = append(c.f.queries, query)
		rows := []entity.Role{}
		for _, role := range c.current().roles {
			rows = append(rows, role)
		}
		slices.SortFunc(rows, func(a, b entity.Role) int {
			if a.Builtin != b.Builtin {
				if a.Builtin {
					return -1
				}
				return 1
			}
			if order := strings.Compare(a.Name, b.Name); order != 0 {
				return order
			}
			return strings.Compare(a.ID, b.ID)
		})
		return effectiveSQLRows(rows[:min(len(rows), rolesSQLLimit(args))])
	case c.list.permissionAlias && strings.Contains(query, `FROM "role_permissions"`):
		c.f.queries = append(c.f.queries, query)
		return effectiveSQLRows([]entity.RolePermission{{RoleID: "ROL_00000", Permission: "members.read"}})
	default:
		return c.roleDefinitionSQLConnection.QueryContext(ctx, query, args)
	}
}

func roleListSQLService(t *testing.T, custom int) (*Service, *rolesSQLFixture, *roleListSQLControl) {
	t.Helper()
	_, f := roleSQLService(t, custom)
	control := &roleListSQLControl{admission: &roleDefinitionSQLControl{applications: map[string]entity.RegistrationApprovalApplication{}}}
	pool := sql.OpenDB(roleListSQLConnector{fixture: f, control: control})
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: pool}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Service{db: db}, f, control
}

func TestRoleListSQLBatchedReadSnapshotAndCounts(t *testing.T) {
	for _, custom := range []int{0, 1, 498, 499, 998} {
		t.Run(fmt.Sprint(custom+2), func(t *testing.T) {
			s, f, _ := roleListSQLService(t, custom)
			rows, err := s.ListRoles(context.Background(), "usr_admin")
			if err != nil || len(rows) != custom+2 {
				t.Fatal(rows, err)
			}
			budget := 5 + (custom+2+memberRolesBatchSize-1)/memberRolesBatchSize
			if len(f.queries) != budget || len(f.writes) != 0 || len(f.transactions) != 1 || !f.transactions[0].ReadOnly || f.transactions[0].Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
				t.Fatal("non-batched or non-snapshot read", len(f.queries), budget, f.transactions)
			}
			for _, row := range rows {
				if row.MemberCount == nil || *row.MemberCount != 1 || row.Permissions == nil {
					t.Fatal("missing authoritative count or complete permissions", row)
				}
			}
			for _, query := range f.queries {
				if strings.Contains(query, "COUNT(*) AS member_count") {
					for _, private := range []string{"password_hash", "email", "team_roles", "team_members", "disabled", "offboarded_at", "registration_approval_applications"} {
						if strings.Contains(query, private) {
							t.Fatal("count acquired private or effective-access semantics", query)
						}
					}
					if strings.Contains(query, "user_roles") && (!strings.Contains(query, `"person"."id" = "assignment"."user_id"`) || !strings.Contains(query, `"selected_role"."id" = "assignment"."role_id"`)) {
						t.Fatal("count lost exact joins", query)
					}
				}
			}
		})
	}
}

func TestRoleListSQLRetainedMembershipAndExactIdentities(t *testing.T) {
	s, f, _ := roleListSQLService(t, 2)
	f.data.assignments = []entity.UserRole{{UserID: "usr_target", RoleID: "rol_00000"}, {UserID: "USR_TARGET", RoleID: "rol_00000"}, {UserID: "usr_missing", RoleID: "rol_00000"}, {UserID: "usr_target", RoleID: "ROL_00000"}, {UserID: "usr_target", RoleID: "rol_admin"}}
	for _, id := range []string{"usr_disabled", "usr_offboarded", "usr_pending", "usr_rejected"} {
		u := f.data.users["usr_target"]
		u.ID = id
		if id == "usr_disabled" {
			u.Disabled = true
		}
		if id == "usr_offboarded" {
			u.OffboardedAt = &u.CreatedAt
		}
		if id == "usr_pending" || id == "usr_rejected" {
			a := "raa_" + id[4:]
			u.ApprovalApplicationID = &a
		}
		f.data.users[id] = u
		f.data.assignments = append(f.data.assignments, entity.UserRole{UserID: id, RoleID: "rol_00000"})
	}
	u := f.data.users["usr_target"]
	u.ID, u.Role = "usr_role_alias", "Member"
	f.data.users[u.ID] = u
	f.data.permissions["rol_00000"] = []string{"Recorded.Mixed_CASE"}
	rows, err := s.ListRoles(context.Background(), "usr_admin")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"rol_admin": 1, "rol_member": 5, "rol_00000": 5, "rol_00001": 0}
	for _, row := range rows {
		if row.MemberCount == nil || *row.MemberCount != want[row.Role.ID] {
			t.Fatal(row, want)
		}
		if row.Role.ID == "rol_00000" && !slices.Equal(row.Permissions, []string{"Recorded.Mixed_CASE"}) {
			t.Fatal("historical permission lost", row)
		}
	}
}

func TestRoleListSQLBoundsDenialAndNoPartialResults(t *testing.T) {
	for _, fault := range []string{"denied", "actor_alias", "disabled", "offboarded", "invalid_base_role", "pending", "catalogue", "permissions", "permission_alias", "builtin_alias", "custom_alias", "duplicate_count", "negative_count", "unsafe_count", "read_outage"} {
		t.Run(fault, func(t *testing.T) {
			custom := 1
			if fault == "catalogue" {
				custom = 999
			}
			s, f, c := roleListSQLService(t, custom)
			switch fault {
			case "denied":
				f.deny["roles.read"] = true
			case "actor_alias":
				f.actorAlias = true
			case "disabled", "offboarded", "invalid_base_role":
				u := f.data.users["usr_admin"]
				u.Disabled = fault == "disabled"
				if fault == "offboarded" {
					u.OffboardedAt = &u.CreatedAt
				}
				if fault == "invalid_base_role" {
					u.Role = "Member"
				}
				f.data.users[u.ID] = u
			case "pending":
				roleDefinitionSQLPending(f, c.admission)
			case "permissions":
				f.data.permissions["rol_00000"] = make([]string, 101)
			case "permission_alias":
				c.permissionAlias = true
			case "builtin_alias":
				c.builtinCounts = []roleListBaseCount{{"Member", 1}}
			case "custom_alias":
				c.customCounts = []roleListCustomCount{{"ROL_00000", 1}}
			case "duplicate_count":
				c.customCounts = []roleListCustomCount{{"rol_00000", 1}, {"rol_00000", 2}}
			case "negative_count":
				c.customCounts = []roleListCustomCount{{"rol_00000", -1}}
			case "unsafe_count":
				c.customCounts = []roleListCustomCount{{"rol_00000", roleListMaximumMemberCount + 1}}
			case "read_outage":
				c.fail = "COUNT(*) AS member_count"
			}
			rows, err := s.ListRoles(context.Background(), "usr_admin")
			if rows != nil || err == nil || len(f.writes) != 0 {
				t.Fatal("partial or unsafe list returned", rows, err)
			}
			if (fault == "catalogue" || fault == "permissions" || fault == "unsafe_count") && err != roleListOverflow {
				t.Fatal("unsupported bounds were not explicit", err)
			}
			if fault == "denied" && err != apperrors.ErrForbidden {
				t.Fatal(err)
			}
			if slices.Contains([]string{"denied", "actor_alias", "pending", "disabled", "offboarded", "invalid_base_role"}, fault) {
				for _, query := range f.queries {
					if strings.Contains(query, `FROM "roles"`) || strings.Contains(query, "COUNT(*) AS member_count") {
						t.Fatal("list read before fresh authorization", query)
					}
				}
			}
		})
	}
}

// Only the portability adapter selects the binary-equality expression. The
// scripted connection remains local; actual driver acceptance is separate.
type roleListSQLMySQLName struct{ gorm.Dialector }

func (roleListSQLMySQLName) Name() string { return "mysql" }

func TestRoleListSQLCountJoinsUsePortabilityAdapter(t *testing.T) {
	s, f, _ := roleListSQLService(t, 1)
	s.db.Dialector = roleListSQLMySQLName{s.db.Dialector}
	if _, err := s.ListRoles(context.Background(), "usr_admin"); err != nil {
		t.Fatal(err)
	}
	var builtin, custom bool
	for _, query := range f.queries {
		if !strings.Contains(query, "COUNT(*) AS member_count") {
			continue
		}
		if strings.Contains(query, `FROM "users"`) {
			builtin = strings.Contains(query, `CAST("role" AS BINARY) = CAST(`)
		} else {
			custom = strings.Contains(query, `CAST("person"."id" AS BINARY) = CAST("assignment"."user_id" AS BINARY)`) && strings.Contains(query, `CAST("selected_role"."id" AS BINARY) = CAST("assignment"."role_id" AS BINARY)`)
		}
	}
	if !builtin || !custom {
		t.Fatal("builtin or custom count bypassed database portability", f.queries)
	}
}
