package service

import (
	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/limits"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func insightsQuotaTeamQuery(tx *gorm.DB, actorID string, platformRead bool) *gorm.DB {
	query := tx.Model(&entity.Team{})
	if platformRead {
		return query
	}
	// Correlate exact identity bytes in the same snapshot. A case-insensitive
	// membership alias must not turn directory discovery into resource access.
	membership := tx.Model(&entity.TeamMembership{}).Select("1").
		Where(database.ExactTextColumns(tx, clause.Column{Table: "team_memberships", Name: "team_id"}, clause.Column{Table: "teams", Name: "id"})).
		Where(database.ExactText(tx, clause.Column{Name: "user_id"}, actorID)).
		Where(database.ExactText(tx, clause.Column{Name: "status"}, entity.ResourceActive)).
		Where(clause.Or(database.ExactText(tx, clause.Column{Name: "role"}, entity.TeamOwner), database.ExactText(tx, clause.Column{Name: "role"}, entity.TeamMember)))
	return query.Where("EXISTS (?)", membership).
		Where(database.ExactText(tx, clause.Column{Table: "teams", Name: "status"}, entity.ResourceActive))
}

func insightsQuotaPolicyQuery(tx *gorm.DB, targets []insightsQuotaTarget) *gorm.DB {
	scopes := make([]clause.Expression, 0, len(targets))
	for _, target := range targets {
		scopes = append(scopes, clause.And(
			database.ExactText(tx, clause.Column{Name: "scope_kind"}, target.kind),
			database.ExactText(tx, clause.Column{Name: "scope_id"}, target.row.ID),
		))
	}
	return tx.Where(clause.Or(scopes...))
}

// Only selectors that already established exact resource authority and the
// complete Key ownership graph may supply targets. This adds no read authority.
func hydrateInsightsQuotaPolicies(tx *gorm.DB, targets []insightsQuotaTarget) error {
	if _, err := insightsQuotaPolicyTargets(targets); err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	var rows []entity.ResourceLimit
	if err := insightsQuotaPolicyQuery(tx, targets).Limit(len(targets) + 1).Find(&rows).Error; err != nil {
		return err
	}
	return projectInsightsQuotaPolicies(targets, rows)
}

func insightsQuotaPolicyTargets(targets []insightsQuotaTarget) (map[string]int, error) {
	// The existing page bound is 50, with one independently validated lookahead.
	if len(targets) > 51 {
		return nil, apperrors.ErrInternal
	}
	byAccount := make(map[string]int, len(targets))
	for i, target := range targets {
		account := limitAccount(target.kind, target.row.ID)
		if target.kind != "team" && target.kind != "key" || !safeTeamSessionID(target.row.ID) {
			return nil, apperrors.ErrInternal
		}
		if _, duplicate := byAccount[account]; duplicate {
			return nil, apperrors.ErrInternal
		}
		byAccount[account] = i
	}
	return byAccount, nil
}

func projectInsightsQuotaPolicies(targets []insightsQuotaTarget, rows []entity.ResourceLimit) error {
	byAccount, err := insightsQuotaPolicyTargets(targets)
	if err != nil {
		return err
	}
	stored := make([]entity.ResourceLimit, len(targets))
	for i, target := range targets {
		stored[i] = entity.ResourceLimit{ScopeKind: target.kind, ScopeID: target.row.ID, ETag: "0"}
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		account := limitAccount(row.ScopeKind, row.ScopeID)
		i, found := byAccount[account]
		if !found || seen[account] || row.ScopeKind != targets[i].kind || row.ScopeID != targets[i].row.ID {
			return apperrors.ErrInternal
		}
		seen[account], stored[i] = true, row
	}
	policies := make([]limits.Policy, len(targets))
	for i, row := range stored {
		var err error
		if row.ScopeKind == "key" {
			policies[i], err = decodeLimitPolicy(row, true)
		} else {
			policies[i], err = policyFromRow(row)
			if err == nil {
				err = validateTeamLimitPolicy("team", policies[i])
			}
		}
		if err != nil {
			return err
		}
	}
	// Do not leave a partially hydrated page when any selected policy is invalid.
	for i := range targets {
		targets[i].policy = policies[i]
	}
	return nil
}
