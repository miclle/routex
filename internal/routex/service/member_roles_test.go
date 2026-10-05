package service

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func roleSnapshotForTest(t *testing.T, actor, subject entity.User, assigned []string) *memberRolesSnapshot {
	t.Helper()
	definitions := map[string]memberRoleDefinition{}
	for _, role := range []entity.Role{{ID: "rol_member", Name: "Member", Builtin: true, DefinitionRevision: memberRoleBaseline}, {ID: "rol_admin", Name: "Administrator", Builtin: true, DefinitionRevision: memberRoleBaseline}, {ID: "rol_reader", Name: "Reader", DefinitionRevision: memberRoleBaseline}, {ID: "rol_writer", Name: "Writer", DefinitionRevision: memberRoleBaseline}} {
		d, err := projectMemberRoleDefinition(role, []string{"members.read"})
		if err != nil {
			t.Fatal(err)
		}
		definitions[role.ID] = d
	}
	snapshot, err := projectMemberRoles(actor, subject, assigned, []string{"rol_reader", "rol_writer"}, definitions, false)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func roleInputForTest(snapshot *memberRolesSnapshot, ids ...string) MemberRolesInput {
	input := MemberRolesInput{RoleIDs: slices.Clone(ids), RoleDefinitions: []MemberRoleDefinitionProof{}, BuiltinDefinitionETag: snapshot.Page.BuiltinRole.DefinitionETag, Reason: "Reviewed role assignment"}
	if input.RoleIDs == nil {
		input.RoleIDs = []string{}
	}
	for _, id := range ids {
		input.RoleDefinitions = append(input.RoleDefinitions, MemberRoleDefinitionProof{id, snapshot.Definitions[id].Summary.DefinitionETag})
	}
	return input
}
func TestMemberRolesStrictImmutableInput(t *testing.T) {
	base := MemberRolesInput{RoleIDs: []string{"rol_reader"}, RoleDefinitions: []MemberRoleDefinitionProof{{"rol_reader", strings.Repeat("a", 64)}}, BuiltinDefinitionETag: strings.Repeat("b", 64), Reason: "Reviewed 😀"}
	raw, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	var got MemberRolesInput
	if err := json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(got, base) {
		t.Fatal(err, got)
	}
	for _, change := range []func(MemberRolesInput) MemberRolesInput{
		func(v MemberRolesInput) MemberRolesInput { v.RoleIDs = nil; return v },
		func(v MemberRolesInput) MemberRolesInput { v.RoleDefinitions = nil; return v },
		func(v MemberRolesInput) MemberRolesInput { v.Reason = " R"; return v },
		func(v MemberRolesInput) MemberRolesInput { v.Reason = strings.Repeat("😀", 257); return v },
		func(v MemberRolesInput) MemberRolesInput { v.BuiltinDefinitionETag = strings.Repeat("A", 64); return v },
		func(v MemberRolesInput) MemberRolesInput { v.RoleIDs = []string{"rol_admin"}; return v },
	} {
		v := change(base)
		r, _ := json.Marshal(v)
		if json.Unmarshal(r, &got) == nil {
			t.Fatal(string(r))
		}
	}
	for _, bad := range []string{"null", "[]", "{}", string(raw) + " {}", strings.Replace(string(raw), `"reason":`, `"reason":"First","reason":`, 1), strings.Replace(string(raw), `"reason":`, `"\u0072eason":"First","reason":`, 1), strings.Replace(string(raw), `"id":"rol_reader"`, `"id":"rol_reader","id":"rol_writer"`, 1), strings.Replace(string(raw), `"id":"rol_reader"`, `"ID":"rol_reader"`, 1), strings.Replace(string(raw), `Reviewed 😀`, `\ud800`, 1), string(append([]byte(`{"reason":"`), 0xff))} {
		if json.Unmarshal([]byte(bad), &got) == nil {
			t.Fatal(bad)
		}
	}
	base.RoleIDs = []string{}
	base.RoleDefinitions = []MemberRoleDefinitionProof{}
	r, _ := json.Marshal(base)
	if json.Unmarshal(r, &got) != nil {
		t.Fatal("clear rejected")
	}
}
func TestMemberRolesRevisionABAAndCurrentDefinitionReconciliation(t *testing.T) {
	now := time.Now().UTC()
	actor := entity.User{ID: "usr_admin", Role: entity.RoleAdmin, CreatedAt: now}
	subject := entity.User{ID: "usr_target", Role: entity.RoleMember, CreatedAt: now, MemberRoleRevision: memberRoleBaseline}
	old := roleSnapshotForTest(t, actor, subject, []string{"rol_reader"})
	desired := roleInputForTest(old, "rol_writer")
	if err := memberRolesReview(old, old.Page.ETag, desired); err != nil {
		t.Fatal(err)
	}
	subject.MemberRoleRevision = strings.Repeat("1", 64)
	changed := roleSnapshotForTest(t, actor, subject, []string{"rol_reader"})
	if changed.Page.ETag == old.Page.ETag || memberRolesReview(changed, old.Page.ETag, desired) != catalogConflict {
		t.Fatal("assignment ABA did not fence")
	}
	current := roleSnapshotForTest(t, actor, subject, []string{"rol_writer"})
	if err := memberRolesReview(current, old.Page.ETag, desired); err != nil {
		t.Fatal("no-write exact current confirmation rejected", err)
	}
	definition := current.Definitions["rol_writer"]
	r := entity.Role{ID: "rol_writer", Name: "Writer", DefinitionRevision: strings.Repeat("2", 64)}
	d, err := projectMemberRoleDefinition(r, definition.Permissions)
	if err != nil {
		t.Fatal(err)
	}
	current.Definitions["rol_writer"] = d
	if memberRolesReview(current, old.Page.ETag, desired) != catalogConflict {
		t.Fatal("matching IDs concealed definition ABA")
	}
	current = roleSnapshotForTest(t, actor, subject, []string{"rol_writer"})
	current.Page.BuiltinRole.DefinitionETag = strings.Repeat("c", 64)
	if memberRolesReview(current, old.Page.ETag, desired) != catalogConflict {
		t.Fatal("builtin changed")
	}
	for _, change := range []func(*entity.User){func(u *entity.User) { u.Role = entity.RoleAdmin }, func(u *entity.User) { u.Disabled = true }, func(u *entity.User) { u.OffboardedAt = &now }, func(u *entity.User) { u.CreatedAt = u.CreatedAt.Add(time.Microsecond) }} {
		u := subject
		change(&u)
		s := roleSnapshotForTest(t, actor, u, []string{"rol_reader"})
		if s.Page.ETag == changed.Page.ETag {
			t.Fatal("identity/lifecycle omitted")
		}
	}
}
func TestMemberRolesReadBoundsAndProjectionPrivacy(t *testing.T) {
	actor := entity.User{ID: "usr_admin", Role: entity.RoleAdmin}
	subject := entity.User{ID: "usr_target", Role: entity.RoleMember, MemberRoleRevision: memberRoleBaseline}
	snapshot := roleSnapshotForTest(t, actor, subject, []string{})
	rows := map[string]memberRoleDefinition{snapshot.Page.BuiltinRole.ID: snapshot.Definitions[snapshot.Page.BuiltinRole.ID]}
	ids := []string{}
	for i := range 10000 {
		id := fmt.Sprintf("rol_%05d", i)
		d, err := projectMemberRoleDefinition(entity.Role{ID: id, Name: "Retained\nlabel", DefinitionRevision: memberRoleBaseline}, []string{"members.read", "legacy.unknown"})
		if err != nil {
			t.Fatal(err)
		}
		rows[id] = d
		ids = append(ids, id)
	}
	full, err := projectMemberRoles(actor, subject, ids, nil, rows, false)
	if err != nil || len(full.Page.AssignedRoles) != 10000 || full.Page.CanEdit || !slices.Contains(full.Page.EditBlockers, "assignment_audit_bound") || !slices.Equal(full.Page.EffectivePermissions, []string{"members.read"}) {
		t.Fatal(err, full.Page.CanEdit)
	}
	if _, err := projectMemberRoles(actor, subject, append(ids, "rol_more"), nil, rows, false); err != memberRolesOverflow {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(full.Page)
	if strings.Contains(string(raw), `"permissions"`) || strings.Contains(string(raw), "legacy.unknown") || strings.Contains(string(raw), "revision") || strings.Contains(string(raw), "password") {
		t.Fatal("expanded/private metadata leaked")
	}
	subject.Disabled = true
	disabled := roleSnapshotForTest(t, actor, subject, []string{})
	if !disabled.Page.CanEdit || disabled.Page.PermissionUse != "inactive" {
		t.Fatal(disabled.Page)
	}
	now := time.Now()
	subject.OffboardedAt = &now
	offboarded := roleSnapshotForTest(t, actor, subject, []string{})
	if offboarded.Page.CanEdit || memberRolesReview(offboarded, offboarded.Page.ETag, roleInputForTest(offboarded)) != catalogConflict {
		t.Fatal(offboarded.Page)
	}
	actor.Role = entity.RoleMember
	subject.OffboardedAt = nil
	reader := roleSnapshotForTest(t, actor, subject, []string{})
	if reader.Page.CanEdit || reader.Page.CandidateStatus != "not_authorized" {
		t.Fatal(reader.Page)
	}
}
func TestMemberRolesTypedFullAuditBounds(t *testing.T) {
	before := memberRolesAuditValues{entity.RoleMember, []string{}, []string{"members.read"}}
	after := memberRolesAuditValues{entity.RoleMember, []string{"rol_reader"}, []string{"members.read"}}
	for i := range 1000 {
		before.RoleIDs = append(before.RoleIDs, fmt.Sprintf("rol_%026d", i))
	}
	raw, err := encodeMemberRolesAudit("usr_subject", strings.Repeat("r", 1024), before, after)
	if err != nil || len(raw) >= memberRolesAuditBudget {
		t.Fatal(err, len(raw))
	}
	row := entity.AuditEvent{Action: "member.roles.update", ResourceType: "user", ResourceID: "usr_subject", DetailsJSON: &raw}
	projected, ok := memberRolesAuditProjection(row)
	if !ok || !reflect.DeepEqual(projected.Before, before) {
		t.Fatal("full before not preserved")
	}
	for _, malformed := range []string{strings.Replace(raw, `"usr_subject"`, `"usr_other"`, 1), strings.Replace(raw, `"members.read"`, `"unknown.permission"`, 1), strings.Repeat(" ", memberRolesAuditBudget+1)} {
		row.DetailsJSON = &malformed
		if _, ok := memberRolesAuditProjection(row); ok {
			t.Fatal("malformed audit rendered")
		}
	}
	before.RoleIDs = make([]string, 10000)
	for i := range before.RoleIDs {
		before.RoleIDs[i] = fmt.Sprintf("rol_%026d", i)
	}
	if _, err := encodeMemberRolesAudit("usr_subject", "R", before, after); err != memberRolesOverflow {
		t.Fatal("unbounded write audit", err)
	}
	if memberRolesReview(&memberRolesSnapshot{Page: MemberRolesWorkspace{}}, strings.Repeat("a", 64), MemberRolesInput{}) != catalogConflict {
		t.Fatal("ineligible write")
	}
	if validateMemberRolesInput(MemberRolesInput{}) != apperrors.ErrBadRequest {
		t.Fatal("missing intent")
	}
}

func TestMemberRolesRecordedPermissionCodesPreserveCaseWithoutAuthority(t *testing.T) {
	role := entity.Role{ID: "rol_retained", Name: "Retained", DefinitionRevision: memberRoleBaseline}
	permissions := []string{"projects.models.WRITE", "projects.limits.WRITE", "projects.models.write", "LEGACY_Unknown"}
	definition, err := projectMemberRoleDefinition(role, permissions)
	if err != nil {
		t.Fatal("safe recorded case aliases blocked definition read", err)
	}
	want := slices.Clone(permissions)
	slices.Sort(want)
	if !slices.Equal(definition.Permissions, want) || definition.Summary.PermissionCount != len(want) || !slices.Equal(permissions, []string{"projects.models.WRITE", "projects.limits.WRITE", "projects.models.write", "LEGACY_Unknown"}) {
		t.Fatal("recorded identity/count or caller-owned permissions changed", definition)
	}
	definitions := map[string]memberRoleDefinition{role.ID: definition}
	if union := memberRolesUnion(definitions, []string{role.ID}); !slices.Equal(union, []string{"projects.models.write"}) {
		t.Fatal("unknown/case-alias permissions became active", union)
	}
	aliasOnly, err := projectMemberRoleDefinition(role, []string{"projects.models.WRITE", "projects.limits.WRITE"})
	if err != nil || len(memberRolesUnion(map[string]memberRoleDefinition{role.ID: aliasOnly}, []string{role.ID})) != 0 {
		t.Fatal("case aliases gained effective authority", err)
	}
	lower, err := projectMemberRoleDefinition(role, []string{"projects.models.write", "projects.limits.write"})
	if err != nil || lower.Summary.DefinitionETag == aliasOnly.Summary.DefinitionETag {
		t.Fatal("definition proof normalized recorded permission case", err)
	}
	if err := validateRoleInput("New role", []string{"projects.models.WRITE"}); err != apperrors.ErrBadRequest {
		t.Fatal("read compatibility changed new-role permission validation", err)
	}
}

func TestMemberRolesRecordedPermissionCodeSafety(t *testing.T) {
	role := entity.Role{ID: "rol_retained", Name: "Retained", DefinitionRevision: memberRoleBaseline}
	for _, code := range []string{"projects.models.WRITE", "projects.limits.WRITE", "LEGACY_Unknown-01", strings.Repeat("A", 80)} {
		if _, err := projectMemberRoleDefinition(role, []string{code}); err != nil {
			t.Fatalf("safe recorded code rejected %q: %v", code, err)
		}
	}
	for _, code := range []string{"", strings.Repeat("A", 81), "projects.models. WRITE", "projects.models.WRITE\n", "projects.models.WRITE\t", "projects.models.WRITE\x00", "projects/models/WRITE", "projects:models:WRITE", "项目.WRITE", "projects.models.ＷRITE"} {
		if _, err := projectMemberRoleDefinition(role, []string{code}); err != memberRolesUnavailable {
			t.Fatalf("unsafe recorded code accepted %q: %v", code, err)
		}
	}
	if _, err := projectMemberRoleDefinition(role, []string{"projects.models.WRITE", "projects.models.WRITE"}); err != memberRolesUnavailable {
		t.Fatal("exact recorded duplicate accepted", err)
	}
}

func TestMemberRolesUnrelatedRecordedAliasesDoNotBlockTrustedClear(t *testing.T) {
	s, fixture := roleSQLService(t, 1)
	fixture.data.roles["rol_initial_alias"] = entity.Role{ID: "rol_initial_alias", Name: "Alias permissions", DefinitionRevision: memberRoleBaseline}
	fixture.data.permissions["rol_initial_alias"] = []string{"projects.models.WRITE", "projects.limits.WRITE"}
	original := fixture.data.clone()
	page, err := s.GetMemberRoles(context.Background(), "usr_admin", "usr_target")
	if err != nil || len(page.AssignedRoles) != 1 || !page.CanEdit {
		t.Fatal("unrelated safe historical role poisoned reviewed workspace", page, err)
	}
	result, err := s.SetMemberRoles(context.Background(), "usr_admin", "usr_target", []string{})
	if err != nil || result == nil || len(result.RoleIDs) != 0 || len(fixture.data.assignments) != 0 || len(fixture.data.audits) != 1 {
		t.Fatal("unrelated case aliases blocked exact trusted clear", result, err)
	}
	if !reflect.DeepEqual(fixture.data.roles, original.roles) || !reflect.DeepEqual(fixture.data.permissions, original.permissions) || fixture.data.users["usr_target"].MemberRoleRevision == original.users["usr_target"].MemberRoleRevision || !fixture.data.users["usr_target"].UpdatedAt.Equal(original.users["usr_target"].UpdatedAt) || !reflect.DeepEqual(fixture.data.users["usr_admin"], original.users["usr_admin"]) {
		t.Fatal("clear changed unrelated recorded definitions or immutable user facts")
	}
}
