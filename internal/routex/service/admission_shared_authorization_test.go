package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
)

func sharedAuthorizationTestTime() time.Time {
	return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
}

func sharedAuthorizationTestFacts(kind string) sharedAuthorizationFacts {
	at := sharedAuthorizationTestTime()
	birth := at.Add(-time.Hour)
	revision := strings.Repeat("a", 64)
	u := &entity.User{ID: "usr_shared", CreatedAt: birth, PersonalGrantRevision: revision, Role: entity.RoleMember, PasswordHash: "test-private-password-hash"}
	f := sharedAuthorizationFacts{Kind: kind, Model: entity.Model{ID: "mdl_shared", CreatedAt: birth, Status: entity.ResourceActive}}
	if kind != "project_key" {
		f.User = u
		f.Admission = &runtimeAdmissionProof{CreatedAt: birth, State: "not_required", Eligible: true}
	}
	if kind == "personal_key" || kind == "personal_session" {
		f.PersonalRows = []entity.UserModelGrant{{UserID: u.ID, ModelID: f.Model.ID, CreatedAt: birth}}
		f.PersonalGrants = &runtimePersonalGrantState{CreatedAt: birth, Enabled: true, Revision: revision, Hash: personalGrantHash(f.PersonalRows)}
	}
	if kind == "personal_key" {
		expires := at.Add(time.Hour)
		f.PersonalKey = &entity.APIKey{ID: "key_shared", UserID: u.ID, CreatedAt: birth, Status: entity.KeyActive, ExpiresAt: &expires, LifecycleRevision: "kvr_01arz3ndektsv4rrffq69g5fav", TokenHash: "test-private-key-verifier", Name: "test-private-key-name"}
		f.KeyModels = []string{f.Model.ID}
	}
	if kind == "project_key" {
		f.Project = &entity.Project{ID: "prj_shared", Status: entity.ResourceActive, CreatedAt: birth, CreatorID: "usr_not_owner", Name: "test-private-project-name"}
		f.ProjectKey = &entity.ProjectKey{ID: "pky_shared", ProjectID: f.Project.ID, CreatorID: "usr_not_owner", CreatedAt: birth, Status: entity.KeyActive, TokenHash: "test-private-project-key-verifier"}
		f.ProjectGrant = &entity.ProjectModelGrant{ProjectID: f.Project.ID, ModelID: f.Model.ID}
		f.KeyModels = []string{f.Model.ID}
	}
	if kind == "personal_session" || kind == "team_session" {
		f.Session = &entity.Session{ID: "ses_shared", UserID: u.ID, CreatedAt: birth, ExpiresAt: at.Add(time.Hour), TokenHash: "test-private-session-verifier"}
		f.Policy = &entity.GovernanceSetting{ID: 1, AuthenticationPolicyRevision: revision}
	}
	if kind == "team_session" {
		f.Team = &entity.Team{ID: "tem_shared", Status: entity.ResourceActive, CreatedAt: birth, Name: "test-private-team-name"}
		f.Membership = &entity.TeamMembership{ID: "tmb_shared", TeamID: f.Team.ID, UserID: u.ID, Status: entity.ResourceActive, Role: entity.TeamMember}
		f.TeamGrant = &entity.TeamModelGrant{TeamID: f.Team.ID, ModelID: f.Model.ID}
	}
	return f
}

