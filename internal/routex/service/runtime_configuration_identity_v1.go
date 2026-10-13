package service

import "time"

// Identity policy/binding rows are recorded source facts. Their verification
// flags and historical Session proofs confer no current login or invocation.
func configurationIdentitySourcesV1(d *runtimeConfigurationTeamSessionDataV1) bool {
	if p := d.OIDCProvider; p != nil {
		if p.ID != "oidc" || !configurationIdentityPolicyV1(p.ConfigRevision, p.PolicyRevision, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt) {
			return false
		}
	}
	if p := d.OAuthProvider; p != nil {
		if p.ID != "oauth" || !configurationIdentityPolicyV1(p.ConfigRevision, p.PolicyRevision, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt) {
			return false
		}
	}
	if p := d.LDAPProvider; p != nil {
		if p.ID != "ldap" || !configurationIdentityPolicyV1(p.ConfigRevision, p.PolicyRevision, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt) {
			return false
		}
	}
	if p := d.SAMLProvider; p != nil {
		if p.ID != "saml" || !configurationIdentityPolicyV1(p.ConfigRevision, p.PolicyRevision, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt) {
			return false
		}
	}
	for id, p := range d.NamedIdentityProviders {
		if p == nil || p.ID != id || !namedIdentityProfile(p.ID, p.ProfileID, p.IdentityIssuer) || !configurationIdentityPolicyV1(p.ConfigRevision, p.PolicyRevision, p.VerifiedConfigRevision, p.VerifiedBy, p.VerifiedUserCreatedAt, p.VerifiedBindingID, p.VerifiedBindingCreatedAt) {
			return false
		}
	}
	for _, b := range d.OIDCBindings {
		if !configurationIDV1(b.ID, "oib") || !configurationIDV1(b.UserID, "usr") || b.UserCreatedAt.IsZero() || !runtimeRouteDigest.MatchString(b.ConfigRevision) || !runtimeRouteDigest.MatchString(b.SubjectDigest) || b.Subject == "" {
			return false
		}
	}
	for _, b := range d.OAuthBindings {
		if !configurationIDV1(b.ID, "oab") || !configurationIDV1(b.UserID, "usr") || b.UserCreatedAt.IsZero() || !runtimeRouteDigest.MatchString(b.ConfigRevision) || !runtimeRouteDigest.MatchString(b.SubjectDigest) || b.Subject == "" {
			return false
		}
	}
	for _, b := range d.LDAPBindings {
		if !configurationIDV1(b.ID, "ldb") || !configurationIDV1(b.UserID, "usr") || b.UserCreatedAt.IsZero() || !runtimeRouteDigest.MatchString(b.ConfigRevision) || !runtimeRouteDigest.MatchString(b.SubjectDigest) || b.Subject == "" {
			return false
		}
	}
	for _, b := range d.SAMLBindings {
		if !configurationIDV1(b.ID, "smb") || !configurationIDV1(b.UserID, "usr") || b.UserCreatedAt.IsZero() || !runtimeRouteDigest.MatchString(b.ConfigRevision) || !runtimeRouteDigest.MatchString(b.SubjectDigest) || b.Subject == "" {
			return false
		}
	}
	for _, b := range d.NamedIdentityBindings {
		if !configurationIDV1(b.ID, "nib") || !configurationIDV1(b.UserID, "usr") || b.UserCreatedAt.IsZero() || !runtimeRouteDigest.MatchString(b.ConfigRevision) || !runtimeRouteDigest.MatchString(b.SubjectDigest) || b.Subject == "" {
			return false
		}
	}
	return true
}
func configurationIdentityPolicyV1(config, policy, verified, actor string, actorBirth *time.Time, binding string, bindingBirth *time.Time) bool {
	if !runtimeRouteDigest.MatchString(config) || !runtimeRouteDigest.MatchString(policy) {
		return false
	}
	if verified != "" && !runtimeRouteDigest.MatchString(verified) {
		return false
	}
	if actor != "" && !configurationIDV1(actor, "usr") {
		return false
	}
	if binding != "" && !configurationCanonicalIDV1.MatchString(binding) {
		return false
	}
	return (actorBirth == nil || !actorBirth.IsZero()) && (bindingBirth == nil || !bindingBirth.IsZero())
}
