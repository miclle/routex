package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func ldapValidInput() LDAPProviderInput {
	return LDAPProviderInput{Name: "Corporate directory", Endpoint: "ldaps://directory.example:636", BindDN: "cn=service,dc=example", BaseDN: "dc=example", UserFilter: "(uid={username})", IdentityAttribute: "entryUUID", SecretAction: "replace", BindPassword: "transient-service-password", Reason: "Configure directory"}
}
func TestLDAPApplicationStrictConfigAndProofWire(t *testing.T) {
	good := ldapValidInput()
	raw, e := json.Marshal(good)
	if e != nil {
		t.Fatal(e)
	}
	var out LDAPProviderInput
	if e = json.Unmarshal(raw, &out); e != nil || !reflect.DeepEqual(out, good) {
		t.Fatal("valid config", e)
	}
	for _, mutate := range []func(*LDAPProviderInput){func(v *LDAPProviderInput) { v.Endpoint = "ldap://directory.example:389" }, func(v *LDAPProviderInput) { v.Endpoint += "/dc=example" }, func(v *LDAPProviderInput) { v.Endpoint += "?filter=all" }, func(v *LDAPProviderInput) { v.Endpoint = "ldaps://user:secret@directory.example:636" }, func(v *LDAPProviderInput) { v.BindDN = "invalid" }, func(v *LDAPProviderInput) { v.BaseDN = "" }, func(v *LDAPProviderInput) { v.UserFilter = "(uid=all)" }, func(v *LDAPProviderInput) { v.UserFilter = "(|(uid={username})(cn={username}))" }, func(v *LDAPProviderInput) { v.IdentityAttribute = "entryuuid" }, func(v *LDAPProviderInput) { v.IdentityAttribute = "uid" }, func(v *LDAPProviderInput) { v.BindPassword = "" }, func(v *LDAPProviderInput) { v.SecretAction = "keep" }, func(v *LDAPProviderInput) { v.Reason = "" }} {
		v := good
		mutate(&v)
		encoded, _ := json.Marshal(v)
		if json.Unmarshal(encoded, &out) == nil {
			t.Fatal("unsafe config accepted")
		}
	}
	for _, bad := range []string{strings.Replace(string(raw), `"name":"Corporate directory"`, `"name":null`, 1), strings.TrimSuffix(string(raw), "}") + `,"name":"duplicate"}`, strings.TrimSuffix(string(raw), "}") + `,"extra":true}`} {
		if json.Unmarshal([]byte(bad), &out) == nil {
			t.Fatal("null/duplicate/unknown admitted")
		}
	}
	good.SecretAction = "keep"
	good.BindPassword = ""
	if ldapValidateConfigInput(&good) != nil {
		t.Fatal("explicit keep")
	}
	for _, raw := range []string{`{"password":"local-password-123","proof":{},"reason":"Link","username":"alice","directory_password":" x "}`, `{"password":"local-password-123","proof":{"code":"123456"},"reason":"Link","username":"alice","directory_password":"directory"}`} {
		var v LDAPBindingInput
		if json.Unmarshal([]byte(raw), &v) != nil {
			t.Fatal("valid explicit proof")
		}
	}
	for _, raw := range []string{`{"password":"local-password-123","proof":null,"reason":"Link","username":"alice","directory_password":"directory"}`, `{"password":"local-password-123","proof":{"code":"123456","recovery_code":"other"},"reason":"Link","username":"alice","directory_password":"directory"}`, `{"username":"alice","password":""}`, `{"username":"alice","password":"directory","extra":true}`} {
		var v LDAPBindingInput
		var login LDAPLoginInput
		if json.Unmarshal([]byte(raw), &v) == nil || json.Unmarshal([]byte(raw), &login) == nil {
			t.Fatal("unsafe proof admitted")
		}
	}
}
func TestLDAPApplicationImmutableSubjectBytesAndNamespace(t *testing.T) {
	uuid := []byte("12345678-1234-4321-abcd-123456789abc")
	upper := []byte(strings.ToUpper(string(uuid)))
	guid := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	for _, tc := range []struct {
		mode string
		raw  []byte
		ok   bool
	}{{"entryUUID", uuid, true}, {"entryUUID", upper, true}, {"objectGUID", guid, true}, {"entryUUID", nil, false}, {"objectGUID", make([]byte, 16), false}, {"entryUUID", []byte("00000000-0000-0000-0000-000000000000"), false}, {"objectGUID", uuid, false}, {"entryuuid", uuid, false}} {
		if ldapSubjectValid(tc.mode, tc.raw) != tc.ok {
			t.Fatal("stable identity bounds", tc.mode)
		}
	}
	a := base64.StdEncoding.EncodeToString(uuid)
	z := base64.StdEncoding.EncodeToString(upper)
	if ldapSubjectDigest("entryUUID", a) == ldapSubjectDigest("entryUUID", z) || ldapSubjectDigest("entryUUID", a) == ldapSubjectDigest("objectGUID", a) {
		t.Fatal("opaque case/attribute alias")
	}
	row := entity.LDAPBinding{IdentityAttribute: "entryUUID", Subject: a, SubjectDigest: ldapSubjectDigest("entryUUID", a)}
	if !ldapStoredSubject(row) {
		t.Fatal("canonical subject")
	}
	row.Subject += "\n"
	if ldapStoredSubject(row) {
		t.Fatal("noncanonical base64 accepted")
	}
}
func TestLDAPApplicationCaptureAndMixedPrimaryNeverDowngrade(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.LDAPProvider{ID: "ldap", CreatedAt: now, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), ReviewRevision: strings.Repeat("c", 64)}
	if !ldapSameProvider(p, p) {
		t.Fatal("baseline")
	}
	for _, mutate := range []func(*entity.LDAPProvider){func(v *entity.LDAPProvider) { v.CreatedAt = v.CreatedAt.Add(time.Microsecond) }, func(v *entity.LDAPProvider) { v.ConfigRevision = strings.Repeat("d", 64) }, func(v *entity.LDAPProvider) { v.PolicyRevision = strings.Repeat("e", 64) }, func(v *entity.LDAPProvider) { v.ReviewRevision = strings.Repeat("f", 64) }, func(v *entity.LDAPProvider) { v.SecretGeneration = "changed" }} {
		q := p
		mutate(&q)
		if ldapSameProvider(p, q) {
			t.Fatal("stale provider capture")
		}
	}
	for _, row := range []entity.Session{{LDAPBindingID: "ldb_retained"}, {LDAPBindingCreatedAt: &now}, {LDAPConfigRevision: strings.Repeat("a", 64)}, {LDAPPolicyRevision: strings.Repeat("b", 64)}, {LDAPUserCreatedAt: &now}, {PrimaryMethod: "ldap"}, {PrimaryMethod: "LDAP"}, {PrimaryMethod: "oidc", LDAPBindingID: "mixed"}, {PrimaryMethod: "oauth", LDAPBindingID: "mixed"}} {
		if !errors.Is(primaryValidateSession(nil, row), apperrors.ErrUnauthorized) {
			t.Fatal("mixed/missing proof degraded")
		}
	}
	if ldapError(errors.New("private DN/password")) != ldapUnavailable {
		t.Fatal("error leakage")
	}
}
func TestLDAPRootInventoryV5PreservesAllHistoricalScopes(t *testing.T) {
	want := []string{"provider_credentials", "egresses", "smtp_settings", "storage_revisions", "user_mfa", "vault_writer_auth", "vault_reader_auth", "oidc_providers", "oauth_providers", "ldap_providers"}
	current := append(append([]string(nil), want...), "named_identity_providers")
	if rootInventoryVersion != 8 || !reflect.DeepEqual(rootDomains, current) {
		t.Fatal("current V8 inventory")
	}
	for v, n := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9, 5: 10} {
		if !reflect.DeepEqual(rootInventoryDomains(v), want[:n]) {
			t.Fatal("historical scope", v)
		}
	}
	for _, v := range []int{6, 7, 8} {
		if !reflect.DeepEqual(rootInventoryDomains(v), current) {
			t.Fatal("named identity scope", v)
		}
	}
	if rootInventoryDomains(9) != nil {
		t.Fatal("future scope")
	}
	spec, e := rootSpec("ldap_providers")
	if e != nil || spec.table != "ldap_providers" || spec.ciphertext != "auth_ciphertext" || rootReference("ldap_providers", "ldap", "generation") != "ldap:ldap:generation" {
		t.Fatal("LDAP root reference")
	}
}

