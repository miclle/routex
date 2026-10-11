package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/vault"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAPIKeyDeliveryPublicConfirmRequiresManualMode(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	now := birth.Add(time.Second)
	ack := entity.APIKeyVaultDeliveryBinding{KeyKind: "personal", KeyID: "key_exact", KeyBirth: birth, Version: 1, AcknowledgedAt: &now}
	ack.KeyDigest = apiKeyDeliveryKeyDigest(ack.KeyKind, ack.KeyID, birth)
	if e := apiKeyDeliveryConfirmMode("manual", ack.KeyID, birth, nil); e != nil {
		t.Fatal(e)
	}
	if e := apiKeyDeliveryConfirmMode("managed_vault", ack.KeyID, birth, &ack); e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"managed_vault", "managed_vault ", "MANAGED_VAULT", "", "other"} {
		if e := apiKeyDeliveryConfirmMode(mode, ack.KeyID, birth, nil); e == nil {
			t.Fatalf("public confirmation admitted mode %q", mode)
		}
	}
	for name, mutate := range map[string]func(*entity.APIKeyVaultDeliveryBinding){"id": func(b *entity.APIKeyVaultDeliveryBinding) { b.KeyID += "x" }, "birth": func(b *entity.APIKeyVaultDeliveryBinding) { b.KeyBirth = b.KeyBirth.Add(time.Nanosecond) }, "version": func(b *entity.APIKeyVaultDeliveryBinding) { b.Version = 0 }, "ack": func(b *entity.APIKeyVaultDeliveryBinding) { b.AcknowledgedAt = nil }, "digest": func(b *entity.APIKeyVaultDeliveryBinding) { b.KeyDigest = strings.Repeat("0", 64) }} {
		t.Run(name, func(t *testing.T) {
			changed := ack
			mutate(&changed)
			if e := apiKeyDeliveryConfirmMode("managed_vault", ack.KeyID, birth, &changed); e == nil {
				t.Fatal("unproven binding admitted")
			}
		})
	}
	if e := apiKeyDeliveryConfirmMode("manual", ack.KeyID, birth, &ack); e == nil {
		t.Fatal("internal managed proof changed manual mode")
	}
}
func TestAPIKeyDeliveryValidationFactsRejectExtraPrivileges(t *testing.T) {
	now := time.Now()
	p := APIKeyDeliveryValidationPlan{SHA256: strings.Repeat("a", 64), Purpose: "writer", Capabilities: []APIKeyDeliveryCapability{{"kv/data/profile/routex-api-key-key", []string{"create"}}}}
	base := APIKeyDeliveryValidationFacts{PlanSHA256: p.SHA256, OwnIdentityObserved: true, TTLSeconds: 60, TokenType: "service", ExpiresAt: now.Add(time.Minute), MachineIdentityCommitment: strings.Repeat("b", 64), Capabilities: []APIKeyDeliveryCapability{{p.Capabilities[0].Path, []string{"create"}}}, CloseState: vault.ResponseCloseState{Observed: true}}
	if !apiKeyDeliveryValidationMatches(p, base) {
		t.Fatal("exact observed create-only plan rejected")
	}
	changes := map[string]func(*APIKeyDeliveryValidationFacts){"plan": func(f *APIKeyDeliveryValidationFacts) { f.PlanSHA256 = strings.Repeat("c", 64) }, "identity": func(f *APIKeyDeliveryValidationFacts) { f.OwnIdentityObserved = false }, "ttl": func(f *APIKeyDeliveryValidationFacts) { f.TTLSeconds = 0 }, "expiry": func(f *APIKeyDeliveryValidationFacts) { f.ExpiresAt = now.Add(-time.Second) }, "batch": func(f *APIKeyDeliveryValidationFacts) { f.TokenType = "batch" }, "missing_commitment": func(f *APIKeyDeliveryValidationFacts) { f.MachineIdentityCommitment = "" }, "close_unknown": func(f *APIKeyDeliveryValidationFacts) { f.CloseState.Observed = false }, "close_failed": func(f *APIKeyDeliveryValidationFacts) { f.CloseState.Failed = true }, "close_pending": func(f *APIKeyDeliveryValidationFacts) { f.CloseState.Pending = 1 }, "read": func(f *APIKeyDeliveryValidationFacts) {
		f.Capabilities[0].Actions = append(f.Capabilities[0].Actions, "read")
	}, "update": func(f *APIKeyDeliveryValidationFacts) {
		f.Capabilities[0].Actions = append(f.Capabilities[0].Actions, "update")
	}, "duplicate_action": func(f *APIKeyDeliveryValidationFacts) {
		f.Capabilities[0].Actions = append(f.Capabilities[0].Actions, "create")
	}, "other_path": func(f *APIKeyDeliveryValidationFacts) { f.Capabilities[0].Path += "x" }, "extra_path": func(f *APIKeyDeliveryValidationFacts) {
		f.Capabilities = append(f.Capabilities, APIKeyDeliveryCapability{"sys/*", []string{"sudo"}})
	}}
	for name, mutate := range changes {
		t.Run(name, func(t *testing.T) {
			f := base
			f.Capabilities = []APIKeyDeliveryCapability{{base.Capabilities[0].Path, slices.Clone(base.Capabilities[0].Actions)}}
			mutate(&f)
			if apiKeyDeliveryValidationMatches(p, f) {
				t.Fatal("nonexact or unproven facts admitted")
			}
		})
	}
}
func TestAPIKeyDeliveryValidationPlanBindsGeneratedInputs(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	auth := entity.APIKeyDeliveryAuth{ID: "kda_writer", Purpose: "writer", SecretGeneration: "generation", IntegrationID: "vlt_exact", IntegrationBirth: birth, IntegrationRevision: strings.Repeat("a", 64), VaultRevisionID: "vlr_exact", VaultRevisionBirth: birth, CreatedAt: birth}
	x := apiKeyDeliverySnapshot{Revision: entity.VaultRevision{Endpoint: "https://vault.example", Namespace: "org", Mount: "kv", Prefix: "api-keys"}, Template: entity.APIKeyCallerTemplate{AuthMount: "jwt"}}
	plan := apiKeyDeliveryValidationPlan(x, auth, "vap_exact", "vpb_exact", "key_exact")
	if plan.KVMount != "kv" || plan.KeyPrefix != "api-keys" || plan.AuthMount != "jwt" || plan.ProfileID != "vap_exact" || plan.PrincipalID != "vpb_exact" || plan.ResourceID != "key_exact" || plan.Capabilities[0].Path != "kv/data/api-keys/vap_exact/routex-api-key-key_exact" {
		t.Fatal("generated writer plan drift")
	}
	changed := auth
	changed.SecretGeneration += "x"
	if apiKeyDeliveryValidationPlan(x, changed, plan.ProfileID, plan.PrincipalID, plan.ResourceID).SHA256 == plan.SHA256 {
		t.Fatal("auth generation unbound")
	}
	if apiKeyDeliveryValidationPlan(x, auth, plan.ProfileID, plan.PrincipalID, "key_other").SHA256 == plan.SHA256 {
		t.Fatal("destination unbound")
	}
	auth.Purpose = "provisioner"
	got := apiKeyDeliveryValidationPlan(x, auth, plan.ProfileID, plan.PrincipalID, plan.ResourceID)
	want := []APIKeyDeliveryCapability{{"auth/jwt/config", []string{"read"}}, {"sys/policies/acl/routex-api-key-vap_exact", []string{"read", "update"}}, {"auth/jwt/role/routex-api-key-vpb_exact", []string{"create", "read", "update"}}}
	if !reflect.DeepEqual(got.Capabilities, want) || got.SHA256 == plan.SHA256 {
		t.Fatal("separate provisioner capabilities/purpose drift")
	}
}
func TestAPIKeyDeliveryMissingValidatorCannotVerify(t *testing.T) {
	s := &Service{}
	code, e := s.apiKeyDeliveryValidateMachines(context.Background(), apiKeyDeliverySnapshot{}, "vap_exact", "vpb_exact", "key_exact")
	if !errors.Is(e, apiKeyDeliveryUnavailable) || code != "dependency_validation_unavailable" {
		t.Fatal("missing dedicated validation falsely verified")
	}
}
func TestAPIKeyDeliveryIntentCanonicalSetAndExactSelection(t *testing.T) {
	expiry := time.Unix(1800000000, 123000000).UTC()
	in := ManagedAPIKeyInput{Name: "Example", ModelIDs: []string{"mod_b", "mod_a"}, ExpiresAt: &expiry, RequestID: "intent", Reason: "reviewed", Delivery: APIKeyDeliverySelection{ConsumerID: "worker", Environment: "prod", Principal: APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: "worker-exact"}, TargetReview: strings.Repeat("a", 64)}}
	before := apiKeyDeliveryIntent(in, "")
	copy := in
	copy.ModelIDs = []string{"mod_a", "mod_b"}
	local := expiry.In(time.FixedZone("offset", 8*3600))
	copy.ExpiresAt = &local
	if apiKeyDeliveryIntent(copy, "") != before || in.ModelIDs[0] != "mod_b" {
		t.Fatal("set/instant canonicalization or caller mutation")
	}
	for name, mutate := range map[string]func(*ManagedAPIKeyInput){"subject_case": func(v *ManagedAPIKeyInput) { v.Delivery.Principal.Subject = "Worker-exact" }, "consumer": func(v *ManagedAPIKeyInput) { v.Delivery.ConsumerID += "x" }, "environment": func(v *ManagedAPIKeyInput) { v.Delivery.Environment += "x" }, "review": func(v *ManagedAPIKeyInput) { v.Delivery.TargetReview = strings.Repeat("b", 64) }, "reason": func(v *ManagedAPIKeyInput) { v.Reason += "x" }, "nil_expiry": func(v *ManagedAPIKeyInput) { v.ExpiresAt = nil }, "models": func(v *ManagedAPIKeyInput) { v.ModelIDs = []string{"mod_a"} }} {
		t.Run(name, func(t *testing.T) {
			v := in
			mutate(&v)
			if apiKeyDeliveryIntent(v, "") == before {
				t.Fatal("changed intent aliases original")
			}
		})
	}
	if apiKeyDeliveryIntent(in, "key_predecessor") == before {
		t.Fatal("rotation aliases create")
	}
}
func TestAPIKeyDeliveryClaimRejectsReplayAndRenewal(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	a := &Authentication{User: entity.User{ID: "usr_exact", CreatedAt: birth}, Session: entity.Session{ID: "ses_exact", CreatedAt: birth}}
	c := entity.APIKeyDeliveryCommand{ID: "kdo_exact", IntentDigest: strings.Repeat("a", 64), KeyID: "key_exact", KeyBirth: &birth, BindingID: "kvb_exact", ActorID: a.User.ID, ActorBirth: birth, SessionID: a.Session.ID, SessionBirth: birth, DeliveryStatus: "pending"}
	if !apiKeyDeliveryClaimable(c, c, a) {
		t.Fatal("fresh original stack rejected")
	}
	for name, mutate := range map[string]func(*entity.APIKeyDeliveryCommand){"claimed": func(v *entity.APIKeyDeliveryCommand) {
		v.ClaimedAt = &birth
		v.ClaimProcessID = "ins_exact"
		v.ClaimProcessBirth = &birth
	}, "partial_claim": func(v *entity.APIKeyDeliveryCommand) { v.ClaimProcessID = "ins_exact" }, "revoked": func(v *entity.APIKeyDeliveryCommand) { v.RevokedAt = &birth }, "acknowledged": func(v *entity.APIKeyDeliveryCommand) { v.AcknowledgedAt = &birth }, "unknown": func(v *entity.APIKeyDeliveryCommand) { v.DeliveryStatus = "unknown" }, "session": func(v *entity.APIKeyDeliveryCommand) { v.SessionID = "ses_new" }, "actor_birth": func(v *entity.APIKeyDeliveryCommand) { v.ActorBirth = v.ActorBirth.Add(time.Nanosecond) }, "key_birth": func(v *entity.APIKeyDeliveryCommand) { b := birth.Add(time.Nanosecond); v.KeyBirth = &b }} {
		t.Run(name, func(t *testing.T) {
			v := c
			mutate(&v)
			if apiKeyDeliveryClaimable(v, c, a) {
				t.Fatal("obsolete/replayed claim admitted")
			}
		})
	}
	captured := apiKeyDeliveryCopyAuthentication(a)
	a.Session.ID = "ses_renewed"
	if captured.Session.ID != "ses_exact" || apiKeyDeliveryClaimable(c, c, a) {
		t.Fatal("authority capture/renewal drift")
	}
}
func TestAPIKeyDeliveryOriginalDeadlineAndCancelledAdmission(t *testing.T) {
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	deadline, _ := parent.Deadline()
	for _, read := range []bool{true, false} {
		ctx, done, e := apiKeyDeliveryContext(parent, read)
		if e != nil {
			t.Fatal(e)
		}
		got, _ := ctx.Deadline()
		done()
		if !got.Equal(deadline) {
			t.Fatal("original parent budget renewed")
		}
	}
	parent, cancel = context.WithCancel(context.Background())
	cancel()
	if _, done, e := apiKeyDeliveryContext(parent, false); e == nil {
		done()
		t.Fatal("cancelled admission accepted")
	}
	//lint:ignore SA1012 This deliberately verifies rejection of a nil context.
	//nolint:staticcheck // Intentionally exercise nil-context rejection.
	if _, done, e := apiKeyDeliveryContext(nil, false); e == nil {
		done()
		t.Fatal("nil context accepted")
	}
	for _, read := range []bool{true, false} {
		start := time.Now()
		ctx, done, e := apiKeyDeliveryContext(context.Background(), read)
		if e != nil {
			t.Fatal(e)
		}
		deadline, _ := ctx.Deadline()
		done()
		limit := 10 * time.Second
		if read {
			limit = 3 * time.Second
		}
		if deadline.Sub(start) > limit+time.Second || deadline.Sub(start) < limit-time.Second {
			t.Fatal("operation bound drift")
		}
	}
}
func TestAPIKeyDeliveryRuntimeProofRequiresCurrentExactKey(t *testing.T) {
	now := time.Now()
	birth := time.Unix(1700000000, 0).UTC()
	key := entity.APIKey{ID: "key_exact", UserID: "usr_exact", CreatedAt: birth, Status: entity.KeyActive, DeliveryMode: "managed_vault", TokenHash: strings.Repeat("a", 64), LifecycleRevision: strings.Repeat("b", 64)}
	auth := &runtimeAuthorization{ValidUntil: now.Add(time.Minute), KeysByID: map[string]runtimeKey{key.ID: {Key: key, Models: []string{"mod_a", "mod_b"}}}}
	if !apiKeyDeliveryRuntimeMatches(auth, key, "", []string{"mod_b", "mod_a"}, now) {
		t.Fatal("exact current serving proof rejected")
	}
	if apiKeyDeliveryRuntimeMatches(nil, key, "", nil, now) {
		t.Fatal("nil runtime inferred success")
	}
	for name, mutate := range map[string]func(*runtimeKey){"hash": func(v *runtimeKey) { v.Key.TokenHash = strings.Repeat("c", 64) }, "owner": func(v *runtimeKey) { v.Key.UserID = "usr_other" }, "birth": func(v *runtimeKey) { v.Key.CreatedAt = v.Key.CreatedAt.Add(time.Nanosecond) }, "lifecycle": func(v *runtimeKey) { v.Key.LifecycleRevision = strings.Repeat("d", 64) }, "mode": func(v *runtimeKey) { v.Key.DeliveryMode = "manual" }, "models": func(v *runtimeKey) { v.Models = []string{"mod_a"} }, "project": func(v *runtimeKey) { v.ProjectID = "prj_other" }, "status": func(v *runtimeKey) { v.Key.Status = entity.KeyPending }} {
		t.Run(name, func(t *testing.T) {
			got := auth.KeysByID[key.ID]
			mutate(&got)
			changed := *auth
			changed.KeysByID = map[string]runtimeKey{key.ID: got}
			if apiKeyDeliveryRuntimeMatches(&changed, key, "", []string{"mod_a", "mod_b"}, now) {
				t.Fatal("nonexact runtime proof accepted")
			}
		})
	}
	if apiKeyDeliveryRuntimeMatches(auth, key, "", []string{"mod_a", "mod_b"}, auth.ValidUntil) {
		t.Fatal("expired generation accepted")
	}
}
func TestAPIKeyDeliveryManagedResultsDoNotSerializePrivateRecords(t *testing.T) {
	sentinel := "secret-proof-never-returned"
	r := ManagedAPIKeyResult{Personal: &KeyRecord{Key: entity.APIKey{TokenHash: sentinel, Prefix: sentinel}}, Delivery: APIKeyDeliveryReceiptView{RequestID: "request", KeyID: "key_exact", Mode: "managed_vault", DeliveryStatus: "unknown", RuntimeStatus: "unknown"}}
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), sentinel) || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "token_hash") {
		t.Fatal("private managed record serialized")
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(raw, &fields); e != nil || len(fields) != 1 || fields["delivery"] == nil {
		t.Fatal("managed envelope drift")
	}
	input := APIKeyDeliveryIdentityInput{Token: sentinel}
	raw, e = json.Marshal(input)
	if e != nil || string(raw) != "{}" || strings.Contains(input.String(), sentinel) || strings.Contains(input.GoString(), sentinel) {
		t.Fatal("machine input exposed")
	}
}

