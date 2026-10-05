package service

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type memberStateAuditValue struct {
	BaseRole     string     `json:"base_role"`
	Disabled     bool       `json:"disabled"`
	OffboardedAt *time.Time `json:"offboarded_at"`
}
type memberStateAudit struct {
	UserID    string                `json:"user_id"`
	Operation string                `json:"operation"`
	Reason    string                `json:"reason"`
	Before    memberStateAuditValue `json:"before"`
	After     memberStateAuditValue `json:"after"`
}

func memberStateAuditDetails(before, after entity.User, input MemberStateInput) memberStateAudit {
	operation := "base_role"
	if input.Disabled != nil {
		operation = "enable"
		if after.Disabled {
			operation = "disable"
		} else if before.OffboardedAt != nil {
			operation = "reactivate"
		}
	}
	return memberStateAudit{before.ID, operation, input.Reason, memberStateAuditValue{before.Role, before.Disabled, utcMemberMetadataTime(before.OffboardedAt)}, memberStateAuditValue{after.Role, after.Disabled, utcMemberMetadataTime(after.OffboardedAt)}}
}
func appendMemberStateAudit(tx *gorm.DB, actorID string, before, after entity.User, input MemberStateInput) error {
	raw, err := json.Marshal(memberStateAuditDetails(before, after, input))
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	detail := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: "member.state.update", ResourceType: "user", ResourceID: before.ID, DetailsJSON: &detail}).Error
}
func memberStateAuditProjection(row entity.AuditEvent) (memberStateAudit, bool) {
	var result memberStateAudit
	if row.Action != "member.state.update" || row.ResourceType != "user" || !safeTeamSessionID(row.ResourceID) || row.DetailsJSON == nil || len(*row.DetailsJSON) > 64*1024 {
		return result, false
	}
	raw := []byte(*row.DetailsJSON)
	if !utf8.Valid(raw) || !modelCreationUnicode(raw) {
		return result, false
	}
	fields, err := decodeDefaultLimitObject(raw, []string{"user_id", "operation", "reason", "before", "after"})
	if err != nil {
		return result, false
	}
	for _, field := range []string{"before", "after"} {
		values, err := decodeDefaultLimitObject(fields[field], []string{"base_role", "disabled", "offboarded_at"})
		if err != nil {
			return result, false
		}
		if string(values["base_role"]) == "null" || string(values["disabled"]) == "null" {
			return result, false
		}
	}
	if json.Unmarshal(raw, &result) != nil || result.UserID != row.ResourceID || !validCredentialMetadataReason(result.Reason) || strings.TrimSpace(result.Reason) != result.Reason {
		return result, false
	}
	valid := func(v memberStateAuditValue) bool {
		return (v.BaseRole == entity.RoleAdmin || v.BaseRole == entity.RoleMember) && (v.OffboardedAt == nil || v.Disabled && !v.OffboardedAt.IsZero())
	}
	if !valid(result.Before) || !valid(result.After) {
		return result, false
	}
	switch result.Operation {
	case "base_role":
		return result, result.Before.BaseRole != result.After.BaseRole && result.Before.Disabled == result.After.Disabled && memberStateTimesEqual(result.Before.OffboardedAt, result.After.OffboardedAt)
	case "disable":
		return result, !result.Before.Disabled && result.After.Disabled && result.Before.BaseRole == result.After.BaseRole && memberStateTimesEqual(result.Before.OffboardedAt, result.After.OffboardedAt)
	case "enable":
		return result, result.Before.Disabled && !result.After.Disabled && result.Before.OffboardedAt == nil && result.After.OffboardedAt == nil && result.Before.BaseRole == result.After.BaseRole
	case "reactivate":
		return result, result.Before.Disabled && !result.After.Disabled && result.Before.OffboardedAt != nil && result.After.OffboardedAt == nil && result.Before.BaseRole == result.After.BaseRole
	default:
		return result, false
	}
}
func memberStateTimesEqual(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}
