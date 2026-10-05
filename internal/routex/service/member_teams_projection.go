package service

import (
	"strconv"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/limits"
)

type MemberTeamPolicyValues struct {
	TokensMonth *string `json:"tokens_month"`
	MoneyMonth  *string `json:"money_month"`
	Currency    *string `json:"currency"`
	RPM         *string `json:"rpm"`
	TPM         *string `json:"tpm"`
	Concurrency *string `json:"concurrency"`
}
type MemberTeamLimits struct {
	PolicyRecorded     bool                        `json:"policy_recorded"`
	PolicyETag         string                      `json:"policy_etag"`
	Stored             MemberTeamPolicyValues      `json:"stored"`
	ParentStored       MemberTeamPolicyValues      `json:"parent_stored"`
	RuntimeApplied     bool                        `json:"runtime_applied"`
	UsageStatus        string                      `json:"usage_status"`
	Usage              *OverviewAccountUsage       `json:"usage"`
	ActiveReservations *OverviewActiveReservations `json:"active_reservations"`
}

func memberTeamPolicyValues(policy limits.Policy) MemberTeamPolicyValues {
	exact := func(value *int64) *string {
		if value == nil {
			return nil
		}
		formatted := strconv.FormatInt(*value, 10)
		return &formatted
	}
	result := MemberTeamPolicyValues{TokensMonth: exact(policy.TokensMonth), MoneyMonth: policy.MoneyMonth, RPM: exact(policy.RPM), TPM: exact(policy.TPM), Concurrency: exact(policy.Concurrency)}
	if policy.MoneyMonth != nil {
		denomination := policy.Currency
		result.Currency = &denomination
	}
	return result
}
func memberTeamsTargets(rows []memberTeamIdentity, userID string) []overviewAccountTarget {
	targets := make([]overviewAccountTarget, 0, 2*len(rows))
	for _, row := range rows {
		targets = append(targets, overviewAccountTarget{kind: "team", id: row.ID, teamID: row.ID, membershipID: row.MembershipID, created: row.CreatedAt}, overviewAccountTarget{kind: "team_member", id: teamMemberLimitScopeID(row.ID, userID), teamID: row.ID, membershipID: row.MembershipID, created: row.CreatedAt})
	}
	return targets
}
func (s *Service) memberTeamsApplied(auth *runtimeAuthorization, subject entity.User, row memberTeamIdentity, target overviewAccountTarget, all []overviewAccountTarget, setting entity.QuotaSetting, currency string, applications map[string]entity.RegistrationApprovalApplication) bool {
	if auth == nil || subject.Disabled || subject.OffboardedAt != nil || subject.ID != row.MembershipUserID || subject.CreatedAt.IsZero() || row.Status != entity.ResourceActive || row.MembershipStatus != entity.ResourceActive || row.MembershipRole != entity.TeamMember && row.MembershipRole != entity.TeamOwner || !s.RuntimeStatus().Ready {
		return false
	}
	proof, exists := auth.UserProofs[subject.ID]
	return exists && s.registrationAdvisoryPublished(auth, subject, applications) && proof.Enabled && proof.CreatedAt.Equal(subject.CreatedAt) && s.memberOverviewApplied(auth, subject.ID, target, all, setting, currency)
}
