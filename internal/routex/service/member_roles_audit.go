package service

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type memberRolesAuditValues struct {
	IdentityRole         string   `json:"identity_role"`
	RoleIDs              []string `json:"role_ids"`
	EffectivePermissions []string `json:"effective_permissions"`
}
type memberRolesAuditChanges struct {
	UserID string                 `json:"user_id"`
	Reason string                 `json:"reason"`
	Before memberRolesAuditValues `json:"before"`
	After  memberRolesAuditValues `json:"after"`
}

func memberRolesAuditValuesFor(snapshot *memberRolesSnapshot) memberRolesAuditValues {
	return memberRolesAuditValues{snapshot.Subject.Role, slices.Clone(snapshot.Assigned), slices.Clone(snapshot.Page.EffectivePermissions)}
}
func encodeMemberRolesAudit(userID, reason string, before, after memberRolesAuditValues) (string, error) {
	raw, err := json.Marshal(memberRolesAuditChanges{userID, reason, before, after})
	if err != nil {
		return "", err
	}
	if len(raw) > memberRolesAuditBudget {
		return "", memberRolesOverflow
	}
	return string(raw), nil
}
func appendMemberRolesAudit(tx *gorm.DB, actorID, userID, raw string) error {
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	return memberRolesDB(tx).Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "member.roles.update", ResourceType: "user", ResourceID: userID, DetailsJSON: &raw}).Error
}
func validMemberRolesAuditValues(value memberRolesAuditValues, bound int) bool {
	if value.IdentityRole != entity.RoleAdmin && value.IdentityRole != entity.RoleMember || value.RoleIDs == nil || len(value.RoleIDs) > bound || !slices.IsSorted(value.RoleIDs) || value.EffectivePermissions == nil || len(value.EffectivePermissions) > len(AvailablePermissions) || !slices.IsSorted(value.EffectivePermissions) {
		return false
	}
	for i, id := range value.RoleIDs {
		if !memberRoleID(id) || id == "rol_admin" || id == "rol_member" || i > 0 && value.RoleIDs[i-1] == id {
			return false
		}
	}
	for i, p := range value.EffectivePermissions {
		if !slices.Contains(AvailablePermissions, p) || i > 0 && value.EffectivePermissions[i-1] == p {
			return false
		}
	}
	return true
}
func memberRolesAuditProjection(row entity.AuditEvent) (memberRolesAuditChanges, bool) {
	var changes memberRolesAuditChanges
	if row.Action != "member.roles.update" || row.ResourceType != "user" || row.DetailsJSON == nil || len(*row.DetailsJSON) > memberRolesAuditBudget || !safeTeamSessionID(row.ResourceID) {
		return changes, false
	}
	if json.Unmarshal([]byte(*row.DetailsJSON), &changes) != nil || strings.TrimSpace(changes.Reason) != changes.Reason || changes.UserID != row.ResourceID || !validCredentialMetadataReason(changes.Reason) || !validMemberRolesAuditValues(changes.Before, memberRolesWriteBeforeBudget) || !validMemberRolesAuditValues(changes.After, memberRolesWriteBudget) || changes.Before.IdentityRole != changes.After.IdentityRole {
		return changes, false
	}
	return changes, true
}
