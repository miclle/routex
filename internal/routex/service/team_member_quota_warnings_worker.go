package service

import (
	"context"
	"errors"
	"github.com/miclle/routex/internal/routex/entity"
)

func (s *Service) reconcileMonthlyTeamMemberQuotaWarningBatch(ctx context.Context, cursor *string) (bool, error) {
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
	return reconcileTeamMemberQuotaNotificationRows(ctx, candidates, policies, cursor, s.observeMonthlyTeamMemberQuotaWarning)
}
func (s *Service) reconcileMonthlyTeamMemberQuotaWarnings(ctx context.Context) error {
	cursor := ""
	var failures error
	for {
		previous := cursor
		done, err := s.reconcileMonthlyTeamMemberQuotaWarningBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil || err != nil && previous == cursor {
			return failures
		}
	}
}
