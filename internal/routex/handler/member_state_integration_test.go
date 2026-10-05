package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

// Root runs this on both real drivers. All retained credentials are pending;
// this fixture sends zero native requests and claims no inference readiness.
func testMemberStateLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New([]byte(strings.Repeat("s", 32)))
	if err != nil {
		t.Fatal(err)
	}
	router, svc := memberStateRuntimeFixtureRouter(t, db, service.WithCredentialStorage(store))
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"state-admin@example.invalid","password":"state-test-password","name":"State administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	subject, subjectCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "state-subject", nil)
	_, readerCookie, readerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "state-reader", []string{"members.read"})
	writer, writerCookie, writerCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "state-writer", []string{"members.write"})
	_, outsiderCookie, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "state-outsider", nil)
	path := func(id string) string { return "/api/v1/admin/members/" + id }
	user := func(id string) entity.User {
		t.Helper()
		var row entity.User
		if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, id)).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	audits := func(id string) int64 {
		t.Helper()
		var count int64
		if err := db.Model(&entity.AuditEvent{}).Where("action = ? AND resource_id = ?", "member.state.update", id).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	protected := func() map[string][]map[string]any {
		t.Helper()
		result := map[string][]map[string]any{}
		for _, table := range []string{"api_key_models", "user_model_grants", "team_model_grants", "project_model_grants", "resource_limits", "models", "model_names", "provider_models", "teams", "team_memberships", "projects", "project_managers", "call_records", "call_attempts"} {
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
	get := func(cookie *http.Cookie, id string) memberStateFixtureRecord {
		t.Helper()
		response := identityRequest(router, "GET", path(id)+"/state", "", cookie, "")
		record := decodeCatalogResponse[memberStateFixtureRecord](t, response, 200)
		var fields map[string]json.RawMessage
		if json.Unmarshal(response.Body.Bytes(), &fields) != nil || len(fields) != 11 || record.UserID != id || response.Header().Get("ETag") != strconv.Quote(record.ETag) || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("state read expanded private fields or lost validator", response.Body.String())
		}
		return record
	}
	patch := func(cookie *http.Cookie, csrf string, review memberStateFixtureRecord, fields any, want int) *httptest.ResponseRecorder {
		t.Helper()
		response := memberStateFixturePATCH(t, router, cookie, csrf, review.UserID, review.ETag, fields)
		expectStatus(t, response, want)
		if response.Code == 200 {
			var values map[string]json.RawMessage
			if json.Unmarshal(response.Body.Bytes(), &values) != nil || len(values) != 13 {
				t.Fatal("state confirmation projection expanded", response.Body.String())
			}
		}
		return response
	}
	roleBody := func(role string) map[string]any {
		return map[string]any{"role": role, "reason": "Reviewed base identity"}
	}
	statusBody := func(disabled bool) map[string]any {
		return map[string]any{"disabled": disabled, "reason": "Reviewed account access"}
	}

	t.Run("exact-scope-and-independent-permissions", func(t *testing.T) {
		read := get(readerCookie, subject.User.ID)
		write := get(writerCookie, subject.User.ID)
		if read.CanChangeStatus || read.CanChangeBaseRole || !write.CanChangeStatus || write.CanChangeBaseRole {
			t.Fatal("independent edit authority changed", read, write)
		}
		for _, id := range []string{writer.User.ID, admin.User.ID} {
			review := get(writerCookie, id)
			if review.CanChangeStatus || review.CanChangeBaseRole {
				t.Fatal("delegated protected target editable")
			}
			patch(writerCookie, writerCSRF, review, statusBody(review.Disabled), 403)
		}
		before := user(subject.User.ID)
		patch(readerCookie, readerCSRF, read, statusBody(true), 403)
		patch(writerCookie, writerCSRF, write, roleBody("member"), 403)
		if !reflect.DeepEqual(before, user(subject.User.ID)) || audits(subject.User.ID) != 0 {
			t.Fatal("denied state write changed subject")
		}
		expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"/state", "", outsiderCookie, ""), 403)
		expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members", "", writerCookie, ""), 403)
		expectStatus(t, identityRequest(router, "GET", path(strings.ToUpper(subject.User.ID))+"/state", "", adminCookie, ""), 404)
		expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"%20/state", "", adminCookie, ""), 400)
		expectStatus(t, identityRequest(router, "GET", path(subject.User.ID)+"/state?role=admin", "", adminCookie, ""), 400)
		if _, err := svc.GetMemberState(ctx, strings.ToUpper(admin.User.ID), subject.User.ID); err == nil {
			t.Fatal("actor collation alias acquired state authority")
		}
	})

	t.Run("strict-reviewed-body", func(t *testing.T) {
		review := get(adminCookie, subject.User.ID)
		before := user(subject.User.ID)
		for _, body := range []string{`{}`, `{"disabled":null,"reason":"x"}`, `{"disabled":true,"role":"member","reason":"x"}`, `{"disabled":true,"disabled":false,"reason":"x"}`, `{"disabled":true,"reason":"x","name":"hidden"}`, `{"role":"ADMIN","reason":"x"}`, `{"role":"member","reason":" "}`, `{"role":"member","reason":"\u0000"}`, `{"role":"member","reason":"\ud800"}`} {
			req := httptest.NewRequest("PATCH", "http://routex.test"+path(subject.User.ID), strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("If-Match", strconv.Quote(review.ETag))
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
			req.AddCookie(adminCookie)
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			expectStatus(t, out, 400)
		}
		patch(adminCookie, admin.CSRFToken, review, map[string]any{"disabled": true, "reason": strings.Repeat("x", 1025)}, 400)
		if !reflect.DeepEqual(before, user(subject.User.ID)) || audits(subject.User.ID) != 0 {
			t.Fatal("malformed write changed state")
		}
	})

	provider, err := svc.CreateProvider(ctx, admin.User.ID, "State retained provider", service.CreateConnectionInput{Name: "State connection", BaseURL: "https://state-setup.example.invalid/v1", Protocol: entity.ProtocolOpenAIChat, CredentialName: "State credential", Secret: "test-only-state-credential"})
	if err != nil {
		t.Fatal(err)
	}
	pm, err := svc.CreateProviderModel(ctx, admin.User.ID, provider.Connections[0].Connection.ID, "state-preserved-upstream")
	if err != nil {
		t.Fatal(err)
	}
	model, err := svc.CreateModel(ctx, admin.User.ID, "state-preserved-model", pm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, model.Model.ID, []string{subject.User.ID}); err != nil {
		t.Fatal(err)
	}
	created, err := svc.CreatePersonalKey(ctx, subject.User.ID, "Retained pending state Key", []string{model.Model.ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := func() entity.APIKey {
		t.Helper()
		var row entity.APIKey
		if err := db.Where(database.ExactText(db, clause.Column{Name: "id"}, created.Record.Key.ID)).First(&row).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	stable := protected()

	t.Run("base-aba-and-zero-write-current-reconciliation", func(t *testing.T) {
		original := get(adminCookie, subject.User.ID)
		before := user(subject.User.ID)
		patch(adminCookie, admin.CSRFToken, original, roleBody("admin"), 200)
		patch(adminCookie, admin.CSRFToken, get(adminCookie, subject.User.ID), roleBody("member"), 200)
		current := user(subject.User.ID)
		if current.MemberRoleRevision == before.MemberRoleRevision {
			t.Fatal("base identity ABA retained revision")
		}
		patch(adminCookie, admin.CSRFToken, original, roleBody("admin"), 409)
		beforeAudits := audits(subject.User.ID)
		patch(adminCookie, admin.CSRFToken, original, roleBody("member"), 200)
		if !reflect.DeepEqual(current, user(subject.User.ID)) || beforeAudits != audits(subject.User.ID) || !reflect.DeepEqual(stable, protected()) {
			t.Fatal("current reconciliation replayed mutation/history")
		}
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 200)
	})

	t.Run("metadata-label-review-coexistence", func(t *testing.T) {
		review := get(adminCookie, subject.User.ID)
		before := user(subject.User.ID)
		metadata, err := svc.GetMemberMetadata(ctx, admin.User.ID, subject.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberMetadata(ctx, admin.User.ID, subject.User.ID, metadata.ETag, service.MemberMetadataInput{Name: "Reviewed current target name", Reason: "Separate display label correction"}); err != nil {
			t.Fatal(err)
		}
		after := user(subject.User.ID)
		if after.MemberRoleRevision != before.MemberRoleRevision || after.Role != before.Role || after.Disabled != before.Disabled || after.Name == before.Name {
			t.Fatal("metadata altered lifecycle revision/state")
		}
		patch(adminCookie, admin.CSRFToken, review, roleBody("admin"), 409)
		beforeAudits := audits(subject.User.ID)
		confirmed := patch(adminCookie, admin.CSRFToken, review, roleBody("member"), 200)
		current := decodeCatalogResponse[memberStateFixtureResult](t, confirmed, 200)
		if current.Name != after.Name || !reflect.DeepEqual(after, user(subject.User.ID)) || beforeAudits != audits(subject.User.ID) {
			t.Fatal("current matching name reconciliation rewrote or confirmed old label")
		}
	})

	var failAudit, failPublication, armPublication atomic.Bool
	const createCallback = "state-fixture-audit-outage"
	const afterCallback = "state-fixture-publication-arm"
	const queryCallback = "state-fixture-publication-outage"
	if err := db.Callback().Create().Before("gorm:create").Register(createCallback, func(tx *gorm.DB) {
		if failAudit.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled state audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().After("gorm:create").Register(afterCallback, func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && event.Action == "member.state.update" && armPublication.Swap(false) {
			failPublication.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "providers" {
			_ = tx.AddError(errors.New("controlled state publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() {
		failAudit.Store(false)
		failPublication.Store(false)
		svc.StopRuntime()
		_ = db.Callback().Create().Remove(createCallback)
		_ = db.Callback().Create().Remove(afterCallback)
		_ = db.Callback().Query().Remove(queryCallback)
	}()

	t.Run("atomic-audit-rollback-and-committed-publication-retry", func(t *testing.T) {
		review := get(adminCookie, subject.User.ID)
		before, beforeKey, beforeAudits := user(subject.User.ID), key(), audits(subject.User.ID)
		failAudit.Store(true)
		patch(adminCookie, admin.CSRFToken, review, statusBody(true), 503)
		failAudit.Store(false)
		if !reflect.DeepEqual(before, user(subject.User.ID)) || !reflect.DeepEqual(beforeKey, key()) || beforeAudits != audits(subject.User.ID) {
			t.Fatal("audit failure leaked state/Key mutation")
		}
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 200)
		armPublication.Store(true)
		patch(adminCookie, admin.CSRFToken, review, statusBody(true), 503)
		failPublication.Store(false)
		committed, revoked := user(subject.User.ID), key()
		if !committed.Disabled || committed.MemberRoleRevision == before.MemberRoleRevision || revoked.Status != entity.KeyRevoked || revoked.LifecycleRevision == beforeKey.LifecycleRevision || audits(subject.User.ID) != beforeAudits+1 {
			t.Fatal("publication failure did not retain exactly committed effects")
		}
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 401)
		patch(adminCookie, admin.CSRFToken, review, statusBody(true), 200)
		if !reflect.DeepEqual(committed, user(subject.User.ID)) || !reflect.DeepEqual(revoked, key()) || audits(subject.User.ID) != beforeAudits+1 {
			t.Fatal("original retry rewrote committed state")
		}
		patch(adminCookie, admin.CSRFToken, get(adminCookie, subject.User.ID), statusBody(false), 200)
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 401)
		if !reflect.DeepEqual(revoked, key()) || !reflect.DeepEqual(stable, protected()) {
			t.Fatal("ordinary enable revived Key or changed unrelated facts")
		}
	})

	t.Run("reactivation-preserves-historical-offboarding", func(t *testing.T) {
		role, err := svc.SaveRole(ctx, admin.User.ID, "", "State target custom role", []string{"prices.read"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, subject.User.ID, []string{role.Role.ID}); err != nil {
			t.Fatal(err)
		}
		body := `{"request_id":"state-test-offboarding","current_password":"state-test-password","reason":"Reviewed resource-free departure","team_assignments":[]}`
		response := identityRequest(router, "POST", path(subject.User.ID)+"/offboarding/emergency", body, adminCookie, admin.CSRFToken)
		caseRecord := decodeCatalogResponse[service.OffboardingCaseRecord](t, response, 200)
		review := get(adminCookie, subject.User.ID)
		if review.Status != "offboarded" || review.ActivationMode == nil || *review.ActivationMode != "reactivate" {
			t.Fatal("offboarding did not require explicit reactivation")
		}
		patch(adminCookie, admin.CSRFToken, review, statusBody(false), 200)
		if user(subject.User.ID).OffboardedAt != nil || key().Status != entity.KeyRevoked {
			t.Fatal("reactivation state/credential contract changed")
		}
		var retained entity.OffboardingCase
		if err := db.First(&retained, "id = ?", caseRecord.ID).Error; err != nil || retained.Status != "completed" {
			t.Fatal("reactivation erased completed departure", err)
		}
		var roles int64
		if err := db.Model(&entity.UserRole{}).Where("user_id = ?", subject.User.ID).Count(&roles).Error; err != nil || roles != 0 {
			t.Fatal("reactivation restored cleared custom roles", err)
		}
	})

	t.Run("self-disable-is-committed-without-success-confirmation", func(t *testing.T) {
		second, err := svc.CreateMember(ctx, admin.User.ID, "state-second-admin@example.invalid", "state-test-password", "Second state admin", entity.RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"state-second-admin@example.invalid","password":"state-test-password"}`, nil, "")
		expectStatus(t, login, 200)
		secondSession, secondCookie := readIdentity(t, login)
		review := get(secondCookie, second.User.ID)
		patch(secondCookie, secondSession.CSRFToken, review, statusBody(true), 401)
		if !user(second.User.ID).Disabled || audits(second.User.ID) != 1 {
			t.Fatal("self-disable401 did not have independently observed commit")
		}
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", secondCookie, ""), 401)
		patch(adminCookie, admin.CSRFToken, get(adminCookie, second.User.ID), statusBody(false), 200)
		expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", secondCookie, ""), 401)
	})

	t.Run("self-demotion-retained-session-does-not-confirm-role-write", func(t *testing.T) {
		second, err := svc.CreateMember(ctx, admin.User.ID, "state-demotion-admin@example.invalid", "state-test-password", "Demotion state admin", entity.RoleAdmin)
		if err != nil {
			t.Fatal(err)
		}
		role, err := svc.SaveRole(ctx, admin.User.ID, "", "State demoted writer", []string{"members.write"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, second.User.ID, []string{role.Role.ID}); err != nil {
			t.Fatal(err)
		}
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"state-demotion-admin@example.invalid","password":"state-test-password"}`, nil, "")
		expectStatus(t, login, 200)
		secondSession, secondCookie := readIdentity(t, login)
		patch(secondCookie, secondSession.CSRFToken, get(secondCookie, second.User.ID), roleBody("member"), 403)
		if user(second.User.ID).Role != entity.RoleMember || audits(second.User.ID) != 1 {
			t.Fatal("self-demotion403 lacked independently observed commit")
		}
		currentSession := decodeCatalogResponse[SessionResponse](t, identityRequest(router, "GET", "/api/v1/auth/session", "", secondCookie, ""), 200)
		if currentSession.User.Role != entity.RoleMember {
			t.Fatal("retained Session revived former base identity")
		}
		patch(secondCookie, secondSession.CSRFToken, get(secondCookie, second.User.ID), roleBody("member"), 403)
		if audits(second.User.ID) != 1 {
			t.Fatal("forbidden matching role write created another audit")
		}
	})

	t.Run("missing-runtime-distinguishes-base-from-account-effect", func(t *testing.T) {
		other, _, _ := createSystemStatusMember(t, svc, router, admin.User.ID, "state-no-runtime", nil)
		unstarted, err := service.New(ctx, db, service.WithCredentialStorage(store))
		if err != nil {
			t.Fatal(err)
		}
		unstartedRouter := fox.New()
		New(unstarted).RegisterRoutes(unstartedRouter)
		review := memberStateFixtureReview(t, unstartedRouter, adminCookie, other.User.ID)
		if review.AccountAccessRuntimeApplied {
			t.Fatal("unstarted runtime claimed lifecycle application")
		}
		expectStatus(t, memberStateFixturePATCH(t, unstartedRouter, adminCookie, admin.CSRFToken, other.User.ID, review.ETag, roleBody("admin")), 200)
		patch(adminCookie, admin.CSRFToken, get(adminCookie, other.User.ID), roleBody("member"), 200)
		review = memberStateFixtureReview(t, unstartedRouter, adminCookie, other.User.ID)
		beforeAudits := audits(other.User.ID)
		expectStatus(t, memberStateFixturePATCH(t, unstartedRouter, adminCookie, admin.CSRFToken, other.User.ID, review.ETag, statusBody(true)), 503)
		committed := user(other.User.ID)
		if !committed.Disabled || audits(other.User.ID) != beforeAudits+1 {
			t.Fatal("unavailable lifecycle proof hid real commit")
		}
		patch(adminCookie, admin.CSRFToken, review, statusBody(true), 200)
		if !reflect.DeepEqual(committed, user(other.User.ID)) || audits(other.User.ID) != beforeAudits+1 {
			t.Fatal("started-runtime reconciliation rewrote committed operation")
		}
	})

	// Restart uses the same configured root/database and no inference replay.
	svc.StopRuntime()
	fresh, next := memberStateRuntimeFixtureRouter(t, db, service.WithCredentialStorage(store))
	defer next.StopRuntime()
	retained := memberStateFixtureReview(t, fresh, adminCookie, subject.User.ID)
	if retained.Disabled || retained.OffboardedAt != nil || !retained.AccountAccessRuntimeApplied || key().Status != entity.KeyRevoked {
		t.Fatal("restart lost lifecycle or revoked Key")
	}
	expectStatus(t, identityRequest(fresh, "GET", "/api/v1/auth/session", "", subjectCookie, ""), 401)
	var calls, attempts int64
	if err := db.Model(&entity.CallRecord{}).Count(&calls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&entity.CallAttempt{}).Count(&attempts).Error; err != nil || calls != 0 || attempts != 0 {
		t.Fatal("zero-native lifecycle created call history", calls, attempts, err)
	}
}
