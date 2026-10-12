package service

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const notificationKindOffboardingCompleted = "offboarding_completed"

func offboardingCompletionRecipients(row entity.OffboardingCase, assignments OffboardingAssignments) []string {
	ids := []string{row.CompletedBy}
	for _, project := range assignments.Projects {
		ids = append(ids, project.ManagerUserIDs...)
	}
	for _, team := range assignments.Teams {
		ids = append(ids, team.OwnerUserIDs...)
	}
	ids = slices.DeleteFunc(ids, func(value string) bool { return value == "" || value == row.UserID })
	slices.Sort(ids)
	return slices.Compact(ids)
}

// publishOffboardingCompletion runs only inside the governance/user-locked
// completion transaction, after the Case and audit fact are saved. It never
// enumerates operators or current owners and never sends mail in the transaction.
func publishOffboardingCompletion(tx *gorm.DB, row entity.OffboardingCase, assignments OffboardingAssignments) error {
	if tx == nil || row.ID == "" || row.UserID == "" || row.CompletedBy == "" || row.Status != "completed" || row.CompletedAt == nil || row.CompletedAt.IsZero() {
		return fmt.Errorf("invalid offboarding completion notification")
	}
	var prior entity.OperationalAlertOccurrence
	if err := tx.Where(database.ExactText(tx, clause.Column{Name: "source_type"}, notificationKindOffboardingCompleted)).Where(database.ExactText(tx, clause.Column{Name: "source_id"}, row.ID)).Take(&prior).Error; err == nil {
		return nil // The original recipient set and intents remain immutable.
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	alertID, err := id.NewPrefixed("alr")
	if err != nil {
		return err
	}
	occurrenceID, err := id.NewPrefixed("occ")
	if err != nil {
		return err
	}
	etag, err := id.NewPrefixed("rev")
	if err != nil {
		return err
	}
	at := row.CompletedAt.UTC()
	alert := entity.OperationalAlert{ID: alertID, GroupKey: "offboarding_completed:" + row.ID, Kind: notificationKindOffboardingCompleted, Severity: "medium", DetailCode: "completed", State: "resolved", OccurrenceCount: 1, FirstSeenAt: at, LastSeenAt: at, ETag: etag, UpdatedAt: at}
	occurrence := entity.OperationalAlertOccurrence{ID: occurrenceID, AlertID: alertID, SourceType: notificationKindOffboardingCompleted, SourceID: row.ID, DetailCode: "completed", OccurredAt: at}
	if err := tx.Create(&alert).Error; err != nil {
		return err
	}
	if err := tx.Create(&occurrence).Error; err != nil {
		return err
	}
	var smtp entity.SMTPSetting
	if err := tx.First(&smtp, 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	smtpEligible := smtp.Enabled && smtp.ETag != "" && smtpclient.ValidAddress(smtp.SenderEmail) && smtpclient.ValidSenderName(smtp.SenderName)
	for _, recipientID := range offboardingCompletionRecipients(row, assignments) {
		recipient, err := registrationAdmittedUser(tx, recipientID, false)
		if errors.Is(err, apperrors.ErrUnauthorized) || errors.Is(err, apperrors.ErrForbidden) || errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if recipient.ID != recipientID || recipient.CreatedAt.IsZero() {
			return fmt.Errorf("invalid offboarding recipient birth")
		}
		notificationID, err := id.NewPrefixed("ntf")
		if err != nil {
			return err
		}
		notice := entity.Notification{ID: notificationID, RecipientID: recipientID, AlertID: alertID, LatestOccurrenceID: occurrenceID, Kind: notificationKindOffboardingCompleted, Severity: "medium", DetailCode: "completed", OccurrenceCount: 1, FirstSeenAt: at, LastSeenAt: at}
		if err := tx.Create(&notice).Error; err != nil {
			return err
		}
		receipt := entity.OffboardingNotificationRecipient{CaseID: row.ID, RecipientID: recipientID, OccurrenceID: occurrenceID, NotificationID: notificationID, RecipientCreatedAt: recipient.CreatedAt}
		if err := tx.Create(&receipt).Error; err != nil {
			return err
		}
		settings, err := defaultNotificationSettings(tx, recipientID)
		if err != nil {
			return err
		}
		if settings.UserID != recipientID {
			return fmt.Errorf("invalid notification preference identity")
		}
		if !smtpEligible || !settings.EmailMedium || !smtpclient.ValidAddress(settings.ExternalEmail) {
			continue
		}
		intentID, err := id.NewPrefixed("ndl")
		if err != nil {
			return err
		}
		intent := entity.NotificationDeliveryIntent{ID: intentID, OccurrenceID: occurrenceID, RecipientID: recipientID, RecipientEmail: settings.ExternalEmail, Kind: notificationKindOffboardingCompleted, Severity: "medium", DetailCode: "completed", SMTPETag: smtp.ETag, Status: "pending", NextAttemptAt: at, CreatedAt: at, UpdatedAt: at}
		if err := tx.Create(&intent).Error; err != nil {
			return err
		}
	}
	return nil
}

// Recipient history uses its immutable receipt, not today's resource assignments.
// Exact joins also reject MySQL case/trailing-space collation aliases.
func offboardingInboxQuery(tx *gorm.DB, access quotaInboxAccess) *gorm.DB {
	query := tx.Model(&entity.Notification{}).
		Joins("JOIN offboarding_notification_recipients AS receipt ON receipt.notification_id = notifications.id").
		Joins("JOIN operational_alert_occurrences AS occurrence ON occurrence.id = receipt.occurrence_id").
		Joins("JOIN offboarding_cases AS completed_case ON completed_case.id = receipt.case_id").
		Where(database.ExactText(tx, clause.Column{Table: "notifications", Name: "recipient_id"}, access.ActorID)).
		Where(database.ExactText(tx, clause.Column{Table: "receipt", Name: "recipient_id"}, access.ActorID)).
		Where("receipt.recipient_created_at = ?", access.ActorCreatedAt).
		Where(database.ExactText(tx, clause.Column{Table: "notifications", Name: "kind"}, notificationKindOffboardingCompleted)).
		Where(database.ExactText(tx, clause.Column{Table: "notifications", Name: "severity"}, "medium")).
		Where(database.ExactText(tx, clause.Column{Table: "notifications", Name: "detail_code"}, "completed")).
		Where("notifications.occurrence_count = ?", 1).
		Where("OCTET_LENGTH(notifications.subject_type) = 0 AND OCTET_LENGTH(notifications.subject_id) = 0 AND OCTET_LENGTH(notifications.subject_name) = 0").
		Where("OCTET_LENGTH(occurrence.subject_type) = 0 AND OCTET_LENGTH(occurrence.subject_id) = 0 AND OCTET_LENGTH(occurrence.subject_name) = 0").
		Where(database.ExactText(tx, clause.Column{Table: "occurrence", Name: "source_type"}, notificationKindOffboardingCompleted)).
		Where(database.ExactText(tx, clause.Column{Table: "occurrence", Name: "detail_code"}, "completed")).
		Where(database.ExactText(tx, clause.Column{Table: "completed_case", Name: "status"}, "completed")).
		Where("completed_case.completed_at IS NOT NULL AND completed_case.completed_at = occurrence.occurred_at AND notifications.first_seen_at = occurrence.occurred_at AND notifications.last_seen_at = occurrence.occurred_at")
	for _, pair := range [][2]clause.Column{
		{{Table: "receipt", Name: "notification_id"}, {Table: "notifications", Name: "id"}},
		{{Table: "receipt", Name: "occurrence_id"}, {Table: "occurrence", Name: "id"}},
		{{Table: "notifications", Name: "latest_occurrence_id"}, {Table: "occurrence", Name: "id"}},
		{{Table: "notifications", Name: "alert_id"}, {Table: "occurrence", Name: "alert_id"}},
		{{Table: "receipt", Name: "case_id"}, {Table: "completed_case", Name: "id"}},
		{{Table: "occurrence", Name: "source_id"}, {Table: "completed_case", Name: "id"}},
	} {
		query = query.Where(database.ExactTextColumns(tx, pair[0], pair[1]))
	}
	if access.ActorCreatedAt.IsZero() {
		query = query.Where("1 = 0")
	}
	return query
}

func offboardingNotificationPage(tx *gorm.DB, access quotaInboxAccess, filter NotificationFilter, limit int, cursorTime time.Time, cursorID string) ([]NotificationRecord, int64, error) {
	query := offboardingInboxQuery(tx, access)
	if filter.Severity != "" {
		query = query.Where("notifications.severity = ?", filter.Severity)
	}
	if filter.UnreadOnly {
		query = query.Where(clause.Eq{Column: clause.Column{Table: "notifications", Name: "read"}, Value: false})
	}
	if filter.Cursor != "" {
		query = query.Where("notifications.last_seen_at < ? OR (notifications.last_seen_at = ? AND notifications.id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []entity.Notification
	if err := query.Select("notifications.*").Order("notifications.last_seen_at DESC, notifications.id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	occurrenceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		occurrenceIDs = append(occurrenceIDs, row.LatestOccurrenceID)
	}
	var intents []entity.NotificationDeliveryIntent
	if len(occurrenceIDs) > 0 {
		if err := tx.Where("occurrence_id IN ?", occurrenceIDs).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Find(&intents).Error; err != nil {
			return nil, 0, err
		}
	}
	byOccurrence := make(map[string]entity.NotificationDeliveryIntent, len(intents))
	for _, intent := range intents {
		if intent.RecipientID == access.ActorID && validOffboardingDeliveryMetadata(intent) {
			byOccurrence[intent.OccurrenceID] = intent
		}
	}
	records := make([]NotificationRecord, 0, len(rows))
	for _, row := range rows {
		record := notificationRecord(row)
		if intent, ok := byOccurrence[row.LatestOccurrenceID]; ok {
			record.DeliveryStatus, record.DeliveryCode = intent.Status, intent.ResultCode
			record.DeliveryAttempts, record.DeliveryUpdatedAt = intent.Attempts, &intent.UpdatedAt
		}
		records = append(records, record)
	}
	var unread int64
	err := offboardingInboxQuery(tx, access).Where(clause.Eq{Column: clause.Column{Table: "notifications", Name: "read"}, Value: false}).Count(&unread).Error
	return records, unread, err
}

func markOffboardingNotificationRead(tx *gorm.DB, access quotaInboxAccess, notificationID string) (NotificationRecord, bool, error) {
	var row entity.Notification
	err := offboardingInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "notifications", Name: "id"}, notificationID)).Select("notifications.*").Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotificationRecord{}, false, nil
	}
	if err != nil {
		return NotificationRecord{}, false, err
	}
	if !row.Read {
		now := time.Now().UTC()
		if err := tx.Model(&entity.Notification{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, row.ID)).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Updates(map[string]any{"read": true, "read_at": now}).Error; err != nil {
			return NotificationRecord{}, false, err
		}
		row.Read, row.ReadAt = true, &now
	}
	return notificationRecord(row), true, nil
}

