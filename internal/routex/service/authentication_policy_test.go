package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

func TestAuthenticationPolicyAllowlistAndOriginalBudget(t *testing.T) {
	for _, enforced := range []bool{false, true} {
		for _, method := range []string{"", "local", "oidc", "ldap", "oauth", "saml", "github", "google", "discord", "emergency"} {
			p := entity.GovernanceSetting{ForcedSSOEnabled: enforced}
			want := !enforced && method != "emergency" || enforced && (method == "oidc" || method == "ldap" || method == "emergency")
			if authenticationMethodAllowed(p, method) != want {
				t.Fatalf("allowlist changed for %q/%t", method, enforced)
			}
		}
	}
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(2*time.Second))
	defer cancel()
	outer, local, stop := authenticationContexts(parent)
	defer stop()
	pd, _ := parent.Deadline()
	od, _ := outer.Deadline()
	ld, _ := local.Deadline()
	if !pd.Equal(od) || !pd.Equal(ld) {
		t.Fatal("caller deadline extended")
	}
	cancel()
	if outer.Err() == nil || local.Err() == nil {
		t.Fatal("cancellation not inherited")
	}
	outer, local, stop = authenticationContexts(context.Background())
	defer stop()
	od, _ = outer.Deadline()
	ld, _ = local.Deadline()
	if od.Sub(ld) != 5*time.Second || time.Until(ld) > 5*time.Second || time.Until(od) > 10*time.Second {
		t.Fatal("local/remote deadlines not fixed at one admission")
	}
	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	_, local, stop = authenticationContexts(expired)
	defer stop()
	if local.Err() != context.DeadlineExceeded {
		t.Fatal("expired work received a new budget")
	}
}

func authenticationEmergencyFixture() (entity.GovernanceSetting, entity.Session, entity.User, entity.UserMFA, time.Time) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	birth := now.Add(-time.Hour)
	factor := now.Add(-time.Minute)
	u := entity.User{ID: "usr_emergency", CreatedAt: birth, Role: entity.RoleAdmin, PasswordHash: "private-verifier"}
	p := entity.GovernanceSetting{ForcedSSOEnabled: true, AuthenticationPolicyRevision: strings.Repeat("a", 64), EmergencyAdminID: u.ID, EmergencyAdminCreatedAt: &birth}
	m := entity.UserMFA{UserID: u.ID, Enabled: true, Generation: strings.Repeat("A", 43)}
	row := entity.Session{ID: "ses_emergency", UserID: u.ID, TokenHash: secret.SHA256Hex(strings.Repeat("s", 43)), PrimaryMethod: "emergency", CreatedAt: factor, ExpiresAt: now.Add(time.Hour), EmergencyPolicyRevision: p.AuthenticationPolicyRevision, EmergencyUserCreatedAt: &birth, EmergencyMFAGeneration: m.Generation, EmergencyPasswordDigest: secret.SHA256Hex(u.PasswordHash), EmergencyFactorCompletedAt: &factor}
	return p, row, u, m, now
}
func TestEmergencyExactCurrentProofAndCanonicalGeneration(t *testing.T) {
	p, row, u, m, now := authenticationEmergencyFixture()
	if !emergencyCurrent(p, row, u, m, now, false) {
		t.Fatal("complete current proof rejected")
	}
	for _, c := range []struct {
		name   string
		mutate func(*entity.GovernanceSetting, *entity.Session, *entity.User, *entity.UserMFA)
	}{
		{"policy_off", func(p *entity.GovernanceSetting, _ *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			p.ForcedSSOEnabled = false
		}},
		{"policy_revision", func(p *entity.GovernanceSetting, _ *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			p.AuthenticationPolicyRevision = strings.Repeat("b", 64)
		}},
		{"designation", func(p *entity.GovernanceSetting, _ *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			p.EmergencyAdminID = "usr_other"
		}},
		{"user_birth", func(_ *entity.GovernanceSetting, _ *entity.Session, u *entity.User, _ *entity.UserMFA) {
			u.CreatedAt = u.CreatedAt.Add(time.Microsecond)
		}},
		{"disabled", func(_ *entity.GovernanceSetting, _ *entity.Session, u *entity.User, _ *entity.UserMFA) {
			u.Disabled = true
		}},
		{"offboarded", func(_ *entity.GovernanceSetting, _ *entity.Session, u *entity.User, _ *entity.UserMFA) {
			v := now
			u.OffboardedAt = &v
		}},
		{"demoted", func(_ *entity.GovernanceSetting, _ *entity.Session, u *entity.User, _ *entity.UserMFA) {
			u.Role = entity.RoleMember
		}},
		{"password", func(_ *entity.GovernanceSetting, _ *entity.Session, u *entity.User, _ *entity.UserMFA) {
			u.PasswordHash += "changed"
		}},
		{"generation", func(_ *entity.GovernanceSetting, _ *entity.Session, _ *entity.User, m *entity.UserMFA) {
			m.Generation = strings.Repeat("E", 43)
		}},
		{"mfa_off", func(_ *entity.GovernanceSetting, _ *entity.Session, _ *entity.User, m *entity.UserMFA) {
			m.Enabled = false
		}},
		{"factor_missing", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			r.EmergencyFactorCompletedAt = nil
		}},
		{"factor_future", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			v := now.Add(time.Second)
			r.EmergencyFactorCompletedAt = &v
		}},
		{"factor_after_creation", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			v := r.CreatedAt.Add(time.Microsecond)
			r.EmergencyFactorCompletedAt = &v
		}},
		{"expired", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			r.ExpiresAt = now
		}},
		{"mixed_oidc", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			r.OIDCBindingID = "oib_mixed"
		}},
		{"mixed_named", func(_ *entity.GovernanceSetting, r *entity.Session, _ *entity.User, _ *entity.UserMFA) {
			r.NamedIdentityProfileID = "github"
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, r, u, m, now := authenticationEmergencyFixture()
			c.mutate(&p, &r, &u, &m)
			if emergencyCurrent(p, r, u, m, now, false) {
				t.Fatal("stale or mixed proof accepted")
			}
		})
	}
	row.EmergencyFactorCompletedAt = nil
	if !emergencyCurrent(p, row, u, m, now, true) || emergencyCurrent(p, row, u, m, now, false) {
		t.Fatal("pending challenge confused with completed Session")
	}
	for _, value := range []string{strings.Repeat("A", 43), strings.Repeat("E", 43)} {
		if !authenticationMFAGeneration(value) {
			t.Fatal("native canonical generation rejected")
		}
	}
	for _, value := range []string{"", strings.Repeat("a", 64), strings.Repeat("A", 42), strings.Repeat("A", 42) + "B", strings.Repeat("A", 43) + "=", strings.Repeat("A", 42) + "+"} {
		if authenticationMFAGeneration(value) {
			t.Fatal("noncanonical generation accepted")
		}
	}
}

