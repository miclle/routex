package service

import (
	"context"
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func personalRollingWarningScanQuery(db *gorm.DB, cursor quotaNotificationCursor) *gorm.DB {
	states := db.Table("personal_rolling_quota_warning_states").Select("1").Where(database.ExactTextColumns(db, clause.Column{Table: "personal_rolling_quota_warning_states", Name: "owner_id"}, clause.Column{Table: "resource_limits", Name: "scope_id"}))
	query := db.Model(&entity.ResourceLimit{}).Select("scope_kind", "scope_id").Where(database.ExactText(db, clause.Column{Name: "scope_kind"}, "user")).Where("tokens5_h > ? OR tokens7_d > ? OR EXISTS (?)", 0, 0, states)
	if cursor.ID != "" {
		query = query.Where("scope_id > ?", cursor.ID)
	}
	return query.Order("scope_id").Limit(quotaNotificationBatchSize)
}
func (s *Service) reconcilePersonalRollingQuotaWarningBatch(ctx context.Context, cursor *quotaNotificationCursor) (bool, error) {
	var rows []entity.ResourceLimit
	if err := personalRollingWarningScanQuery(s.authDB(ctx), *cursor).Find(&rows).Error; err != nil {
		return false, err
	}
	return reconcileQuotaNotificationRows(ctx, rows, cursor, s.observePersonalRollingQuotaWarnings)
}
func (s *Service) reconcilePersonalRollingQuotaWarnings(ctx context.Context) error {
	cursor := quotaNotificationCursor{}
	var failures error
	for {
		previous := cursor
		done, err := s.reconcilePersonalRollingQuotaWarningBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil || err != nil && previous == cursor {
			return failures
		}
	}
}
