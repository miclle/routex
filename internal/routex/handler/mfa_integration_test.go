package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fox-gonic/fox"
	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secretstore"
)

func mfaFixtureCode(t *testing.T, encoded string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(encoded)
	if err != nil {
		t.Fatal("invalid fixture secret")
	}
	counter := make([]byte, 8)
	binary.BigEndian.PutUint64(counter, uint64(time.Now().Unix()/30+offset))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter)
	digest := mac.Sum(nil)
	index := digest[len(digest)-1] & 15
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(digest[index:index+4])&0x7fffffff)%1000000)
}
func mfaFixtureBody(value any) string { data, _ := json.Marshal(value); return string(data) }

func testMFALifecycle(t *testing.T, db *gorm.DB) {
	t.Helper()
	ctx := context.Background()
	store, err := secretstore.New(bytes.Repeat([]byte{11}, 32))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal(err)
	}
	router := fox.New()
	New(svc).RegisterRoutes(router)
	password := "test-only-mfa-password"
	setup := identityRequest(router, "POST", "/api/v1/setup", mfaFixtureBody(SetupRequest{Email: "mfa-admin@example.invalid", Password: password, Name: "MFA administrator"}), nil, "")
	expectStatus(t, setup, 201)
	admin, _ := readIdentity(t, setup)
	var administrator entity.User
	if err := db.First(&administrator, "id = ?", admin.User.ID).Error; err != nil {
		t.Fatal(err)
	}
	member := entity.User{ID: "usr_mfa_member", Email: "mfa-member@example.invalid", Name: "MFA member", Role: entity.RoleMember, PasswordHash: administrator.PasswordHash}
	if err := db.Create(&member).Error; err != nil {
		t.Fatal(err)
	}
	login := func() *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/auth/login", mfaFixtureBody(LoginRequest{Email: member.Email, Password: password}), nil, "")
	}
	first := login()
	expectStatus(t, first, 200)
	session, cookie := readIdentity(t, first)
	enroll := func() *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/account/mfa/enrollment", mfaFixtureBody(MFAPasswordRequest{CurrentPassword: password}), cookie, session.CSRFToken)
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/account/mfa/enrollment", mfaFixtureBody(MFAPasswordRequest{CurrentPassword: "wrong-test-only-password"}), cookie, session.CSRFToken), 401)
	initial := decodeCatalogResponse[service.MFAEnrollment](t, enroll(), 200)
	pending := decodeCatalogResponse[service.MFAEnrollment](t, enroll(), 200)
	if initial.Secret == pending.Secret || initial.EnrollmentToken == pending.EnrollmentToken {
		t.Fatal("new enrollment reused secret or token")
	}
	enable := func(value service.MFAEnrollment) *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/account/mfa/enable", mfaFixtureBody(MFAEnableRequest{CurrentPassword: password, EnrollmentToken: value.EnrollmentToken, Code: mfaFixtureCode(t, value.Secret, -1)}), cookie, session.CSRFToken)
	}
	expectStatus(t, enable(initial), 401)
	if err := db.Model(&entity.MFAChallenge{}).Where("user_id = ?", member.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, enable(pending), 401)
	enrollmentResponse := enroll()
	expectStatus(t, enrollmentResponse, 200)
	if enrollmentResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("enrollment secrets may be cached")
	}
	enrollment := decodeCatalogResponse[service.MFAEnrollment](t, enrollmentResponse, 200)
	var state entity.UserMFA
	if err := db.First(&state, "user_id = ?", member.ID).Error; err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.SecretCiphertext == enrollment.Secret || strings.Contains(state.SecretCiphertext, enrollment.Secret) {
		t.Fatal("pending secret not encrypted")
	}
	plain, err := store.Open("mfa:"+member.ID+":"+state.Generation, state.SecretCiphertext)
	if err != nil || plain != enrollment.Secret {
		t.Fatal("encrypted MFA secret lost")
	}
	expectStatus(t, identityRequest(router, "POST", "/api/v1/account/mfa/enable", mfaFixtureBody(MFAEnableRequest{CurrentPassword: password, EnrollmentToken: enrollment.EnrollmentToken, Code: "123456"}), cookie, ""), 403)
	enabledResponse := enable(enrollment)
	enabled := decodeCatalogResponse[MFARecoveryResponse](t, enabledResponse, 200)
	if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 || len(enabledResponse.Result().Cookies()) != 1 {
		t.Fatal("enable did not rotate session and deliver recovery codes")
	}
	oldCookie := cookie
	cookie = enabledResponse.Result().Cookies()[0]
	session = *enabled.Session
	expectStatus(t, identityRequest(router, "GET", "/api/v1/auth/session", "", oldCookie, ""), 401)
	statusResponse := identityRequest(router, "GET", "/api/v1/account/mfa", "", cookie, "")
	status := decodeCatalogResponse[service.MFAStatus](t, statusResponse, 200)
	if !status.Enabled || status.EnrollmentPending || status.RecoveryCodesRemaining != 10 || strings.Contains(statusResponse.Body.String(), enrollment.Secret) || strings.Contains(statusResponse.Body.String(), enabled.RecoveryCodes[0]) {
		t.Fatal("safe MFA status violated")
	}
	if auth, err := svc.Login(ctx, member.Email, password); err == nil || auth != nil {
		t.Fatal("password-only service login bypassed MFA")
	}
	var sessionsBefore int64
	if err := db.Model(&entity.Session{}).Where("user_id = ?", member.ID).Count(&sessionsBefore).Error; err != nil {
		t.Fatal(err)
	}
	begin := func() service.MFALoginChallenge {
		response := login()
		value := decodeCatalogResponse[service.MFALoginChallenge](t, response, 202)
		if !value.MFARequired || len(response.Result().Cookies()) != 0 || strings.Contains(response.Body.String(), "csrf_token") || strings.Contains(response.Body.String(), member.Email) {
			t.Fatal("challenge issued session authority or identity")
		}
		return value
	}
	challenge := begin()
	var sessionsAfter int64
	if err := db.Model(&entity.Session{}).Where("user_id = ?", member.ID).Count(&sessionsAfter).Error; err != nil || sessionsAfter != sessionsBefore {
		t.Fatal("password step created a session")
	}
	verify := func(token string, proof service.MFAProof) *httptest.ResponseRecorder {
		return identityRequest(router, "POST", "/api/v1/auth/mfa/verify", mfaFixtureBody(MFALoginRequest{ChallengeToken: token, Code: proof.Code, RecoveryCode: proof.RecoveryCode}), nil, "")
	}
	currentCode := mfaFixtureCode(t, enrollment.Secret, 0)
	verified := verify(challenge.ChallengeToken, service.MFAProof{Code: currentCode})
	expectStatus(t, verified, 200)
	expectStatus(t, verify(challenge.ChallengeToken, service.MFAProof{Code: currentCode}), 401)
	replay := begin()
	expectStatus(t, verify(replay.ChallengeToken, service.MFAProof{Code: currentCode}), 401)
	// Challenge replacement never resets the account-level invalid-proof budget.
	for range 4 {
		next := begin()
		expectStatus(t, verify(next.ChallengeToken, service.MFAProof{Code: "invalid"}), 401)
	}
	expectStatus(t, login(), 401)
	restarted, err := service.New(ctx, db, service.WithCredentialStorage(store))
	if err != nil {
		t.Fatal(err)
	}
	if auth, challenge, err := restarted.BeginLogin(ctx, member.Email, password); err == nil || auth != nil || challenge != nil {
		t.Fatal("service restart bypassed durable factor lockout")
	}
	if err := db.First(&state, "user_id = ?", member.ID).Error; err != nil || state.FailedAttempts != 5 || state.LockedUntil == nil || !state.LockedUntil.After(time.Now()) {
		t.Fatal("invalid attempts were rolled back or challenge reset bypassed lockout")
	}
	if err := db.Model(&entity.UserMFA{}).Where("user_id = ?", member.ID).Update("locked_until", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	challenge = begin()
	// Exactly one concurrent recovery proof can complete a challenge.
	results := make(chan *service.Authentication, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			auth, err := svc.CompleteMFALogin(ctx, challenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[0]})
			results <- auth
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	successes := 0
	for err := range failures {
		if err == nil {
			successes++
		}
	}
	var recovered *service.Authentication
	for auth := range results {
		if auth != nil {
			recovered = auth
		}
	}
	if successes != 1 || recovered == nil {
		t.Fatal("concurrent recovery challenge was not single-use")
	}
	consumed := begin()
	expectStatus(t, verify(consumed.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[0]}), 401)
	// A separate challenge expires without consuming a valid recovery code.
	expiring := begin()
	if err := db.Model(&entity.MFAChallenge{}).Where("user_id = ? AND purpose = ?", member.ID, "login").Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expectStatus(t, verify(expiring.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[1]}), 401)
	// Root-key failure cannot silently downgrade TOTP to password authentication.
	wrongStore, err := secretstore.New(bytes.Repeat([]byte{12}, 32))
	if err != nil {
		t.Fatal(err)
	}
	wrongSvc, err := service.New(ctx, db, service.WithCredentialStorage(wrongStore))
	if err != nil {
		t.Fatal(err)
	}
	rootChallenge := begin()
	if auth, err := wrongSvc.CompleteMFALogin(ctx, rootChallenge.ChallengeToken, service.MFAProof{Code: mfaFixtureCode(t, enrollment.Secret, 1)}); err == nil || auth != nil {
		t.Fatal("wrong encryption root bypassed TOTP")
	} else {
		var public *apperrors.Error
		if !errors.As(err, &public) || public.Code != 503 {
			t.Fatal("encryption failure did not remain a sanitized availability error")
		}
	}
	// Recheck password and revoke pending challenges when changing credentials.
	passwordChallenge := begin()
	changed, err := svc.ChangePassword(ctx, member.ID, password, "test-only-mfa-new-password")
	if err != nil {
		t.Fatal(err)
	}
	password = "test-only-mfa-new-password"
	if auth, err := svc.Authenticate(ctx, changed.Token); err != nil || auth == nil {
		t.Fatal("password mutation did not issue a current session")
	}
	expectStatus(t, verify(passwordChallenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[1]}), 401)
	if auth, err := svc.Authenticate(ctx, recovered.Token); err == nil || auth != nil {
		t.Fatal("password rotation retained prior MFA session")
	}
	lifecycleChallenge := begin()
	disabled := true
	if _, err := svc.UpdateMember(ctx, admin.User.ID, member.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	disabled = false
	if _, err := svc.UpdateMember(ctx, admin.User.ID, member.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, verify(lifecycleChallenge.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[1]}), 401)
	// Recovery after reactivation still requires a fresh password challenge.
	fresh := begin()
	newSession, err := svc.CompleteMFALogin(ctx, fresh.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[1]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChangeMFA(ctx, newSession, "wrong-test-only-password", service.MFAProof{RecoveryCode: enabled.RecoveryCodes[2]}, false); err == nil {
		t.Fatal("recovery regeneration skipped password recheck")
	}
	regenerated, err := svc.ChangeMFA(ctx, newSession, password, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[2]}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(regenerated.RecoveryCodes) != 10 || regenerated.RecoveryCodes[0] == enabled.RecoveryCodes[0] {
		t.Fatal("recovery regeneration did not replace codes")
	}
	if auth, err := svc.Authenticate(ctx, newSession.Token); err == nil || auth != nil {
		t.Fatal("recovery regeneration retained old session")
	}
	oldRecovery := begin()
	expectStatus(t, verify(oldRecovery.ChallengeToken, service.MFAProof{RecoveryCode: enabled.RecoveryCodes[3]}), 401)
	// Offboarding and explicit reactivation must also invalidate issued challenges.
	offboardingChallenge := begin()
	if _, err := svc.EmergencyOffboarding(ctx, admin.User.ID, member.ID, service.OffboardingEmergencyInput{RequestID: "req_mfa_offboard", CurrentPassword: "test-only-mfa-password", Reason: "MFA lifecycle regression"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateMember(ctx, admin.User.ID, member.ID, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, verify(offboardingChallenge.ChallengeToken, service.MFAProof{RecoveryCode: regenerated.RecoveryCodes[0]}), 401)
	fresh = begin()
	active, err := svc.CompleteMFALogin(ctx, fresh.ChallengeToken, service.MFAProof{RecoveryCode: regenerated.RecoveryCodes[0]})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := svc.ChangeMFA(ctx, active, password, service.MFAProof{Code: "123456", RecoveryCode: regenerated.RecoveryCodes[1]}, true); err == nil || result != nil {
		t.Fatal("ambiguous proof accepted")
	}
	disabledResult, err := svc.ChangeMFA(ctx, active, password, service.MFAProof{RecoveryCode: regenerated.RecoveryCodes[1]}, true)
	if err != nil {
		t.Fatal(err)
	}
	final, err := svc.AccountMFA(ctx, member.ID)
	if err != nil || final.Enabled || final.RecoveryCodesRemaining != 0 {
		t.Fatal("disable retained active factor or recovery codes")
	}
	expectStatus(t, login(), 200)
	if auth, err := svc.Authenticate(ctx, active.Token); err == nil || auth != nil {
		t.Fatal("disable retained previous session")
	}
	if auth, err := svc.Authenticate(ctx, disabledResult.Authentication.Token); err != nil || auth == nil {
		t.Fatal("disable failed to rotate current session")
	}
	// Verification is a pre-session endpoint with the same Origin protections as login.
	request := httptest.NewRequest("POST", "http://routex.test/api/v1/auth/mfa/verify", strings.NewReader(`{"challenge_token":"invalid","code":"123456"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.invalid")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	expectStatus(t, response, http.StatusForbidden)
}
