package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

type ldapIdentityAuditChanges struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func ldapAuditStableID(value, prefix string) bool {
	return len(value) > len(prefix) && strings.HasPrefix(value, prefix) && safeTeamSessionID(value)
}

func ldapAuditBirth(value time.Time) bool {
	return !value.IsZero() && value.Nanosecond()%int(time.Microsecond) == 0
}

// Only the versioned reason is public. Remote identity claims, configuration and
// protocol material must never be reconstructed from arbitrary audit JSON.
func ldapAuditProjection(row entity.AuditEvent) (ldapIdentityAuditChanges, bool) {
	var result ldapIdentityAuditChanges
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 || !ldapAuditStableID(row.ActorID, "usr_") {
		return result, false
	}
	needsBinding := false
	switch row.Action {
	case "identity.ldap.config.update", "identity.ldap.status.update":
		if row.ResourceType != "ldap_provider" || row.ResourceID != "ldap" {
			return result, false
		}
	case "identity.ldap.verify":
		if row.ResourceType != "ldap_provider" || row.ResourceID != "ldap" {
			return result, false
		}
		needsBinding = true
	case "account.ldap.bind", "account.ldap.unlink":
		if row.ResourceType != "ldap_binding" || !ldapAuditStableID(row.ResourceID, "ldb_") {
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
	var details ldapAuditDetails
	if json.Unmarshal([]byte(*row.DetailsJSON), &details) != nil || details.Version != 1 || !validRegistrationReason(details.Reason) || details.ProviderID != "ldap" || !ldapAuditBirth(details.ActorCreatedAt) || !ldapAuditBirth(details.ProviderCreatedAt) || !validMemberRoleDigest(details.ReviewRevision) || !validMemberRoleDigest(details.ConfigRevision) || !validMemberRoleDigest(details.PolicyRevision) {
		return result, false
	}
	if needsBinding {
		if !ldapAuditStableID(details.BindingID, "ldb_") || details.BindingCreatedAt == nil || !ldapAuditBirth(*details.BindingCreatedAt) || row.ResourceType == "ldap_binding" && row.ResourceID != details.BindingID {
			return result, false
		}
	} else if details.BindingID != "" || details.BindingCreatedAt != nil {
		return result, false
	}
	return ldapIdentityAuditChanges{Kind: "ldap_identity", Reason: details.Reason}, true
}
