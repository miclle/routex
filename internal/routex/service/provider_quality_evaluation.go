package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
)

// StartProviderQualityEvaluation starts the bounded policy evaluator and joins
// it through the returned stop function before the database is closed.
func (s *Service) StartProviderQualityEvaluation(ctx context.Context) (func(), error) {
	var rows []entity.ProviderQualityPolicy
	if err := s.authDB(ctx).Select("provider_id").Limit(1).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(providerQualityPoll)
		defer ticker.Stop()
		failed := false
		for {
			err := s.FlushProviderQualityEvaluation(run)
			if err != nil && !errors.Is(err, context.Canceled) {
				if !failed {
					log.Printf("provider quality evaluation failed: %v", err)
				}
				failed = true
			} else if err == nil && failed {
				log.Printf("provider quality evaluation resumed")
				failed = false
			}
			select {
			case <-run.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

// FlushProviderQualityEvaluation closes the latest elapsed UTC window for
// every enabled policy. Window persistence and notification projection use
// separate transactions, so notification outages cannot erase source facts.
func (s *Service) FlushProviderQualityEvaluation(ctx context.Context) error {
	now := time.Now().UTC()
	afterProviderID := ""
	failed := 0
	var firstFailure error
	for {
		var policies []entity.ProviderQualityPolicy
		query := s.authDB(ctx).Where("enabled = ?", true)
		if afterProviderID != "" {
			query = query.Where("provider_id > ?", afterProviderID)
		}
		if err := query.Order("provider_id").Limit(100).Find(&policies).Error; err != nil {
			return catalogError(err)
		}
		batchFailed, batchFirstFailure := evaluateProviderQualityPolicies(ctx, policies, now, s.evaluateProviderQualityPolicy)
		failed += batchFailed
		if firstFailure == nil {
			firstFailure = batchFirstFailure
		}
		if len(policies) < 100 {
			break
		}
		afterProviderID = policies[len(policies)-1].ProviderID
	}
	// Projection is replayable from the immutable window and call facts.
	projectionErr := s.authDB(ctx).Transaction(func(tx *gorm.DB) error { return s.reconcileNotificationSources(tx) })
	if projectionErr != nil {
		projectionErr = catalogError(projectionErr)
	}
	if failed > 0 {
		firstFailure = fmt.Errorf("%d provider quality policies failed; first failure: %w", failed, firstFailure)
	}
	return errors.Join(firstFailure, projectionErr)
}

func evaluateProviderQualityPolicies(ctx context.Context, policies []entity.ProviderQualityPolicy, now time.Time, evaluate func(context.Context, entity.ProviderQualityPolicy, time.Time) error) (int, error) {
	failed := 0
	var firstFailure error
	for _, policy := range policies {
		if err := evaluate(ctx, policy, now); err != nil {
			failed++
			if firstFailure == nil {
				firstFailure = err
			}
		}
	}
	return failed, firstFailure
}

func closedQualityWindow(now time.Time, minutes int) (time.Time, time.Time) {
	duration := time.Duration(minutes) * time.Minute
	end := now.Add(-providerQualityGrace).Truncate(duration)
	return end.Add(-duration), end
}

func (s *Service) evaluateProviderQualityPolicy(ctx context.Context, policy entity.ProviderQualityPolicy, now time.Time) error {
	if policy.WindowMinutes < 5 || policy.WindowMinutes > 1440 {
		return apperrors.ErrInternal
	}
	from, to := closedQualityWindow(now, policy.WindowMinutes)
	var provider entity.Provider
	if err := s.authDB(ctx).First(&provider, "id = ?", policy.ProviderID).Error; err != nil {
		return catalogError(err)
	}
	quality, err := providerQualityRange(s.authDB(ctx), provider, from, to, now)
	if err != nil {
		return err
	}
	state, detail := qualityState(quality, policy)
	windowID, err := id.NewPrefixed("pqw")
	if err != nil {
		return apperrors.ErrInternal
	}
	window := entity.ProviderQualityWindow{
		ID: windowID, ProviderID: provider.ID, ProviderName: provider.Name,
		WindowStart: from, WindowEnd: to, PolicyETag: policy.ETag,
		EligibleAttempts: quality.EligibleAttempts, ExcludedAttempts: quality.ExcludedAttempts,
		UnknownAttributionAttempts: quality.UnknownAttributionAttempts, Successes: quality.Successes,
		HTTP429: quality.HTTP429, HTTP5XX: quality.HTTP5XX,
		KnownDurationAttempts: quality.KnownDurationAttempts, P95DurationMS: quality.P95DurationMS,
		State: state, DetailCode: detail, CreatedAt: now,
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var currentPolicy entity.ProviderQualityPolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&currentPolicy, "provider_id = ?", policy.ProviderID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (!currentPolicy.Enabled || currentPolicy.ETag != policy.ETag) {
			return errProviderQualityPolicyChanged
		}
		if err != nil {
			return err
		}
		created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "provider_id"}, {Name: "window_end"}}, DoNothing: true}).Create(&window)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 0 {
			return nil
		}
		var current entity.ProviderQualityState
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "provider_id = ?", provider.ID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if current.ProviderID != "" && current.LastWindowEnd != nil && !current.LastWindowEnd.Before(to) {
			return nil
		}
		stateRow := entity.ProviderQualityState{ProviderID: provider.ID, LastWindowEnd: &to, LastState: state, UpdatedAt: now}
		if current.ProviderID == "" {
			if err := tx.Create(&stateRow).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&current).Updates(map[string]any{"last_window_end": to, "last_state": state, "updated_at": now}).Error; err != nil {
			return err
		}
		return nil
	})
	if errors.Is(err, errProviderQualityPolicyChanged) {
		return nil
	}
	return catalogError(err)
}

