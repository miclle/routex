package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func oauthTeamSessionFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, localCookie, bearer := teamSessionFixture(t)
	if data.Users[0].CreatedAt.IsZero() {
		data.Users[0].CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	user := data.Users[0]
	birth := time.Now().UTC().Truncate(time.Microsecond)
	p := entity.OAuthProvider{ID: "oauth", AuthorizationURL: "https://issuer.example/authorize", TokenURL: "https://issuer.example/token", UserInfoURL: "https://issuer.example/profile", Enabled: true, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), VerifiedConfigRevision: strings.Repeat("a", 64)}
	b := entity.OAuthBinding{ID: "oab_existing", ProviderID: "oauth", SubjectKind: "string", UserID: user.ID, UserCreatedAt: user.CreatedAt, CreatedAt: birth, ConfigRevision: p.ConfigRevision, Subject: "case-sensitive-subject", SubjectDigest: oauthSubjectDigest(p.ID, "string", "case-sensitive-subject")}
	cookie := strings.Repeat("o", 43)
	row := entity.Session{ID: "ses_oauth", UserID: user.ID, TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour), PrimaryMethod: "oauth", OAuthBindingID: b.ID, OAuthBindingCreatedAt: &b.CreatedAt, OAuthUserCreatedAt: &b.UserCreatedAt, OAuthConfigRevision: p.ConfigRevision, OAuthPolicyRevision: p.PolicyRevision}
	p.VerifiedBy = user.ID
	p.VerifiedUserCreatedAt = &b.UserCreatedAt
	p.VerifiedBindingID = b.ID
	p.VerifiedBindingCreatedAt = &b.CreatedAt
	data.TeamSessionData.Sessions = append(data.TeamSessionData.Sessions, row)
	data.TeamSessionData.OAuthProvider = &p
	data.TeamSessionData.OAuthBindings = []entity.OAuthBinding{b}
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