func TestAPIKeyDeliveryPendingActivationBindsIsolationAndSecurity(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	p := entity.VaultAccessProfile{ID: "vap_exact", CreatedAt: birth, OwnerKind: "personal", OwnerID: "usr_exact", OwnerBirth: birth, OwnerDigest: strings.Repeat("a", 64), ConsumerID: "worker", Environment: "prod", ConfigRevision: strings.Repeat("b", 64), SecurityRevision: strings.Repeat("c", 64), IntegrationID: "vlt_exact", IntegrationBirth: birth, IntegrationRevision: strings.Repeat("d", 64), VaultRevisionID: "vlr_exact", VaultRevisionBirth: birth, InfrastructureSecurityRevision: strings.Repeat("d", 64), WriterIdentityID: "kda_writer", WriterIdentityBirth: birth, WriterGeneration: "kdg_writer", ProvisionerIdentityID: "kda_provisioner", ProvisionerIdentityBirth: birth, ProvisionerGeneration: "kdg_provisioner", TemplateID: "jwt", TemplateBirth: birth, TemplateSecurityRevision: strings.Repeat("e", 64), AuthMount: "jwt", Issuer: "https://issuer.example", JWKSURL: "https://issuer.example/jwks", Audience: "service", TrustSecurityRevision: strings.Repeat("f", 64)}
	principal := entity.VaultPrincipalBinding{ID: "vpb_exact", CreatedAt: birth, SecurityRevision: strings.Repeat("1", 64), Method: "jwt", Subject: "worker-exact"}
	att := entity.VaultPrincipalIsolationAttestation{Revision: strings.Repeat("2", 64), TupleDigest: apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, principal))}
	key := entity.APIKey{ID: "key_exact", CreatedAt: birth}
	binding, e := apiKeyDeliveryBinding(p, principal, att, key, entity.APIKeyDeliverySlot{ID: "kds_exact", DescriptorRevision: 1}, vault.APIKeyWritePlan{DescriptorSHA256: strings.Repeat("3", 64)}, entity.VaultRevision{Mount: "kv", Prefix: "api-keys", DataField: "value"})
	if e != nil {
		t.Fatal(e)
	}
	if !apiKeyDeliveryBindingCurrent(binding, p, principal, att) {
		t.Fatal("exact immutable target snapshot rejected")
	}
	for name, mutate := range map[string]func(*entity.APIKeyVaultDeliveryBinding){"profile_birth": func(v *entity.APIKeyVaultDeliveryBinding) { v.ProfileBirth = v.ProfileBirth.Add(time.Nanosecond) }, "profile_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.ProfileSecurityRevision = strings.Repeat("4", 64) }, "principal_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.PrincipalSecurityRevision = strings.Repeat("5", 64) }, "isolation_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.IsolationRevision = strings.Repeat("6", 64) }, "isolation_tuple": func(v *entity.APIKeyVaultDeliveryBinding) { v.IsolationTupleDigest = strings.Repeat("7", 64) }, "vault_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.VaultRevisionID += "x" }, "vault_birth": func(v *entity.APIKeyVaultDeliveryBinding) {
		v.VaultRevisionBirth = v.VaultRevisionBirth.Add(time.Nanosecond)
	}, "writer_generation": func(v *entity.APIKeyVaultDeliveryBinding) { v.WriterGeneration += "x" }, "provisioner_generation": func(v *entity.APIKeyVaultDeliveryBinding) { v.ProvisionerGeneration += "x" }, "template_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.TemplateSecurityRevision = strings.Repeat("8", 64) }, "trust_revision": func(v *entity.APIKeyVaultDeliveryBinding) { v.TrustSecurityRevision = strings.Repeat("9", 64) }, "subject_case": func(v *entity.APIKeyVaultDeliveryBinding) { v.PrincipalSubject = "Worker-exact" }} {
		t.Run(name, func(t *testing.T) {
			v := binding
			mutate(&v)
			if apiKeyDeliveryBindingCurrent(v, p, principal, att) {
				t.Fatal("changed security tuple admitted pending activation")
			}
		})
	}
	before := apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, principal))
	p.SecurityRevision = strings.Repeat("0", 64)
	if apiKeyDeliveryTupleDigest(apiKeyDeliveryTargetTuple(p, principal)) == before {
		t.Fatal("ABA security generation ignored")
	}
}