func TestAuthenticationSessionComparisonCoversEveryPersistedField(t *testing.T) {
	_, original, _, _, _ := authenticationEmergencyFixture()
	if !authenticationSessionEqual(original, original) {
		t.Fatal("reflexive comparison failed")
	}
	// Reflection only drives independent field perturbations, never production
	// serialization or acceptance. Every future persisted field requires a decision.
	typ := reflect.TypeFor[entity.Session]()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		t.Run(field.Name, func(t *testing.T) {
			changed := original
			v := reflect.ValueOf(&changed).Elem().Field(i)
			switch v.Type() {
			case reflect.TypeFor[string]():
				v.SetString(v.String() + "different")
			case reflect.TypeFor[time.Time]():
				v.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Microsecond)))
			case reflect.TypeFor[*time.Time]():
				now := time.Now().UTC()
				if !v.IsNil() {
					now = v.Interface().(*time.Time).Add(time.Microsecond)
				}
				v.Set(reflect.ValueOf(&now))
			default:
				t.Fatalf("unclassified Session field %s", field.Name)
			}
			if authenticationSessionEqual(original, changed) {
				t.Fatalf("persisted proof field omitted: %s", field.Name)
			}
		})
	}
	sameInstant := original
	instant := original.CreatedAt.In(time.FixedZone("other", 3600))
	sameInstant.CreatedAt = instant
	if !authenticationSessionEqual(original, sameInstant) {
		t.Fatal("instant-equivalent driver location rejected")
	}
}

