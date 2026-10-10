package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func oidcValidInput() OIDCProviderInput {
	return OIDCProviderInput{Name: "Corporate identity", Issuer: "https://identity.example/tenant", ClientID: "routex", CallbackURL: "https://routex.example/api/v1/auth/oidc/callback", SecretAction: "replace", ClientSecret: "transient-secret", Reason: "Configure corporate sign-in"}
}
func TestOIDCStrictConfigurationWire(t *testing.T) {
	good := oidcValidInput()
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var parsed OIDCProviderInput
	if e = json.Unmarshal(raw, &parsed); e != nil || parsed != good {
		t.Fatal("valid configuration", e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*OIDCProviderInput)
	}{
		{"http_issuer", func(v *OIDCProviderInput) { v.Issuer = "http://identity.example" }},
		{"issuer_query", func(v *OIDCProviderInput) { v.Issuer += "?tenant=other" }},
		{"issuer_fragment", func(v *OIDCProviderInput) { v.Issuer += "#other" }},
		{"issuer_userinfo", func(v *OIDCProviderInput) { v.Issuer = "https://user:password@identity.example" }},
		{"callback_wrong_path", func(v *OIDCProviderInput) { v.CallbackURL = "https://routex.example/login" }},
		{"callback_encoded_path", func(v *OIDCProviderInput) { v.CallbackURL = "https://routex.example/api/v1/auth/oidc/%63allback" }},
		{"client_whitespace", func(v *OIDCProviderInput) { v.ClientID = "routex client" }},
		{"client_overflow", func(v *OIDCProviderInput) { v.ClientID = strings.Repeat("a", 257) }},
		{"name_overflow", func(v *OIDCProviderInput) { v.Name = strings.Repeat("界", 101) }},
		{"keep_with_secret", func(v *OIDCProviderInput) { v.SecretAction = "keep" }},
		{"replace_empty", func(v *OIDCProviderInput) { v.ClientSecret = "" }},
		{"secret_overflow", func(v *OIDCProviderInput) { v.ClientSecret = strings.Repeat("a", 4097) }},
		{"empty_reason", func(v *OIDCProviderInput) { v.Reason = "" }},
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
	if e = oidcValidateConfigInput(&good); e != nil {
		t.Fatal("client ID boundary", e)
	}
}
func TestOIDCLocalProofWireIsExplicit(t *testing.T) {
	for _, valid := range []string{`{"password":"a-valid-password","proof":{},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"recovery_code":"recovery"},"reason":"Link account"}`} {
		var v OIDCIdentityInput
		if e := json.Unmarshal([]byte(valid), &v); e != nil {
			t.Fatal(e)
		}
	}
	for _, invalid := range []string{`{"password":"a-valid-password","reason":"Link account"}`, `{"password":"a-valid-password","proof":null,"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":""},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","recovery_code":"recovery"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":null},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"unknown":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","code":"654321"},"reason":"Link account"}`} {
		var v OIDCIdentityInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid local proof admitted", e)
		}
	}
	for _, invalid := range []string{`{"enabled":null,"reason":"Enable"}`, `{"enabled":true,"reason":"Enable","extra":true}`, `{"enabled":true,"reason":""}`} {
		var v OIDCStatusInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("status wire", e)
		}
	}
}
func TestOIDCCookieDerivationAndCeremonyLifetime(t *testing.T) {
	cookie := strings.Repeat("a", 43)
	a := oidcDerived(cookie, "oic_first", "pkce")
	if !oidcBrowserValue(a) || a == oidcDerived(cookie, "oic_first", "nonce") || a == oidcDerived(cookie, "oic_second", "pkce") || a == oidcDerived(strings.Repeat("b", 43), "oic_first", "pkce") {
		t.Fatal("browser derivation lacks separation")
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	birth := now.Add(-time.Minute)
	p := entity.OIDCProvider{ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("a", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "oib_admin", VerifiedBindingCreatedAt: &birth}
	c := entity.OIDCCeremony{ID: "oic_first", Purpose: "login", CreatedAt: birth, ExpiresAt: birth.Add(oidcCeremonyLifetime), ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if !oidcCeremonyCurrent(p, c, now) {
		t.Fatal("valid browser ceremony")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*entity.OIDCProvider, *entity.OIDCCeremony)
	}{
		{"disabled", func(p *entity.OIDCProvider, _ *entity.OIDCCeremony) { p.Enabled = false }},
		{"unverified", func(p *entity.OIDCProvider, _ *entity.OIDCCeremony) { p.VerifiedConfigRevision = "" }},
		{"policy_changed", func(p *entity.OIDCProvider, _ *entity.OIDCCeremony) { p.PolicyRevision = strings.Repeat("c", 64) }},
		{"config_changed", func(p *entity.OIDCProvider, _ *entity.OIDCCeremony) { p.ConfigRevision = strings.Repeat("c", 64) }},
		{"expired", func(_ *entity.OIDCProvider, c *entity.OIDCCeremony) { c.ExpiresAt = now }},
		{"renewed_lifetime", func(_ *entity.OIDCProvider, c *entity.OIDCCeremony) {
			c.ExpiresAt = c.CreatedAt.Add(oidcCeremonyLifetime + time.Microsecond)
		}},
		{"future_birth", func(_ *entity.OIDCProvider, c *entity.OIDCCeremony) { c.CreatedAt = now.Add(time.Second) }},
		{"unknown_purpose", func(_ *entity.OIDCProvider, c *entity.OIDCCeremony) { c.Purpose = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, d := p, c
			tc.mutate(&q, &d)
			if oidcCeremonyCurrent(q, d, now) {
				t.Fatal("obsolete ceremony accepted")
			}
		})
	}
	p.Enabled = false
	c.Purpose = "verify"
	if !oidcCeremonyCurrent(p, c, now) {
		t.Fatal("explicit admin verification while disabled")
	}
}
func TestOIDCExternalIdentityAndReviewNeverAlias(t *testing.T) {
	if oidcSubjectDigest("https://issuer.example", "Alice") == oidcSubjectDigest("https://issuer.example", "alice") || oidcSubjectDigest("https://issuer.example/A", "subject") == oidcSubjectDigest("https://issuer.example/a", "subject") || oidcSubjectDigest("ab", "c") == oidcSubjectDigest("a", "bc") {
		t.Fatal("external identity normalized or ambiguous")
	}
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	p := entity.OIDCProvider{ID: "oidc", CreatedAt: now, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64)}
	u := entity.User{ID: "usr_actor", CreatedAt: now}
	m := entity.UserMFA{}
	before := oidcReview(p, u, m)
	p.ReviewRevision = strings.Repeat("d", 64)
	if before == oidcReview(p, u, m) {
		t.Fatal("review ABA")
	}
	p.ReviewRevision = strings.Repeat("a", 64)
	u.CreatedAt = now.Add(time.Microsecond)
	if before == oidcReview(p, u, m) {
		t.Fatal("actor rebirth")
	}
}
func TestOIDCPrimaryMarkersNeverDowngrade(t *testing.T) {
	if e := oidcValidatePrimary(nil, entity.Session{}); e != nil {
		t.Fatal("legacy local session", e)
	}
	now := time.Now()
	for _, s := range []entity.Session{{PrimaryMethod: "local"}, {OIDCBindingID: "oib_retained"}, {OIDCBindingCreatedAt: &now}, {OIDCConfigRevision: strings.Repeat("a", 64)}, {OIDCPolicyRevision: strings.Repeat("b", 64)}, {OIDCUserCreatedAt: &now}, {PrimaryMethod: "oidc"}, {PrimaryMethod: "OIDC"}} {
		if e := oidcValidatePrimary(nil, s); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("incomplete external provenance became local", e)
		}
	}
	if e := oidcValidateChallengePrimary(nil, entity.MFAChallenge{}); e != nil {
		t.Fatal("legacy local challenge", e)
	}
	for _, c := range []entity.MFAChallenge{{PrimaryMethod: "local"}, {PrimaryMethod: "oidc"}, {OIDCBindingID: "oib_retained"}, {OIDCBindingCreatedAt: &now}, {OIDCConfigRevision: strings.Repeat("a", 64)}, {OIDCPolicyRevision: strings.Repeat("b", 64)}, {OIDCUserCreatedAt: &now}} {
		if e := oidcValidateChallengePrimary(nil, c); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("partial challenge provenance became local", e)
		}
	}
	if oidcError(errors.New("secret upstream body")) != oidcUnavailable || oidcError(apperrors.ErrForbidden) != apperrors.ErrForbidden {
		t.Fatal("unsafe error exposure")
	}
}

