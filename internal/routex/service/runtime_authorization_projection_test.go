package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

const projectionUser = "usr_00000000000000000000000001"
const projectionProject = "prj_00000000000000000000000001"
const projectionRoot = "pky_00000000000000000000000001"
const projectionPersonal = "key_00000000000000000000000001"
const projectionModel = "mdl_00000000000000000000000001"
const projectionTeam = "tem_00000000000000000000000001"

// The positive prerequisite uses the production builder and replacement-chain
// selector. No database, remote service or fabricated successful observation.
func projectionFixture(t *testing.T) *runtimeAuthorization {
	t.Helper()
	birth := time.Date(2026, 10, 10, 1, 2, 3, 123456789, time.UTC)
	data := &runtimeData{
		Users:          []entity.User{{ID: projectionUser, CreatedAt: birth}},
		Keys:           []entity.APIKey{{ID: projectionPersonal, UserID: projectionUser, LifecycleRevision: "0", TokenHash: strings.Repeat("a", 64), Status: entity.KeyActive, CreatedAt: birth.Add(time.Second)}},
		Scopes:         []entity.APIKeyModel{{KeyID: projectionPersonal, ModelID: projectionModel}},
		Grants:         []entity.UserModelGrant{{UserID: projectionUser, ModelID: projectionModel}},
		Models:         []entity.Model{{ID: projectionModel, Status: "active", CreatedAt: birth}},
		Names:          []entity.ModelName{{Name: "public", ModelID: projectionModel, CreatedAt: birth}},
		Providers:      []entity.Provider{{ID: "prv_one", Enabled: true, CreatedAt: birth, ETag: "0"}},
		Connections:    []entity.ProviderConnection{{ID: "con_one", ProviderID: "prv_one", Enabled: true, TransportGeneration: "0", ETag: "0", CreatedAt: birth, Protocol: entity.ProtocolOpenAIChat}},
		ProviderModels: []entity.ProviderModel{{ID: "pmd_one", ConnectionID: "con_one", CapabilityTransportGeneration: "0", ETag: "0", CreatedAt: birth}},
		ProjectData: &projectRuntimeData{
			Projects: []entity.Project{{ID: projectionProject, CreatorID: projectionUser, Status: entity.ResourceActive, CreatedAt: birth}},
			Managers: []entity.ProjectManager{{ID: "pmg_one", ProjectID: projectionProject, UserID: projectionUser}},
			Keys:     []entity.ProjectKey{{ID: projectionRoot, ProjectID: projectionProject, CreatorID: projectionUser, TokenHash: strings.Repeat("b", 64), Status: entity.KeyActive, CreatedAt: birth.Add(time.Second)}},
			Scopes:   []entity.ProjectKeyModel{{KeyID: projectionRoot, ModelID: projectionModel}},
			Grants:   []entity.ProjectModelGrant{{ProjectID: projectionProject, ModelID: projectionModel}},
		},
		TeamSessionData: &teamSessionRuntimeData{
			Sessions:    []entity.Session{{ID: "ses_one", UserID: projectionUser, TokenHash: strings.Repeat("c", 64), ExpiresAt: birth.Add(time.Hour)}},
			Teams:       []entity.Team{{ID: projectionTeam, Status: entity.ResourceActive, CreatedAt: birth}},
			Memberships: []entity.TeamMembership{{ID: "tmb_one", TeamID: projectionTeam, UserID: projectionUser, Status: entity.ResourceActive, Role: entity.TeamOwner}},
			Grants:      []entity.TeamModelGrant{{TeamID: projectionTeam, ModelID: projectionModel}},
		},
		Quota: &runtimeQuotaData{
			Setting:   entity.QuotaSetting{ID: 1, TimeZone: "UTC", ETag: "0"},
			Bounds:    map[string]entity.ReservationBound{"pmd_one": {ProviderModelID: "pmd_one", Protocol: entity.ProtocolOpenAIChat, TransportGeneration: "0", MaxInputTokens: 100, MaxOutputTokens: 50, ETag: "0"}},
			Created:   map[string]time.Time{limitAccount("key", projectionRoot): birth.Add(time.Second), limitAccount("key", projectionPersonal): birth.Add(time.Second)},
			Revisions: map[string]string{limitAccount("key", projectionRoot): "0"}, Currency: "USD",
		},
	}
	if err := compileRuntimeLimits(data); err != nil {
		t.Fatal("valid builder prerequisite", err)
	}
	auth := buildRuntimeAuthorization(data, birth.Add(5*time.Second))
	auth.SourceDigest = strings.Repeat("d", 64)
	if len(auth.Keys) != 2 || len(auth.TeamSessions) != 1 || len(auth.ProjectLimitOwners) != 1 {
		t.Fatal("production builder omitted positive authority")
	}
	return auth
}
func projectionDigest(t *testing.T, auth *runtimeAuthorization) string {
	t.Helper()
	digest, err := runtimeAuthorizationProjection(t.Context(), auth)
	if err != nil || len(digest) != 64 {
		t.Fatal("complete projection prerequisite failed", err)
	}
	for _, ch := range digest {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			t.Fatal("noncanonical aggregate")
		}
	}
	return digest
}
func projectionReject(t *testing.T, ctx context.Context, auth *runtimeAuthorization) {
	t.Helper()
	digest, err := runtimeAuthorizationProjection(ctx, auth)
	if err == nil || digest != "" {
		t.Fatal("invalid/late projection returned a partial or complete digest")
	}
	for _, value := range []string{strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64), "private-sentinel"} {
		if strings.Contains(err.Error(), value) {
			t.Fatal("private input escaped in error")
		}
	}
}

