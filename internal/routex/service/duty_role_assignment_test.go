package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func dutyRolesForTest() []entity.Role {
	return []entity.Role{
		{ID: "rol_procurement", Name: "Procurement", Builtin: true, DefinitionRevision: memberRoleBaseline},
		{ID: "rol_finance", Name: "Finance", Builtin: true, DefinitionRevision: memberRoleBaseline},
		{ID: "rol_operations", Name: "Operations", Builtin: true, DefinitionRevision: memberRoleBaseline},
	}
}
func assertAssignmentKind(t *testing.T, value any, want string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["assignment_kind"] != want {
		t.Fatalf("missing authoritative assignment kind %s: %s", want, raw)
	}
}
func TestDutyRoleAssignmentProjectionAndRevocation(t *testing.T) {
	actor := entity.User{ID: "usr_admin", Role: entity.RoleAdmin, MemberRoleRevision: memberRoleBaseline}
	subject := entity.User{ID: "usr_target", Role: entity.RoleMember, MemberRoleRevision: memberRoleBaseline}
	defs := map[string]memberRoleDefinition{}
	for _, id := range []string{"rol_admin", "rol_member"} {
		d, err := projectMemberRoleDefinition(entity.Role{ID: id, Name: id, Builtin: true, DefinitionRevision: memberRoleBaseline}, []string{})
		if err != nil {
			t.Fatal(err)
		}
		defs[id] = d
	}
	for _, role := range dutyRolesForTest() {
		permissions := []string{"providers.read"}
		if role.ID == "rol_finance" {
			permissions = append(permissions, "teams.money.write")
		}
		if role.ID == "rol_operations" {
			permissions = append(permissions, "teams.tokens.write")
		}
		d, err := projectMemberRoleDefinition(role, permissions)
		if err != nil {
			t.Fatal(err)
		}
		defs[role.ID] = d
		assertAssignmentKind(t, d.Summary, "explicit")
	}
	p, err := projectMemberRoles(actor, subject, []string{"rol_finance", "rol_operations"}, []string{"rol_finance", "rol_operations", "rol_procurement"}, defs, false)
	if err != nil || !p.Page.CanEdit {
		t.Fatal("supported duties cannot be assigned", err)
	}
	if !slices.Equal(p.Page.EffectivePermissions, []string{"providers.read", "teams.money.write", "teams.tokens.write"}) {
		t.Fatal("duty union", p.Page.EffectivePermissions)
	}
	assertAssignmentKind(t, p.Page.BuiltinRole, "intrinsic")
	input := MemberRolesInput{RoleIDs: []string{"rol_operations"}, RoleDefinitions: []MemberRoleDefinitionProof{{"rol_operations", defs["rol_operations"].Summary.DefinitionETag}}, BuiltinDefinitionETag: p.Page.BuiltinRole.DefinitionETag, Reason: "Reviewed duty removal"}
	if err = memberRolesReview(p, p.Page.ETag, input); err != nil {
		t.Fatal("explicit duty cannot be removed", err)
	}
	after, err := projectMemberRoles(actor, subject, input.RoleIDs, p.Catalogue, defs, false)
	if err != nil || !slices.Equal(after.Page.EffectivePermissions, []string{"providers.read", "teams.tokens.write"}) {
		t.Fatal("removal destroyed shared or retained exclusive grants", err)
	}
	subject.Disabled = true
	p, err = projectMemberRoles(actor, subject, input.RoleIDs, []string{}, defs, false)
	if err != nil || p.Page.PermissionUse != "inactive" || len(p.Page.AssignedRoles) != 1 {
		t.Fatal("retained inactive duty metadata", err)
	}
}
func TestDutyRoleReservedIdentityFlagsFailClosed(t *testing.T) {
	for _, id := range []string{"rol_admin", "rol_member", "rol_procurement", "rol_finance", "rol_operations"} {
		t.Run(id, func(t *testing.T) {
			role := entity.Role{ID: id, Name: "Retained", DefinitionRevision: memberRoleBaseline}
			if _, err := projectMemberRoleDefinition(role, []string{}); err == nil {
				t.Fatal("reserved identity with corrupt builtin false accepted")
			}
		})
	}
	for _, id := range []string{"rol_unknown", "rol_FINANCE", "rol_finance "} {
		t.Run(id, func(t *testing.T) {
			role := entity.Role{ID: id, Name: "Retained", Builtin: true, DefinitionRevision: memberRoleBaseline}
			if _, err := projectMemberRoleDefinition(role, []string{}); err == nil {
				t.Fatal("unknown or aliased builtin accepted")
			}
		})
	}
}
func TestDutyRoleListRetainedCountsAndQueryBudget(t *testing.T) {
	s, f, _ := roleListSQLService(t, 0)
	for _, role := range dutyRolesForTest() {
		f.data.roles[role.ID] = role
		f.data.permissions[role.ID] = []string{"providers.read"}
	}
	inactive := f.data.users["usr_target"]
	inactive.Disabled = true
	f.data.users[inactive.ID] = inactive
	f.data.assignments = []entity.UserRole{{UserID: inactive.ID, RoleID: "rol_finance"}, {UserID: inactive.ID, RoleID: "rol_operations"}}
	rows, err := s.ListRoles(context.Background(), "usr_admin")
	if err != nil || len(rows) != 5 {
		t.Fatal("duties absent from role catalogue", len(rows), err)
	}
	if len(f.queries) != 6 {
		t.Fatal("new statements instead of two grouped counts", len(f.queries))
	}
	for _, row := range rows {
		if row.MemberCount == nil {
			t.Fatal("missing count")
		}
		want := int64(1)
		if row.Role.ID == "rol_procurement" {
			want = 0
		}
		if *row.MemberCount != want {
			t.Fatal("retained global count", row.Role.ID, *row.MemberCount, want)
		}
	}
	for _, q := range f.queries {
		if strings.Contains(strings.ToLower(q), "team_") {
			t.Fatal("Team scope borrowed for platform duty count", q)
		}
	}
}