func sharedAuthorizationTestPrimary(f *sharedAuthorizationFacts, method string) {
	s, birth, revision := f.Session, f.User.CreatedAt, strings.Repeat("b", 64)
	s.PrimaryMethod = method
	switch method {
	case "oidc":
		s.OIDCBindingID, s.OIDCBindingCreatedAt, s.OIDCConfigRevision, s.OIDCPolicyRevision, s.OIDCUserCreatedAt = "oib_shared", &birth, revision, revision, &birth
	case "oauth":
		s.OAuthBindingID, s.OAuthBindingCreatedAt, s.OAuthConfigRevision, s.OAuthPolicyRevision, s.OAuthUserCreatedAt = "oab_shared", &birth, revision, revision, &birth
	case "ldap":
		s.LDAPBindingID, s.LDAPBindingCreatedAt, s.LDAPConfigRevision, s.LDAPPolicyRevision, s.LDAPUserCreatedAt = "ldb_shared", &birth, revision, revision, &birth
	case "saml":
		s.SAMLBindingID, s.SAMLBindingCreatedAt, s.SAMLConfigRevision, s.SAMLPolicyRevision, s.SAMLUserCreatedAt = "sab_shared", &birth, revision, revision, &birth
	case "github", "google", "discord", "telegram":
		s.NamedIdentityProviderID, s.NamedIdentityProfileID = method, namedIdentityProfileID(method)
		s.NamedIdentityBindingID, s.NamedIdentityBindingCreatedAt, s.NamedIdentityConfigRevision, s.NamedIdentityPolicyRevision, s.NamedIdentityUserCreatedAt = "nib_shared", &birth, revision, revision, &birth
	case "emergency":
		f.User.Role = entity.RoleAdmin
		f.Policy.ForcedSSOEnabled, f.Policy.EmergencyAdminID, f.Policy.EmergencyAdminCreatedAt = true, f.User.ID, &birth
		generation := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
		f.MFA = &entity.UserMFA{UserID: f.User.ID, Enabled: true, Generation: generation}
		s.EmergencyPolicyRevision, s.EmergencyUserCreatedAt, s.EmergencyMFAGeneration, s.EmergencyFactorCompletedAt = f.Policy.AuthenticationPolicyRevision, &birth, generation, &birth
		s.EmergencyPasswordDigest = secret.SHA256Hex(f.User.PasswordHash)
	}
}

func TestSharedAuthorizationFourPrincipalsAndSecretFreeProjection(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			facts := sharedAuthorizationTestFacts(kind)
			proof, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime())
			if err != nil || !sharedAuthorizationValid(proof) {
				t.Fatalf("capture %s: %v", kind, err)
			}
			call := CallFact{ModelID: "mdl_shared"}
			if proof.User != nil {
				call.UserID = proof.User.ID
			}
			if proof.Key != nil {
				call.KeyID = proof.Key.ID
			}
			if proof.Project != nil {
				call.ProjectID = proof.Project.ID
			}
			if proof.Team != nil {
				call.TeamID, call.TeamMembershipID = proof.Team.ID, proof.Team.MembershipID
			}
			if !proof.matchesCall(call) {
				t.Fatal("exact original principal did not match call scope")
			}
			call.ModelID = "mdl_other"
			if proof.matchesCall(call) {
				t.Fatal("another Model matched")
			}
			raw, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			for _, excluded := range []string{"test-private-", "usr_not_owner", "TokenHash", "PasswordHash", "SubjectDigest", "csrf"} {
				if strings.Contains(string(raw), excluded) {
					t.Fatalf("projection exposed excluded field %s", excluded)
				}
			}
			if kind == "project_key" && (proof.User != nil || proof.Key.LifecycleRevision != "") {
				t.Fatal("Project Key invented a User owner or lifecycle revision")
			}
			if kind == "team_session" && proof.Team.JoinedAt != nil {
				t.Fatal("legacy missing membership instant was fabricated")
			}
		})
	}
}

func TestSharedAuthorizationPrimaryFamiliesAndEmergencyRedaction(t *testing.T) {
	for _, method := range []string{"", "oidc", "oauth", "ldap", "saml", "github", "google", "discord", "telegram", "emergency"} {
		t.Run("method_"+method, func(t *testing.T) {
			facts := sharedAuthorizationTestFacts("personal_session")
			sharedAuthorizationTestPrimary(&facts, method)
			proof, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime())
			if err != nil || proof.Session.Primary.Method != method {
				t.Fatalf("primary family rejected: %v", err)
			}
			raw, err := json.Marshal(proof)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), "test-private-") || strings.Contains(string(raw), "PasswordDigest") || method == "emergency" && strings.Contains(string(raw), facts.Session.EmergencyPasswordDigest) {
				t.Fatal("primary proof persisted password or bearer-derived material")
			}
			if namedIdentityMethod(method) && proof.Session.Primary.Binding.ProfileID != namedIdentityProfileID(method) {
				t.Fatal("named profile namespace lost")
			}
		})
	}
}

