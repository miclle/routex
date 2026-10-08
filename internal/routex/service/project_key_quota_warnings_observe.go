package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) observeMonthlyProjectKeyQuotaWarning(ctx context.Context, kind, rootID string) error {
	if kind != "key" || !projectWarningKeyID(rootID) || s.recorder == nil || s.recorder.queue == nil || s.runtime == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	// Waiters must read warning state committed by the preceding observer. A
	// repeatable-read snapshot is established before the governance lock wait.
	// Governance/owner/graph/policy locks and the final runtime fence keep the
	// authority and usage proof coherent while later reads see committed state.
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var seed entity.ProjectKey
		err := tx.Select("id,project_id").Where("id = ?", rootID).Where(database.ExactText(tx, clause.Column{Name: "id"}, rootID)).Take(&seed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if seed.ID != rootID || !safeTeamSessionID(seed.ProjectID) {
			return nil
		}
		var project entity.Project
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", seed.ProjectID).Where(database.ExactText(tx, clause.Column{Name: "id"}, seed.ProjectID)).Take(&project).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if project.ID != seed.ProjectID || project.Status != entity.ResourceActive || project.CreatedAt.IsZero() {
			return nil
		}
		var keys []entity.ProjectKey
		if err = projectKeyWarningChainQuery(tx, project.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&keys).Error; err != nil {
			return err
		}
		roots, complete := projectKeyWarningRoots(keys, project)
		if !complete {
			return errQuotaNotificationIdentity
		}
		if roots[rootID] != rootID {
			return nil
		}
		var root entity.ProjectKey
		for _, key := range keys {
			if key.ID == rootID {
				root = key
				break
			}
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_kind = ? AND scope_id = ?", "key", rootID).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, rootID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		policy, err := projectKeyPolicyFromRow(row, root, project)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		var prices entity.PricingSetting
		if err = tx.First(&calendar, 1).Error; err != nil {
			return err
		}
		if err = tx.First(&prices, 1).Error; err != nil {
			return err
		}
		auth := s.runtime.auth.Load()
		if !s.projectKeyWarningApplied(ctx, auth, project, keys, rootID, row, policy, calendar, prices.PlatformCurrency) {
			return nil
		}
		frame, err := s.recorder.queue.AccountQuotaUsageProofBatch([]string{limitAccount("key", rootID)}, time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if frame.TimeZone != calendar.TimeZone {
			return nil
		}
		observations := projectKeyMonthlyWarnings(row, root, project, frame, prices.PlatformCurrency)
		if len(observations) == 0 {
			return nil
		}
		recipients, err := s.projectQuotaWarningRecipients(tx, auth, project.ID)
		if err != nil {
			return err
		}
		for _, v := range observations {
			if err = persistProjectKeyQuotaWarning(tx, v, recipients); err != nil {
				return err
			}
		}
		if !s.projectKeyWarningApplied(ctx, auth, project, keys, rootID, row, policy, calendar, prices.PlatformCurrency) {
			return runtimeUnavailable
		}
		for _, recipient := range recipients {
			if runtimeDenied(&s.runtime.deniedUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedSessionUsers, recipient.ID) || runtimeDenied(&s.runtime.deniedProjects, project.ID) {
				return runtimeUnavailable
			}
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}
