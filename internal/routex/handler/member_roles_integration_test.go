package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type memberRolesFixtureSummary struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Builtin         bool                       `json:"builtin"`
	AssignmentKind  service.RoleAssignmentKind `json:"assignment_kind"`
	PermissionCount int                        `json:"permission_count"`
	DefinitionETag  string                     `json:"definition_etag"`
}

func memberRolesFixtureSummaryFields(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 {
		t.Fatal("incomplete compact Role summary")
	}
	for _, key := range []string{"id", "name", "builtin", "assignment_kind", "permission_count", "definition_etag"} {
		if _, ok := fields[key]; !ok {
			t.Fatal("missing compact Role summary field", key)
		}
	}
}

type memberRolesFixtureWorkspace struct {
	UserID               string                      `json:"user_id"`
	ObservedAt           time.Time                   `json:"observed_at"`
	IdentityRole         string                      `json:"identity_role"`
	SubjectStatus        string                      `json:"subject_status"`
	BuiltinRole          memberRolesFixtureSummary   `json:"builtin_role"`
	AssignedRoles        []memberRolesFixtureSummary `json:"assigned_roles"`
	EffectivePermissions []string                    `json:"effective_permissions"`
	PermissionUse        string                      `json:"permission_use"`
	ETag                 string                      `json:"etag"`
	CanEdit              bool                        `json:"can_edit"`
	EditBlockers         []string                    `json:"edit_blockers"`
	CandidateStatus      string                      `json:"candidate_status"`
}
type memberRolesFixtureProof struct {
	ID   string `json:"id"`
	ETag string `json:"etag"`
}
type memberRolesFixtureInput struct {
	RoleIDs               []string                  `json:"role_ids"`
	RoleDefinitions       []memberRolesFixtureProof `json:"role_definitions"`
	BuiltinDefinitionETag string                    `json:"builtin_definition_etag"`
	Reason                string                    `json:"reason"`
}
type memberRolesFixtureResult struct {
	UserID       string   `json:"user_id"`
	RoleIDs      []string `json:"role_ids"`
	ETag         string   `json:"etag"`
	Confirmation string   `json:"confirmation"`
	Effect       string   `json:"effect"`
}