func TestSharedAuthorizationForcedSSOUsesExistingPolicy(t *testing.T) {
	for _, method := range []string{"oidc", "ldap"} {
		t.Run(method, func(t *testing.T) {
			facts := sharedAuthorizationTestFacts("team_session")
			sharedAuthorizationTestPrimary(&facts, method)
			birth := facts.User.CreatedAt
			facts.Policy.ForcedSSOEnabled = true
			facts.Policy.EmergencyAdminID, facts.Policy.EmergencyAdminCreatedAt = "usr_emergency", &birth
			proof, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime())
			if err != nil || !proof.Session.ForcedSSO || proof.Session.Primary.Method != method {
				t.Fatalf("valid forced-SSO capture rejected: %v", err)
			}
			facts.Policy.EmergencyAdminCreatedAt = nil
			if _, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime()); err == nil {
				t.Fatal("malformed forced-SSO policy admitted")
			}
		})
	}
	facts := sharedAuthorizationTestFacts("personal_session")
	facts.Policy.EmergencyAdminID = "usr_emergency"
	if _, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime()); err == nil {
		t.Fatal("disabled policy with retained emergency administrator admitted")
	}
}

func TestSharedAuthorizationRejectsInconsistentCapturedFacts(t *testing.T) {
	cases := []struct {
		name, kind string
		change     func(*sharedAuthorizationFacts)
	}{
		{"wrong_key_owner", "personal_key", func(f *sharedAuthorizationFacts) { f.PersonalKey.UserID = "usr_other" }},
		{"wrong_project_owner", "project_key", func(f *sharedAuthorizationFacts) { f.ProjectKey.ProjectID = "prj_other" }},
		{"wrong_project_grant", "project_key", func(f *sharedAuthorizationFacts) { f.ProjectGrant.ModelID = "mdl_other" }},
		{"missing_key_scope", "personal_key", func(f *sharedAuthorizationFacts) { f.KeyModels = []string{"mdl_other"} }},
		{"wrong_personal_grant_owner", "personal_session", func(f *sharedAuthorizationFacts) {
			f.PersonalRows[0].UserID = "usr_other"
			f.PersonalGrants.Hash = personalGrantHash(f.PersonalRows)
		}},
		{"missing_personal_model", "personal_session", func(f *sharedAuthorizationFacts) {
			f.PersonalRows[0].ModelID = "mdl_other"
			f.PersonalGrants.Hash = personalGrantHash(f.PersonalRows)
		}},
		{"wrong_user_birth", "personal_key", func(f *sharedAuthorizationFacts) { f.User.CreatedAt = f.User.CreatedAt.Add(time.Second) }},
		{"wrong_grant_birth", "personal_key", func(f *sharedAuthorizationFacts) {
			f.PersonalGrants.CreatedAt = f.PersonalGrants.CreatedAt.Add(time.Second)
		}},
		{"future_model_birth", "project_key", func(f *sharedAuthorizationFacts) {
			f.Model.CreatedAt = sharedAuthorizationTestTime().Add(time.Nanosecond)
		}},
		{"disabled_user", "personal_session", func(f *sharedAuthorizationFacts) { f.User.Disabled = true }},
		{"archived_model", "team_session", func(f *sharedAuthorizationFacts) { f.Model.Status = entity.ResourceArchived }},
		{"wrong_session_owner", "personal_session", func(f *sharedAuthorizationFacts) { f.Session.UserID = "usr_other" }},
		{"wrong_team_membership_owner", "team_session", func(f *sharedAuthorizationFacts) { f.Membership.UserID = "usr_other" }},
		{"wrong_team_grant", "team_session", func(f *sharedAuthorizationFacts) { f.TeamGrant.TeamID = "tem_other" }},
		{"mixed_primary_families", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "oidc")
			sharedAuthorizationTestPrimary(f, "google")
		}},
		{"wrong_primary_user_birth", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "oidc")
			b := f.User.CreatedAt.Add(time.Second)
			f.Session.OIDCUserCreatedAt = &b
		}},
		{"missing_primary_config", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "ldap")
			f.Session.LDAPConfigRevision = ""
		}},
		{"foreign_named_profile", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "google")
			f.Session.NamedIdentityProfileID = namedIdentityProfileID("github")
		}},
		{"partial_local_binding", "personal_session", func(f *sharedAuthorizationFacts) { f.Session.OIDCPolicyRevision = strings.Repeat("c", 64) }},
		{"forced_sso_rejects_local", "personal_session", func(f *sharedAuthorizationFacts) { f.Policy.ForcedSSOEnabled = true }},
		{"forced_sso_rejects_named", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "google")
			f.Policy.ForcedSSOEnabled = true
		}},
		{"emergency_password_changed", "personal_session", func(f *sharedAuthorizationFacts) {
			sharedAuthorizationTestPrimary(f, "emergency")
			f.User.PasswordHash = "another-password-hash"
		}},
		{"mixed_key_session", "personal_key", func(f *sharedAuthorizationFacts) { f.Session = &entity.Session{} }},
		{"invented_project_user", "project_key", func(f *sharedAuthorizationFacts) { f.User = &entity.User{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := sharedAuthorizationTestFacts(tc.kind)
			tc.change(&facts)
			if _, err := newSharedCapturedAuthorization(facts, sharedAuthorizationTestTime()); !errors.Is(err, errSharedAdmissionInvalid) {
				t.Fatalf("invalid facts admitted: %v", err)
			}
		})
	}
}