func TestDutyRoleReviewedSQLAssignmentAndAuditRollback(t *testing.T) {
	for _, fault := range []bool{false, true} {
		t.Run(fmt.Sprint(fault), func(t *testing.T) {
			s, f := roleSQLService(t, 0)
			for _, r := range dutyRolesForTest() {
				f.data.roles[r.ID] = r
				f.data.permissions[r.ID] = []string{"providers.read"}
			}
			snapshot, err := s.readMemberRoles(context.Background(), "usr_admin", "usr_target")
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Catalogue) != 3 {
				t.Fatal("complete duty candidates", snapshot.Catalogue)
			}
			input := roleInputForTest(snapshot, "rol_finance", "rol_operations")
			f.failAudit = fault
			result, err := s.SetReviewedMemberRoles(context.Background(), "usr_admin", "usr_target", snapshot.Page.ETag, input)
			if fault {
				if result != nil || err != apperrors.ErrInternal || len(f.data.assignments) != 0 || len(f.data.audits) != 0 {
					t.Fatal("duty assignment escaped audit rollback", result, err)
				}
				return
			}
			if err != nil || !slices.Equal(result.RoleIDs, input.RoleIDs) || result.Confirmation != "current_member_roles" || result.Effect != "current_database" || len(f.data.audits) != 1 {
				t.Fatal("reviewed complete assignment", result, err)
			}
			if f.data.users["usr_target"].Role != entity.RoleMember {
				t.Fatal("duty assignment changed intrinsic identity")
			}
			again, err := s.SetReviewedMemberRoles(context.Background(), "usr_admin", "usr_target", snapshot.Page.ETag, input)
			if err != nil || again == nil || len(f.data.audits) != 1 {
				t.Fatal("current identical retry duplicated audit", err)
			}
			after, err := s.readMemberRoles(context.Background(), "usr_admin", "usr_target")
			if err != nil {
				t.Fatal(err)
			}
			removed := roleInputForTest(after, "rol_operations")
			if _, err = s.SetReviewedMemberRoles(context.Background(), "usr_admin", "usr_target", after.Page.ETag, removed); err != nil || len(f.data.assignments) != 1 || f.data.assignments[0].RoleID != "rol_operations" || len(f.data.audits) != 2 {
				t.Fatal("explicit duty cannot be revoked", err)
			}
			if _, err = s.SetReviewedMemberRoles(context.Background(), "usr_target", "usr_target", after.Page.ETag, removed); err != apperrors.ErrForbidden {
				t.Fatal("duty role obtained administrator assignment authority", err)
			}
		})
	}
}
func TestDutyRoleListExistingCatalogueBounds(t *testing.T) {
	for _, total := range []int{500, 501, 1000, 1001} {
		t.Run(fmt.Sprint(total), func(t *testing.T) {
			s, f, _ := roleListSQLService(t, total-5)
			for _, r := range dutyRolesForTest() {
				f.data.roles[r.ID] = r
				f.data.permissions[r.ID] = []string{"providers.read"}
			}
			rows, err := s.ListRoles(context.Background(), "usr_admin")
			if total == 1001 {
				if rows != nil || err != roleListOverflow {
					t.Fatal("catalogue overflow silently truncated", len(rows), err)
				}
				return
			}
			want := 5 + (total+memberRolesBatchSize-1)/memberRolesBatchSize
			if err != nil || len(rows) != total || len(f.queries) != want {
				t.Fatal("duties changed catalogue completeness/query budget", len(rows), len(f.queries), want, err)
			}
		})
	}
}

