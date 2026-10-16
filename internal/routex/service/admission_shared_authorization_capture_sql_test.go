package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/secret"
	"gorm.io/gorm"
)

// SQL is a read-only fixture here; the transient identity comes from actual
// runtime projection/authentication, never a successful collector callback.
func sharedAuthorizationCaptureSQLFixture(t *testing.T, kind string) (*gorm.DB, *sharedAuthorizationSQLFixture, gatewayIdentity, time.Time) {
	t.Helper()
	db, f, _ := sharedAuthorizationSQLTestDB(t, kind)
	now := time.Now().UTC()
	// Runtime authentication uses the live clock. Keep every locked fixture
	// birth before that clock instead of mixing static future births with it.
	birth := now.Add(-time.Hour)
	f.facts.Model.CreatedAt = birth
	if f.facts.User != nil {
		f.facts.User.CreatedAt, f.facts.Admission.CreatedAt = birth, birth
	}
	if f.facts.PersonalGrants != nil {
		for i := range f.facts.PersonalRows {
			f.facts.PersonalRows[i].CreatedAt = birth
		}
		f.facts.PersonalGrants.CreatedAt, f.facts.PersonalGrants.Hash = birth, personalGrantHash(f.facts.PersonalRows)
	}
	if f.facts.PersonalKey != nil {
		f.facts.PersonalKey.CreatedAt = birth
	}
	if f.facts.Project != nil {
		f.facts.Project.CreatedAt, f.facts.ProjectKey.CreatedAt, f.managerUser.CreatedAt = birth, birth, birth
	}
	if f.facts.Session != nil {
		f.facts.Session.CreatedAt = birth
	}
	if f.facts.Team != nil {
		f.facts.Team.CreatedAt = birth
	}
	expires := now.Add(time.Hour)
	raw := strings.Repeat("c", 43)
	data := &runtimeData{Models: []entity.Model{f.facts.Model}, Grants: f.facts.PersonalRows}
	if f.facts.User != nil {
		data.Users = []entity.User{*f.facts.User}
	}
	if f.facts.PersonalKey != nil {
		raw = "rx_" + raw
		f.facts.PersonalKey.TokenHash, f.facts.PersonalKey.ExpiresAt = secret.SHA256Hex(raw), &expires
		data.Keys = []entity.APIKey{*f.facts.PersonalKey}
		data.Scopes = []entity.APIKeyModel{{KeyID: f.facts.PersonalKey.ID, ModelID: f.facts.Model.ID}}
	}
	if f.facts.ProjectKey != nil {
		raw = "rxp_" + raw
		f.facts.ProjectKey.TokenHash, f.facts.ProjectKey.ExpiresAt = secret.SHA256Hex(raw), &expires
		data.Users = []entity.User{f.managerUser}
		data.ProjectData = &projectRuntimeData{Projects: []entity.Project{*f.facts.Project}, Managers: f.managers,
			Keys: []entity.ProjectKey{*f.facts.ProjectKey}, Scopes: []entity.ProjectKeyModel{{KeyID: f.facts.ProjectKey.ID, ModelID: f.facts.Model.ID}},
			Grants: []entity.ProjectModelGrant{*f.facts.ProjectGrant}}
	}
	if f.facts.Session != nil {
		f.facts.Session.TokenHash, f.facts.Session.ExpiresAt = secret.SHA256Hex(raw), expires
		data.TeamSessionData = &teamSessionRuntimeData{Sessions: []entity.Session{*f.facts.Session}, AuthenticationPolicy: *f.facts.Policy}
		if f.facts.Team != nil {
			data.TeamSessionData.Teams = []entity.Team{*f.facts.Team}
			data.TeamSessionData.Memberships = []entity.TeamMembership{*f.facts.Membership}
			data.TeamSessionData.Grants = []entity.TeamModelGrant{*f.facts.TeamGrant}
		}
	}
	svc := &Service{runtime: &gatewayRuntime{}}
	svc.runtime.auth.Store(buildRuntimeAuthorization(data, time.Now().Add(runtimeAuthorizationLease)))
	identity := sharedAuthorizationCaptureAuthenticate(t, svc, kind, raw, f.facts)
	return db, f, identity, now
}

func sharedAuthorizationCaptureAuthenticate(t *testing.T, svc *Service, kind, raw string, f sharedAuthorizationFacts) gatewayIdentity {
	t.Helper()
	identity := gatewayIdentity{selectedModelID: f.Model.ID}
	var err error
	switch kind {
	case "personal_key", "project_key":
		identity.key, err = svc.authenticateRuntimeKey(raw)
		if f.PersonalGrants != nil {
			identity.personalGrantRevision = f.PersonalGrants.Revision
		}
	case "personal_session":
		identity.personal, err = svc.RuntimeAuthenticatePersonalSession(t.Context(), raw)
	case "team_session":
		identity.team, err = svc.RuntimeAuthenticateTeamSession(t.Context(), raw, f.Team.ID)
	default:
		t.Fatal("unsupported test principal")
	}
	if err != nil {
		t.Fatal("production runtime authentication", err)
	}
	return identity
}

