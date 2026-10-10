package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func oidcTeamSessionFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, localCookie, bearer := teamSessionFixture(t)
	if data.Users[0].CreatedAt.IsZero() {
		data.Users[0].CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	user := data.Users[0]
	birth := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.OIDCProvider{ID: "oidc", Issuer: "https://issuer.example", Enabled: true, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), VerifiedConfigRevision: strings.Repeat("a", 64)}
	b := entity.OIDCBinding{ID: "oib_existing", UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: "case-sensitive-subject", SubjectDigest: oidcSubjectDigest(p.Issuer, "case-sensitive-subject")}
	cookie := strings.Repeat("o", 43)
	row := entity.Session{ID: "ses_oidc", UserID: user.ID, TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour), PrimaryMethod: "oidc", OIDCBindingID: b.ID, OIDCBindingCreatedAt: &b.CreatedAt, OIDCUserCreatedAt: &b.UserCreatedAt, OIDCConfigRevision: p.ConfigRevision, OIDCPolicyRevision: p.PolicyRevision}
	p.VerifiedBy = user.ID
	p.VerifiedUserCreatedAt = &b.UserCreatedAt
	p.VerifiedBindingID = b.ID
	p.VerifiedBindingCreatedAt = &b.CreatedAt
	data.TeamSessionData.Sessions = append(data.TeamSessionData.Sessions, row)
	data.TeamSessionData.OIDCProvider = &p
	data.TeamSessionData.OIDCBindings = []entity.OIDCBinding{b}
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

func TestOIDCRuntimeRevocationPreservesLocalSession(t *testing.T) {
	for _, mode := range []string{"provider-policy", "exact-binding"} {
		t.Run(mode, func(t *testing.T) {
			s, data, cookie, local := oidcTeamSessionFixture(t)
			identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			if err != nil {
				t.Fatal(err)
			}
			p := data.TeamSessionData.OIDCProvider
			b := data.TeamSessionData.OIDCBindings[0]
			if mode == "provider-policy" {
				s.invalidateRuntimeOIDCPolicy(p.PolicyRevision)
			} else {
				s.invalidateRuntimeOIDCBinding(b.ID, b.CreatedAt)
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

func TestOIDCRuntimeRejectsStaleAndUnknownPrimaryProof(t *testing.T) {
	cases := map[string]func(*teamSessionRuntimeData){
		"missing provider":                   func(d *teamSessionRuntimeData) { d.OIDCProvider = nil },
		"disabled provider":                  func(d *teamSessionRuntimeData) { d.OIDCProvider.Enabled = false },
		"unverified revision":                func(d *teamSessionRuntimeData) { d.OIDCProvider.VerifiedConfigRevision = strings.Repeat("c", 64) },
		"changed policy":                     func(d *teamSessionRuntimeData) { d.OIDCProvider.PolicyRevision = strings.Repeat("c", 64) },
		"missing verifier":                   func(d *teamSessionRuntimeData) { d.OIDCProvider.VerifiedBy = "" },
		"missing verifier birth":             func(d *teamSessionRuntimeData) { d.OIDCProvider.VerifiedUserCreatedAt = nil },
		"missing verification binding":       func(d *teamSessionRuntimeData) { d.OIDCProvider.VerifiedBindingID = "" },
		"missing verification binding birth": func(d *teamSessionRuntimeData) { d.OIDCProvider.VerifiedBindingCreatedAt = nil },
		"missing binding":                    func(d *teamSessionRuntimeData) { d.OIDCBindings = nil },
		"reborn binding": func(d *teamSessionRuntimeData) {
			d.OIDCBindings[0].CreatedAt = d.OIDCBindings[0].CreatedAt.Add(time.Microsecond)
		},
		"reborn user": func(d *teamSessionRuntimeData) {
			d.OIDCBindings[0].UserCreatedAt = d.OIDCBindings[0].UserCreatedAt.Add(time.Microsecond)
		},
		"changed exact subject":  func(d *teamSessionRuntimeData) { d.OIDCBindings[0].Subject = "Case-sensitive-subject" },
		"folded actor":           func(d *teamSessionRuntimeData) { d.OIDCBindings[0].UserID = strings.ToUpper(d.OIDCBindings[0].UserID) },
		"unknown primary method": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "OIDC" },
		"missing primary marker": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "" },
		"missing binding birth":  func(d *teamSessionRuntimeData) { d.Sessions[1].OIDCBindingCreatedAt = nil },
		"missing user birth":     func(d *teamSessionRuntimeData) { d.Sessions[1].OIDCUserCreatedAt = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, data, cookie, local := oidcTeamSessionFixture(t)
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

func TestOIDCRuntimeCapturedProofCannotMoveToNewPolicy(t *testing.T) {
	s, data, cookie, _ := oidcTeamSessionFixture(t)
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.OIDCProvider.PolicyRevision = strings.Repeat("d", 64)
	data.TeamSessionData.Sessions[1].OIDCPolicyRevision = strings.Repeat("d", 64)
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); err != nil {
		t.Fatal("fresh valid policy", err)
	}
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 401, "invalid_session")
}

func TestOIDCRuntimeRevocationFenceHasNoSessionCountLimit(t *testing.T) {
	s, data, _, _ := oidcTeamSessionFixture(t)
	revision := data.TeamSessionData.OIDCProvider.PolicyRevision
	s.invalidateRuntimeOIDCPolicy(revision)
	count := 0
	s.runtime.deniedOIDCPolicies.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Fatal("revocation must retain one policy fence")
	}
	for i := 0; i < 10001; i++ {
		if !s.runtimeOIDCSessionDenied(runtimeTeamSession{OIDCPolicyRevision: revision}) {
			t.Fatal("large Session population bypassed revocation")
		}
	}
	generation := s.runtime.epoch.Load()
	s.invalidateRuntimeOIDCPolicy(strings.Repeat("e", 64))
	clearRuntimeTombstones(&s.runtime.deniedOIDCPolicies, generation)
	if !runtimeDenied(&s.runtime.deniedOIDCPolicies, strings.Repeat("e", 64)) {
		t.Fatal("stale publication erased a newer fence")
	}
}