// Root alone registers and executes the fixture on the real supported drivers.
// No native requests, synthetic Session proof, or runtime bypass are involved.
func testMemberRolesLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	type marker struct{}
	var queryMu sync.Mutex
	var queries []string
	var failedStatements []memberRolesFixtureStatementFailure
	const queryCallback = "test:member-roles-query"
	observer := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(marker{}) == true {
			queryMu.Lock()
			queries = append(queries, tx.Statement.SQL.String())
			if tx.Error != nil {
				failedStatements = append(failedStatements, memberRolesFixtureStatementDiagnostic(tx))
			}
			queryMu.Unlock()
		}
	}
	startStatement := func(tx *gorm.DB) {
		if tx.Statement.Context.Value(marker{}) == true {
			tx.Statement.Settings.Store("test:member-roles-start", time.Now())
		}
	}
	for _, register := range []func() error{
		func() error {
			return db.Callback().Query().Before("gorm:query").Register(queryCallback+":start", startStatement)
		},
		func() error {
			return db.Callback().Row().Before("gorm:row").Register(queryCallback+":start", startStatement)
		},
		func() error { return db.Callback().Query().After("gorm:query").Register(queryCallback, observer) },
		func() error { return db.Callback().Row().After("gorm:row").Register(queryCallback, observer) },
	} {
		if err := register(); err != nil {
			t.Fatal(err)
		}
	}
	defer func() {
		_ = db.Callback().Query().Remove(queryCallback)
		_ = db.Callback().Row().Remove(queryCallback)
		_ = db.Callback().Query().Remove(queryCallback + ":start")
		_ = db.Callback().Row().Remove(queryCallback + ":start")
	}()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"roles-admin@example.invalid","password":"roles-test-password","name":"Roles administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, subjectCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-subject", nil)
	_, readCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-both-read", []string{"members.read", "roles.read"})
	_, memberReadCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-member-read", []string{"members.read"})
	_, roleReadCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-role-read", []string{"roles.read"})
	delegated, delegatedCookie, delegatedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "roles-delegated", []string{"members.write", "members.read", "roles.read"})
	if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	path := func(id string) string { return "/api/v1/admin/members/" + id + "/roles" }
	request := func(method, p string, body any, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
		t.Helper()
		var encoded []byte
		if body != nil {
			var err error
			encoded, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, "http://routex.test"+p, strings.NewReader(string(encoded)))
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
	get := func(cookie *http.Cookie, id string) memberRolesFixtureWorkspace {
		t.Helper()
		out := request("GET", path(id), nil, cookie, "", "")
		expectStatus(t, out, 200)
		var value memberRolesFixtureWorkspace
		var fields map[string]json.RawMessage
		if json.Unmarshal(out.Body.Bytes(), &value) != nil || json.Unmarshal(out.Body.Bytes(), &fields) != nil || len(fields) != 12 || value.UserID != id || value.ObservedAt.IsZero() || value.ObservedAt.Location() != time.UTC || out.Header().Get("ETag") != strconv.Quote(value.ETag) || out.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("invalid scoped workspace", out.Body.String(), out.Header())
		}
		if value.AssignedRoles == nil || value.EffectivePermissions == nil || value.EditBlockers == nil || !slices.IsSorted(value.EffectivePermissions) {
			t.Fatal("incomplete projection", value)
		}
		memberRolesFixtureSummaryFields(t, fields["builtin_role"])
		if value.BuiltinRole.AssignmentKind != service.RoleAssignmentIntrinsic {
			t.Fatal("base Role is not intrinsic")
		}
		var assignedFields []json.RawMessage
		if json.Unmarshal(fields["assigned_roles"], &assignedFields) != nil || len(assignedFields) != len(value.AssignedRoles) {
			t.Fatal("incomplete assigned Role summaries")
		}
		for i, row := range value.AssignedRoles {
			memberRolesFixtureSummaryFields(t, assignedFields[i])
			if row.AssignmentKind != service.RoleAssignmentExplicit || len(row.DefinitionETag) != 64 || row.PermissionCount < 0 || row.PermissionCount > 100 || i > 0 && value.AssignedRoles[i-1].ID >= row.ID {
				t.Fatal("invalid assigned summary", row)
			}
		}
		return value
	}
	detail := func(cookie *http.Cookie, id, roleID, etag string) memberRolesFixtureSummary {
		t.Helper()
		out := request("GET", path(id)+"/"+roleID, nil, cookie, "", etag)
		expectStatus(t, out, 200)
		var value struct {
			UserID      string                    `json:"user_id"`
			Role        memberRolesFixtureSummary `json:"role"`
			Permissions []string                  `json:"permissions"`
			ETag        string                    `json:"etag"`
		}
		if json.Unmarshal(out.Body.Bytes(), &value) != nil || value.UserID != id || value.Role.ID != roleID || value.ETag != etag || out.Header().Get("ETag") != strconv.Quote(etag) || len(value.Permissions) != value.Role.PermissionCount || !slices.IsSorted(value.Permissions) {
			t.Fatal(out.Body.String())
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(out.Body.Bytes(), &fields) != nil || value.Role.AssignmentKind != service.RoleAssignmentExplicit {
			t.Fatal("invalid detail assignment kind")
		}
		memberRolesFixtureSummaryFields(t, fields["role"])
		return value.Role
	}
	input := func(review memberRolesFixtureWorkspace, ids []string) memberRolesFixtureInput {
		t.Helper()
		ids = slices.Clone(ids)
		if ids == nil {
			ids = []string{}
		}
		slices.Sort(ids)
		result := memberRolesFixtureInput{RoleIDs: ids, RoleDefinitions: []memberRolesFixtureProof{}, BuiltinDefinitionETag: review.BuiltinRole.DefinitionETag, Reason: "Reviewed custom platform roles"}
		for _, id := range ids {
			row := detail(adminCookie, review.UserID, id, review.ETag)
			result.RoleDefinitions = append(result.RoleDefinitions, memberRolesFixtureProof{ID: id, ETag: row.DefinitionETag})
		}
		return result
	}
	put := func(cookie *http.Cookie, csrf string, review memberRolesFixtureWorkspace, body memberRolesFixtureInput, status int) memberRolesFixtureResult {
		t.Helper()
		out := request("PUT", path(review.UserID), body, cookie, csrf, review.ETag)
		expectStatus(t, out, status)
		var result memberRolesFixtureResult
		if status == 200 {
			var fields map[string]json.RawMessage
			if json.Unmarshal(out.Body.Bytes(), &result) != nil || json.Unmarshal(out.Body.Bytes(), &fields) != nil || len(fields) != 5 || result.UserID != review.UserID || !reflect.DeepEqual(result.RoleIDs, body.RoleIDs) || result.Confirmation != "current_member_roles" || result.Effect != "current_database" || out.Header().Get("ETag") != strconv.Quote(result.ETag) {
				t.Fatal("current state was not confirmed", out.Body.String())
			}
		}
		return result
	}
	user := func(id string) entity.User {
		t.Helper()
		var row entity.User
		if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, id)).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	audits := func() int64 {
		t.Helper()
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.roles.update", subject.User.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	assignments := func(id string) []entity.UserRole {
		t.Helper()
		var rows []entity.UserRole
		if err := db.Where(database.ExactText(db, clause.Column{Name: "user_id"}, id)).Order("role_id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		return rows
	}
	protected := func() map[string][]map[string]any {
		t.Helper()
		out := map[string][]map[string]any{}
		for _, table := range []string{"sessions", "api_keys", "api_key_models", "user_model_grants", "team_model_grants", "resource_limits", "models", "model_names", "provider_models", "teams", "team_memberships", "call_records", "call_attempts"} {
			var rows []map[string]any
			if err := db.Table(table).Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(rows, func(a, b map[string]any) int {
				left, _ := json.Marshal(a)
				right, _ := json.Marshal(b)
				return strings.Compare(string(left), string(right))
			})
			out[table] = rows
		}
		return out
	}

	t.Run("independent-authority-and-scoped-reads", func(t *testing.T) {
		for _, cookie := range []*http.Cookie{memberReadCookie, roleReadCookie, subjectCookie} {
			expectStatus(t, request("GET", path(subject.User.ID), nil, cookie, "", ""), 403)
		}
		expectStatus(t, request("GET", path(subject.User.ID), nil, nil, "", ""), 401)
		review := get(readCookie, subject.User.ID)
		if review.CanEdit || review.CandidateStatus != "not_authorized" || !slices.Contains(review.EditBlockers, "not_platform_admin") {
			t.Fatal(review)
		}
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates", nil, readCookie, "", review.ETag), 403)
		expectStatus(t, request("GET", path(subject.User.ID)+"?limit=1", nil, adminCookie, "", ""), 400)
		expectStatus(t, request("GET", path(strings.ToUpper(subject.User.ID)), nil, adminCookie, "", ""), 404)
		expectStatus(t, request("GET", path(subject.User.ID+"%20"), nil, adminCookie, "", ""), 400)
		expectStatus(t, request("GET", path(subject.User.ID)+"%20", nil, adminCookie, "", ""), 404)
		valid := get(adminCookie, subject.User.ID)
		if !valid.CanEdit || valid.BuiltinRole.ID != "rol_member" || !valid.BuiltinRole.Builtin || valid.PermissionUse != "active" {
			t.Fatal(valid)
		}
		put(delegatedCookie, delegatedCSRF, valid, input(valid, nil), 403)
		for _, actor := range []string{strings.ToUpper(admin.User.ID), admin.User.ID + " "} {
			if _, err := svc.GetMemberRoles(ctx, actor, subject.User.ID); err == nil {
				t.Fatal("actor alias acquired authority")
			}
		}
	})
	first, err := svc.SaveRole(ctx, admin.User.ID, "", "Roles exact first", []string{"prices.read"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.SaveRole(ctx, admin.User.ID, "", "Roles exact second", []string{"providers.read"})
	if err != nil {
		t.Fatal(err)
	}
	review := get(adminCookie, subject.User.ID)
	t.Run("candidates-literal-cursor-and-detail", func(t *testing.T) {
		page := request("GET", path(subject.User.ID)+"/candidates?limit=1", nil, adminCookie, "", review.ETag)
		expectStatus(t, page, 200)
		var value struct {
			Items      []memberRolesFixtureSummary `json:"items"`
			NextCursor *string                     `json:"next_cursor"`
			ETag       string                      `json:"etag"`
		}
		if json.Unmarshal(page.Body.Bytes(), &value) != nil || len(value.Items) != 1 || value.NextCursor == nil || value.ETag != review.ETag {
			t.Fatal(page.Body.String())
		}
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates?limit=1&cursor="+url.QueryEscape(*value.NextCursor), nil, adminCookie, "", review.ETag), 200)
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates?q=changed&cursor="+url.QueryEscape(*value.NextCursor), nil, adminCookie, "", review.ETag), 409)
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates?q=%25", nil, adminCookie, "", review.ETag), 200)
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates?limit=51", nil, adminCookie, "", review.ETag), 400)
		expectStatus(t, request("GET", path(subject.User.ID)+"/candidates?limit=1&limit=2", nil, adminCookie, "", review.ETag), 400)
		detail(adminCookie, subject.User.ID, first.Role.ID, review.ETag)
		expectStatus(t, request("GET", path(subject.User.ID)+"/"+strings.ToUpper(first.Role.ID), nil, adminCookie, "", review.ETag), 404)
		expectStatus(t, request("GET", path(subject.User.ID)+"/"+first.Role.ID, nil, readCookie, "", get(readCookie, subject.User.ID).ETag), 404)
	})
	original := input(review, []string{first.Role.ID})
	baselineUser, baselineProtected := user(subject.User.ID), protected()
	put(adminCookie, admin.CSRFToken, review, original, 200)
	savedUser, savedAssignments, savedAudits := user(subject.User.ID), assignments(subject.User.ID), audits()
	if savedUser.MemberRoleRevision == baselineUser.MemberRoleRevision || savedAudits < 1 {
		t.Fatal("assignment revision/audit missing")
	}
	compare := savedUser
	compare.MemberRoleRevision = baselineUser.MemberRoleRevision
	compare.UpdatedAt = baselineUser.UpdatedAt
	if !reflect.DeepEqual(compare, baselineUser) || !reflect.DeepEqual(protected(), baselineProtected) {
		t.Fatal("custom assignment changed unrelated facts")
	}
	put(adminCookie, admin.CSRFToken, review, original, 200)
	if !reflect.DeepEqual(user(subject.User.ID), savedUser) || !reflect.DeepEqual(assignments(subject.User.ID), savedAssignments) || audits() != savedAudits {
		t.Fatal("current retry rewrote/audited")
	}
	t.Run("creation-and-definition-no-op-generations", func(t *testing.T) {
		if savedUser.MemberRoleRevision == strings.Repeat("0", 64) || baselineUser.MemberRoleRevision == strings.Repeat("0", 64) {
			t.Fatal("real creation/assignment did not initialize local revision")
		}
		var before entity.Role
		if err := db.First(&before, "id = ?", first.Role.ID).Error; err != nil {
			t.Fatal(err)
		}
		var n int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "role.save", first.Role.ID).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SaveRole(ctx, admin.User.ID, first.Role.ID, before.Name, []string{"prices.read"}); err != nil {
			t.Fatal(err)
		}
		var after entity.Role
		if err := db.First(&after, "id = ?", first.Role.ID).Error; err != nil {
			t.Fatal(err)
		}
		var current int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "role.save", first.Role.ID).Count(&current).Error; err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) || n != current {
			t.Fatal("exact role definition no-op advanced generation/audit")
		}
		previous := user(subject.User.ID)
		rows := assignments(subject.User.ID)
		count := audits()
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{first.Role.ID}); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(previous, user(subject.User.ID)) || !reflect.DeepEqual(rows, assignments(subject.User.ID)) || audits() != count {
			t.Fatal("legacy service no-op replaced reviewed engine")
		}
	})
	t.Run("definition-proof-and-assignment-ABA", func(t *testing.T) {
		current := get(adminCookie, subject.User.ID)
		old := input(current, []string{second.Role.ID})
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{first.Role.ID}); err != nil {
			t.Fatal(err)
		}
		if get(adminCookie, subject.User.ID).ETag == current.ETag {
			t.Fatal("assignment ABA kept review")
		}
		put(adminCookie, admin.CSRFToken, current, old, 409)
		selected := get(adminCookie, subject.User.ID)
		noOp := input(selected, []string{first.Role.ID})
		if _, err := svc.SaveRole(ctx, admin.User.ID, first.Role.ID, "Roles exact first changed", []string{"prices.read"}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SaveRole(ctx, admin.User.ID, first.Role.ID, "Roles exact first", []string{"prices.read"}); err != nil {
			t.Fatal(err)
		}
		put(adminCookie, admin.CSRFToken, selected, noOp, 409)
		current = get(adminCookie, subject.User.ID)
		fresh := input(current, []string{first.Role.ID})
		before := user(subject.User.ID)
		n := audits()
		if _, err := svc.SaveRole(ctx, admin.User.ID, "", "Unrelated candidate churn", []string{}); err != nil {
			t.Fatal(err)
		}
		put(adminCookie, admin.CSRFToken, current, fresh, 200)
		if !reflect.DeepEqual(user(subject.User.ID), before) || audits() != n {
			t.Fatal("matching current state depended on unrelated candidate churn")
		}
	})
	t.Run("strict-body-and-retained-permissions", func(t *testing.T) {
		current := get(adminCookie, subject.User.ID)
		body := input(current, []string{first.Role.ID})
		expectStatus(t, request("PUT", path(subject.User.ID), body, adminCookie, admin.CSRFToken, ""), 400)
		body.Reason = strings.Repeat("x", 1025)
		put(adminCookie, admin.CSRFToken, current, body, 400)
		body = input(current, []string{first.Role.ID})
		body.RoleDefinitions[0].ETag = strings.Repeat("a", 64)
		put(adminCookie, admin.CSRFToken, current, body, 409)
		unknown := entity.RolePermission{RoleID: first.Role.ID, Permission: "retained.future_permission"}
		if err := db.Create(&unknown).Error; err != nil {
			t.Fatal(err)
		}
		retained := get(adminCookie, subject.User.ID)
		row := detail(adminCookie, subject.User.ID, first.Role.ID, retained.ETag)
		if row.PermissionCount != 2 || slices.Contains(retained.EffectivePermissions, unknown.Permission) {
			t.Fatal("unknown retained permission inferred implemented authority")
		}
		if err := db.Where("role_id = ? AND permission = ?", unknown.RoleID, unknown.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		alias := entity.UserRole{UserID: subject.User.ID, RoleID: strings.ToUpper(first.Role.ID)}
		if err := db.Create(&alias).Error; err == nil {
			expectStatus(t, request("GET", path(subject.User.ID), nil, adminCookie, "", ""), 503)
			if err := db.Where(database.ExactText(db, clause.Column{Name: "user_id"}, alias.UserID)).Where(database.ExactText(db, clause.Column{Name: "role_id"}, alias.RoleID)).Delete(&entity.UserRole{}).Error; err != nil {
				t.Fatal(err)
			}
		} else {
			t.Log("existing identity constraint rejected case-alias assignment")
		}
	})
	t.Run("builtin-proof-mismatch", func(t *testing.T) {
		current := get(adminCookie, subject.User.ID)
		body := input(current, []string{first.Role.ID})
		body.BuiltinDefinitionETag = strings.Repeat("f", 64)
		put(adminCookie, admin.CSRFToken, current, body, 409)
	})
	t.Run("audit-rollback-and-lost-confirmation", func(t *testing.T) {
		current := get(adminCookie, subject.User.ID)
		body := input(current, []string{second.Role.ID})
		before, rows, n := user(subject.User.ID), assignments(subject.User.ID), audits()
		const callback = "test:roles-audit-rollback"
		if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.roles.update" {
				_ = tx.AddError(errors.New("controlled audit failure"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		out := request("PUT", path(subject.User.ID), body, adminCookie, admin.CSRFToken, current.ETag)
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Fatal(err)
		}
		expectStatus(t, out, 500)
		if !reflect.DeepEqual(user(subject.User.ID), before) || !reflect.DeepEqual(assignments(subject.User.ID), rows) || audits() != n {
			t.Fatal("audit failure partially applied")
		}
		var armed atomic.Bool
		const arm = "test:roles-confirmation-arm"
		const fail = "test:roles-confirmation-fail"
		if err := db.Callback().Create().After("gorm:create").Register(arm, func(tx *gorm.DB) {
			if row, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && row.Action == "member.roles.update" && tx.Error == nil {
				armed.Store(true)
			}
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.Callback().Query().Before("gorm:query").Register(fail, func(tx *gorm.DB) {
			if armed.Load() && tx.Statement.Table == "users" {
				_ = tx.AddError(errors.New("controlled confirmation outage"))
			}
		}); err != nil {
			t.Fatal(err)
		}
		out = request("PUT", path(subject.User.ID), body, adminCookie, admin.CSRFToken, current.ETag)
		_ = db.Callback().Create().Remove(arm)
		_ = db.Callback().Query().Remove(fail)
		expectStatus(t, out, 503)
		committed, n := user(subject.User.ID), audits()
		if n < 1 || len(assignments(subject.User.ID)) != 1 || assignments(subject.User.ID)[0].RoleID != second.Role.ID {
			t.Fatal("uncertain commit missing")
		}
		put(adminCookie, admin.CSRFToken, current, body, 200)
		if !reflect.DeepEqual(user(subject.User.ID), committed) || audits() != n {
			t.Fatal("uncertain retry repeated write")
		}
		var event entity.AuditEvent
		if err := db.Where("action = ? AND resource_id = ?", "member.roles.update", subject.User.ID).Order("created_at DESC,id DESC").First(&event).Error; err != nil {
			t.Fatal(err)
		}
		if event.ActorID != admin.User.ID || event.DetailsJSON == nil || len(*event.DetailsJSON) > 60*1024 {
			t.Fatal("typed audit incomplete or oversized")
		}
		var projection struct {
			UserID string `json:"user_id"`
			Reason string `json:"reason"`
			Before struct {
				IdentityRole         string   `json:"identity_role"`
				RoleIDs              []string `json:"role_ids"`
				EffectivePermissions []string `json:"effective_permissions"`
			} `json:"before"`
			After struct {
				IdentityRole         string   `json:"identity_role"`
				RoleIDs              []string `json:"role_ids"`
				EffectivePermissions []string `json:"effective_permissions"`
			} `json:"after"`
		}
		if json.Unmarshal([]byte(*event.DetailsJSON), &projection) != nil || projection.UserID != subject.User.ID || projection.Reason != body.Reason || !reflect.DeepEqual(projection.After.RoleIDs, body.RoleIDs) || projection.Before.IdentityRole != "member" {
			t.Fatal("typed audit content", *event.DetailsJSON)
		}
	})
	t.Run("base-lifecycle-and-offboarding-generations", func(t *testing.T) {
		current := get(adminCookie, subject.User.ID)
		body := input(current, []string{})
		role := entity.RoleAdmin
		if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, nil, &role); err != nil {
			t.Fatal(err)
		}
		role = entity.RoleMember
		if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, nil, &role); err != nil {
			t.Fatal(err)
		}
		put(adminCookie, admin.CSRFToken, current, body, 409)
		current = get(adminCookie, subject.User.ID)
		body = input(current, []string{})
		disabled := true
		if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, &disabled, nil); err != nil {
			t.Fatal(err)
		}
		inactive := get(adminCookie, subject.User.ID)
		if inactive.SubjectStatus != "disabled" || inactive.PermissionUse != "inactive" || !inactive.CanEdit {
			t.Fatal(inactive)
		}
		disabled = false
		if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, &disabled, nil); err != nil {
			t.Fatal(err)
		}
		put(adminCookie, admin.CSRFToken, current, body, 409)
		departure, err := svc.EmergencyOffboarding(ctx, admin.User.ID, subject.User.ID, service.OffboardingEmergencyInput{RequestID: "req_roles_depart", CurrentPassword: "roles-test-password", Reason: "Roles fixture terminal departure"})
		if err != nil || departure == nil {
			t.Fatal(err)
		}
		terminal := get(adminCookie, subject.User.ID)
		if terminal.CanEdit || terminal.SubjectStatus != "offboarded" || terminal.PermissionUse != "inactive" {
			t.Fatal(terminal)
		}
		terminalBody := memberRolesFixtureInput{RoleIDs: []string{}, RoleDefinitions: []memberRolesFixtureProof{}, BuiltinDefinitionETag: terminal.BuiltinRole.DefinitionETag, Reason: "Terminal denied"}
		put(adminCookie, admin.CSRFToken, terminal, terminalBody, 409)
		disabled = false
		if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.User.ID, &disabled, nil); err != nil {
			t.Fatal(err)
		}
		if get(adminCookie, subject.User.ID).ETag == current.ETag {
			t.Fatal("offboard/reactivate restored generation")
		}
	})
	t.Run("all-admin-helper-calls-exact", func(t *testing.T) {
		for _, actor := range []string{strings.ToUpper(admin.User.ID), admin.User.ID + " ", delegated.User.ID} {
			if _, err := svc.SaveRole(ctx, actor, "", "Denied exact helper", []string{}); err == nil {
				t.Fatal("SaveRole alias/delegation")
			}
			if err := svc.DeleteRole(ctx, actor, first.Role.ID); err == nil {
				t.Fatal("DeleteRole alias/delegation")
			}
			if _, err := svc.SetMemberRoles(ctx, actor, subject.User.ID, []string{}); err == nil {
				t.Fatal("assignment alias/delegation")
			}
			if err := svc.SetRegistrationEnabled(ctx, actor, true); err == nil {
				t.Fatal("registration alias/delegation")
			}
			if _, err := svc.CreateMember(ctx, actor, "denied-helper@example.invalid", "roles-test-password", "Denied", "admin"); err == nil {
				t.Fatal("admin creation alias/delegation")
			}
		}
	})
	t.Run("inactive-offboarded-admin-helper", func(t *testing.T) {
		retained, err := svc.CreateMember(ctx, admin.User.ID, "roles-other-admin@example.invalid", "roles-test-password", "Other admin", entity.RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		for _, state := range []string{"disabled", "offboarded"} {
			change := map[string]any{"disabled": state == "disabled", "offboarded_at": nil}
			if state == "offboarded" {
				change["offboarded_at"] = time.Now().UTC().Truncate(time.Millisecond)
			}
			if err := db.Model(&entity.User{}).Where("id = ?", retained.User.ID).Updates(change).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.SaveRole(ctx, retained.User.ID, "", "Inactive denied", nil); err == nil {
				t.Fatal(state, "SaveRole")
			}
			if err := svc.DeleteRole(ctx, retained.User.ID, first.Role.ID); err == nil {
				t.Fatal(state, "DeleteRole")
			}
			if _, err := svc.SetMemberRoles(ctx, retained.User.ID, subject.User.ID, nil); err == nil {
				t.Fatal(state, "assignment")
			}
			if err := svc.SetRegistrationEnabled(ctx, retained.User.ID, true); err == nil {
				t.Fatal(state, "registration")
			}
			if _, err := svc.CreateMember(ctx, retained.User.ID, "roles-denied-admin@example.invalid", "roles-test-password", "Denied admin", entity.RoleAdmin); err == nil {
				t.Fatal(state, "creation")
			}
		}
	})
	t.Run("complete-history-and-query-bounds", func(t *testing.T) {
		bulk := make([]entity.Role, 10001)
		links := make([]entity.UserRole, len(bulk))
		for i := range bulk {
			name := fmt.Sprintf("Roles retained %05d", i)
			bulk[i] = entity.Role{ID: fmt.Sprintf("rol_rolehist_%05d", i), Name: name, NameKey: secret.SHA256Hex(name)}
			links[i] = entity.UserRole{UserID: subject.User.ID, RoleID: bulk[i].ID}
		}
		if err := db.CreateInBatches(bulk[:101], 100).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.CreateInBatches(links[:101], 100).Error; err != nil {
			t.Fatal(err)
		}
		retained101 := get(adminCookie, subject.User.ID)
		if len(retained101.AssignedRoles) != 101 || !retained101.CanEdit {
			t.Fatal("retained 101 cannot be deliberately reduced")
		}
		put(adminCookie, admin.CSRFToken, retained101, input(retained101, nil), 200)
		if err := db.CreateInBatches(links[:101], 100).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.CreateInBatches(bulk[101:], 200).Error; err != nil {
			t.Fatal(err)
		}
		measure := func(want int) memberRolesFixtureWorkspace {
			t.Helper()
			queryMu.Lock()
			queries = nil
			failedStatements = nil
			queryMu.Unlock()
			started := time.Now()
			value, err := svc.GetMemberRoles(context.WithValue(ctx, marker{}, true), admin.User.ID, subject.User.ID)
			if err != nil {
				queryMu.Lock()
				diagnostics := slices.Clone(failedStatements)
				queryMu.Unlock()
				for _, diagnostic := range diagnostics {
					t.Logf("failed bounded Roles statement: error_type=%s message=%q context_error=%q elapsed=%s", diagnostic.ErrorType, diagnostic.Message, diagnostic.ContextError, diagnostic.Elapsed)
				}
				t.Fatalf("bounded Roles read failed: error_type=%T error=%v elapsed=%s failed_statements=%d", err, err, time.Since(started), len(diagnostics))
			}
			queryMu.Lock()
			captured := slices.Clone(queries)
			queryMu.Unlock()
			if len(captured) > 16+2*((want+500)/500) {
				t.Fatal("per-role SQL", len(captured))
			}
			for _, sql := range captured {
				if strings.Contains(sql, "call_records") || strings.Contains(sql, "api_keys") || strings.Contains(sql, "teams") {
					t.Fatal("unrelated query", sql)
				}
			}
			out := get(adminCookie, subject.User.ID)
			if value == nil || len(out.AssignedRoles) != want {
				t.Fatal("complete retained rows", len(out.AssignedRoles), want)
			}
			t.Log("bounded Roles statements", want, len(captured))
			return out
		}
		old101 := measure(101)
		if old101.CandidateStatus != "overflow" || old101.CanEdit {
			t.Fatal("catalogue bound", old101.CandidateStatus)
		}
		if err := db.CreateInBatches(links[101:1001], 200).Error; err != nil {
			t.Fatal(err)
		}
		old1001 := measure(1001)
		if !slices.Contains(old1001.EditBlockers, "assignment_audit_bound") {
			t.Fatal("oversized before set editable")
		}
		permissions := make([]entity.RolePermission, 0, 100100)
		ids := make([]string, 1001)
		for i := range 1001 {
			ids[i] = bulk[i].ID
			for code := range 100 {
				permissions = append(permissions, entity.RolePermission{RoleID: bulk[i].ID, Permission: fmt.Sprintf("retained.bound_%03d", code)})
			}
		}
		if err := db.CreateInBatches(&permissions, 500).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request("GET", path(subject.User.ID), nil, adminCookie, "", ""), 422)
		if err := db.Where("role_id IN ?", ids).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.CreateInBatches(links[1001:10000], 200).Error; err != nil {
			t.Fatal(err)
		}
		measure(10000)
		if err := db.Create(&links[10000]).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, request("GET", path(subject.User.ID), nil, adminCookie, "", ""), 422)
		if err := db.Where("user_id = ? AND role_id IN ?", subject.User.ID, func() []string {
			ids := make([]string, len(bulk))
			for i := range bulk {
				ids[i] = bulk[i].ID
			}
			return ids
		}()).Delete(&entity.UserRole{}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Where("id LIKE ?", "rol_rolehist_%").Delete(&entity.Role{}).Error; err != nil {
			t.Fatal(err)
		}
	})
	// A new service instance reads the same local revisions and exact assignments.
	current := get(adminCookie, subject.User.ID)
	before := protected()
	fresh, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	value, err := fresh.GetMemberRoles(ctx, admin.User.ID, subject.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	var restarted memberRolesFixtureWorkspace
	if json.Unmarshal(encoded, &restarted) != nil || restarted.ETag != current.ETag || !reflect.DeepEqual(protected(), before) {
		t.Fatal("restart changed saved Roles/facts")
	}
	var native int64
	if err := db.Model(&entity.CallRecord{}).Count(&native).Error; err != nil || native != 0 {
		t.Fatal("Roles fixture produced inference", native, err)
	}
}