func sharedAuthorizationCaptureInTransaction(db *gorm.DB, identity gatewayIdentity, now time.Time) (sharedCapturedAuthorization, error) {
	var captured sharedCapturedAuthorization
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		captured, err = captureSharedAuthorizationSQL(tx, identity, now)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return captured, err
}

func TestSharedAuthorizationInitialCaptureFourPrincipalsAndExactLoaderReuse(t *testing.T) {
	for _, kind := range []string{"personal_key", "project_key", "personal_session", "team_session"} {
		t.Run(kind, func(t *testing.T) {
			db, f, identity, now := sharedAuthorizationCaptureSQLFixture(t, kind)
			captured, err := sharedAuthorizationCaptureInTransaction(db, identity, now)
			if err != nil {
				t.Fatal("initial current SQL capture", err)
			}
			want, err := newSharedCapturedAuthorization(f.facts, now)
			if err != nil || !reflect.DeepEqual(captured, want) {
				t.Fatal("initial capture must project actual locked facts", err)
			}
			captureQueries := append([]string(nil), f.queries...)
			f.queries = nil
			if err := db.Transaction(func(tx *gorm.DB) error {
				return validateSharedAuthorizationSQL(tx, captured, now)
			}, &sql.TxOptions{Isolation: sql.LevelReadCommitted}); err != nil {
				t.Fatal("captured proof must pass unchanged current validator", err)
			}
			if !reflect.DeepEqual(captureQueries, f.queries) {
				t.Fatal("initial collector and current validator diverged in query/lock sequence")
			}
			raw, err := json.Marshal(captured)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{secret.SHA256Hex(strings.Repeat("c", 43)), secret.SHA256Hex("rx_" + strings.Repeat("c", 43)), secret.SHA256Hex("rxp_" + strings.Repeat("c", 43)), "test-private-password-hash", "test-private-key-name", "TokenHash", "PasswordHash"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("captured durable projection leaked transient or private material")
				}
			}
			if kind == "project_key" {
				if identity.key.Key.UserID != "" || captured.User != nil || captured.Project.ID != f.facts.ProjectKey.ProjectID || captured.Key.LifecycleRevision != "" {
					t.Fatal("neutral Project metadata fabricated User or Personal lifecycle authority")
				}
			}
		})
	}
}

