package service

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func ldapTeamSessionFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, localCookie, bearer := teamSessionFixture(t)
	if data.Users[0].CreatedAt.IsZero() {
		data.Users[0].CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	user := data.Users[0]
	birth := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.LDAPProvider{ID: "ldap", Endpoint: "ldaps://directory.example:636", IdentityAttribute: "entryUUID", Enabled: true, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), VerifiedConfigRevision: strings.Repeat("a", 64)}
	b := entity.LDAPBinding{ID: "ldb_existing", ProviderID: "ldap", IdentityAttribute: "entryUUID", UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: base64.StdEncoding.EncodeToString([]byte("12345678-1234-4321-abcd-123456789abc")), SubjectDigest: ldapSubjectDigest("entryUUID", base64.StdEncoding.EncodeToString([]byte("12345678-1234-4321-abcd-123456789abc")))}
	cookie := strings.Repeat("o", 43)
	row := entity.Session{ID: "ses_ldap", UserID: user.ID, TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour), PrimaryMethod: "ldap", LDAPBindingID: b.ID, LDAPBindingCreatedAt: &b.CreatedAt, LDAPUserCreatedAt: &b.UserCreatedAt, LDAPConfigRevision: p.ConfigRevision, LDAPPolicyRevision: p.PolicyRevision}
	p.VerifiedBy = user.ID
	p.VerifiedUserCreatedAt = &b.UserCreatedAt
	p.VerifiedBindingID = b.ID
	p.VerifiedBindingCreatedAt = &b.CreatedAt
	data.TeamSessionData.Sessions = append(data.TeamSessionData.Sessions, row)
	data.TeamSessionData.LDAPProvider = &p
	data.TeamSessionData.LDAPBindings = []entity.LDAPBinding{b}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	// Prove both independent methods before every negative mutation.
	for _, value := range []string{cookie, localCookie} {
		if _, err := s.RuntimeAuthenticateTeamSession(context.Background(), value, "tem_one"); err != nil {
			t.Fatal("positive Session baseline", err)
		}
	}
	if _, err := s.AuthenticateAPIKey(context.Background(), bearer); err != nil {
		t.Fatal("positive Key baseline", err)
	}
	return s, data, cookie, localCookie
}

func TestLDAPRuntimeRevocationPreservesLocalSession(t *testing.T) {
	for _, mode := range []string{"provider-policy", "exact-binding"} {
		t.Run(mode, func(t *testing.T) {
			s, data, cookie, local := ldapTeamSessionFixture(t)
			identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			if err != nil {
				t.Fatal(err)
			}
			p := data.TeamSessionData.LDAPProvider
			b := data.TeamSessionData.LDAPBindings[0]
			if mode == "provider-policy" {
				s.invalidateRuntimeLDAPPolicy(p.PolicyRevision)
			} else {
				s.invalidateRuntimeLDAPBinding(b.ID, b.CreatedAt)
			}
			_, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			requireTeamSessionError(t, err, 401, "invalid_session")
			requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 401, "invalid_session")
			if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), local, "tem_one"); err != nil {
				t.Fatal("local Session was revoked", err)
			}
		})
	}
}

func TestLDAPRuntimeRejectsStaleAndUnknownPrimaryProof(t *testing.T) {
	cases := map[string]func(*teamSessionRuntimeData){
		"different identity attribute":       func(d *teamSessionRuntimeData) { d.LDAPProvider.IdentityAttribute = "objectGUID" },
		"mixed OAuth proof":                  func(d *teamSessionRuntimeData) { d.Sessions[1].OAuthBindingID = "oab_mixed" },
		"missing provider":                   func(d *teamSessionRuntimeData) { d.LDAPProvider = nil },
		"disabled provider":                  func(d *teamSessionRuntimeData) { d.LDAPProvider.Enabled = false },
		"unverified revision":                func(d *teamSessionRuntimeData) { d.LDAPProvider.VerifiedConfigRevision = strings.Repeat("c", 64) },
		"changed policy":                     func(d *teamSessionRuntimeData) { d.LDAPProvider.PolicyRevision = strings.Repeat("c", 64) },
		"missing verifier":                   func(d *teamSessionRuntimeData) { d.LDAPProvider.VerifiedBy = "" },
		"missing verifier birth":             func(d *teamSessionRuntimeData) { d.LDAPProvider.VerifiedUserCreatedAt = nil },
		"missing verification binding":       func(d *teamSessionRuntimeData) { d.LDAPProvider.VerifiedBindingID = "" },
		"missing verification binding birth": func(d *teamSessionRuntimeData) { d.LDAPProvider.VerifiedBindingCreatedAt = nil },
		"missing binding":                    func(d *teamSessionRuntimeData) { d.LDAPBindings = nil },
		"reborn binding": func(d *teamSessionRuntimeData) {
			d.LDAPBindings[0].CreatedAt = d.LDAPBindings[0].CreatedAt.Add(time.Microsecond)
		},
		"reborn user": func(d *teamSessionRuntimeData) {
			d.LDAPBindings[0].UserCreatedAt = d.LDAPBindings[0].UserCreatedAt.Add(time.Microsecond)
		},
		"changed exact subject": func(d *teamSessionRuntimeData) {
			d.LDAPBindings[0].Subject = base64.StdEncoding.EncodeToString([]byte("12345678-1234-4321-ABCD-123456789ABC"))
		},
		"folded actor": func(d *teamSessionRuntimeData) {
			d.LDAPBindings[0].UserID = strings.ToUpper(d.LDAPBindings[0].UserID)
		},
		"unknown primary method": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "LDAP" },
		"missing primary marker": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "" },
		"missing binding birth":  func(d *teamSessionRuntimeData) { d.Sessions[1].LDAPBindingCreatedAt = nil },
		"missing user birth":     func(d *teamSessionRuntimeData) { d.Sessions[1].LDAPUserCreatedAt = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, data, cookie, local := ldapTeamSessionFixture(t)
			mutate(data.TeamSessionData)
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			_, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			requireTeamSessionError(t, err, 401, "invalid_session")
			if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), local, "tem_one"); err != nil {
				t.Fatal("local Session changed", err)
			}
		})
	}
}

func TestLDAPRuntimeCapturedProofCannotMoveToNewPolicy(t *testing.T) {
	s, data, cookie, _ := ldapTeamSessionFixture(t)
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.LDAPProvider.PolicyRevision = strings.Repeat("d", 64)
	data.TeamSessionData.Sessions[1].LDAPPolicyRevision = strings.Repeat("d", 64)
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); err != nil {
		t.Fatal("fresh valid policy", err)
	}
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 401, "invalid_session")
}
