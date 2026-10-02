package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func teamSessionFixture(t *testing.T) (*Service, *runtimeData, string, string) {
	t.Helper()
	s, data, bearer := runtimeFixture(t, "http://127.0.0.1")
	cookie := strings.Repeat("s", 43)
	data.TeamSessionData = &teamSessionRuntimeData{
		Sessions:    []entity.Session{{ID: "ses_one", UserID: "usr_one", TokenHash: secret.SHA256Hex(cookie), ExpiresAt: time.Now().Add(time.Hour)}},
		Teams:       []entity.Team{{ID: "tem_one", Status: entity.ResourceActive, CreatedAt: time.Now().UTC()}},
		Memberships: []entity.TeamMembership{{ID: "tmm_one", TeamID: "tem_one", UserID: "usr_one", Role: entity.TeamMember, Status: entity.ResourceActive}},
		Grants:      []entity.TeamModelGrant{{TeamID: "tem_one", ModelID: "mdl_one"}},
	}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	return s, data, cookie, bearer
}

func requireTeamSessionError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var gateway *GatewayError
	if !errors.As(err, &gateway) || gateway.Status != status || gateway.Code != code {
		t.Fatalf("expected %d/%s, got %v", status, code, err)
	}
}

func TestRuntimeTeamSessionExactScopeAndCSRF(t *testing.T) {
	s, data, cookie, bearer := teamSessionFixture(t)
	ctx := context.Background()
	identity, err := s.RuntimeAuthenticateTeamSession(ctx, cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	if identity.SessionID != "ses_one" || identity.TeamMembershipID != "tmm_one" || len(identity.ModelIDs) != 1 || identity.ModelIDs[0] != "mdl_one" {
		t.Fatalf("wrong published scope: %+v", identity)
	}
	if !ValidateTeamSessionCSRF(identity, (&Authentication{Token: cookie}).CSRFToken()) || ValidateTeamSessionCSRF(identity, strings.Repeat("a", 64)) || ValidateTeamSessionCSRF(identity, "") {
		t.Fatal("cookie CSRF contract changed")
	}
	identity.ModelIDs[0] = "untrusted"
	if err := s.ReauthorizeTeamSession(ctx, identity, "mdl_one"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReauthorizeTeamSession(ctx, identity, ""); err != nil {
		t.Fatal("identity-only discovery failed", err)
	}
	requireTeamSessionError(t, s.ReauthorizeTeamSession(ctx, identity, "mdl_other"), 404, "model_not_found")
	data.TeamSessionData.Grants = nil
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	requireTeamSessionError(t, s.ReauthorizeTeamSession(ctx, identity, "mdl_one"), 404, "model_not_found")
	// Team grant removal does not widen or revoke the independent Personal Key.
	if _, err := s.AuthenticateAPIKey(ctx, bearer); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeTeamSessionRejectsInactiveAndFoldedIdentities(t *testing.T) {
	cases := map[string]func(*runtimeData){
		"disabled actor":          func(d *runtimeData) { d.Users[0].Disabled = true },
		"offboarded actor":        func(d *runtimeData) { now := time.Now(); d.Users[0].OffboardedAt = &now },
		"expired session":         func(d *runtimeData) { d.TeamSessionData.Sessions[0].ExpiresAt = time.Now().Add(-time.Second) },
		"folded session actor":    func(d *runtimeData) { d.TeamSessionData.Sessions[0].UserID = "USR_one" },
		"inactive team":           func(d *runtimeData) { d.TeamSessionData.Teams[0].Status = entity.ResourceDisabled },
		"inactive membership":     func(d *runtimeData) { d.TeamSessionData.Memberships[0].Status = entity.ResourceDisabled },
		"folded membership team":  func(d *runtimeData) { d.TeamSessionData.Memberships[0].TeamID = "TEM_one" },
		"folded membership actor": func(d *runtimeData) { d.TeamSessionData.Memberships[0].UserID = "USR_one" },
		"invalid role":            func(d *runtimeData) { d.TeamSessionData.Memberships[0].Role = "OWNER" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s, data, cookie, _ := teamSessionFixture(t)
			mutate(data)
			s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
			if _, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); err == nil {
				t.Fatal("invalid published identity accepted")
			}
		})
	}
	s, data, cookie, _ := teamSessionFixture(t)
	data.TeamSessionData.Grants[0].ModelID = "MDL_one"
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil || len(identity.ModelIDs) != 0 {
		t.Fatal("folded model grant accepted", err)
	}
	_, err = s.RuntimeAuthenticateTeamSession(context.Background(), "wrong", "tem_one")
	requireTeamSessionError(t, err, 401, "invalid_session")
	_, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_foreign")
	requireTeamSessionError(t, err, 403, "team_access_denied")
}

func TestRuntimeTeamSessionLocalRevocationAndCapturedMembership(t *testing.T) {
	for _, kind := range []string{"session", "session-user", "team", "member"} {
		t.Run(kind, func(t *testing.T) {
			s, _, cookie, bearer := teamSessionFixture(t)
			identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "session":
				s.invalidateRuntimeSession("ses_one")
			case "session-user":
				s.invalidateRuntimeSessionUser("usr_one")
			case "team":
				s.invalidateRuntimeTeam("tem_one")
			case "member":
				s.invalidateRuntimeTeamMember("tem_one", "usr_one")
			}
			if err := s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"); err == nil {
				t.Fatal("revocation not immediate")
			}
			if _, err := s.AuthenticateAPIKey(context.Background(), bearer); err != nil {
				t.Fatal("Session-only revocation affected Personal Key", err)
			}
			generation := s.runtime.epoch.Load()
			s.invalidateRuntimeSession("ses_newer")
			clearRuntimeTombstones(&s.runtime.deniedSessions, generation)
			if !runtimeDenied(&s.runtime.deniedSessions, "ses_newer") {
				t.Fatal("old publication erased newer logout")
			}
		})
	}
	s, data, cookie, _ := teamSessionFixture(t)
	identity, err := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.Memberships[0].ID = "tmm_rejoined"
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 403, "team_access_denied")
	auth := *s.runtime.auth.Load()
	auth.ValidUntil = time.Now().Add(-time.Second)
	s.runtime.auth.Store(&auth)
	requireTeamSessionError(t, s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one"), 503, "service_unavailable")
	s.runtime = nil
	_, err = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	requireTeamSessionError(t, err, 503, "service_unavailable")
}

func TestRuntimeTeamSessionAuthorizationDoesNotChangeRouteDigest(t *testing.T) {
	_, data, _, _ := teamSessionFixture(t)
	before, err := runtimeDigest(data)
	if err != nil {
		t.Fatal(err)
	}
	data.TeamSessionData.Sessions = nil
	data.TeamSessionData.Grants = nil
	after, err := runtimeDigest(data)
	if err != nil || before != after {
		t.Fatal("auth-only state changed route configuration", err)
	}
}
