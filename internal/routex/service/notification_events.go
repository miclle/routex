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
	if sourceType == notificationSourceModelDiscovery {
		return kind == notificationKindModelDiscovery && severity == "low" && detailCode == "first_recorded"
	}
	if !slices.Contains([]string{"high", "medium"}, severity) {
		return false
	}
	switch sourceType {
	case notificationSourceSystemJob:
		if kind != notificationKindSystemJobFailure {
			return false
		}
		if detailCode == "rotation_blocked" {
			return severity == "high"
		}
		return slices.Contains([]string{
			"database_unavailable", "invalid_configuration", "publication_failed", "executor_lost",
			"buffer_read_failed", "invalid_fact", "identity_mismatch", "persistence_failed", "acknowledge_failed",
			"claim_failed", "delete_failed", "state_update_failed",
		}, detailCode)
	case notificationSourceCredentialInference:
		return kind == notificationKindCredentialInference && severity == "medium" && detailCode == notificationDetailCredentialInference
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
	return s.recordOperationalAlertOccurrenceWithModelCount(tx, sourceType, sourceID, groupKey, kind, severity, detailCode, subjectType, subjectID, subjectName, occurredAt, 0)
}

func (s *Service) recordOperationalAlertOccurrenceWithModelCount(tx *gorm.DB, sourceType, sourceID, groupKey, kind, severity, detailCode, subjectType, subjectID, subjectName string, occurredAt time.Time, modelCount int, credentialIDs ...string) error {
	credentialID := ""
	if len(credentialIDs) > 1 {
		return fmt.Errorf("invalid credential inference envelope")
	}
	if len(credentialIDs) == 1 {
		credentialID = credentialIDs[0]
	}
	return s.recordOperationalAlertOccurrenceProjection(tx, sourceType, sourceID, groupKey, kind, severity, detailCode, subjectType, subjectID, subjectName, occurredAt, modelCount, credentialID, nil)
}

// Credential inference passes its already locked durable state. Other sources
// retain their existing occurrence-time projection contract.
func (s *Service) recordOperationalAlertOccurrenceProjection(tx *gorm.DB, sourceType, sourceID, groupKey, kind, severity, detailCode, subjectType, subjectID, subjectName string, occurredAt time.Time, modelCount int, credentialID string, inferenceState *entity.CredentialInferenceFailureState) error {
	if inferenceState != nil && (kind != notificationKindCredentialInference || !validCredentialInferenceState(*inferenceState) || inferenceState.CredentialID != credentialID) {
		return fmt.Errorf("invalid credential inference projection state")
	}
	if !validCredentialInferenceEnvelope(kind, credentialID) {
		return fmt.Errorf("invalid credential inference envelope")
	}
	if !validDiscoveryCount(kind, modelCount) {
		return fmt.Errorf("invalid first-recorded model count")
	}
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
		CredentialID: credentialID, FirstRecordedModelCount: modelCount, ID: alertID, GroupKey: groupKey, Kind: kind, Severity: severity, DetailCode: detailCode,
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
	occurrence := entity.OperationalAlertOccurrence{CredentialID: credentialID, FirstRecordedModelCount: modelCount, ID: occurrenceID, AlertID: alert.ID, SourceType: sourceType, SourceID: sourceID, DetailCode: detailCode, SubjectType: subjectType, SubjectID: subjectID, SubjectName: subjectName, OccurredAt: occurredAt}
	inserted := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "source_type"}, {Name: "source_id"}}, DoNothing: true}).Create(&occurrence)
	if inserted.Error != nil {
		return inserted.Error
	}
	if inserted.RowsAffected == 0 {
		return nil
	}
	var latestOccurrence entity.OperationalAlertOccurrence
	latestQuery := tx.Where("alert_id = ?", alert.ID).Order("occurred_at DESC, id DESC")
	projectedState := "open"
	if inferenceState != nil {
		// Acceptance sequence, not a timestamp captured before a row-lock wait,
		// determines which immutable episode supplies the latest snapshot.
		latestQuery = tx.Table("operational_alert_occurrences AS o").Select("o.*").
			Joins("JOIN credential_inference_failure_episodes AS e ON e.trigger_attempt_id = o.source_id").
			Where("o.alert_id = ? AND o.source_type = ? AND e.credential_digest = ?", alert.ID, notificationSourceCredentialInference, inferenceState.CredentialDigest).
			Order("e.acceptance_sequence DESC").Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := latestQuery.First(&latestOccurrence).Error; err != nil {
		return err
	}
	isLatest := latestOccurrence.ID == occurrence.ID
	resetRead := isLatest
	if inferenceState != nil {
		var latestEpisode entity.CredentialInferenceFailureEpisode
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("trigger_attempt_id = ?", latestOccurrence.SourceID).Take(&latestEpisode).Error; err != nil {
			return err
		}
		if !credentialInferenceEpisodeActive(*inferenceState, latestEpisode) {
			projectedState = "resolved"
		}
		resetRead = isLatest && projectedState == "open"
		if created.RowsAffected != 0 && projectedState != alert.State {
			if err := tx.Model(&alert).Update("state", projectedState).Error; err != nil {
				return err
			}
			alert.State = projectedState
		}
	}
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
			updates["state"] = projectedState
			updates["first_recorded_model_count"] = modelCount
			updates["credential_id"] = credentialID
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
			alert.FirstRecordedModelCount = modelCount
			alert.CredentialID = credentialID
			alert.LastSeenAt, alert.State, alert.DetailCode = occurredAt, projectedState, detailCode
			alert.SubjectType, alert.SubjectID, alert.SubjectName = subjectType, subjectID, subjectName
		}
	}
	return s.publishOperationalAlert(tx, alert, occurrence, latestOccurrence, isLatest, resetRead)
}

func maxTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func (s *Service) publishOperationalAlert(tx *gorm.DB, alert entity.OperationalAlert, occurrence, latestOccurrence entity.OperationalAlertOccurrence, isLatest, resetRead bool) error {
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
			CredentialID: alert.CredentialID, FirstRecordedModelCount: alert.FirstRecordedModelCount, ID: notificationID, RecipientID: user.ID, AlertID: alert.ID, LatestOccurrenceID: latestOccurrence.ID, Kind: alert.Kind,
			Severity: alert.Severity, DetailCode: alert.DetailCode, SubjectType: alert.SubjectType, SubjectID: alert.SubjectID, SubjectName: alert.SubjectName, OccurrenceCount: alert.OccurrenceCount,
			Read: false, FirstSeenAt: alert.FirstSeenAt, LastSeenAt: alert.LastSeenAt,
		}
		assignments := map[string]any{
			"kind": alert.Kind, "severity": alert.Severity, "detail_code": alert.DetailCode, "credential_id": alert.CredentialID,
			"subject_type": alert.SubjectType, "subject_id": alert.SubjectID, "subject_name": alert.SubjectName,
			"occurrence_count": alert.OccurrenceCount, "first_seen_at": alert.FirstSeenAt,
		}
		if isLatest {
			assignments["first_recorded_model_count"] = alert.FirstRecordedModelCount
			assignments["latest_occurrence_id"] = occurrence.ID
			if resetRead {
				assignments["read"] = false
				assignments["read_at"] = nil
			}
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
		deliveryCredentialID := alert.CredentialID
		deliverySubjectType, deliverySubjectID, deliverySubjectName := alert.SubjectType, alert.SubjectID, alert.SubjectName
		if alert.Kind == notificationKindCredentialInference {
			deliveryCredentialID = occurrence.CredentialID
			deliverySubjectType, deliverySubjectID, deliverySubjectName = occurrence.SubjectType, occurrence.SubjectID, occurrence.SubjectName
		}
		intent := entity.NotificationDeliveryIntent{
			CredentialID: deliveryCredentialID, ID: intentID, OccurrenceID: occurrence.ID, RecipientID: user.ID, RecipientEmail: settings.ExternalEmail,
			Kind: alert.Kind, Severity: alert.Severity, DetailCode: alert.DetailCode,
			SubjectType: deliverySubjectType, SubjectID: deliverySubjectID, SubjectName: deliverySubjectName, SMTPETag: smtp.ETag,
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
	case SystemJobSecretRootRotation:
		allowed = slices.Contains([]string{"rotation_blocked", "executor_lost"}, detail)
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
		Where("(j.code = ? AND j.detail_code IN ?) OR (j.code = ? AND j.detail_code IN ?) OR (j.code = ? AND j.detail_code IN ?) OR (j.code = ? AND j.detail_code IN ?)",
			SystemJobRuntimePublication, []string{"database_unavailable", "invalid_configuration", "publication_failed", "executor_lost"},
			SystemJobCallRecordDelivery, []string{"buffer_read_failed", "invalid_fact", "identity_mismatch", "persistence_failed", "acknowledge_failed", "executor_lost"},
			SystemJobStorageCleanup, []string{"claim_failed", "delete_failed", "state_update_failed", "executor_lost"},
			SystemJobSecretRootRotation, []string{"rotation_blocked", "executor_lost"}).
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
	if err := s.reconcileRouteUnavailableNotifications(tx); err != nil {
		return err
	}
	if err := s.reconcileModelDiscoveryNotices(tx); err != nil {
		return err
	}
	return s.reconcileCredentialInferenceFailures(tx)
}
