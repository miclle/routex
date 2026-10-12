package handler

import (
	"encoding/json"
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
	var initialAuditCount int64
	if err := db.Model(&entity.AuditEvent{}).Count(&initialAuditCount).Error; err != nil {
		t.Fatal(err)
	}
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
	if len(next.Items) != len(rows)+int(initialAuditCount)-50 || next.NextCursor != nil {
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
	if got := get("/api/v1/admin/audit?category=limits&q=limits.update"); len(got.Items) != 1 || got.Items[0].ResourceType != "key" {
		t.Fatal("Key limits category missing")
	}
	if got := get("/api/v1/admin/audit?category=limits&q=limits.default.apply"); len(got.Items) != 1 || got.Items[0].Action != "limits.default.apply" || got.Items[0].ResourceType != "user" || got.Items[0].ResourceID != auth.User.ID || !strings.Contains(string(got.Items[0].Changes), `"default_rule_etag"`) || !strings.Contains(string(got.Items[0].Changes), `"after"`) {
		t.Fatal("creation default audit facts missing or misclassified")
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
	// The system category projects a closed historical command schema, including
	// a failed refresh whose audit transaction itself successfully committed.
	for i, outcome := range []string{"claimed", "refresh_returned_nil", "refresh_returned_error", "malformed"} {
		action := "runtime.publication.finalize"
		if outcome == "claimed" {
			action = "runtime.publication.claim"
		}
		detail := fmt.Sprintf(`{"request_id":"6f739b12-12ce-4a20-8da7-%012d","reason":"Reviewed audit publication","outcome":"%s"}`, i+1, outcome)
		if outcome == "malformed" {
			detail = `{"request_id":"6f739b12-12ce-4a20-8da7-000000000004","reason":"Reviewed audit publication","outcome":"refresh_returned_nil","secret":"do-not-leak"}`
		}
		if db.Create(&entity.AuditEvent{ID: fmt.Sprintf("aud_000000000000000000000001%02d", i), ActorID: auth.User.ID, Action: action, ResourceType: "system_instance", ResourceID: "ins_01m36yee4gkbns18pfcqqc75a3", DetailsJSON: &detail, CreatedAt: base}).Error != nil {
			t.Fatal("create bounded publication audit fixtures")
		}
	}
	publication := get("/api/v1/admin/audit?category=system&q=runtime.publication")
	if len(publication.Items) != 4 {
		t.Fatal("publication events missing from system audit category")
	}
	projected, redacted := 0, 0
	for _, event := range publication.Items {
		if event.Result != "committed" || event.RequestID != nil || event.Source != nil || event.IP != nil {
			t.Fatal("publication audit changed committed result or invented transport metadata")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(event.Changes, &fields) != nil || strings.Contains(string(event.Changes), "do-not-leak") {
			t.Fatal("unsafe publication projection")
		}
		if fields == nil {
			if event.ID != "aud_00000000000000000000000103" {
				t.Fatal("valid publication audit was redacted")
			}
			redacted++
			continue
		}
		var changes struct {
			Kind             string `json:"kind"`
			CommandRequestID string `json:"command_request_id"`
			Reason           string `json:"reason"`
			Outcome          string `json:"outcome"`
		}
		if len(fields) != 4 || json.Unmarshal(event.Changes, &changes) != nil || changes.Kind != "runtime_publication" || changes.Reason != "Reviewed audit publication" {
			t.Fatal("unsafe publication projection")
		}
		expected := map[string]string{
			"6f739b12-12ce-4a20-8da7-000000000001": "claimed",
			"6f739b12-12ce-4a20-8da7-000000000002": "refresh_returned_nil",
			"6f739b12-12ce-4a20-8da7-000000000003": "refresh_returned_error",
		}
		if expected[changes.CommandRequestID] == "" || changes.Outcome != expected[changes.CommandRequestID] || (changes.Outcome == "claimed") != (event.Action == "runtime.publication.claim") {
			t.Fatal("publication projection changed exact command or outcome")
		}
		projected++
	}
	if projected != 3 || redacted != 1 {
		t.Fatal("publication schema did not fail closed")
	}
	if err := db.Model(&entity.User{}).Where("id = ?", auth.User.ID).Update("role", entity.RoleMember).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", auth.User.ID).Delete(&entity.UserRole{}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/audit", "", cookie, ""), 403)
}