func TestRuntimeAuthorizationProjectionRealBuilderAndRootProof(t *testing.T) {
	auth := projectionFixture(t)
	digest := projectionDigest(t, auth)
	result := &GatewayResult{KeyID: projectionRoot, ProjectID: projectionProject}
	if !projectKeyQuotaProof(auth, result, limitAccount("key", projectionRoot)) {
		t.Fatal("root quota proof positive prerequisite failed")
	}
	for _, replacement := range []string{"", " ", " pky_other ", "pky_other", projectionRoot} {
		t.Run(fmt.Sprintf("nonnil_%q", replacement), func(t *testing.T) {
			fresh := projectionFixture(t)
			root := fresh.ProjectLimitOwners[projectionRoot]
			root.Root.ReplacesKeyID = &replacement
			fresh.ProjectLimitOwners[projectionRoot] = root
			projectionReject(t, t.Context(), fresh)
			if projectKeyQuotaProof(fresh, result, limitAccount("key", projectionRoot)) {
				t.Fatal("nonnil root passed consumed quota invariant")
			}
			if projectionDigest(t, auth) != digest || auth.ProjectLimitOwners[projectionRoot].Root.ReplacesKeyID != nil {
				t.Fatal("rejected evidence mutated prior authority")
			}
		})
	}
}

