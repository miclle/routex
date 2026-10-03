package service

import (
	"encoding/json"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
)

type teamRoleAuditValues struct {
	RoleIDs     []string `json:"role_ids"`
	TeamActions []string `json:"team_actions"`
}

func validTeamRoleAuditValues(value teamRoleAuditValues) bool {
	if value.RoleIDs == nil || value.TeamActions == nil || len(value.RoleIDs) > 100 || len(value.TeamActions) > 2 {
		return false
	}
	seen := make(map[string]bool, len(value.RoleIDs))
	for _, roleID := range value.RoleIDs {
		if !safeTeamSessionID(roleID) || !strings.HasPrefix(roleID, "rol_") || seen[roleID] {
			return false
		}
		seen[roleID] = true
	}
	seen = make(map[string]bool, len(value.TeamActions))
	for _, action := range value.TeamActions {
		if action != "teams.write" && action != "teams.models.write" || seen[action] {
			return false
		}
		seen[action] = true
	}
	return len(value.RoleIDs) != 0 || len(value.TeamActions) == 0
}

func teamRoleAuditProjection(row entity.AuditEvent) (any, bool) {
	var detail struct {
		TeamID string              `json:"team_id"`
		Before teamRoleAuditValues `json:"before"`
		After  teamRoleAuditValues `json:"after"`
		Reason string              `json:"reason"`
	}
	if row.DetailsJSON == nil || row.Action != "team.roles.replace" || row.ResourceType != "teams" ||
		json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
		!safeTeamSessionID(detail.TeamID) || !strings.HasPrefix(detail.TeamID, "tea_") || detail.TeamID != row.ResourceID ||
		!validTeamRoleAuditValues(detail.Before) || !validTeamRoleAuditValues(detail.After) ||
		strings.TrimSpace(detail.Reason) != detail.Reason || !validCredentialMetadataReason(detail.Reason) {
		return nil, false
	}
	return detail, true
}
