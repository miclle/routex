package service

import (
	"encoding/json"
	"slices"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type roleDefinitionAuditValues struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

type roleDefinitionAuditChanges struct {
	RoleID string                    `json:"role_id"`
	Reason string                    `json:"reason"`
	Before roleDefinitionAuditValues `json:"before"`
	After  roleDefinitionAuditValues `json:"after"`
}

func validRoleDefinitionAuditValues(value roleDefinitionAuditValues) bool {
	if !memberAccessLabel(value.Name) || value.Permissions == nil || len(value.Permissions) > roleDefinitionPermissionBudget || !slices.IsSorted(value.Permissions) {
		return false
	}
	for i, code := range value.Permissions {
		if !memberRolePermissionCode(code) || i > 0 && value.Permissions[i-1] == code {
			return false
		}
	}
	return true
}

func encodeRoleDefinitionAudit(roleID, reason string, before, after roleDefinitionAuditValues) (string, error) {
	if !memberRoleID(roleID) || !validRoleDefinitionReason(reason) || !validRoleDefinitionAuditValues(before) || !validRoleDefinitionAuditValues(after) {
		return "", roleDefinitionUnavailable
	}
	raw, err := json.Marshal(roleDefinitionAuditChanges{RoleID: roleID, Reason: reason, Before: before, After: after})
	if err != nil {
		return "", err
	}
	if len(raw) > roleDefinitionAuditBudget {
		return "", roleDefinitionOverflow
	}
	return string(raw), nil
}

func appendRoleDefinitionAudit(tx *gorm.DB, actorID, roleID, raw string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	return memberRolesDB(tx).Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "role.definition.update", ResourceType: "role", ResourceID: roleID, DetailsJSON: &raw}).Error
}

func roleDefinitionAuditProjection(row entity.AuditEvent) (roleDefinitionAuditChanges, bool) {
	var changes roleDefinitionAuditChanges
	if row.Action != "role.definition.update" || row.ResourceType != "role" || row.DetailsJSON == nil || len(*row.DetailsJSON) > roleDefinitionAuditBudget || !memberRoleID(row.ResourceID) || !utf8.ValidString(*row.DetailsJSON) || !modelCreationUnicode([]byte(*row.DetailsJSON)) {
		return changes, false
	}
	fields, err := memberRolesObject([]byte(*row.DetailsJSON))
	if err != nil || len(fields) != 4 {
		return changes, false
	}
	for _, key := range []string{"role_id", "reason", "before", "after"} {
		if _, ok := fields[key]; !ok {
			return changes, false
		}
	}
	for _, key := range []string{"before", "after"} {
		values, err := memberRolesObject(fields[key])
		if err != nil || len(values) != 2 || values["name"] == nil || values["permissions"] == nil {
			return changes, false
		}
	}
	if json.Unmarshal([]byte(*row.DetailsJSON), &changes) != nil || changes.RoleID != row.ResourceID || !validRoleDefinitionReason(changes.Reason) || !validRoleDefinitionAuditValues(changes.Before) || !validRoleDefinitionAuditValues(changes.After) {
		return changes, false
	}
	return changes, true
}
