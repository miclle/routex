package service

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type registrationDecisionAudit struct {
	UserID        string `json:"user_id"`
	ApplicationID string `json:"application_id"`
	Before        string `json:"before"`
	After         string `json:"after"`
	Reason        string `json:"reason"`
}
type registrationPolicyAuditValue struct {
	Enabled          bool `json:"enabled"`
	ApprovalRequired bool `json:"approval_required"`
}
type registrationPolicyAudit struct {
	Before registrationPolicyAuditValue `json:"before"`
	After  registrationPolicyAuditValue `json:"after"`
	Reason string                       `json:"reason"`
}

// V2 explicitly retains domain policy; V1 keeps its original two-field projection.
type registrationDomainPolicyAuditValue struct {
	Enabled             bool     `json:"enabled"`
	ApprovalRequired    bool     `json:"approval_required"`
	AllowedEmailDomains []string `json:"allowed_email_domains"`
}
type registrationDomainPolicyAudit struct {
	Version int                                `json:"version"`
	Before  registrationDomainPolicyAuditValue `json:"before"`
	After   registrationDomainPolicyAuditValue `json:"after"`
	Reason  string                             `json:"reason"`
}

func registrationDomainPolicyAuditValid(v registrationDomainPolicyAudit) bool {
	if v.Version != 2 || !validRegistrationReason(v.Reason) {
		return false
	}
	for _, value := range []registrationDomainPolicyAuditValue{v.Before, v.After} {
		canonical, err := canonicalRegistrationDomains(value.AllowedEmailDomains)
		if err != nil || !slices.Equal(canonical, value.AllowedEmailDomains) {
			return false
		}
	}
	return v.Before.Enabled != v.After.Enabled || v.Before.ApprovalRequired != v.After.ApprovalRequired || !slices.Equal(v.Before.AllowedEmailDomains, v.After.AllowedEmailDomains)
}
func appendRegistrationAudit(tx *gorm.DB, actor, action, resource, resourceID string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return appendRegistrationAuditRaw(tx, actor, action, resource, resourceID, raw)
}
func appendRegistrationAuditRaw(tx *gorm.DB, actor, action, resource, resourceID string, raw []byte) error {
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail := string(raw)
	return tx.Session(&gorm.Session{NewDB: true}).Create(&entity.AuditEvent{ID: auditID, ActorID: actor, Action: action, ResourceType: resource, ResourceID: resourceID, DetailsJSON: &detail}).Error
}
func appendRegistrationDecisionAudit(tx *gorm.DB, actor, user, application, before, after, reason string) error {
	return appendRegistrationAudit(tx, actor, "member.approval.decide", "user", user, registrationDecisionAudit{user, application, before, after, reason})
}

// HTML escaping is unnecessary for typed JSON strings and can expand a valid
// maximum reason sixfold. V2 uses ordinary JSON escaping to retain the 8KiB cap;
// clients still render all recorded text as escaped text, never raw HTML.
func registrationDomainAuditJSON(value registrationDomainPolicyAudit) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}
func appendRegistrationPolicyAudit(tx *gorm.DB, actor string, b entity.GovernanceSetting, a RegistrationPolicyInput) error {
	domains, err := registrationStoredDomains(b.RegistrationAllowedEmailDomains)
	if err != nil {
		return err
	}
	value := registrationDomainPolicyAudit{Version: 2, Before: registrationDomainPolicyAuditValue{b.RegistrationEnabled, b.RegistrationApprovalRequired, domains}, After: registrationDomainPolicyAuditValue{a.Enabled, a.ApprovalRequired, slices.Clone(a.AllowedEmailDomains)}, Reason: a.Reason}
	raw, err := registrationDomainAuditJSON(value)
	if err != nil || len(raw) > 8192 || !registrationDomainPolicyAuditValid(value) {
		return registrationApprovalUnavailable
	}
	return appendRegistrationAuditRaw(tx, actor, "registration.policy.update", "registration", "1", raw)
}
func registrationApprovalAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 8192 {
		return nil, false
	}
	raw := []byte(*row.DetailsJSON)
	switch row.Action {
	case "member.approval.decide":
		if row.ResourceType != "user" || len(raw) > 4096 {
			return nil, false
		}
		if _, err := registrationStrictObject(raw, []string{"user_id", "application_id", "before", "after", "reason"}); err != nil {
			return nil, false
		}
		var v registrationDecisionAudit
		if json.Unmarshal(raw, &v) != nil || v.UserID != row.ResourceID || !safeTeamSessionID(v.UserID) || !registrationApplicationID(v.ApplicationID) || v.Before != "pending" || v.After != "approved" && v.After != "rejected" || !validRegistrationReason(v.Reason) {
			return nil, false
		}
		return v, true
	case "registration.policy.update":
		if row.ResourceType != "registration" || row.ResourceID != "1" {
			return nil, false
		}
		// The field sets are versioned explicitly; arbitrary historical JSON stays private.
		if f, err := registrationStrictObject(raw, []string{"version", "before", "after", "reason"}); err == nil {
			for _, name := range []string{"before", "after"} {
				if _, err := registrationStrictObject(f[name], []string{"enabled", "approval_required", "allowed_email_domains"}); err != nil {
					return nil, false
				}
			}
			var value registrationDomainPolicyAudit
			if json.Unmarshal(raw, &value) != nil || !registrationDomainPolicyAuditValid(value) {
				return nil, false
			}
			return value, true
		}
		if len(raw) > 4096 {
			return nil, false
		}
		f, err := registrationStrictObject(raw, []string{"before", "after", "reason"})
		if err != nil {
			return nil, false
		}
		for _, name := range []string{"before", "after"} {
			if _, err := registrationStrictObject(f[name], []string{"enabled", "approval_required"}); err != nil {
				return nil, false
			}
		}
		var value registrationPolicyAudit
		if json.Unmarshal(raw, &value) != nil || value.Before == value.After || !validRegistrationReason(value.Reason) {
			return nil, false
		}
		return value, true

	default:
		return nil, false
	}
}
