package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func roleDefinitionFixtureRequest(t *testing.T, router http.Handler, method, roleID string, body any, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "http://routex.test/api/v1/admin/roles/"+roleID, strings.NewReader(string(raw)))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if etag != "" {
		req.Header.Set("If-Match", strconv.Quote(etag))
	}
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	return out
}

func roleDefinitionFixtureReview(t *testing.T, router http.Handler, cookie *http.Cookie, roleID string) service.RoleDefinitionRecord {
	t.Helper()
	out := roleDefinitionFixtureRequest(t, router, "GET", roleID, nil, cookie, "", "")
	expectStatus(t, out, 200)
	var result service.RoleDefinitionRecord
	var fields map[string]json.RawMessage
	if json.Unmarshal(out.Body.Bytes(), &result) != nil || json.Unmarshal(out.Body.Bytes(), &fields) != nil || len(fields) != 11 || result.ID != roleID || result.Permissions == nil || result.AvailablePermissions == nil || !slices.IsSorted(result.Permissions) || !slices.IsSorted(result.AvailablePermissions) || len(result.DefinitionETag) != 64 || len(result.ReviewETag) != 64 || out.Header().Get("ETag") != strconv.Quote(result.ReviewETag) || out.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("invalid complete role resource review")
	}
	for _, key := range []string{"id", "name", "description", "builtin", "assignment_kind", "permissions", "available_permissions", "definition_etag", "identity_etag", "review_etag", "can_edit"} {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing role review field", key)
		}
	}
	wantKind := service.RoleAssignmentExplicit
	if roleID == "rol_admin" || roleID == "rol_member" {
		wantKind = service.RoleAssignmentIntrinsic
	}
	if result.AssignmentKind != wantKind {
		t.Fatal("unexpected reviewed Role assignment kind")
	}
	return result
}

