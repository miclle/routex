package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type oidcIdentityAuditChanges struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func oidcAuditStableID(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix) && safeTeamSessionID(value)
}

func oidcAuditBirth(value time.Time) bool {
	return !value.IsZero() && value.Nanosecond()%int(time.Microsecond) == 0
}

// Only the versioned reason is public. Remote identity claims, configuration and
// protocol material must never be reconstructed from arbitrary audit JSON.
func oidcAuditProjection(row entity.AuditEvent) (oidcIdentityAuditChanges, bool) {
	var result oidcIdentityAuditChanges
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 || !oidcAuditStableID(row.ActorID, "usr_") {
		return result, false
	}
	needsBinding := false
	switch row.Action {
	case "identity.oidc.config.update", "identity.oidc.status.update":
		if row.ResourceType != "oidc_provider" || row.ResourceID != "oidc" {
			return result, false
		}
	case "identity.oidc.verify":
		if row.ResourceType != "oidc_provider" || row.ResourceID != "oidc" {
			return result, false
		}
		needsBinding = true
	case "account.oidc.bind", "account.oidc.unlink":
		if row.ResourceType != "oidc_binding" || !oidcAuditStableID(row.ResourceID, "oib_") {
			return result, false
		}
		needsBinding = true
	default:
		return result, false
	}
	names := []string{"version", "reason", "actor_created_at", "provider_id", "provider_created_at", "review_revision", "config_revision", "policy_revision"}
	if needsBinding {
		names = append(names, "binding_id", "binding_created_at")
	}
	if _, err := registrationStrictObject([]byte(*row.DetailsJSON), names); err != nil {
		return result, false
	}
	var details oidcAuditDetails
	if json.Unmarshal([]byte(*row.DetailsJSON), &details) != nil || details.Version != 1 || !validRegistrationReason(details.Reason) || details.ProviderID != "oidc" || !oidcAuditBirth(details.ActorCreatedAt) || !oidcAuditBirth(details.ProviderCreatedAt) || !validMemberRoleDigest(details.ReviewRevision) || !validMemberRoleDigest(details.ConfigRevision) || !validMemberRoleDigest(details.PolicyRevision) {
		return result, false
	}
	if needsBinding {
		if !oidcAuditStableID(details.BindingID, "oib_") || details.BindingCreatedAt == nil || !oidcAuditBirth(*details.BindingCreatedAt) || row.ResourceType == "oidc_binding" && row.ResourceID != details.BindingID {
			return result, false
		}
	} else if details.BindingID != "" || details.BindingCreatedAt != nil {
		return result, false
	}
	return oidcIdentityAuditChanges{Kind: "oidc_identity", Reason: details.Reason}, true
}
