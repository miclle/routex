package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	notificationSourceSystemJob              = "system_job"
	notificationSourceCredentialVerification = "credential_verification"
	notificationSourceProviderQuality        = "provider_quality_window"
	notificationSourceGatewayCall            = "gateway_call"
	notificationKindSystemJobFailure         = "system_job_failure"
	notificationKindCredentialFailure        = "credential_verification_failure"
	notificationKindProviderQuality          = "provider_quality_degraded"
	notificationKindRouteUnavailable         = "route_unavailable"
)

func notificationEventAllowed(sourceType, kind, severity, detailCode string) bool {
	if !slices.Contains([]string{"high", "medium"}, severity) {
		return false
	}
	switch sourceType {
	case notificationSourceSystemJob:
		if kind != notificationKindSystemJobFailure {
			return false
		}
		return slices.Contains([]string{
			"database_unavailable", "invalid_configuration", "publication_failed", "executor_lost",
			"buffer_read_failed", "invalid_fact", "identity_mismatch", "persistence_failed", "acknowledge_failed",
			"claim_failed", "delete_failed", "state_update_failed",
		}, detailCode)
	case notificationSourceCredentialVerification:
		return kind == notificationKindCredentialFailure && severity == "high" && detailCode == "verification_failed"
	case notificationSourceProviderQuality:
		return kind == notificationKindProviderQuality && severity == "high" && slices.Contains([]string{"success_rate_below_threshold", "p95_duration_above_threshold", "multiple_thresholds_breached"}, detailCode)
	case notificationSourceGatewayCall:
		return kind == notificationKindRouteUnavailable && severity == "medium" && slices.Contains([]string{"no_candidates", "attempt_budget_exhausted"}, detailCode)
	default:
		return false
	}
}

// recordOperationalAlertOccurrence accepts only server-owned source identifiers
// and allowlisted codes. Callers run it in a transaction after the source fact
// is durable; source uniqueness makes every reconciliation replay an exact no-op.
func (s *Service) recordOperationalAlertOccurrence(tx *gorm.DB, sourceType, sourceID, groupKey, kind, severity, detailCode string, occurredAt time.Time) error {
	return s.recordOperationalAlertOccurrenceForSubject(tx, sourceType, sourceID, groupKey, kind, severity, detailCode, "", "", "", occurredAt)
}

func validNotificationSubject(subjectType, subjectID, subjectName string) bool {
	if subjectType == "" {
		return subjectID == "" && subjectName == ""
	}
	return slices.Contains([]string{"provider", "model"}, subjectType) && subjectID != "" && len(subjectID) <= 64 && safeCallID.MatchString(subjectID) &&
		subjectName != "" && strings.TrimSpace(subjectName) == subjectName && utf8.ValidString(subjectName) && utf8.RuneCountInString(subjectName) <= 128 && !strings.ContainsFunc(subjectName, unicode.IsControl)
}

