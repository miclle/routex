package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func oauthValidInput() OAuthProviderInput {
	return OAuthProviderInput{Name: "Corporate identity", AuthorizationURL: "https://identity.example/authorize", TokenURL: "https://identity.example/token", UserInfoURL: "https://identity.example/profile", ClientAuthMethod: "client_secret_basic", Scopes: []string{"profile"}, SubjectPath: []string{"account", "id"}, ClientID: "routex", CallbackURL: "https://routex.example/api/v1/auth/oauth/callback", SecretAction: "replace", ClientSecret: "transient-secret", Reason: "Configure corporate sign-in"}
}
func TestOAuthStrictConfigurationWire(t *testing.T) {
	good := oauthValidInput()
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var parsed OAuthProviderInput
	if e = json.Unmarshal(raw, &parsed); e != nil || !reflect.DeepEqual(parsed, good) {
		t.Fatal("valid configuration", e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*OAuthProviderInput)
	}{
		{"http_issuer", func(v *OAuthProviderInput) { v.AuthorizationURL = "http://identity.example" }},
		{"issuer_query", func(v *OAuthProviderInput) { v.AuthorizationURL += "?tenant=other" }},
		{"issuer_fragment", func(v *OAuthProviderInput) { v.AuthorizationURL += "#other" }},
		{"issuer_userinfo", func(v *OAuthProviderInput) { v.AuthorizationURL = "https://user:password@identity.example" }},
		{"callback_wrong_path", func(v *OAuthProviderInput) { v.CallbackURL = "https://routex.example/login" }},
		{"callback_encoded_path", func(v *OAuthProviderInput) { v.CallbackURL = "https://routex.example/api/v1/auth/oauth/%63allback" }},
		{"client_control", func(v *OAuthProviderInput) { v.ClientID = "routex\nclient" }},
		{"client_overflow", func(v *OAuthProviderInput) { v.ClientID = strings.Repeat("a", 257) }},
		{"name_overflow", func(v *OAuthProviderInput) { v.Name = strings.Repeat("界", 101) }},
		{"keep_with_secret", func(v *OAuthProviderInput) { v.SecretAction = "keep" }},
		{"replace_empty", func(v *OAuthProviderInput) { v.ClientSecret = "" }},
		{"secret_overflow", func(v *OAuthProviderInput) { v.ClientSecret = strings.Repeat("a", 4097) }},
		{"empty_reason", func(v *OAuthProviderInput) { v.Reason = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := good
			tc.mutate(&v)
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			if e = json.Unmarshal(b, &parsed); !errors.Is(e, apperrors.ErrBadRequest) {
				t.Fatal("unsafe configuration admitted", e)
			}
		})
	}
	for _, bad := range []string{strings.Replace(string(raw), `"name":"Corporate identity"`, `"name":null`, 1), strings.TrimSuffix(string(raw), "}") + `,"extra":true}`, strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`} {
		if e = json.Unmarshal([]byte(bad), &parsed); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("unknown/null/duplicate field", e)
		}
	}
	good.ClientID = strings.Repeat("a", 256)
	if e = oauthValidateConfigInput(&good); e != nil {
		t.Fatal("client ID boundary", e)
	}
}
func TestOAuthLocalProofWireIsExplicit(t *testing.T) {
	for _, valid := range []string{`{"password":"a-valid-password","proof":{},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"recovery_code":"recovery"},"reason":"Link account"}`} {
		var v OAuthIdentityInput
		if e := json.Unmarshal([]byte(valid), &v); e != nil {
			t.Fatal(e)
		}
	}
	for _, invalid := range []string{`{"password":"a-valid-password","reason":"Link account"}`, `{"password":"a-valid-password","proof":null,"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":""},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","recovery_code":"recovery"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":null},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"unknown":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","code":"654321"},"reason":"Link account"}`} {
		var v OAuthIdentityInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid local proof admitted", e)
		}
	}
	for _, invalid := range []string{`{"enabled":null,"reason":"Enable"}`, `{"enabled":true,"reason":"Enable","extra":true}`, `{"enabled":true,"reason":""}`} {
		var v OAuthStatusInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("status wire", e)
		}
	}
}
func TestOAuthCookieDerivationAndCeremonyLifetime(t *testing.T) {
	cookie := strings.Repeat("a", 43)
	a := oauthDerived(cookie, "oac_first", "pkce")
	if !oauthBrowserValue(a) || a == oauthDerived(cookie, "oac_first", "nonce") || a == oauthDerived(cookie, "oac_second", "pkce") || a == oauthDerived(strings.Repeat("b", 43), "oac_first", "pkce") {
		t.Fatal("browser derivation lacks separation")
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	birth := now.Add(-time.Minute)
	p := entity.OAuthProvider{ID: "oauth", ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("a", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "oab_admin", VerifiedBindingCreatedAt: &birth}
	c := entity.OAuthCeremony{ID: "oac_first", ProviderID: "oauth", Purpose: "login", CreatedAt: birth, ExpiresAt: birth.Add(oauthCeremonyLifetime), ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if !oauthCeremonyCurrent(p, c, now) {
		t.Fatal("valid browser ceremony")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*entity.OAuthProvider, *entity.OAuthCeremony)
	}{
		{"disabled", func(p *entity.OAuthProvider, _ *entity.OAuthCeremony) { p.Enabled = false }},
		{"unverified", func(p *entity.OAuthProvider, _ *entity.OAuthCeremony) { p.VerifiedConfigRevision = "" }},
		{"policy_changed", func(p *entity.OAuthProvider, _ *entity.OAuthCeremony) { p.PolicyRevision = strings.Repeat("c", 64) }},
		{"config_changed", func(p *entity.OAuthProvider, _ *entity.OAuthCeremony) { p.ConfigRevision = strings.Repeat("c", 64) }},
		{"expired", func(_ *entity.OAuthProvider, c *entity.OAuthCeremony) { c.ExpiresAt = now }},
		{"renewed_lifetime", func(_ *entity.OAuthProvider, c *entity.OAuthCeremony) {
			c.ExpiresAt = c.CreatedAt.Add(oauthCeremonyLifetime + time.Microsecond)
		}},
		{"future_birth", func(_ *entity.OAuthProvider, c *entity.OAuthCeremony) { c.CreatedAt = now.Add(time.Second) }},
		{"unknown_purpose", func(_ *entity.OAuthProvider, c *entity.OAuthCeremony) { c.Purpose = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, d := p, c
			tc.mutate(&q, &d)
			if oauthCeremonyCurrent(q, d, now) {
				t.Fatal("obsolete ceremony accepted")
			}
		})
	}
	p.Enabled = false
	c.Purpose = "verify"
	if !oauthCeremonyCurrent(p, c, now) {
		t.Fatal("explicit admin verification while disabled")
	}
}
func TestOAuthExternalIdentityAndReviewNeverAlias(t *testing.T) {
	if oauthSubjectDigest("oauth", "string", "1") == oauthSubjectDigest("oauth", "integer", "1") || oauthSubjectDigest("oauth", "string", "Alice") == oauthSubjectDigest("oauth", "string", "alice") || oauthSubjectDigest("oauth", "string", "x") == oauthSubjectDigest("other", "string", "x") {
		t.Fatal("typed provider subject aliases")
	}
	for _, tc := range []struct {
		kind, value string
		valid       bool
	}{{"string", "1", true}, {"integer", "1", true}, {"integer", "9007199254740993123456789", true}, {"integer", "0", true}, {"integer", "01", false}, {"integer", "1.0", false}, {"integer", "1e0", false}, {"integer", "-1", false}, {"number", "1", false}, {"string", "", false}, {"string", "\u0000", false}} {
		if oauthSubject(tc.kind, tc.value) != tc.valid {
			t.Fatal("typed subject admission", tc.kind)
		}
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	p := entity.OAuthProvider{ID: "oauth", CreatedAt: now, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64)}
	u := entity.User{ID: "usr_actor", CreatedAt: now}
	m := entity.UserMFA{}
	before := oauthReview(p, u, m)
	p.ReviewRevision = strings.Repeat("d", 64)
	if before == oauthReview(p, u, m) {
		t.Fatal("review ABA")
	}
	p.ReviewRevision = strings.Repeat("a", 64)
	u.CreatedAt = now.Add(time.Microsecond)
	if before == oauthReview(p, u, m) {
		t.Fatal("actor rebirth")
	}
}
func TestOAuthPrimaryMarkersNeverDowngrade(t *testing.T) {
	if e := primaryValidateSession(nil, entity.Session{}); e != nil {
		t.Fatal("legacy local session", e)
	}
	now := time.Now()
	for _, s := range []entity.Session{{PrimaryMethod: "local"}, {OAuthBindingID: "oab_retained"}, {OAuthBindingCreatedAt: &now}, {OAuthConfigRevision: strings.Repeat("a", 64)}, {OAuthPolicyRevision: strings.Repeat("b", 64)}, {OAuthUserCreatedAt: &now}, {PrimaryMethod: "oauth"}, {PrimaryMethod: "OAuth"}} {
		if e := primaryValidateSession(nil, s); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("incomplete external provenance became local", e)
		}
	}
	if e := primaryValidateChallenge(nil, entity.MFAChallenge{}); e != nil {
		t.Fatal("legacy local challenge", e)
	}
	for _, c := range []entity.MFAChallenge{{PrimaryMethod: "local"}, {PrimaryMethod: "oauth"}, {OAuthBindingID: "oab_retained"}, {OAuthBindingCreatedAt: &now}, {OAuthConfigRevision: strings.Repeat("a", 64)}, {OAuthPolicyRevision: strings.Repeat("b", 64)}, {OAuthUserCreatedAt: &now}} {
		if e := primaryValidateChallenge(nil, c); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("partial challenge provenance became local", e)
		}
	}
	if oauthError(errors.New("secret upstream body")) != oauthUnavailable || oauthError(apperrors.ErrForbidden) != apperrors.ErrForbidden {
		t.Fatal("unsafe error exposure")
	}
}

