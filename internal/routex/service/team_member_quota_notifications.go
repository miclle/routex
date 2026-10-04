package service

import (
	"context"
	"database/sql"
	"errors"
	"github.com/miclle/routex/pkg/limits"
	"reflect"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type teamMemberQuotaTarget struct{ MembershipID, TeamID, UserID string }

func validTeamMemberQuotaTarget(target teamMemberQuotaTarget) bool {
	return safeTeamSessionID(target.MembershipID) && safeTeamSessionID(target.TeamID) && safeTeamSessionID(target.UserID)
}

func sameQuotaMemberProof(a, b entity.QuotaNotificationObservation) bool {
	equal := func(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
	return equal(a.TeamID, b.TeamID) && equal(a.MemberUserID, b.MemberUserID)
}

func validTeamMemberQuotaObservation(observation entity.QuotaNotificationObservation, teamID, userID string) bool {
	return observation.ScopeKind == "team_member" && safeTeamSessionID(teamID) && safeTeamSessionID(userID) &&
		observation.TeamID != nil && *observation.TeamID == teamID && observation.MemberUserID != nil && *observation.MemberUserID == userID &&
		observation.ScopeID == teamMemberLimitScopeID(teamID, userID)
}

func validTeamMemberQuotaInboxObservation(observation entity.QuotaNotificationObservation, access quotaInboxAccess) bool {
	if observation.TeamID == nil {
		return false
	}
	for _, teamID := range access.TeamIDs {
		if validTeamMemberQuotaObservation(observation, teamID, access.ActorID) {
			return true
		}
	}
	return false
}

func teamMemberQuotaInboxScope(tx *gorm.DB, actorID, teamID string) *gorm.DB {
	return tx.Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_kind"}, "team_member")).
		Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "scope_id"}, teamMemberLimitScopeID(teamID, actorID))).
		Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "team_id"}, teamID)).
		Where(database.ExactText(tx, clause.Column{Table: "observation", Name: "member_user_id"}, actorID))
}

func teamMemberMonthlyQuotaObservations(current *teamLimitContext, usage *eventqueue.AccountQuotaUsage) []entity.QuotaNotificationObservation {
	if current == nil || current.Member == nil || current.Row.ScopeKind != "team_member" || current.Row.ScopeID != teamMemberLimitScopeID(current.Team.ID, current.Member.UserID) ||
		!safeTeamSessionID(current.Team.ID) || !safeTeamSessionID(current.Member.UserID) || !validCatalogLabel(current.Team.Name) ||
		!validMonthlyQuotaFacts(current.Row, current.Team.CreatedAt, usage, 64) {
		return nil
	}
	policy, err := policyFromRow(current.Row)
	if err != nil || validateTeamLimitPolicy("team_member", policy) != nil {
		return nil
	}
	observations := monthlyQuotaSettledObservations(current.Row, current.Team.CreatedAt, usage, current.Pricing.PlatformCurrency)
	for i := range observations {
		// Each immutable snapshot owns its proof strings; no mutable context pointers.
		teamID, userID := current.Team.ID, current.Member.UserID
		observations[i].TeamID, observations[i].MemberUserID = &teamID, &userID
		observations[i].ScopeName = current.Team.Name
	}
	return observations
}

func (s *Service) teamMemberQuotaNotificationApplied(current *teamLimitContext, setting entity.QuotaSetting) bool {
	if current == nil || current.Member == nil || current.Actor.ID != current.Member.UserID || current.Actor.Disabled || current.Actor.OffboardedAt != nil ||
		current.Member.TeamID != current.Team.ID || current.Member.Status != entity.ResourceActive || current.Member.Role != entity.TeamMember && current.Member.Role != entity.TeamOwner ||
		!validTeamMemberQuotaTarget(teamMemberQuotaTarget{current.Member.ID, current.Team.ID, current.Member.UserID}) ||
		current.Row.ScopeKind != "team_member" || current.Row.ScopeID != teamMemberLimitScopeID(current.Team.ID, current.Member.UserID) ||
		current.Resolved.kind != current.Row.ScopeKind || current.Resolved.id != current.Row.ScopeID ||
		current.Team.Status != entity.ResourceActive || s.runtime == nil || s.recorder == nil {
		return false
	}
	auth := s.runtime.auth.Load()
	if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || setting.ETag == "" || auth.Quota.Setting.ETag != setting.ETag || auth.Quota.Setting.TimeZone != setting.TimeZone ||
		runtimeDenied(&s.runtime.deniedLimits, "quota_settings") || runtimeDenied(&s.runtime.deniedTeams, current.Team.ID) ||
		runtimeDenied(&s.runtime.deniedUsers, current.Member.UserID) || runtimeDenied(&s.runtime.deniedTeamMembers, teamMemberRuntimeKey(current.Team.ID, current.Member.UserID)) {
		return false
	}
	team, exists := auth.Teams[current.Team.ID]
	if !exists || current.Team.CreatedAt.IsZero() || !team.CreatedAt.Equal(current.Team.CreatedAt) || team.Members[current.Member.UserID] != current.Member.ID {
		return false
	}
	if current.ParentRow.ScopeKind != "team" || current.ParentRow.ScopeID != current.Team.ID {
		return false
	}
	rows := []entity.ResourceLimit{current.Row, current.ParentRow}
	policies := []limits.Policy{current.Stored, current.Parent}
	for i, row := range rows {
		account := limitAccount(row.ScopeKind, row.ScopeID)
		revision := auth.Quota.Revisions[account]
		if revision == "" {
			revision = "0"
		}
		published, err := limits.Normalize(auth.LimitPolicies[account])
		if err != nil || revision != row.ETag || !reflect.DeepEqual(published, policies[i]) || runtimeDenied(&s.runtime.deniedLimits, account) ||
			policies[i].MoneyMonth != nil && (policies[i].Currency != auth.Quota.Currency || auth.Quota.Currency != current.Pricing.PlatformCurrency) {
			return false
		}
	}
	return true
}

