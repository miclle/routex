package service

import (
	"context"
	"slices"

	"github.com/miclle/routex/pkg/vault"
)

// WithNativeAPIKeyDeliveryValidator installs only the dedicated two-request
// machine validator. It grants no Provider Reader, caller login or KV operation.
func WithNativeAPIKeyDeliveryValidator(allowPrivate bool) Option {
	return WithAPIKeyDeliveryValidator(nativeAPIKeyDeliveryValidator{allowPrivate: allowPrivate})
}

type nativeAPIKeyDeliveryValidator struct{ allowPrivate bool }

func (v nativeAPIKeyDeliveryValidator) Validate(ctx context.Context, p APIKeyDeliveryValidationPlan, token string) (APIKeyDeliveryValidationFacts, error) {
	facts := APIKeyDeliveryValidationFacts{}
	if ctx == nil || ctx.Err() != nil {
		return facts, apiKeyDeliveryUnavailable
	}
	captured := p.SHA256
	p.SHA256 = ""
	if !personalModelETag(captured) || apiKeyDeliveryDigest("api-key-delivery:validation-plan:v1", p) != captured {
		return facts, apiKeyDeliveryUnavailable
	}
	p.SHA256 = captured
	client, err := vault.NewAPIKeyMachineValidator(vault.APIKeyMachineValidatorConfig{Endpoint: p.Endpoint, Namespace: p.Namespace, KVMount: p.KVMount, KeyPrefix: p.KeyPrefix, AuthMount: p.AuthMount}, v.allowPrivate)
	if err != nil {
		return facts, apiKeyDeliveryUnavailable
	}
	defer client.Close()
	resource := p.ResourceID
	if p.Purpose == "provisioner" {
		resource = ""
	}
	prepared, err := client.Prepare(vault.APIKeyMachineValidationInput{Purpose: vault.APIKeyMachinePurpose(p.Purpose), ProfileID: p.ProfileID, PrincipalID: p.PrincipalID, ResourceID: resource})
	if err != nil {
		return facts, apiKeyDeliveryUnavailable
	}
	defer prepared.Close()
	generated := prepared.Plan()
	// The trusted typed tuple and the SDK's independently generated finite paths
	// must agree before either metadata request. No path parser or override exists.
	if len(generated.Grants) != len(p.Capabilities) {
		return facts, apiKeyDeliveryUnavailable
	}
	for i, grant := range generated.Grants {
		if grant.Path != p.Capabilities[i].Path || !slices.Equal(grant.Actions, p.Capabilities[i].Actions) {
			return facts, apiKeyDeliveryUnavailable
		}
	}
	observed, err := client.Validate(ctx, token, prepared)
	facts.CloseState = client.ResponseCloseState()
	if err != nil || ctx.Err() != nil || !observed.Consumed || !observed.Validated || !observed.Lookup.Succeeded || !observed.CapabilityCheck.Succeeded || observed.Plan.Purpose != generated.Purpose || observed.Plan.SHA256 != generated.SHA256 {
		return facts, apiKeyDeliveryUnavailable
	}
	facts.PlanSHA256 = captured
	facts.OwnIdentityObserved = true
	facts.TTLSeconds = observed.TTLSeconds
	facts.TokenType = observed.TokenType
	facts.ExpiresAt = observed.ExpiresAt
	facts.MachineIdentityCommitment = observed.MachineIdentityCommitment
	facts.Capabilities = make([]APIKeyDeliveryCapability, len(observed.Grants))
	for i, grant := range observed.Grants {
		facts.Capabilities[i] = APIKeyDeliveryCapability{Path: grant.Path, Actions: slices.Clone(grant.Actions)}
	}
	if !apiKeyDeliveryValidationMatches(p, facts) {
		return APIKeyDeliveryValidationFacts{CloseState: facts.CloseState}, apiKeyDeliveryUnavailable
	}
	return facts, nil
}