type memberRolesFixtureStatementFailure struct {
	ErrorType, Message, ContextError string
	Elapsed                          time.Duration
}

func memberRolesFixtureSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	upper := strings.ToUpper(message)
	for _, sensitive := range []string{"SELECT ", "INSERT ", "UPDATE ", "DELETE ", "WHERE ", "VALUES ", "SQL SYNTAX", "DETAIL:", "://", "USER=", "DATABASE=", "HOST=", "PASSWORD", "TOKEN", "SECRET", "AUTHORIZATION", "DSN"} {
		if strings.Contains(upper, sensitive) {
			return "statement error text omitted because it may contain SQL or credentials"
		}
	}
	message = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, message)
	runes := []rune(message)
	if len(runes) > 300 {
		message = string(runes[:300]) + "…"
	}
	return message
}

func memberRolesFixtureStatementDiagnostic(tx *gorm.DB) memberRolesFixtureStatementFailure {
	diagnostic := memberRolesFixtureStatementFailure{ErrorType: fmt.Sprintf("%T", tx.Error), Message: memberRolesFixtureSafeError(tx.Error), ContextError: memberRolesFixtureSafeError(tx.Statement.Context.Err())}
	if value, ok := tx.Statement.Settings.Load("test:member-roles-start"); ok {
		if started, ok := value.(time.Time); ok {
			diagnostic.Elapsed = time.Since(started)
		}
	}
	return diagnostic
}

func TestMemberRolesFixtureDiagnosticsExcludeSQLAndCredentials(t *testing.T) {
	for _, message := range []string{"SELECT role_id WHERE id = secret", "postgres://user:password@host/database", "token=private", "INSERT INTO users values (...)", "syntax failure near WHERE name=private", "failed to connect user=private host=private"} {
		if got := memberRolesFixtureSafeError(errors.New(message)); got != "statement error text omitted because it may contain SQL or credentials" {
			t.Fatal("unsafe diagnostic", got)
		}
	}
	for _, err := range []error{context.DeadlineExceeded, context.Canceled, errors.New("Error 1205 (HY000): Lock wait timeout exceeded; try restarting transaction")} {
		if got := memberRolesFixtureSafeError(err); got != err.Error() {
			t.Fatal("causal diagnostic lost", got)
		}
	}
	if memberRolesFixtureSafeError(nil) != "" || len([]rune(memberRolesFixtureSafeError(errors.New(strings.Repeat("x", 301))))) != 301 {
		t.Fatal("diagnostic bounds")
	}
}
