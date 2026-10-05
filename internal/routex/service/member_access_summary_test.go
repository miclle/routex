package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/clause"
)

func TestMemberAccessRolesCompleteExactRetainedProjection(t *testing.T) {
	assignments := []entity.UserRole{{UserID: "usr_subject", RoleID: "rol_z"}, {UserID: "usr_subject", RoleID: "rol_A"}}
	rows := []entity.Role{{ID: "rol_z", Name: "\x00 retained\n"}, {ID: "rol_A", Name: "", Builtin: true}}
	result := projectMemberAccessRoles("usr_subject", assignments, rows)
	if result.Status != "available" || len(result.Items) != 2 || result.Items[0].ID != "rol_A" || !result.Items[0].Builtin || result.Items[1].Name != rows[0].Name {
		t.Fatal(result)
	}
	for _, test := range []struct {
		name        string
		assignments []entity.UserRole
		rows        []entity.Role
	}{
		{"missing", assignments, rows[:1]},
		{"metadata alias", assignments, []entity.Role{rows[0], {ID: "ROL_A", Name: "foreign"}}},
		{"metadata trailing", assignments, []entity.Role{rows[0], {ID: "rol_A ", Name: "foreign"}}},
		{"metadata duplicate", assignments, []entity.Role{rows[0], rows[0]}},
		{"relationship duplicate", []entity.UserRole{assignments[0], assignments[0]}, rows[:1]},
		{"foreign actor", []entity.UserRole{{UserID: "USR_SUBJECT", RoleID: "rol_A"}}, rows[1:]},
		{"unsafe role", []entity.UserRole{{UserID: "usr_subject", RoleID: "rol A"}}, nil},
		{"extra directory", assignments[:1], rows},
		{"invalid utf8", assignments[:1], []entity.Role{{ID: "rol_z", Name: string([]byte{0xff})}}},
		{"overlong label", assignments[:1], []entity.Role{{ID: "rol_z", Name: strings.Repeat("界", 101)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := projectMemberAccessRoles("usr_subject", test.assignments, test.rows)
			if got.Status != "unavailable" || got.Items != nil {
				t.Fatal(got)
			}
		})
	}
	empty := projectMemberAccessRoles("usr_subject", nil, nil)
	if empty.Status != "available" || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatal(empty)
	}
	if !memberAccessLabel(strings.Repeat("😀", 100)) {
		t.Fatal("code points counted as bytes")
	}
}

