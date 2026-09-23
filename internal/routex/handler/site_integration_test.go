package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func testSiteLifecycle(t *testing.T, db *gorm.DB) {
	router := identityRouter(t, db)
	defaults := decodeCatalogResponse[entity.SiteSetting](t, identityRequest(router, "GET", "/api/v1/site", "", nil, ""), 200)
	if defaults.Name != "RouteX" || defaults.DefaultLanguage != "en" || defaults.ETag != "0" {
		t.Fatal("invalid presentation defaults")
	}
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"site@example.test","password":"site-admin-password","name":"Site Admin"}`, nil, "")
	expectStatus(t, setup, 201)
	auth, cookie := readIdentity(t, setup)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		return identityRequest(router, method, path, string(encoded), cookie, auth.CSRFToken)
	}
	input := service.SiteInput{Name: "Workspace", ServiceURL: "https://site.example.test/", LogoURL: "https://cdn.example.test/logo.png", Footer: "Organization", DefaultLanguage: "zh", ETag: defaults.ETag}
	raw, _ := json.Marshal(input)
	expectStatus(t, identityRequest(router, "PUT", "/api/v1/admin/site", string(raw), cookie, ""), 403)
	expectStatus(t, identityRequest(router, "PUT", "/api/v1/admin/site", `{"name":"bad","unknown":true}`, cookie, auth.CSRFToken), 400)
	saved := decodeCatalogResponse[entity.SiteSetting](t, request("PUT", "/api/v1/admin/site", input), 200)
	if saved.ETag == defaults.ETag || saved.ServiceURL != "https://site.example.test" {
		t.Fatal("site update missing")
	}
	expectStatus(t, request("PUT", "/api/v1/admin/site", input), 409)
	restarted := identityRouter(t, db)
	persisted := decodeCatalogResponse[entity.SiteSetting](t, identityRequest(restarted, "GET", "/api/v1/site", "", nil, ""), 200)
	if persisted.Name != "Workspace" || persisted.DefaultLanguage != "zh" {
		t.Fatal("settings lost on service reopen")
	}
	if err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	current := decodeCatalogResponse[entity.SiteSetting](t, request("GET", "/api/v1/site", nil), 200)
	if current.ETag != saved.ETag {
		t.Fatal("repeat migration reset settings")
	}
	// Concurrent reviewed edits have exactly one winner.
	input.ETag = saved.ETag
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for _, name := range []string{"First", "Second"} {
		candidate := input
		candidate.Name = name
		encoded, _ := json.Marshal(candidate)
		wg.Go(func() {
			statuses <- identityRequest(router, "PUT", "/api/v1/admin/site", string(encoded), cookie, auth.CSRFToken).Code
		})
	}
	wg.Wait()
	close(statuses)
	winners, conflicts := 0, 0
	for status := range statuses {
		switch status {
		case 200:
			winners++
		case 409:
			conflicts++
		default:
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatal("lost-update protection failed")
	}
	literal := "<script>literal text</script>\nMaintenance"
	published := decodeCatalogResponse[entity.Announcement](t, request("POST", "/api/v1/admin/announcements", map[string]string{"content": literal}), 201)
	if published.Content != literal || published.Status != "active" {
		t.Fatal("announcement content/status changed")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/announcements", "", nil, ""), 401)
	visible := decodeCatalogResponse[service.AnnouncementPage](t, request("GET", "/api/v1/announcements", nil), 200)
	if len(visible.Items) != 1 || visible.Items[0].ID != published.ID {
		t.Fatal("active announcement missing")
	}
	path := "/api/v1/admin/announcements/" + published.ID
	changed := decodeCatalogResponse[entity.Announcement](t, request("PATCH", path, map[string]string{"content": "Updated", "etag": published.ETag}), 200)
	expectStatus(t, request("POST", path+"/close", map[string]string{"etag": published.ETag}), 409)
	closed := decodeCatalogResponse[entity.Announcement](t, request("POST", path+"/close", map[string]string{"etag": changed.ETag}), 200)
	if closed.Status != "closed" || closed.ClosedAt == nil {
		t.Fatal("close did not retain history")
	}
	again := decodeCatalogResponse[entity.Announcement](t, request("POST", path+"/close", map[string]string{"etag": closed.ETag}), 200)
	if again.ETag != closed.ETag {
		t.Fatal("idempotent close created revision")
	}
	editedClosed := decodeCatalogResponse[entity.Announcement](t, request("PATCH", path, map[string]string{"content": "Historical correction", "etag": closed.ETag}), 200)
	if editedClosed.Status != "closed" || editedClosed.ClosedAt == nil {
		t.Fatal("edit reopened closed announcement")
	}
	visible = decodeCatalogResponse[service.AnnouncementPage](t, request("GET", "/api/v1/announcements", nil), 200)
	if len(visible.Items) != 0 {
		t.Fatal("closed announcement still visible")
	}
	// History pagination must be complete and active delivery must not truncate.
	now := time.Now().UTC()
	rows := []entity.Announcement{}
	for i := range 52 {
		suffix := strings.Repeat("0", 24) + string(rune('A'+i/26)) + string(rune('A'+i%26))
		rows = append(rows, entity.Announcement{ID: "ann_" + suffix, Content: "History", Status: "closed", ETag: "0", CreatedAt: now, UpdatedAt: now, ClosedAt: &now})
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	page := decodeCatalogResponse[service.AnnouncementPage](t, request("GET", "/api/v1/admin/announcements", nil), 200)
	if len(page.Items) != 50 || page.NextCursor == nil {
		t.Fatal("missing history page")
	}
	next := decodeCatalogResponse[service.AnnouncementPage](t, request("GET", "/api/v1/admin/announcements?cursor="+*page.NextCursor, nil), 200)
	if len(next.Items) != 3 || next.NextCursor != nil {
		t.Fatal("history pagination incomplete")
	}
	for range 20 {
		expectStatus(t, request("POST", "/api/v1/admin/announcements", map[string]string{"content": "Active"}), 201)
	}
	expectStatus(t, request("POST", "/api/v1/admin/announcements", map[string]string{"content": "Overflow"}), 422)
	visible = decodeCatalogResponse[service.AnnouncementPage](t, request("GET", "/api/v1/announcements", nil), 200)
	if len(visible.Items) != 20 {
		t.Fatal("active announcement delivery incomplete")
	}
	expectStatus(t, request("GET", "/api/v1/admin/announcements?cursor=a&cursor=b", nil), 400)
	expectStatus(t, request("GET", "/api/v1/announcements?status=closed", nil), 400)
	// A downgraded account can read the active feed but loses administration immediately.
	if err := db.Model(&entity.User{}).Where("id = ?", auth.User.ID).Update("role", entity.RoleMember).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, request("GET", "/api/v1/admin/announcements", nil), 403)
	expectStatus(t, request("POST", "/api/v1/admin/announcements", map[string]string{"content": "Forbidden"}), 403)
	input.ETag = current.ETag
	expectStatus(t, request("PUT", "/api/v1/admin/site", input), 403)
	expectStatus(t, request("GET", "/api/v1/announcements", nil), 200)
	var auditCount int64
	if err := db.Model(&entity.AuditEvent{}).Where("action IN ?", []string{"site.update", "announcement.publish", "announcement.update", "announcement.close"}).Count(&auditCount).Error; err != nil || auditCount != 26 {
		t.Fatalf("audit history count %d, error %v", auditCount, err)
	}
}
