package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type namedIdentityIdentityAuditChanges struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func namedIdentityAuditStableID(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix) && safeTeamSessionID(value)
}

func namedIdentityAuditBirth(value time.Time) bool {
	return !value.IsZero() && value.Nanosecond()%int(time.Microsecond) == 0
}

// Only the versioned reason is public. Remote identity claims, configuration and
// protocol material must never be reconstructed from arbitrary audit JSON.
func namedIdentityAuditProjection(row entity.AuditEvent) (namedIdentityIdentityAuditChanges, bool) {
	var result namedIdentityIdentityAuditChanges
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 || !namedIdentityAuditStableID(row.ActorID, "usr_") {
		return result, false
	}
	providerID := ""
	for _, id := range []string{githubProviderID, googleProviderID} {
		if strings.HasPrefix(row.Action, "identity."+id+".") || strings.HasPrefix(row.Action, "account."+id+".") {
			providerID = id
		}
	}
	if providerID == "" {
		return result, false
	}
	needsBinding := false
	switch row.Action {
	case "identity.github.config.update", "identity.github.status.update", "identity.google.config.update", "identity.google.status.update":
		if row.ResourceType != "named_identity_provider" || row.ResourceID != providerID {
			return result, false
		}
	case "identity.github.verify", "identity.google.verify":
		if row.ResourceType != "named_identity_provider" || row.ResourceID != providerID {
			return result, false
		}
		needsBinding = true
	case "account.github.bind", "account.github.unlink", "account.google.bind", "account.google.unlink":
		if row.ResourceType != "named_identity_binding" || !namedIdentityAuditStableID(row.ResourceID, "nib_") {
			return result, false
		}
		needsBinding = true
	default:
		return result, false
	}
	names := []string{"version", "reason", "profile_id", "identity_issuer", "actor_created_at", "provider_id", "provider_created_at", "review_revision", "config_revision", "policy_revision"}
	if needsBinding {
		names = append(names, "binding_id", "binding_created_at")
	}
	if _, err := registrationStrictObject([]byte(*row.DetailsJSON), names); err != nil {
		return result, false
	}
	var details namedIdentityAuditDetails
	if json.Unmarshal([]byte(*row.DetailsJSON), &details) != nil || details.ProviderID != providerID || details.Version != 1 || !validRegistrationReason(details.Reason) || !namedIdentityProfile(details.ProviderID, details.ProfileID, details.IdentityIssuer) || !namedIdentityAuditBirth(details.ActorCreatedAt) || !namedIdentityAuditBirth(details.ProviderCreatedAt) || !validMemberRoleDigest(details.ReviewRevision) || !validMemberRoleDigest(details.ConfigRevision) || !validMemberRoleDigest(details.PolicyRevision) {
		return result, false
	}
	if needsBinding {
		if !namedIdentityAuditStableID(details.BindingID, "nib_") || details.BindingCreatedAt == nil || !namedIdentityAuditBirth(*details.BindingCreatedAt) || row.ResourceType == "named_identity_binding" && row.ResourceID != details.BindingID {
			return result, false
		}
	} else if details.BindingID != "" || details.BindingCreatedAt != nil {
		return result, false
	}
	return namedIdentityIdentityAuditChanges{Kind: providerID + "_identity", Reason: details.Reason}, true
}
