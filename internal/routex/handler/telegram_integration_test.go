package handler

import (
	"context"
	"encoding/json"
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

func telegramAssertPrimary(t *testing.T, db *gorm.DB, cookie *http.Cookie, userID string) entity.Session {
	t.Helper()
	var row entity.Session
	var provider entity.NamedIdentityProvider
	var binding entity.NamedIdentityBinding
	if db.Where("token_hash = ?", secret.SHA256Hex(cookie.Value)).Take(&row).Error != nil || row.UserID != userID || row.PrimaryMethod != "telegram" || row.NamedIdentityProviderID != "telegram" || row.NamedIdentityProfileID != telegramFixtureProfile || row.NamedIdentityBindingID == "" || row.NamedIdentityBindingCreatedAt == nil || row.NamedIdentityUserCreatedAt == nil {
		t.Fatal("issued Session lacks exact Telegram primary provenance")
	}
	if db.Take(&provider, "id = ?", "telegram").Error != nil || db.Take(&binding, "id = ?", row.NamedIdentityBindingID).Error != nil || binding.ProviderID != "telegram" || binding.ProfileID != telegramFixtureProfile || binding.IdentityIssuer != telegramFixtureIssuer || binding.SubjectKind != "string" || !binding.CreatedAt.Equal(*row.NamedIdentityBindingCreatedAt) || !binding.UserCreatedAt.Equal(*row.NamedIdentityUserCreatedAt) || binding.UserID != userID || binding.ConfigRevision != row.NamedIdentityConfigRevision || row.NamedIdentityConfigRevision != provider.ConfigRevision || row.NamedIdentityPolicyRevision != provider.PolicyRevision {
		t.Fatal("issued Session differs from current exact profile/binding/birth")
	}
	if row.OIDCBindingID != "" || row.OAuthBindingID != "" || row.LDAPBindingID != "" || row.SAMLBindingID != "" {
		t.Fatal("named identity borrowed legacy primary proof")
	}
	return row
}
func telegramEnableMFA(t *testing.T, f *telegramApplicationFixture, session SessionResponse, cookie *http.Cookie) (SessionResponse, *http.Cookie, []string) {
	t.Helper()
	enrollment := telegramApplicationDecode[service.MFAEnrollment](t, f.request("POST", "/api/v1/account/mfa/enrollment", MFAPasswordRequest{CurrentPassword: telegramFixturePassword}, "", session.CSRFToken, cookie), 200)
	response := f.request("POST", "/api/v1/account/mfa/enable", MFAEnableRequest{CurrentPassword: telegramFixturePassword, EnrollmentToken: enrollment.EnrollmentToken, Code: mfaFixtureCode(t, enrollment.Secret, 0)}, "", session.CSRFToken, cookie)
	enabled := telegramApplicationDecode[MFARecoveryResponse](t, response, 200)
	if enabled.Session == nil || len(enabled.RecoveryCodes) != 10 {
		t.Fatal("native MFA enrollment incomplete")
	}
	return *enabled.Session, telegramApplicationCookie(t, response, sessionCookie), enabled.RecoveryCodes
}
func telegramChallenge(t *testing.T, f *telegramApplicationFixture, subject string) service.MFALoginChallenge {
	t.Helper()
	c := f.begin(t, "/api/v1/auth/telegram/start", subject, nil, "", "", "")
	r := f.finish(c, nil, "")
	v := telegramApplicationDecode[service.MFALoginChallenge](t, r, 202)
	if !v.MFARequired || v.ChallengeToken == "" {
		t.Fatal("Telegram primary did not require native MFA")
	}
	for _, cookie := range r.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			t.Fatal("native MFA challenge issued Session")
		}
	}
	return v
}
func testTelegramLifecycle(t *testing.T, db *gorm.DB) {
	f := newTelegramApplicationFixture(t, db)
	t.Run("fixed_profile_manual_completion_and_replay", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/telegram/start", telegramMemberSubject, nil, "", "", "")
		c.callback += "&scope=openid+profile&future_decoration=ignored"
		f.stage(t, c)
		var staged entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&staged).Error != nil || staged.ProviderID != "telegram" || staged.ProfileID != telegramFixtureProfile || staged.IdentityIssuer != telegramFixtureIssuer || staged.Status != "verified" || staged.SubjectKind != "string" || staged.Subject != telegramMemberSubject || staged.VerifiedAt == nil || staged.ConsumedAt != nil || staged.BindingID == "" || staged.BindingCreatedAt == nil {
			t.Fatal("callback did not stage exact immutable binding")
		}
		tokens, profiles, closed := f.peer.counts()
		if tokens < 3 || profiles != tokens || closed < 2*tokens {
			t.Fatal("each staged proof must read one JWKS and close its operation transports")
		}
		var before int64
		if db.Model(&entity.Session{}).Count(&before).Error != nil {
			t.Fatal("count Sessions")
		}
		session, cookie := telegramApplicationSession(t, f.finish(c, nil, ""))
		if session.User.ID != f.member.User.ID {
			t.Fatal("signed string identity changed member")
		}
		telegramAssertPrimary(t, db, cookie, session.User.ID)
		telegramApplicationStatus(t, f.finish(c, nil, ""), 401)
		var after int64
		if db.Model(&entity.Session{}).Count(&after).Error != nil || after != before+1 {
			t.Fatal("replay issued another Session")
		}
		f.stage(t, c)
		a, b, d := f.peer.counts()
		if a != tokens || b != profiles || d != closed {
			t.Fatal("callback replay performed remote I/O")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		for _, private := range []string{c.cookie.Value, staged.CookieHash, staged.StateHash} {
			if strings.Contains(session.CSRFToken, private) {
				t.Fatal("public Session exposed proof")
			}
		}
	})
	t.Run("state_and_duplicate_cookie_fail_before_exchange", func(t *testing.T) {
		c := f.start(t, "/api/v1/auth/telegram/start", telegramMemberSubject, nil, "", "", "")
		tokens, profiles, closed := f.peer.counts()
		for _, kind := range []string{"empty_issuer", "bare_issuer", "foreign_issuer", "duplicate_issuer", "escaped_duplicate_state", "duplicate_scope", "duplicate_decoration"} {
			callback, err := url.Parse(c.callback)
			if err != nil {
				t.Fatal("controlled callback URI")
			}
			q := callback.Query()
			switch kind {
			case "empty_issuer":
				q.Set("iss", "")
			case "bare_issuer":
				q.Set("iss", "oauth.telegram.org")
			case "foreign_issuer":
				q.Set("iss", "https://issuer.example.invalid")
			case "duplicate_issuer":
				q.Set("iss", telegramFixtureIssuer)
				q.Add("iss", telegramFixtureIssuer)
			case "duplicate_scope":
				q.Add("scope", "openid")
				q.Add("scope", "profile")
			case "duplicate_decoration":
				q.Add("future_decoration", "ignored")
				q.Add("future_decoration", "also_ignored")
			}
			callback.RawQuery = q.Encode()
			if kind == "escaped_duplicate_state" {
				callback.RawQuery += "&%73tate=" + q.Get("state")
			}
			telegramApplicationStatus(t, f.request("GET", callback.RequestURI(), nil, "", "", c.cookie), 303)
			a, b, d := f.peer.counts()
			if a != tokens || b != profiles || d != closed {
				t.Fatal("invalid callback authority reached network", kind)
			}
		}
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
		telegramApplicationStatus(t, f.request("GET", target.RequestURI(), nil, "", "", c.cookie), 303)
		telegramApplicationStatus(t, f.request("GET", c.callback, nil, "", "", c.cookie, c.cookie), 303)
		wrongCookie := *c.cookie
		replacement = "a"
		if wrongCookie.Value[0] == 'a' {
			replacement = "b"
		}
		wrongCookie.Value = replacement + wrongCookie.Value[1:]
		telegramApplicationStatus(t, f.finish(telegramBrowserCeremony{cookie: &wrongCookie}, nil, ""), 401)
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
		session, cookie := telegramApplicationSession(t, f.finish(c, nil, ""))
		telegramAssertPrimary(t, db, cookie, session.User.ID)
	})
	t.Run("signed_identity_validation_exact_issuer_no_alias", func(t *testing.T) {
		for _, bad := range []string{"unknown_subject", "wrong_nonce", "foreign_issuer", "issuer_alias", "duplicate_subject", "numeric_subject"} {
			// Successful same-authority proof before every negative prevents an unrelated
			// shared denial from satisfying the intended signed-claim rejection.
			admitted, cookie := f.login(t, telegramMemberSubject)
			telegramAssertPrimary(t, db, cookie, admitted.User.ID)
			f.peer.mu.Lock()
			switch bad {
			case "unknown_subject":
				f.peer.subject = "Other-Exact-Subject"
			case "wrong_nonce":
				f.peer.nonce = strings.Repeat("n", 43)
			case "foreign_issuer":
				f.peer.issuer = "https://issuer.example.invalid"
			case "issuer_alias":
				f.peer.issuer = "oauth.telegram.org"
			case "duplicate_subject":
				f.peer.duplicateSubject = true
			case "numeric_subject":
				f.peer.subject = json.Number("202")
			}
			f.peer.mu.Unlock()
			c := f.start(t, "/api/v1/auth/telegram/start", telegramMemberSubject, nil, "", "", "")
			tokens, keys, _ := f.peer.counts()
			f.stage(t, c)
			telegramApplicationStatus(t, f.finish(c, nil, ""), 401)
			gotTokens, gotKeys, _ := f.peer.counts()
			if gotTokens != tokens+1 || gotKeys < keys || gotKeys > keys+1 {
				t.Fatal("signed denial replayed or skipped the exact exchange", bad)
			}
			var proof entity.NamedIdentityCeremony
			if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil {
				t.Fatal("invalid signed identity staged proof", bad)
			}
			f.peer.mu.Lock()
			f.peer.subject = nil
			f.peer.nonce = ""
			f.peer.issuer = ""
			f.peer.duplicateSubject = false
			f.peer.mu.Unlock()
		}
		admitted, cookie := f.login(t, telegramMemberSubject)
		row := telegramAssertPrimary(t, db, cookie, admitted.User.ID)
		f.peer.mu.Lock()
		f.peer.issuer = ""
		f.peer.mu.Unlock()
		var binding entity.NamedIdentityBinding
		if db.Take(&binding, "id = ?", row.NamedIdentityBindingID).Error != nil || binding.IdentityIssuer != telegramFixtureIssuer || binding.Subject != telegramMemberSubject {
			t.Fatal("verified Telegram exact issuer changed binding namespace")
		}
		f.peer.mu.Lock()
		unexpected := f.peer.unexpected
		f.peer.mu.Unlock()
		if unexpected != 0 {
			t.Fatal("Telegram exchange fetched discovery, UserInfo or an unknown endpoint")
		}
	})
	t.Run("cross_profile_primary_proofs_never_mix", func(t *testing.T) {
		admitted, cookie := f.login(t, telegramMemberSubject)
		before := telegramAssertPrimary(t, db, cookie, admitted.User.ID)
		githubSession, githubCookie := f.github.login(t, githubMemberSubject)
		githubRow := githubAssertPrimary(t, db, githubCookie, githubSession.User.ID)
		if before.NamedIdentityBindingID == githubRow.NamedIdentityBindingID {
			t.Fatal("two profile subjects shared a binding")
		}
		for _, bad := range []map[string]any{{"primary_method": "github"}, {"named_identity_provider_id": "github"}, {"named_identity_profile_id": githubFixtureProfile}, {"oidc_binding_id": "oib_cross"}} {
			telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
			if db.Transaction(func(tx *gorm.DB) error {
				return tx.Model(&entity.Session{}).Where("id = ?", before.ID).Updates(bad).Error
			}) == nil {
				t.Fatal("cross-profile or old-method proof accepted")
			}
			var retained entity.Session
			if db.Session(&gorm.Session{QueryFields: true}).Take(&retained, "id = ?", before.ID).Error != nil || !reflect.DeepEqual(retained, before) {
				t.Fatal("rejected mixed proof changed original Session")
			}
			telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		}
	})
	t.Run("management_requires_intrinsic_admin_and_registration_write", func(t *testing.T) {
		telegramApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/telegram", nil, "", "", f.memberCookie), 403)
		view := f.config(t)
		tokens, keys, closed := f.peer.counts()
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
		telegramApplicationStatus(t, f.request("GET", "/api/v1/admin/auth/telegram", nil, "", "", f.adminCookie), 403)
		telegramApplicationStatus(t, f.request("POST", "/api/v1/admin/auth/telegram/verify", telegramIdentityInput(""), view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 403)
		a, b, d := f.peer.counts()
		if a != tokens || b != keys || d != closed {
			t.Fatal("denied verification performed remote I/O")
		}

		telegramApplicationStatus(t, f.request("GET", "/api/v1/account/identity/telegram", nil, "", "", f.memberCookie), 200)
	})
	t.Run("name_only_review_preserves_primary_and_staged_proof", func(t *testing.T) {
		session, cookie := f.login(t, telegramMemberSubject)
		before := telegramAssertPrimary(t, db, cookie, session.User.ID)
		c := f.begin(t, "/api/v1/auth/telegram/start", telegramMemberSubject, nil, "", "", "")
		view := f.config(t)
		saved := telegramApplicationDecode[service.TelegramProviderView](t, f.request("PUT", "/api/v1/admin/auth/telegram", service.TelegramProviderInput{Name: "Renamed configured identity", ClientID: view.ClientID, CallbackURL: view.CallbackURL, SecretAction: "keep", Reason: "Change display name only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if !saved.Enabled || !saved.Verified || saved.ReviewETag == view.ReviewETag {
			t.Fatal("name-only change altered security or omitted review revision")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 200)
		after := telegramAssertPrimary(t, db, cookie, session.User.ID)
		if !reflect.DeepEqual(before, after) {
			t.Fatal("name edit rewrote Session")
		}
		admitted, admittedCookie := telegramApplicationSession(t, f.finish(c, nil, ""))
		telegramAssertPrimary(t, db, admittedCookie, admitted.User.ID)
	})
	t.Run("held_verification_rechecks_original_session_before_staging", func(t *testing.T) {
		original, originalCookie := telegramApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: f.admin.User.Email, Password: telegramFixturePassword}, "", ""))
		view := telegramApplicationDecode[service.TelegramProviderView](t, f.request("GET", "/api/v1/admin/auth/telegram", nil, "", "", originalCookie), 200)
		c := f.start(t, "/api/v1/admin/auth/telegram/verify", telegramAdminSubject, originalCookie, original.CSRFToken, view.ReviewETag, "")
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
			done <- telegramApplicationRequest(operation, f.router, "GET", c.callback, nil, "", "", c.cookie)
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
		telegramApplicationStatus(t, f.request("DELETE", "/api/v1/account/sessions/"+row.ID, nil, "", f.admin.CSRFToken, f.adminCookie), 204)
		hold.release()
		select {
		case r := <-done:
			joined = true
			telegramApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held verification exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late verification completion")
		}
		telegramApplicationStatus(t, f.finish(c, originalCookie, original.CSRFToken), 401)
		var proof entity.NamedIdentityCeremony
		if db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error != nil || proof.Status == "verified" || proof.VerifiedAt != nil {
			t.Fatal("revoked originating Session staged a usable verification")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
	})
	t.Run("held_binding_rejects_security_reconfiguration_without_cross_profile_revocation", func(t *testing.T) {
		_, githubCookie := f.github.login(t, githubMemberSubject)
		var githubBefore entity.NamedIdentityProvider
		var githubBinding entity.NamedIdentityBinding
		if db.Take(&githubBefore, "id = ?", "github").Error != nil || db.Where("provider_id = ? AND user_id = ?", "github", f.member.User.ID).Take(&githubBinding).Error != nil {
			t.Fatal("capture independent GitHub configuration")
		}
		account := f.account(t, f.memberCookie)
		c := f.start(t, "/api/v1/account/identity/telegram/bind", telegramMemberSubject, f.memberCookie, f.member.CSRFToken, account.ReviewETag, "")
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
					t.Error("held binding did not join original operation")
				}
			}
			cancel()
		}()
		go func() {
			done <- telegramApplicationRequest(operation, f.router, "GET", c.callback, nil, "", "", c.cookie)
		}()
		select {
		case <-hold.entered:
		case <-done:
			joined = true
			t.Fatal("binding did not enter controlled token exchange")
		case <-operation.Done():
			t.Fatal("binding hold timed out")
		}
		before := f.config(t)
		changed := telegramApplicationDecode[service.TelegramProviderView](t, f.request("PUT", "/api/v1/admin/auth/telegram", map[string]any{"name": "Controlled Telegram", "client_id": "routex-telegram-test-client", "callback_url": "https://routex.test/api/v1/auth/telegram/callback", "secret_action": "replace", "client_secret": "test-only-telegram-client-secret", "reason": "Replace exact Telegram secret generation during held binding"}, before.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		if changed.Enabled || changed.Verified {
			t.Fatal("security replacement preserved enablement/verification")
		}
		hold.release()
		select {
		case r := <-done:
			joined = true
			telegramApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held binding exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late held binding completion")
		}
		telegramApplicationStatus(t, f.finish(c, f.memberCookie, f.member.CSRFToken), 401)
		var proof entity.NamedIdentityCeremony
		e := db.Where("cookie_hash = ?", secret.SHA256Hex(c.cookie.Value)).Take(&proof).Error
		if e != gorm.ErrRecordNotFound && (e != nil || proof.Status == "verified" || proof.VerifiedAt != nil) {
			t.Fatal("obsolete security generation staged usable proof")
		}
		var currentGitHub entity.NamedIdentityProvider
		var currentBinding entity.NamedIdentityBinding
		if db.Take(&currentGitHub, "id = ?", "github").Error != nil || db.Take(&currentBinding, "id = ?", githubBinding.ID).Error != nil || !reflect.DeepEqual(currentGitHub, githubBefore) || !reflect.DeepEqual(currentBinding, githubBinding) {
			t.Fatal("Telegram security change rewrote independent GitHub identity")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		// Re-establish Telegram only through genuine verification, explicit enable and self binding.
		verification := f.begin(t, "/api/v1/admin/auth/telegram/verify", telegramAdminSubject, f.adminCookie, f.admin.CSRFToken, changed.ReviewETag, "")
		telegramApplicationStatus(t, f.finish(verification, f.adminCookie, f.admin.CSRFToken), 200)
		current := f.config(t)
		telegramApplicationStatus(t, f.request("PUT", "/api/v1/admin/auth/telegram/status", map[string]any{"enabled": true, "reason": "Explicitly restore newly verified Telegram generation"}, current.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		account = f.account(t, f.memberCookie)
		bound := f.begin(t, "/api/v1/account/identity/telegram/bind", telegramMemberSubject, f.memberCookie, f.member.CSRFToken, account.ReviewETag, "")
		telegramApplicationStatus(t, f.finish(bound, f.memberCookie, f.member.CSRFToken), 200)
	})
	t.Run("planned_departure_revokes_native_mfa_and_held_callback", func(t *testing.T) {
		var admin entity.User
		if db.Take(&admin, "id = ?", f.admin.User.ID).Error != nil {
			t.Fatal("read password baseline")
		}
		member := entity.User{ID: "usr_telegram_departing_member", Email: "telegram-departing@example.invalid", Name: "Departing identity member", Role: entity.RoleMember, PasswordHash: admin.PasswordHash}
		if db.Create(&member).Error != nil || db.Take(&member, "id = ?", member.ID).Error != nil {
			t.Fatal("create existing departure member")
		}
		local, localCookie := telegramApplicationSession(t, f.request("POST", "/api/v1/auth/login", LoginRequest{Email: member.Email, Password: telegramFixturePassword}, "", ""))
		account := f.account(t, localCookie)
		binding := f.begin(t, "/api/v1/account/identity/telegram/bind", "Telegram-Departing-303", localCookie, local.CSRFToken, account.ReviewETag, "")
		bound := telegramApplicationDecode[struct {
			Kind string `json:"kind"`
		}](t, f.finish(binding, localCookie, local.CSRFToken), 200)
		if bound.Kind != "bound" {
			t.Fatal("departure requires genuine explicit binding")
		}
		local, localCookie, recovery := telegramEnableMFA(t, f, local, localCookie)
		factor := telegramChallenge(t, f, "Telegram-Departing-303")
		admitted, telegramCookie := telegramApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		telegramAssertPrimary(t, db, telegramCookie, admitted.User.ID)
		pending := telegramChallenge(t, f, "Telegram-Departing-303")
		staged := f.begin(t, "/api/v1/auth/telegram/start", "Telegram-Departing-303", nil, "", "", "")
		held := f.start(t, "/api/v1/auth/telegram/start", "Telegram-Departing-303", nil, "", "", "")
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
			done <- telegramApplicationRequest(operation, f.router, "GET", held.callback, nil, "", "", held.cookie)
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
		if db.Take(&providerBefore, "id = ?", "telegram").Error != nil || db.Order("id").Find(&bindingsBefore).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryBefore).Error != nil {
			t.Fatal("capture exact retained departure facts")
		}
		request := func(method, path string, body any, cookie *http.Cookie, csrf, etag string, extra *http.Cookie) *httptest.ResponseRecorder {
			return f.request(method, path, body, etag, csrf, cookie, extra)
		}
		assertRetained := exerciseEnterpriseOffboarding(t, f.ctx, db, f.service, "telegram", member.ID, f.admin, f.adminCookie, request)
		hold.release()
		select {
		case r := <-done:
			joined = true
			telegramApplicationStatus(t, r, 303)
		case <-operation.Done():
			t.Fatal("held callback exceeded original bound")
		}
		if operation.Err() != nil {
			t.Fatal("late callback cannot qualify positive closure")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", telegramCookie), 401)
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", localCookie), 401)
		telegramApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		for _, c := range []telegramBrowserCeremony{staged, held} {
			telegramApplicationStatus(t, f.finish(c, nil, ""), 401)
			telegramApplicationStatus(t, f.finish(c, nil, ""), 401)
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
		if db.Take(&providerAfter, "id = ?", "telegram").Error != nil || db.Order("id").Find(&bindingsAfter).Error != nil || db.Where("user_id = ?", member.ID).Order("code_hash").Find(&recoveryAfter).Error != nil || !reflect.DeepEqual(providerBefore, providerAfter) || !reflect.DeepEqual(bindingsBefore, bindingsAfter) || !reflect.DeepEqual(recoveryBefore, recoveryAfter) {
			t.Fatal("late proof changed retained identity/recovery facts")
		}
		assertRetained()
	})
	t.Run("native_mfa_and_disable_revalidate_exact_primary", func(t *testing.T) {
		var recovery []string
		f.member, f.memberCookie, recovery = telegramEnableMFA(t, f, f.member, f.memberCookie)
		githubFactor := githubChallenge(t, f.github, githubMemberSubject)
		githubSession, githubCookie := telegramApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: githubFactor.ChallengeToken, RecoveryCode: recovery[3]}, "", ""))
		githubAssertPrimary(t, db, githubCookie, githubSession.User.ID)
		factor := telegramChallenge(t, f, telegramMemberSubject)
		session, cookie := telegramApplicationSession(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: factor.ChallengeToken, RecoveryCode: recovery[0]}, "", ""))
		telegramAssertPrimary(t, db, cookie, session.User.ID)
		pending := telegramChallenge(t, f, telegramMemberSubject)
		staged := f.begin(t, "/api/v1/auth/telegram/start", telegramMemberSubject, nil, "", "", "")
		view := f.config(t)
		telegramApplicationDecode[service.TelegramProviderView](t, f.request("PUT", "/api/v1/admin/auth/telegram/status", service.TelegramStatusInput{Enabled: false, Reason: "Revoke this named identity only"}, view.ReviewETag, f.admin.CSRFToken, f.adminCookie), 200)
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", cookie), 401)
		telegramApplicationStatus(t, f.request("POST", "/api/v1/auth/mfa/verify", MFALoginRequest{ChallengeToken: pending.ChallengeToken, RecoveryCode: recovery[1]}, "", ""), 401)
		telegramApplicationStatus(t, f.finish(staged, nil, ""), 401)
		var count int64
		if db.Model(&entity.MFARecoveryCode{}).Where("user_id = ? AND used_at IS NULL", f.member.User.ID).Count(&count).Error != nil || count != 8 {
			t.Fatal("rejected primary consumed recovery proof")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.adminCookie), 200)
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", githubCookie), 200)
		account := f.account(t, f.memberCookie)
		if !account.Bound || account.Available || !account.MFARequired {
			t.Fatal("disabled provider fabricated binding/MFA facts")
		}
		unlinked := telegramApplicationDecode[service.TelegramAccountView](t, f.request("POST", "/api/v1/account/identity/telegram/unlink", telegramIdentityInput(recovery[2]), account.ReviewETag, f.member.CSRFToken, f.memberCookie), 200)
		if unlinked.Bound {
			t.Fatal("explicit unlink retained binding")
		}
		telegramApplicationStatus(t, f.request("GET", "/api/v1/auth/session", nil, "", "", f.memberCookie), 200)
	})
}