func TestAuthenticationPolicyReviewBindsCandidateAndIndependentAuthorityFacts(t *testing.T) {
	p, _, u, m, _ := authenticationEmergencyFixture()
	review := authenticationPolicyReview{Policy: p, Candidate: &u, CandidateMFA: m, DesignationUser: &u, DesignationMFA: m, OIDC: entity.OIDCProvider{ID: "oidc"}, LDAP: entity.LDAPProvider{ID: "ldap"}}
	digest := authenticationReviewETag(review)
	for _, c := range []struct {
		name   string
		mutate func(*authenticationPolicyReview)
	}{
		{"candidate_generation", func(r *authenticationPolicyReview) { r.CandidateMFA.Generation = strings.Repeat("E", 43) }},
		{"candidate_birth", func(r *authenticationPolicyReview) {
			v := *r.Candidate
			v.CreatedAt = v.CreatedAt.Add(time.Microsecond)
			r.Candidate = &v
		}},
		{"candidate_absent", func(r *authenticationPolicyReview) { r.Candidate = nil }},
		{"registration", func(r *authenticationPolicyReview) { r.Policy.RegistrationPolicyRevision = strings.Repeat("c", 64) }},
		{"oidc_policy", func(r *authenticationPolicyReview) { r.OIDC.PolicyRevision = strings.Repeat("d", 64) }},
		{"ldap_verified", func(r *authenticationPolicyReview) { r.LDAP.VerifiedConfigRevision = strings.Repeat("e", 64) }},
		{"stored_mfa", func(r *authenticationPolicyReview) { r.DesignationMFA.Enabled = false }},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := review
			c.mutate(&r)
			if authenticationReviewETag(r) == digest {
				t.Fatal("review authority tuple not committed")
			}
		})
	}
}

func TestAuthenticationPolicyStrictInputsAndNoSecretProjection(t *testing.T) {
	good := `{"enforced":false,"emergency_admin_id":"","reason":"Reviewed"}`
	var input AuthenticationPolicyInput
	if json.Unmarshal([]byte(good), &input) != nil {
		t.Fatal("valid disable rejected")
	}
	for _, raw := range []string{
		`{"enforced":null,"emergency_admin_id":"","reason":"Reviewed"}`,
		`{"enforced":false,"enforced":false,"emergency_admin_id":"","reason":"Reviewed"}`,
		`{"enforced":false,"emergency_admin_id":"","reason":"Reviewed","extra":true}`,
		`{"enforced":false,"emergency_admin_id":"usr_other","reason":"Reviewed"}`,
		`{"enforced":true,"emergency_admin_id":"","reason":"Reviewed"}`,
		`{"enforced":false,"emergency_admin_id":"","reason":"\ud800"}`,
	} {
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("ambiguous policy input accepted")
		}
	}
	var validProof EmergencyProofInput
	if json.Unmarshal([]byte(`{"challenge_token":"`+strings.Repeat("A", 43)+`","recovery_code":"one-time"}`), &validProof) != nil || validProof.RecoveryCode != "one-time" {
		t.Fatal("valid proof schema rejected")
	}
	for _, raw := range []string{
		`{"challenge_token":"` + strings.Repeat("A", 43) + `","code":"123456","recovery_code":"one-time"}`,
		`{"challenge_token":"` + strings.Repeat("A", 43) + `","code":"123456","code":"123456"}`,
		`{"challenge_token":"` + strings.Repeat("A", 43) + `","code":null}`,
	} {
		var v EmergencyProofInput
		if json.Unmarshal([]byte(raw), &v) == nil {
			t.Fatal("ambiguous factor accepted")
		}
	}
	raw, e := json.Marshal(AuthenticationPolicyPublic{Enforced: true, EmergencyEntryEnabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if string(raw) != `{"enforced":true,"password_login_enabled":false,"emergency_entry_enabled":true}` {
		t.Fatal("public projection changed")
	}
}

func TestAuthenticationPolicyRuntimeHeldSnapshotAndFailedRefreshFence(t *testing.T) {
	s, data, cookie, bearer := teamSessionFixture(t)
	data.TeamSessionData.AuthenticationPolicy = entity.GovernanceSetting{AuthenticationPolicyRevision: strings.Repeat("0", 64)}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	identity, e := s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one")
	if e != nil {
		t.Fatal(e)
	}
	// A commit fences the held snapshot before any database refresh can succeed.
	on := runtimeAuthenticationPolicy{Enforced: true, Revision: strings.Repeat("a", 64), EmergencyAdminID: "usr_admin"}
	s.runtime.authenticationPolicyFence.Store(&on)
	if s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one") == nil {
		t.Fatal("held local pre-dispatch survived policy commit")
	}
	if _, e = s.AuthenticateAPIKey(context.Background(), bearer); e != nil {
		t.Fatal("control-plane policy changed independent Key authority", e)
	}
	off := runtimeAuthenticationPolicy{Revision: strings.Repeat("b", 64)}
	s.runtime.authenticationPolicyFence.Store(&off)
	if s.ReauthorizeTeamSession(context.Background(), identity, "mdl_one") == nil {
		t.Fatal("off-on ABA revived the older local snapshot")
	}
	// Only a newly built snapshot of the actual off revision restores admission.
	data.TeamSessionData.AuthenticationPolicy = entity.GovernanceSetting{AuthenticationPolicyRevision: off.Revision}
	s.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(time.Minute)))
	if _, e = s.RuntimeAuthenticateTeamSession(context.Background(), cookie, "tem_one"); e != nil {
		t.Fatal(e)
	}
	old := s.runtime.auth.Load()
	row := old.TeamSessions[secret.SHA256Hex(cookie)]
	row.PrimaryMethod = "oidc"
	if s.runtimeAuthenticationPolicyDenied(old, row) {
		t.Fatal("policy fence supplanted independent OIDC validation")
	}
	row.PrimaryMethod = "ldap"
	if s.runtimeAuthenticationPolicyDenied(old, row) {
		t.Fatal("policy fence supplanted independent LDAP validation")
	}
}
func TestAuthenticationPolicyRuntimeLoaderHasNoOffFallback(t *testing.T) {
	s, _, _, state := providerMetadataSQLService(t)
	data, e := loadTeamSessionRuntimeData(s.authDB(context.Background()))
	if e != nil {
		t.Fatal(e)
	}
	if data.AuthenticationPolicy.ForcedSSOEnabled || data.AuthenticationPolicy.AuthenticationPolicyRevision != strings.Repeat("0", 64) {
		t.Fatal("actual initial governance row was not loaded")
	}
	state.base.failTable = "governance_settings"
	if data, e = loadTeamSessionRuntimeData(s.authDB(context.Background())); e == nil || data != nil {
		t.Fatal("failed governance read fell back to off")
	}
}