func TestRuntimeAuthorizationProjectionEveryLogicalDomain(t *testing.T) {
	baseline := projectionDigest(t, projectionFixture(t))
	changes := map[string]func(*runtimeAuthorization){
		"Connections": func(a *runtimeAuthorization) {
			v := a.Connections["con_one"]
			v.Enabled = false
			a.Connections["con_one"] = v
		},
		"Providers": func(a *runtimeAuthorization) {
			v := a.Providers["prv_one"]
			v.Revision = "next"
			a.Providers["prv_one"] = v
		},
		"PersonalGrantStates": func(a *runtimeAuthorization) {
			a.PersonalGrantStates[projectionUser] = runtimePersonalGrantState{Revision: "next"}
		},
		"ModelEligibilityHashes": func(a *runtimeAuthorization) { a.ModelEligibilityHashes[projectionModel] = "next" },
		"PersonalKeyStates": func(a *runtimeAuthorization) {
			a.PersonalKeyStates[projectionPersonal] = runtimePersonalKeyState{UserID: projectionUser, Revision: "next", Status: entity.KeyDisabled}
		},
		"UserAdmissions": func(a *runtimeAuthorization) {
			v := a.UserAdmissions[projectionUser]
			v.Eligible = !v.Eligible
			a.UserAdmissions[projectionUser] = v
		},
		"UserProofs": func(a *runtimeAuthorization) {
			v := a.UserProofs[projectionUser]
			v.Enabled = false
			a.UserProofs[projectionUser] = v
		},
		"ProjectCreationStates": func(a *runtimeAuthorization) {
			v := a.ProjectCreationStates[projectionProject]
			v.Eligible = !v.Eligible
			a.ProjectCreationStates[projectionProject] = v
		},
		"PersonalGrantSources": func(a *runtimeAuthorization) {
			a.PersonalGrantSources[projectionUser] = map[string]string{projectionModel: "request_one"}
		},
		"TeamGrantSources": func(a *runtimeAuthorization) {
			a.TeamGrantSources[projectionTeam] = map[string]string{projectionModel: "request_one"}
		},
		"TeamCreationGrants": func(a *runtimeAuthorization) {
			a.TeamCreationGrants[projectionTeam][projectionModel] = runtimeTeamCreationGrant{CreationID: "creation_one"}
		},
		"SourceDigest":           func(a *runtimeAuthorization) { a.SourceDigest = strings.Repeat("e", 64) },
		"CredentialRevisions":    func(a *runtimeAuthorization) { a.CredentialRevisions["crd_one"] = "next" },
		"ProviderModelRevisions": func(a *runtimeAuthorization) { a.ProviderModelRevisions["pmd_one"] = "next" },
		"Quota":                  func(a *runtimeAuthorization) { a.Quota.Setting.AccountingStarted = true },
		"ConnectionRevisions":    func(a *runtimeAuthorization) { a.ConnectionRevisions["con_one"] = "next" },
		"LimitPolicies": func(a *runtimeAuthorization) {
			zero := int64(0)
			a.LimitPolicies["user_one"] = limits.Policy{Tokens5H: &zero}
		},
		"LimitRoots":          func(a *runtimeAuthorization) { a.LimitRoots[projectionRoot] = "other" },
		"PersonalLimitOwners": func(a *runtimeAuthorization) { a.PersonalLimitOwners[projectionPersonal] = "other" },
		"ProjectLimitOwners": func(a *runtimeAuthorization) {
			v := a.ProjectLimitOwners[projectionRoot]
			v.Root.Status = entity.KeyDisabled
			a.ProjectLimitOwners[projectionRoot] = v
		},
		"Keys": func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			v.Key.LifecycleRevision = "next"
			a.Keys[strings.Repeat("a", 64)] = v
			a.KeysByID[v.Key.ID] = v
		},
		"Names":            func(a *runtimeAuthorization) { v := a.Names["public"]; v.ModelID = "other"; a.Names["public"] = v },
		"Models":           func(a *runtimeAuthorization) { a.Models[projectionModel] = false },
		"Credentials":      func(a *runtimeAuthorization) { a.Credentials["crd_one"] = false },
		"ProviderModels":   func(a *runtimeAuthorization) { a.ProviderModels["pmd_one"] = !a.ProviderModels["pmd_one"] },
		"CredentialAccess": func(a *runtimeAuthorization) { a.CredentialAccess["crd_one"] = map[string]bool{"pmd_one": true} },
		"ModelCreated": func(a *runtimeAuthorization) {
			a.ModelCreated[projectionModel] = a.ModelCreated[projectionModel].Add(time.Nanosecond)
		},
		"TeamSessions": func(a *runtimeAuthorization) {
			k := strings.Repeat("c", 64)
			v := a.TeamSessions[k]
			v.NamedIdentityBindingKey = "google:binding"
			a.TeamSessions[k] = v
		},
		"Teams": func(a *runtimeAuthorization) {
			v := a.Teams[projectionTeam]
			v.Members[projectionUser] = "other"
			a.Teams[projectionTeam] = v
		},
	}
	if len(changes) != 29 {
		t.Fatal("logical domain control missing")
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			a := projectionFixture(t)
			change(a)
			if projectionDigest(t, a) == baseline {
				t.Fatal("changed installed domain did not change source")
			}
		})
	}
	a := projectionFixture(t)
	a.publicationEpoch++
	a.ValidUntil = a.ValidUntil.Add(time.Hour)
	if projectionDigest(t, a) != baseline {
		t.Fatal("renewable guards changed logical source")
	}
	// The separately owned publication hook adds exactly this private cache.
	a.installationProjectionDigest = "private-cache-sentinel"
	if projectionDigest(t, a) != baseline {
		t.Fatal("cached aggregate recursively hashed")
	}
}

