package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func teamCallExportScope(query *gorm.DB, actorID, teamID string) *gorm.DB {
	return query.Where(database.ExactText(query, clause.Column{Name: "user_id"}, actorID)).Where(database.ExactText(query, clause.Column{Name: "team_id"}, teamID))
}

// ExportTeamCalls exposes only the current member's own immutable Team facts.
// Current membership authorizes reading; it never replaces historical attribution.
func (s *Service) ExportTeamCalls(ctx context.Context, actorID, teamID string, filter CallFilter) (*CallCSVExport, error) {
	if err := validateCallExportFilter(filter, false); err != nil {
		return nil, err
	}
	if filter.KeyID != "" || filter.TeamID != "" {
		return nil, apperrors.ErrBadRequest
	}
	if ctx.Err() != nil {
		return nil, callExportUnavailable
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := &CallCSVExport{}
	err := s.authDB(queryCtx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeTeamCalls(tx, actorID, teamID); err != nil {
			return err
		}
		records := []entity.CallRecord{}
		query := teamCallExportScope(callExportQuery(tx, filter), actorID, teamID)
		if err := query.Order("started_at DESC, request_id DESC").Limit(callExportRows + 1).Find(&records).Error; err != nil {
			return err
		}
		if len(records) > callExportRows {
			return callExportTooLarge
		}
		encoded, err := encodeCallCSV(records, false)
		if err != nil {
			return err
		}
		result.CSV = encoded
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if queryCtx.Err() != nil {
		return nil, callExportUnavailable
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, callExportUnavailable
		}
		return nil, catalogError(err)
	}
	return result, nil
}