func TestSharedAuthorizationExplicitEvaluationAndNullableExpiry(t *testing.T) {
	at := sharedAuthorizationTestTime()
	facts := sharedAuthorizationTestFacts("personal_key")
	facts.PersonalKey.ExpiresAt = &at
	if _, err := newSharedCapturedAuthorization(facts, at); err == nil {
		t.Fatal("expiry boundary admitted")
	}
	if _, err := newSharedCapturedAuthorization(facts, at.Add(-time.Nanosecond)); err != nil {
		t.Fatalf("explicit earlier evaluation changed: %v", err)
	}
	facts.PersonalKey.ExpiresAt = nil
	proof, err := newSharedCapturedAuthorization(facts, at)
	if err != nil || proof.Key.ExpiresAt != nil {
		t.Fatal("nullable expiry fabricated")
	}
	if _, err := newSharedCapturedAuthorization(facts, time.Time{}); err == nil {
		t.Fatal("missing evaluation instant manufactured")
	}
	facts = sharedAuthorizationTestFacts("team_session")
	facts.Session.ExpiresAt = at
	if _, err := newSharedCapturedAuthorization(facts, at); err == nil {
		t.Fatal("expired Session admitted")
	}
}

func TestSharedAuthorizationRegistrationAndDetachedCopies(t *testing.T) {
	at := sharedAuthorizationTestTime()
	facts := sharedAuthorizationTestFacts("personal_session")
	application := "raa_01arz3ndektsv4rrffq69g5fav"
	facts.User.ApprovalApplicationID = &application
	facts.Admission.State, facts.Admission.ApplicationID, facts.Admission.ApplicationCreatedAt, facts.Admission.Revision = "approved", application, facts.User.CreatedAt, strings.Repeat("d", 64)
	sharedAuthorizationTestPrimary(&facts, "oidc")
	proof, err := newSharedCapturedAuthorization(facts, at.In(time.FixedZone("capture", 8*3600)))
	if err != nil {
		t.Fatal(err)
	}
	original := sharedTestCopy(proof)
	*facts.User.ApprovalApplicationID = "raa_changed"
	*facts.Session.OIDCBindingCreatedAt = at.Add(time.Hour)
	facts.Session.OIDCConfigRevision = "changed"
	facts.Admission.ApplicationCreatedAt = at.Add(time.Hour)
	facts.PersonalGrants.Hash = "changed"
	if !reflect.DeepEqual(proof, original) || proof.EvaluatedAt.Location() != time.UTC {
		t.Fatal("input aliases or time location changed captured proof")
	}
	for _, state := range []string{"pending", "rejected", "unknown"} {
		changed := sharedTestCopy(original)
		changed.User.AdmissionState = state
		if sharedAuthorizationValid(changed) {
			t.Fatal("nonadmitted registration state accepted", state)
		}
	}
	team := sharedAuthorizationTestFacts("team_session")
	joined := team.Team.CreatedAt
	request, creation := "tmr_shared", "29f720d7-21cd-4f86-8e71-7d53da56f0a1"
	team.Membership.JoinedAt, team.TeamGrant.SourceRequestID, team.TeamGrant.SourceCreationReceiptID = &joined, &request, &creation
	teamProof, err := newSharedCapturedAuthorization(team, at)
	if err != nil {
		t.Fatal(err)
	}
	joined, request, creation = at, "changed", "changed"
	if teamProof.Team.JoinedAt.Equal(at) || *teamProof.Team.GrantRequestID != "tmr_shared" || *teamProof.Team.GrantCreationReceipt != "29f720d7-21cd-4f86-8e71-7d53da56f0a1" {
		t.Fatal("Team input pointers alias capture")
	}
	key := sharedAuthorizationTestFacts("personal_key")
	keyProof, err := newSharedCapturedAuthorization(key, at)
	if err != nil {
		t.Fatal(err)
	}
	*key.PersonalKey.ExpiresAt = at
	if !keyProof.Key.ExpiresAt.After(at) {
		t.Fatal("Key expiry input aliases capture")
	}
}