func TestDutyRoleReservedCorruptionCannotEditOrDelete(t *testing.T) {
	for _, id := range []string{"rol_admin", "rol_member", "rol_procurement", "rol_finance", "rol_operations"} {
		t.Run(id, func(t *testing.T) {
			s, f, _ := roleDefinitionSQLService(t)
			f.data.roles[id] = entity.Role{ID: id, Name: "Retained reserved identity", DefinitionRevision: memberRoleBaseline}
			f.data.permissions[id] = []string{}
			for _, action := range []string{"save", "delete"} {
				f.writes = nil
				var err error
				if action == "save" {
					_, err = s.SaveRole(context.Background(), "usr_admin", id, "Changed", []string{})
				} else {
					err = s.DeleteRole(context.Background(), "usr_admin", id)
				}
				if err == nil || len(f.writes) != 0 || f.data.roles[id].Name != "Retained reserved identity" {
					t.Fatal("corrupt reserved flag allowed trusted metadata mutation", action, err, f.writes)
				}
			}
		})
	}
}
func TestDutyRoleCustomNameDoesNotBecomeTemplate(t *testing.T) {
	role := entity.Role{ID: "rol_custom_finance", Name: "Finance", DefinitionRevision: memberRoleBaseline}
	d, err := projectMemberRoleDefinition(role, []string{"members.read"})
	if err != nil || d.Summary.Builtin {
		t.Fatal("same-name custom changed classification", err)
	}
	assertAssignmentKind(t, d.Summary, "explicit")
	if slices.Contains(d.Permissions, "teams.money.write") {
		t.Fatal("duty grants inferred from name")
	}
}

func TestDutyRoleExplicitSQLScopeUsesBoundExactIDs(t *testing.T) {
	for _, f := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{"postgres", postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"})},
		{"mysql", mysql.New(mysql.Config{DSN: "test:test@tcp(localhost:3306)/test", SkipInitializeWithVersion: true})},
	} {
		t.Run(f.name, func(t *testing.T) {
			db, err := gorm.Open(f.dialector, &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			pool, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pool.Close() })
			q := db.Table("roles AS selected_role").Where(explicitRoleScope(db, "selected_role")).Find(&[]entity.Role{})
			if q.Error != nil || !reflect.DeepEqual(q.Statement.Vars, []any{false, true, "rol_procurement", "rol_finance", "rol_operations"}) {
				t.Fatal("finite scope bindings", q.Error, q.Statement.Vars)
			}
			sql := q.Statement.SQL.String()
			if !strings.Contains(sql, " OR ") || !strings.Contains(sql, " AND ") {
				t.Fatal("builtin duties not separately constrained", sql)
			}
			if f.name == "mysql" && strings.Count(sql, "CAST(`selected_role`.`id` AS BINARY)") != 3 {
				t.Fatal("collation aliases acquire duty eligibility", sql)
			}
			for _, id := range []string{"rol_procurement", "rol_finance", "rol_operations"} {
				if strings.Contains(sql, id) {
					t.Fatal("duty identity escaped parameters", sql)
				}
			}
		})
	}
}
