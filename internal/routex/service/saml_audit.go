package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type samlIdentityAuditChanges struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func samlAuditStableID(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix) && safeTeamSessionID(value)
}

func samlAuditBirth(value time.Time) bool {
	return !value.IsZero() && value.Nanosecond()%int(time.Microsecond) == 0
}

// Only the versioned reason is public. Remote identity claims, configuration and
// protocol material must never be reconstructed from arbitrary audit JSON.
func samlAuditProjection(row entity.AuditEvent) (samlIdentityAuditChanges, bool) {
	var result samlIdentityAuditChanges
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 || !samlAuditStableID(row.ActorID, "usr_") {
		return result, false
	}
	needsBinding := false
	switch row.Action {
	case "identity.saml.config.update", "identity.saml.status.update":
		if row.ResourceType != "saml_provider" || row.ResourceID != "saml" {
			return result, false
		}
	case "identity.saml.verify":
		if row.ResourceType != "saml_provider" || row.ResourceID != "saml" {
			return result, false
		}
		needsBinding = true
	case "account.saml.bind", "account.saml.unlink":
		if row.ResourceType != "saml_binding" || !samlAuditStableID(row.ResourceID, "smb_") {
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
	var details samlAuditDetails
	if json.Unmarshal([]byte(*row.DetailsJSON), &details) != nil || details.Version != 1 || !validRegistrationReason(details.Reason) || details.ProviderID != "saml" || !samlAuditBirth(details.ActorCreatedAt) || !samlAuditBirth(details.ProviderCreatedAt) || !validMemberRoleDigest(details.ReviewRevision) || !validMemberRoleDigest(details.ConfigRevision) || !validMemberRoleDigest(details.PolicyRevision) {
		return result, false
	}
	if needsBinding {
		if !samlAuditStableID(details.BindingID, "smb_") || details.BindingCreatedAt == nil || !samlAuditBirth(*details.BindingCreatedAt) || row.ResourceType == "saml_binding" && row.ResourceID != details.BindingID {
			return result, false
		}
	} else if details.BindingID != "" || details.BindingCreatedAt != nil {
		return result, false
	}
	return samlIdentityAuditChanges{Kind: "saml_identity", Reason: details.Reason}, true
}
