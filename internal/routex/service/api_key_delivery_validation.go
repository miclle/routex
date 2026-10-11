package service

import (
	"context"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/vault"
	"slices"
	"time"
)

func newAPIKeyDeliveryID(prefix string) (string, error) {
	v, e := id.NewPrefixed(prefix)
	if e != nil {
		return "", apiKeyDeliveryUnavailable
	}
	return v, nil
}
func apiKeyDeliveryValidationPlan(x apiKeyDeliverySnapshot, auth entity.APIKeyDeliveryAuth, profile, principal, resource string) APIKeyDeliveryValidationPlan {
	p := APIKeyDeliveryValidationPlan{Purpose: auth.Purpose, AuthID: auth.ID, Generation: auth.SecretGeneration, IntegrationID: auth.IntegrationID, IntegrationRevision: auth.IntegrationRevision, VaultRevisionID: auth.VaultRevisionID, AuthBirth: auth.CreatedAt.UTC(), IntegrationBirth: auth.IntegrationBirth.UTC(), VaultRevisionBirth: auth.VaultRevisionBirth.UTC(), Endpoint: x.Revision.Endpoint, Namespace: x.Revision.Namespace, KVMount: x.Revision.Mount, KeyPrefix: x.Revision.Prefix, AuthMount: x.Template.AuthMount, ProfileID: profile, PrincipalID: principal, ResourceID: resource}
	if auth.Purpose == "writer" {
		p.Capabilities = []APIKeyDeliveryCapability{{x.Revision.Mount + "/data/" + x.Revision.Prefix + "/" + profile + "/routex-api-key-" + resource, []string{"create"}}}
	} else {
		p.Capabilities = []APIKeyDeliveryCapability{{"auth/" + x.Template.AuthMount + "/config", []string{"read"}}, {"sys/policies/acl/routex-api-key-" + profile, []string{"read", "update"}}, {"auth/" + x.Template.AuthMount + "/role/routex-api-key-" + principal, []string{"create", "read", "update"}}}
	}
	p.SHA256 = apiKeyDeliveryDigest("api-key-delivery:validation-plan:v1", p)
	return p
}
func apiKeyDeliveryValidationMatches(p APIKeyDeliveryValidationPlan, f APIKeyDeliveryValidationFacts) bool {
	if !personalModelETag(p.SHA256) || p.SHA256 != f.PlanSHA256 || !f.OwnIdentityObserved || f.TTLSeconds == 0 || !personalModelETag(f.MachineIdentityCommitment) || f.TokenType != "service" || !f.ExpiresAt.After(time.Now()) || !f.CloseState.Observed || f.CloseState.Failed || f.CloseState.Pending != 0 || len(p.Capabilities) != len(f.Capabilities) {
		return false
	}
	seen := map[string]bool{}
	for _, want := range p.Capabilities {
		found := false
		for _, got := range f.Capabilities {
			if got.Path != want.Path {
				continue
			}
			if seen[got.Path] {
				return false
			}
			seen[got.Path] = true
			v := slices.Clone(got.Actions)
			slices.Sort(v)
			if !slices.Equal(v, want.Actions) {
				return false
			}
			found = true
		}
		if !found {
			return false
		}
	}
	return len(seen) == len(p.Capabilities)
}
func (s *Service) apiKeyDeliveryValidateMachines(ctx context.Context, x apiKeyDeliverySnapshot, profile, principal, resource string) (string, error) {
	if s.apiKeyDeliveryValidator == nil {
		return "dependency_validation_unavailable", apiKeyDeliveryUnavailable
	}
	var facts [2]APIKeyDeliveryValidationFacts
	for i, auth := range []entity.APIKeyDeliveryAuth{x.Writer, x.Provisioner} {
		if ctx.Err() != nil {
			return "dependency_validation_unavailable", apiKeyDeliveryUnavailable
		}
		ref, e := apiKeyDeliveryAuthReference(auth)
		if e != nil {
			return "dependency_validation_unavailable", apiKeyDeliveryUnavailable
		}
		material, e := s.openSecret(ref, auth.AuthCiphertext)
		if e != nil || !vaultToken(material) {
			return "dependency_validation_unavailable", apiKeyDeliveryUnavailable
		}
		plan := apiKeyDeliveryValidationPlan(x, auth, profile, principal, resource)
		// The injected validator receives an independent copied slice. It cannot
		// mutate the expected immutable plan while reporting matching facts.
		passed := plan
		passed.Capabilities = make([]APIKeyDeliveryCapability, len(plan.Capabilities))
		for j, c := range plan.Capabilities {
			passed.Capabilities[j] = APIKeyDeliveryCapability{c.Path, slices.Clone(c.Actions)}
		}
		observed, e := s.apiKeyDeliveryValidator.Validate(ctx, passed, material)
		if observed.CloseState.Failed || observed.CloseState.Pending != 0 {
			s.credentialSources.mu.Lock()
			s.credentialSources.processPoisoned = true
			s.credentialSources.mu.Unlock()
		}
		if e != nil || ctx.Err() != nil || !apiKeyDeliveryValidationMatches(plan, observed) {
			return "dependency_validation_unavailable", apiKeyDeliveryUnavailable
		}
		facts[i] = observed
	}
	if facts[0].MachineIdentityCommitment == facts[1].MachineIdentityCommitment {
		return "machine_identity_reused", apiKeyDeliveryUnavailable
	}
	return "", nil
}
func apiKeyDeliveryProvisionerConfig(x apiKeyDeliverySnapshot) vault.APIKeyProvisionerConfig {
	return vault.APIKeyProvisionerConfig{Endpoint: x.Revision.Endpoint, Namespace: x.Revision.Namespace, KVMount: x.Revision.Mount, KeyPrefix: x.Revision.Prefix, AuthMount: x.Template.AuthMount, Issuer: x.Template.Issuer, JWKSURL: x.Template.JWKSURL}
}
func apiKeyDeliveryProvisionerInput(p entity.VaultAccessProfile, b entity.VaultPrincipalBinding, t entity.APIKeyCallerTemplate) vault.APIKeyProvisionerInput {
	return vault.APIKeyProvisionerInput{ProfileID: p.ID, PrincipalID: b.ID, Subject: b.Subject, Audience: t.Audience, TokenTTLSeconds: int64(t.TokenTTLSeconds), TokenMaxTTLSeconds: int64(t.TokenMaxTTLSeconds)}
}
