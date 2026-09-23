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

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

// Invoked by the shared lifecycle owner against each fresh migrated database.
func testGovernanceLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
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
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+admin.User.ID, map[string]any{"disabled": true}), 409)
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+admin.User.ID, map[string]any{"role": "member"}), 409)
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/admin/registration", `{"enabled":true}`, adminCookie, ""), 403)
	decodeCatalogResponse[RegistrationResponse](t, request("PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true}), 200)
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
	expectStatus(t, memberRequest("PATCH", "/api/v1/admin/registration", map[string]bool{"enabled": false}), 403)
	roles := decodeCatalogResponse[RolesResponse](t, request("GET", "/api/v1/admin/roles", nil), 200)
	if len(roles.Items) != 2 || len(roles.AvailablePermissions) == 0 {
		t.Fatal("builtin roles or permission allowlist missing")
	}
	expectStatus(t, request("PUT", "/api/v1/admin/roles/rol_admin", map[string]any{"name": "Changed admin", "permissions": []string{}}), 403)
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/rol_member", nil), 403)
	expectStatus(t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Unknown permission", "permissions": []string{"arbitrary.superuser"}}), 400)
	reader := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Catalog reader", "permissions": []string{"members.read", "providers.read"}}), 201)
	expectStatus(t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Catalog reader", "permissions": []string{}}), 409)
	caseRole := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "catalog reader", "permissions": []string{}}), 201)
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/"+caseRole.ID, nil), 204)
	writer := decodeCatalogResponse[RoleResponse](t, request("POST", "/api/v1/admin/roles", map[string]any{"name": "Member manager", "permissions": []string{"members.write", "calls.read_all"}}), 201)
	memberPath := "/api/v1/admin/members/" + member.User.ID
	assigned := decodeCatalogResponse[MemberResponse](t, request("PUT", memberPath+"/roles", map[string]any{"role_ids": []string{reader.ID, writer.ID}}), 200)
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
	expectStatus(t, memberRequest("PUT", memberPath+"/roles", map[string]any{"role_ids": []string{"rol_admin"}}), 403)
	expectStatus(t, memberRequest("PATCH", memberPath, map[string]any{"role": "admin"}), 403)
	expectStatus(t, memberRequest("PATCH", memberPath, map[string]any{"disabled": true}), 403)
	expectStatus(t, memberRequest("PATCH", "/api/v1/admin/members/"+admin.User.ID, map[string]any{"disabled": true}), 403)
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
	decodeCatalogResponse[MemberResponse](t, memberRequest("PATCH", "/api/v1/admin/members/"+managed.ID, map[string]bool{"disabled": true}), 200)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", managedCookie, ""), 401)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"managed@example.com","password":"governance-password"}`, nil, ""), 401)
	if err := db.First(&key, "id = ?", key.ID).Error; err != nil || key.Status != entity.KeyRevoked {
		t.Fatal("member disable did not revoke personal Key")
	}
	var sessions int64
	if err := db.Model(&entity.Session{}).Where("user_id = ?", managed.ID).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Fatal("member disable did not delete sessions")
	}
	decodeCatalogResponse[MemberResponse](t, request("PATCH", "/api/v1/admin/members/"+managed.ID, map[string]bool{"disabled": false}), 200)
	if err := db.First(&key, "id = ?", key.ID).Error; err != nil || key.Status != entity.KeyRevoked {
		t.Fatal("reenabling a member restored a revoked Key")
	}
	expectStatus(t, request("DELETE", "/api/v1/admin/roles/"+reader.ID, nil), 409)
	expectStatus(t, request("PUT", memberPath+"/roles", map[string]any{"role_ids": []string{"rol_admin"}}), 400)
	var bindings int64
	if err := db.Model(&entity.UserRole{}).Where("user_id = ?", member.User.ID).Count(&bindings).Error; err != nil || bindings != 2 {
		t.Fatal("invalid role assignment partially replaced roles")
	}
	decodeCatalogResponse[MemberResponse](t, request("PUT", memberPath+"/roles", map[string]any{"role_ids": []string{}}), 200)
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
	decodeCatalogResponse[RegistrationResponse](t, request("PATCH", "/api/v1/admin/registration", map[string]bool{"enabled": false}), 200)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/register", `{"email":"closed@example.com","password":"governance-password","name":"Closed registration"}`, nil, ""), 403)
	secondAdmin := decodeCatalogResponse[MemberResponse](t, request("POST", "/api/v1/admin/members", map[string]any{"email": "second-admin@example.com", "name": "Second admin", "password": "governance-password", "role": "admin"}), 201)
	secondLogin := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"second-admin@example.com","password":"governance-password"}`, nil, "")
	expectStatus(t, secondLogin, 200)
	secondSession, secondCookie := readIdentity(t, secondLogin)
	responses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, candidate := range []struct {
		id     string
		cookie *http.Cookie
		csrf   string
	}{{admin.User.ID, adminCookie, admin.CSRFToken}, {secondAdmin.ID, secondCookie, secondSession.CSRFToken}} {
		wg.Go(func() {
			responses <- identityRequest(router, "PATCH", "/api/v1/admin/members/"+candidate.id, `{"role":"member"}`, candidate.cookie, candidate.csrf).Code
		})
	}
	wg.Wait()
	close(responses)
	counts := map[int]int{}
	for code := range responses {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent last administrator protection returned %v", counts)
	}
	var administrators int64
	if err := db.Model(&entity.User{}).Where("role = ? AND disabled = ?", entity.RoleAdmin, false).Count(&administrators).Error; err != nil || administrators != 1 {
		t.Fatal("last active administrator was lost")
	}
	// Reads after a new service instance still observe the persisted switch.
	fresh := identityRouter(t, db)
	persisted := decodeCatalogResponse[RegistrationResponse](t, identityRequest(fresh, "GET", "/api/v1/auth/registration", "", nil, ""), 200)
	if persisted.Enabled {
		t.Fatal("registration setting did not survive service restart")
	}
}
