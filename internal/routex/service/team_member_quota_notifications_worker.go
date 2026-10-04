package service

import (
	"context"
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamMemberQuotaCandidatesQuery(tx *gorm.DB, after string) *gorm.DB {
	query := quotaTeamQuery(tx)
	if after != "" {
		query = query.Where("member.id > ?", after)
	}
	return query.Order("member.id").Limit(quotaNotificationBatchSize)
}

func teamMemberQuotaPoliciesQuery(tx *gorm.DB, candidates []quotaTeamIdentity) *gorm.DB {
	var scopes []clause.Expression
	for _, candidate := range candidates {
		if validQuotaTeamMember(candidate, "", "") {
			scopes = append(scopes, database.ExactText(tx, clause.Column{Name: "scope_id"}, teamMemberLimitScopeID(candidate.TeamID, candidate.UserID)))
		}
	}
	if len(scopes) == 0 {
		scopes = append(scopes, clause.IN{Column: clause.Column{Name: "scope_id"}})
	}
	return tx.Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "team_member")).
		Where(clause.Or(scopes...)).Where("tokens_month IS NOT NULL OR money_month IS NOT NULL")
}

func (s *Service) reconcileTeamMemberQuotaNotificationBatch(ctx context.Context, cursor *string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	var candidates []quotaTeamIdentity
	db := s.authDB(ctx)
	if err := teamMemberQuotaCandidatesQuery(db, *cursor).Scan(&candidates).Error; err != nil {
		return false, err
	}
	var policies []entity.ResourceLimit
	valid := false
	for _, candidate := range candidates {
		valid = valid || validQuotaTeamMember(candidate, "", "")
	}
	if valid {
		if err := teamMemberQuotaPoliciesQuery(db, candidates).Find(&policies).Error; err != nil {
			return false, err
		}
	}
	return reconcileTeamMemberQuotaNotificationRows(ctx, candidates, policies, cursor, s.observeTeamMemberMonthlyQuotaNotification)
}

func reconcileTeamMemberQuotaNotificationRows(ctx context.Context, candidates []quotaTeamIdentity, policies []entity.ResourceLimit, cursor *string, observe func(context.Context, teamMemberQuotaTarget) error) (bool, error) {
	if len(candidates) == 0 {
		*cursor = ""
		return true, nil
	}
	finite := make(map[string]bool, len(policies))
	for _, policy := range policies {
		if policy.ScopeKind == "team_member" && (policy.TokensMonth != nil || policy.MoneyMonth != nil) {
			finite[policy.ScopeID] = true
		}
	}
	var failures error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return false, errors.Join(failures, err)
		}
		if validQuotaTeamMember(candidate, "", "") && finite[teamMemberLimitScopeID(candidate.TeamID, candidate.UserID)] {
			target := teamMemberQuotaTarget{candidate.MembershipID, candidate.TeamID, candidate.UserID}
			failures = errors.Join(failures, observe(ctx, target))
		}
		// A skip or failed current observation never starves later members. Policies
		// are re-read under governance; this batch only selects bounded candidates.
		*cursor = candidate.MembershipID
	}
	return false, failures
}

func (s *Service) reconcileTeamMemberQuotaNotifications(ctx context.Context) error {
	cursor := ""
	var failures error
	for {
		previous := cursor
		done, err := s.reconcileTeamMemberQuotaNotificationBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil || err != nil && cursor == previous {
			return failures
		}
	}
}
