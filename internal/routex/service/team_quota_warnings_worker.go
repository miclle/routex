package service

import (
	"context"
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamQuotaWarningScanQuery(db *gorm.DB, cursor quotaNotificationCursor) *gorm.DB {
	query := db.Model(&entity.ResourceLimit{}).Select("scope_kind", "scope_id").Where(database.ExactText(db, clause.Column{Name: "scope_kind"}, "team")).Where("tokens_month > ? OR money_month IS NOT NULL", 0)
	if cursor.ID != "" {
		query = query.Where("scope_id > ?", cursor.ID)
	}
	return query.Order("scope_id").Limit(quotaNotificationBatchSize)
}
func (s *Service) reconcileMonthlyTeamQuotaWarningBatch(ctx context.Context, cursor *quotaNotificationCursor) (bool, error) {
	var rows []entity.ResourceLimit
	if err := teamQuotaWarningScanQuery(s.authDB(ctx), *cursor).Find(&rows).Error; err != nil {
		return false, err
	}
	return reconcileQuotaNotificationRows(ctx, rows, cursor, s.observeMonthlyTeamQuotaWarning)
}
func (s *Service) reconcileMonthlyTeamQuotaWarnings(ctx context.Context) error {
	cursor := quotaNotificationCursor{}
	var failures error
	for {
		previous := cursor
		done, err := s.reconcileMonthlyTeamQuotaWarningBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil || err != nil && previous == cursor {
			return failures
		}
	}
}