func TestLDAPAuditProjectsReasonWithoutPrivateIdentity(t *testing.T) {
	birth := time.Now().UTC().Truncate(time.Microsecond)
	u := entity.User{ID: "usr_audit", CreatedAt: birth}
	p := entity.LDAPProvider{ID: "ldap", CreatedAt: birth, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64)}
	b := entity.LDAPBinding{ID: "ldb_audit", UserID: u.ID, UserCreatedAt: birth, ProviderID: "ldap", ConfigRevision: p.ConfigRevision, CreatedAt: birth, Subject: "private-subject", SubjectDigest: "private-digest"}
	raw, e := ldapAuditJSON(u, p, &b, "Reviewed directory link")
	if e != nil {
		t.Fatal(e)
	}
	row := entity.AuditEvent{ActorID: u.ID, ResourceType: "ldap_binding", ResourceID: b.ID, Action: "account.ldap.bind", DetailsJSON: &raw}
	v, ok := ldapAuditProjection(row)
	if !ok || v.Kind != "ldap_identity" || v.Reason != "Reviewed directory link" {
		t.Fatal("typed reason projection")
	}
	for _, private := range []string{b.Subject, b.SubjectDigest} {
		if strings.Contains(raw, private) {
			t.Fatal("private identity escaped")
		}
	}
	for _, mutate := range []func(*entity.AuditEvent){func(r *entity.AuditEvent) { r.ResourceID = "ldb_other" }, func(r *entity.AuditEvent) { r.ResourceType = "ldap_provider" }, func(r *entity.AuditEvent) { r.Action = "identity.ldap.login" }, func(r *entity.AuditEvent) {
		bad := strings.TrimSuffix(*r.DetailsJSON, "}") + `,"subject":"private"}`
		r.DetailsJSON = &bad
	}, func(r *entity.AuditEvent) {
		bad := strings.TrimSuffix(*r.DetailsJSON, "}") + `,"reason":"duplicate"}`
		r.DetailsJSON = &bad
	}} {
		bad := row
		mutate(&bad)
		if _, ok := ldapAuditProjection(bad); ok {
			t.Fatal("untrusted audit admitted")
		}
	}
}