func TestAuthenticationPolicyActualRefreshCannotEraseNewerDenyFence(t *testing.T) {
	s, _, _, state := providerMetadataSQLService(t)
	if e := s.RefreshRuntime(context.Background()); e != nil {
		t.Fatal("initial actual-policy publication", e)
	}
	if a := s.runtime.auth.Load(); a == nil || a.AuthenticationPolicy.Revision != strings.Repeat("0", 64) {
		t.Fatal("startup did not publish actual initial policy")
	}
	committed := &runtimeAuthenticationPolicy{Enforced: true, Revision: strings.Repeat("d", 64), EmergencyAdminID: "usr_admin"}
	s.runtime.authenticationPolicyFence.Store(committed)
	// Model the old committed database snapshot; refresh must not infer that its
	// authorization tuple may erase a later deny fence, even if projection succeeds.
	if e := s.RefreshRuntime(context.Background()); e != nil {
		t.Fatal(e)
	}
	if s.runtime.authenticationPolicyFence.Load() != committed {
		t.Fatal("refresh overwrote newer policy fence")
	}
	row := runtimeTeamSession{PrimaryMethod: "", UserID: "usr_admin"}
	if !s.runtimeAuthenticationPolicyDenied(s.runtime.auth.Load(), row) {
		t.Fatal("older off publication revived local Session")
	}
	state.base.failTable = "governance_settings"
	if s.RefreshRuntime(context.Background()) == nil {
		t.Fatal("policy outage published an off fallback")
	}
	if s.runtime.authenticationPolicyFence.Load() != committed || !s.runtimeAuthenticationPolicyDenied(s.runtime.auth.Load(), row) {
		t.Fatal("failed publication removed current denial")
	}
}

// Proof-shape fixtures must first load the mandatory stored policy. Nil is not off.
func authenticationProofPolicyDB(t *testing.T) *gorm.DB {
	t.Helper()
	s, _, _, _ := providerMetadataSQLService(t)
	tx := s.authDB(context.Background())
	policy, err := authenticationPolicy(tx)
	if err != nil || policy.ForcedSSOEnabled || policy.AuthenticationPolicyRevision != strings.Repeat("0", 64) {
		t.Fatal("stored off-policy fixture unavailable", err)
	}
	return tx
}

func TestAuthenticationPolicyMissingDatabaseNeverAdmitsLocalProof(t *testing.T) {
	if _, err := authenticationPolicy(nil); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("missing policy did not fail closed", err)
	}
	if err := primaryValidateSession(nil, entity.Session{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("missing policy admitted a local Session", err)
	}
	if err := primaryValidateChallenge(nil, entity.MFAChallenge{}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatal("missing policy admitted a local challenge", err)
	}
}
