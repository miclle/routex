package service

import (
	"context"
	"errors"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamRollingQuotaWarningScanQuery(db *gorm.DB, cursor quotaNotificationCursor) *gorm.DB {
	states := db.Table("team_rolling_quota_warning_states").Select("1").Where(database.ExactTextColumns(db, clause.Column{Table: "team_rolling_quota_warning_states", Name: "team_id"}, clause.Column{Table: "resource_limits", Name: "scope_id"}))
	query := db.Model(&entity.ResourceLimit{}).Select("scope_kind", "scope_id").Where(database.ExactText(db, clause.Column{Name: "scope_kind"}, "team")).Where("tokens5_h > ? OR tokens7_d > ? OR EXISTS (?)", 0, 0, states)
	if cursor.ID != "" {
		query = query.Where("scope_id > ?", cursor.ID)
	}
	return query.Order("scope_id").Limit(quotaNotificationBatchSize)
}
func (s *Service) reconcileTeamRollingQuotaWarningBatch(ctx context.Context, cursor *quotaNotificationCursor) (bool, error) {
	var rows []entity.ResourceLimit
	if err := teamRollingQuotaWarningScanQuery(s.authDB(ctx), *cursor).Find(&rows).Error; err != nil {
		return false, err
	}
	return reconcileQuotaNotificationRows(ctx, rows, cursor, s.observeTeamRollingQuotaWarnings)
}
func (s *Service) reconcileTeamRollingQuotaWarnings(ctx context.Context) error {
	cursor := quotaNotificationCursor{}
	var failures error
	for {
		previous := cursor
		done, err := s.reconcileTeamRollingQuotaWarningBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil || err != nil && previous == cursor {
			return failures
		}
	}
}