func TestOAuthAuditFactsRetainReasonWithoutRemoteMaterial(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 123456000, time.UTC)
	u := entity.User{ID: "usr_actor", CreatedAt: birth}
	p := entity.OAuthProvider{ID: "oauth", CreatedAt: birth, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), AuthorizationURL: "https://private-issuer.example", ClientID: "private-client", AuthCiphertext: "encrypted-private-secret"}
	b := entity.OAuthBinding{ID: "oab_actor", ProviderID: "oauth", SubjectKind: "string", CreatedAt: birth, UserID: u.ID, UserCreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: "private-subject", SubjectDigest: "private-digest"}
	raw, e := oauthAuditJSON(u, p, &b, "Review corporate access")
	if e != nil {
		t.Fatal(e)
	}
	var facts map[string]json.RawMessage
	if e = json.Unmarshal([]byte(raw), &facts); e != nil {
		t.Fatal(e)
	}
	if len(facts) != 10 || string(facts["reason"]) != `"Review corporate access"` || string(facts["binding_id"]) != `"oab_actor"` {
		t.Fatal("typed audit facts/reason", raw)
	}
	for _, private := range []string{p.AuthorizationURL, p.ClientID, p.AuthCiphertext, b.Subject, b.SubjectDigest} {
		if strings.Contains(raw, private) {
			t.Fatal("remote identity or secret retained in audit")
		}
	}
	for _, reason := range []string{"", "  Review  ", strings.Repeat("a", 1025)} {
		if _, e = oauthAuditJSON(u, p, &b, reason); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid audit reason admitted")
		}
	}
	reborn := b
	reborn.UserCreatedAt = birth.Add(time.Microsecond)
	if _, e = oauthAuditJSON(u, p, &reborn, "Review"); !errors.Is(e, apperrors.ErrBadRequest) {
		t.Fatal("aliased actor birth accepted")
	}
}