func TestOIDCAuditFactsRetainReasonWithoutRemoteMaterial(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 123456000, time.UTC)
	u := entity.User{ID: "usr_actor", CreatedAt: birth}
	p := entity.OIDCProvider{ID: "oidc", CreatedAt: birth, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), Issuer: "https://private-issuer.example", ClientID: "private-client", AuthCiphertext: "encrypted-private-secret"}
	b := entity.OIDCBinding{ID: "oib_actor", CreatedAt: birth, UserID: u.ID, UserCreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: "private-subject", SubjectDigest: "private-digest"}
	raw, e := oidcAuditJSON(u, p, &b, "Review corporate access")
	if e != nil {
		t.Fatal(e)
	}
	var facts map[string]json.RawMessage
	if e = json.Unmarshal([]byte(raw), &facts); e != nil {
		t.Fatal(e)
	}
	if len(facts) != 10 || string(facts["reason"]) != `"Review corporate access"` || string(facts["binding_id"]) != `"oib_actor"` {
		t.Fatal("typed audit facts/reason", raw)
	}
	for _, private := range []string{p.Issuer, p.ClientID, p.AuthCiphertext, b.Subject, b.SubjectDigest} {
		if strings.Contains(raw, private) {
			t.Fatal("remote identity or secret retained in audit")
		}
	}
	for _, reason := range []string{"", "  Review  ", strings.Repeat("a", 1025)} {
		if _, e = oidcAuditJSON(u, p, &b, reason); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid audit reason admitted")
		}
	}
	reborn := b
	reborn.UserCreatedAt = birth.Add(time.Microsecond)
	if _, e = oidcAuditJSON(u, p, &reborn, "Review"); !errors.Is(e, apperrors.ErrBadRequest) {
		t.Fatal("aliased actor birth accepted")
	}
}