func TestOAuthRuntimeRevocationPreservesLocalSession(t *testing.T) {
	for _, mode := range []string{"provider-policy", "exact-binding"} {
		t.Run(mode, func(t *testing.T) {
			s, data, cookie, local := oauthTeamSessionFixture(t)
			identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			if err != nil {
				t.Fatal(err)
			}
			p := data.TeamSessionData.OAuthProvider
			b := data.TeamSessionData.OAuthBindings[0]
			if mode == "provider-policy" {
				s.invalidateRuntimeOAuthPolicy(p.PolicyRevision)
			} else {
				s.invalidateRuntimeOAuthBinding(b.ID, b.CreatedAt)
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

func TestOAuthRuntimeRejectsStaleAndUnknownPrimaryProof(t *testing.T) {
	cases := map[string]func(*teamSessionRuntimeData){
		"missing provider":                   func(d *teamSessionRuntimeData) { d.OAuthProvider = nil },
		"disabled provider":                  func(d *teamSessionRuntimeData) { d.OAuthProvider.Enabled = false },
		"unverified revision":                func(d *teamSessionRuntimeData) { d.OAuthProvider.VerifiedConfigRevision = strings.Repeat("c", 64) },
		"changed policy":                     func(d *teamSessionRuntimeData) { d.OAuthProvider.PolicyRevision = strings.Repeat("c", 64) },
		"missing verifier":                   func(d *teamSessionRuntimeData) { d.OAuthProvider.VerifiedBy = "" },
		"missing verifier birth":             func(d *teamSessionRuntimeData) { d.OAuthProvider.VerifiedUserCreatedAt = nil },
		"missing verification binding":       func(d *teamSessionRuntimeData) { d.OAuthProvider.VerifiedBindingID = "" },
		"missing verification binding birth": func(d *teamSessionRuntimeData) { d.OAuthProvider.VerifiedBindingCreatedAt = nil },
		"missing binding":                    func(d *teamSessionRuntimeData) { d.OAuthBindings = nil },
		"reborn binding": func(d *teamSessionRuntimeData) {
			d.OAuthBindings[0].CreatedAt = d.OAuthBindings[0].CreatedAt.Add(time.Microsecond)
		},
		"reborn user": func(d *teamSessionRuntimeData) {
			d.OAuthBindings[0].UserCreatedAt = d.OAuthBindings[0].UserCreatedAt.Add(time.Microsecond)
		},
		"changed exact subject": func(d *teamSessionRuntimeData) { d.OAuthBindings[0].Subject = "Case-sensitive-subject" },
		"folded actor": func(d *teamSessionRuntimeData) {
			d.OAuthBindings[0].UserID = strings.ToUpper(d.OAuthBindings[0].UserID)
		},
		"unknown primary method": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "OAuth" },
		"missing primary marker": func(d *teamSessionRuntimeData) { d.Sessions[1].PrimaryMethod = "" },
		"missing binding birth":  func(d *teamSessionRuntimeData) { d.Sessions[1].OAuthBindingCreatedAt = nil },
		"missing user birth":     func(d *teamSessionRuntimeData) { d.Sessions[1].OAuthUserCreatedAt = nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, data, cookie, local := oauthTeamSessionFixture(t)
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

func TestOAuthRuntimeCapturedProofCannotMoveToNewPolicy(t *testing.T) {
	s, data, cookie, _ := oauthTeamSessionFixture(t)
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.OAuthProvider.PolicyRevision = strings.Repeat("d", 64)
	data.TeamSessionData.Sessions[1].OAuthPolicyRevision = strings.Repeat("d", 64)
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); err != nil {
		t.Fatal("fresh valid policy", err)
	}
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 401, "invalid_session")
}

func TestOAuthRuntimeRevocationFenceHasNoSessionCountLimit(t *testing.T) {
	s, data, _, _ := oauthTeamSessionFixture(t)
	revision := data.TeamSessionData.OAuthProvider.PolicyRevision
	s.invalidateRuntimeOAuthPolicy(revision)
	count := 0
	s.runtime.deniedOAuthPolicies.Range(func(_, _ any) bool { count++; return true })
	if count != 1 {
		t.Fatal("revocation must retain one policy fence")
	}
	for i := 0; i < 10001; i++ {
		if !s.runtimeOAuthSessionDenied(runtimeTeamSession{OAuthPolicyRevision: revision}) {
			t.Fatal("large Session population bypassed revocation")
		}
	}
	generation := s.runtime.epoch.Load()
	s.invalidateRuntimeOAuthPolicy(strings.Repeat("e", 64))
	clearRuntimeTombstones(&s.runtime.deniedOAuthPolicies, generation)
	if !runtimeDenied(&s.runtime.deniedOAuthPolicies, strings.Repeat("e", 64)) {
		t.Fatal("stale publication erased a newer fence")
	}
}

func TestOAuthRuntimeFencesCannotRevokeOIDCOrLocal(t *testing.T) {
	s, data, oidcCookie, localCookie := oidcTeamSessionFixture(t)
	policy := data.TeamSessionData.OIDCProvider.PolicyRevision
	binding := data.TeamSessionData.OIDCBindings[0]
	s.invalidateRuntimeOAuthPolicy(policy)
	s.invalidateRuntimeOAuthBinding(binding.ID, binding.CreatedAt)
	for _, cookie := range []string{oidcCookie, localCookie} {
		if _, e := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); e != nil {
			t.Fatal("OAuth fence crossed authentication namespace", e)
		}
	}
	row := data.TeamSessionData.Sessions[1]
	row.OAuthBindingID = "oab_unrelated"
	if primaryRuntimeSession(row, data.TeamSessionData.OIDCProvider, map[string]entity.OIDCBinding{binding.ID: binding}, nil, nil, map[string]entity.User{binding.UserID: data.Users[0]}) {
		t.Fatal("mixed external runtime proof admitted")
	}
}
