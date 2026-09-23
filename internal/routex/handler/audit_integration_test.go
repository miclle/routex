package handler

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func testAuditLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"audit@example.test","password":"audit-admin-password","name":"Audit Operator"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/audit", "", nil, ""), 401)
	base := time.Now().UTC().Add(-time.Hour)
	var rows []entity.AuditEvent
	for i := 0; i < 55; i++ {
		action := "model.update"
		resource := "model"
		if i == 0 {
			action = "literal_%!_update"
		}
		if i == 1 {
			action = "limits.update"
			resource = "key"
		}
		rows = append(rows, entity.AuditEvent{ID: fmt.Sprintf("aud_%026d", i), ActorID: auth.User.ID, Action: action, ResourceType: resource, ResourceID: "mdl_record", CreatedAt: base.Add(time.Duration(i) * time.Millisecond)})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	get := func(path string) *service.AuditPage {
		page := decodeCatalogResponse[service.AuditPage](t, identityRequest(router, "GET", path, "", cookie, ""), 200)
		return &page
	}
	first := get("/api/v1/admin/audit")
	if len(first.Items) != 50 || first.NextCursor == nil {
		t.Fatal("audit first page invalid")
	}
	next := get("/api/v1/admin/audit?cursor=" + *first.NextCursor)
	if len(next.Items) != 5 || next.NextCursor != nil {
		t.Fatal("audit continuation invalid")
	}
	if first.Items[0].ActorName != "Audit Operator" || first.Items[0].Source != nil || first.Items[0].IP != nil || first.Items[0].RequestID != nil {
		t.Fatal("incorrect metadata projection")
	}
	if got := get("/api/v1/admin/audit?q=" + url.QueryEscape("_%!_")); len(got.Items) != 1 {
		t.Fatalf("wildcards were not literal: %d", len(got.Items))
	}
	if got := get("/api/v1/admin/audit?q=" + url.QueryEscape("audit operator")); len(got.Items) != 50 {
		t.Fatal("actor search missing")
	}
	if got := get("/api/v1/admin/audit?category=limits"); len(got.Items) != 1 || got.Items[0].ResourceType != "key" {
		t.Fatal("Key limits category missing")
	}
	old := entity.AuditEvent{ID: "aud_99999999999999999999999999", ActorID: auth.User.ID, Action: "old", ResourceType: "model", ResourceID: "mdl_old", CreatedAt: time.Now().UTC().Add(-31 * 24 * time.Hour)}
	if err := db.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	if got := get("/api/v1/admin/audit?range=30d&q=old"); len(got.Items) != 0 {
		t.Fatal("range leaked old event")
	}
	for _, query := range []string{"range=all", "category=unknown", "cursor=invalid", "q=one&q=two", "unknown=value"} {
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/audit?"+query, "", cookie, ""), 400)
	}
	raw := `{"Before":{"rpm":1},"After":{"rpm":2},"password":"do-not-leak"}`
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "limits.update").Update("details_json", raw).Error; err != nil {
		t.Fatal(err)
	}
	response := identityRequest(router, "GET", "/api/v1/admin/audit?category=limits", "", cookie, "")
	expectStatus(t, response, 200)
	if strings.Contains(response.Body.String(), "do-not-leak") || !strings.Contains(response.Body.String(), `"before"`) {
		t.Fatal("unsafe detail response")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", auth.User.ID).Update("role", entity.RoleMember).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", auth.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/audit", "", cookie, ""), 403)
}
