package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/secretstore"
)

func TestDiscordConfigRejectsNoncanonicalClientBeforeSecretOrDatabase(t *testing.T) {
	good := DiscordProviderInput{Name: "Discord sign-in", ClientID: "18446744073709551615", CallbackURL: "https://routex.example/api/v1/auth/discord/callback", SecretAction: "replace", ClientSecret: "private-test-secret", Reason: "Configure sign-in"}
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	var out DiscordProviderInput
	if err = json.Unmarshal(raw, &out); err != nil || out != good {
		t.Fatal("valid exact config", err)
	}
	svc := &Service{}
	for _, id := range []string{"0", "01", "18446744073709551616", "1.0", "1e0", "-1", "+1", "１", " 1", "1 ", ""} {
		in := good
		in.ClientID = id
		encoded, e := json.Marshal(in)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(encoded, &out); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("noncanonical ID admitted by wire", id)
		}
		// A nil DB and absent secret store make any accidental later access observable.
		if _, e = svc.SaveDiscordProvider(context.Background(), "usr_admin", strings.Repeat("a", 64), in); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("noncanonical ID reached persistence or sealing", id, e)
		}
	}
	for _, bad := range []string{
		strings.Replace(string(raw), "/discord/callback", "/google/callback", 1),
		strings.Replace(string(raw), "/discord/callback", "/github/callback", 1),
		strings.Replace(string(raw), "/discord/callback", "/discord/%63allback", 1),
		strings.TrimSuffix(string(raw), "}") + `,"profile_id":"discord.oauth2.v1"}`,
		strings.TrimSuffix(string(raw), "}") + `,"client_id":"1"}`,
		strings.Replace(string(raw), `"client_id":"18446744073709551615"`, `"client_id":null`, 1),
		strings.Replace(string(raw), `"secret_action":"replace"`, `"secret_action":"keep"`, 1),
	} {
		if err = json.Unmarshal([]byte(bad), &out); !errors.Is(err, apperrors.ErrBadRequest) {
			t.Fatal("ambiguous or foreign config admitted", err)
		}
	}
	good.SecretAction, good.ClientSecret = "keep", ""
	raw, err = json.Marshal(good)
	if err != nil || json.Unmarshal(raw, &out) != nil || out != good {
		t.Fatal("keep exact input", err)
	}
	for _, bad := range []string{`{"password":"a-valid-password","proof":null,"reason":"Link"}`, `{"password":"a-valid-password","proof":{"code":"123456","recovery_code":"private"},"reason":"Link"}`, `{"password":"a-valid-password","proof":{},"reason":"Link","subject":"1"}`} {
		var in DiscordIdentityInput
		if e := json.Unmarshal([]byte(bad), &in); !errors.Is(e, apperrors.ErrBadRequest) {
			t.Fatal("ambiguous identity proof admitted", e)
		}
	}
}

