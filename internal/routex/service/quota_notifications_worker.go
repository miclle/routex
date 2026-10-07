package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm/clause"
)

const quotaNotificationBatchSize = 32
const quotaNotificationInterval = time.Second

type quotaNotificationCursor struct{ Kind, ID string }

func (s *Service) reconcileMonthlyQuotaNotificationBatch(ctx context.Context, cursor *quotaNotificationCursor) (bool, error) {
	var rows []entity.ResourceLimit
	db := s.authDB(ctx)
	query := db.Select("scope_kind", "scope_id").Where(clause.Or(database.ExactText(db, clause.Column{Name: "scope_kind"}, "user"), database.ExactText(db, clause.Column{Name: "scope_kind"}, "project"), database.ExactText(db, clause.Column{Name: "scope_kind"}, "team"))).Where("tokens_month IS NOT NULL OR money_month IS NOT NULL")
	if cursor.Kind != "" {
		query = query.Where("scope_kind > ? OR (scope_kind = ? AND scope_id > ?)", cursor.Kind, cursor.Kind, cursor.ID)
	}
	if err := query.Order("scope_kind,scope_id").Limit(quotaNotificationBatchSize).Find(&rows).Error; err != nil {
		return false, err
	}
	return reconcileQuotaNotificationRows(ctx, rows, cursor, s.observeMonthlyQuotaNotification)
}

func reconcileQuotaNotificationRows(ctx context.Context, rows []entity.ResourceLimit, cursor *quotaNotificationCursor, observe func(context.Context, string, string) error) (bool, error) {
	if len(rows) == 0 {
		*cursor = quotaNotificationCursor{}
		return true, nil
	}
	var failures error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		failures = errors.Join(failures, observe(ctx, row.ScopeKind, row.ScopeID))
		// Skips and failures both advance. A transient failure is revisited after
		// wraparound, rather than starving every later resource behind the first32.
		*cursor = quotaNotificationCursor{Kind: row.ScopeKind, ID: row.ScopeID}
	}
	return false, failures
}

// ReconcileMonthlyQuotaNotifications scans one complete current-policy cycle in
// bounded keyset batches. It does not reconstruct missed historical months.
func (s *Service) ReconcileMonthlyQuotaNotifications(ctx context.Context) error {
	cursor := quotaNotificationCursor{}
	var failures error
	for {
		previous := cursor
		done, err := s.reconcileMonthlyQuotaNotificationBatch(ctx, &cursor)
		failures = errors.Join(failures, err)
		if done || ctx.Err() != nil {
			return errors.Join(failures, s.reconcileTeamMemberQuotaNotifications(ctx), s.reconcileMonthlyQuotaWarnings(ctx), s.reconcilePersonalRollingQuotaWarnings(ctx), s.reconcileMonthlyTeamQuotaWarnings(ctx), s.reconcileMonthlyProjectQuotaWarnings(ctx), s.reconcileMonthlyTeamMemberQuotaWarnings(ctx), s.reconcileMonthlyPersonalKeyQuotaWarnings(ctx), s.reconcileMonthlyProjectKeyQuotaWarnings(ctx))
		}
		// A failed query did not advance the cursor; retry on the next reconciliation.
		if err != nil && cursor == previous {
			return errors.Join(failures, s.reconcileTeamMemberQuotaNotifications(ctx), s.reconcileMonthlyQuotaWarnings(ctx), s.reconcilePersonalRollingQuotaWarnings(ctx), s.reconcileMonthlyTeamQuotaWarnings(ctx), s.reconcileMonthlyProjectQuotaWarnings(ctx), s.reconcileMonthlyTeamMemberQuotaWarnings(ctx), s.reconcileMonthlyPersonalKeyQuotaWarnings(ctx), s.reconcileMonthlyProjectKeyQuotaWarnings(ctx))
		}
	}
}

// StartQuotaNotifications returns a cancellation-and-join function. Call it
// after recorder/runtime startup and join it before closing the durable journal.
func (s *Service) StartQuotaNotifications(ctx context.Context) (func(), error) {
	if s.recorder == nil || s.runtime == nil {
		return nil, runtimeUnavailable
	}
	if _, err := s.recorder.queue.QuotaStatus(); err != nil {
		return nil, runtimeUnavailable
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(quotaNotificationInterval)
		defer ticker.Stop()
		cursor := quotaNotificationCursor{}
		memberCursor := ""
		warningCursor := quotaNotificationCursor{}
		rollingWarningCursor := quotaNotificationCursor{}
		teamWarningCursor := quotaNotificationCursor{}
		projectWarningCursor := quotaNotificationCursor{}
		memberWarningCursor := ""
		keyWarningCursor := quotaNotificationCursor{}
		projectKeyWarningCursor := quotaNotificationCursor{}
		deferred := false
		for {
			if runCtx.Err() != nil {
				return
			}
			_, err := s.reconcileMonthlyQuotaNotificationBatch(runCtx, &cursor)
			_, memberErr := s.reconcileTeamMemberQuotaNotificationBatch(runCtx, &memberCursor)
			_, warningErr := s.reconcileMonthlyQuotaWarningBatch(runCtx, &warningCursor)
			_, rollingWarningErr := s.reconcilePersonalRollingQuotaWarningBatch(runCtx, &rollingWarningCursor)
			_, teamWarningErr := s.reconcileMonthlyTeamQuotaWarningBatch(runCtx, &teamWarningCursor)
			_, projectWarningErr := s.reconcileMonthlyProjectQuotaWarningBatch(runCtx, &projectWarningCursor)
			_, memberWarningErr := s.reconcileMonthlyTeamMemberQuotaWarningBatch(runCtx, &memberWarningCursor)
			_, keyWarningErr := s.reconcileMonthlyPersonalKeyQuotaWarningBatch(runCtx, &keyWarningCursor)
			_, projectKeyWarningErr := s.reconcileMonthlyProjectKeyQuotaWarningBatch(runCtx, &projectKeyWarningCursor)
			err = errors.Join(err, memberErr, warningErr, rollingWarningErr, teamWarningErr, projectWarningErr, memberWarningErr, keyWarningErr, projectKeyWarningErr)
			if err != nil && runCtx.Err() == nil && !deferred {
				log.Print("monthly quota notification observation deferred")
			} else if err == nil && deferred {
				log.Print("monthly quota notification observation resumed")
			}
			deferred = err != nil
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); <-done }) }, nil
}