func TestRuntimeAuthorizationProjectionCanonicalAndTypedValues(t *testing.T) {
	baseline := projectionFixture(t)
	values := []string{"mdl_Z", "mdl_a"}
	key := baseline.Keys[strings.Repeat("a", 64)]
	key.Models = values
	baseline.Keys[key.Key.TokenHash] = key
	baseline.KeysByID[key.Key.ID] = key
	baseline.LimitPolicies["scope"] = limits.Policy{IPRanges: []string{"192.0.2.0/24", "2001:db8::/32"}}
	before := projectionDigest(t, baseline)
	reversed := projectionFixture(t)
	key = reversed.Keys[strings.Repeat("a", 64)]
	key.Models = []string{"mdl_a", "mdl_Z"}
	key.Key.CreatedAt = key.Key.CreatedAt.In(time.FixedZone("offset", 8*60*60))
	reversed.Keys[key.Key.TokenHash] = key
	reversed.KeysByID[key.Key.ID] = key
	reversed.LimitPolicies["scope"] = limits.Policy{IPRanges: []string{"2001:db8::/32", "192.0.2.0/24"}}
	if projectionDigest(t, reversed) != before {
		t.Fatal("genuine set/time-location reorder changed source")
	}
	if !reflect.DeepEqual(values, []string{"mdl_Z", "mdl_a"}) || !reflect.DeepEqual(baseline.LimitPolicies["scope"].IPRanges, []string{"192.0.2.0/24", "2001:db8::/32"}) {
		t.Fatal("canonicalization mutated published slices")
	}
	for _, test := range []struct {
		name   string
		change func(*runtimeAuthorization)
	}{
		{"nil_map", func(a *runtimeAuthorization) { a.CredentialAccess = nil }},
		{"nil_quota", func(a *runtimeAuthorization) { a.Quota = nil }},
		{"false_present", func(a *runtimeAuthorization) { a.Credentials["absent"] = false }},
		{"optional_zero_time", func(a *runtimeAuthorization) {
			v := a.Names["public"]
			zero := time.Time{}
			v.ExpiresAt = &zero
			a.Names["public"] = v
		}},
		{"optional_empty_string", func(a *runtimeAuthorization) {
			v := a.Names["public"]
			empty := ""
			v.CurrentModelID = &empty
			a.Names["public"] = v
		}},
		{"nanosecond", func(a *runtimeAuthorization) {
			v := a.UserProofs[projectionUser]
			v.CreatedAt = v.CreatedAt.Add(time.Nanosecond)
			a.UserProofs[projectionUser] = v
		}},
		{"nil_vs_empty_set", func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			v.Models = nil
			a.Keys[v.Key.TokenHash] = v
			a.KeysByID[v.Key.ID] = v
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := projectionFixture(t)
			original := projectionDigest(t, a)
			test.change(a)
			if projectionDigest(t, a) == original {
				t.Fatal("typed source distinction collapsed")
			}
		})
	}
	// Nil and empty effective Model sets are separately represented.
	a := projectionFixture(t)
	v := a.Keys[strings.Repeat("a", 64)]
	v.Models = nil
	a.Keys[v.Key.TokenHash] = v
	a.KeysByID[v.Key.ID] = v
	nilSet := projectionDigest(t, a)
	v.Models = []string{}
	a.Keys[v.Key.TokenHash] = v
	a.KeysByID[v.Key.ID] = v
	if projectionDigest(t, a) == nilSet {
		t.Fatal("nil and empty set aliased")
	}
	// Null and explicit zero/decimal zero are distinct policy authority.
	a = projectionFixture(t)
	a.LimitPolicies["scope"] = limits.Policy{}
	nilPolicy := projectionDigest(t, a)
	z := int64(0)
	money := "0"
	a.LimitPolicies["scope"] = limits.Policy{Tokens5H: &z, MoneyMonth: &money}
	if projectionDigest(t, a) == nilPolicy {
		t.Fatal("zero policy became null")
	}
	// Map insertion order does not define source; exact key bytes still do.
	a = projectionFixture(t)
	a.ModelEligibilityHashes = map[string]string{"Z": "one", "a": "two"}
	first := projectionDigest(t, a)
	a.ModelEligibilityHashes = map[string]string{"a": "two", "Z": "one"}
	if projectionDigest(t, a) != first {
		t.Fatal("map insertion order affected source")
	}
	a.ModelEligibilityHashes = map[string]string{"a": "two", "z": "one"}
	if projectionDigest(t, a) == first {
		t.Fatal("case-sensitive map identity collapsed")
	}
}

