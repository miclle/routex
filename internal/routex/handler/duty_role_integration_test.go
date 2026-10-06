package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

var dutyFixtureGrants = map[string][]string{
	"rol_procurement": {"models.read_all", "prices.read", "providers.read"},
	"rol_finance":     {"prices.read", "providers.read", "teams.money.write"},
	"rol_operations":  {"calls.read_all", "egress.read", "egress.test", "egress.write", "models.read_all", "prices.read", "providers.read", "providers.write", "system.read", "system.write", "teams.tokens.write"},
}

func TestDutyRoleFixtureUsesImplementedAssignablePermissions(t *testing.T) {
	for roleID, grants := range dutyFixtureGrants {
		if !slices.IsSorted(grants) || len(grants) == 0 {
			t.Fatal("unsorted/empty duty set", roleID)
		}
		for _, permission := range grants {
			if !slices.Contains(service.AvailablePermissions, permission) || !slices.Contains(service.AssignablePermissions(), permission) {
				t.Fatal("unimplemented/protected duty permission", roleID, permission)
			}
		}
	}
}

func dutyFixtureRows(t *testing.T, db *gorm.DB) ([]entity.Role, []entity.RolePermission) {
	t.Helper()
	var rows []entity.Role
	if err := db.Where("id IN ?", []string{"rol_finance", "rol_operations", "rol_procurement"}).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	var permissions []entity.RolePermission
	if err := db.Where("role_id IN ?", []string{"rol_finance", "rol_operations", "rol_procurement"}).Order("role_id, permission").Find(&permissions).Error; err != nil {
		t.Fatal(err)
	}
	return rows, permissions
}

func dutyFixtureEqualRoles(left, right []entity.Role) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return false
		}
		a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
		if !reflect.DeepEqual(a, b) {
			return false
		}
	}
	return true
}

