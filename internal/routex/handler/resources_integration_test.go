package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
)

// The shared integration driver invokes this against a fresh migrated database.
func testResourceLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"resources@example.com","password":"resources-password","name":"Resource admin"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	as := func(cookie *http.Cookie, csrf, method, path string, body any) *httptest.ResponseRecorder {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		return identityRequest(router, method, path, string(encoded), cookie, csrf)
	}
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(adminCookie, admin.CSRFToken, method, path, body)
	}
	makeMember := func(email string) (SessionResponse, *http.Cookie) {
		response := request("POST", "/api/v1/admin/members", map[string]any{"email": email, "password": "resources-password", "name": email})
		expectStatus(t, response, 201)
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"`+email+`","password":"resources-password"}`, nil, "")
		expectStatus(t, login, 200)
		return readIdentity(t, login)
	}
	owner, ownerCookie := makeMember("owner@example.com")
	second, secondCookie := makeMember("second@example.com")
	outsider, outsiderCookie := makeMember("outsider@example.com")
	ownerRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(ownerCookie, owner.CSRFToken, method, path, body)
	}
	secondRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(secondCookie, second.CSRFToken, method, path, body)
	}
	outsiderRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		return as(outsiderCookie, outsider.CSRFToken, method, path, body)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/teams", "", nil, ""), 401)
	expectStatus(t, ownerRequest("POST", "/api/v1/admin/teams", map[string]any{"name": "Forbidden", "owner_ids": []string{owner.User.ID}}), 403)
	expectStatus(t, request("POST", "/api/v1/admin/teams", map[string]any{"name": "No owner", "owner_ids": []string{}}), 400)
	team := decodeCatalogResponse[TeamResponse](t, request("POST", "/api/v1/admin/teams", map[string]any{"name": "Engineering", "description": "Production team", "owner_ids": []string{owner.User.ID}}), 201)
	if len(team.Members) != 1 || team.Members[0].Role != "owner" || len(team.ModelIDs) != 0 {
		t.Fatal("invalid initial Team state")
	}
	teamPath := "/api/v1/admin/teams/" + team.ID
	expectStatus(t, ownerRequest("GET", "/api/v1/teams/"+team.ID, nil), 200)
	expectStatus(t, outsiderRequest("GET", "/api/v1/teams/"+team.ID, nil), 404)
	expectStatus(t, ownerRequest("PATCH", teamPath, map[string]any{"name": "Escalated"}), 403)
	expectStatus(t, ownerRequest("PUT", teamPath+"/models", map[string]any{"model_ids": []string{}}), 403)
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+owner.User.ID, map[string]any{"disabled": true}), 409)
	expectStatus(t, identityRequest(router, "PUT", teamPath+"/members", `{"members":[]}`, adminCookie, ""), 403)
	expectStatus(t, request("PUT", teamPath+"/members", map[string]any{"members": []map[string]string{{"user_id": owner.User.ID, "role": "member", "status": "active"}}}), 409)
	expectStatus(t, request("PUT", teamPath+"/members", map[string]any{"members": []map[string]string{{"user_id": owner.User.ID, "role": "owner", "status": "pending"}}}), 400)
	memberRows := []map[string]string{{"user_id": owner.User.ID, "role": "owner", "status": "active"}, {"user_id": second.User.ID, "role": "member", "status": "disabled"}}
	updatedTeam := decodeCatalogResponse[TeamResponse](t, request("PUT", teamPath+"/members", map[string]any{"members": memberRows}), 200)
	if updatedTeam.Members[0].ID != team.Members[0].ID {
		t.Fatal("membership identity changed during replacement")
	}
	expectStatus(t, secondRequest("GET", "/api/v1/teams/"+team.ID, nil), 404)
	memberRows[1]["status"] = "active"
	expectStatus(t, request("PUT", teamPath+"/members", map[string]any{"members": memberRows}), 200)
	expectStatus(t, secondRequest("GET", "/api/v1/teams/"+team.ID, nil), 200)
	project := decodeCatalogResponse[ProjectResponse](t, ownerRequest("POST", "/api/v1/projects", map[string]any{"name": "Independent project", "description": "No Team inheritance"}), 201)
	projectPath := "/api/v1/projects/" + project.ID
	if project.CreatorID != owner.User.ID || len(project.Managers) != 1 || len(project.ModelIDs) != 0 {
		t.Fatal("invalid initial Project state")
	}
	expectStatus(t, secondRequest("GET", projectPath, nil), 404)
	expectStatus(t, outsiderRequest("GET", projectPath, nil), 404)
	expectStatus(t, ownerRequest("PATCH", projectPath, map[string]any{"name": "Renamed project"}), 200)
	expectStatus(t, ownerRequest("PATCH", projectPath, map[string]any{"status": "disabled"}), 403)
	expectStatus(t, ownerRequest("PUT", projectPath+"/models", map[string]any{"model_ids": []string{}}), 403)
	expectStatus(t, ownerRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{}}), 409)
	expectStatus(t, outsiderRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{outsider.User.ID}}), 403)
	expectStatus(t, ownerRequest("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{owner.User.ID, second.User.ID}}), 200)
	expectStatus(t, secondRequest("GET", projectPath, nil), 200)
	// A Project-only manager also blocks suspension independently of Team checks.
	independent := decodeCatalogResponse[ProjectResponse](t, outsiderRequest("POST", "/api/v1/projects", map[string]any{"name": "Project-only continuity"}), 201)
	independentPath := "/api/v1/projects/" + independent.ID
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+outsider.User.ID, map[string]any{"disabled": true}), 409)
	// Removing the creator from management removes access; creator is audit-only.
	expectStatus(t, outsiderRequest("PUT", independentPath+"/managers", map[string]any{"user_ids": []string{admin.User.ID}}), 200)
	expectStatus(t, outsiderRequest("GET", independentPath, nil), 404)
	expectStatus(t, outsiderRequest("PATCH", independentPath, map[string]any{"name": "Creator escalation"}), 403)
	expectStatus(t, request("PATCH", "/api/v1/admin/members/"+outsider.User.ID, map[string]any{"disabled": true}), 200)
	// Invalid relationship replacement leaves the previous owner intact.
	expectStatus(t, request("PUT", teamPath+"/members", map[string]any{"members": []map[string]string{{"user_id": owner.User.ID, "role": "owner", "status": "active"}, {"user_id": "usr_missing", "role": "member", "status": "active"}}}), 400)
	intactTeam := decodeCatalogResponse[TeamResponse](t, request("GET", teamPath, nil), 200)
	if len(intactTeam.Members) != 2 || intactTeam.Members[0].ID != team.Members[0].ID {
		t.Fatal("invalid membership replacement changed existing relationships")
	}
	// Team and Project model grants never become direct user or personal Key grants.
	model := entity.Model{ID: "mdl_resource_model", Status: "active"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.ModelName{Name: "resource-model", ModelID: model.ID, CurrentModelID: &model.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{teamPath + "/models", projectPath + "/models"} {
		expectStatus(t, request("PUT", path, map[string]any{"model_ids": []string{model.ID}}), 200)
		expectStatus(t, request("PUT", path, map[string]any{"model_ids": []string{model.ID, "mdl_missing"}}), 400)
	}
	afterTeam := decodeCatalogResponse[TeamResponse](t, request("GET", teamPath, nil), 200)
	if len(afterTeam.ModelIDs) != 1 || afterTeam.ModelIDs[0] != model.ID {
		t.Fatal("failed model grant replacement was not atomic")
	}
	var directGrants int64
	if err := db.Model(&entity.UserModelGrant{}).Count(&directGrants).Error; err != nil || directGrants != 0 {
		t.Fatal("resource grants leaked into direct model access")
	}
	expectStatus(t, ownerRequest("POST", "/api/v1/keys", map[string]any{"name": "Scope escalation", "model_ids": []string{model.ID}}), 403)
	// Scoped and global lists have explicit, independent permissions.
	scoped := decodeCatalogResponse[TeamsResponse](t, ownerRequest("GET", "/api/v1/teams", nil), 200)
	if len(scoped.Items) != 1 {
		t.Fatal("owner did not see scoped Team")
	}
	adminScoped := decodeCatalogResponse[TeamsResponse](t, request("GET", "/api/v1/teams", nil), 200)
	if len(adminScoped.Items) != 0 {
		t.Fatal("personal Team list implicitly became global for admin")
	}
	expectStatus(t, ownerRequest("GET", "/api/v1/admin/teams", nil), 403)
	extra := decodeCatalogResponse[TeamResponse](t, request("POST", "/api/v1/admin/teams", map[string]any{"name": "Archive candidate", "owner_ids": []string{admin.User.ID}}), 201)
	firstPage := decodeCatalogResponse[TeamsResponse](t, request("GET", "/api/v1/admin/teams?limit=1", nil), 200)
	if len(firstPage.Items) != 1 || firstPage.NextCursor == nil {
		t.Fatal("resource pagination cursor missing")
	}
	nextPage := decodeCatalogResponse[TeamsResponse](t, request("GET", "/api/v1/admin/teams?limit=1&cursor="+*firstPage.NextCursor, nil), 200)
	if len(nextPage.Items) != 1 || nextPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatal("resource pagination repeated record")
	}
	searched := decodeCatalogResponse[TeamsResponse](t, request("GET", "/api/v1/admin/teams?q=Engineering", nil), 200)
	if len(searched.Items) != 1 || searched.Items[0].ID != team.ID {
		t.Fatal("resource search mismatch")
	}
	archivePath := "/api/v1/admin/teams/" + extra.ID
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "archived"}), 409)
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "disabled"}), 200)
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "active"}), 200)
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "disabled"}), 200)
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "archived"}), 200)
	expectStatus(t, request("PATCH", archivePath, map[string]any{"status": "active"}), 409)
	expectStatus(t, request("PUT", archivePath+"/models", map[string]any{"model_ids": []string{}}), 409)
	// Two suspensions cannot remove both active owners/managers.
	memberRows[1]["role"] = "owner"
	expectStatus(t, request("PUT", teamPath+"/members", map[string]any{"members": memberRows}), 200)
	var wg sync.WaitGroup
	statuses := make(chan int, 2)
	for _, userID := range []string{owner.User.ID, second.User.ID} {
		wg.Go(func() {
			statuses <- request("PATCH", "/api/v1/admin/members/"+userID, map[string]any{"disabled": true}).Code
		})
	}
	wg.Wait()
	close(statuses)
	successes, conflicts := 0, 0
	for status := range statuses {
		switch status {
		case 200:
			successes++
		case 409:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent suspension status %d", status)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("continuity lost: successes=%d conflicts=%d", successes, conflicts)
	}
	var activeManagers int64
	if err := db.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND u.disabled = ?", project.ID, false).Count(&activeManagers).Error; err != nil || activeManagers != 1 {
		t.Fatal("Project lost its last active manager")
	}
	// Archived resources retain historical relations and cannot be mutated.
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "disabled"}), 200)
	expectStatus(t, request("PATCH", projectPath, map[string]any{"status": "archived"}), 200)
	expectStatus(t, request("PUT", projectPath+"/managers", map[string]any{"user_ids": []string{admin.User.ID}}), 409)
	// Foreign keys exist on both supported databases.
	if err := db.Create(&entity.TeamMembership{ID: "tmm_orphan", TeamID: "tea_missing", UserID: admin.User.ID, Role: "owner", Status: "active"}).Error; err == nil {
		t.Fatal("Team foreign key missing")
	}
	if err := db.Create(&entity.ProjectManager{ID: "pmg_orphan", ProjectID: project.ID, UserID: "usr_missing"}).Error; err == nil {
		t.Fatal("Project manager foreign key missing")
	}
	var events int64
	if err := db.Model(&entity.AuditEvent{}).Where("resource_type IN ?", []string{"teams", "projects"}).Count(&events).Error; err != nil || events < 10 {
		t.Fatal("resource mutations were not audited")
	}
}
