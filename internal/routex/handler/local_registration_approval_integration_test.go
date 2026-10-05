package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

type approvalFixturePolicy struct {
	Enabled          bool   `json:"enabled"`
	ApprovalRequired bool   `json:"approval_required"`
	ReviewETag       string `json:"review_etag"`
}
type approvalFixtureApplication struct {
	ID              string     `json:"id"`
	State           string     `json:"state"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at"`
	DecisionActorID *string    `json:"decision_actor_id"`
	DecisionReason  *string    `json:"decision_reason"`
}
type approvalFixtureReview struct {
	UserID            string                      `json:"user_id"`
	Name              string                      `json:"name"`
	IdentityRole      string                      `json:"identity_role"`
	Disabled          bool                        `json:"disabled"`
	OffboardedAt      *time.Time                  `json:"offboarded_at"`
	ApprovalStatus    string                      `json:"approval_status"`
	Application       *approvalFixtureApplication `json:"application"`
	CanApprove        bool                        `json:"can_approve"`
	CanReject         bool                        `json:"can_reject"`
	AdmissionEligible bool                        `json:"admission_eligible"`
	RuntimeApplied    bool                        `json:"runtime_applied"`
	ReviewETag        string                      `json:"review_etag"`
}
type approvalFixtureResult struct {
	Confirmation      string `json:"confirmation"`
	UserID            string `json:"user_id"`
	ApplicationID     string `json:"application_id"`
	Decision          string `json:"decision"`
	AdmissionEligible bool   `json:"admission_eligible"`
	RuntimeApplied    bool   `json:"runtime_applied"`
}

