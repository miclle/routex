package service

import (
	"encoding/base32"
)

// Scope IDs are complete SHA256 pair digests. Syntax is not authority; callers
// still resolve exact current Team/User relationships before reading or admission.
func canonicalTeamMemberScopeID(value string) bool {
	codec := base32.StdEncoding.WithPadding(base32.NoPadding)
	raw, err := codec.DecodeString(value)
	return err == nil && len(raw) == 32 && codec.EncodeToString(raw) == value
}

// Team admission has already reauthorized the request-local Session. Bind soft
// behavior to the current exact pair, membership and original Team birth again.
func teamMemberQuotaProof(auth *runtimeAuthorization, result *GatewayResult, account string) bool {
	if auth == nil || auth.Quota == nil || result == nil || result.identity.team == nil || result.ProjectID != "" || result.KeyID != "" {
		return false
	}
	identity := result.identity.team
	if !safeTeamSessionID(result.TeamID) || !safeTeamSessionID(result.UserID) || !safeTeamSessionID(result.TeamMembershipID) || identity.TeamID != result.TeamID || identity.UserID != result.UserID || identity.TeamMembershipID != result.TeamMembershipID || identity.teamCreatedAt.IsZero() {
		return false
	}
	team, ok := auth.Teams[result.TeamID]
	expected := teamMemberLimitAccount(result.TeamID, result.UserID)
	return ok && team.Members[result.UserID] == result.TeamMembershipID && auth.UserAdmissions[result.UserID].Eligible && team.CreatedAt.Equal(identity.teamCreatedAt) && auth.Quota.Created[expected].Equal(team.CreatedAt) && account == expected
}
