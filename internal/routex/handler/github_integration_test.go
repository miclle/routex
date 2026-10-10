package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

func githubAssertPrimary(t *testing.T, db *gorm.DB, cookie *http.Cookie, userID string) entity.Session {
	t.Helper()
	var row entity.Session
	var provider entity.NamedIdentityProvider
	var binding entity.NamedIdentityBinding
	if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil || row.UserID != userID || row.PrimaryMethod != "github" || row.NamedIdentityProviderID != "github" || row.NamedIdentityProfileID != githubFixtureProfile || row.NamedIdentityBindingID == "" || row.NamedIdentityBindingCreatedAt == nil || row.NamedIdentityUserCreatedAt == nil {
		t.Fatal("issued Session lacks exact GitHub primary provenance")
	}
	if db.Take(&provider, "id = ?", "github").Error != nil || db.Take(&binding, "id = ?", row.NamedIdentityBindingID).Error != nil || binding.ProviderID != "github" || binding.ProfileID != githubFixtureProfile || binding.IdentityIssuer != "https://github.com" || binding.SubjectKind != "integer" || !binding.CreatedAt.Equal(*row.NamedIdentityBindingCreatedAt) || !binding.UserCreatedAt.Equal(*row.NamedIdentityUserCreatedAt) || binding.UserID != userID || binding.ConfigRevision != row.NamedIdentityConfigRevision || row.NamedIdentityConfigRevision != provider.ConfigRevision || row.NamedIdentityPolicyRevision != provider.PolicyRevision {
		t.Fatal("issued Session differs from current exact profile/binding/birth")
	}
	if row.OIDCBindingID != "" || row.OAuthBindingID != "" || row.LDAPBindingID != "" || row.SAMLBindingID != "" {
		t.Fatal("named identity borrowed legacy primary proof")
	}
	return row
}
func githubEnableMFA(t *testing.T, f *githubApplicationFixture, session SessionResponse, cookie *http.Cookie) (SessionResponse, *http.Cookie, []string) {
	t.Helper()
	enrollment := githubApplicationDecode[service.MFAEnrollment](t, f.request("POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: githubFixturePassword}, "", session.CSRFToken, cookie), 200)
	response := f.request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: githubFixturePassword, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, "", session.CSRFToken, cookie)
	enabled := githubApplicationDecode[MFARecoveryResponse](t, response, 200)
	if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 {
		t.Fatal("native MFA enrollment incomplete")
	}
	return *enabled.Session, githubApplicationCookie(t, response, sessionCookie), enabled.RecoveryCodes
}
func githubChallenge(t *testing.T, f *githubApplicationFixture, subject string) service.MFALoginChallenge {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/github/start", subject, nil, "", "", "")
	r := f.finish(c, nil, "")
	v := githubApplicationDecode[service.MFALoginChallenge](t, r, 202)
	if !v.MFARequired || v.ChallengeToken == "" {
		t.Fatal("GitHub primary did not require native MFA")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			t.Fatal("native MFA challenge issued Session")
		}
	}
	return v
}
func testGitHubLifecycle(t *testing.T, db *gorm.DB) {
	f := newGitHubApplicationFixture(t, db)
	t.Run("fixed_profile_manual_completion_and_replay", func(t *testing.T) {
		c := f.begin(t, "/api/v1/auth/github/start", githubMemberSubject, nil, "", "", "")
		var staged entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&staged).Error != nil || staged.ProviderID != "github" || staged.ProfileID != githubFixtureProfile || staged.IdentityIssuer != "https://github.com" || staged.Status != "verified" || staged.SubjectKind != "integer" || staged.Subject != githubMemberSubject || staged.VerifiedAt == nil || staged.ConsumedAt != nil || staged.BindingID == "" || staged.BindingCreatedAt == nil {
			t.Fatal("callback did not stage exact immutable binding")
		}
		tokens, profiles, closed := f.peer.counts()
		if tokens < 3 || profiles != tokens || closed != 2*tokens {
			t.Fatal("each staged proof must close the start and token/profile transports")
		}
		var before int64
		if db.Model(&entity.Session{}).Count(&before).Error != nil {
			t.Fatal("count Sessions")
		}
		session, cookie := githubApplicationSession(t, f.finish(c, nil, ""))
		if session.User.ID != f.member.User.ID {
			t.Fatal("integer identity changed member")
		}
		githubAssertPrimary(t, db, cookie, session.User.ID)
		githubApplicationStatus(t, f.finish(c, nil, ""), 401)
		var after int64
		if db.Model(&entity.Session{}).Count(&after).Error != nil || after != before+1 {
			t.Fatal("replay issued another Session")
		}
		f.stage(t, c)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closed {
			t.Fatal("callback replay performed remote I/O")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		for _, private := range []string{c.cookie.Value, staged.CookieHash, staged.StateHash} {
			if strings.Contains(session.CSRFToken, private) {
				t.Fatal("public Session exposed proof")
			}
		}
	})
	t.Run("state_and_duplicate_cookie_fail_before_exchange", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/github/start", githubMemberSubject, nil, "", "", "")
		tokens, profiles, closed := f.peer.counts()
		target, err := url.Parse(c.callback)
		if err != nil {
			t.Fatal("controlled callback URI")
		}
		q := target.Query()
		originalState := q.Get("state")
		replacement := "a"
		if originalState[0] == 'a' {
			replacement = "b"
		}
		q.Set("state", replacement+originalState[1:])
		target.RawQuery = q.Encode()
		githubApplicationStatus(t, f.request("GET", target.RequestURI(), nil, "", "", c.cookie), 303)
		githubApplicationStatus(t, f.request("GET", c.callback, nil, "", "", c.cookie, c.cookie), 303)
		wrongCookie := *c.cookie
		replacement = "a"
		if wrongCookie.Value[0] == 'a' {
			replacement = "b"
		}
		wrongCookie.Value = replacement + wrongCookie.Value[1:]
		githubApplicationStatus(t, f.finish(githubBrowserCeremony{cookie: &wrongCookie}, nil, ""), 401)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closed {
			t.Fatal("invalid state/cookie reached token or profile endpoint")
		}
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status != "pending" || proof.VerifiedAt != nil {
			t.Fatal("invalid state/cookie staged proof")
		}
		// The exact original request remains usable; failed state cannot consume it.
		f.stage(t, c)
		session, cookie := githubApplicationSession(t, f.finish(c, nil, ""))
		githubAssertPrimary(t, db, cookie, session.User.ID)
	})
	t.Run("unknown_or_string_identity_never_matches_integer_binding", func(t *testing.T) {
		c := f.begin(t, "/api/v1/auth/github/start", "999", nil, "", "", "")
		githubApplicationStatus(t, f.finish(c, nil, ""), 401)
		f.peer.mu.Lock()
		f.peer.profileID = githubMemberSubject
		f.peer.mu.Unlock()
		defer func() { f.peer.mu.Lock(); f.peer.profileID = nil; f.peer.mu.Unlock() }()
		c = f.start(t, "/api/v1/auth/github/start", githubMemberSubject, nil, "", "", "")
		f.stage(t, c)
		githubApplicationStatus(t, f.finish(c, nil, ""), 401)
	})
	t.Run("management_requires_intrinsic_admin_and_registration_write", func(t *testing.T) {
		githubApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/github", nil, "", "", f.memberCookie), 403)
		var permission entity.RolePermission
		if db.Where("role_id = ? AND permission = ?", "rol_admin", "registration.write").Take(&permission).Error != nil {
			t.Fatal("capture independent admin permission")
		}
		removed := db.Where("role_id = ? AND permission = ?", permission.RoleID, permission.Permission).Delete(&entity.RolePermission{})
		if removed.Error != nil || removed.RowsAffected != 1 {
			t.Fatal("remove exact test permission")
		}
		defer func() {
			if db.Create(&permission).Error != nil {
				t.Error("restore exact test permission")
			}
		}()
		githubApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/github", nil, "", "", f.adminCookie), 403)
		githubApplicationStatus(t, f.request("GET", "/api/v1/account/identity/github", nil, "", "", f.memberCookie), 200)
	})
	t.Run("name_only_review_preserves_primary_and_staged_proof", func(t *testing.T) {
		session, cookie := f.login(t, githubMemberSubject)
		before := githubAssertPrimary(t, db, cookie, session.User.ID)
		c := f.begin(t, "/api/v1/auth/github/start", githubMemberSubject, nil, "", "", "")
		view := f.config(t)
		saved := githubApplicationDecode[service.GitHubProviderView](t, f.request("PUT", "/api/v1/admin/auth/github", service.GitHubProviderInput{Name: "Renamed configured identity", ClientID: view.ClientID, CallbackURL: view.CallbackURL, SecretAction: "keep", Reason: "Change display name only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if !saved.Enabled || !saved.Verified || saved.ReviewETag == view.ReviewETag {
			t.Fatal("name-only change altered security or omitted review revision")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		after := githubAssertPrimary(t, db, cookie, session.User.ID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("name edit rewrote Session")
		}
		admitted, admittedCookie := githubApplicationSession(t, f.finish(c, nil, ""))
		githubAssertPrimary(t, db, admittedCookie, admitted.User.ID)
	})
	t.Run("held_verification_rechecks_original_session_before_staging", func(t *testing.T) {
		original, originalCookie := githubApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: githubFixturePassword}, "", ""))
		view := githubApplicationDecode[service.GitHubProviderView](t, f.request("GET", "/api/v1/admin/auth/github", nil, "", "", originalCookie), 200)
		c := f.start(t, "/api/v1/admin/auth/github/verify", githubAdminSubject, originalCookie, original.CSRFToken, view.ReviewETag, "")
		hold := f.peer.holdToken(t)
		operation, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			if !joined {
				select {
				case <-done:
				case <-operation.Done():
					t.Error("held verification did not join within original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- githubApplicationRequest(operation, f.router, "GET", c.callback, nil, "", "", c.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("verification failed before real held token request")
		case <-operation.Done():
			t.Fatal("verification hold timed out")
		}
		var row entity.Session
		if db.Where("token_hash = ?", secret.SHA256Hex(originalCookie.Value)).Take(&row).Error != nil {
			t.Fatal("read exact originating Session")
		}
		githubApplicationStatus(t, f.request("DELETE", "/api/v1/account/sessions/"+row.ID, nil, "", f.admin.CSRFToken, f.adminCookie), 204)
		hold.release()
		select {
		case r := <-done:
			joined = true
			githubApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held verification exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late verification completion")
		}
		githubApplicationStatus(t, f.finish(c, originalCookie, original.CSRFToken), 401)
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil {
			t.Fatal("revoked originating Session staged a usable verification")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
	})
	t.Run("planned_departure_revokes_native_mfa_and_held_callback", func(t *testing.T) {
		var admin entity.User
		if db.Take(&admin, "id = ?", f.admin.User.ID).Error != nil {
			t.Fatal("read password baseline")
		}
		member := entity.User{ID: "usr_github_departing_member", Email: "github-departing@example.invalid", Name: "Departing identity member", Role: entity.RoleMember, PasswordHash: admin.PasswordHash}
		if db.Create(&member).Error != nil || db.Take(&member, "id = ?", member.ID).Error != nil {
			t.Fatal("create existing departure member")
		}
		local, localCookie := githubApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: githubFixturePassword}, "", ""))
		account := f.account(t, localCookie)
		binding := f.begin(t, "/api/v1/account/identity/github/bind", "303", localCookie, local.CSRFToken, account.ReviewETag, "")
		bound := githubApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, f.finish(binding, localCookie, local.CSRFToken), 200)
		if bound.Kind != "bound" {
			t.Fatal("departure requires genuine explicit binding")
		}
		local, localCookie, recovery := githubEnableMFA(t, f, local, localCookie)
		factor := githubChallenge(t, f, "303")
		admitted, githubCookie := githubApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		githubAssertPrimary(t, db, githubCookie, admitted.User.ID)
		pending := githubChallenge(t, f, "303")
		staged := f.begin(t, "/api/v1/auth/github/start", "303", nil, "", "", "")
		held := f.start(t, "/api/v1/auth/github/start", "303", nil, "", "", "")
		hold := f.peer.holdToken(t)
		operation, cancel := context.WithTimeout(f.ctx, 10*time.Second)
		defer cancel()
		done := make(chan *httptest.ResponseRecorder, 1)
		joined := false
		defer func() {
			hold.release()
			if !joined {
				select {
				case <-done:
				case <-operation.Done():
					t.Error("held callback did not join within original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- githubApplicationRequest(operation, f.router, "GET", held.callback, nil, "", "", held.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("callback did not reach held real token exchange")
		case <-operation.Done():
			t.Fatal("held callback admission timed out")
		}
		var exchanging entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(held.cookie.Value)).Take(&exchanging).Error != nil || exchanging.Status != "exchanging" || exchanging.VerifiedAt != nil {
			t.Fatal("remote I/O did not follow durable exclusive claim")
		}
		var providerBefore entity.NamedIdentityProvider
		var bindingsBefore []entity.NamedIdentityBinding
		var recoveryBefore []entity.MFARecoveryCode
		if db.Take(&providerBefore, "id = ?", "github").Error != nil || db.Order("id").Find(&bindingsBefore).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryBefore).Error != nil {
			t.Fatal("capture exact retained departure facts")
		}
		request := func(method, path string, body any, cookie *http.Cookie, csrf, etag string, extra *http.Cookie) *httptest.ResponseRecorder {
			return f.request(method, path, body, etag, csrf, cookie, extra)
		}
		assertRetained := exerciseEnterpriseOffboarding(t, f.ctx, db, f.service, "github", member.ID, f.admin, f.adminCookie, request)
		hold.release()
		select {
		case r := <-done:
			joined = true
			githubApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held callback exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late callback cannot qualify positive closure")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 401)
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", localCookie), 401)
		githubApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		for _, c := range []githubBrowserCeremony{staged, held} {
			githubApplicationStatus(t, f.finish(c, nil, ""), 401)
			githubApplicationStatus(t, f.finish(c, nil, ""), 401)
		}
		tokens, profiles, closes := f.peer.counts()
		f.stage(t, held)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closes {
			t.Fatal("departed callback replay performed I/O")
		}
		var providerAfter entity.NamedIdentityProvider
		var bindingsAfter []entity.NamedIdentityBinding
		var recoveryAfter []entity.MFARecoveryCode
		if db.Take(&providerAfter, "id = ?", "github").Error != nil || db.Order("id").Find(&bindingsAfter).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryAfter).Error != nil || !reflect.DeepEqual(providerBefore, providerAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) || !reflect.DeepEqual(recoveryBefore, recoveryAfter) {
			t.Fatal("late proof changed retained identity/recovery facts")
		}
		assertRetained()
	})
	t.Run("native_mfa_and_disable_revalidate_exact_primary", func(t *testing.T) {
		var recovery []string
		f.member, f.memberCookie, recovery = githubEnableMFA(t, f, f.member, f.memberCookie)
		factor := githubChallenge(t, f, githubMemberSubject)
		session, cookie := githubApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		githubAssertPrimary(t, db, cookie, session.User.ID)
		pending := githubChallenge(t, f, githubMemberSubject)
		staged := f.begin(t, "/api/v1/auth/github/start", githubMemberSubject, nil, "", "", "")
		view := f.config(t)
		githubApplicationDecode[service.GitHubProviderView](t, f.request("PUT", "/api/v1/admin/auth/github/status", service.GitHubStatusInput{Enabled: false, Reason: "Revoke this named identity only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 401)
		githubApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		githubApplicationStatus(t, f.finish(staged, nil, ""), 401)
		var count int64
		if db.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NULL", f.member.User.ID).Count(&count).Error != nil || count != 9 {
			t.Fatal("rejected primary consumed recovery proof")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
		account := f.account(t, f.memberCookie)
		if !account.Bound || account.Available || !account.MFARequired {
			t.Fatal("disabled provider fabricated binding/MFA facts")
		}
		unlinked := githubApplicationDecode[service.GitHubAccountView](t, f.request("POST", "/api/v1/account/identity/github/unlink", githubIdentityInput(recovery[2]), account.ReviewETag, f.member.CSRFToken, f.memberCookie), 200)
		if unlinked.Bound {
			t.Fatal("explicit unlink retained binding")
		}
		githubApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
	})
}