func (s *Service) recordOperationalAlertOccurrenceForSubject(tx *gorm.DB, sourceType, sourceID, groupKey, kind, severity, detailCode, subjectType, subjectID, subjectName string, occurredAt time.Time) error {
	if tx == nil || sourceID == "" || len(sourceID) > 80 || groupKey == "" || len(groupKey) > 120 || !notificationEventAllowed(sourceType, kind, severity, detailCode) || !validNotificationSubject(subjectType, subjectID, subjectName) {
		return fmt.Errorf("invalid operational notification event")
	}
	var existing entity.OperationalAlertOccurrence
	if err := tx.Where("source_type = ? AND source_id = ?", sourceType, sourceID).Limit(1).Find(&existing).Error; err != nil {
		return err
	}
	if existing.ID != "" {
		return nil
	}
	alertID, err := id.NewPrefixed("alr")
	if err != nil {
		return err
	}
	etag, err := id.NewPrefixed("rev")
	if err != nil {
		return err
	}
	alert := entity.OperationalAlert{
		ID: alertID, GroupKey: groupKey, Kind: kind, Severity: severity, DetailCode: detailCode,
		SubjectType: subjectType, SubjectID: subjectID, SubjectName: subjectName,
		State: "open", OccurrenceCount: 1, FirstSeenAt: occurredAt, LastSeenAt: occurredAt, ETag: etag, UpdatedAt: occurredAt,
	}
	created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "group_key"}}, DoNothing: true}).Create(&alert)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		alert = entity.OperationalAlert{}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&alert, "group_key = ?", groupKey).Error; err != nil {
			return err
		}
	}
	occurrenceID, err := id.NewPrefixed("occ")
	if err != nil {
		return err
	}
	occurrence := entity.OperationalAlertOccurrence{ID: occurrenceID, AlertID: alert.ID, SourceType: sourceType, SourceID: sourceID, DetailCode: detailCode, SubjectType: subjectType, SubjectID: subjectID, SubjectName: subjectName, OccurredAt: occurredAt}
	inserted := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_type"}, {Name: "source_id"}}, DoNothing: true}).Create(&occurrence)
	if inserted.Error != nil {
		return inserted.Error
	}
	if inserted.RowsAffected == 0 {
		return nil
	}
	var latestOccurrence entity.OperationalAlertOccurrence
	if err := tx.Where("alert_id = ?", alert.ID).Order("occurred_at DESC, id DESC").First(&latestOccurrence).Error; err != nil {
		return err
	}
	isLatest := latestOccurrence.ID == occurrence.ID
	if created.RowsAffected == 0 {
		firstSeenAt := alert.FirstSeenAt
		if occurredAt.Before(firstSeenAt) {
			firstSeenAt = occurredAt
		}
		etag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		updates := map[string]any{
			"occurrence_count": gorm.Expr("occurrence_count + 1"), "first_seen_at": firstSeenAt,
			"e_tag": etag, "updated_at": maxTime(alert.UpdatedAt, occurredAt),
		}
		if isLatest {
			updates["last_seen_at"] = occurredAt
			updates["state"] = "open"
			updates["detail_code"] = detailCode
			updates["subject_type"] = subjectType
			updates["subject_id"] = subjectID
			updates["subject_name"] = subjectName
		}
		if err := tx.Model(&alert).Updates(updates).Error; err != nil {
			return err
		}
		alert.OccurrenceCount++
		alert.FirstSeenAt, alert.ETag, alert.UpdatedAt = firstSeenAt, etag, maxTime(alert.UpdatedAt, occurredAt)
		if isLatest {
			alert.LastSeenAt, alert.State, alert.DetailCode = occurredAt, "open", detailCode
			alert.SubjectType, alert.SubjectID, alert.SubjectName = subjectType, subjectID, subjectName
		}
	}
	return s.publishOperationalAlert(tx, alert, occurrence, latestOccurrence, isLatest)
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func (s *Service) publishOperationalAlert(tx *gorm.DB, alert entity.OperationalAlert, occurrence, latestOccurrence entity.OperationalAlertOccurrence, isLatest bool) error {
	var users []entity.User
	if err := tx.Where("disabled = ? AND offboarded_at IS NULL", false).Order("id").Find(&users).Error; err != nil {
		return err
	}
	var smtp entity.SMTPSetting
	if err := tx.First(&smtp, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	smtpEligible := smtp.Enabled && smtp.ETag != "" && smtpclient.ValidAddress(smtp.SenderEmail) && smtpclient.ValidSenderName(smtp.SenderName)
	for _, user := range users {
		permissions, err := permissionsFor(tx, user.ID)
		if err != nil {
			return err
		}
		if !slices.Contains(permissions, "system.read") {
			continue
		}
		notificationID, err := id.NewPrefixed("ntf")
		if err != nil {
			return err
		}
		notification := entity.Notification{
			ID: notificationID, RecipientID: user.ID, AlertID: alert.ID, LatestOccurrenceID: latestOccurrence.ID, Kind: alert.Kind,
			Severity: alert.Severity, DetailCode: alert.DetailCode, SubjectType: alert.SubjectType, SubjectID: alert.SubjectID, SubjectName: alert.SubjectName, OccurrenceCount: alert.OccurrenceCount,
			Read: false, FirstSeenAt: alert.FirstSeenAt, LastSeenAt: alert.LastSeenAt,
		}
		assignments := map[string]any{
			"kind": alert.Kind, "severity": alert.Severity, "detail_code": alert.DetailCode,
			"subject_type": alert.SubjectType, "subject_id": alert.SubjectID, "subject_name": alert.SubjectName,
			"occurrence_count": alert.OccurrenceCount, "first_seen_at": alert.FirstSeenAt,
		}
		if isLatest {
			assignments["latest_occurrence_id"] = occurrence.ID
			assignments["read"] = false
			assignments["read_at"] = nil
			assignments["last_seen_at"] = alert.LastSeenAt
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "recipient_id"}, {Name: "alert_id"}},
			DoUpdates: clause.Assignments(assignments),
		}).Create(&notification).Error; err != nil {
			return err
		}
		settings, err := defaultNotificationSettings(tx, user.ID)
		if err != nil {
			return err
		}
		emailEnabled := alert.Severity == "high" && settings.EmailHigh || alert.Severity == "medium" && settings.EmailMedium
		if !smtpEligible || !emailEnabled || !smtpclient.ValidAddress(settings.ExternalEmail) {
			continue
		}
		intentID, err := id.NewPrefixed("ndl")
		if err != nil {
			return err
		}
		intent := entity.NotificationDeliveryIntent{
			ID: intentID, OccurrenceID: occurrence.ID, RecipientID: user.ID, RecipientEmail: settings.ExternalEmail,
			Kind: alert.Kind, Severity: alert.Severity, DetailCode: alert.DetailCode,
			SubjectType: alert.SubjectType, SubjectID: alert.SubjectID, SubjectName: alert.SubjectName, SMTPETag: smtp.ETag,
			Status: "pending", NextAttemptAt: occurrence.OccurredAt, CreatedAt: occurrence.OccurredAt, UpdatedAt: occurrence.OccurredAt,
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "occurrence_id"}, {Name: "recipient_id"}}, DoNothing: true}).Create(&intent).Error; err != nil {
			return err
		}
	}
	return nil
}