// Select then update bounded exact IDs rather than selecting from the UPDATE
// target in a subquery, which MySQL rejects. The governance lock covers every
// batch and the request context bounds the complete operation.
func markAllOffboardingNotificationsRead(tx *gorm.DB, access quotaInboxAccess, now time.Time) error {
	for {
		var ids []string
		if err := offboardingInboxQuery(tx, access).Where(clause.Eq{Column: clause.Column{Table: "notifications", Name: "read"}, Value: false}).Order("notifications.id").Limit(500).Pluck("notifications.id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		result := tx.Model(&entity.Notification{}).Where("id IN ?", ids).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, access.ActorID)).Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).Updates(map[string]any{"read": true, "read_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != int64(len(ids)) {
			return fmt.Errorf("notification read set changed")
		}
	}
}

func validOffboardingDeliveryMetadata(intent entity.NotificationDeliveryIntent) bool {
	return intent.Kind == notificationKindOffboardingCompleted && intent.Severity == "medium" && intent.DetailCode == "completed" && intent.SubjectType == "" && intent.SubjectID == "" && intent.SubjectName == ""
}

func offboardingDeliveryEligible(tx *gorm.DB, intent entity.NotificationDeliveryIntent) bool {
	if !validOffboardingDeliveryMetadata(intent) {
		return false
	}
	actor, err := registrationAdmittedUser(tx, intent.RecipientID, false)
	if err != nil || actor.ID != intent.RecipientID || actor.CreatedAt.IsZero() {
		return false
	}
	var row entity.Notification
	err = offboardingInboxQuery(tx, quotaInboxAccess{ActorID: actor.ID, ActorCreatedAt: actor.CreatedAt}).Where(database.ExactText(tx, clause.Column{Table: "receipt", Name: "occurrence_id"}, intent.OccurrenceID)).Select("notifications.*").Take(&row).Error
	return err == nil
}
