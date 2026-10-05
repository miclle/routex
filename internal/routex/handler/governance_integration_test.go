package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

// Invoked by the shared lifecycle owner against each fresh migrated database.
func testGovernanceLifecycle(t *testing.T, db *gorm.DB) {
	router, _ := memberStateRuntimeFixtureRouter(t, db)
	status := decodeCatalogResponse[RegistrationResponse](t, identityRequest(router, "GET", "/api/v1/auth/registration", "", nil, ""), 200)
	if status.Enabled {
		t.Fatal("registration must be disabled on a new database")
	}
	registrationBody := `{"email":"registered@example.com","password":"governance-password","name":"Registered member","role":"admin"}`
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/register", registrationBody, nil, ""), 403)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"governance@example.com","password":"governance-password","name":"Governance admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), adminCookie, admin.CSRFToken)
	}
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, adminCookie, admin.CSRFToken, admin.User.ID, map[string]any{"disabled": true}), 409)
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, adminCookie, admin.CSRFToken, admin.User.ID, map[string]any{"role": "member"}), 409)
	policyReview := approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken)
	expectStatus(t, approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true, "approval_required": false, "reason": "Verify registration CSRF denial"}, adminCookie, "", policyReview.ReviewETag), 403)
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, false, "Enable immediate registration fixture")
	registered := identityRequest(router, "POST", "/api/v1/auth/register", registrationBody, nil, "")
	expectStatus(t, registered, 201)
	member, memberCookie := readIdentity(t, registered)
	if member.User.Role != "member" {
		t.Fatal("public registration accepted an administrator role")
	}
	var grants int64
	if err := db.Model(&entity.UserModelGrant{}).Where("user_id = ?", member.User.ID).Count(&grants).Error; err != nil || grants != 0 {
		t.Fatal("public registration implicitly granted models")
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/register", registrationBody, nil, ""), 409)
	memberRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), memberCookie, member.CSRFToken)
	}
	expectStatus(t, memberRequest("GET", "/api/v1/admin/members", nil), 403)
	policyReview = approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken)
	expectStatus(t, approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": false, "approval_required": false, "reason": "Verify registration member denial"}, memberCookie, member.CSRFToken, policyReview.ReviewETag), 403)
	roles := decodeCatalogResponse[RolesResponse](t, request("GET", "/api/v1/admin/roles", nil), 200)
	if len(roles.Items) != 2 || len(roles.AvailablePermissions) == 0 {
		t.Fatal("builtin roles or permission allowlist missing")
	}
	adminRoleReview := roleDefinitionFixtureReview(t, router, adminCookie, "rol_admin")
	adminRoleIdentity := strings.Repeat("a", 64)
	if adminRoleReview.IdentityETag != nil {
		adminRoleIdentity = *adminRoleReview.IdentityETag
	}
	expectStatus(t, roleDefinitionFixtureRequest(t, router, "PUT", "rol_admin", map[string]any{"name": "Changed admin", "permissions": []string{}, "identity_etag": adminRoleIdentity, "reason": "Verify builtin definition denial"}, adminCookie, admin.CSRFToken, adminRoleReview.ReviewETag), 403)
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/rol_member", nil), 403)
	expectStatus(t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Unknown permission", "permissions": []string{"arbitrary.superuser"}}), 400)
	reader := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Catalog reader", "permissions": []string{"members.read", "providers.read"}}), 201)
	expectStatus(t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Catalog reader", "permissions": []string{}}), 409)
	caseRole := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "catalog reader", "permissions": []string{}}), 201)
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/"+caseRole.ID, nil), 204)
	writer := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Member manager", "permissions": []string{"members.write", "calls.read_all"}}), 201)
	memberPath := "/api/v1/admin/members/" + member.User.ID
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, adminCookie, admin.CSRFToken, member.User.ID, []string{reader.ID, writer.ID}), 200)
	assigned := decodeCatalogResponse[MemberResponse](t, request("GET", memberPath, nil), 200)
	if len(assigned.RoleIDs) != 2 {
		t.Fatal("combined roles not persisted")
	}
	permissions := decodeCatalogResponse[PermissionResponse](t, memberRequest("GET", "/api/v1/auth/permissions", nil), 200)
	for _, permission := range []string{"members.read", "members.write", "providers.read", "calls.read_all"} {
		if !slices.Contains(permissions.Permissions, permission) {
			t.Fatalf("missing combined permission %s", permission)
		}
	}
	expectStatus(t, memberRequest("GET", "/api/v1/admin/members", nil), 200)
	expectStatus(t, memberRequest("GET", "/api/v1/admin/providers", nil), 200)
	expectStatus(t, memberRequest("GET", "/api/v1/admin/calls", nil), 200)
	expectStatus(t, memberRequest("POST", "/api/v1/admin/providers", map[string]any{}), 403)
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, memberCookie, member.CSRFToken, member.User.ID, []string{"rol_admin"}), 403)
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, memberCookie, member.CSRFToken, member.User.ID, map[string]any{"role": "admin"}), 403)
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, memberCookie, member.CSRFToken, member.User.ID, map[string]any{"disabled": true}), 403)
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, memberCookie, member.CSRFToken, admin.User.ID, map[string]any{"disabled": true}), 403)
	expectStatus(t, memberRequest("POST", "/api/v1/admin/roles", map[string]any{"name": "Self escalation", "permissions": roles.AvailablePermissions}), 403)
	expectStatus(t, memberRequest("POST", "/api/v1/admin/members", map[string]any{"name": "Escalated", "email": "escalated@example.com", "password": "governance-password", "role": "admin"}), 403)
	managed := decodeCatalogResponse[MemberResponse](t, memberRequest("POST", "/api/v1/admin/members", map[string]any{"name": "Managed member", "email": "managed@example.com", "password": "governance-password"}), 201)
	if managed.Role != "member" || managed.Disabled {
		t.Fatal("delegated creation produced wrong identity state")
	}
	managedLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"managed@example.com","password":"governance-password"}`, nil, "")
	expectStatus(t, managedLogin, 200)
	_, managedCookie := readIdentity(t, managedLogin)
	bearer := "rx_" + strings.Repeat("a", 43)
	key := entity.APIKey{ID: "key_governance", UserID: managed.ID, Name: "Managed key", Prefix: "rx_test", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyActive}
	if err := db.Create(&key).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, memberCookie, member.CSRFToken, managed.ID, map[string]bool{"disabled": true}), 200)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", managedCookie, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"managed@example.com","password":"governance-password"}`, nil, ""), 401)
	if err := db.First(&key, "id = ?", key.ID).Error; err != nil || key.Status != entity.KeyRevoked {
		t.Fatal("member disable did not revoke personal Key")
	}
	var sessions int64
	if err := db.Model(&entity.Session{}).Where("user_id = ?", managed.ID).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Fatal("member disable did not delete sessions")
	}
	expectStatus(t, reviewedMemberStateFixtureRequest(t, router, adminCookie, admin.CSRFToken, managed.ID, map[string]bool{"disabled": false}), 200)
	if err := db.First(&key, "id = ?", key.ID).Error; err != nil || key.Status != entity.KeyRevoked {
		t.Fatal("reenabling a member restored a revoked Key")
	}
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/"+reader.ID, nil), 409)
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, adminCookie, admin.CSRFToken, member.User.ID, []string{"rol_admin"}), 400)
	var bindings int64
	if err := db.Model(&entity.UserRole{}).Where("user_id = ?", member.User.ID).Count(&bindings).Error; err != nil || bindings != 2 {
		t.Fatal("invalid role assignment partially replaced roles")
	}
	expectStatus(t, reviewedMemberRolesFixtureRequest(t, router, adminCookie, adminCookie, admin.CSRFToken, member.User.ID, []string{}), 200)
	decodeCatalogResponse[MemberResponse](t, request("GET", memberPath, nil), 200)
	expectStatus(t, memberRequest("GET", "/api/v1/admin/members", nil), 403)
	expectStatus(t, memberRequest("GET", "/api/v1/admin/providers", nil), 403)
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/"+reader.ID, nil), 204)
	listResponse := request("GET", "/api/v1/admin/members?q=MANAGED&status=active&role=member", nil)
	members := decodeCatalogResponse[MembersResponse](t, listResponse, 200)
	if len(members.Items) != 1 || members.Items[0].ID != managed.ID || strings.Contains(listResponse.Body.String(), "password") {
		t.Fatal("member filtering or redaction failed")
	}
	page := decodeCatalogResponse[MembersResponse](t, request("GET", "/api/v1/admin/members?limit=1", nil), 200)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("member pagination missing")
	}
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, false, false, "Close immediate registration fixture")
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/register", `{"email":"closed@example.com","password":"governance-password","name":"Closed registration"}`, nil, ""), 403)
	secondAdmin := decodeCatalogResponse[MemberResponse](t, request("POST", "/api/v1/admin/members", map[string]any{"email": "second-admin@example.com", "name": "Second admin", "password": "governance-password", "role": "admin"}), 201)
	secondLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"second-admin@example.com","password":"governance-password"}`, nil, "")
	expectStatus(t, secondLogin, 200)
	secondSession, secondCookie := readIdentity(t, secondLogin)
	adminReview := memberStateFixtureReview(t, router, adminCookie, admin.User.ID)
	secondReview := memberStateFixtureReview(t, router, secondCookie, secondAdmin.ID)
	var beforeStateAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "member.state.update").Count(&beforeStateAudits).Error; err != nil {
		t.Fatal(err)
	}
	responses := make(chan int, 2)
	startStateChanges := make(chan struct{})
	var wg sync.WaitGroup
	for _, candidate := range []struct {
		id     string
		cookie *http.Cookie
		csrf   string
		etag   string
	}{{admin.User.ID, adminCookie, admin.CSRFToken, adminReview.ETag}, {secondAdmin.ID, secondCookie, secondSession.CSRFToken, secondReview.ETag}} {
		wg.Go(func() {
			<-startStateChanges
			responses <- memberStateFixturePATCH(t, router, candidate.cookie, candidate.csrf, candidate.id, candidate.etag, map[string]any{"role": "member", "reason": "Concurrent last administrator review"}).Code
		})
	}
	close(startStateChanges)
	wg.Wait()
	close(responses)
	counts := map[int]int{}
	for code := range responses {
		counts[code]++
	}
	if counts[403] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent last administrator protection returned %v", counts)
	}
	var administrators int64
	if err := db.Model(&entity.User{}).Where(database.ExactText(db, clause.Column{Name: "role"}, entity.RoleAdmin)).Where("disabled = ? AND offboarded_at IS NULL", false).Count(&administrators).Error; err != nil || administrators != 1 {
		t.Fatal("last active administrator was lost")
	}
	var afterStateAudits int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "member.state.update").Count(&afterStateAudits).Error; err != nil || afterStateAudits-beforeStateAudits != 1 {
		t.Fatal("concurrent self demotion did not commit exactly one typed audit", afterStateAudits, beforeStateAudits, err)
	}
	var stateEvents []entity.AuditEvent
	if err := db.Where("action = ? AND resource_id IN ?", "member.state.update", []string{admin.User.ID, secondAdmin.ID}).Find(&stateEvents).Error; err != nil || len(stateEvents) != 1 {
		t.Fatal("concurrent self demotion audit target was not exact", err)
	}
	event := stateEvents[0]
	var stateDetails struct {
		UserID    string `json:"user_id"`
		Operation string `json:"operation"`
		Reason    string `json:"reason"`
		Before    struct {
			BaseRole string `json:"base_role"`
		} `json:"before"`
		After struct {
			BaseRole string `json:"base_role"`
		} `json:"after"`
	}
	if event.ActorID != event.ResourceID || event.DetailsJSON == nil || json.Unmarshal([]byte(*event.DetailsJSON), &stateDetails) != nil || stateDetails.UserID != event.ResourceID || stateDetails.Operation != "base_role" || stateDetails.Reason != "Concurrent last administrator review" || stateDetails.Before.BaseRole != entity.RoleAdmin || stateDetails.After.BaseRole != entity.RoleMember {
		t.Fatal("concurrent self demotion lost typed attribution")
	}
	// Reads after a new service instance still observe the persisted switch.
	fresh := identityRouter(t, db)
	persisted := decodeCatalogResponse[RegistrationResponse](t, identityRequest(fresh, "GET", "/api/v1/auth/registration", "", nil, ""), 200)
	if persisted.Enabled {
		t.Fatal("registration setting did not survive service restart")
	}
}