func systemJobNotificationMetadata(code, detail string) (severity, group string, ok bool) {
	allowed := false
	switch code {
	case SystemJobRuntimePublication:
		allowed = slices.Contains([]string{"database_unavailable", "invalid_configuration", "publication_failed", "executor_lost"}, detail)
	case SystemJobCallRecordDelivery:
		allowed = slices.Contains([]string{"buffer_read_failed", "invalid_fact", "identity_mismatch", "persistence_failed", "acknowledge_failed", "executor_lost"}, detail)
	case SystemJobStorageCleanup:
		allowed = slices.Contains([]string{"claim_failed", "delete_failed", "state_update_failed", "executor_lost"}, detail)
	}
	if !allowed {
		return "", "", false
	}
	severity = "high"
	if code == SystemJobStorageCleanup {
		severity = "medium"
	}
	return severity, "system_job:" + code + ":" + detail, true
}

func (s *Service) reconcileFailedSystemJobNotifications(tx *gorm.DB) error {
	var jobs []entity.SystemJob
	err := tx.Table("system_jobs AS j").Select("j.*").
		Joins("LEFT JOIN operational_alert_occurrences AS o ON o.source_type = ? AND o.source_id = j.id", notificationSourceSystemJob).
		Where("j.status = ? AND o.id IS NULL", systemJobFailed).
		Where("(j.code = ? AND j.detail_code IN ?) OR (j.code = ? AND j.detail_code IN ?) OR (j.code = ? AND j.detail_code IN ?)",
			SystemJobRuntimePublication, []string{"database_unavailable", "invalid_configuration", "publication_failed", "executor_lost"},
			SystemJobCallRecordDelivery, []string{"buffer_read_failed", "invalid_fact", "identity_mismatch", "persistence_failed", "acknowledge_failed", "executor_lost"},
			SystemJobStorageCleanup, []string{"claim_failed", "delete_failed", "state_update_failed", "executor_lost"}).
		Order("j.completed_at, j.id").Limit(100).Scan(&jobs).Error
	if err != nil {
		return err
	}
	for _, job := range jobs {
		severity, group, ok := systemJobNotificationMetadata(job.Code, job.DetailCode)
		if !ok {
			continue
		}
		occurredAt := job.UpdatedAt
		if job.CompletedAt != nil {
			occurredAt = *job.CompletedAt
		}
		if err := s.recordOperationalAlertOccurrence(tx, notificationSourceSystemJob, job.ID, group, notificationKindSystemJobFailure, severity, job.DetailCode, occurredAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcileFailedCredentialNotifications(tx *gorm.DB) error {
	var events []entity.AuditEvent
	err := tx.Table("audit_events AS a").Select("a.*").
		Joins("LEFT JOIN operational_alert_occurrences AS o ON o.source_type = ? AND o.source_id = a.id", notificationSourceCredentialVerification).
		Where("a.action = ? AND a.resource_type = ? AND o.id IS NULL", "credential.verify.failed", "credential").
		Order("a.created_at, a.id").Limit(100).Scan(&events).Error
	if err != nil {
		return err
	}
	for _, event := range events {
		if err := s.recordOperationalAlertOccurrence(tx, notificationSourceCredentialVerification, event.ID, "credential_verification:"+event.ResourceID, notificationKindCredentialFailure, "high", "verification_failed", event.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) reconcileNotificationSources(tx *gorm.DB) error {
	if err := s.reconcileFailedSystemJobNotifications(tx); err != nil {
		return err
	}
	if err := s.reconcileFailedCredentialNotifications(tx); err != nil {
		return err
	}
	if err := s.reconcileProviderQualityNotifications(tx); err != nil {
		return err
	}
	return s.reconcileRouteUnavailableNotifications(tx)
}