func TestOAuthConfigArraysAreExactAndBounded(t *testing.T) {
	for _, mutate := range []func(*OAuthProviderInput){
		func(v *OAuthProviderInput) { v.Scopes = nil },
		func(v *OAuthProviderInput) { v.SubjectPath = nil },
		func(v *OAuthProviderInput) { v.SubjectPath = []string{} },
		func(v *OAuthProviderInput) { v.Scopes = []string{"profile", "profile"} },
		func(v *OAuthProviderInput) { v.Scopes = []string{"invalid scope"} },
		func(v *OAuthProviderInput) { v.SubjectPath = []string{""} },
		func(v *OAuthProviderInput) { v.SubjectPath = []string{strings.Repeat("x", 129)} },
		func(v *OAuthProviderInput) { v.SubjectPath = make([]string, 17) },
		func(v *OAuthProviderInput) { v.ClientAuthMethod = "automatic" },
		func(v *OAuthProviderInput) { v.TokenURL = "http://identity.example/token" },
		func(v *OAuthProviderInput) { v.UserInfoURL += "?access_token=secret" },
		func(v *OAuthProviderInput) { v.AuthorizationURL = "https://identity.example\\escape" },
		func(v *OAuthProviderInput) { v.ClientSecret = "secret\n" },
	} {
		v := oauthValidInput()
		mutate(&v)
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		var out OAuthProviderInput
		if json.Unmarshal(raw, &out) == nil {
			t.Fatal("unsafe configuration admitted")
		}
	}
	v := oauthValidInput()
	v.Scopes = []string{}
	v.SubjectPath = []string{"account.id", "0"}
	v.ClientID = "client:+ /é"
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out OAuthProviderInput
	if json.Unmarshal(raw, &out) != nil || !reflect.DeepEqual(v, out) {
		t.Fatal("exact arrays or client identity normalized")
	}
	for _, bad := range []string{
		strings.Replace(string(raw), `"subject_path":["account.id","0"]`, `"subject_path":["\ud800"]`, 1),
		strings.Replace(string(raw), `"subject_path":["account.id","0"]`, `"subject_path":[null]`, 1),
		strings.TrimSuffix(string(raw), "}") + `,"scopes":[]}`,
	} {
		if json.Unmarshal([]byte(bad), &out) == nil {
			t.Fatal("ambiguous path/array admitted")
		}
	}
}
func TestOAuthMixedPrimaryNeverDowngradesLocalOrOIDC(t *testing.T) {
	now := time.Now()
	for _, r := range []entity.Session{
		{OIDCBindingID: "oib_old"}, {OAuthBindingID: "oab_old"},
		{PrimaryMethod: "oidc", OAuthBindingID: "oab_old"},
		{PrimaryMethod: "oauth", OIDCBindingID: "oib_old"},
		{PrimaryMethod: "", OAuthUserCreatedAt: &now},
		{PrimaryMethod: "unknown"},
	} {
		if primaryValidateSession(nil, r) == nil {
			t.Fatal("mixed primary admitted")
		}
	}
	if primaryValidateSession(nil, entity.Session{}) != nil || primaryValidateChallenge(nil, entity.MFAChallenge{}) != nil {
		t.Fatal("local compatibility")
	}
}
func TestOAuthUnconfiguredSafeViewUsesEmptyArrays(t *testing.T) {
	p := entity.OAuthProvider{ID: "oauth", ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), ScopesJSON: "[]", SubjectPathJSON: "[]"}
	v := oauthConfigView(p, entity.User{}, entity.UserMFA{})
	raw, e := json.Marshal(v)
	if e != nil || !strings.Contains(string(raw), `"scopes":[]`) || !strings.Contains(string(raw), `"subject_path":[]`) || v.Enabled || v.Verified || v.SecretConfigured {
		t.Fatal("unconfigured safe DTO")
	}
}