var errProviderQualityPolicyChanged = errors.New("provider quality policy changed during evaluation")

func resolveOperationalAlertGroup(tx *gorm.DB, groupKey string, now time.Time) error {
	var alert entity.OperationalAlert
	if err := tx.First(&alert, "group_key = ?", groupKey).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if alert.State == "resolved" {
		return nil
	}
	etag, err := id.NewPrefixed("rev")
	if err != nil {
		return err
	}
	if err := tx.Model(&alert).Updates(map[string]any{"state": "resolved", "e_tag": etag, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&entity.Notification{}).Where("alert_id = ?", alert.ID).
		Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).
		Updates(map[string]any{"read": true, "read_at": now}).Error
}

func (s *Service) reconcileProviderQualityNotifications(tx *gorm.DB) error {
	const limit = 100
	var recovered []entity.ProviderQualityWindow
	if err := tx.Table("provider_quality_windows AS w").Select("w.*").
		Joins("JOIN provider_quality_policies AS p ON p.provider_id = w.provider_id AND p.e_tag = w.policy_e_tag AND p.enabled = ?", true).
		Joins("JOIN operational_alerts AS a ON a.kind = ? AND a.subject_type = ? AND a.subject_id = w.provider_id AND a.state <> ?", notificationKindProviderQuality, "provider", "resolved").
		Where("w.state = ?", "healthy").
		Where("NOT EXISTS (SELECT 1 FROM provider_quality_windows newer WHERE newer.provider_id = w.provider_id AND newer.policy_e_tag = w.policy_e_tag AND (newer.window_end > w.window_end OR (newer.window_end = w.window_end AND newer.id > w.id)))").
		Order("w.window_end, w.id").Limit(limit).Scan(&recovered).Error; err != nil {
		return err
	}
	for _, window := range recovered {
		if err := resolveOperationalAlertGroup(tx, "provider_quality:"+window.ProviderID, window.WindowEnd); err != nil {
			return err
		}
	}

	var degraded []entity.ProviderQualityWindow
	if err := tx.Table("provider_quality_windows AS w").Select("w.*").
		Joins("JOIN provider_quality_policies AS p ON p.provider_id = w.provider_id AND p.e_tag = w.policy_e_tag AND p.enabled = ?", true).
		Where("w.state = ?", "degraded").
		Where("NOT EXISTS (SELECT 1 FROM provider_quality_windows newer WHERE newer.provider_id = w.provider_id AND newer.policy_e_tag = w.policy_e_tag AND (newer.window_end > w.window_end OR (newer.window_end = w.window_end AND newer.id > w.id)))").
		Where(`NOT EXISTS (
			SELECT 1 FROM operational_alert_occurrences o
			JOIN provider_quality_windows ow ON o.source_type = ? AND o.source_id = ow.id
			WHERE ow.provider_id = w.provider_id AND ow.policy_e_tag = w.policy_e_tag
			AND NOT EXISTS (
				SELECT 1 FROM provider_quality_windows recovered
				WHERE recovered.provider_id = w.provider_id AND recovered.policy_e_tag = w.policy_e_tag AND recovered.state = ?
				AND (recovered.window_end > ow.window_end OR (recovered.window_end = ow.window_end AND recovered.id > ow.id))
				AND (recovered.window_end < w.window_end OR (recovered.window_end = w.window_end AND recovered.id <= w.id))
			)
		)`, notificationSourceProviderQuality, "healthy").
		Order("w.window_end, w.id").Limit(limit).Scan(&degraded).Error; err != nil {
		return err
	}
	for _, window := range degraded {
		var policy entity.ProviderQualityPolicy
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&policy, "provider_id = ? AND enabled = ? AND e_tag = ?", window.ProviderID, true, window.PolicyETag).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if err := s.reconcileProviderQualityPolicyNotification(tx, policy); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcileProviderQualityPolicyNotification(tx *gorm.DB, policy entity.ProviderQualityPolicy) error {
	var latest entity.ProviderQualityWindow
	if err := tx.Where("provider_id = ? AND policy_e_tag = ?", policy.ProviderID, policy.ETag).
		Order("window_end DESC, id DESC").Limit(1).Find(&latest).Error; err != nil {
		return err
	}
	if latest.ID == "" {
		return nil
	}
	group := "provider_quality:" + policy.ProviderID
	if latest.State == "healthy" {
		return resolveOperationalAlertGroup(tx, group, latest.WindowEnd)
	}
	if latest.State != "degraded" {
		return nil
	}
	var previousRecovery entity.ProviderQualityWindow
	if err := tx.Where("provider_id = ? AND policy_e_tag = ? AND window_end < ? AND state = ?", policy.ProviderID, policy.ETag, latest.WindowEnd, "healthy").
		Order("window_end DESC, id DESC").Limit(1).Find(&previousRecovery).Error; err != nil {
		return err
	}
	transition := entity.ProviderQualityWindow{}
	query := tx.Where("provider_id = ? AND policy_e_tag = ? AND state = ?", policy.ProviderID, policy.ETag, "degraded")
	if previousRecovery.ID != "" {
		query = query.Where("window_end > ?", previousRecovery.WindowEnd)
	}
	if err := query.Order("window_end, id").Limit(1).Find(&transition).Error; err != nil {
		return err
	}
	if transition.ID == "" {
		return nil
	}
	if err := s.recordOperationalAlertOccurrenceForSubject(tx, notificationSourceProviderQuality, transition.ID,
		group, notificationKindProviderQuality, "high", transition.DetailCode,
		"provider", transition.ProviderID, transition.ProviderName, transition.WindowEnd); err != nil {
		return err
	}
	var alert entity.OperationalAlert
	if err := tx.First(&alert, "group_key = ?", group).Error; err != nil {
		return err
	}
	return tx.Model(&entity.ProviderQualityState{}).Where("provider_id = ?", transition.ProviderID).Update("last_alert_id", alert.ID).Error
}

func (s *Service) reconcileRouteUnavailableNotifications(tx *gorm.DB) error {
	var calls []entity.CallRecord
	err := tx.Table("call_records AS c").Select("c.*").
		Joins("LEFT JOIN operational_alert_occurrences AS o ON o.source_type = ? AND o.source_id = c.request_id", notificationSourceGatewayCall).
		Where("c.route_stop_reason IN ? AND o.id IS NULL", []string{"no_candidates", "attempt_budget_exhausted"}).
		Order("c.completed_at, c.request_id").Limit(100).Scan(&calls).Error
	if err != nil {
		return err
	}
	for _, call := range calls {
		group := "route_unavailable:" + call.ModelID + ":" + call.Protocol + ":" + call.RouteStopReason
		if err := s.recordOperationalAlertOccurrenceForSubject(tx, notificationSourceGatewayCall, call.RequestID,
			group, notificationKindRouteUnavailable, "medium", call.RouteStopReason,
			"model", call.ModelID, call.ModelName, call.CompletedAt); err != nil {
			return err
		}
	}
	return nil
}