func TestAPIKeyDeliveryKnownAcknowledgementCannotRestoreLostIssuer(t *testing.T) {
	birth := time.Unix(1700000000, 0).UTC()
	now := birth.Add(time.Second)
	actor := &Authentication{User: entity.User{ID: "usr_exact", CreatedAt: birth}, Session: entity.Session{ID: "ses_original", CreatedAt: birth}}
	command := entity.APIKeyDeliveryCommand{ID: "kdo_exact", ActorID: actor.User.ID, ActorBirth: birth, SessionID: actor.Session.ID, SessionBirth: birth, DeliveryStatus: "pending", RuntimeStatus: "pending"}
	if !apiKeyDeliveryOriginalIssuer(command, actor) {
		t.Fatal("exact original issuer rejected")
	}
	fresh := *actor
	fresh.Session.ID = "ses_renewed"
	for name, changed := range map[string]*Authentication{"renewed_session": &fresh, "missing_session": nil} {
		t.Run(name, func(t *testing.T) {
			if apiKeyDeliveryOriginalIssuer(command, changed) {
				t.Fatal("later authority adopted an in-flight original acknowledgement")
			}
		})
	}
	changed := *actor
	changed.Session.CreatedAt = changed.Session.CreatedAt.Add(time.Microsecond)
	if apiKeyDeliveryOriginalIssuer(command, &changed) {
		t.Fatal("Session birth ABA accepted")
	}
	changed = *actor
	changed.User.CreatedAt = changed.User.CreatedAt.Add(time.Microsecond)
	if apiKeyDeliveryOriginalIssuer(command, &changed) {
		t.Fatal("actor birth ABA accepted")
	}
	lost := command
	apiKeyDeliveryApplyAcknowledgement(&lost, now, false)
	if lost.DeliveryStatus != "acknowledged" || lost.AcknowledgedAt == nil || !lost.AcknowledgedAt.Equal(now) || lost.RevokedAt == nil || !lost.RevokedAt.Equal(now) || lost.TerminalAt == nil || !lost.TerminalAt.Equal(now) || lost.RuntimeStatus != "unknown" || lost.FailureCode != "issuing_authority_changed" {
		t.Fatal("known ack did not preserve historical outcome and terminal revocation together")
	}
	if apiKeyDeliveryClaimable(lost, command, &fresh) {
		t.Fatal("renewed Session reclaimed a terminal acknowledgement")
	}
	admitted := command
	apiKeyDeliveryApplyAcknowledgement(&admitted, now, true)
	if admitted.DeliveryStatus != "acknowledged" || admitted.AcknowledgedAt == nil || admitted.RevokedAt != nil || admitted.TerminalAt != nil || admitted.RuntimeStatus != "pending" {
		t.Fatal("eligible ack was confused with terminal loss or publication success")
	}
	if !apiKeyDeliveryKnownStateLoss(apiKeyDeliveryConflict) || !apiKeyDeliveryKnownStateLoss(apiKeyDeliveryUnavailable) || apiKeyDeliveryKnownStateLoss(errors.New("database transport unavailable")) {
		t.Fatal("unknown database failure was treated as definite authority loss")
	}
}
func TestAPIKeyDeliveryNewConsumerIsAllocatedWithoutChangingIntent(t *testing.T) {
	in := APIKeyDeliveryResolveInput{RequestID: "00000000-0000-4000-8000-000000000001", ApplicationName: "Application", Environment: "prod", Principal: APIKeyDeliveryPrincipalInput{Method: "jwt", Subject: "workload"}, Reason: "reviewed"}
	intent := apiKeyDeliveryResolveIntent(in)
	first, e := apiKeyDeliveryAllocateConsumer(in.ConsumerID)
	if e != nil || !apiKeyDeliveryCode(first) || first == "" {
		t.Fatal("empty new Consumer input not allocated safely")
	}
	second, e := apiKeyDeliveryAllocateConsumer("")
	if e != nil || second == first {
		t.Fatal("new explicit request reused an allocated Consumer identity")
	}
	if in.ConsumerID != "" || apiKeyDeliveryResolveIntent(in) != intent {
		t.Fatal("server allocation rewrote the retained client intent")
	}
	existing, e := apiKeyDeliveryAllocateConsumer(first)
	if e != nil || existing != first {
		t.Fatal("existing Consumer selection normalized")
	}
	selected := in
	selected.ConsumerID = first
	if apiKeyDeliveryResolveIntent(selected) == intent {
		t.Fatal("different selected Consumer aliased original empty allocation intent")
	}
	for _, bad := range []string{" has space", "../other", strings.Repeat("a", 65)} {
		if _, e := apiKeyDeliveryAllocateConsumer(bad); e == nil {
			t.Fatal("invalid Consumer identifier admitted")
		}
	}
}
func TestAPIKeyDeliveryMissingTemplateHasSafeReviewedEmptyView(t *testing.T) {
	review := strings.Repeat("a", 64)
	view := apiKeyDeliveryUnconfiguredTemplate(review)
	if view.ReviewETag != review || view.AuthMount != "" || view.Issuer != "" || view.JWKSURL != "" || view.Audience != "" || view.TokenTTLSeconds != 0 || view.TokenMaxTTLSeconds != 0 || view.Verified || view.AllowedSubjects == nil || len(view.AllowedSubjects) != 0 {
		t.Fatal("unconfigured template view inferred configuration or lost current review")
	}
	raw, e := json.Marshal(view)
	if e != nil || !strings.Contains(string(raw), `"allowed_subjects":[]`) || !strings.Contains(string(raw), `"verified":false`) {
		t.Fatal("safe empty template wire shape changed")
	}
}