func TestSharedAuthorizationInitialCaptureRequiresTransientProofAndTransaction(t *testing.T) {
	db, f, identity, now := sharedAuthorizationCaptureSQLFixture(t, "personal_key")
	for _, input := range []gatewayIdentity{
		{},
		{selectedModelID: identity.selectedModelID},
		{selectedModelID: identity.selectedModelID, key: &KeyRecord{Key: entity.APIKey{ID: identity.key.Key.ID, UserID: identity.key.Key.UserID, CreatedAt: identity.key.Key.CreatedAt}, ModelIDs: identity.key.ModelIDs}},
		{selectedModelID: identity.selectedModelID, key: identity.key, personal: &PersonalSessionIdentity{}},
		{selectedModelID: "", key: identity.key},
	} {
		f.queries = nil
		if got, err := sharedAuthorizationCaptureInTransaction(db, input, now); err == nil || !reflect.DeepEqual(got, sharedCapturedAuthorization{}) || len(f.queries) != 0 {
			t.Fatal("locator-only or ambiguous input reached authority reads", err)
		}
	}
	f.queries = nil
	if _, err := captureSharedAuthorizationSQL(db, identity, now); !errors.Is(err, errSharedAdmissionInvalid) || len(f.queries) != 0 {
		t.Fatal("initial capture accepted an unlocked nontransactional database", err)
	}
	if _, err := captureSharedAuthorizationSQL(nil, identity, now); !errors.Is(err, errSharedAdmissionInvalid) {
		t.Fatal("initial capture accepted a nil transaction", err)
	}
	if _, err := sharedAuthorizationCaptureInTransaction(db, identity, time.Time{}); !errors.Is(err, errSharedAdmissionInvalid) {
		t.Fatal("initial capture accepted an absent evaluation instant", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := sharedAuthorizationCaptureInTransaction(db.WithContext(cancelled), identity, now); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled initial capture returned a proof", err)
	}
}

func TestSharedAuthorizationInitialCaptureRejectsChangedLockedFacts(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		change     func(*sharedAuthorizationSQLFixture)
	}{
		{"personal_verifier", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalKey.TokenHash = strings.Repeat("d", 64) }},
		{"personal_birth", "personal_key", func(f *sharedAuthorizationSQLFixture) {
			f.facts.PersonalKey.CreatedAt = f.facts.PersonalKey.CreatedAt.Add(time.Second)
		}},
		{"personal_lifecycle", "personal_key", func(f *sharedAuthorizationSQLFixture) {
			f.facts.PersonalKey.LifecycleRevision = "kvr_01arz3ndektsv4rrffq69g5faz"
		}},
		{"project_verifier", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.ProjectKey.TokenHash = strings.Repeat("d", 64) }},
		{"project_birth", "project_key", func(f *sharedAuthorizationSQLFixture) {
			f.facts.ProjectKey.CreatedAt = f.facts.ProjectKey.CreatedAt.Add(time.Second)
		}},
		{"project_owner", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.ProjectKey.ProjectID = "prj_other" }},
		{"personal_session_verifier", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Session.TokenHash = strings.Repeat("d", 64) }},
		{"personal_session_birth", "personal_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.Session.CreatedAt = f.facts.Session.CreatedAt.Add(time.Second)
		}},
		{"personal_session_expiry", "personal_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.Session.ExpiresAt = f.facts.Session.ExpiresAt.Add(time.Second)
		}},
		{"personal_user_birth", "personal_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.User.CreatedAt = f.facts.User.CreatedAt.Add(time.Second)
		}},
		{"personal_grant_generation", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.User.PersonalGrantRevision = strings.Repeat("d", 64) }},
		{"team_session_verifier", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Session.TokenHash = strings.Repeat("d", 64) }},
		{"team_birth", "team_session", func(f *sharedAuthorizationSQLFixture) {
			f.facts.Team.CreatedAt = f.facts.Team.CreatedAt.Add(time.Second)
		}},
		{"team_member_identity", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.ID = "tmb_other" }},
		{"team_member_disabled", "team_session", func(f *sharedAuthorizationSQLFixture) { f.facts.Membership.Status = entity.ResourceDisabled }},
		{"key_revoked", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalKey.Status = entity.KeyRevoked }},
		{"model_disabled", "project_key", func(f *sharedAuthorizationSQLFixture) { f.facts.Model.Status = entity.ResourceDisabled }},
		{"user_disabled", "personal_session", func(f *sharedAuthorizationSQLFixture) { f.facts.User.Disabled = true }},
		{"grant_missing", "personal_key", func(f *sharedAuthorizationSQLFixture) { f.facts.PersonalRows = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, f, identity, now := sharedAuthorizationCaptureSQLFixture(t, test.kind)
			test.change(f)
			got, err := sharedAuthorizationCaptureInTransaction(db, identity, now)
			if err == nil || !reflect.DeepEqual(got, sharedCapturedAuthorization{}) {
				t.Fatal("changed locked authority produced an initial proof", err)
			}
		})
	}
}

func TestSharedAuthorizationInitialCaptureRejectsMalformedPrivateSessionProof(t *testing.T) {
	for _, kind := range []string{"personal_session", "team_session"} {
		for _, field := range []string{"primary", "oidc", "oauth", "ldap", "saml", "named_policy", "named_binding", "emergency"} {
			t.Run(kind+"_"+field, func(t *testing.T) {
				db, _, identity, now := sharedAuthorizationCaptureSQLFixture(t, kind)
				if identity.personal != nil {
					s := &identity.personal.session
					switch field {
					case "primary":
						s.PrimaryMethod = "oidc"
					case "oidc":
						s.OIDCBindingKey = "changed"
					case "oauth":
						s.OAuthPolicyRevision = "changed"
					case "ldap":
						s.LDAPBindingKey = "changed"
					case "saml":
						s.SAMLPolicyRevision = "changed"
					case "named_policy":
						s.NamedIdentityPolicyKey = "changed"
					case "named_binding":
						s.NamedIdentityBindingKey = "changed"
					case "emergency":
						s.EmergencyPasswordDigest = "changed"
					}
				} else {
					s := identity.team
					switch field {
					case "primary":
						s.primaryMethod = "oidc"
					case "oidc":
						s.oidcBindingKey = "changed"
					case "oauth":
						s.oauthPolicyRevision = "changed"
					case "ldap":
						s.ldapBindingKey = "changed"
					case "saml":
						s.samlPolicyRevision = "changed"
					case "named_policy":
						s.namedIdentityPolicyKey = "changed"
					case "named_binding":
						s.namedIdentityBindingKey = "changed"
					case "emergency":
						s.emergencyPasswordDigest = "changed"
					}
				}
				if _, err := sharedAuthorizationCaptureInTransaction(db, identity, now); err == nil {
					t.Fatal("altered authenticated private primary proof was accepted")
				}
			})
		}
	}
}
