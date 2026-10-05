package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Root registers this source-prepared fixture on the real supported drivers.
// It performs no native requests and does not synthesize runtime or usage facts.
func testMemberAccessSummaryLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	type queryMarker struct{}
	var queryMu sync.Mutex
	var queries []string
	const callback = "test:member-access-summary-query"
	observe := func(query *gorm.DB) {
		if query.Statement.Context.Value(queryMarker{}) == true {
			queryMu.Lock()
			queries = append(queries, query.Statement.SQL.String())
			queryMu.Unlock()
		}
	}
	if err := db.Callback().Query().After("gorm:query").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Row().After("gorm:row").Register(callback, observe); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = db.Callback().Query().Remove(callback)
		_ = db.Callback().Row().Remove(callback)
	}()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"access-admin@example.invalid","password":"access-test-password","name":"Access administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-subject", nil)
	reader, readerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-reader", []string{"members.read"})
	roleReader, roleCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-role-reader", []string{"members.read", "roles.read"})
	teamReader, teamCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-team-reader", []string{"members.read", "teams.read_all"})
	fullReader, fullCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-full-reader", []string{"members.read", "roles.read", "teams.read_all"})
	_, writerCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-write-only", []string{"members.write", "roles.read", "teams.read_all"})
	path := func(userID string) string { return "/api/v1/admin/members/" + userID + "/access" }
	get := func(cookie *http.Cookie, userID string) service.MemberAccessSummary {
		t.Helper()
		response := identityRequest(router, "GET", path(userID), "", cookie, "")
		expectStatus(t, response, 200)
		var result service.MemberAccessSummary
		var fields map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &result) != nil || json.Unmarshal(response.Body.Bytes(), &fields) != nil || len(fields) != 6 || result.UserID != userID || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC || response.Header().Get("Cache-Control") != "private, no-store" || response.Header().Get("ETag") != "" {
			t.Fatal(response.Body.String(), response.Header())
		}
		return result
	}
	role, err := svc.SaveRole(ctx, admin.User.ID, "", "Access selected role", []string{"prices.read"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{role.Role.ID}); err != nil {
		t.Fatal(err)
	}
	teams := []entity.Team{
		{ID: "tea_access_active", Name: "Active retained", Status: "active"},
		{ID: "tea_access_disabled", Name: "Disabled retained", Status: "disabled"},
		{ID: "tea_access_archived", Name: "Archived retained", Status: "archived"},
	}
	members := []entity.TeamMembership{
		{ID: "tmm_access_active", TeamID: teams[0].ID, UserID: subject.User.ID, Role: "owner", Status: "active"},
		{ID: "tmm_access_disabled", TeamID: teams[1].ID, UserID: subject.User.ID, Role: "member", Status: "disabled"},
		{ID: "tmm_access_archived", TeamID: teams[2].ID, UserID: subject.User.ID, Role: "member", Status: "active"},
	}
	if err := db.Create(&teams).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatal(err)
	}
	private := get(readerCookie, subject.User.ID)
	if private.IdentityRole != "member" || private.UpdatedAt == nil || private.Roles.Status != "not_authorized" || private.Roles.Items != nil || private.Teams.Status != "not_authorized" || private.Teams.Items != nil {
		t.Fatal(private)
	}
	roles := get(roleCookie, subject.User.ID)
	if roles.Roles.Status != "available" || len(roles.Roles.Items) != 1 || roles.Roles.Items[0].ID != role.Role.ID || roles.Roles.Items[0].Name != role.Role.Name || roles.Teams.Items != nil {
		t.Fatal(roles)
	}
	teamView := get(teamCookie, subject.User.ID)
	if teamView.Roles.Items != nil || teamView.Teams.Status != "available" || len(teamView.Teams.Items) != 3 {
		t.Fatal(teamView)
	}
	for _, row := range teamView.Teams.Items {
		index := slices.IndexFunc(teams, func(team entity.Team) bool { return team.ID == row.ID })
		if index < 0 || row.Name != teams[index].Name || row.Status != teams[index].Status || row.MembershipStatus != members[index].Status || row.MembershipRole != members[index].Role {
			t.Fatal(row)
		}
	}
	get(fullCookie, subject.User.ID)
	get(adminCookie, subject.User.ID)
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID), "", writerCookie, ""), 403)
	expectStatus(t, identityRequest(router, "GET", path(strings.ToUpper(subject.User.ID)), "", readerCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"?team=tea_access_active", "", readerCookie, ""), 400)
	expectStatus(t, identityRequest(router, "GET", path("usr_access_missing"), "", readerCookie, ""), 404)

	marked := context.WithValue(ctx, queryMarker{}, true)
	measure := func(actorID, userID string) (service.MemberAccessSummary, []string) {
		t.Helper()
		queryMu.Lock()
		queries = nil
		queryMu.Unlock()
		value, err := svc.GetMemberAccessSummary(marked, actorID, userID)
		if err != nil {
			t.Fatal(err)
		}
		queryMu.Lock()
		captured := slices.Clone(queries)
		queryMu.Unlock()
		if len(captured) > 9 {
			t.Fatal("unbounded read count", len(captured), captured)
		}
		for _, sql := range captured {
			for _, forbidden := range []string{"call_records", "call_attempts", "resource_limits", "user_model_grants", "provider_models", "api_keys"} {
				if strings.Contains(sql, forbidden) {
					t.Fatal("unrelated directory or accounting read", sql)
				}
			}
		}
		return *value, captured
	}
	_, small := measure(fullReader.User.ID, subject.User.ID)
	if len(small) != 9 {
		t.Fatal("unexpected complete read plan", len(small), small)
	}
	_, denied := measure(reader.User.ID, subject.User.ID)
	if len(denied) != 5 {
		t.Fatal("unauthorized metadata read", len(denied), denied)
	}
	_, roleQueries := measure(roleReader.User.ID, subject.User.ID)
	_, teamQueries := measure(teamReader.User.ID, subject.User.ID)
	if len(roleQueries) != 7 || len(teamQueries) != 7 {
		t.Fatal(len(roleQueries), len(teamQueries))
	}
	if _, err := svc.GetMemberAccessSummary(ctx, strings.ToUpper(reader.User.ID), subject.User.ID); err == nil {
		t.Fatal("actor alias acquired authority")
	}

	// Selected roles may retain more than the write-picker's current 100 limit.
	moreRoles := make([]entity.Role, 101)
	moreAssignments := make([]entity.UserRole, len(moreRoles))
	for i := range moreRoles {
		name := fmt.Sprintf("Access retained %03d", i)
		moreRoles[i] = entity.Role{ID: fmt.Sprintf("rol_access_%03d", i), Name: name, NameKey: secret.SHA256Hex(name)}
		moreAssignments[i] = entity.UserRole{UserID: subject.User.ID, RoleID: moreRoles[i].ID}
	}
	if err := db.CreateInBatches(&moreRoles, 100).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&moreAssignments, 100).Error; err != nil {
		t.Fatal(err)
	}
	large, largeQueries := measure(fullReader.User.ID, subject.User.ID)
	if large.Roles.Status != "available" || len(large.Roles.Items) != 102 || len(largeQueries) != len(small) {
		t.Fatal(large.Roles.Status, len(large.Roles.Items), len(largeQueries))
	}
	// A retained case-alias reference must not borrow a differently named row.
	alias := entity.UserRole{UserID: subject.User.ID, RoleID: strings.ToUpper(role.Role.ID)}
	if err := db.Create(&alias).Error; err == nil {
		aliasView := get(fullCookie, subject.User.ID)
		if aliasView.Roles.Status != "unavailable" || aliasView.Roles.Items != nil || aliasView.Teams.Status != "available" {
			t.Fatal("aliased role was partially hydrated", aliasView)
		}
		if err := db.Where(database.ExactText(db, clause.Column{Name: "user_id"}, alias.UserID)).Where(database.ExactText(db, clause.Column{Name: "role_id"}, alias.RoleID)).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
	} else {
		t.Log("driver rejected retained alias reference through its existing constraint")
	}

	// Preserve historical text rather than applying current write normalization.
	if err := db.Model(&entity.Role{}).Where("id = ?", role.Role.ID).Update("name", "\n retained role ").Error; err != nil {
		t.Fatal(err)
	}
	if got := get(fullCookie, subject.User.ID); got.Roles.Status != "available" || !slices.ContainsFunc(got.Roles.Items, func(row service.MemberAccessRole) bool {
		return row.ID == role.Role.ID && row.Name == "\n retained role "
	}) {
		t.Fatal(got)
	}
	snapshot := func() map[string][]map[string]any {
		t.Helper()
		result := map[string][]map[string]any{}
		for _, table := range []string{"users", "user_roles", "roles", "role_permissions", "teams", "team_memberships", "user_model_grants", "team_model_grants", "api_keys", "api_key_models", "resource_limits", "audit_events", "call_records", "call_attempts"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(rows, func(a, b map[string]any) int {
				left, _ := json.Marshal(a)
				right, _ := json.Marshal(b)
				return strings.Compare(string(left), string(right))
			})
			result[table] = rows
		}
		return result
	}
	before := snapshot()
	get(fullCookie, subject.User.ID)
	get(readerCookie, subject.User.ID)
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		t.Fatal("read-only Access changed retained identities, policy, audit or call facts")
	}
	// Retained relationships exceed contemporary write-picker limits. Test each
	// full-summary budget independently and ensure overflow never hydrates it.
	overflowSubject, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "access-overflow", nil)
	retainedRoles := make([]entity.Role, 10001)
	retainedAssignments := make([]entity.UserRole, len(retainedRoles))
	for i := range retainedRoles {
		name := fmt.Sprintf("Access bounded role %05d", i)
		retainedRoles[i] = entity.Role{ID: fmt.Sprintf("rol_access_bound_%05d", i), Name: name, NameKey: secret.SHA256Hex(name)}
		retainedAssignments[i] = entity.UserRole{UserID: overflowSubject.User.ID, RoleID: retainedRoles[i].ID}
	}
	if err := db.CreateInBatches(&retainedRoles, 250).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&retainedAssignments, 250).Error; err != nil {
		t.Fatal(err)
	}
	roleOverflow, boundedRoleQueries := measure(fullReader.User.ID, overflowSubject.User.ID)
	if roleOverflow.Roles.Status != "overflow" || roleOverflow.Roles.Items != nil || roleOverflow.Teams.Status != "available" || roleOverflow.Teams.Items == nil || len(boundedRoleQueries) != 7 {
		t.Fatal(roleOverflow, boundedRoleQueries)
	}
	retainedTeams := make([]entity.Team, 1001)
	retainedMemberships := make([]entity.TeamMembership, len(retainedTeams))
	for i := range retainedTeams {
		teamID := fmt.Sprintf("tea_access_bound_%04d", i)
		retainedTeams[i] = entity.Team{ID: teamID, Name: "Retained bounded Team", Status: "active"}
		retainedMemberships[i] = entity.TeamMembership{ID: fmt.Sprintf("tmm_access_bound_%04d", i), UserID: overflowSubject.User.ID, TeamID: teamID, Role: "member", Status: "active"}
	}
	if err := db.CreateInBatches(&retainedTeams, 250).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.CreateInBatches(&retainedMemberships, 250).Error; err != nil {
		t.Fatal(err)
	}
	bothOverflow, boundedBothQueries := measure(fullReader.User.ID, overflowSubject.User.ID)
	if bothOverflow.Roles.Status != "overflow" || bothOverflow.Roles.Items != nil || bothOverflow.Teams.Status != "overflow" || bothOverflow.Teams.Items != nil || len(boundedBothQueries) != 7 {
		t.Fatal(bothOverflow, boundedBothQueries)
	}
	privateOverflow := get(readerCookie, overflowSubject.User.ID)
	if privateOverflow.Roles.Status != "not_authorized" || privateOverflow.Teams.Status != "not_authorized" || privateOverflow.Roles.Items != nil || privateOverflow.Teams.Items != nil {
		t.Fatal("overflow disclosed private cardinality", privateOverflow)
	}
	for _, lifecycle := range []map[string]any{{"disabled": true}, {"offboarded_at": time.Now().UTC()}} {
		if err := db.Model(&entity.User{}).Where("id = ?", subject.User.ID).Updates(lifecycle).Error; err != nil {
			t.Fatal(err)
		}
		retained := get(fullCookie, subject.User.ID)
		if retained.IdentityRole != "member" || retained.Roles.Status != "available" || retained.Teams.Status != "available" {
			t.Fatal("inactive subject lost retained context", retained)
		}
	}
	if err := db.Model(&entity.User{}).Where("id = ?", reader.User.ID).Update("disabled", true).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetMemberAccessSummary(ctx, reader.User.ID, subject.User.ID); err == nil {
		t.Fatal("inactive actor retained authority")
	}
}
