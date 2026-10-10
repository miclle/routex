package service

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func samlValidInput(t *testing.T) SAMLProviderInput {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return SAMLProviderInput{Name: "Corporate identity", IDPIssuer: "urn:example:idp", SSOURL: "https://identity.example/sso", SPEntityID: "urn:example:routex", ACSURL: "https://routex.example/api/v1/auth/saml/acs", SigningCertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Reason: "Configure corporate sign-in"}
}
func TestSAMLStrictPublicConfigurationAndCanonicalCertificate(t *testing.T) {
	good := samlValidInput(t)
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var parsed SAMLProviderInput
	if e = json.Unmarshal(raw, &parsed); e != nil || parsed != good {
		t.Fatal("valid exact configuration", e)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*SAMLProviderInput)
	}{
		{"http", func(v *SAMLProviderInput) { v.SSOURL = "http://identity.example/sso" }},
		{"sso_query", func(v *SAMLProviderInput) { v.SSOURL += "?other=1" }},
		{"userinfo", func(v *SAMLProviderInput) { v.SSOURL = "https://user:password@identity.example/sso" }},
		{"wrong_acs", func(v *SAMLProviderInput) { v.ACSURL = "https://routex.example/login" }},
		{"encoded_acs", func(v *SAMLProviderInput) { v.ACSURL = "https://routex.example/api/v1/auth/saml/%61cs" }},
		{"relative_issuer", func(v *SAMLProviderInput) { v.IDPIssuer = "directory" }},
		{"issuer_control", func(v *SAMLProviderInput) { v.IDPIssuer = "urn:example:idp\n" }},
		{"name_bound", func(v *SAMLProviderInput) { v.Name = strings.Repeat("界", 101) }},
		{"no_reason", func(v *SAMLProviderInput) { v.Reason = "" }},
		{"private_key", func(v *SAMLProviderInput) {
			v.SigningCertificatePEM = strings.ReplaceAll(v.SigningCertificatePEM, "CERTIFICATE", "PRIVATE KEY")
		}},
		{"multiple_certificates", func(v *SAMLProviderInput) { v.SigningCertificatePEM += v.SigningCertificatePEM }},
		{"malformed_first_then_valid", func(v *SAMLProviderInput) {
			v.SigningCertificatePEM = "-----BEGIN CERTIFICATE-----\n!\n" + v.SigningCertificatePEM
		}},
		{"trailing_data", func(v *SAMLProviderInput) { v.SigningCertificatePEM += "not a certificate" }},
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
	for _, bad := range []string{strings.Replace(string(raw), `"name":"Corporate identity"`, `"name":null`, 1), strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`, strings.TrimSuffix(string(raw), "}") + `,"private_key":"never"}`} {
		if json.Unmarshal([]byte(bad), &parsed) == nil {
			t.Fatal("ambiguous/unknown/null admitted")
		}
	}
	wrapped := good
	wrapped.SigningCertificatePEM = " \n" + strings.ReplaceAll(good.SigningCertificatePEM, "\n", "\r\n") + " \n"
	if e = samlValidateConfigInput(&wrapped); e != nil || wrapped.SigningCertificatePEM != good.SigningCertificatePEM {
		t.Fatal("same certificate identity lost", e)
	}
}
func TestSAMLDoubleCookieInputsAndExactReceiptIdentity(t *testing.T) {
	raw := bytes.Repeat([]byte{1}, 32)
	v := base64.RawURLEncoding.EncodeToString(raw)
	if !samlBrowserValue(v) {
		t.Fatal("valid browser secret")
	}
	for _, bad := range []string{v + "=", v[:42], v[:42] + "F", strings.Repeat("!", 43)} {
		if samlBrowserValue(bad) {
			t.Fatal("noncanonical secret admitted")
		}
	}
	var service Service
	for _, pair := range [][2]string{{v, ""}, {"", v}, {v, v + "="}} {
		if _, e := service.CompleteSAML(context.Background(), nil, pair[0], pair[1]); !errors.Is(e, apperrors.ErrUnauthorized) {
			t.Fatal("one cookie must never reach storage", e)
		}
	}
	if samlAssertionDigest("urn:idp", "a") == samlAssertionDigest("urn:IDP", "a") || samlAssertionDigest("ab", "c") == samlAssertionDigest("a", "bc") || samlAssertionDigest("urn:idp", "a") == samlSubjectDigest("urn:idp", "a") {
		t.Fatal("receipt identity aliases")
	}
}
func TestSAMLCeremonyProviderBirthAndLifecycleFences(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	birth := now.Add(-time.Minute)
	p := entity.SAMLProvider{ID: "saml", CreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("a", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "smb_admin", VerifiedBindingCreatedAt: &birth}
	c := entity.SAMLCeremony{ID: "smc_first", ProviderCreatedAt: birth, Purpose: "login", CreatedAt: birth, ExpiresAt: birth.Add(samlCeremonyLifetime), ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if !samlCeremonyCurrent(p, c, now) {
		t.Fatal("valid captured ceremony")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*entity.SAMLProvider, *entity.SAMLCeremony)
	}{
		{"disabled", func(p *entity.SAMLProvider, _ *entity.SAMLCeremony) { p.Enabled = false }},
		{"unverified", func(p *entity.SAMLProvider, _ *entity.SAMLCeremony) { p.VerifiedConfigRevision = "" }},
		{"provider_aba", func(p *entity.SAMLProvider, _ *entity.SAMLCeremony) { p.CreatedAt = p.CreatedAt.Add(time.Microsecond) }},
		{"config_changed", func(p *entity.SAMLProvider, _ *entity.SAMLCeremony) { p.ConfigRevision = strings.Repeat("c", 64) }},
		{"policy_changed", func(p *entity.SAMLProvider, _ *entity.SAMLCeremony) { p.PolicyRevision = strings.Repeat("c", 64) }},
		{"expired", func(_ *entity.SAMLProvider, c *entity.SAMLCeremony) { c.ExpiresAt = now }},
		{"extended_budget", func(_ *entity.SAMLProvider, c *entity.SAMLCeremony) {
			c.ExpiresAt = c.CreatedAt.Add(samlCeremonyLifetime + time.Microsecond)
		}},
		{"future_birth", func(_ *entity.SAMLProvider, c *entity.SAMLCeremony) { c.CreatedAt = now.Add(time.Second) }},
		{"unknown_purpose", func(_ *entity.SAMLProvider, c *entity.SAMLCeremony) { c.Purpose = "other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, d := p, c
			tc.mutate(&q, &d)
			if samlCeremonyCurrent(q, d, now) {
				t.Fatal("obsolete ceremony admitted")
			}
		})
	}
	p.Enabled = false
	c.Purpose = "verify"
	if !samlCeremonyCurrent(p, c, now) {
		t.Fatal("explicit disabled admin verification")
	}
}
func TestSAMLPrimaryAndPrivateErrorsNeverDowngrade(t *testing.T) {
	if e := primaryValidateSession(nil, entity.Session{}); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	for _, r := range []entity.Session{{SAMLBindingID: "smb_old"}, {SAMLBindingCreatedAt: &now}, {SAMLConfigRevision: strings.Repeat("a", 64)}, {SAMLPolicyRevision: strings.Repeat("b", 64)}, {SAMLUserCreatedAt: &now}, {PrimaryMethod: "saml"}, {PrimaryMethod: "SAML"}, {PrimaryMethod: "ldap", SAMLBindingID: "smb_mixed"}, {PrimaryMethod: "saml", LDAPBindingID: "ldb_mixed"}} {
		if !errors.Is(primaryValidateSession(nil, r), apperrors.ErrUnauthorized) {
			t.Fatal("partial/mixed primary admitted")
		}
	}
	for _, r := range []entity.MFAChallenge{{SAMLBindingID: "smb_old"}, {PrimaryMethod: "saml"}, {PrimaryMethod: "oauth", SAMLBindingID: "smb_mixed"}} {
		if !errors.Is(primaryValidateChallenge(nil, r), apperrors.ErrUnauthorized) {
			t.Fatal("mixed challenge admitted")
		}
	}
	if samlError(errors.New("private assertion subject secret")) != samlUnavailable || samlError(apperrors.ErrForbidden) != apperrors.ErrForbidden {
		t.Fatal("raw error exposed")
	}
}

func TestSAMLLocalProofWireIsExplicit(t *testing.T) {
	for _, valid := range []string{`{"password":"a-valid-password","proof":{},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"recovery_code":"recovery"},"reason":"Link account"}`} {
		var v SAMLIdentityInput
		if e := json.Unmarshal([]byte(valid), &v); e != nil {
			t.Fatal(e)
		}
	}
	for _, invalid := range []string{`{"password":"a-valid-password","reason":"Link account"}`, `{"password":"a-valid-password","proof":null,"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":""},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","recovery_code":"recovery"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":null},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"unknown":"123456"},"reason":"Link account"}`, `{"password":"a-valid-password","proof":{"code":"123456","code":"654321"},"reason":"Link account"}`} {
		var v SAMLIdentityInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("invalid local proof admitted", e)
		}
	}
	for _, invalid := range []string{`{"enabled":null,"reason":"Enable"}`, `{"enabled":true,"reason":"Enable","extra":true}`, `{"enabled":true,"reason":""}`} {
		var v SAMLStatusInput
		if e := json.Unmarshal([]byte(invalid), &v); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("status wire", e)
		}
	}
}

func TestSAMLReplayRetentionNeverTruncatesSignedExpiry(t *testing.T) {
	baseline := time.Date(2026, 10, 10, 0, 0, 0, 123456000, time.UTC)
	for _, offset := range []time.Duration{0, time.Nanosecond, 999 * time.Nanosecond, time.Microsecond} {
		v := baseline.Add(offset)
		got := samlReceiptExpiry(v)
		if got.Before(v) || got.Sub(v) >= time.Microsecond || got.Nanosecond()%1000 != 0 {
			t.Fatal("replay receipt expired before proof")
		}
	}
}

func TestSAMLCompletionDeadlineAndOpaqueSubjectAdmission(t *testing.T) {
	for _, v := range []string{"", strings.Repeat("a", 257), "opaque\nsubject", string([]byte{255})} {
		if samlSubject(v) {
			t.Fatal("invalid persistent subject admitted")
		}
	}
	for _, v := range []string{"Exact Subject", "exact subject", "  retained spaces  ", "身份"} {
		if !samlSubject(v) {
			t.Fatal("opaque subject normalized")
		}
	}
	if samlSubjectDigest("urn:idp", "Exact Subject") == samlSubjectDigest("urn:idp", "exact subject") {
		t.Fatal("subject case folded")
	}
	future := time.Now().Add(time.Minute)
	c := entity.SAMLCeremony{ProofExpiresAt: &future, ExpiresAt: future}
	if e := samlCompletionCurrent(context.Background(), c); e != nil {
		t.Fatal("fresh completion", e)
	}
	past := time.Now().Add(-time.Microsecond)
	c.ProofExpiresAt = &past
	if !errors.Is(samlCompletionCurrent(context.Background(), c), apperrors.ErrUnauthorized) {
		t.Fatal("expired completion admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.ProofExpiresAt = &future
	if !errors.Is(samlCompletionCurrent(ctx, c), context.Canceled) {
		t.Fatal("canceled completion admitted")
	}
}