func TestDiscordNamespaceDerivationAndRootEpochDoNotAliasLegacy(t *testing.T) {
	for _, tc := range []struct {
		kind, subject string
		ok            bool
	}{
		{"string", "1", true}, {"string", "18446744073709551615", true}, {"integer", "1", false}, {"string", "0", false}, {"string", "01", false}, {"string", "18446744073709551616", false}, {"string", "1e0", false},
	} {
		if namedIdentityProfileSubject("discord", tc.kind, tc.subject) != tc.ok {
			t.Fatal("typed Discord subject", tc.kind, tc.subject)
		}
	}
	if namedIdentitySubjectDigest("discord", "string", "1") == namedIdentitySubjectDigest("google", "string", "1") || namedIdentitySubjectDigest("discord", "string", "1") == namedIdentitySubjectDigest("github", "integer", "1") {
		t.Fatal("profile subject alias")
	}
	cookie, cid := strings.Repeat("c", 43), "nic_exact"
	legacy := sha256.Sum256([]byte("routex-named-identity:github.com.oauth-app.v1:pkce:" + cookie + ":" + cid))
	google := sha256.Sum256([]byte("routex-named-identity:google.oidc.v1:pkce:" + cookie + ":" + cid))
	discord := sha256.Sum256([]byte("routex-named-identity:discord.oauth2.v1:pkce:" + cookie + ":" + cid))
	if namedIdentityDerived(cookie, cid, "pkce") != base64.RawURLEncoding.EncodeToString(legacy[:]) || namedIdentityDerivedFor("google", cookie, cid, "pkce") != base64.RawURLEncoding.EncodeToString(google[:]) || namedIdentityDerivedFor("discord", cookie, cid, "pkce") != base64.RawURLEncoding.EncodeToString(discord[:]) {
		t.Fatal("profile derivation changed or aliased")
	}
	gen := strings.Repeat("a", 64)
	refs := []string{rootReference("named_identity_providers", "github", gen), rootReference("named_identity_providers", "google", gen), rootReference("named_identity_providers", "discord", gen)}
	if refs[0] != "named-identity:github.com.oauth-app.v1:github:"+gen || refs[1] != "named-identity:google.oidc.v1:google:"+gen || refs[2] != "named-identity:discord.oauth2.v1:discord:"+gen || rootReference("named_identity_providers", "Discord", gen) != "" {
		t.Fatal("fixed AAD")
	}
	store, err := secretstore.New([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := store.Seal(refs[2], "private-client-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs[:2] {
		if _, e := store.Open(ref, ciphertext); e == nil {
			t.Fatal("secret crossed profile AAD")
		}
	}
	if value, e := store.Open(refs[2], ciphertext); e != nil || value != "private-client-secret" {
		t.Fatal("own AAD", e)
	}
	if rootInventoryVersion != 8 || len(rootDomains) != 11 || rootDomains[10] != "named_identity_providers" {
		t.Fatal("current coverage")
	}
	for version, size := range map[int]int{1: 5, 2: 7, 3: 8, 4: 9, 5: 10, 6: 11, 7: 11, 8: 11} {
		if !reflect.DeepEqual(rootInventoryDomains(version), rootDomains[:size]) {
			t.Fatal("historical domains", version)
		}
	}
	if rootInventoryDomains(9) != nil {
		t.Fatal("future inventory admitted")
	}
	// Match persisted observation precision on every platform.
	now := time.Date(2026, 10, 10, 0, 0, 0, 123456000, time.UTC)
	started, confirmed := now.Add(-secretObservationDuration), now
	counts := map[string]rootDomainCounts{}
	for _, domain := range rootDomains {
		counts[domain] = rootDomainCounts{}
	}
	encoded, err := json.Marshal(counts)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{rootNow: func() time.Time { return now }}
	job := entity.SecretRotationJob{InventoryVersion: 8, Domain: 11, CountsJSON: string(encoded), ObservationStartedAt: &started, ObservationLastConfirmedAt: &confirmed, VerifiedProcessID: "process", VerifiedSnapshotID: "snapshot"}
	proof := entity.SecretProcessVerification{InventoryVersion: 8, ProcessID: "process", RuntimeSnapshotID: "snapshot"}
	if !svc.rootObservationEligible(job, proof) {
		t.Fatal("complete current coverage rejected")
	}
	beforeBoundary := job
	shortStart := started.Add(time.Microsecond)
	beforeBoundary.ObservationStartedAt = &shortStart
	if svc.rootObservationEligible(beforeBoundary, proof) {
		t.Fatal("observation eligible before the exact duration boundary")
	}
	futureConfirmation := job
	future := confirmed.Add(time.Microsecond)
	futureConfirmation.ObservationLastConfirmedAt = &future
	if svc.rootObservationEligible(futureConfirmation, proof) {
		t.Fatal("future confirmation eligible")
	}
	for _, version := range []int{6, 7} {
		oldJob, oldProof := job, proof
		oldJob.InventoryVersion, oldProof.InventoryVersion = version, version
		if svc.rootObservationEligible(oldJob, oldProof) {
			t.Fatal("legacy observation relabeled Discord-covered")
		}
		view := rootRotationView(oldJob, false, false)
		if view == nil || view.InventoryVersion != version || len(view.Domains) != 11 {
			t.Fatal("historical job no longer readable", version)
		}
	}
}

func TestDiscordRuntimeExactSevenProofAndIndependentTombstones(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	config, policy := strings.Repeat("a", 64), strings.Repeat("b", 64)
	u := entity.User{ID: "usr_exact", CreatedAt: birth}
	p := entity.NamedIdentityProvider{ID: "discord", ProfileID: discordProfileID, IdentityIssuer: discordIdentityIssuer, Enabled: true, ConfigRevision: config, PolicyRevision: policy, VerifiedConfigRevision: config, VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	b := entity.NamedIdentityBinding{ID: "nib_member", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, UserID: u.ID, UserCreatedAt: birth, CreatedAt: birth, ConfigRevision: config, SubjectKind: "string", Subject: "18446744073709551615", SubjectDigest: namedIdentitySubjectDigest(p.ID, "string", "18446744073709551615")}
	row := entity.Session{PrimaryMethod: "discord", UserID: u.ID, NamedIdentityProviderID: p.ID, NamedIdentityProfileID: p.ProfileID, NamedIdentityBindingID: b.ID, NamedIdentityBindingCreatedAt: &birth, NamedIdentityConfigRevision: config, NamedIdentityPolicyRevision: policy, NamedIdentityUserCreatedAt: &birth}
	bindings := map[string]entity.NamedIdentityBinding{b.ID: b}
	users := map[string]entity.User{u.ID: u}
	if !namedIdentityRuntimePrimary(row, &p, bindings, users) {
		t.Fatal("valid Discord runtime")
	}
	for _, mode := range []string{"method", "provider", "profile", "binding_birth", "user_birth", "config", "policy", "disabled", "issuer", "typed_subject", "mixed_binding"} {
		t.Run(mode, func(t *testing.T) {
			r, q, d := row, p, b
			later := birth.Add(time.Microsecond)
			switch mode {
			case "method":
				r.PrimaryMethod = "github"
			case "provider":
				r.NamedIdentityProviderID = "github"
			case "profile":
				r.NamedIdentityProfileID = githubProfileID
			case "binding_birth":
				r.NamedIdentityBindingCreatedAt = &later
			case "user_birth":
				r.NamedIdentityUserCreatedAt = &later
			case "config":
				r.NamedIdentityConfigRevision = strings.Repeat("c", 64)
			case "policy":
				r.NamedIdentityPolicyRevision = strings.Repeat("c", 64)
			case "disabled":
				q.Enabled = false
			case "issuer":
				q.IdentityIssuer = "https://Discord.com"
			case "typed_subject":
				d.SubjectKind = "integer"
			case "mixed_binding":
				d.ProviderID = "github"
				d.ProfileID = githubProfileID
				d.IdentityIssuer = githubIdentityIssuer
			}
			if namedIdentityRuntimePrimary(r, &q, map[string]entity.NamedIdentityBinding{d.ID: d}, users) {
				t.Fatal("obsolete/mixed proof admitted")
			}
		})
	}
	rt := &Service{runtime: &gatewayRuntime{}}
	discord := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKeyFor("discord", policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKeyFor("discord", b.ID, &birth)}
	github := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKey(policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKey(b.ID, &birth)}
	rt.invalidateRuntimeNamedIdentityPolicy("discord", policy)
	rt.invalidateRuntimeNamedIdentityBinding("discord", b.ID, birth)
	google := runtimeTeamSession{NamedIdentityPolicyKey: namedIdentityRuntimePolicyKeyFor("google", policy), NamedIdentityBindingKey: namedIdentityRuntimeBindingKeyFor("google", b.ID, &birth)}
	if !rt.runtimeNamedIdentitySessionDenied(discord) || rt.runtimeNamedIdentitySessionDenied(github) || rt.runtimeNamedIdentitySessionDenied(google) {
		t.Fatal("profile fence collision")
	}
	if !errors.Is(primaryValidateSession(nil, entity.Session{PrimaryMethod: "discord", NamedIdentityProviderID: "github", NamedIdentityProfileID: githubProfileID}), apperrors.ErrUnauthorized) {
		t.Fatal("method/profile downgrade")
	}
	if !errors.Is(primaryValidateChallenge(nil, entity.MFAChallenge{PrimaryMethod: "discord"}), apperrors.ErrUnauthorized) {
		t.Fatal("incomplete native MFA")
	}
}
func TestDiscordCeremonyFencesExactAuthorityAndOriginalDeadline(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	birth := now.Add(-time.Minute)
	p := entity.NamedIdentityProvider{ID: discordProviderID, ProfileID: discordProfileID, IdentityIssuer: discordIdentityIssuer, CreatedAt: birth, ConfigRevision: strings.Repeat("a", 64), PolicyRevision: strings.Repeat("b", 64), Enabled: true, VerifiedConfigRevision: strings.Repeat("a", 64), VerifiedBy: "usr_admin", VerifiedUserCreatedAt: &birth, VerifiedBindingID: "nib_admin", VerifiedBindingCreatedAt: &birth}
	c := entity.NamedIdentityCeremony{ID: "nic_first", ProviderID: p.ID, ProfileID: p.ProfileID, IdentityIssuer: p.IdentityIssuer, ProviderCreatedAt: birth, Purpose: "login", CreatedAt: birth, ExpiresAt: birth.Add(namedIdentityCeremonyLifetime), ConfigRevision: p.ConfigRevision, PolicyRevision: p.PolicyRevision}
	if !namedIdentityCeremonyCurrent(p, c, now) {
		t.Fatal("valid ceremony")
	}
	for _, mode := range []string{"profile", "issuer", "provider_birth", "config", "policy", "disabled", "expired", "extended", "future", "purpose"} {
		t.Run(mode, func(t *testing.T) {
			q, d := p, c
			switch mode {
			case "profile":
				d.ProfileID = "other"
			case "issuer":
				d.IdentityIssuer = "https://Discord.com"
			case "provider_birth":
				q.CreatedAt = q.CreatedAt.Add(time.Microsecond)
			case "config":
				q.ConfigRevision = strings.Repeat("c", 64)
			case "policy":
				q.PolicyRevision = strings.Repeat("c", 64)
			case "disabled":
				q.Enabled = false
			case "expired":
				d.ExpiresAt = now
			case "extended":
				d.ExpiresAt = d.CreatedAt.Add(namedIdentityCeremonyLifetime + time.Microsecond)
			case "future":
				d.CreatedAt = now.Add(time.Second)
			case "purpose":
				d.Purpose = "unknown"
			}
			if namedIdentityCeremonyCurrent(q, d, now) {
				t.Fatal("obsolete proof")
			}
		})
	}
	p.Enabled = false
	c.Purpose = "verify"
	if !namedIdentityCeremonyCurrent(p, c, now) {
		t.Fatal("explicit admin verification blocked")
	}
	cookie := strings.Repeat("a", 43)
	if namedIdentityDerivedFor("discord", cookie, "nic_a", "pkce") == namedIdentityDerivedFor("discord", cookie, "nic_b", "pkce") || namedIdentityDerivedFor("discord", cookie, "nic_a", "pkce") == oauthDerived(cookie, "nic_a", "pkce") {
		t.Fatal("browser namespace missing")
	}

	admitted := time.Now().Add(-6 * time.Second)
	outer, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	local, done := namedIdentityLocalContext(outer, admitted)
	defer done()
	deadline, ok := local.Deadline()
	if !ok || !deadline.Equal(admitted.Add(5*time.Second)) || !errors.Is(local.Err(), context.DeadlineExceeded) || outer.Err() != nil {
		t.Fatal("local budget renewed or remote cap conflated")
	}
	parent, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	local2, done2 := namedIdentityLocalContext(parent, time.Now())
	defer done2()
	a, _ := parent.Deadline()
	z, _ := local2.Deadline()
	if !a.Equal(z) || !errors.Is(local2.Err(), context.DeadlineExceeded) {
		t.Fatal("short parent not preserved")
	}
	canceled, stop3 := context.WithCancel(context.Background())
	local3, done3 := namedIdentityLocalContext(canceled, time.Now())
	defer done3()
	stop3()
	if !errors.Is(local3.Err(), context.Canceled) {
		t.Fatal("cancellation lost")
	}
}

func TestDiscordAuditProjectsOnlyReasonAndExactMethod(t *testing.T) {
	birth := time.Date(2026, 10, 10, 0, 0, 0, 123456000, time.UTC)
	actor := entity.User{ID: "usr_auditor", CreatedAt: birth}
	provider := entity.NamedIdentityProvider{ID: discordProviderID, ProfileID: discordProfileID, IdentityIssuer: discordIdentityIssuer, CreatedAt: birth, ReviewRevision: strings.Repeat("a", 64), ConfigRevision: strings.Repeat("b", 64), PolicyRevision: strings.Repeat("c", 64), ClientID: "private-client", AuthCiphertext: "private-ciphertext"}
	binding := entity.NamedIdentityBinding{ID: "nib_retained", ProviderID: provider.ID, ProfileID: provider.ProfileID, IdentityIssuer: provider.IdentityIssuer, UserID: actor.ID, UserCreatedAt: birth, CreatedAt: birth, ConfigRevision: provider.ConfigRevision, SubjectKind: "string", Subject: "18446744073709551615"}
	for _, action := range []string{"identity.discord.config.update", "identity.discord.status.update", "identity.discord.verify", "account.discord.bind", "account.discord.unlink"} {
		t.Run(action, func(t *testing.T) {
			var b *entity.NamedIdentityBinding
			resource, id := "named_identity_provider", "discord"
			if action == "identity.discord.verify" || strings.HasPrefix(action, "account.") {
				b = &binding
			}
			if strings.HasPrefix(action, "account.") {
				resource, id = "named_identity_binding", binding.ID
			}
			raw, e := namedIdentityAuditJSON(actor, provider, b, "Review access 原因")
			if e != nil {
				t.Fatal(e)
			}
			row := entity.AuditEvent{ID: "aud_retained", ActorID: actor.ID, Action: action, ResourceType: resource, ResourceID: id, DetailsJSON: &raw}
			result, ok := namedIdentityAuditProjection(row)
			if !ok || result.Kind != "discord_identity" || result.Reason != "Review access 原因" {
				t.Fatal("typed projection", ok)
			}
			record := auditRecord(row)
			var fields map[string]json.RawMessage
			if e = json.Unmarshal(record.Changes, &fields); e != nil || len(fields) != 2 || string(fields["kind"]) != `"discord_identity"` || fields["reason"] == nil {
				t.Fatal("public projection", e)
			}
			for _, private := range []string{"profile_id", "identity_issuer", "client_id", "ciphertext", "subject", "revision", "binding_created_at"} {
				if strings.Contains(string(record.Changes), private) {
					t.Fatal("private field projected", private)
				}
			}
			var details map[string]any
			if e = json.Unmarshal([]byte(raw), &details); e != nil {
				t.Fatal(e)
			}
			details["access_token"] = "PRIVATE_SENTINEL"
			changed, e := json.Marshal(details)
			if e != nil {
				t.Fatal(e)
			}
			bad := string(changed)
			row.DetailsJSON = &bad
			if _, ok = namedIdentityAuditProjection(row); ok {
				t.Fatal("unreviewed secret details projected")
			}
			row.DetailsJSON = &raw
			row.Action = strings.Replace(action, "discord", "google", 1)
			if _, ok = namedIdentityAuditProjection(row); ok {
				t.Fatal("cross-profile audit projected")
			}
		})
	}
}
