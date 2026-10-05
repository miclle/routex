package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"gorm.io/gorm"
)

func testRegistrationEmailDomains(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	svc, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	setup := identityRequest(router, "POST", "/api/v1/setup", `{"email":"domains-admin@example.invalid","password":"test-only-domain-password","name":"Domain administrator"}`, nil, "")
	expectStatus(t, setup, 201)
	admin, cookie := readIdentity(t, setup)
	review := func() service.RegistrationPolicy {
		t.Helper()
		r := identityRequest(router, "GET", "/api/v1/admin/registration", "", cookie, "")
		expectStatus(t, r, 200)
		value := decodeCatalogResponse[service.RegistrationPolicy](t, r, 200)
		if r.Header().Get("ETag") != `"`+value.ReviewETag+`"` || value.AllowedEmailDomains == nil {
			t.Fatal("invalid complete domain review")
		}
		return value
	}
	put := func(etag string, required bool, domains []string, reason string, status int) service.RegistrationPolicyResult {
		t.Helper()
		r := approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true, "approval_required": required, "allowed_email_domains": domains, "reason": reason}, cookie, admin.CSRFToken, etag)
		expectStatus(t, r, status)
		if status != 200 {
			return service.RegistrationPolicyResult{}
		}
		value := decodeCatalogResponse[service.RegistrationPolicyResult](t, r, 200)
		if value.Confirmation != "current_registration_policy" || r.Header().Get("ETag") != `"`+value.ReviewETag+`"` {
			t.Fatal("policy current-state confirmation absent")
		}
		return value
	}
	initial := review()
	restricted := put(initial.ReviewETag, false, []string{" A.INVALID "}, "Allow one exact email domain", 200)
	if !slices.Equal(restricted.AllowedEmailDomains, []string{"a.invalid"}) || restricted.ReviewETag == initial.ReviewETag {
		t.Fatal("domain normalization/generation missing")
	}
	delegated, delegatedCookie, delegatedCSRF := createSystemStatusMember(t, svc, router, admin.User.ID, "domains-delegated", nil)
	// Seed only a retained legacy permission: ordinary Role writers forbid delegation.
	var assigned entity.UserRole
	if err := db.Where("user_id = ?", delegated.User.ID).First(&assigned).Error; err != nil || assigned.UserID != delegated.User.ID || assigned.RoleID == "" {
		t.Fatal("controlled delegated fixture ownership differs", err)
	}
	if err := db.Create(&entity.RolePermission{RoleID: assigned.RoleID, Permission: "registration.write"}).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/registration", "", delegatedCookie, ""), 403)
	expectStatus(t, approvalFixtureRequest(t, router, "PATCH", "/api/v1/admin/registration", map[string]any{"enabled": true, "approval_required": false, "allowed_email_domains": []string{}, "reason": "Delegated policy denied"}, delegatedCookie, delegatedCSRF, restricted.ReviewETag), 403)
	public := identityRequest(router, "GET", "/api/v1/auth/registration", "", nil, "")
	approvalFixtureObject(t, public, 200, "enabled", "approval_required", "allowed_email_domains")
	if public.Header().Get("ETag") != "" {
		t.Fatal("public guidance leaked private validator")
	}
	counters := func() []int64 {
		t.Helper()
		var values []int64
		for _, table := range []string{"users", "sessions", "registration_approval_applications", "audit_events", "user_mfa", "mfa_challenges", "user_model_grants", "api_keys", "resource_limits", "call_records", "call_attempts"} {
			var n int64
			if err := db.Table(table).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			values = append(values, n)
		}
		return values
	}
	register := func(email string, status int) *http.Cookie {
		t.Helper()
		raw, _ := json.Marshal(map[string]string{"email": email, "password": "test-only-domain-password", "name": "Applicant"})
		r := identityRequest(router, "POST", "/api/v1/auth/register", string(raw), nil, "")
		expectStatus(t, r, status)
		if status == 201 {
			_, value := readIdentity(t, r)
			return value
		}
		if len(r.Result().Cookies()) != 0 {
			t.Fatal("nonadmitted registration issued authentication cookie")
		}
		return nil
	}
	for _, email := range []string{"denied@b.invalid", "denied@sub.a.invalid", "denied@not-a.invalid", "denied@a.invalid.attacker.invalid"} {
		before := counters()
		register(email, 403)
		if !reflect.DeepEqual(counters(), before) {
			t.Fatal("denied domain persisted identity/security/grant/audit facts")
		}
	}
	register("  MEMBER@A.INVALID  ", 201)
	var existing entity.User
	if err := db.Where("email = ?", "member@a.invalid").First(&existing).Error; err != nil || existing.LastLoginAt != nil || existing.ApprovalApplicationID != nil {
		t.Fatal("ordinary domain registration changed signin/approval contract", err)
	}
	assertNoImplicitResources := func(userID string, sessionCount int64) {
		t.Helper()
		for _, table := range []string{"sessions", "api_keys", "user_model_grants", "user_mfa", "mfa_challenges"} {
			var n int64
			if err := db.Table(table).Where("user_id = ?", userID).Count(&n).Error; err != nil {
				t.Fatal(err)
			}
			want := int64(0)
			if table == "sessions" {
				want = sessionCount
			}
			if n != want {
				t.Fatal("registration created unintended authentication/resource facts", table)
			}
		}
	}
	assertNoImplicitResources(existing.ID, 1)
	policy := put(restricted.ReviewETag, true, []string{"a.invalid"}, "Require approved registration", 200)
	pendingResponse := approvalFixtureRequest(t, router, "POST", "/api/v1/auth/register", map[string]any{"email": "pending@a.invalid", "password": "test-only-domain-password", "name": "Pending"}, nil, "", "")
	approvalFixtureObject(t, pendingResponse, 202, "kind")
	if len(pendingResponse.Result().Cookies()) != 0 {
		t.Fatal("pending registration created Session")
	}
	var pending entity.User
	if err := db.Where("email = ?", "pending@a.invalid").First(&pending).Error; err != nil || pending.ApprovalApplicationID == nil || pending.LastLoginAt != nil {
		t.Fatal("pending facts incorrect", err)
	}
	var appBefore, appAfter entity.RegistrationApprovalApplication
	if err := db.First(&appBefore, "id = ?", *pending.ApprovalApplicationID).Error; err != nil {
		t.Fatal(err)
	}
	if appBefore.ID != *pending.ApprovalApplicationID || appBefore.UserID != pending.ID || !appBefore.UserCreatedAt.Equal(pending.CreatedAt) || appBefore.State != "pending" || appBefore.CreatedAt.IsZero() || appBefore.DecidedAt != nil || appBefore.DecisionActorID != nil || appBefore.DecisionReason != nil {
		t.Fatal("pending application invented admission/decision facts")
	}
	assertNoImplicitResources(pending.ID, 0)
	var auditBefore int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "registration.policy.update").Count(&auditBefore).Error; err != nil {
		t.Fatal(err)
	}
	_ = put(initial.ReviewETag, true, []string{"b.invalid"}, "Stale differing policy", 409)
	current := put(policy.ReviewETag, true, []string{"b.invalid"}, "Change future applicants only", 200)
	if err := db.First(&appAfter, "id = ?", appBefore.ID).Error; err != nil || !reflect.DeepEqual(appBefore, appAfter) {
		t.Fatal("domain policy changed existing application history")
	}
	var existingAfter entity.User
	if err := db.First(&existingAfter, "id = ?", existing.ID).Error; err != nil || !reflect.DeepEqual(existing, existingAfter) {
		t.Fatal("domain policy changed existing User")
	}
	// A stale identical retry confirms only current policy and must not write again.
	_ = put(policy.ReviewETag, true, []string{"b.invalid"}, "Change future applicants only", 200)
	var auditAfter int64
	if err := db.Model(&entity.AuditEvent{}).Where("action = ?", "registration.policy.update").Count(&auditAfter).Error; err != nil || auditAfter != auditBefore+1 {
		t.Fatal("current-state replay duplicated policy audit")
	}
	login := identityRequest(router, "POST", "/api/v1/auth/login", `{"email":"member@a.invalid","password":"test-only-domain-password"}`, nil, "")
	expectStatus(t, login, 200)
	failureName := "domain-policy-atomic-audit-fixture"
	if err := db.Callback().Create().Before("gorm:create").Register(failureName, func(tx *gorm.DB) {
		if value, ok := tx.Statement.Dest.(*entity.AuditEvent); ok && value.Action == "registration.policy.update" {
			_ = tx.AddError(errors.New("controlled domain audit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	callbackRegistered := true
	t.Cleanup(func() {
		if callbackRegistered {
			if err := db.Callback().Create().Remove(failureName); err != nil {
				t.Error(err)
			}
		}
	})
	_ = put(current.ReviewETag, false, []string{}, "Rollback failed policy audit", 503)
	if got := review(); got.ReviewETag != current.ReviewETag || !slices.Equal(got.AllowedEmailDomains, []string{"b.invalid"}) {
		t.Fatal("audit failure persisted policy")
	}
	if err := db.Callback().Create().Remove(failureName); err != nil {
		t.Fatal(err)
	}
	callbackRegistered = false
	// Same-DB new Service proves durable policy reads; process restart is root-owned.
	restarted, err := service.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := restarted.GetRegistrationPolicy(ctx, admin.User.ID)
	if err != nil || retained.ReviewETag != current.ReviewETag || !slices.Equal(retained.AllowedEmailDomains, []string{"b.invalid"}) {
		t.Fatal("new Service lost saved domains", err)
	}
	before := counters()
	register("later@a.invalid", 403)
	if !reflect.DeepEqual(counters(), before) {
		t.Fatal("post-reconfiguration denial changed facts")
	}
}