// Root alone registers this proposal against the accepted Approval predecessor.
// It makes no native calls and never treats a GET as operation confirmation.
func testRoleDefinitionLifecycle(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	var auditFail, confirmationArmed atomic.Bool
	var confirmationMode atomic.Int32
	var mutationActor atomic.Pointer[string]
	const auditBefore = "test:role-definition-audit-before"
	const auditAfter = "test:role-definition-audit-after"
	const queryBefore = "test:role-definition-confirmation-before"
	// Install all callbacks before service/request use. Only the flags change;
	// no live callback-registry mutation or runtime worker bypass is needed.
	if err := db.Callback().Create().Before("gorm:create").Register(auditBefore, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "role.definition.update" && auditFail.Load() {
			_ = tx.AddError(errors.New("controlled role definition audit outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(auditAfter, func(tx *gorm.DB) {
		event, ok := tx.Statement.Dest.(*entity.AuditEvent)
		if !ok || event.Action != "role.definition.update" || tx.Error != nil {
			return
		}
		mode := confirmationMode.Load()
		if mode == 1 {
			confirmationArmed.Store(true)
			return
		}
		if mode == 5 {
			q := tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "role_id"}, event.ResourceID)).Delete(&entity.RolePermission{})
			if q.Error != nil {
				_ = tx.AddError(q.Error)
				return
			}
			q = tx.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(tx, clause.Column{Name: "id"}, event.ResourceID)).Delete(&entity.Role{})
			if q.Error != nil {
				_ = tx.AddError(q.Error)
			}
			return
		}
		actor := mutationActor.Load()
		if actor == nil || mode < 2 {
			return
		}
		fields := map[string]any{}
		switch mode {
		case 2:
			fields["role"] = entity.RoleMember
		case 3:
			fields["disabled"] = true
		case 4:
			fields["offboarded_at"] = time.Now().UTC().Truncate(time.Microsecond)
		default:
			return
		}
		// A controlled concurrent authority effect is made durable in the same
		// database transaction after authorization and audit. The independent
		// confirmation must reread it, rather than return its earlier snapshot.
		q := tx.Session(&gorm.Session{NewDB: true}).Model(&entity.User{}).
			Where(database.ExactText(tx, clause.Column{Name: "id"}, *actor)).UpdateColumns(fields)
		if q.Error != nil {
			_ = tx.AddError(q.Error)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryBefore, func(tx *gorm.DB) {
		if confirmationArmed.Load() && tx.Statement.Table == "users" {
			_ = tx.AddError(errors.New("controlled fresh confirmation read outage"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, err := range []error{db.Callback().Create().Remove(auditBefore), db.Callback().Create().Remove(auditAfter), db.Callback().Query().Remove(queryBefore)} {
			if err != nil {
				t.Error("owned callback cleanup failed", err)
			}
		}
	})
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"definition-admin@example.invalid","password":"definition-test-password","name":"Definition administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "definition-reader", []string{"roles.read"})
	delegated, delegatedCookie, delegatedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "definition-delegated", []string{"members.write", "roles.read"})
	_, noReadCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "definition-no-read", nil)
	target, err := svc.SaveRole(ctx, admin.User.ID, "", "Definition target", []string{"prices.read"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.SaveRole(ctx, admin.User.ID, "", "Definition occupied name", []string{})
	if err != nil {
		t.Fatal(err)
	}
	deletedTarget, err := svc.SaveRole(ctx, admin.User.ID, "", "Definition deleted during confirmation", []string{})
	if err != nil {
		t.Fatal(err)
	}
	retainedAdmin, err := svc.CreateMember(ctx, admin.User.ID, "definition-other-admin@example.invalid", "definition-test-password", "Definition second administrator", entity.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"definition-other-admin@example.invalid","password":"definition-test-password"}`, nil, "")
	expectStatus(t, login, 200)
	secondAuth, secondCookie := readIdentity(t, login)
	// Pending registration is produced by the real product engine. Promotion
	// is deliberately inert preconfiguration; it must not grant admission.
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, true, "Prepare pending definition actor")
	pendingResult, err := svc.RegisterWithApproval(ctx, "definition-pending@example.invalid", "definition-test-password", "Definition pending actor")
	if err != nil || pendingResult == nil || !pendingResult.ApprovalPending || pendingResult.Authentication != nil {
		t.Fatal("pending registration issued authentication", err)
	}
	var pendingUser entity.User
	if err := db.Where(database.ExactText(db, clause.Column{Name: "email"}, "definition-pending@example.invalid")).First(&pendingUser).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Session(&gorm.Session{NewDB: true}).Model(&entity.User{}).Where(database.ExactText(db, clause.Column{Name: "id"}, pendingUser.ID)).UpdateColumn("role", entity.RoleAdmin).Error; err != nil {
		t.Fatal(err)
	}
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, false, false, "Close pending definition fixture")
	id := target.Role.ID
	readRole := func() entity.Role {
		t.Helper()
		var row entity.Role
		if err := db.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(db, clause.Column{Name: "id"}, id)).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	readPermissions := func() []entity.RolePermission {
		t.Helper()
		var rows []entity.RolePermission
		if err := db.Session(&gorm.Session{NewDB: true}).Where(database.ExactText(db, clause.Column{Name: "role_id"}, id)).Order("permission").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	audits := func() []entity.AuditEvent {
		t.Helper()
		var rows []entity.AuditEvent
		if err := db.Where("action = ? AND resource_id = ?", "role.definition.update", id).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	snapshot := func() map[string][]string {
		t.Helper()
		result := map[string][]string{}
		for _, table := range []string{"users", "sessions", "api_keys", "api_key_models", "user_roles", "user_model_grants", "team_model_grants", "resource_limits", "models", "model_names", "provider_models", "teams", "team_memberships", "call_records", "call_attempts"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			values := make([]string, 0, len(rows))
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
	baseline := snapshot()
	input := func(review service.RoleDefinitionRecord, name string, permissions []string) service.RoleDefinitionInput {
		t.Helper()
		if review.IdentityETag == nil {
			t.Fatal("ordinary created role lacks identity proof")
		}
		description := review.Description
		if description == "" {
			description = "Reviewed role purpose"
		}
		return service.RoleDefinitionInput{Name: name, Description: description, Permissions: permissions, IdentityETag: *review.IdentityETag, Reason: "Reviewed exact custom definition"}
	}
	put := func(cookie *http.Cookie, csrf string, review service.RoleDefinitionRecord, body service.RoleDefinitionInput, status int) service.RoleDefinitionResult {
		t.Helper()
		out := roleDefinitionFixtureRequest(t, router, "PUT", id, body, cookie, csrf, review.ReviewETag)
		expectStatus(t, out, status)
		if out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private writer header missing")
		}
		var result service.RoleDefinitionResult
		if status != 200 {
			if out.Header().Get("ETag") != "" {
				t.Fatal("failed confirmation returned success ETag")
			}
			return result
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(out.Body.Bytes(), &result) != nil || json.Unmarshal(out.Body.Bytes(), &fields) != nil || len(fields) != 8 || result.ID != id || result.Name != body.Name || result.Description != body.Description || !slices.Equal(result.Permissions, body.Permissions) || result.IdentityETag != body.IdentityETag || result.Confirmation != "current_role_definition" || result.Effect != "current_database" || out.Header().Get("ETag") != strconv.Quote(result.ETag) {
			t.Fatal("role writer did not confirm exact current database contents")
		}
		for _, key := range []string{"id", "name", "permissions", "identity_etag", "etag", "confirmation", "effect"} {
			if _, ok := fields[key]; !ok {
				t.Fatal("missing current result field", key)
			}
		}
		return result
	}
	t.Run("independent-read-and-intrinsic-writer", func(t *testing.T) {
		review := roleDefinitionFixtureReview(t, router, adminCookie, id)
		reader := roleDefinitionFixtureReview(t, router, readerCookie, id)
		if reader.CanEdit || !review.CanEdit || reader.ReviewETag == review.ReviewETag {
			t.Fatal("actor-bound independent authority missing")
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", id, nil, noReadCookie, "", ""), 403)
		before := readRole()
		perms := readPermissions()
		count := len(audits())
		put(readerCookie, readerCSRF, reader, input(reader, "Reader denied", []string{}), 403)
		delegatedReview := roleDefinitionFixtureReview(t, router, delegatedCookie, id)
		put(delegatedCookie, delegatedCSRF, delegatedReview, input(delegatedReview, "Delegated denied", []string{}), 403)
		put(adminCookie, "", review, input(review, "Missing CSRF denied", []string{}), 403)
		if !reflect.DeepEqual(before, readRole()) || !reflect.DeepEqual(perms, readPermissions()) || count != len(audits()) {
			t.Fatal("denied editor changed role")
		}
		alias := "rol_" + strings.ToUpper(strings.TrimPrefix(id, "rol_"))
		if alias == id {
			t.Fatal("case alias test requires a distinct exact ID")
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", alias, nil, adminCookie, "", ""), 404)
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", id+"%20", nil, adminCookie, "", ""), 400)
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", id+"?limit=1", nil, adminCookie, "", ""), 400)
		builtin := roleDefinitionFixtureReview(t, router, adminCookie, "rol_admin")
		builtinProof := strings.Repeat("a", 64)
		if builtin.IdentityETag != nil {
			builtinProof = *builtin.IdentityETag
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "PUT", "rol_admin", service.RoleDefinitionInput{Name: builtin.Name, Description: "Builtin role remains read-only", Permissions: []string{}, IdentityETag: builtinProof, Reason: "Verify builtin definition denial"}, adminCookie, admin.CSRFToken, builtin.ReviewETag), 403)
		if builtin.CanEdit {
			t.Fatal("builtin editable")
		}
		if result, err := svc.SetReviewedRoleDefinition(ctx, strings.ToUpper(admin.User.ID), id, review.ReviewETag, input(review, "Alias denied", []string{})); err == nil || result != nil {
			t.Fatal("actor alias accepted")
		}
		// The read authority is deliberately removed from the builtin catalogue;
		// an already reviewed intrinsic admin write must not acquire roles.read.
		var readPermission entity.RolePermission
		if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "roles.read").First(&readPermission).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("role_id = ? AND permission = ?", "rol_admin", "roles.read").Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", id, nil, adminCookie, "", ""), 403)
		put(adminCookie, admin.CSRFToken, review, input(review, "Definition write without read", []string{"prices.read"}), 200)
		if err := db.Create(&readPermission).Error; err != nil {
			t.Fatal(err)
		}
	})
	t.Run("pending-intrinsic-admin-cannot-borrow-admission", func(t *testing.T) {
		review := roleDefinitionFixtureReview(t, router, adminCookie, id)
		before := readRole()
		prior := audits()
		if result, err := svc.GetRoleDefinition(ctx, pendingUser.ID, id); !errors.Is(err, apperrors.ErrUnauthorized) || result != nil {
			t.Fatal("pending actor read borrowed intrinsic identity", err)
		}
		if result, err := svc.SetReviewedRoleDefinition(ctx, pendingUser.ID, id, review.ReviewETag, input(review, "Pending actor denied", []string{})); !errors.Is(err, apperrors.ErrUnauthorized) || result != nil {
			t.Fatal("pending actor write borrowed intrinsic identity", err)
		}
		if !reflect.DeepEqual(before, readRole()) || !reflect.DeepEqual(prior, audits()) {
			t.Fatal("pending admission denial mutated definition")
		}
		approvalReview, err := svc.GetMemberApproval(ctx, admin.User.ID, pendingUser.ID)
		if err != nil {
			t.Fatal(err)
		}
		decision, decisionErr := svc.SetMemberApproval(ctx, admin.User.ID, pendingUser.ID, approvalReview.ReviewETag, service.MemberApprovalInput{Decision: "reject", Reason: "Retain denied definition actor"})
		// This service deliberately has no runtime instance. A saved rejection
		// can remain unconfirmed503; it must still deny before-use authority.
		if decisionErr != nil {
			var app *apperrors.Error
			if !errors.As(decisionErr, &app) || app.Code != 503 {
				t.Fatal("unexpected rejection outcome", decisionErr)
			}
		} else if decision == nil || decision.Decision != "rejected" {
			t.Fatal("wrong current rejection result")
		}
		var application entity.RegistrationApprovalApplication
		if pendingUser.ApprovalApplicationID == nil {
			t.Fatal("pending user lacks exact application link")
		}
		if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, *pendingUser.ApprovalApplicationID)).First(&application).Error; err != nil || application.UserID != pendingUser.ID || application.State != "rejected" {
			t.Fatal("real rejection was not durable", err)
		}
		if result, err := svc.GetRoleDefinition(ctx, pendingUser.ID, id); !errors.Is(err, apperrors.ErrUnauthorized) || result != nil {
			t.Fatal("rejected actor read borrowed intrinsic identity", err)
		}
		if result, err := svc.SetReviewedRoleDefinition(ctx, pendingUser.ID, id, review.ReviewETag, input(review, "Rejected actor denied", []string{})); !errors.Is(err, apperrors.ErrUnauthorized) || result != nil {
			t.Fatal("rejected actor write borrowed intrinsic identity", err)
		}
		if !reflect.DeepEqual(before, readRole()) || !reflect.DeepEqual(prior, audits()) {
			t.Fatal("rejected admission denial changed definition")
		}
	})
	t.Run("atomic-change-noop-aba-and-portable-name-conflict", func(t *testing.T) {
		first := roleDefinitionFixtureReview(t, router, adminCookie, id)
		original := readRole()
		count := len(audits())
		body := input(first, "Definition changed", []string{"prices.read", "providers.read"})
		put(adminCookie, admin.CSRFToken, first, body, 200)
		changed := readRole()
		if changed.DefinitionRevision == original.DefinitionRevision || !changed.CreatedAt.Equal(original.CreatedAt) || len(audits()) != count+1 {
			t.Fatal("change did not preserve birth and advance atomic definition")
		}
		event := audits()[count]
		var details struct {
			RoleID string `json:"role_id"`
			Reason string `json:"reason"`
			Before struct {
				Name        string   `json:"name"`
				Permissions []string `json:"permissions"`
			} `json:"before"`
			After struct {
				Name        string   `json:"name"`
				Permissions []string `json:"permissions"`
			} `json:"after"`
		}
		if event.ActorID != admin.User.ID || event.ResourceType != "role" || event.DetailsJSON == nil || json.Unmarshal([]byte(*event.DetailsJSON), &details) != nil || details.RoleID != id || details.Reason != body.Reason || details.Before.Name != first.Name || !slices.Equal(details.Before.Permissions, first.Permissions) || details.After.Name != body.Name || !slices.Equal(details.After.Permissions, body.Permissions) {
			t.Fatal("typed atomic definition audit lost exact before/after")
		}
		stable := readRole()
		stablePermissions := readPermissions()
		stableAudits := audits()
		put(adminCookie, admin.CSRFToken, first, body, 200)
		if !reflect.DeepEqual(stable, readRole()) || !reflect.DeepEqual(stablePermissions, readPermissions()) || !reflect.DeepEqual(stableAudits, audits()) {
			t.Fatal("current stale retry wrote or claimed history")
		}
		changing := input(first, "Stale changing request", []string{})
		put(adminCookie, admin.CSRFToken, first, changing, 409)
		current := roleDefinitionFixtureReview(t, router, adminCookie, id)
		put(adminCookie, admin.CSRFToken, current, input(current, first.Name, first.Permissions), 200)
		restored := roleDefinitionFixtureReview(t, router, adminCookie, id)
		if restored.DefinitionETag == first.DefinitionETag || restored.ReviewETag == first.ReviewETag {
			t.Fatal("definition ABA lost private generation")
		}
		put(adminCookie, admin.CSRFToken, first, changing, 409)
		before := readRole()
		count = len(audits())
		put(adminCookie, admin.CSRFToken, restored, input(restored, other.Role.Name, []string{}), 409)
		if !reflect.DeepEqual(before, readRole()) || count != len(audits()) {
			t.Fatal("portable uniqueness conflict changed definition")
		}
	})
	t.Run("audit-rollback-and-independent-confirmation-outage", func(t *testing.T) {
		review := roleDefinitionFixtureReview(t, router, adminCookie, id)
		before := readRole()
		permissions := readPermissions()
		prior := audits()
		body := input(review, "Definition audit rollback", []string{})
		auditFail.Store(true)
		put(adminCookie, admin.CSRFToken, review, body, 503)
		auditFail.Store(false)
		if !reflect.DeepEqual(before, readRole()) || !reflect.DeepEqual(permissions, readPermissions()) || !reflect.DeepEqual(prior, audits()) {
			t.Fatal("audit failure committed partial definition")
		}
		body.Name = "Definition durable unknown confirmation"
		confirmationMode.Store(1)
		put(adminCookie, admin.CSRFToken, review, body, 503)
		confirmationMode.Store(0)
		confirmationArmed.Store(false)
		committed := readRole()
		committedPermissions := readPermissions()
		committedAudits := audits()
		if committed.Name != body.Name || len(committedAudits) != len(prior)+1 {
			t.Fatal("confirmation outage hid durable commit")
		}
		// Matching GET is merely a new review; the explicit original PUT alone
		// reconciles the exact current state without replaying its original audit.
		roleDefinitionFixtureReview(t, router, adminCookie, id)
		put(adminCookie, admin.CSRFToken, review, body, 200)
		if !reflect.DeepEqual(committed, readRole()) || !reflect.DeepEqual(committedPermissions, readPermissions()) || !reflect.DeepEqual(committedAudits, audits()) {
			t.Fatal("uncertain reconciliation replayed mutation")
		}
	})
	t.Run("postcommit-current-authority-and-retained-ineligible-actors", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			mode   int32
			status int
		}{{"demoted", 2, 403}, {"disabled", 3, 401}, {"offboarded", 4, 401}} {
			t.Run(test.name, func(t *testing.T) {
				var original entity.User
				if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, retainedAdmin.User.ID)).First(&original).Error; err != nil {
					t.Fatal(err)
				}
				review := roleDefinitionFixtureReview(t, router, secondCookie, id)
				body := input(review, "Definition committed before "+test.name, []string{})
				actorID := original.ID
				mutationActor.Store(&actorID)
				confirmationMode.Store(test.mode)
				beforeCount := len(audits())
				put(secondCookie, secondAuth.CSRFToken, review, body, test.status)
				confirmationMode.Store(0)
				mutationActor.Store(nil)
				if readRole().Name != body.Name || len(audits()) != beforeCount+1 {
					t.Fatal("postcommit denial was misreported as rollback")
				}
				committed := readRole()
				prior := audits()
				put(secondCookie, secondAuth.CSRFToken, review, body, test.status)
				if !reflect.DeepEqual(committed, readRole()) || !reflect.DeepEqual(prior, audits()) {
					t.Fatal("ineligible retry mutated current role")
				}
				// Restore only the controlled authority fields; preserve full unchanged
				// User/LastLogin/assignment/security/generation facts from real setup.
				if err := db.Session(&gorm.Session{NewDB: true}).Model(&entity.User{}).Where(database.ExactText(db, clause.Column{Name: "id"}, original.ID)).UpdateColumns(map[string]any{"role": original.Role, "disabled": original.Disabled, "offboarded_at": original.OffboardedAt}).Error; err != nil {
					t.Fatal(err)
				}
				put(secondCookie, secondAuth.CSRFToken, review, body, 200)
				if !reflect.DeepEqual(committed, readRole()) || !reflect.DeepEqual(prior, audits()) {
					t.Fatal("restored authority retry replayed historical change")
				}
			})
		}
		// An explicit delegated member remains denied even with current read data.
		review := roleDefinitionFixtureReview(t, router, delegatedCookie, id)
		put(delegatedCookie, delegatedCSRF, review, input(review, review.Name, review.Permissions), 403)
		if delegated.User.Role != entity.RoleMember {
			t.Fatal("delegated fixture unexpectedly intrinsic admin")
		}
	})
	t.Run("postcommit-exact-target-absence-is-not-success", func(t *testing.T) {
		review := roleDefinitionFixtureReview(t, router, adminCookie, deletedTarget.Role.ID)
		body := input(review, "Definition deleted after its write", []string{})
		confirmationMode.Store(5)
		out := roleDefinitionFixtureRequest(t, router, "PUT", deletedTarget.Role.ID, body, adminCookie, admin.CSRFToken, review.ReviewETag)
		confirmationMode.Store(0)
		expectStatus(t, out, 404)
		if out.Header().Get("ETag") != "" {
			t.Fatal("absent target returned current definition proof")
		}
		var count int64
		if err := db.Model(&entity.Role{}).Where(database.ExactText(db, clause.Column{Name: "id"}, deletedTarget.Role.ID)).Count(&count).Error; err != nil || count != 0 {
			t.Fatal("controlled target absence was not durable", err)
		}
		var prior []entity.AuditEvent
		if err := db.Where("action = ? AND resource_id = ?", "role.definition.update", deletedTarget.Role.ID).Find(&prior).Error; err != nil || len(prior) != 1 {
			t.Fatal("durable write lost audit before target deletion", err)
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "PUT", deletedTarget.Role.ID, body, adminCookie, admin.CSRFToken, review.ReviewETag), 404)
		var after []entity.AuditEvent
		if err := db.Where("action = ? AND resource_id = ?", "role.definition.update", deletedTarget.Role.ID).Find(&after).Error; err != nil || !reflect.DeepEqual(prior, after) {
			t.Fatal("absent target retry replayed change", err)
		}
	})
	t.Run("retained-safe-unknown-bounds-and-birth-continuity", func(t *testing.T) {
		unknown := entity.RolePermission{RoleID: id, Permission: "Historical.Safe"}
		if err := db.Create(&unknown).Error; err != nil {
			t.Fatal(err)
		}
		review := roleDefinitionFixtureReview(t, router, adminCookie, id)
		if !slices.Contains(review.Permissions, unknown.Permission) || slices.Contains(review.AvailablePermissions, unknown.Permission) {
			t.Fatal("retained unknown permission inferred assignability")
		}
		before := readRole()
		permissions := readPermissions()
		count := len(audits())
		put(adminCookie, admin.CSRFToken, review, input(review, review.Name, review.Permissions), 400)
		if !reflect.DeepEqual(before, readRole()) || !reflect.DeepEqual(permissions, readPermissions()) || count != len(audits()) {
			t.Fatal("unknown equal definition bypassed current assignability")
		}
		if err := db.Where("role_id = ? AND permission = ?", id, unknown.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		review = roleDefinitionFixtureReview(t, router, adminCookie, id)
		body := input(review, review.Name, review.Permissions)
		// Exact recreation with different persisted birth must conflict before even
		// same-content confirmation. No supported create flow reuses its old ID.
		original := readRole()
		if err := db.Session(&gorm.Session{NewDB: true}).Model(&entity.Role{}).Where(database.ExactText(db, clause.Column{Name: "id"}, id)).UpdateColumn("created_at", original.CreatedAt.Add(time.Second)).Error; err != nil {
			t.Fatal(err)
		}
		put(adminCookie, admin.CSRFToken, review, body, 409)
		if err := db.Session(&gorm.Session{NewDB: true}).Model(&entity.Role{}).Where(database.ExactText(db, clause.Column{Name: "id"}, id)).UpdateColumn("created_at", original.CreatedAt).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, readRole()) {
			t.Fatal("birth-only continuity probe was not exactly restored")
		}
		put(adminCookie, admin.CSRFToken, review, body, 200)
		extra := make([]entity.RolePermission, 0, 101)
		for i := 0; i < 101; i++ {
			extra = append(extra, entity.RolePermission{RoleID: id, Permission: fmt.Sprintf("history.%03d", i)})
		}
		if err := db.Create(&extra).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, roleDefinitionFixtureRequest(t, router, "GET", id, nil, adminCookie, "", ""), 422)
		if err := db.Where("role_id = ? AND permission IN ?", id, func() []string {
			result := make([]string, 0, len(extra))
			for _, row := range extra {
				result = append(result, row.Permission)
			}
			return result
		}()).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		roleDefinitionFixtureReview(t, router, adminCookie, id)
	})
	t.Run("competing-reviewed-definitions-serialize", func(t *testing.T) {
		review := roleDefinitionFixtureReview(t, router, adminCookie, id)
		count := len(audits())
		start := make(chan struct{})
		outputs := make(chan *httptest.ResponseRecorder, 2)
		var wg sync.WaitGroup
		for _, name := range []string{"Definition competing A", "Definition competing B"} {
			body := input(review, name, []string{})
			wg.Go(func() {
				<-start
				outputs <- roleDefinitionFixtureRequest(t, router, "PUT", id, body, adminCookie, admin.CSRFToken, review.ReviewETag)
			})
		}
		close(start)
		wg.Wait()
		close(outputs)
		counts := map[int]int{}
		for out := range outputs {
			counts[out.Code]++
			if out.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("concurrent writer lost privacy")
			}
		}
		if counts[200] != 1 || counts[409] != 1 || len(audits()) != count+1 {
			t.Fatal("reviewed competing writes did not serialize", counts)
		}
		roleDefinitionFixtureReview(t, router, adminCookie, id)
	})
	if !reflect.DeepEqual(baseline, snapshot()) {
		t.Fatal("role definition editor changed unrelated identity, assignment, native rights or immutable history")
	}
	// Restart the service against exactly the same database and retained Session.
	fresh, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	freshRouter := fox.New()
	New(fresh).RegisterRoutes(freshRouter)
	after := roleDefinitionFixtureReview(t, freshRouter, adminCookie, id)
	current := roleDefinitionFixtureReview(t, router, adminCookie, id)
	if !reflect.DeepEqual(after, current) || !reflect.DeepEqual(baseline, snapshot()) {
		t.Fatal("current definition or protected facts changed across service restart")
	}
}
