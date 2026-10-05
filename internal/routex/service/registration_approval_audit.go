package service

import (
	"encoding/json"
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

func appendRegistrationAudit(tx *gorm.DB, actor, action, resource, resourceID string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
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
func appendRegistrationPolicyAudit(tx *gorm.DB, actor string, b entity.GovernanceSetting, a RegistrationPolicyInput) error {
	return appendRegistrationAudit(tx, actor, "registration.policy.update", "registration", "1", registrationPolicyAudit{registrationPolicyAuditValue{b.RegistrationEnabled, b.RegistrationApprovalRequired}, registrationPolicyAuditValue{a.Enabled, a.ApprovalRequired}, a.Reason})
}
func registrationApprovalAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON == nil || len(*row.DetailsJSON) > 4096 {
		return nil, false
	}
	raw := []byte(*row.DetailsJSON)
	switch row.Action {
	case "member.approval.decide":
		if row.ResourceType != "user" {
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
		f, err := registrationStrictObject(raw, []string{"before", "after", "reason"})
		if err != nil {
			return nil, false
		}
		for _, n := range []string{"before", "after"} {
			if _, err := registrationStrictObject(f[n], []string{"enabled", "approval_required"}); err != nil {
				return nil, false
			}
		}
		var v registrationPolicyAudit
		if json.Unmarshal(raw, &v) != nil || v.Before == v.After || !validRegistrationReason(v.Reason) {
			return nil, false
		}
		return v, true
	default:
		return nil, false
	}
}