func TestMemberAccessTeamsPreserveIndependentRetainedStates(t *testing.T) {
	memberships := []entity.TeamMembership{
		{ID: "tmm_b", UserID: "usr_subject", TeamID: "tea_b", Role: "owner", Status: "disabled"},
		{ID: "tmm_A", UserID: "usr_subject", TeamID: "tea_A", Role: "member", Status: "active"},
	}
	rows := []entity.Team{{ID: "tea_b", Name: "\n retained ", Status: "archived"}, {ID: "tea_A", Name: "", Status: "disabled"}}
	got := projectMemberAccessTeams("usr_subject", memberships, rows)
	if got.Status != "available" || len(got.Items) != 2 || got.Items[0].ID != "tea_A" || got.Items[0].Status != "disabled" || got.Items[0].MembershipStatus != "active" || got.Items[1].MembershipRole != "owner" || got.Items[1].Name != rows[0].Name {
		t.Fatal(got)
	}
	for _, test := range []struct {
		name    string
		members []entity.TeamMembership
		rows    []entity.Team
	}{
		{"missing", memberships, rows[:1]},
		{"metadata alias", memberships, []entity.Team{rows[0], {ID: "TEA_A", Status: "active"}}},
		{"metadata duplicate", memberships, []entity.Team{rows[0], rows[0]}},
		{"membership duplicate", []entity.TeamMembership{memberships[0], memberships[0]}, rows[:1]},
		{"identity duplicate", []entity.TeamMembership{memberships[0], {ID: "tmm_b", UserID: "usr_subject", TeamID: "tea_A", Role: "member", Status: "active"}}, rows},
		{"foreign user", []entity.TeamMembership{{ID: "tmm_A", UserID: "USR_SUBJECT", TeamID: "tea_A", Role: "member", Status: "active"}}, rows[1:]},
		{"unsafe identity", []entity.TeamMembership{{ID: "tmm A", UserID: "usr_subject", TeamID: "tea_A", Role: "member", Status: "active"}}, rows[1:]},
		{"unknown role", []entity.TeamMembership{{ID: "tmm_A", UserID: "usr_subject", TeamID: "tea_A", Role: "admin", Status: "active"}}, rows[1:]},
		{"unknown membership state", []entity.TeamMembership{{ID: "tmm_A", UserID: "usr_subject", TeamID: "tea_A", Role: "member", Status: "ACTIVE"}}, rows[1:]},
		{"unknown team state", memberships[:1], []entity.Team{{ID: "tea_b", Status: "ACTIVE"}}},
		{"invalid text", memberships[:1], []entity.Team{{ID: "tea_b", Status: "active", Name: string([]byte{0xff})}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := projectMemberAccessTeams("usr_subject", test.members, test.rows)
			if got.Status != "unavailable" || got.Items != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestMemberAccessIndependentWholeSectionBudgets(t *testing.T) {
	assignments, rows := make([]entity.UserRole, memberAccessRoleBudget+1), make([]entity.Role, memberAccessRoleBudget)
	for i := range assignments {
		assignments[i] = entity.UserRole{UserID: "usr_subject", RoleID: fmt.Sprintf("rol_%05d", i)}
		if i < len(rows) {
			rows[i] = entity.Role{ID: assignments[i].RoleID}
		}
	}
	if got := projectMemberAccessRoles("usr_subject", assignments[:memberAccessRoleBudget], rows); got.Status != "available" || len(got.Items) != 10000 {
		t.Fatal(got.Status, len(got.Items))
	}
	if got := projectMemberAccessRoles("usr_subject", assignments, nil); got.Status != "overflow" || got.Items != nil {
		t.Fatal(got)
	}
	members, teams := make([]entity.TeamMembership, memberAccessTeamBudget+1), make([]entity.Team, memberAccessTeamBudget)
	for i := range members {
		members[i] = entity.TeamMembership{ID: fmt.Sprintf("tmm_%04d", i), UserID: "usr_subject", TeamID: fmt.Sprintf("tea_%04d", i), Role: "member", Status: "active"}
		if i < len(teams) {
			teams[i] = entity.Team{ID: members[i].TeamID, Status: "active"}
		}
	}
	if got := projectMemberAccessTeams("usr_subject", members[:memberAccessTeamBudget], teams); got.Status != "available" || len(got.Items) != 1000 {
		t.Fatal(got.Status, len(got.Items))
	}
	if got := projectMemberAccessTeams("usr_subject", members, nil); got.Status != "overflow" || got.Items != nil {
		t.Fatal(got)
	}
}

func TestMemberAccessQueryIsolationExactnessBoundsAndSafeProjection(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			origin := db.Clauses(clause.Locking{Strength: "SHARE"})
			for _, test := range []struct {
				query  *gorm.DB
				model  any
				table  string
				budget int
			}{
				{memberAccessUserQuery(origin, "usr_subject"), &entity.User{}, "users", 0},
				{memberAccessRoleQuery(origin, "usr_subject"), &entity.UserRole{}, "user_roles", 10001},
				{memberAccessTeamQuery(origin, "usr_subject"), &entity.TeamMembership{}, "team_memberships", 1001},
			} {
				if err := test.query.Statement.Parse(test.model); err != nil {
					t.Fatal(err)
				}
				test.query.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "LIMIT", "FOR"}
				callbacks.BuildQuerySQL(test.query)
				sql := test.query.Statement.SQL.String()
				if !strings.Contains(sql, test.table) || len(test.query.Statement.Vars) == 0 || test.query.Statement.Vars[0] != "usr_subject" {
					t.Fatal(sql, test.query.Statement.Vars)
				}
				if test.budget != 0 {
					limit := test.query.Statement.Clauses["LIMIT"].Expression.(clause.Limit)
					if limit.Limit == nil || *limit.Limit != test.budget {
						t.Fatal(sql, limit)
					}
				}
				for _, private := range []string{"password_hash", "email", "last_login_at", "personal_grant_revision", "joined_at", "name_key"} {
					if strings.Contains(sql, private) {
						t.Fatal("private projection", sql)
					}
				}
				if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") != 2 {
					t.Fatal("inexact identity", sql)
				}
			}
			if origin.Statement.Model != nil || origin.Statement.Clauses["WHERE"].Expression != nil {
				t.Fatal("initialized query bleed")
			}
		})
	}
}

func TestMemberAccessInputAndPublicNullShape(t *testing.T) {
	svc := &Service{}
	for _, test := range []struct {
		actor, subject string
		expected       error
	}{
		{"bad actor", "usr_subject", apperrors.ErrUnauthorized},
		{"usr_actor", "bad subject", apperrors.ErrBadRequest},
	} {
		if _, err := svc.GetMemberAccessSummary(context.Background(), test.actor, test.subject); err != test.expected {
			t.Fatal(err)
		}
	}
	row := MemberAccessSummary{UserID: "usr_subject", ObservedAt: time.Date(2026, 10, 5, 0, 0, 0, 1, time.UTC), IdentityRole: "member", Roles: MemberAccessRoles{Status: "not_authorized"}, Teams: MemberAccessTeams{Status: "available", Items: []MemberAccessTeam{}}}
	raw, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 || string(fields["updated_at"]) != "null" {
		t.Fatal(string(raw))
	}
	for _, name := range []string{"roles", "teams"} {
		var section map[string]json.RawMessage
		if json.Unmarshal(fields[name], &section) != nil || len(section) != 2 {
			t.Fatal(string(raw))
		}
	}
	if !reflect.DeepEqual(string(fields["roles"]), `{"status":"not_authorized","items":null}`) || string(fields["teams"]) != `{"status":"available","items":[]}` {
		t.Fatal(string(raw))
	}
}