func TestRuntimeAuthorizationProjectionVerifierAndExactIdentities(t *testing.T) {
	for _, purpose := range []string{"personal", "project", "team"} {
		t.Run(purpose, func(t *testing.T) {
			a := projectionFixture(t)
			old := projectionDigest(t, a)
			next := strings.Repeat("f", 64)
			switch purpose {
			case "personal", "project":
				lookup := strings.Repeat("a", 64)
				if purpose == "project" {
					lookup = strings.Repeat("b", 64)
				}
				value := a.Keys[lookup]
				delete(a.Keys, lookup)
				value.Key.TokenHash = next
				a.Keys[next] = value
				a.KeysByID[value.Key.ID] = value
			case "team":
				value := a.TeamSessions[strings.Repeat("c", 64)]
				delete(a.TeamSessions, value.TokenHash)
				value.TokenHash = next
				a.TeamSessions[next] = value
			}
			if projectionDigest(t, a) == old {
				t.Fatal("changed actual verifier mapping ignored")
			}
		})
	}
	w := &runtimeProjectionWriter{ctx: t.Context(), digest: sha256.New()}
	raw := strings.Repeat("a", 64)
	if w.verifier("personal-key", raw) == w.verifier("project-key", raw) || w.verifier("personal-key", raw) == w.verifier("team-session", raw) {
		t.Fatal("verifier purposes aliased")
	}
	for _, test := range []struct {
		name   string
		change func(*runtimeAuthorization)
	}{
		{"missing_key_index", func(a *runtimeAuthorization) { delete(a.KeysByID, projectionPersonal) }},
		{"wrong_key_index", func(a *runtimeAuthorization) {
			v := a.KeysByID[projectionPersonal]
			delete(a.KeysByID, projectionPersonal)
			a.KeysByID["alias"] = v
		}},
		{"different_index_status", func(a *runtimeAuthorization) {
			v := a.KeysByID[projectionPersonal]
			v.Key.Status = entity.KeyDisabled
			a.KeysByID[projectionPersonal] = v
		}},
		{"different_index_expiry", func(a *runtimeAuthorization) {
			v := a.KeysByID[projectionPersonal]
			zero := time.Time{}
			v.Key.ExpiresAt = &zero
			a.KeysByID[projectionPersonal] = v
		}},
		{"different_index_set", func(a *runtimeAuthorization) {
			v := a.KeysByID[projectionPersonal]
			v.Models = []string{"other"}
			a.KeysByID[projectionPersonal] = v
		}},
		{"lookup_disagrees", func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			delete(a.Keys, v.Key.TokenHash)
			a.Keys[strings.Repeat("f", 64)] = v
		}},
		{"duplicate_key_id", func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			v.Key.TokenHash = strings.Repeat("f", 64)
			a.Keys[v.Key.TokenHash] = v
		}},
		{"dual_key_owner", func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			v.ProjectID = projectionProject
			a.Keys[v.Key.TokenHash] = v
			a.KeysByID[v.Key.ID] = v
		}},
		{"duplicate_models", func(a *runtimeAuthorization) {
			v := a.Keys[strings.Repeat("a", 64)]
			v.Models = []string{projectionModel, projectionModel}
			a.Keys[v.Key.TokenHash] = v
			a.KeysByID[v.Key.ID] = v
		}},
		{"root_map_alias", func(a *runtimeAuthorization) {
			v := a.ProjectLimitOwners[projectionRoot]
			delete(a.ProjectLimitOwners, projectionRoot)
			a.ProjectLimitOwners["alias"] = v
		}},
		{"root_project_alias", func(a *runtimeAuthorization) {
			v := a.ProjectLimitOwners[projectionRoot]
			v.Root.ProjectID = "other"
			a.ProjectLimitOwners[projectionRoot] = v
		}},
		{"name_alias", func(a *runtimeAuthorization) {
			v := a.Names["public"]
			a.Names = map[string]entity.ModelName{"alias": v}
		}},
		{"bound_alias", func(a *runtimeAuthorization) {
			v := a.Quota.Bounds["pmd_one"]
			a.Quota.Bounds = map[string]entity.ReservationBound{"alias": v}
		}},
		{"project_alias", func(a *runtimeAuthorization) {
			v := a.ProjectCreationStates[projectionProject]
			a.ProjectCreationStates = map[string]runtimeProjectCreationState{"alias": v}
		}},
		{"team_lookup", func(a *runtimeAuthorization) {
			v := a.TeamSessions[strings.Repeat("c", 64)]
			v.TokenHash = strings.Repeat("f", 64)
			a.TeamSessions[strings.Repeat("c", 64)] = v
		}},
		{"team_duplicate_id", func(a *runtimeAuthorization) {
			v := a.TeamSessions[strings.Repeat("c", 64)]
			v.TokenHash = strings.Repeat("f", 64)
			a.TeamSessions[v.TokenHash] = v
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := projectionFixture(t)
			projectionDigest(t, a)
			test.change(a)
			projectionReject(t, t.Context(), a)
		})
	}
}

// Cancel from an actual hash write: verifies that late cancellation is checked
// inside work and before accepting a digest, without timers/sleeps or goroutines.
type projectionCancelHash struct {
	hash.Hash
	cancel context.CancelFunc
	once   bool
}