// The observer has no platform actor: its only recipient is the exact current
// member. Every query starts a fresh statement, including when tx carries locks.
func loadTeamMemberQuotaContext(tx *gorm.DB, target teamMemberQuotaTarget) (*teamLimitContext, error) {
	fresh := func() *gorm.DB {
		return tx.Session(&gorm.Session{NewDB: true}).Clauses(clause.Locking{Strength: "UPDATE"})
	}
	current := &teamLimitContext{}
	if err := fresh().Where(database.ExactText(tx, clause.Column{Name: "id"}, target.TeamID)).First(&current.Team).Error; err != nil {
		return nil, err
	}
	if current.Team.ID != target.TeamID || current.Team.Status != entity.ResourceActive || !validCatalogLabel(current.Team.Name) {
		return nil, gorm.ErrRecordNotFound
	}
	if err := fresh().Where(database.ExactText(tx, clause.Column{Name: "id"}, target.UserID)).First(&current.Actor).Error; err != nil {
		return nil, err
	}
	if current.Actor.ID != target.UserID || current.Actor.Disabled || current.Actor.OffboardedAt != nil {
		return nil, gorm.ErrRecordNotFound
	}
	var member entity.TeamMembership
	if err := fresh().Where(database.ExactText(tx, clause.Column{Name: "id"}, target.MembershipID)).Where(database.ExactText(tx, clause.Column{Name: "team_id"}, target.TeamID)).Where(database.ExactText(tx, clause.Column{Name: "user_id"}, target.UserID)).First(&member).Error; err != nil {
		return nil, err
	}
	if member.ID != target.MembershipID || member.TeamID != target.TeamID || member.UserID != target.UserID || member.Status != entity.ResourceActive || member.Role != entity.TeamMember && member.Role != entity.TeamOwner {
		return nil, gorm.ErrRecordNotFound
	}
	current.Member = &member
	scopeID := teamMemberLimitScopeID(target.TeamID, target.UserID)
	current.Resolved = resolvedLimitTarget{kind: "team_member", id: scopeID, teamID: target.TeamID, userID: target.UserID, parentKind: "team", parentID: target.TeamID}
	var err error
	current.Row, current.Stored, err = readTeamLimitPolicy(fresh(), "team_member", scopeID)
	if err != nil {
		return nil, err
	}
	current.ParentRow, current.Parent, err = readTeamLimitPolicy(fresh(), "team", target.TeamID)
	if err != nil {
		return nil, err
	}
	if err := tx.Session(&gorm.Session{NewDB: true}).First(&current.Pricing, 1).Error; err != nil {
		return nil, err
	}
	return current, nil
}

func (s *Service) observeTeamMemberMonthlyQuotaNotification(ctx context.Context, target teamMemberQuotaTarget) error {
	if !validTeamMemberQuotaTarget(target) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		current, err := loadTeamMemberQuotaContext(tx, target)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Row.TokensMonth == nil && current.Row.MoneyMonth == nil {
			return nil
		}
		var setting entity.QuotaSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		if !s.teamMemberQuotaNotificationApplied(current, setting) {
			return nil
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount(current.Row.ScopeKind, current.Row.ScopeID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != setting.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		for _, observation := range teamMemberMonthlyQuotaObservations(current, usage) {
			if err := persistQuotaNotification(tx, observation, []string{target.UserID}); err != nil {
				return err
			}
		}
		// The governance lock protects current SQL membership through commit, while
		// this repeated private proof rejects publication expiry or revocation.
		if !s.teamMemberQuotaNotificationApplied(current, setting) {
			return runtimeUnavailable
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