func TestSharedAuthorizationCoordinatorScopeAndDigestBinding(t *testing.T) {
	c, spool, authority, request := sharedTestFixture(t)
	owner := c.intent.Owner
	for _, change := range []func(*sharedCapturedAuthorization){
		func(a *sharedCapturedAuthorization) { a.Version = 2 },
		func(a *sharedCapturedAuthorization) { a.User.ID = "usr_other" },
		func(a *sharedCapturedAuthorization) { a.Key.ID = "key_other" },
		func(a *sharedCapturedAuthorization) { a.Model.ID = "mdl_other" },
		func(a *sharedCapturedAuthorization) {
			a.Project = &sharedAuthorizationBirth{ID: "prj_other", CreatedAt: a.EvaluatedAt}
		},
	} {
		changed := sharedTestCopy(request)
		change(&changed.Authorization)
		if _, err := newSharedAdmissionCoordinator(owner, changed, spool, authority, sharedCoordinatorOptions{OperationTimeout: time.Second}); !errors.Is(err, errSharedAdmissionInvalid) {
			t.Fatalf("conflicting scope accepted: %v", err)
		}
	}
	original := sharedTestCopy(c.intent)
	changed := sharedTestCopy(original)
	changed.Authorization.PersonalGrants.Revision = strings.Repeat("f", 64)
	if sharedHasDigest(changed, original.Digest) {
		t.Fatal("changed durable grant revision matched original digest")
	}
	changed = sharedTestCopy(original)
	changed.Authorization.Key.CreatedAt = changed.Authorization.Key.CreatedAt.Add(time.Nanosecond)
	if sharedHasDigest(changed, original.Digest) {
		t.Fatal("changed Key birth matched original digest")
	}
	request.Authorization.User.ID = "usr_other"
	request.Authorization.Key.ExpiresAt = nil
	if err := c.Admit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(spool.record.Intent.Authorization, original.Authorization) {
		t.Fatal("caller mutation changed durable original authorization")
	}
}
