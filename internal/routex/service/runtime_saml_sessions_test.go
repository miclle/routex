package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func samlTeamSessionFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, localCookie, bearer := teamSessionFixture(t)
	if data.Users[0].CreatedAt.IsZero() {
		data.Users[0].CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	user := data.Users[0]
	birth := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.SAMLProvider{ID: "saml", IDPIssuer: "urn:example:directory", Enabled: true, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), VerifiedConfigRevision: strings.Repeat("a", 64)}
	b := entity.SAMLBinding{ID: "smb_existing", ProviderID: "saml", Issuer: p.IDPIssuer, UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: "persistent-opaque-subject", SubjectDigest: samlSubjectDigest(p.IDPIssuer, "persistent-opaque-subject")}
	cookie := strings.Repeat("o", 43)
	row := entity.Session{ID: "ses_saml", UserID: user.ID, TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour), PrimaryMethod: "saml", SAMLBindingID: b.ID, SAMLBindingCreatedAt: &b.CreatedAt, SAMLUserCreatedAt: &b.UserCreatedAt, SAMLConfigRevision: p.ConfigRevision, SAMLPolicyRevision: p.PolicyRevision}
	p.VerifiedBy = user.ID
	p.VerifiedUserCreatedAt = &b.UserCreatedAt
	p.VerifiedBindingID = b.ID
	p.VerifiedBindingCreatedAt = &b.CreatedAt
	data.TeamSessionData.Sessions = append(data.TeamSessionData.Sessions, row)
	data.TeamSessionData.SAMLProvider = &p
	data.TeamSessionData.SAMLBindings = []entity.SAMLBinding{b}
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

func TestSAMLRuntimeRevocationPreservesLocalSession(t *testing.T) {
	for _, mode := range []string{"provider-policy", "exact-binding"} {
		t.Run(mode, func(t *testing.T) {
			s, data, cookie, local := samlTeamSessionFixture(t)
			identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			if err != nil {
				t.Fatal(err)
			}
			p := data.TeamSessionData.SAMLProvider
			b := data.TeamSessionData.SAMLBindings[0]
			if mode == "provider-policy" {
				s.invalidateRuntimeSAMLPolicy(p.PolicyRevision)
			} else {
				s.invalidateRuntimeSAMLBinding(b.ID, b.CreatedAt)
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

func TestSAMLRuntimeRejectsStaleAndUnknownPrimaryProof(t *testing.T) {
	cases := map[string]func(*teamSessionRuntimeData){
		"different issuer":                   func(d *teamSessionRuntimeData) { d.SAMLProvider.IDPIssuer = "urn:example:other" },
		"mixed OAuth proof":                  func(d *teamSessionRuntimeData) { d.Sessions[1].OAuthBindingID = "oab_mixed" },
		"missing provider":                   func(d *teamSessionRuntimeData) { d.SAMLProvider = nil },
		"disabled provider":                  func(d *teamSessionRuntimeData) { d.SAMLProvider.Enabled = false },
		"unverified revision":                func(d *teamSessionRuntimeData) { d.SAMLProvider.VerifiedConfigRevision = strings.Repeat("c", 64) },
		"changed policy":                     func(d *teamSessionRuntimeData) { d.SAMLProvider.PolicyRevision = strings.Repeat("c", 64) },
		"missing verifier":                   func(d *teamSessionRuntimeData) { d.SAMLProvider.VerifiedBy = "" },
		"missing verifier birth":             func(d *teamSessionRuntimeData) { d.SAMLProvider.VerifiedUserCreatedAt = nil },
		"missing verification binding":       func(d *teamSessionRuntimeData) { d.SAMLProvider.VerifiedBindingID = "" },
		"missing verification binding birth": func(d *teamSessionRuntimeData) { d.SAMLProvider.VerifiedBindingCreatedAt = nil },
		"missing binding":                    func(d *teamSessionRuntimeData) { d.SAMLBindings = nil },
		"reborn binding": func(d *teamSessionRuntimeData) {
			d.SAMLBindings[0].CreatedAt = d.SAMLBindings[0].CreatedAt.Add(time.Microsecond)
		},
		"reborn user": func(d *teamSessionRuntimeData) {
			d.SAMLBindings[0].UserCreatedAt = d.SAMLBindings[0].UserCreatedAt.Add(time.Microsecond)
		},
		"changed exact subject": func(d *teamSessionRuntimeData) {
			d.SAMLBindings[0].Subject = "Persistent-opaque-subject"
		},
		"folded actor": func(d *teamSessionRuntimeData) {
			d.SAMLBindings[0].UserID = strings.ToUpper(d.SAMLBindings[0].UserID)
		},
		"unknown primary method": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "SAML" },
		"missing primary marker": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "" },
		"missing binding birth":  func(d *teamSessionRuntimeData) { d.Sessions[1].SAMLBindingCreatedAt = nil },
		"missing user birth":     func(d *teamSessionRuntimeData) { d.Sessions[1].SAMLUserCreatedAt = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, data, cookie, local := samlTeamSessionFixture(t)
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

func TestSAMLRuntimeCapturedProofCannotMoveToNewPolicy(t *testing.T) {
	s, data, cookie, _ := samlTeamSessionFixture(t)
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.SAMLProvider.PolicyRevision = strings.Repeat("d", 64)
	data.TeamSessionData.Sessions[1].SAMLPolicyRevision = strings.Repeat("d", 64)
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); err != nil {
		t.Fatal("fresh valid policy", err)
	}
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 401, "invalid_session")
}
