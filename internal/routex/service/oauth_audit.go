package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type oauthIdentityAuditChanges struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func oauthAuditStableID(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix) && safeTeamSessionID(value)
}

func oauthAuditBirth(value time.Time) bool {
	return !value.IsZero() && value.Nanosecond()%int(time.Microsecond) == 0
}

// Only the versioned reason is public. Remote identity claims, configuration and
// protocol material must never be reconstructed from arbitrary audit JSON.
func oauthAuditProjection(row entity.AuditEvent) (oauthIdentityAuditChanges, bool) {
	var result oauthIdentityAuditChanges
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 || !oauthAuditStableID(row.ActorID, "usr_") {
		return result, false
	}
	needsBinding := false
	switch row.Action {
	case "identity.oauth.config.update", "identity.oauth.status.update":
		if row.ResourceType != "oauth_provider" || row.ResourceID != "oauth" {
			return result, false
		}
	case "identity.oauth.verify":
		if row.ResourceType != "oauth_provider" || row.ResourceID != "oauth" {
			return result, false
		}
		needsBinding = true
	case "account.oauth.bind", "account.oauth.unlink":
		if row.ResourceType != "oauth_binding" || !oauthAuditStableID(row.ResourceID, "oab_") {
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
	var details oauthAuditDetails
	if json.Unmarshal([]byte(*row.DetailsJSON), &details) != nil || details.Version != 1 || !validRegistrationReason(details.Reason) || details.ProviderID != "oauth" || !oauthAuditBirth(details.ActorCreatedAt) || !oauthAuditBirth(details.ProviderCreatedAt) || !validMemberRoleDigest(details.ReviewRevision) || !validMemberRoleDigest(details.ConfigRevision) || !validMemberRoleDigest(details.PolicyRevision) {
		return result, false
	}
	if needsBinding {
		if !oauthAuditStableID(details.BindingID, "oab_") || details.BindingCreatedAt == nil || !oauthAuditBirth(*details.BindingCreatedAt) || row.ResourceType == "oauth_binding" && row.ResourceID != details.BindingID {
			return result, false
		}
	} else if details.BindingID != "" || details.BindingCreatedAt != nil {
		return result, false
	}
	return oauthIdentityAuditChanges{Kind: "oauth_identity", Reason: details.Reason}, true
}