// Root registers V68 and the two ordered scenarios. No SQL seed helper, native
// request, foreign-key bypass or harness change is supplied by this fixture.
func testDutyRoleMigration(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var ledger []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &ledger).Error; err != nil {
		t.Fatal(err)
	}
	if len(ledger) != 68 || ledger[67] != 68 {
		t.Fatal("V68 must be registered after the exact V67 prefix")
	}
	roles, grants := dutyFixtureRows(t, db)
	if len(roles) != 3 || len(grants) != 17 {
		t.Fatal("duty seed cardinality")
	}
	for _, row := range roles {
		if !row.Builtin || row.CreatedAt.IsZero() || len(row.DefinitionRevision) != 64 || row.Description == "" || row.NameKey == secret.SHA256Hex(row.Name) {
			t.Fatal("duty seed provenance", row.ID)
		}
		var actual []string
		for _, grant := range grants {
			if grant.RoleID == row.ID {
				actual = append(actual, grant.Permission)
			}
		}
		if !slices.Equal(actual, dutyFixtureGrants[row.ID]) {
			t.Fatal("unexpected duty grant", row.ID, actual)
		}
	}
	var assignments int64
	if err := db.Model(&entity.UserRole{}).Count(&assignments).Error; err != nil || assignments != 0 {
		t.Fatal("seeding created assignments", err)
	}
	// Same display names remain independent exact-name custom roles, including
	// immutable unrelated permissions; namespace uniqueness must not merge them.
	custom := entity.Role{ID: "rol_duty_history", Name: "Finance", NameKey: secret.SHA256Hex("Finance"), Description: "Retained custom Finance", DefinitionRevision: strings.Repeat("a", 64), CreatedAt: time.Now().UTC().Truncate(time.Millisecond)}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: custom.ID, Permission: "Retained.Mixed_CASE"}).Error; err != nil {
		t.Fatal(err)
	}
	var savedCustom entity.Role
	if err := db.Take(&savedCustom, "id = ?", custom.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Retain a real fixture identity and its existing custom assignment before
	// any duty seed is present in the reconstructed V67 database.
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := svc.Initialize(ctx, "duty-history@example.invalid", "duty-history-password", "Retained duty identity")
	if err != nil {
		t.Fatal(err)
	}
	customAssignment := entity.UserRole{UserID: auth.User.ID, RoleID: custom.ID}
	if err := db.Create(&customAssignment).Error; err != nil {
		t.Fatal(err)
	}
	var savedAssignments []entity.UserRole
	if err := db.Order("user_id, role_id").Find(&savedAssignments).Error; err != nil || !reflect.DeepEqual(savedAssignments, []entity.UserRole{customAssignment}) {
		t.Fatal("retained custom assignment setup", err)
	}
	checkRetained := func() {
		t.Helper()
		var retainedAssignments []entity.UserRole
		if err := db.Order("user_id, role_id").Find(&retainedAssignments).Error; err != nil || !reflect.DeepEqual(retainedAssignments, savedAssignments) {
			t.Fatal("duty migration changed retained assignments", err)
		}
		var retained entity.Role
		if err := db.Take(&retained, "id = ?", custom.ID).Error; err != nil || !dutyFixtureEqualRoles([]entity.Role{retained}, []entity.Role{savedCustom}) {
			t.Fatal("custom collision changed history", err)
		}
		var p []string
		if err := db.Model(&entity.RolePermission{}).Where("role_id = ?", custom.ID).Pluck("permission", &p).Error; err != nil || !slices.Equal(p, []string{"Retained.Mixed_CASE"}) {
			t.Fatal("custom grants changed", err)
		}
	}
	check := func() {
		t.Helper()
		nowRoles, nowGrants := dutyFixtureRows(t, db)
		if !dutyFixtureEqualRoles(nowRoles, roles) || !reflect.DeepEqual(nowGrants, grants) {
			t.Fatal("duty replay changed immutable rows")
		}
		checkRetained()
	}
	removeLedger := func() {
		t.Helper()
		q := db.Table("schema_migrations").Where("version = ?", 68).Delete(&struct{}{})
		if q.Error != nil || q.RowsAffected != 1 {
			t.Fatal("ledger removal", q.Error)
		}
	}
	migrate := func() {
		t.Helper()
		if err := database.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
		var got []int
		if err := db.Table("schema_migrations").Order("version").Pluck("version", &got).Error; err != nil || !slices.Equal(got, ledger) {
			t.Fatal("ledger changed", err)
		}
	}
	reject := func() {
		t.Helper()
		var beforeRoles, afterRoles []entity.Role
		var beforeGrants, afterGrants []entity.RolePermission
		if err := db.Order("id").Find(&beforeRoles).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Order("role_id, permission").Find(&beforeGrants).Error; err != nil {
			t.Fatal(err)
		}
		if err := database.Migrate(ctx, db); err == nil || err.Error() != "migration 68: incompatible retained duty role seed" {
			t.Fatal("incompatible seed adopted", err)
		}
		if err := db.Order("id").Find(&afterRoles).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Order("role_id, permission").Find(&afterGrants).Error; err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", 68).Count(&count).Error; err != nil || count != 0 || !dutyFixtureEqualRoles(beforeRoles, afterRoles) || !reflect.DeepEqual(beforeGrants, afterGrants) {
			t.Fatal("rejected seed changed retained rows or ledger", err)
		}
	}
	// Reconstruct an existing V67 database with same-name custom history before
	// the first duty seed. Remove only this fixture's three rows and 17 grants.
	removeLedger()
	seedIDs := []string{"rol_finance", "rol_operations", "rol_procurement"}
	if q := db.Where("role_id IN ?", seedIDs).Delete(&entity.RolePermission{}); q.Error != nil || q.RowsAffected != 17 {
		t.Fatal("V67 duty grant removal", q.Error)
	}
	if q := db.Where("id IN ?", seedIDs).Delete(&entity.Role{}); q.Error != nil || q.RowsAffected != 3 {
		t.Fatal("V67 duty row removal", q.Error)
	}
	checkRetained()
	missingRoles, missingGrants := dutyFixtureRows(t, db)
	var beforeUpgrade []int
	if err := db.Table("schema_migrations").Order("version").Pluck("version", &beforeUpgrade).Error; err != nil || !slices.Equal(beforeUpgrade, ledger[:67]) || len(missingRoles) != 0 || len(missingGrants) != 0 {
		t.Fatal("existing V67 upgrade baseline", err)
	}
	seedStarted := time.Now().UTC().Truncate(time.Millisecond)
	migrate()
	roles, grants = dutyFixtureRows(t, db) // Newly inserted rows are the replay baseline.
	seedFinished := time.Now().UTC()
	if len(roles) != 3 || len(grants) != 17 {
		t.Fatal("V67 upgrade duty seed cardinality")
	}
	for _, row := range roles {
		birth := row.CreatedAt.UTC()
		if !row.Builtin || birth.IsZero() || birth.Before(seedStarted) || birth.After(seedFinished) || birth.Nanosecond()%int(time.Millisecond) != 0 || len(row.DefinitionRevision) != 64 || row.Description == "" || row.NameKey == secret.SHA256Hex(row.Name) {
			t.Fatal("V67 upgrade new duty provenance", row.ID)
		}
		var actual []string
		for _, grant := range grants {
			if grant.RoleID == row.ID {
				actual = append(actual, grant.Permission)
			}
		}
		if !slices.Equal(actual, dutyFixtureGrants[row.ID]) {
			t.Fatal("V67 upgrade unexpected duty grant", row.ID, actual)
		}
	}
	check()
	migrate()
	check()
	removeLedger()
	if err := db.Where("role_id = ? AND permission = ?", "rol_finance", "teams.money.write").Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	check() // Compatible partial data resumes without rewriting births.
	removeLedger()
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errorsOut <- database.Migrate(ctx, db) })
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal("concurrent seed", err)
		}
	}
	check()
	for _, field := range []string{"Name", "NameKey", "Description", "Builtin", "DefinitionRevision"} {
		removeLedger()
		value := any("incompatible")
		if field == "Builtin" {
			value = false
		}
		if field == "DefinitionRevision" || field == "NameKey" {
			value = strings.Repeat("b", 64)
		}
		if err := db.Model(&entity.Role{}).Where("id = ?", "rol_finance").UpdateColumn(field, value).Error; err != nil {
			t.Fatal(err)
		}
		reject()
		var original entity.Role
		for _, row := range roles {
			if row.ID == "rol_finance" {
				original = row
			}
		}
		if err := db.Model(&entity.Role{}).Where("id = ?", original.ID).Select(field).Updates(&original).Error; err != nil {
			t.Fatal(err)
		}
		migrate()
		check()
	}
	removeLedger()
	extra := entity.RolePermission{RoleID: "rol_finance", Permission: "roles.write"}
	if err := db.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	reject()
	if err := db.Delete(&extra).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	check()
	// Replace an exact seed with a case alias, so both drivers exercise the same
	// incompatible retained identity without relying on collation-specific insert.
	removeLedger()
	var finance entity.Role
	for _, row := range roles {
		if row.ID == "rol_finance" {
			finance = row
		}
	}
	if err := db.Where("role_id = ?", finance.ID).Delete(&entity.RolePermission{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&finance).Error; err != nil {
		t.Fatal(err)
	}
	alias := finance
	alias.ID = "ROL_FINANCE"
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	reject()
	if err := db.Delete(&alias).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&finance).Error; err != nil {
		t.Fatal(err)
	}
	migrate()
	check()
}

