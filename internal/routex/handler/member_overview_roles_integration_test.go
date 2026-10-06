package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

// Exercises self-only current Role labels on both supported database drivers.
func testMemberOverviewRolesLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		svc.StopRuntime()
		if err := svc.StopCallRecorder(); err != nil {
			t.Error(err)
		}
	})
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"home-roles-admin@example.invalid","password":"home-roles-test-password","name":"Home Roles administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	member, cookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "home-self-labels", nil)
	other, otherCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "home-other-labels", nil)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, member.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	read := func(query string, c *http.Cookie) service.MemberOverviewRolesPage {
		t.Helper()
		res := identityRequest(router, "GET", "/api/v1/overview/roles"+query, "", c, "")
		expectStatus(t, res, 200)
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("self labels cacheable")
		}
		var raw map[string]json.RawMessage
		if json.Unmarshal(res.Body.Bytes(), &raw) != nil || len(raw) != 5 {
			t.Fatal("self page leaked or omitted facts")
		}
		var page service.MemberOverviewRolesPage
		if json.Unmarshal(res.Body.Bytes(), &page) != nil || page.ObservedAt.IsZero() || page.IdentityRole != "member" || page.Roles == nil {
			t.Fatal("incomplete self identity")
		}
		var labels []map[string]json.RawMessage
		if json.Unmarshal(raw["roles"], &labels) != nil {
			t.Fatal("invalid labels")
		}
		for _, label := range labels {
			if len(label) != 4 {
				t.Fatal("extra private Role facts")
			}
		}
		return page
	}
	page := read("", cookie)
	if page.ActorUserID != member.User.ID || len(page.Roles) != 0 || page.NextCursor != nil {
		t.Fatal("intrinsic/duty seeds became implicit assignments", page)
	}
	for _, query := range []string{"?user_id=" + other.User.ID, "?limit=51", "?limit=01", "?cursor=bad", "?limit=1&limit=2"} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles"+query, "", cookie, ""), 400)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/roles", "", cookie, ""), 403)
	custom, err := svc.SaveRole(ctx, admin.User.ID, "", "Home direct custom", []string{})
	if err != nil {
		t.Fatal(err)
	}
	selected := []string{custom.Role.ID, "rol_finance"}
	slices.Sort(selected)
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, member.User.ID, selected); err != nil {
		t.Fatal(err)
	}
	page = read("?limit=1", cookie)
	if len(page.Roles) != 1 || page.Roles[0].ID != selected[0] || page.NextCursor == nil {
		t.Fatal("first exact assignment page changed", page)
	}
	firstCursor := *page.NextCursor
	next := read("?limit=1&cursor="+firstCursor, cookie)
	if len(next.Roles) != 1 || next.Roles[0].ID != selected[1] || next.NextCursor != nil {
		t.Fatal("terminal page duplicated/invented", next)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles?cursor="+firstCursor, "", otherCookie, ""), 400)
	ownOther := read("", otherCookie)
	for _, row := range ownOther.Roles {
		if slices.Contains(selected, row.ID) {
			t.Fatal("other actor received self labels")
		}
	}
	for _, row := range read("", cookie).Roles {
		if row.AssignmentKind != service.RoleAssignmentExplicit {
			t.Fatal("intrinsic conflated with explicit")
		}
	}
	// Legacy unusable retained names are unknown; they do not become invented labels.
	if err := db.Model(&entity.Role{}).Where("id = ?", custom.Role.ID).UpdateColumn("Name", "").Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range read("", cookie).Roles {
		if row.ID == custom.Role.ID && row.Name != nil {
			t.Fatal("unrecorded label invented")
		}
	}
	if err := db.Model(&entity.Role{}).Where("id = ?", custom.Role.ID).UpdateColumn("Name", custom.Role.Name).Error; err != nil {
		t.Fatal(err)
	}
	// Exact metadata matching must reject collation aliases instead of borrowing a row.
	var originalRole entity.Role
	if err := db.Where("id = ?", custom.Role.ID).First(&originalRole).Error; err != nil {
		t.Fatal(err)
	}
	aliasID := strings.ToUpper(custom.Role.ID)
	swapHomeRoleLabelFixtureIdentity(t, db, member.User.ID, originalRole.ID, aliasID)
	var aliasedRole entity.Role
	if err := db.Where("id = ?", aliasID).First(&aliasedRole).Error; err != nil || aliasedRole.ID != aliasID || !sameHomeRoleLabelFixtureMetadata(aliasedRole, originalRole) {
		t.Fatal("physical alias metadata changed", err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles", "", cookie, ""), 503)
	swapHomeRoleLabelFixtureIdentity(t, db, member.User.ID, aliasID, originalRole.ID)
	var restoredRole entity.Role
	if err := db.Where("id = ?", originalRole.ID).First(&restoredRole).Error; err != nil || restoredRole.ID != originalRole.ID || !sameHomeRoleLabelFixtureMetadata(restoredRole, originalRole) {
		t.Fatal("original Role not restored exactly", err)
	}
	read("", cookie)
	// Current admission is independent of Session survival and never inferred from a partial User.
	var saved entity.User
	if err := db.Where("id = ?", member.User.ID).First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	appID, err := id.NewPrefixed("raa")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	reason := "Controlled self-label admission"
	app := entity.RegistrationApprovalApplication{ID: appID, UserID: saved.ID, UserCreatedAt: saved.CreatedAt, CreatedAt: now, Revision: strings.Repeat("a", 64), State: "approved", DecidedAt: &now, DecisionActorID: &admin.User.ID, DecisionReason: &reason}
	if err := db.Create(&app).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("ApprovalApplicationID", appID).Error; err != nil {
		t.Fatal(err)
	}
	read("", cookie)
	// Millisecond precision is portable; prove the persisted identity really changed.
	changedBirth := saved.CreatedAt.Add(time.Millisecond)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("CreatedAt", changedBirth).Error; err != nil {
		t.Fatal(err)
	}
	var changed entity.User
	if err := db.Where("id = ?", saved.ID).First(&changed).Error; err != nil || changed.CreatedAt.Equal(saved.CreatedAt) {
		t.Fatal("birth corruption not persisted", err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles", "", cookie, ""), 401)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("CreatedAt", saved.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	read("", cookie)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("Disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles", "", cookie, ""), 401)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("Disabled", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("OffboardedAt", now).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles", "", cookie, ""), 401)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("OffboardedAt", nil).Error; err != nil {
		t.Fatal(err)
	}
	read("", cookie)
	// Capture only this request context; the background publisher cannot inflate or
	// hide the original fixed self-read query budget.
	type marker struct{}
	queryCtx := context.WithValue(ctx, marker{}, true)
	var mu sync.Mutex
	var queries []string
	callback := "home_roles_read"
	observe := func(q *gorm.DB) {
		if q.Statement.Context.Value(marker{}) != nil {
			mu.Lock()
			queries = append(queries, q.Statement.SQL.String())
			mu.Unlock()
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	defer func() { svc.StopRuntime(); _ = svc.StopCallRecorder(); _ = db.Callback().Query().Remove(callback) }()
	mu.Lock()
	queries = nil
	mu.Unlock()
	managedStarted := time.Now()
	managedPage, managedErr := svc.MemberOverviewRoles(queryCtx, saved.ID, service.MemberOverviewRolesFilter{Limit: 50})
	managedElapsed := time.Since(managedStarted)
	mu.Lock()
	managedQueries := append([]string(nil), queries...)
	mu.Unlock()
	if managedErr != nil || managedPage == nil || len(managedPage.Roles) != 2 || len(managedQueries) != 4 || managedElapsed > 5*time.Second {
		t.Fatal("managed self snapshot query/admission budget changed", managedErr, len(managedQueries), managedElapsed)
	}
	t.Logf("self Role snapshot: managed queries=%d elapsed=%s", len(managedQueries), managedElapsed)
	if err := db.Model(&entity.User{}).Where("id = ?", saved.ID).UpdateColumn("ApprovalApplicationID", nil).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", saved.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	birth := time.Now().UTC().Truncate(time.Millisecond)
	roles := make([]entity.Role, 10001)
	assignments := make([]entity.UserRole, 10001)
	for i := range roles {
		name := fmt.Sprintf("Home retained %05d", i)
		digest := sha256.Sum256([]byte(name))
		rid := fmt.Sprintf("rol_home_%05d", i)
		roles[i] = entity.Role{ID: rid, Name: name, NameKey: hex.EncodeToString(digest[:]), CreatedAt: birth, DefinitionRevision: strings.Repeat("b", 64)}
		assignments[i] = entity.UserRole{UserID: saved.ID, RoleID: rid}
	}
	if err := db.CreateInBatches(roles, 500).Error; err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{500, 501, 1000, 10000, 10001} {
		if err := db.Where("user_id = ?", saved.ID).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.CreateInBatches(assignments[:count], 500).Error; err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		queries = nil
		mu.Unlock()
		started := time.Now()
		got, err := svc.MemberOverviewRoles(queryCtx, saved.ID, service.MemberOverviewRolesFilter{Limit: 50})
		elapsed := time.Since(started)
		mu.Lock()
		captured := append([]string(nil), queries...)
		mu.Unlock()
		if elapsed > 5*time.Second {
			t.Fatal("self snapshot exceeded unchanged five-second bound", count, elapsed)
		}
		t.Logf("self Role snapshot: retained=%d queries=%d elapsed=%s", count, len(captured), elapsed)
		if count == 10001 {
			if err == nil || got != nil || len(captured) != 2 {
				t.Fatal("overflow exposed partial labels", count, err, len(captured))
			}
			continue
		}
		if err != nil || len(got.Roles) != 50 || len(captured) != 3 || got.NextCursor == nil {
			t.Fatal("retained query budget changed", count, err, len(captured))
		}
		for _, q := range captured {
			if strings.Contains(q, "role_permissions") || strings.Contains(q, "team_memberships") {
				t.Fatal("directory/permission expansion")
			}
		}
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/overview/roles?user_id="+saved.ID, "", adminCookie, ""), 400)
}

// Swap only a fixture-owned empty-permission custom Role and its sole assignment.
// The delete/rename/insert order preserves released FKs on both drivers without
// disabling constraints or relying on case-insensitive referential matching.
func swapHomeRoleLabelFixtureIdentity(t *testing.T, db *gorm.DB, userID, fromID, toID string) {
	t.Helper()
	if err := db.Transaction(func(tx *gorm.DB) error {
		var row entity.UserRole
		if err := tx.Where("user_id = ? AND role_id = ?", userID, fromID).First(&row).Error; err != nil {
			return err
		}
		if row.UserID != userID || row.RoleID != fromID {
			return fmt.Errorf("fixture assignment identity was not exact")
		}
		var grants, assignments int64
		if err := tx.Model(&entity.RolePermission{}).Where("role_id = ?", fromID).Count(&grants).Error; err != nil {
			return err
		}
		if err := tx.Model(&entity.UserRole{}).Where("role_id = ?", fromID).Count(&assignments).Error; err != nil {
			return err
		}
		if grants != 0 || assignments != 1 {
			return fmt.Errorf("fixture Role must retain no permissions and one assignment")
		}
		result := tx.Where("user_id = ? AND role_id = ?", userID, fromID).Delete(&entity.UserRole{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("fixture assignment deletion was not exact")
		}
		result = tx.Model(&entity.Role{}).Where("id = ?", fromID).UpdateColumn("ID", toID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("fixture Role identity replacement was not exact")
		}
		return tx.Create(&entity.UserRole{UserID: userID, RoleID: toID}).Error
	}); err != nil {
		t.Fatal(err)
	}
	var row entity.UserRole
	if err := db.Where("user_id = ? AND role_id = ?", userID, toID).First(&row).Error; err != nil || row.UserID != userID || row.RoleID != toID {
		t.Fatal("physical assignment identity was not retained", err)
	}
}
func sameHomeRoleLabelFixtureMetadata(a, b entity.Role) bool {
	return a.Name == b.Name && a.NameKey == b.NameKey && a.Description == b.Description && a.Builtin == b.Builtin && a.DefinitionRevision == b.DefinitionRevision && a.CreatedAt.Equal(b.CreatedAt)
}
