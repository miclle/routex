package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

// Proposed companion to the retained Role definition lifecycle. It exercises
// descriptions through the real registered HTTP and atomic service boundaries.
func testRoleDescriptions(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var failAudit, failConfirmation, confirmationArmed atomic.Bool
	const (
		auditBefore = "fixture:role-description-audit-before"
		auditAfter  = "fixture:role-description-audit-after"
		queryBefore = "fixture:role-description-confirmation"
	)
	if err := db.Callback().Create().Before("gorm:create").Register(auditBefore, func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if ok && event.Action == "role.definition.update" && failAudit.Load() {
			_ = tx.AddError(errors.New("controlled description audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(auditAfter, func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if ok && event.Action == "role.definition.update" && tx.Error == nil && failConfirmation.Load() {
			confirmationArmed.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryBefore, func(tx *gorm.DB) {
		if confirmationArmed.Load() && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("controlled description fresh confirmation failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, err := range []error{db.Callback().Create().Remove(auditBefore), db.Callback().Create().Remove(auditAfter), db.Callback().Query().Remove(queryBefore)} {
			if err != nil {
				t.Error(err)
			}
		}
	})
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"description-admin@example.invalid","password":"description-test-password","name":"Description administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "description-reader", []string{"roles.read"})
	post := func(raw string) *httptest.ResponseRecorder {
		t.Helper()
		return identityRequest(router, "POST", "/api/v1/admin/roles", raw, adminCookie, admin.CSRFToken)
	}
	legacy := decodeCatalogResponse[RoleResponse](t, post(`{"name":"Legacy description role","permissions":[]}`), 201)
	if legacy.Description != "" || legacy.MemberCount != nil {
		t.Fatal("legacy creation invented description or count")
	}
	body := `{"name":"Recorded description role","description":"First line\n第二行","permissions":["prices.read"]}`
	created := decodeCatalogResponse[RoleResponse](t, post(body), 201)
	if created.Description != "First line\n第二行" || created.MemberCount != nil {
		t.Fatal("explicit description not recorded exactly")
	}
	target := created.ID
	readRole := func() entity.Role {
		t.Helper()
		var row entity.Role
		if err := db.Take(&row, "id = ?", target).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	countRoles := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.Role{}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	n := countRoles()
	for _, value := range []string{`null`, `""`, `" trailing "`, `"Bad\rvalue"`, `"Bad\tvalue"`, `"\ud800"`, `"` + strings.Repeat("界", 667) + `"`} {
		expectStatus(t, post(`{"name":"Invalid description role","description":`+value+`,"permissions":[]}`), 400)
		if countRoles() != n {
			t.Fatal("invalid description created a Role")
		}
	}
	listed := decodeCatalogResponse[RolesResponse](t, identityRequest(router, "GET", "/api/v1/admin/roles", "", adminCookie, ""), 200)
	found := false
	for _, item := range listed.Items {
		if item.ID == target {
			found = true
			if item.Description != created.Description {
				t.Fatal("list lost recorded description")
			}
		}
		if item.AssignmentKind == service.RoleAssignmentIntrinsic && item.Description != "" {
			t.Fatal("intrinsic description invented")
		}
		if item.Builtin && item.AssignmentKind == service.RoleAssignmentExplicit && item.Description == "" {
			t.Fatal("recorded duty description missing")
		}
	}
	if !found {
		t.Fatal("created Role absent from complete list")
	}
	snapshot := func() map[string][]string {
		t.Helper()
		result := map[string][]string{}
		for _, table := range []string{"users", "sessions", "api_keys", "api_key_models", "user_roles", "team_roles", "user_model_grants", "team_model_grants", "project_model_grants", "models", "resource_limits", "call_records", "call_attempts"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			values := []string{}
			for _, row := range rows {
				raw, err := json.Marshal(row)
				if err != nil {
					t.Fatal(err)
				}
				values = append(values, string(raw))
			}
			slices.Sort(values)
			result[table] = values
		}
		return result
	}
	protected := snapshot()
	permissions := func() []entity.RolePermission {
		t.Helper()
		var rows []entity.RolePermission
		if err := db.Where("role_id = ?", target).Order("permission").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	originalPermissions := permissions()
	audits := func() []entity.AuditEvent {
		t.Helper()
		var rows []entity.AuditEvent
		if err := db.Where("action = ? AND resource_id = ?", "role.definition.update", target).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	review := roleDefinitionFixtureReview(t, router, adminCookie, target)
	if review.Description != created.Description {
		t.Fatal("review lost description")
	}
	input := service.RoleDefinitionInput{Name: review.Name, Description: "Reviewed purpose\n第二行", Permissions: slices.Clone(review.Permissions), IdentityETag: *review.IdentityETag, Reason: "Review recorded purpose"}
	put := func(r service.RoleDefinitionRecord, in service.RoleDefinitionInput, status int) service.RoleDefinitionResult {
		t.Helper()
		out := roleDefinitionFixtureRequest(t, router, "PUT", target, in, adminCookie, admin.CSRFToken, r.ReviewETag)
		expectStatus(t, out, status)
		var result service.RoleDefinitionResult
		if status == 200 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(out.Body.Bytes(), &fields) != nil || len(fields) != 8 || json.Unmarshal(out.Body.Bytes(), &result) != nil || result.Description != in.Description || result.Name != in.Name || !slices.Equal(result.Permissions, in.Permissions) || result.IdentityETag != in.IdentityETag || result.Confirmation != "current_role_definition" || result.Effect != "current_database" {
				t.Fatal("incomplete current confirmation")
			}
		}
		return result
	}
	original := readRole()
	put(review, input, 200)
	changed := readRole()
	if changed.Description != input.Description || changed.Name != original.Name || changed.DefinitionRevision == original.DefinitionRevision || !changed.CreatedAt.Equal(original.CreatedAt) || !reflect.DeepEqual(permissions(), originalPermissions) || !reflect.DeepEqual(snapshot(), protected) || len(audits()) != 1 {
		t.Fatal("description-only write changed protected facts")
	}
	var typed struct {
		Version       int `json:"version"`
		Before, After struct {
			Description string `json:"description"`
		}
	}
	rows := audits()
	if rows[0].DetailsJSON == nil || json.Unmarshal([]byte(*rows[0].DetailsJSON), &typed) != nil || typed.Version != 2 || typed.Before.Description != created.Description || typed.After.Description != input.Description {
		t.Fatal("versioned recorded audit missing")
	}
	put(review, input, 200)
	if !reflect.DeepEqual(readRole(), changed) || len(audits()) != 1 {
		t.Fatal("current-state old retry replayed mutation/audit")
	}
	conflicting := input
	conflicting.Description = "Different original intent"
	put(review, conflicting, 409)
	if !reflect.DeepEqual(readRole(), changed) || len(audits()) != 1 {
		t.Fatal("stale review changed contents")
	}
	fresh := roleDefinitionFixtureReview(t, router, adminCookie, target)
	failAudit.Store(true)
	put(fresh, conflicting, 503)
	failAudit.Store(false)
	if !reflect.DeepEqual(readRole(), changed) || len(audits()) != 1 {
		t.Fatal("typed audit failure did not roll back definition")
	}
	failConfirmation.Store(true)
	put(fresh, conflicting, 503)
	confirmationArmed.Store(false)
	failConfirmation.Store(false)
	committed := readRole()
	if committed.Description != conflicting.Description || len(audits()) != 2 {
		t.Fatal("uncertain durable description absent")
	}
	put(fresh, conflicting, 200)
	if !reflect.DeepEqual(readRole(), committed) || len(audits()) != 2 {
		t.Fatal("uncertain retry rewrote current contents")
	}
	latest := roleDefinitionFixtureReview(t, router, adminCookie, target)
	expectStatus(t, roleDefinitionFixtureRequest(t, router, "PUT", target, input, readerCookie, readerCSRF, latest.ReviewETag), 403)
	builtin := roleDefinitionFixtureReview(t, router, adminCookie, "rol_admin")
	proof := strings.Repeat("a", 64)
	if builtin.IdentityETag != nil {
		proof = *builtin.IdentityETag
	}
	expectStatus(t, roleDefinitionFixtureRequest(t, router, "PUT", "rol_admin", service.RoleDefinitionInput{Name: builtin.Name, Description: "Builtin remains protected", Permissions: []string{}, IdentityETag: proof, Reason: "Check protected builtin"}, adminCookie, admin.CSRFToken, builtin.ReviewETag), http.StatusForbidden)
	saved, err := svc.SaveRole(ctx, admin.User.ID, target, "Trusted renamed role", latest.Permissions)
	if err != nil || saved.Role.Description != committed.Description {
		t.Fatal("trusted omission erased recorded description", err)
	}
	if !reflect.DeepEqual(snapshot(), protected) || !reflect.DeepEqual(permissions(), originalPermissions) || len(audits()) != 2 {
		t.Fatal("description workflow changed identity/assignment/history")
	}
}