func testDutyRoles(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var failAudit atomic.Bool
	const callback = "test:duty-role-audit"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.roles.update" && failAudit.Load() {
			_ = tx.AddError(errors.New("controlled duty assignment audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	// Registration/removal is outside the publisher lifetime.
	t.Cleanup(func() {
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	router, svc := memberStateRuntimeFixtureRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"duty-admin@example.invalid","password":"duty-test-password","name":"Duty administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, false, "Enable duty fixture registration")
	registered := identityRequest(router, "POST", "/api/v1/auth/register", `{"email":"duty-member@example.invalid","password":"duty-test-password","name":"Duty member"}`, nil, "")
	expectStatus(t, registered, 201)
	member, memberCookie := readIdentity(t, registered)
	custom, err := svc.SaveRole(ctx, admin.User.ID, "", "Independent duty fixture custom", []string{"roles.read"})
	if err != nil {
		t.Fatal(err)
	}
	assign := func(ids []string, cookie *http.Cookie, csrf string) {
		t.Helper()
		expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, cookie, csrf, member.User.ID, ids), 200)
	}
	ids := []string{"rol_finance", "rol_operations", custom.Role.ID}
	assign(ids, adminCookie, admin.CSRFToken)
	workspaceResponse := identityRequest(router, "GET", "/api/v1/admin/members/"+member.User.ID+"/roles", "", adminCookie, "")
	expectStatus(t, workspaceResponse, 200)
	var workspace struct {
		BuiltinRole struct {
			ID             string `json:"id"`
			AssignmentKind string `json:"assignment_kind"`
		} `json:"builtin_role"`
		AssignedRoles []struct {
			ID             string `json:"id"`
			Builtin        bool   `json:"builtin"`
			AssignmentKind string `json:"assignment_kind"`
		} `json:"assigned_roles"`
	}
	if err := json.Unmarshal(workspaceResponse.Body.Bytes(), &workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.BuiltinRole.ID != "rol_member" || workspace.BuiltinRole.AssignmentKind != "intrinsic" || len(workspace.AssignedRoles) != 3 {
		t.Fatal("intrinsic identity/complete explicit rows changed")
	}
	for _, row := range workspace.AssignedRoles {
		if row.AssignmentKind != "explicit" || row.Builtin != (row.ID != custom.Role.ID) {
			t.Fatal("duty immutability mistaken for intrinsic assignment", row.ID)
		}
	}
	permissions, err := svc.Permissions(ctx, member.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := append(slices.Clone(dutyFixtureGrants["rol_finance"]), dutyFixtureGrants["rol_operations"]...)
	want = append(want, "roles.read")
	slices.Sort(want)
	want = slices.Compact(want)
	if !slices.Equal(permissions, want) {
		t.Fatal("duty union overgranted or lost custom authority", permissions, want)
	}
	for _, p := range []string{"roles.write", "registration.write", "members.approvals.write", "members.write", "prices.write", "models.write", "audit.read", "limits.users.write", "projects.limits.write", "secrets.read", "storage.write", "smtp.write"} {
		if slices.Contains(permissions, p) {
			t.Fatal("protected or unsupported duty power", p)
		}
	}
	// Immutable definitions do not confer definition-write or assignment rights.
	for _, id := range []string{"rol_finance", "rol_operations", "rol_procurement"} {
		expectStatus(t, identityRequest(router, "DELETE", "/api/v1/admin/roles/"+id, "", adminCookie, admin.CSRFToken), 403)
		if _, err := svc.SaveRole(ctx, admin.User.ID, id, "Changed duty", []string{"prices.read"}); err == nil {
			t.Fatal("duty definition editable")
		}
	}
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, memberCookie, member.CSRFToken, member.User.ID, []string{"rol_procurement"}), 403)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/admin/runtime/publish", `{}`, memberCookie, member.CSRFToken), 403)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/audit", "", memberCookie, ""), 403)
	var original entity.User
	if err := db.Take(&original, "id = ?", member.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	failAudit.Store(true)
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, adminCookie, admin.CSRFToken, member.User.ID, []string{"rol_procurement"}), 500)
	failAudit.Store(false)
	var after entity.User
	if err := db.Take(&after, "id = ?", member.User.ID).Error; err != nil || after.MemberRoleRevision != original.MemberRoleRevision {
		t.Fatal("failed audit changed assignment revision", err)
	}
	var retained []string
	if err := db.Model(&entity.UserRole{}).Where("user_id = ?", member.User.ID).Order("role_id").Pluck("role_id", &retained).Error; err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if !slices.Equal(retained, ids) {
		t.Fatal("failed audit changed assignment")
	}
	assign([]string{"rol_operations", custom.Role.ID}, adminCookie, admin.CSRFToken)
	permissions, err = svc.Permissions(ctx, member.User.ID)
	if err != nil || slices.Contains(permissions, "teams.money.write") || !slices.Contains(permissions, "prices.read") || !slices.Contains(permissions, "providers.read") {
		t.Fatal("removal lost shared grants or retained exclusive money", err)
	}
	var countBefore, countAfter int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "member.roles.update").Count(&countBefore).Error; err != nil {
		t.Fatal(err)
	}
	assign([]string{"rol_operations", custom.Role.ID}, adminCookie, admin.CSRFToken)
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "member.roles.update").Count(&countAfter).Error; err != nil || countAfter != countBefore {
		t.Fatal("current equality wrote another audit", err)
	}
	// Read-only source facts remain separate from runtime and intrinsic identity.
	response := identityRequest(router, "GET", "/api/v1/admin/roles", "", adminCookie, "")
	expectStatus(t, response, 200)
	var list struct {
		Items []struct {
			ID             string `json:"id"`
			Builtin        bool   `json:"builtin"`
			AssignmentKind string `json:"assignment_kind"`
			MemberCount    *int64 `json:"member_count"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	seenDuties := 0
	for _, row := range list.Items {
		if _, ok := dutyFixtureGrants[row.ID]; ok {
			seenDuties++
			expected := int64(0)
			if row.ID == "rol_operations" {
				expected = 1
			}
			if !row.Builtin || row.AssignmentKind != "explicit" || row.MemberCount == nil || *row.MemberCount != expected {
				t.Fatal("duty classification/retained count", row.ID)
			}
		}
	}
	if seenDuties != 3 {
		t.Fatal("incomplete duty list")
	}
	// Inactive retained assignments still count, but confer no current authority.
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("Disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Permissions(ctx, member.User.ID); err == nil {
		t.Fatal("disabled duty member retained active authority")
	}
	retainedList, err := svc.ListRoles(ctx, admin.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range retainedList {
		if row.Role.ID == "rol_operations" && (row.MemberCount == nil || *row.MemberCount != 1) {
			t.Fatal("disabled retained assignment disappeared from count")
		}
	}
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumn("Disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	offboarded := time.Now().UTC().Truncate(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumns(map[string]any{"Disabled": true, "OffboardedAt": &offboarded}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Permissions(ctx, member.User.ID); err == nil {
		t.Fatal("offboarded duty member retained active authority")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", member.User.ID).UpdateColumns(map[string]any{"Disabled": false, "OffboardedAt": nil}).Error; err != nil {
		t.Fatal(err)
	}
	// Team-held duties project none of their global powers or dimension writes.
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Duty scoped Team", "Team-only role projection", []string{member.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	teamReview, err := svc.GetTeamRoles(ctx, admin.User.ID, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	teamResult, err := svc.SetTeamRoles(ctx, admin.User.ID, team.ID, teamReview.ETag, service.TeamRoleInput{RoleIDs: []string{"rol_finance", "rol_operations", "rol_procurement"}, Reason: "Verify scoped duty projection"})
	if err != nil || len(teamResult.EffectiveTeamActions) != 0 {
		t.Fatal("Team duty projection expanded global actions", err)
	}
	assign([]string{}, adminCookie, admin.CSRFToken)
	permissions, err = svc.Permissions(ctx, member.User.ID)
	if err != nil || len(permissions) != 0 {
		t.Fatal("Team holdings leaked global duty powers", permissions, err)
	}
	teamMemberReview, err := svc.GetTeamRoles(ctx, member.User.ID, team.ID)
	if err != nil || len(teamMemberReview.EffectiveTeamActions) != 0 || len(teamMemberReview.ActorTeamActions) != 0 {
		t.Fatal("Team member inherited duty action", err)
	}
	var native int64
	if err := db.Model(&entity.CallRecord{}).Count(&native).Error; err != nil || native != 0 {
		t.Fatal("duty fixture invoked native", err)
	}
	var modelGrants int64
	if err := db.Model(&entity.UserModelGrant{}).Where("user_id = ?", member.User.ID).Count(&modelGrants).Error; err != nil || modelGrants != 0 {
		t.Fatal("duty assignment granted inference", err)
	}
}
