package service

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
)

func TestMemberListFilterBoundsAndLegacyCursor(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("x", 31), "usr_alias ", "usr/alias", "usr_目标"} {
		if _, err := (&Service{}).ListMemberSummaries(context.Background(), value, MemberFilter{}); err != apperrors.ErrUnauthorized {
			t.Fatal(value, err)
		}
	}
	for _, f := range []MemberFilter{{Limit: -1}, {Limit: 101}, {Query: strings.Repeat("a", 201)}, {Query: "\xff"}, {Status: "offboarded"}, {Role: "Admin"}, {Cursor: "user "}, {Cursor: strings.Repeat("a", 31)}} {
		if _, err := normalizeMemberListFilter("legacyActor", f); err != apperrors.ErrBadRequest {
			t.Fatal(f, err)
		}
	}
	f, err := normalizeMemberListFilter("legacyActor", MemberFilter{Cursor: "Z_legacy-9", Query: "%_!字"})
	if err != nil || f.Limit != 40 || f.Cursor != "Z_legacy-9" {
		t.Fatal(f, err)
	}
}
func TestMemberListQueriesProjectSafeFieldsAndUseByteKeyset(t *testing.T) {
	for _, dialect := range []gorm.Dialector{projectQuotaScopeDialector{}, personalLifecycleMySQLDialector{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			if err != nil {
				t.Fatal(err)
			}
			q := memberListUserQuery(db, MemberFilter{Limit: 100, Cursor: "Z_legacy", Query: "%_!", Role: "member"})
			if err := q.Statement.Parse(&entity.User{}); err != nil {
				t.Fatal(err)
			}
			q.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "ORDER BY", "LIMIT"}
			callbacks.BuildQuerySQL(q)
			sql := q.Statement.SQL.String()
			for _, field := range []string{"password_hash", "personal_grant_revision", "SELECT *", "sessions", "token_hash"} {
				if strings.Contains(sql, field) {
					t.Fatal(sql)
				}
			}
			if !strings.Contains(sql, "ORDER BY") || !strings.Contains(sql, "LIMIT") || !reflect.DeepEqual(q.Statement.Vars, []any{"%!%!_!!%", "%!%!_!!%", "member", "Z_legacy", 101}) {
				t.Fatal(sql, q.Statement.Vars)
			}
			if dialect.Name() == "mysql" && !strings.Contains(sql, "AS BINARY") || dialect.Name() == "postgres" && !strings.Contains(sql, `COLLATE "C"`) {
				t.Fatal(sql)
			}
			count := memberListKeyCountQuery(db, []string{"usr_A", "usr_b"})
			count.Statement.BuildClauses = []string{"SELECT", "FROM", "WHERE", "GROUP BY"}
			callbacks.BuildQuerySQL(count)
			sql = count.Statement.SQL.String()
			if !strings.Contains(sql, "api_keys AS retained_key") || strings.Contains(sql, "api_keys AS key") || !strings.Contains(sql, "COUNT(*)") || !strings.Contains(sql, "GROUP BY") || !strings.Contains(sql, "subject") || !reflect.DeepEqual(count.Statement.Vars, []any{"usr_A", "usr_b"}) {
				t.Fatal(sql, count.Statement.Vars)
			}
			for _, field := range []string{"project_keys", "status", "prefix", "token_hash", "expires_at"} {
				if strings.Contains(sql, field) {
					t.Fatal("retained count filtered or leaked", sql)
				}
			}
			if dialect.Name() == "mysql" && strings.Count(sql, "AS BINARY") != 6 {
				t.Fatal("count owner join is not exact", sql)
			}
		})
	}
}
func TestMemberListRoleBudgetPreservesCompleteCompatibility(t *testing.T) {
	ids := make([]string, 100)
	rows := make([]entity.UserRole, 0, 10000)
	for i := range ids {
		ids[i] = fmt.Sprintf("usr_%03d", i)
		for j := 0; j < 100; j++ {
			rows = append(rows, entity.UserRole{UserID: ids[i], RoleID: fmt.Sprintf("rol_%03d", j)})
		}
	}
	roles, err := memberListRoles(ids, rows)
	if err != nil || len(roles) != 100 {
		t.Fatal(err)
	}
	for _, id := range ids {
		if len(roles[id]) != 100 {
			t.Fatal(id)
		}
	}
	rows = append(rows, entity.UserRole{UserID: ids[0], RoleID: "rol_extra"})
	if _, err := memberListRoles(ids, rows); err != memberListRoleOverflow {
		t.Fatal("page budget silently truncated", err)
	}
	if roles, err := memberListRoles(ids[:1], append(rows[:100], entity.UserRole{UserID: ids[0], RoleID: "rol_historical"})); err != nil || len(roles[ids[0]]) != 101 {
		t.Fatal("historical role cardinality silently truncated", err)
	}
	for _, bad := range [][]entity.UserRole{{{UserID: "USR_000", RoleID: "rol_one"}}, {{UserID: ids[0], RoleID: "rol_one"}, {UserID: ids[0], RoleID: "rol_one"}}, {{UserID: ids[0], RoleID: "rol_one "}}} {
		if _, err := memberListRoles(ids, bad); err != apperrors.ErrInternal {
			t.Fatal(bad, err)
		}
	}
}
func TestMemberListCountsRemainExactAndExcludeAliases(t *testing.T) {
	counts, err := memberListCounts([]string{"usr_a", "usr_b"}, []memberListKeyCount{{UserID: "usr_a", Count: math.MaxInt64}})
	if err != nil || counts["usr_a"] != "9223372036854775807" || counts["usr_b"] != "0" {
		t.Fatal(counts, err)
	}
	for _, rows := range [][]memberListKeyCount{{{UserID: "USR_A", Count: 1}}, {{UserID: "usr_a", Count: -1}}, {{UserID: "usr_a", Count: 1}, {UserID: "usr_a", Count: 2}}} {
		if _, err := memberListCounts([]string{"usr_a"}, rows); err != apperrors.ErrInternal {
			t.Fatal(rows, err)
		}
	}
}
func TestMemberListTeamSummaryIntegrityAndSeparateOverflow(t *testing.T) {
	ids := []string{"usr_a", "usr_b"}
	memberships := []entity.TeamMembership{{ID: "tmm_a", UserID: "usr_a", TeamID: "tea_a", Role: "member", Status: "disabled"}}
	teams := []entity.Team{{ID: "tea_a", Name: "Retained", Status: "archived"}}
	out, err := memberListTeamProjection(ids, memberships, teams)
	if err != nil || out["usr_b"].Status != "available" || out["usr_b"].Items == nil || len(out["usr_a"].Items) != 1 || out["usr_a"].Items[0].Status != "archived" || out["usr_a"].Items[0].MembershipStatus != "disabled" {
		t.Fatal(out, err)
	}
	for _, name := range []string{"missing", "alias_user", "alias_team", "duplicate_relation", "duplicate_team", "invalid_role", "invalid_status"} {
		t.Run(name, func(t *testing.T) {
			m := append([]entity.TeamMembership{}, memberships...)
			ts := append([]entity.Team{}, teams...)
			switch name {
			case "missing":
				ts = nil
			case "alias_user":
				m[0].UserID = "USR_A"
			case "alias_team":
				ts[0].ID = "TEA_A"
			case "duplicate_relation":
				m = append(m, m[0])
			case "duplicate_team":
				ts = append(ts, ts[0])
			case "invalid_role":
				m[0].Role = "Owner"
			case "invalid_status":
				m[0].Status = "active "
			}
			if _, err := memberListTeamProjection(ids, m, ts); err != apperrors.ErrInternal {
				t.Fatal(name, err)
			}
		})
	}
	many := make([]entity.TeamMembership, 1001)
	out, err = memberListTeamProjection(ids, many, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if out[id].Status != "overflow" || out[id].Items != nil {
			t.Fatal(out)
		}
	}
}
