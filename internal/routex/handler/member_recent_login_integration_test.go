package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
	"gorm.io/gorm"
)

func testMemberRecentLoginLifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New([]byte(strings.Repeat("r", 32)))
	if err != nil {
		t.Fatal(err)
	}
	var failSession, failStamp, failAudit atomic.Bool
	const callback = "test:recent-login-fault"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "sessions" && failSession.Load() || q.Statement.Table == "audit_events" && failAudit.Load() {
			_ = q.AddError(errors.New("controlled sign-in commit failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(callback, func(q *gorm.DB) {
		if q.Statement.Table == "users" && failStamp.Load() {
			_ = q.AddError(errors.New("controlled timestamp failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	var instances []*service.Service
	t.Cleanup(func() {
		for _, s := range instances {
			s.StopRuntime()
			_ = s.StopCallRecorder()
		}
		if err := db.Callback().Create().Remove(callback); err != nil {
			t.Error(err)
		}
		if err := db.Callback().Update().Remove(callback); err != nil {
			t.Error(err)
		}
	})
	fresh := func() (*service.Service, *fox.Engine) {
		t.Helper()
		s, err := service.New(ctx, db, service.WithCredentialStorage(store))
		if err != nil {
			t.Fatal(err)
		}
		instances = append(instances, s)
		router := fox.New()
		New(s).RegisterRoutes(router)
		return s, router
	}
	svc, router := fresh()
	password := "Recent login test password"
	setup := identityRequest(router, "POST", "/api/v1/setup", mfaFixtureBody(SetupRequest{Email: "login-admin@example.invalid", Password: password, Name: "Login administrator"}), nil, "")
	expectStatus(t, setup, 201)
	admin, adminCookie := readIdentity(t, setup)
	readUser := func(id string) entity.User {
		t.Helper()
		var u entity.User
		if err := db.First(&u, "id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		return u
	}
	if readUser(admin.User.ID).LastLoginAt != nil {
		t.Fatal("setup counted as sign-in")
	}
	member, err := svc.CreateMember(ctx, admin.User.ID, "login-subject@example.invalid", password, "Subject", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	id := member.User.ID
	initial := readUser(id)
	if initial.LastLoginAt != nil {
		t.Fatal("member creation invented sign-in")
	}
	login := func(p string) *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/auth/login", mfaFixtureBody(LoginRequest{Email: initial.Email, Password: p}), nil, "")
	}
	assertUnchanged := func(want entity.User) {
		t.Helper()
		if !reflect.DeepEqual(readUser(id), want) {
			t.Fatal("failed/non-login operation changed retained User")
		}
	}
	expectStatus(t, login("Wrong recent login password"), 401)
	assertUnchanged(initial)
	for _, flag := range []*atomic.Bool{&failSession, &failStamp} {
		flag.Store(true)
		expectStatus(t, login(password), 500)
		flag.Store(false)
		assertUnchanged(initial)
	}
	signed := login(password)
	expectStatus(t, signed, 200)
	session, cookie := readIdentity(t, signed)
	recorded := readUser(id)
	assertOnlyLogin := func(want entity.User, got entity.User) {
		t.Helper()
		got.LastLoginAt = want.LastLoginAt
		if !reflect.DeepEqual(got, want) {
			t.Fatal("sign-in changed unrelated User fields")
		}
	}
	if recorded.LastLoginAt == nil || recorded.LastLoginAt.Nanosecond()%1000 != 0 {
		t.Fatal("successful password login not recorded")
	}
	without := recorded
	without.LastLoginAt = nil
	if !reflect.DeepEqual(without, initial) {
		t.Fatal("login changed identity/security/metadata revision")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", cookie, ""), 200)
	assertUnchanged(recorded)
	detail := decodeCatalogResponse[MemberDetailResponse](t, identityRequest(router, "GET", "/api/v1/admin/members/"+id, "", adminCookie, ""), 200)
	if detail.LastLoginStatus != "recorded" || detail.LastLoginAt == nil || !detail.LastLoginAt.Equal(*recorded.LastLoginAt) {
		t.Fatal("GET-only login projection differs")
	}
	list := decodeCatalogResponse[MemberListResponse](t, identityRequest(router, "GET", "/api/v1/admin/members?q="+initial.Email, "", adminCookie, ""), 200)
	if len(list.Items) != 1 || list.Items[0].LastLoginAt == nil || !list.Items[0].LastLoginAt.Equal(*recorded.LastLoginAt) {
		t.Fatal("bounded list lost recorded login")
	}
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members/"+strings.ToUpper(id), "", adminCookie, ""), 404)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/admin/members/"+id+"?last_login_at=now", "", adminCookie, ""), 400)
	// MFA enrollment/enable rotates a Session; that security replacement is not sign-in.
	enroll := decodeCatalogResponse[service.MFAEnrollment](t, identityRequest(router, "POST", "/api/v1/account/mfa/enrollment", mfaFixtureBody(MFAPasswordRequest{CurrentPassword: password}), cookie, session.CSRFToken), 200)
	enabledResponse := identityRequest(router, "POST", "/api/v1/account/mfa/enable", mfaFixtureBody(MFAEnableRequest{CurrentPassword: password, EnrollmentToken: enroll.EnrollmentToken, Code: mfaFixtureCode(t, enroll.Secret, 0)}), cookie, session.CSRFToken)
	enabled := decodeCatalogResponse[MFARecoveryResponse](t, enabledResponse, 200)
	if len(enabled.RecoveryCodes) != 10 {
		t.Fatal("controlled enrollment missing recovery proof")
	}
	assertUnchanged(recorded)
	challenge := decodeCatalogResponse[service.MFALoginChallenge](t, login(password), 202)
	assertUnchanged(recorded)
	verify := func(token string, proof service.MFAProof) *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/auth/mfa/verify", mfaFixtureBody(MFALoginRequest{ChallengeToken: token, Code: proof.Code, RecoveryCode: proof.RecoveryCode}), nil, "")
	}
	expectStatus(t, verify(challenge.ChallengeToken, service.MFAProof{RecoveryCode: strings.Repeat("X", 24)}), 401)
	assertUnchanged(recorded)
	totp := verify(challenge.ChallengeToken, service.MFAProof{Code: mfaFixtureCode(t, enroll.Secret, 1)})
	expectStatus(t, totp, 200)
	_, _ = readIdentity(t, totp)
	afterTOTP := readUser(id)
	assertOnlyLogin(recorded, afterTOTP)
	if afterTOTP.LastLoginAt == nil || afterTOTP.LastLoginAt.Before(*recorded.LastLoginAt) {
		t.Fatal("TOTP login regressed observation")
	}
	expectStatus(t, verify(challenge.ChallengeToken, service.MFAProof{Code: mfaFixtureCode(t, enroll.Secret, 1)}), 401)
	assertUnchanged(afterTOTP)
	challenge = decodeCatalogResponse[service.MFALoginChallenge](t, login(password), 202)
	failAudit.Store(true)
	expectStatus(t, verify(challenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[0]}), 500)
	failAudit.Store(false)
	assertUnchanged(afterTOTP)
	recovered := verify(challenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[0]})
	expectStatus(t, recovered, 200)
	recoverySession, recoveryCookie := readIdentity(t, recovered)
	afterRecovery := readUser(id)
	assertOnlyLogin(afterTOTP, afterRecovery)
	if afterRecovery.LastLoginAt == nil || afterRecovery.LastLoginAt.Before(*afterTOTP.LastLoginAt) {
		t.Fatal("recovery login regressed observation")
	}
	expectStatus(t, verify(challenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[0]}), 401)
	assertUnchanged(afterRecovery)
	// Genuine new Service instance, retained schema/store/Sessions. No native call.
	_, router = fresh()
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", recoveryCookie, ""), 200)
	assertUnchanged(afterRecovery)
	expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/logout", "", recoveryCookie, recoverySession.CSRFToken), 204)
	assertUnchanged(afterRecovery)
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", recoveryCookie, ""), 401)
	// Generic registration and password security replacement remain distinct from sign-in.
	expectStatus(t, identityRequest(router, "PATCH", "/api/v1/admin/registration", `{"enabled":true}`, adminCookie, admin.CSRFToken), 200)
	registered := identityRequest(router, "POST", "/api/v1/auth/register", mfaFixtureBody(map[string]string{"email": "login-registered@example.invalid", "name": "Registered", "password": password}), nil, "")
	expectStatus(t, registered, 201)
	registeredAuth, registeredCookie := readIdentity(t, registered)
	registeredUser := readUser(registeredAuth.User.ID)
	if registeredUser.LastLoginAt != nil {
		t.Fatal("registration counted as sign-in")
	}
	changed := identityRequest(router, "POST", "/api/v1/account/password", mfaFixtureBody(ChangePasswordRequest{CurrentPassword: password, NewPassword: "Changed recent login password"}), registeredCookie, registeredAuth.CSRFToken)
	expectStatus(t, changed, 200)
	if readUser(registeredUser.ID).LastLoginAt != nil {
		t.Fatal("password security replacement counted as sign-in")
	}
	// Genuine concurrent password logins serialize on the current User. No clock forcing.
	parallel, err := svc.CreateMember(ctx, admin.User.ID, "login-parallel@example.invalid", password, "Parallel", entity.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	parallelBefore := readUser(parallel.User.ID)
	var wg sync.WaitGroup
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() {
			responses <- identityRequest(router, "POST", "/api/v1/auth/login", mfaFixtureBody(LoginRequest{Email: parallelBefore.Email, Password: password}), nil, "")
		})
	}
	wg.Wait()
	close(responses)
	for response := range responses {
		expectStatus(t, response, 200)
	}
	parallelAfter := readUser(parallel.User.ID)
	assertOnlyLogin(parallelBefore, parallelAfter)
	if parallelAfter.LastLoginAt == nil {
		t.Fatal("concurrent committed logins not recorded")
	}
	for _, offboarded := range []bool{false, true} {
		stamp := time.Now().UTC().Truncate(time.Microsecond)
		updates := map[string]any{"disabled": true}
		if offboarded {
			updates["offboarded_at"] = &stamp
		}
		if err := db.Model(&entity.User{}).Where("id = ?", parallel.User.ID).UpdateColumns(updates).Error; err != nil {
			t.Fatal(err)
		}
		before := readUser(parallel.User.ID)
		expectStatus(t, identityRequest(router, "POST", "/api/v1/auth/login", mfaFixtureBody(LoginRequest{Email: before.Email, Password: password}), nil, ""), 401)
		if !reflect.DeepEqual(readUser(before.ID), before) {
			t.Fatal("inactive subject denial changed User")
		}
		retained := decodeCatalogResponse[MemberDetailResponse](t, identityRequest(router, "GET", "/api/v1/admin/members/"+before.ID, "", adminCookie, ""), 200)
		if retained.LastLoginAt == nil || !retained.LastLoginAt.Equal(*before.LastLoginAt) {
			t.Fatal("inactive retained subject lost login observation")
		}
	}
}