func approvalFixtureRequest(t *testing.T, router http.Handler, method, path string, body any, cookie *http.Cookie, csrf, etag string) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, "http://routex.test"+path, bytes.NewReader(raw))
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
		req.Header.Set("If-Match", `"`+etag+`"`)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res
}
func approvalFixtureObject(t *testing.T, res *httptest.ResponseRecorder, status int, fields ...string) {
	t.Helper()
	expectStatus(t, res, status)
	var value map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if len(value) != len(fields) {
		t.Fatal("approval public field count differs")
	}
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			t.Fatal("approval public field absent", field)
		}
	}
}
func approvalFixturePolicyReview(t *testing.T, router http.Handler, cookie *http.Cookie, csrf string) approvalFixturePolicy {
	t.Helper()
	res := approvalFixtureRequest(t, router, "GET", "/api/v1/admin/registration", nil, cookie, csrf, "")
	approvalFixtureObject(t, res, 200, "enabled", "approval_required", "review_etag")
	value := decodeCatalogResponse[approvalFixturePolicy](t, res, 200)
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(value.ReviewETag) || res.Header().Get("ETag") != `"`+value.ReviewETag+`"` {
		t.Fatal("registration policy strong review differs")
	}
	return value
}
func approvalFixtureSetPolicy(t *testing.T, router http.Handler, cookie *http.Cookie, csrf string, enabled, required bool, reason string) approvalFixturePolicy {
	t.Helper()
	review := approvalFixturePolicyReview(t, router, cookie, csrf)
	res := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": enabled, "approval_required": required, "reason": reason}, cookie, csrf, review.ReviewETag)
	approvalFixtureObject(t, res, 200, "confirmation", "enabled", "approval_required", "review_etag")
	value := decodeCatalogResponse[approvalFixturePolicy](t, res, 200)
	if value.Enabled != enabled || value.ApprovalRequired != required || res.Header().Get("ETag") != `"`+value.ReviewETag+`"` {
		t.Fatal("registration policy replacement not confirmed")
	}
	var confirmation struct {
		Confirmation string `json:"confirmation"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &confirmation); err != nil || confirmation.Confirmation != "current_registration_policy" {
		t.Fatal("policy response claimed wrong effect")
	}
	return value
}
func approvalFixtureMemberReview(t *testing.T, router http.Handler, cookie *http.Cookie, csrf, userID string) approvalFixtureReview {
	t.Helper()
	res := approvalFixtureRequest(t, router, "GET", "/api/v1/admin/members/"+userID+"/approval", nil, cookie, csrf, "")
	approvalFixtureObject(t, res, 200, "user_id", "name", "identity_role", "disabled", "offboarded_at", "approval_status", "application", "can_approve", "can_reject", "admission_eligible", "runtime_applied", "review_etag")
	value := decodeCatalogResponse[approvalFixtureReview](t, res, 200)
	if value.UserID != userID || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(value.ReviewETag) || res.Header().Get("ETag") != `"`+value.ReviewETag+`"` || res.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("dedicated approval exact target/private review differs")
	}
	if value.Application != nil {
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(res.Body.Bytes(), &raw)
		var object map[string]json.RawMessage
		_ = json.Unmarshal(raw["application"], &object)
		if len(object) != 6 {
			t.Fatal("application metadata exposes undeclared fields")
		}
		for _, name := range []string{"id", "state", "created_at", "decided_at", "decision_actor_id", "decision_reason"} {
			if _, ok := object[name]; !ok {
				t.Fatal("application metadata missing", name)
			}
		}
	}
	return value
}

// Registered only by the future root-owned identity harness after the migration
// and reviewed routes are installed. The controlled upstream never calls a paid
// provider; all negative native cases must leave its dispatch counter at zero.
func testLocalRegistrationApproval(t *testing.T, db *gorm.DB) {
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{91}, 32))
	if err != nil {
		t.Fatal(err)
	}
	var dispatches atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			switch r.URL.Path {
			case "/v1beta/models":
				_, _ = w.Write([]byte(`{"models":[{"name":"models/native-model","supportedGenerationMethods":["generateContent"]}]}`))
			case "/v1/models":
				if r.Header.Get("anthropic-version") == "2023-06-01" {
					_, _ = w.Write([]byte(`{"data":[{"id":"native-model","type":"model"}],"has_more":false}`))
				} else {
					_, _ = w.Write([]byte(`{"data":[{"id":"native-model"}]}`))
				}
			default:
				t.Error("unexpected controlled discovery path")
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		dispatches.Add(1)
		switch r.URL.Path {
		case "/v1/chat/completions":
			_, _ = w.Write([]byte(`{"object":"chat.completion","model":"native-model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Controlled completion"}}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"prompt_tokens_details":{"cached_tokens":0}}}`))
		case "/v1/responses":
			_, _ = w.Write([]byte(`{"object":"response","id":"controlled-response","status":"completed","model":"native-model","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Controlled completion"}]}],"usage":{"input_tokens":2,"output_tokens":1,"total_tokens":3,"input_tokens_details":{"cached_tokens":0}}}`))
		case "/v1/messages":
			_, _ = w.Write([]byte(`{"id":"controlled-message","type":"message","role":"assistant","model":"native-model","content":[{"type":"text","text":"Controlled completion"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}`))
		case "/v1beta/models/native-model:generateContent":
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"Controlled completion"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1,"thoughtsTokenCount":0,"totalTokenCount":3,"cachedContentTokenCount":0}}`))
		default:
			t.Error("unexpected controlled native path")
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"approval-admin@example.com","password":"approval-password","name":"Approval administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	// Install immutable test callbacks before any background worker starts.
	type nativeQueryMarker struct{}
	var nativeApprovalQueries atomic.Int64
	const nativeQueryHook = "approval_fixture_native_no_sql"
	if err := db.Callback().Query().Before("gorm:query").Register(nativeQueryHook, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(nativeQueryMarker{}) == true && (tx.Statement.Table == "users" || tx.Statement.Table == "registration_approval_applications") {
			nativeApprovalQueries.Add(1)
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Callback().Query().Remove(nativeQueryHook) }()
	var failAudit atomic.Bool
	const auditHook = "approval_fixture_audit_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(auditHook, func(tx *gorm.DB) {
		if failAudit.Load() && tx.Statement.Table == "audit_events" {
			_ = tx.AddError(errors.New("controlled approval audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { failAudit.Store(false); _ = db.Callback().Create().Remove(auditHook) }()
	var failPublication atomic.Bool
	const publicationHook = "approval_fixture_publication_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(publicationHook, func(tx *gorm.DB) {
		if failPublication.Load() && tx.Statement.Table == "registration_approval_applications" && tx.Statement.ReflectValue.Kind() == reflect.Slice {
			_ = tx.AddError(errors.New("controlled approval publication failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { failPublication.Store(false); _ = db.Callback().Query().Remove(publicationHook) }()
	var failApplication atomic.Bool
	const createHook = "approval_fixture_application_failure"
	if err := db.Callback().Create().Before("gorm:create").Register(createHook, func(tx *gorm.DB) {
		if failApplication.Load() && tx.Statement.Table == "registration_approval_applications" {
			_ = tx.AddError(errors.New("controlled application creation failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { failApplication.Store(false); _ = db.Callback().Create().Remove(createHook) }()
	if err := svc.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.StopRuntime()
	recorderCtx, stopRecorder := context.WithCancel(ctx)
	defer stopRecorder()
	if err := svc.StartCallRecorder(recorderCtx, filepath.Join(t.TempDir(), "approval-calls.db")); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = svc.StopCallRecorder() }()
	auditCount := func(action, userID string) int64 {
		t.Helper()
		var n int64
		q := db.Model(&entity.AuditEvent{}).Where("action = ?", action)
		if userID != "" {
			q = q.Where("resource_id = ?", userID)
		}
		if err := q.Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}
	loadUser := func(userID string) entity.User {
		t.Helper()
		var u entity.User
		if err := db.First(&u, "id = ?", userID).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	loadApp := func(user entity.User) entity.RegistrationApprovalApplication {
		t.Helper()
		if user.ApprovalApplicationID == nil {
			t.Fatal("managed registration lost immutable link")
		}
		var a entity.RegistrationApprovalApplication
		if err := db.First(&a, "id = ?", *user.ApprovalApplicationID).Error; err != nil {
			t.Fatal(err)
		}
		if a.UserID != user.ID || !a.UserCreatedAt.Equal(user.CreatedAt) {
			t.Fatal("application exact identity continuity differs")
		}
		return a
	}
	assertNoAuthority := func(u entity.User) {
		t.Helper()
		for _, model := range []any{&entity.Session{}, &entity.UserMFA{}, &entity.MFAChallenge{}, &entity.UserModelGrant{}, &entity.APIKey{}} {
			var n int64
			if err := db.Model(model).Where("user_id = ?", u.ID).Count(&n).Error; err != nil || n != 0 {
				t.Fatal("pending registration created authentication or model authority")
			}
		}
		if u.LastLoginAt != nil {
			t.Fatal("registration fabricated successful sign-in")
		}
	}
	pending := func(suffix string, cookie *http.Cookie) entity.User {
		t.Helper()
		email := "approval-" + suffix + "@example.com"
		res := approvalFixtureRequest(t, router, "POST", "/api/v1/auth/register", map[string]any{"email": email, "password": "approval-password", "name": "Pending " + suffix, "role": "admin"}, cookie, "", "")
		approvalFixtureObject(t, res, 202, "kind")
		if strings.TrimSpace(res.Body.String()) != `{"kind":"approval_pending"}` || len(res.Result().Cookies()) != 0 {
			t.Fatal("pending registration exposes identity or issues a cookie")
		}
		var u entity.User
		if err := db.First(&u, "email = ?", email).Error; err != nil {
			t.Fatal(err)
		}
		a := loadApp(u)
		if u.Role != entity.RoleMember || u.Disabled || u.OffboardedAt != nil || a.State != "pending" || a.DecidedAt != nil || a.DecisionActorID != nil || a.DecisionReason != nil {
			t.Fatal("pending initial authority/decision fields differ")
		}
		assertNoAuthority(u)
		return u
	}
	decision := func(u entity.User, review approvalFixtureReview, want, reason string, status int) *httptest.ResponseRecorder {
		t.Helper()
		res := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/members/"+u.ID+"/approval", map[string]any{"decision": want, "reason": reason}, adminCookie, admin.CSRFToken, review.ReviewETag)
		expectStatus(t, res, status)
		if res.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("approval mutation lacks private denial/application header")
		}
		return res
	}
	confirmed := func(res *httptest.ResponseRecorder, u entity.User, want string, eligible bool) {
		t.Helper()
		approvalFixtureObject(t, res, 200, "confirmation", "user_id", "application_id", "decision", "admission_eligible", "runtime_applied")
		v := decodeCatalogResponse[approvalFixtureResult](t, res, 200)
		if v.Confirmation != "current_account_approval" || v.UserID != u.ID || u.ApprovalApplicationID == nil || v.ApplicationID != *u.ApprovalApplicationID || v.Decision != want || v.AdmissionEligible != eligible || !v.RuntimeApplied {
			t.Fatal("decision did not prove exact current admission state")
		}
	}
	public := identityRequest(router, "GET", "/api/v1/auth/registration", "", nil, "")
	approvalFixtureObject(t, public, 200, "enabled", "approval_required")
	initial := approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken)
	if initial.Enabled || initial.ApprovalRequired {
		t.Fatal("new policy defaults differ")
	}
	policy := approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, true, "Require local registration approval")
	// No-op and old-review matching-state reconciliation are current database
	// confirmations. Neither advances generation nor appends another audit.
	policyAudits := auditCount("registration.policy.update", "")
	noop := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true, "approval_required": true, "reason": "Require local registration approval"}, adminCookie, admin.CSRFToken, initial.ReviewETag)
	approvalFixtureObject(t, noop, 200, "confirmation", "enabled", "approval_required", "review_etag")
	if auditCount("registration.policy.update", "") != policyAudits || approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken).ReviewETag != policy.ReviewETag {
		t.Fatal("policy reconciliation wrote another generation/audit")
	}
	policy = approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, false, true, "Close registrations without discarding approval requirement")
	if policy.Enabled || !policy.ApprovalRequired {
		t.Fatal("closed policy discarded approval requirement")
	}
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, true, "Reopen reviewed registrations")
	stale := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": false, "approval_required": false, "reason": "Stale policy must fail"}, adminCookie, admin.CSRFToken, initial.ReviewETag)
	expectStatus(t, stale, 409)
	t.Run("strict_policy", func(t *testing.T) {
		review := approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken)
		bodies := []string{`{"enabled":true}`, `{"enabled":true,"approval_required":null,"reason":"Review"}`, `{"enabled":true,"enabled":true,"approval_required":true,"reason":"Review"}`, `{"enabled":true,"approval_required":true,"reason":" Review "}`, `{"enabled":true,"approval_required":true,"reason":"Review","extra":true}`, `{"enabled":"true","approval_required":true,"reason":"Review"}`, `{"enabled":true,"approval_required":true,"reason":""}`}
		for _, body := range bodies {
			req := httptest.NewRequest("PATCH", "http://routex.test/api/v1/admin/registration", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(adminCookie)
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
			req.Header.Set("If-Match", `"`+review.ReviewETag+`"`)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			expectStatus(t, res, 400)
		}
		expectStatus(t, approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true, "approval_required": true, "reason": "Missing review"}, adminCookie, admin.CSRFToken, ""), 400)
		expectStatus(t, approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration?extra=", map[string]any{"enabled": true, "approval_required": true, "reason": "Unexpected query"}, adminCookie, admin.CSRFToken, review.ReviewETag), 400)
		if approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken).ReviewETag != review.ReviewETag || auditCount("registration.policy.update", "") != policyAudits+2 {
			t.Fatal("invalid policy mutated saved state")
		}
	})
	// The trusted compatibility setter changes only Enabled; the public HTTP
	// writer above is always reviewed and complete.
	if err := svc.SetRegistrationEnabled(ctx, admin.User.ID, false); err != nil {
		t.Fatal(err)
	}
	legacyPolicy := approvalFixturePolicyReview(t, router, adminCookie, admin.CSRFToken)
	if legacyPolicy.Enabled || !legacyPolicy.ApprovalRequired {
		t.Fatal("trusted compatibility setter erased approval policy")
	}
	if err := svc.SetRegistrationEnabled(ctx, admin.User.ID, true); err != nil {
		t.Fatal(err)
	}
	subject := pending("subject", adminCookie)
	unmanaged, err := svc.CreateMember(ctx, admin.User.ID, "approval-unmanaged@example.com", "approval-password", "Unmanaged", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	unmanagedReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, unmanaged.User.ID)
	if unmanagedReview.ApprovalStatus != "not_required" || unmanagedReview.Application != nil || unmanagedReview.CanApprove || unmanagedReview.CanReject || !unmanagedReview.AdmissionEligible {
		t.Fatal("administrator creation invented historical approval")
	}
	decision(unmanaged.User, unmanagedReview, "approve", "Unmanaged users cannot be approved", 409)
	// Ordinary GET projections expose only the mandatory safe summary, never
	// a private application identifier/reason/reviewer or review generation.
	for _, path := range []string{"/api/v1/admin/members/" + subject.ID, "/api/v1/admin/members"} {
		res := approvalFixtureRequest(t, router, "GET", path, nil, adminCookie, admin.CSRFToken, "")
		expectStatus(t, res, 200)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
			t.Fatal(err)
		}
		if path == "/api/v1/admin/members" {
			var items []map[string]json.RawMessage
			if err := json.Unmarshal(raw["items"], &items); err != nil {
				t.Fatal(err)
			}
			raw = nil
			for _, item := range items {
				var id string
				_ = json.Unmarshal(item["id"], &id)
				if id == subject.ID {
					raw = item
					break
				}
			}
			if raw == nil {
				t.Fatal("pending target missing from retained member list")
			}
		}
		var summary map[string]json.RawMessage
		if err := json.Unmarshal(raw["registration_approval"], &summary); err != nil || len(summary) != 2 {
			t.Fatal("ordinary Member approval summary differs")
		}
		var status string
		var eligible bool
		if json.Unmarshal(summary["status"], &status) != nil || json.Unmarshal(summary["admission_eligible"], &eligible) != nil || status != "pending" || eligible {
			t.Fatal("ordinary pending summary fabricated admission")
		}
	}
	review := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, subject.ID)
	if review.ApprovalStatus != "pending" || !review.CanApprove || !review.CanReject || review.AdmissionEligible || !review.RuntimeApplied {
		t.Fatal("pending review is not an exact published denial")
	}
	// Only the conjunction of current intrinsic administrator identity and both
	// explicit permissions admits this workspace; unrelated custom grants cannot.
	t.Run("review_authority", func(t *testing.T) {
		actor, err := svc.CreateMember(ctx, admin.User.ID, "approval-reader@example.com", "approval-password", "Reader", entity.RoleMember)
		if err != nil {
			t.Fatal(err)
		}
		role := entity.Role{ID: "rol_approval_reader", Name: "Approval reader", Builtin: false}
		if err := db.Create(&role).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&entity.RolePermission{RoleID: role.ID, Permission: "members.read"}).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetMemberRoles(ctx, admin.User.ID, actor.User.ID, []string{role.ID}); err != nil {
			t.Fatal(err)
		}
		login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"approval-reader@example.com","password":"approval-password"}`, nil, "")
		expectStatus(t, login, 200)
		reader, readerCookie := readIdentity(t, login)
		for _, method := range []string{"GET", "PATCH"} {
			body := any(nil)
			if method == "PATCH" {
				body = map[string]any{"decision": "approve", "reason": "Reader cannot approve"}
			}
			res := approvalFixtureRequest(t, router, method, "/api/v1/admin/members/"+subject.ID+"/approval", body, readerCookie, reader.CSRFToken, review.ReviewETag)
			expectStatus(t, res, 403)
			if res.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("permission denial header missing")
			}
		}
		// A controlled retained custom grant cannot substitute for the intrinsic
		// base identity. Public Role creation must reject this builtin-only grant.
		expectStatus(t, approvalFixtureRequest(t, router, "POST", "/api/v1/admin/roles", map[string]any{"name": "Approval cannot delegate", "permissions": []string{"members.approvals.write"}}, adminCookie, admin.CSRFToken, ""), 400)
		controlledGrant := entity.RolePermission{RoleID: role.ID, Permission: "members.approvals.write"}
		if err := db.Create(&controlledGrant).Error; err != nil {
			t.Fatal(err)
		}
		expectStatus(t, approvalFixtureRequest(t, router, "GET", "/api/v1/admin/members/"+subject.ID+"/approval", nil, readerCookie, reader.CSRFToken, ""), 403)
		if err := db.Where("role_id = ? AND permission = ?", role.ID, controlledGrant.Permission).Delete(&entity.RolePermission{}).Error; err != nil {
			t.Fatal(err)
		}
		for _, permission := range []string{"members.read", "members.approvals.write"} {
			removed := entity.RolePermission{RoleID: "rol_admin", Permission: permission}
			if err := db.Where("role_id = ? AND permission = ?", removed.RoleID, permission).Delete(&entity.RolePermission{}).Error; err != nil {
				t.Fatal(err)
			}
			res := approvalFixtureRequest(t, router, "GET", "/api/v1/admin/members/"+subject.ID+"/approval", nil, adminCookie, admin.CSRFToken, "")
			expectStatus(t, res, 403)
			if err := db.Create(&removed).Error; err != nil {
				t.Fatal(err)
			}
		}
		for _, method := range []string{"GET", "PATCH"} {
			res := approvalFixtureRequest(t, router, method, "/api/v1/admin/members/"+subject.ID+"/approval", nil, nil, "", "")
			expectStatus(t, res, 401)
			if res.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal("unauthenticated approval response not private")
			}
		}
	})
	review = approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, subject.ID)
	t.Run("strict_decision", func(t *testing.T) {
		path := "/api/v1/admin/members/" + subject.ID + "/approval"
		for _, body := range []string{`{"decision":"approve"}`, `{"decision":"approved","reason":"Review"}`, `{"decision":"approve","reason":null}`, `{"decision":"approve","decision":"reject","reason":"Review"}`, `{"decision":"approve","reason":" Review "}`, `{"decision":"approve","reason":"Review","disabled":false}`, `{"decision":"approve","reason":"Review","application_id":"foreign"}`} {
			req := httptest.NewRequest("PATCH", "http://routex.test"+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(adminCookie)
			req.Header.Set("X-CSRF-Token", admin.CSRFToken)
			req.Header.Set("If-Match", `"`+review.ReviewETag+`"`)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			expectStatus(t, res, 400)
		}
		expectStatus(t, approvalFixtureRequest(t, router, "PATCH", path, map[string]any{"decision": "approve", "reason": "Missing review"}, adminCookie, admin.CSRFToken, ""), 400)
		expectStatus(t, approvalFixtureRequest(t, router, "PATCH", path, map[string]any{"decision": "approve", "reason": "Missing CSRF"}, adminCookie, "", review.ReviewETag), 403)
		expectStatus(t, approvalFixtureRequest(t, router, "GET", path+"?extra=", nil, adminCookie, admin.CSRFToken, ""), 400)
		expectStatus(t, approvalFixtureRequest(t, router, "GET", "/api/v1/admin/members/"+strings.ToUpper(subject.ID)+"/approval", nil, adminCookie, admin.CSRFToken, ""), 404)
		if loadApp(subject).State != "pending" || auditCount("member.approval.decide", subject.ID) != 0 {
			t.Fatal("invalid decisions changed application")
		}
	})
	// Public policy changes never release an already managed pending user.
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, false, "Allow only future immediate registration")
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"approval-subject@example.com","password":"approval-password"}`, nil, ""), 401)
	approvalFixtureSetPolicy(t, router, adminCookie, admin.CSRFToken, true, true, "Restore reviewed approval registration")
	legacy, legacyErr := svc.Register(ctx, "approval-legacy@example.com", "approval-password", "Legacy pending")
	if legacy != nil || !errors.Is(legacyErr, service.ErrRegistrationApprovalPending) {
		t.Fatal("legacy Register supplied pending authentication")
	}
	var legacyUser entity.User
	if err := db.First(&legacyUser, "email = ?", "approval-legacy@example.com").Error; err != nil {
		t.Fatal(err)
	}
	assertNoAuthority(legacyUser)
	loadApp(legacyUser)
	// Seeded credentials model historical possession, not public issuance to a
	// pending user. They must remain inert and unchanged even after approval.
	token := strings.Repeat("p", 43)
	session := entity.Session{ID: "ses_approval_pending", UserID: subject.ID, TokenHash: secret.SHA256Hex(token), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&session).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&session, "id = ?", session.ID).Error; err != nil {
		t.Fatal(err)
	}
	pendingCookie := &http.Cookie{Name: adminCookie.Name, Value: token}
	if a, err := svc.ChangePassword(ctx, subject.ID, "approval-password", "changed-approval-password"); a != nil || err == nil {
		t.Fatal("pending password replacement issued a Session")
	}
	seededAuth := &service.Authentication{User: subject, Session: session, Token: token}
	if value, err := svc.BeginMFAEnrollment(ctx, seededAuth, "approval-password"); value != nil || err == nil {
		t.Fatal("pending seeded Session changed MFA security state")
	}
	if !reflect.DeepEqual(subject, loadUser(subject.ID)) {
		t.Fatal("pending security replacement changed password/login identity")
	}
	for _, model := range []any{&entity.UserMFA{}, &entity.MFAChallenge{}} {
		var n int64
		if err := db.Model(model).Where("user_id = ?", subject.ID).Count(&n).Error; err != nil || n != 0 {
			t.Fatal("pending security replacement issued MFA material")
		}
	}
	var sessionCount int64
	if err := db.Model(&entity.Session{}).Where("user_id = ?", subject.ID).Count(&sessionCount).Error; err != nil || sessionCount != 1 {
		t.Fatal("pending security replacement issued another Session")
	}
	bearer := "rx_" + strings.Repeat("k", 43)
	seededKey := entity.APIKey{ID: "key_approval_pending", UserID: subject.ID, Name: "Retained pending credential", Prefix: "rx_test", TokenHash: secret.SHA256Hex(bearer), Status: entity.KeyRevoked}
	if err := db.Create(&seededKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&seededKey, "id = ?", seededKey.ID).Error; err != nil {
		t.Fatal(err)
	}
	generation := strings.Repeat("a", 64)
	mfaSecret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	sealed, err := store.Seal("mfa:"+subject.ID+":"+generation, mfaSecret)
	if err != nil {
		t.Fatal(err)
	}
	state := entity.UserMFA{UserID: subject.ID, Enabled: true, Generation: generation, SecretCiphertext: sealed, LastTOTPStep: -1}
	if err := db.Create(&state).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&state, "user_id = ?", subject.ID).Error; err != nil {
		t.Fatal(err)
	}
	challengeToken := strings.Repeat("c", 43)
	challenge := entity.MFAChallenge{UserID: subject.ID, Purpose: "login", TokenHash: secret.SHA256Hex(challengeToken), PasswordDigest: secret.SHA256Hex(subject.PasswordHash), Generation: generation, ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(&challenge).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&challenge, "user_id = ? AND purpose = ?", subject.ID, "login").Error; err != nil {
		t.Fatal(err)
	}
	recovery := entity.MFARecoveryCode{UserID: subject.ID, CodeHash: strings.Repeat("b", 64)}
	if err := db.Create(&recovery).Error; err != nil {
		t.Fatal(err)
	}
	authBefore := loadUser(subject.ID)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", pendingCookie, ""), 401)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/account/sessions", "", pendingCookie, ""), 401)
	for _, proof := range []service.MFAProof{{Code: mfaFixtureCode(t, mfaSecret, 0)}, {Code: "000000"}, {RecoveryCode: "invalid-recovery"}} {
		a, e := svc.CompleteMFALogin(ctx, challengeToken, proof)
		if a != nil || e == nil {
			t.Fatal("pending MFA consumed proof or issued Session")
		}
	}
	var actualState entity.UserMFA
	var actualChallenge entity.MFAChallenge
	var actualRecovery entity.MFARecoveryCode
	if err := db.First(&actualState, "user_id = ?", subject.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&actualChallenge, "user_id = ? AND purpose = ?", subject.ID, "login").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&actualRecovery, "user_id = ? AND code_hash = ?", subject.ID, recovery.CodeHash).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state, actualState) || !reflect.DeepEqual(challenge, actualChallenge) || !reflect.DeepEqual(recovery, actualRecovery) || !reflect.DeepEqual(authBefore, loadUser(subject.ID)) {
		t.Fatal("pending auth modified MFA proof/failure/identity facts")
	}
	if _, err := svc.CreatePersonalKey(ctx, subject.ID, "Pending cannot issue", []string{}, nil); err == nil {
		t.Fatal("pending actor issued Personal Key")
	}
	if _, err := svc.ListMembers(ctx, subject.ID, service.MemberFilter{}); err == nil {
		t.Fatal("pending actor acquired independent control-plane authority")
	}
	// Build all four protocol families using actual Verify, Enable, discovery,
	// binding and explicit positive-weight product operations.
	type exactNativeRoute struct{ CredentialID, ProviderModelID, ConnectionID, ProviderID string }
	exactRoutes := map[string]exactNativeRoute{}
	var modelID string
	var model *service.ModelCatalog
	for index, protocol := range []string{entity.ProtocolOpenAIChat, entity.ProtocolOpenAIResponses, entity.ProtocolAnthropicMessages, entity.ProtocolGeminiGenerateContent} {
		base := upstream.URL + "/v1"
		if protocol == entity.ProtocolGeminiGenerateContent {
			base = upstream.URL + "/v1beta"
		}
		provider, err := svc.CreateProvider(ctx, admin.User.ID, "Approval native "+string(protocol), service.CreateConnectionInput{Name: "Controlled connection", BaseURL: base, Protocol: protocol, CredentialName: "Controlled credential", Secret: "controlled-upstream-secret"})
		if err != nil {
			t.Fatal(err)
		}
		if len(provider.Connections) != 1 || len(provider.Connections[0].Credentials) != 1 {
			t.Fatal("controlled provider did not retain one connection and credential")
		}
		connection := provider.Connections[0]
		credential := connection.Credentials[0].ID
		if connection.Connection.ProviderID != provider.Provider.ID || connection.Connection.Protocol != protocol || connection.Credentials[0].ConnectionID != connection.Connection.ID {
			t.Fatal("controlled discovery ownership differs")
		}
		verification, err := svc.VerifyCredential(ctx, admin.User.ID, credential)
		if err != nil {
			t.Fatal(err)
		}
		if verification == nil || !verification.Verified || verification.DiscoveredModels != 1 {
			t.Fatal("controlled native discovery did not verify exactly one model", protocol)
		}
		enabled, err := svc.SetCredentialEnabled(ctx, admin.User.ID, credential, true)
		if err != nil {
			t.Fatal(err)
		}
		if enabled == nil || enabled.ID != credential || enabled.ConnectionID != connection.Connection.ID || !enabled.Enabled || enabled.VerificationStatus != "verified" || enabled.VerifiedAt == nil || enabled.VerifiedAt.IsZero() {
			t.Fatal("controlled credential enablement lost exact verification ownership", protocol)
		}
		var discovered []entity.ProviderModel
		if err := db.Where("connection_id = ?", connection.Connection.ID).Limit(2).Find(&discovered).Error; err != nil {
			t.Fatal(err)
		}
		if len(discovered) != 1 || discovered[0].ConnectionID != connection.Connection.ID || discovered[0].UpstreamName != "native-model" {
			t.Fatal("controlled discovery model identity differs", protocol)
		}
		pm := discovered[0]
		var accesses []entity.CredentialModelAccess
		if err := db.Where("credential_id = ?", credential).Limit(2).Find(&accesses).Error; err != nil {
			t.Fatal(err)
		}
		if len(accesses) != 1 || accesses[0].CredentialID != credential || accesses[0].ProviderModelID != pm.ID {
			t.Fatal("controlled discovery coverage differs from the exact credential and model", protocol)
		}
		exactRoutes[protocol] = exactNativeRoute{credential, pm.ID, connection.Connection.ID, provider.Provider.ID}
		if index == 0 {
			model, err = svc.CreateModel(ctx, admin.User.ID, "approval-model", pm.ID)
			if err != nil {
				t.Fatal(err)
			}
			modelID = model.Model.ID
		} else {
			if model, err = svc.AddModelBinding(ctx, admin.User.ID, modelID, pm.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	weights := make([]service.ModelWeight, 0, len(model.Bindings))
	for _, b := range model.Bindings {
		weights = append(weights, service.ModelWeight{BindingID: b.Binding.ID, Weight: 100})
	}
	if _, err := svc.SetModelWeights(ctx, admin.User.ID, modelID, weights); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelID, []string{subject.ID, admin.User.ID}); err != nil {
		t.Fatal(err)
	}
	activeBearer := "rx_" + strings.Repeat("v", 43)
	activeKey := entity.APIKey{ID: "key_approval_active", UserID: subject.ID, Name: "Controlled retained active Key", Prefix: "rx_test", TokenHash: secret.SHA256Hex(activeBearer), Status: entity.KeyActive}
	if err := db.Create(&activeKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.APIKeyModel{KeyID: activeKey.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&activeKey, "id = ?", activeKey.ID).Error; err != nil {
		t.Fatal(err)
	}
	team, err := svc.CreateResource(ctx, admin.User.ID, service.TeamResource, "Approval continuity Team", "", []string{admin.User.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetTeamMembers(ctx, admin.User.ID, team.ID, []service.TeamMemberInput{{UserID: admin.User.ID, Role: entity.TeamOwner, Status: entity.ResourceActive}, {UserID: subject.ID, Role: entity.TeamMember, Status: entity.ResourceActive}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.TeamResource, team.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	project, err := svc.CreateProject(ctx, admin.User.ID, service.ProjectCreationInput{Name: "Approval continuity Project"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetProjectManagers(ctx, admin.User.ID, project.ID, []string{admin.User.ID, subject.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetResourceModels(ctx, admin.User.ID, service.ProjectResource, project.ID, []string{modelID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateProjectKey(ctx, subject.ID, project.ID, "Pending cannot issue", "manual", []string{modelID}, nil); err == nil {
		t.Fatal("pending manager issued Project Key")
	}
	projectKey, err := svc.CreateProjectKey(ctx, admin.User.ID, project.ID, "Admitted continuity", "manual", []string{modelID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmProjectKey(ctx, admin.User.ID, project.ID, projectKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreatePersonalKey(ctx, subject.ID, "Pending valid model ceiling", []string{modelID}, nil); err == nil {
		t.Fatal("pending valid Personal issuance bypassed admission")
	}
	pendingExpires := time.Now().Add(time.Hour)
	pendingKey := entity.APIKey{ID: "key_approval_delivery", UserID: subject.ID, Name: "Controlled pending delivery", Prefix: "rx_test", TokenHash: secret.SHA256Hex("rx_" + strings.Repeat("d", 43)), Status: entity.KeyPending, DeliveryExpiresAt: &pendingExpires, ActivateOnConfirm: true}
	if err := db.Create(&pendingKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.APIKeyModel{KeyID: pendingKey.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&pendingKey, "id = ?", pendingKey.ID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, subject.ID, pendingKey.ID); err == nil {
		t.Fatal("pending actor activated a retained Personal Key")
	}
	var pendingAfter entity.APIKey
	if err := db.First(&pendingAfter, "id = ?", pendingKey.ID).Error; err != nil || !reflect.DeepEqual(pendingKey, pendingAfter) {
		t.Fatal("denied pending delivery altered credential")
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	nativeCases := []struct{ path, body, header string }{
		{"/v1/chat/completions", `{"model":"approval-model","messages":[{"role":"user","content":"Controlled request"}]}`, "Authorization"},
		{"/v1/responses", `{"model":"approval-model","input":"Controlled request"}`, "Authorization"},
		{"/v1/messages", `{"model":"approval-model","max_tokens":10,"messages":[{"role":"user","content":"Controlled request"}]}`, "x-api-key"},
		{"/v1beta/models/approval-model:generateContent", `{"contents":[{"role":"user","parts":[{"text":"Controlled request"}]}]}`, "x-goog-api-key"},
	}

	invoke := func(item struct{ path, body, header string }, bearer string, cookie *http.Cookie, teamID string) *httptest.ResponseRecorder {
		t.Helper()
		path := item.path
		if teamID != "" {
			switch item.header {
			case "x-goog-api-key":
				path = "/api/v1/teams/" + teamID + "/models/approval-model:generateContent"
			default:
				path = "/api/v1/teams/" + teamID + strings.TrimPrefix(item.path, "/v1")
			}
		}
		req := httptest.NewRequest("POST", path, strings.NewReader(item.body))
		req = req.WithContext(context.WithValue(req.Context(), nativeQueryMarker{}, true))
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", secret.SHA256Hex("routex-csrf:"+cookie.Value))
		} else {
			value := bearer
			if item.header == "Authorization" {
				value = "Bearer " + bearer
			}
			req.Header.Set(item.header, value)
		}
		if item.header == "x-api-key" {
			req.Header.Set("anthropic-version", "2023-06-01")
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for _, item := range nativeCases {
		expectStatus(t, invoke(item, activeBearer, nil, ""), 401)
		expectStatus(t, invoke(item, "", pendingCookie, team.ID), 401)
	}
	if dispatches.Load() != 0 {
		t.Fatal("pending Personal or Team Session reached a ready upstream")
	}
	if nativeApprovalQueries.Load() != 0 {
		t.Fatal("native approval authentication used hot-path approval SQL")
	}
	if _, err := svc.AuthenticateAPIKey(ctx, activeBearer); err == nil {
		t.Fatal("pending active retained Key authenticated")
	}
	// Immutable review fences a target-state change, even when the application is
	// still pending. Disable/base-role configuration is inert until admission.
	review = approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, subject.ID)
	disabled := true
	adminRole := entity.RoleAdmin
	if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.ID, &disabled, &adminRole); err != nil {
		t.Fatal(err)
	}
	decision(subject, review, "approve", "Pre-configuration review is stale", 409)
	subject = loadUser(subject.ID)
	review = approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, subject.ID)
	if !review.Disabled || review.IdentityRole != entity.RoleAdmin || review.AdmissionEligible || !review.CanApprove {
		t.Fatal("inert pending identity configuration became admission")
	}
	// Full before/after entity equality proves the decision itself does not
	// change credentials, lifecycle or last-login. Existing disable behavior may
	// have revoked/deleted credentials; capture that state before the decision.
	stateBefore := loadUser(subject.ID)
	var keysBefore []entity.APIKey
	var sessionsBefore []entity.Session
	if err := db.Where("user_id = ?", subject.ID).Order("id").Find(&keysBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", subject.ID).Order("id").Find(&sessionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	approved := decision(subject, review, "approve", "Approve the retained disabled account", 200)
	confirmed(approved, subject, "approved", false)
	var keysAfter []entity.APIKey
	var sessionsAfter []entity.Session
	if err := db.Where("user_id = ?", subject.ID).Order("id").Find(&keysAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Where("user_id = ?", subject.ID).Order("id").Find(&sessionsAfter).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stateBefore, loadUser(subject.ID)) || !reflect.DeepEqual(keysBefore, keysAfter) || !reflect.DeepEqual(sessionsBefore, sessionsAfter) {
		t.Fatal("approval mutated inert identity/credential fields")
	}
	approvedApp := loadApp(subject)
	if approvedApp.State != "approved" || approvedApp.DecidedAt == nil || approvedApp.DecisionActorID == nil || *approvedApp.DecisionActorID != admin.User.ID || approvedApp.DecisionReason == nil || *approvedApp.DecisionReason != "Approve the retained disabled account" {
		t.Fatal("terminal decision missing exact actor/time/reason")
	}
	originalAudit := auditCount("member.approval.decide", subject.ID)
	var typedAudit entity.AuditEvent
	if err := db.Where("action = ? AND resource_id = ?", "member.approval.decide", subject.ID).Take(&typedAudit).Error; err != nil {
		t.Fatal(err)
	}
	if typedAudit.ActorID != admin.User.ID || typedAudit.ResourceType != "user" || typedAudit.DetailsJSON == nil {
		t.Fatal("typed approval audit identity missing")
	}
	var auditBody struct {
		UserID        string `json:"user_id"`
		ApplicationID string `json:"application_id"`
		Before        string `json:"before"`
		After         string `json:"after"`
		Reason        string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(*typedAudit.DetailsJSON), &auditBody); err != nil || auditBody.UserID != subject.ID || auditBody.ApplicationID != approvedApp.ID || auditBody.Before != "pending" || auditBody.After != "approved" || auditBody.Reason != "Approve the retained disabled account" {
		t.Fatal("approval audit before/after/reason differs")
	}
	confirmed(decision(subject, review, "approve", "Approve the retained disabled account", 200), subject, "approved", false)
	if originalAudit != 1 || auditCount("member.approval.decide", subject.ID) != originalAudit || !reflect.DeepEqual(approvedApp, loadApp(subject)) {
		t.Fatal("terminal current-state retry added historical operation")
	}
	decision(subject, review, "reject", "Opposite terminal decision must fail", 409)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"approval-subject@example.com","password":"approval-password"}`, nil, ""), 401)
	// Explicit lifecycle restoration is distinct from approval. MFA state remains
	// enabled; use a fresh MFA completion before issuing a fresh product Key.
	disabled = false
	if _, err := svc.UpdateMember(ctx, admin.User.ID, subject.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	// A fresh application of the recorded MFA challenge cannot reuse the seeded
	// challenge: independent disable has already erased/revoked live sessions.
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"approval-subject@example.com","password":"approval-password"}`, nil, "")
	expectStatus(t, login, 202)
	var mfaReview struct {
		ChallengeToken string `json:"challenge_token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &mfaReview); err != nil {
		t.Fatal(err)
	}
	if mfaReview.ChallengeToken == "" {
		t.Fatal("fresh MFA challenge token missing")
	}
	auth, err := svc.CompleteMFALogin(ctx, mfaReview.ChallengeToken, service.MFAProof{Code: mfaFixtureCode(t, mfaSecret, 0)})
	if err != nil || auth == nil {
		t.Fatal("approved enabled account cannot complete fresh MFA")
	}
	if loadUser(subject.ID).LastLoginAt == nil {
		t.Fatal("actual successful MFA sign-in was not recorded")
	}
	// New grant and Key setup is explicit and separate; never prove approval by
	// borrowing a seeded pending Key or widening its ceiling.
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelID, []string{subject.ID, admin.User.ID}); err != nil {
		t.Fatal(err)
	}
	freshKey, err := svc.CreatePersonalKey(ctx, subject.ID, "Fresh approved Key", []string{modelID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ConfirmKeyDelivery(ctx, subject.ID, freshKey.Record.Key.ID); err != nil {
		t.Fatal(err)
	}
	positiveIDs := make([]string, 0, 5)
	for _, item := range nativeCases {
		res := invoke(item, freshKey.Secret, nil, "")
		expectStatus(t, res, 200)
		requestID := res.Header().Get("X-Request-ID")
		if requestID == "" {
			t.Fatal("completed native call has no retained request identity")
		}
		positiveIDs = append(positiveIDs, requestID)
	}
	if dispatches.Load() != 4 {
		t.Fatal("approved native dispatch ledger differs from four controlled calls")
	}
	projectCall := invoke(nativeCases[0], projectKey.Secret, nil, "")
	expectStatus(t, projectCall, 200)
	if projectCall.Header().Get("X-Request-ID") == "" {
		t.Fatal("Project completion lost request identity")
	}
	positiveIDs = append(positiveIDs, projectCall.Header().Get("X-Request-ID"))
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	var immutableCalls []entity.CallRecord
	if err := db.Where("request_id IN ?", positiveIDs).Order("request_id").Find(&immutableCalls).Error; err != nil || len(immutableCalls) != 5 {
		t.Fatal("five controlled calls were not durably retained", err)
	}
	for _, fact := range immutableCalls {
		wantedRoute, ok := exactRoutes[fact.Protocol]
		if !ok || fact.ProviderID != wantedRoute.ProviderID || fact.ProviderModelID != wantedRoute.ProviderModelID || fact.ConnectionID != wantedRoute.ConnectionID {
			t.Fatal("completed call differs from the exact owned native route")
		}
		if fact.Status != "success" || fact.ModelID != modelID || fact.SnapshotID == "" || fact.ConnectionID == "" || fact.ProviderModelID == "" || fact.InputTokens == nil || *fact.InputTokens != 2 || fact.OutputTokens == nil || *fact.OutputTokens != 1 {
			t.Fatal("native immutable routing/usage fact differs")
		}
		if fact.RequestID == projectCall.Header().Get("X-Request-ID") {
			if fact.ProjectID != project.ID || fact.KeyID != projectKey.Record.Key.ID {
				t.Fatal("Project completion borrowed Personal attribution")
			}
		} else if fact.UserID != subject.ID || fact.ProjectID != "" || fact.KeyID != freshKey.Record.Key.ID {
			t.Fatal("Personal native ownership differs")
		}
		assertStoredNativeCompletion(t, db, fact.RequestID, "completed")
	}
	if dispatches.Load() != 5 {
		t.Fatal("Project with another admitted manager lost continuity")
	}
	var totalCalls, totalAttempts int64
	if err := db.Model(&entity.CallRecord{}).Count(&totalCalls).Error; err != nil || totalCalls != 5 {
		t.Fatal("native call ledger is not exactly five owned completions", err)
	}
	if err := db.Model(&entity.CallAttempt{}).Count(&totalAttempts).Error; err != nil || totalAttempts != 5 {
		t.Fatal("denied native requests created dispatch attempts", err)
	}
	var immutableAttempts []entity.CallAttempt
	if err := db.Where("request_id IN ?", positiveIDs).Order("request_id, attempt_number").Find(&immutableAttempts).Error; err != nil || len(immutableAttempts) != 5 {
		t.Fatal("controlled completions lack exact immutable attempt count", err)
	}
	for _, attempt := range immutableAttempts {
		var protocol string
		for _, fact := range immutableCalls {
			if fact.RequestID == attempt.RequestID {
				protocol = fact.Protocol
				break
			}
		}
		route, ok := exactRoutes[protocol]
		if !ok || attempt.CredentialID != route.CredentialID || attempt.ConnectionID != route.ConnectionID || attempt.ProviderModelID != route.ProviderModelID || attempt.ProviderID != route.ProviderID {
			t.Fatal("attempt credential/route attribution differs from owned protocol route")
		}
		if attempt.CredentialID == "" || attempt.SnapshotID == "" || attempt.NativeCompletionEvidence != "completed" || attempt.Status != "success" || !attempt.FinalUsageKnown {
			t.Fatal("completed attempt lost credential/snapshot/terminal/usage proof")
		}
	}

	// Rejection is distinct from disable/offboarding: an otherwise enabled
	// retained user stays denied while seeded possession and inert grants remain
	// unchanged. Every protocol uses the same actually ready owned routing set.
	rejectedUser := pending("rejected", nil)
	if _, err := svc.SetModelGrants(ctx, admin.User.ID, modelID, []string{subject.ID, admin.User.ID, rejectedUser.ID}); err != nil {
		t.Fatal(err)
	}
	rejectedBearer := "rx_" + strings.Repeat("r", 43)
	rejectedKey := entity.APIKey{ID: "key_approval_rejected", UserID: rejectedUser.ID, Name: "Controlled rejected possession", Prefix: "rx_test", TokenHash: secret.SHA256Hex(rejectedBearer), Status: entity.KeyActive}
	if err := db.Create(&rejectedKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&entity.APIKeyModel{KeyID: rejectedKey.ID, ModelID: modelID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&rejectedKey, "id = ?", rejectedKey.ID).Error; err != nil {
		t.Fatal(err)
	}
	// Inert grant preparation legitimately advances only its private generation.
	// Capture the complete persisted baseline before the reviewed rejection.
	rejectedPrepared := loadUser(rejectedUser.ID)
	rejectedExpected := rejectedUser
	rejectedExpected.PersonalGrantRevision = rejectedPrepared.PersonalGrantRevision
	if rejectedPrepared.PersonalGrantRevision == rejectedUser.PersonalGrantRevision || !reflect.DeepEqual(rejectedExpected, rejectedPrepared) {
		t.Fatal("rejected grant preparation changed unexpected User facts")
	}
	rejectedUser = rejectedPrepared
	rejectedReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, rejectedUser.ID)
	confirmed(decision(rejectedUser, rejectedReview, "reject", "Reject enabled pending account", 200), rejectedUser, "rejected", false)
	var rejectedKeyAfter entity.APIKey
	if err := db.First(&rejectedKeyAfter, "id = ?", rejectedKey.ID).Error; err != nil || !reflect.DeepEqual(rejectedKey, rejectedKeyAfter) || !reflect.DeepEqual(rejectedUser, loadUser(rejectedUser.ID)) {
		t.Fatal("rejection changed enabled user or old Key facts")
	}
	for _, item := range nativeCases {
		expectStatus(t, invoke(item, rejectedBearer, nil, ""), 401)
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"approval-rejected@example.com","password":"approval-password"}`, nil, ""), 401)
	if dispatches.Load() != 5 {
		t.Fatal("rejected native requests reached the ready upstream")
	}
	// The database permits a nullable one-way link; literal empty is not NULL.
	// This deliberate private corruption must project unknown/conflict and deny,
	// rather than borrow the existing application or assume historical admission.
	originalLink := *rejectedUser.ApprovalApplicationID
	if err := db.Model(&entity.User{}).Where("id = ?", rejectedUser.ID).UpdateColumn("ApprovalApplicationID", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, approvalFixtureRequest(t, router, "GET", "/api/v1/admin/members/"+rejectedUser.ID+"/approval", nil, adminCookie, admin.CSRFToken, ""), 409)
	expectStatus(t, invoke(nativeCases[0], rejectedBearer, nil, ""), 401)
	if err := db.Model(&entity.User{}).Where("id = ?", rejectedUser.ID).UpdateColumn("ApprovalApplicationID", originalLink).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RefreshRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rejectedUser, loadUser(rejectedUser.ID)) {
		t.Fatal("controlled link restoration changed retained identity")
	}
	// First terminal winner, audit rollback, committed-but-unpublished retry and
	// a newly constructed service exercise distinct transactional boundaries.
	raceUser := pending("race", nil)
	raceReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, raceUser.ID)
	var raceStatus [2]int
	var group sync.WaitGroup
	for index, want := range []string{"approve", "reject"} {
		group.Add(1)
		go func(index int, want string) {
			defer group.Done()
			res := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/members/"+raceUser.ID+"/approval", map[string]any{"decision": want, "reason": "First terminal decision wins"}, adminCookie, admin.CSRFToken, raceReview.ReviewETag)
			raceStatus[index] = res.Code
		}(index, want)
	}
	group.Wait()
	oneDecisionSucceeded := raceStatus[0] == 200 && raceStatus[1] == 409 || raceStatus[0] == 409 && raceStatus[1] == 200
	if !oneDecisionSucceeded || auditCount("member.approval.decide", raceUser.ID) != 1 {
		t.Fatal("concurrent opposite decisions did not have one terminal writer", raceStatus)
	}
	rollbackUser := pending("rollback", nil)
	rollbackReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, rollbackUser.ID)
	rollbackApp := loadApp(rollbackUser)
	failAudit.Store(true)
	decision(rollbackUser, rollbackReview, "approve", "Audit rollback must preserve pending", 503)
	failAudit.Store(false)
	if !reflect.DeepEqual(rollbackApp, loadApp(rollbackUser)) || auditCount("member.approval.decide", rollbackUser.ID) != 0 || !reflect.DeepEqual(rollbackUser, loadUser(rollbackUser.ID)) {
		t.Fatal("audit failure committed decision or identity mutation")
	}
	uncertainUser := pending("uncertain", nil)
	uncertainReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, uncertainUser.ID)
	// Fail only the runtime batch read, not the preceding locked application read.
	failPublication.Store(true)
	decision(uncertainUser, uncertainReview, "approve", "Reconcile exact committed approval", 503)
	failPublication.Store(false)
	uncertainApp := loadApp(uncertainUser)
	if uncertainApp.State != "approved" || auditCount("member.approval.decide", uncertainUser.ID) != 1 {
		t.Fatal("publication failure was not genuine committed uncertainty")
	}
	confirmed(decision(uncertainUser, uncertainReview, "approve", "Reconcile exact committed approval", 200), uncertainUser, "approved", true)
	if !reflect.DeepEqual(uncertainApp, loadApp(uncertainUser)) || auditCount("member.approval.decide", uncertainUser.ID) != 1 {
		t.Fatal("uncertain current-state retry repeated a write")
	}

	offboardUser := pending("offboarded", nil)
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, offboardUser.ID, service.OffboardingEmergencyInput{RequestID: "2cb45772-613b-4ae2-90d4-1d6dc6520944", CurrentPassword: "approval-password", Reason: "Close pending retained identity"}); err != nil {
		t.Fatal(err)
	}
	offboardUser = loadUser(offboardUser.ID)
	offboardBefore := offboardUser
	offboardReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, offboardUser.ID)
	if offboardReview.CanApprove || !offboardReview.CanReject || offboardReview.AdmissionEligible {
		t.Fatal("offboarded pending decision authority differs")
	}
	decision(offboardUser, offboardReview, "approve", "Offboarded cannot be approved", 409)
	confirmed(decision(offboardUser, offboardReview, "reject", "Reject offboarded pending identity", 200), offboardUser, "rejected", false)
	if !reflect.DeepEqual(offboardBefore, loadUser(offboardUser.ID)) {
		t.Fatal("rejection reactivated offboarded identity")
	}
	// A failed application creation rolls back the user, link, default policy and
	// registration audit together. It must not leave an unlinked usable account.
	failApplication.Store(true)
	registerAudits := auditCount("member.register", "")
	var allAuditsBefore []entity.AuditEvent
	if err := db.Order("id").Find(&allAuditsBefore).Error; err != nil {
		t.Fatal(err)
	}
	var limitsBefore []entity.ResourceLimit
	if err := db.Order("scope_kind, scope_id").Find(&limitsBefore).Error; err != nil {
		t.Fatal(err)
	}

	failedRegistration := identityRequest(router, "POST", "/api/v1/auth/register", `{"email":"approval-failed-create@example.com","password":"approval-password","name":"Atomic creation"}`, nil, "")
	expectStatus(t, failedRegistration, 500)
	failApplication.Store(false)
	var failedUsers int64
	if err := db.Model(&entity.User{}).Where("email = ?", "approval-failed-create@example.com").Count(&failedUsers).Error; err != nil || failedUsers != 0 || auditCount("member.register", "") != registerAudits {
		t.Fatal("failed registration left identity or audit")
	}
	var allAuditsAfter []entity.AuditEvent
	if err := db.Order("id").Find(&allAuditsAfter).Error; err != nil || !reflect.DeepEqual(allAuditsBefore, allAuditsAfter) {
		t.Fatal("failed registration left a default-policy or registration audit")
	}
	var limitsAfter []entity.ResourceLimit
	if err := db.Order("scope_kind, scope_id").Find(&limitsAfter).Error; err != nil || !reflect.DeepEqual(limitsBefore, limitsAfter) {
		t.Fatal("failed registration left a default resource policy")
	}
	if err := svc.FlushCallRecorder(ctx); err != nil {
		t.Fatal(err)
	}
	stopRecorder()
	if err := svc.StopCallRecorder(); err != nil {
		t.Fatal(err)
	}
	svc.StopRuntime()
	stoppedReview := approvalFixtureMemberReview(t, router, adminCookie, admin.CSRFToken, uncertainUser.ID)
	if stoppedReview.RuntimeApplied {
		t.Fatal("stopped publication owner claimed exact admission application")
	}
	decision(uncertainUser, uncertainReview, "approve", "Reconcile exact committed approval", 503)
	if auditCount("member.approval.decide", uncertainUser.ID) != 1 || !reflect.DeepEqual(uncertainApp, loadApp(uncertainUser)) {
		t.Fatal("stopped current-state retry wrote another decision")
	}
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store), service.WithUpstreamPolicy(true))
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.StartRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	defer restarted.StopRuntime()
	restartedRouter := fox.New()
	New(restarted).RegisterRoutes(restartedRouter)
	renewed := approvalFixtureMemberReview(t, restartedRouter, adminCookie, admin.CSRFToken, uncertainUser.ID)
	if renewed.ApprovalStatus != "approved" || !renewed.AdmissionEligible || !renewed.RuntimeApplied || renewed.Application == nil || renewed.Application.ID != uncertainApp.ID {
		t.Fatal("same database restart lost durable application/current proof")
	}
	var callsAfter []entity.CallRecord
	if err := db.Where("request_id IN ?", positiveIDs).Order("request_id").Find(&callsAfter).Error; err != nil || !reflect.DeepEqual(immutableCalls, callsAfter) {
		t.Fatal("decision/retry/restart changed immutable native history")
	}
	var attemptsAfter []entity.CallAttempt
	if err := db.Where("request_id IN ?", positiveIDs).Order("request_id, attempt_number").Find(&attemptsAfter).Error; err != nil || !reflect.DeepEqual(immutableAttempts, attemptsAfter) {
		t.Fatal("decision/reconciliation/restart changed immutable attempt proofs")
	}
	if nativeApprovalQueries.Load() != 0 {
		t.Fatal("native approval gating performed live approval/User SQL")
	}
	if dispatches.Load() != 5 {
		t.Fatal("read/reconciliation/restart sent an extra native call")
	}
}