func (h *projectionCancelHash) Write(v []byte) (int, error) {
	n, err := h.Hash.Write(v)
	if !h.once {
		h.once = true
		h.cancel()
	}
	return n, err
}
func TestRuntimeAuthorizationProjectionBudgetAndPrivacy(t *testing.T) {
	a := projectionFixture(t)
	projectionDigest(t, a)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	projectionReject(t, cancelled, a)
	expired, done := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer done()
	projectionReject(t, expired, a)
	// A nil caller is rejected without inventing a background evidence budget.
	projectionReject(t, nil, a) // Deliberate invalid caller.
	projectionReject(t, t.Context(), nil)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	h := &projectionCancelHash{Hash: sha256.New(), cancel: stop}
	w := &runtimeProjectionWriter{ctx: ctx, digest: h}
	w.text("private-sentinel")
	if w.check() {
		t.Fatal("cancellation during projection accepted")
	}
	for _, test := range []struct {
		name   string
		change func(*runtimeAuthorization)
	}{
		{"stream_bytes", func(a *runtimeAuthorization) {
			a.SourceDigest = strings.Repeat("private-sentinel", runtimeProjectionMaxBytes/16+1)
		}},
		{"map_entries", func(a *runtimeAuthorization) {
			a.Models = make(map[string]bool, runtimeProjectionMaxEntries+1)
			for i := 0; i <= runtimeProjectionMaxEntries; i++ {
				a.Models[fmt.Sprint(i)] = false
			}
		}},
		{"nested_entries", func(a *runtimeAuthorization) {
			a.CredentialAccess = map[string]map[string]bool{}
			for i := 0; i < 512; i++ {
				inner := make(map[string]bool, 512)
				for j := 0; j < 512; j++ {
					inner[fmt.Sprint(j)] = false
				}
				a.CredentialAccess[fmt.Sprint(i)] = inner
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) { a := projectionFixture(t); test.change(a); projectionReject(t, t.Context(), a) })
	}
}

func TestRuntimeAuthorizationProjectionPrimaryProofAndMetadata(t *testing.T) {
	for _, field := range []string{"OIDCPolicyRevision", "OIDCBindingKey", "OAuthPolicyRevision", "OAuthBindingKey", "LDAPPolicyRevision", "LDAPBindingKey", "SAMLPolicyRevision", "SAMLBindingKey", "NamedIdentityPolicyKey", "NamedIdentityBindingKey"} {
		t.Run(field, func(t *testing.T) {
			a := projectionFixture(t)
			before := projectionDigest(t, a)
			lookup := strings.Repeat("c", 64)
			session := a.TeamSessions[lookup]
			reflect.ValueOf(&session).Elem().FieldByName(field).SetString("changed-exact-proof")
			a.TeamSessions[lookup] = session
			if projectionDigest(t, a) == before {
				t.Fatal("installed primary proof ignored")
			}
		})
	}
	// These stored metadata fields do not participate in Gateway authorization.
	a := projectionFixture(t)
	before := projectionDigest(t, a)
	project := a.ProjectCreationStates[projectionProject]
	project.Project.Name = "display"
	project.Project.Description = "description"
	project.Project.UpdatedAt = time.Now()
	a.ProjectCreationStates[projectionProject] = project
	root := a.ProjectLimitOwners[projectionRoot]
	root.Root.Name = "display"
	root.Root.Prefix = "prefix"
	root.Root.UpdatedAt = time.Now()
	root.Root.DeliveryMode = "deferred"
	root.Root.DeliveryExpiresAt = new(time.Time)
	root.Root.ActivateOnConfirm = true
	root.Project.Name = "display"
	root.Project.UpdatedAt = time.Now()
	a.ProjectLimitOwners[projectionRoot] = root
	key := a.Keys[strings.Repeat("a", 64)]
	key.Key.Name = "display"
	key.Key.Prefix = "prefix"
	key.Key.UpdatedAt = time.Now()
	key.Key.DeliveryExpiresAt = new(time.Time)
	key.Key.ActivateOnConfirm = true
	a.Keys[key.Key.TokenHash] = key
	a.KeysByID[key.Key.ID] = key
	a.Quota.Setting.ActorID = "actor"
	a.Quota.Setting.Reason = "reason"
	a.Quota.Setting.PreviousETag = "previous"
	a.Quota.Setting.UpdatedAt = time.Now()
	bound := a.Quota.Bounds["pmd_one"]
	bound.Evidence = "prose"
	bound.ActorID = "actor"
	bound.Reason = "reason"
	bound.PreviousETag = "previous"
	bound.UpdatedAt = time.Now()
	a.Quota.Bounds["pmd_one"] = bound
	if projectionDigest(t, a) != before {
		t.Fatal("excluded display/audit/delivery metadata affected admission identity")
	}
}

func projectionFieldParity(t *testing.T, value any, included, excluded string) {
	t.Helper()
	classified := map[string]bool{}
	for _, name := range strings.Fields(included + " " + excluded) {
		if classified[name] {
			t.Fatal("duplicate field classification", name)
		}
		classified[name] = true
	}
	typ := reflect.TypeOf(value)
	if typ.NumField() != len(classified) {
		t.Fatalf("%s field count changed: %d vs classified %d", typ.Name(), typ.NumField(), len(classified))
	}
	for i := 0; i < typ.NumField(); i++ {
		if !classified[typ.Field(i).Name] {
			t.Fatalf("%s has unclassified field %s", typ.Name(), typ.Field(i).Name)
		}
	}
}
func TestRuntimeAuthorizationProjectionClosedFieldParity(t *testing.T) {
	// Closed original32 plus one explicitly excluded cache. A new unclassified
	// field fails here; reflection never supplies production serialization.
	projectionFieldParity(t, runtimeAuthorization{},
		"Connections Providers PersonalGrantStates ModelEligibilityHashes PersonalKeyStates UserAdmissions UserProofs ProjectCreationStates PersonalGrantSources TeamGrantSources TeamCreationGrants SourceDigest CredentialRevisions ProviderModelRevisions Quota ConnectionRevisions LimitPolicies LimitRoots PersonalLimitOwners ProjectLimitOwners Keys KeysByID Names Models Credentials ProviderModels CredentialAccess ModelCreated TeamSessions Teams",
		"publicationEpoch ValidUntil installationProjectionDigest")
	projectionFieldParity(t, runtimeConnectionProof{}, "TransportGeneration ProviderID Birth Enabled", "")
	projectionFieldParity(t, runtimeProviderProof{}, "Birth Enabled Revision", "")
	projectionFieldParity(t, runtimePersonalGrantState{}, "CreatedAt Enabled Revision Hash", "")
	projectionFieldParity(t, runtimePersonalKeyState{}, "UserID Revision Status", "")
	projectionFieldParity(t, runtimeAdmissionProof{}, "CreatedAt ApplicationID ApplicationCreatedAt Revision State Eligible", "")
	projectionFieldParity(t, runtimeUserProof{}, "CreatedAt Enabled", "")
	projectionFieldParity(t, runtimeProjectCreationState{}, "Project Managers EnabledManagers Models Eligible", "")
	projectionFieldParity(t, runtimeTeamCreationGrant{}, "ModelCreatedAt CreationID RequestID", "")
	projectionFieldParity(t, runtimeKey{}, "Key ProjectID Models", "")
	projectionFieldParity(t, projectKeyMonthlyIdentity{}, "Root Project", "")
	projectionFieldParity(t, runtimeQuotaData{}, "Setting Bounds Created Revisions Currency", "")
	projectionFieldParity(t, runtimeTeamSession{}, "ID UserID TokenHash ExpiresAt OIDCPolicyRevision OIDCBindingKey OAuthPolicyRevision OAuthBindingKey LDAPPolicyRevision LDAPBindingKey SAMLPolicyRevision SAMLBindingKey NamedIdentityPolicyKey NamedIdentityBindingKey", "")
	projectionFieldParity(t, runtimeTeam{}, "CreatedAt Members Models", "")
	projectionFieldParity(t, entity.APIKey{}, "ID UserID TokenHash LifecycleRevision Status ExpiresAt CreatedAt ReplacesKeyID", "Name Prefix DeliveryExpiresAt ActivateOnConfirm UpdatedAt")
	// APIKey.ReplacesKeyID is represented by already-built LimitRoots, not by
	// delivery metadata. Root.ReplacesKeyID is independently consumed and MUST
	// remain in the included group; nonnil controls above forbid omission.
	projectionFieldParity(t, entity.ProjectKey{}, "ID ProjectID CreatorID TokenHash Status ExpiresAt CreatedAt ReplacesKeyID", "Name Prefix DeliveryMode DeliveryExpiresAt ActivateOnConfirm UpdatedAt")
	projectionFieldParity(t, entity.Project{}, "ID Status CreatorID CreatedAt", "Name Description UpdatedAt")
	projectionFieldParity(t, entity.ModelName{}, "Name ModelID CurrentModelID ExpiresAt CreatedAt", "")
	projectionFieldParity(t, entity.QuotaSetting{}, "ID AccountingStarted TimeZone ETag", "PreviousETag ActorID Reason UpdatedAt")
	projectionFieldParity(t, entity.ReservationBound{}, "ProviderModelID Protocol TransportGeneration MaxInputTokens MaxOutputTokens ETag", "Evidence PreviousETag ActorID Reason UpdatedAt")
	projectionFieldParity(t, limits.Policy{}, "Tokens5H Tokens7D TokensMonthBehavior MoneyMonthBehavior TokensMonth TPM MoneyMonth Currency RPM Concurrency IPMode IPRanges", "")
}

func TestRuntimeAuthorizationProjectionCollectionNilnessAndBoundaries(t *testing.T) {
	typ := reflect.TypeOf(runtimeAuthorization{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Map || field.Name == "Keys" || field.Name == "KeysByID" {
			continue
		}
		t.Run(field.Name, func(t *testing.T) {
			nilAuth, emptyAuth := projectionFixture(t), projectionFixture(t)
			reflect.ValueOf(nilAuth).Elem().FieldByName(field.Name).Set(reflect.Zero(field.Type))
			reflect.ValueOf(emptyAuth).Elem().FieldByName(field.Name).Set(reflect.MakeMap(field.Type))
			if projectionDigest(t, nilAuth) == projectionDigest(t, emptyAuth) {
				t.Fatal("nil map aliased empty map")
			}
		})
	}
	// Empty derived Key lookup still has a typed nil distinction.
	a := projectionFixture(t)
	a.Keys = nil
	a.KeysByID = nil
	bothNil := projectionDigest(t, a)
	a.Keys = map[string]runtimeKey{}
	a.KeysByID = map[string]runtimeKey{}
	bothEmpty := projectionDigest(t, a)
	if bothNil == bothEmpty {
		t.Fatal("nil and empty Key maps aliased")
	}
	a.KeysByID = nil
	if projectionDigest(t, a) == bothEmpty {
		t.Fatal("derived index nilness erased")
	}
	// Nested maps are typed independently of presence at their parent.
	a = projectionFixture(t)
	a.PersonalGrantSources[projectionUser] = nil
	nestedNil := projectionDigest(t, a)
	a.PersonalGrantSources[projectionUser] = map[string]string{}
	if projectionDigest(t, a) == nestedNil {
		t.Fatal("nested map nilness erased")
	}
	for _, count := range []int{runtimeProjectionMaxBytes - 9, runtimeProjectionMaxBytes - 8} {
		w := &runtimeProjectionWriter{ctx: t.Context(), digest: sha256.New()}
		w.text(strings.Repeat("x", count))
		if w.check() != (count == runtimeProjectionMaxBytes-9) {
			t.Fatal("stream limit boundary is not exact")
		}
	}
	for _, entries := range []int{runtimeProjectionMaxEntries - 1, runtimeProjectionMaxEntries} {
		w := &runtimeProjectionWriter{ctx: t.Context(), digest: sha256.New()}
		w.visit(entries)
		w.boolean(false)
		if w.check() != (entries == runtimeProjectionMaxEntries-1) {
			t.Fatal("visited-entry boundary is not exact")
		}
	}
}

func TestRuntimeAuthorizationProjectionEveryLimitAndQuotaField(t *testing.T) {
	changes := map[string]func(*limits.Policy){
		"Tokens5H":            func(p *limits.Policy) { v := int64(1); p.Tokens5H = &v },
		"Tokens7D":            func(p *limits.Policy) { v := int64(1); p.Tokens7D = &v },
		"TokensMonthBehavior": func(p *limits.Policy) { p.TokensMonthBehavior = "alert_only" },
		"MoneyMonthBehavior":  func(p *limits.Policy) { p.MoneyMonthBehavior = "alert_only" },
		"TokensMonth":         func(p *limits.Policy) { v := int64(1); p.TokensMonth = &v },
		"TPM":                 func(p *limits.Policy) { v := int64(1); p.TPM = &v },
		"MoneyMonth":          func(p *limits.Policy) { v := "0.000000000000000001"; p.MoneyMonth = &v },
		"Currency":            func(p *limits.Policy) { p.Currency = "EUR" },
		"RPM":                 func(p *limits.Policy) { v := int64(1); p.RPM = &v },
		"Concurrency":         func(p *limits.Policy) { v := int64(1); p.Concurrency = &v },
		"IPMode":              func(p *limits.Policy) { p.IPMode = "allow" },
		"IPRanges":            func(p *limits.Policy) { p.IPRanges = []string{"192.0.2.0/24"} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			a := projectionFixture(t)
			a.LimitPolicies["scope"] = limits.Policy{}
			before := projectionDigest(t, a)
			p := a.LimitPolicies["scope"]
			change(&p)
			a.LimitPolicies["scope"] = p
			if projectionDigest(t, a) == before {
				t.Fatal("effective limit lost")
			}
		})
	}
	for name, change := range map[string]func(*runtimeQuotaData){
		"setting_id":         func(q *runtimeQuotaData) { q.Setting.ID++ },
		"setting_accounting": func(q *runtimeQuotaData) { q.Setting.AccountingStarted = true },
		"setting_zone":       func(q *runtimeQuotaData) { q.Setting.TimeZone = "Etc/UTC" },
		"setting_revision":   func(q *runtimeQuotaData) { q.Setting.ETag = "next" },
		"bound_protocol": func(q *runtimeQuotaData) {
			v := q.Bounds["pmd_one"]
			v.Protocol = entity.ProtocolOpenAIResponses
			q.Bounds["pmd_one"] = v
		},
		"bound_transport": func(q *runtimeQuotaData) {
			v := q.Bounds["pmd_one"]
			v.TransportGeneration = "next"
			q.Bounds["pmd_one"] = v
		},
		"bound_input":    func(q *runtimeQuotaData) { v := q.Bounds["pmd_one"]; v.MaxInputTokens++; q.Bounds["pmd_one"] = v },
		"bound_output":   func(q *runtimeQuotaData) { v := q.Bounds["pmd_one"]; v.MaxOutputTokens++; q.Bounds["pmd_one"] = v },
		"bound_revision": func(q *runtimeQuotaData) { v := q.Bounds["pmd_one"]; v.ETag = "next"; q.Bounds["pmd_one"] = v },
		"account_birth": func(q *runtimeQuotaData) {
			q.Created[limitAccount("key", projectionRoot)] = q.Created[limitAccount("key", projectionRoot)].Add(time.Nanosecond)
		},
		"account_revision": func(q *runtimeQuotaData) { q.Revisions[limitAccount("key", projectionRoot)] = "next" },
		"denomination":     func(q *runtimeQuotaData) { q.Currency = "EUR" },
	} {
		t.Run(name, func(t *testing.T) {
			a := projectionFixture(t)
			before := projectionDigest(t, a)
			change(a.Quota)
			if projectionDigest(t, a) == before {
				t.Fatal("quota authority lost")
			}
		})
	}
}
