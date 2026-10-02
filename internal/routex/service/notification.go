package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const notificationPageLimit = 100

type NotificationFilter struct {
	UnreadOnly bool
	Severity   string
	Cursor     string
	Limit      int
}

type QuotaNotificationSnapshot struct {
	ScopeKind      string    `json:"scope_kind"`
	ScopeID        string    `json:"scope_id"`
	Dimension      string    `json:"dimension"`
	PolicyRevision string    `json:"policy_revision"`
	MonthStart     time.Time `json:"month_start"`
	MonthEnd       time.Time `json:"month_end"`
	TimeZone       string    `json:"time_zone"`
	AsOf           time.Time `json:"as_of"`
	Limit          string    `json:"limit"`
	Settled        string    `json:"settled"`
	Currency       *string   `json:"currency"`
}

type NotificationRecord struct {
	QuotaObservationID string                     `json:"quota_observation_id,omitempty"`
	Quota              *QuotaNotificationSnapshot `json:"quota,omitempty"`
	ID                 string                     `json:"id"`
	AlertID            string                     `json:"alert_id,omitempty"`
	Kind               string                     `json:"kind"`
	Severity           string                     `json:"severity"`
	DetailCode         string                     `json:"detail_code"`
	SubjectType        string                     `json:"subject_type,omitempty"`
	SubjectID          string                     `json:"subject_id,omitempty"`
	SubjectName        string                     `json:"subject_name,omitempty"`
	OccurrenceCount    int                        `json:"occurrence_count"`
	Read               bool                       `json:"read"`
	FirstSeenAt        time.Time                  `json:"first_seen_at"`
	LastSeenAt         time.Time                  `json:"last_seen_at"`
	ReadAt             *time.Time                 `json:"read_at"`
	DeliveryStatus     string                     `json:"delivery_status,omitempty"`
	DeliveryCode       string                     `json:"delivery_code,omitempty"`
	DeliveryAttempts   int                        `json:"delivery_attempts,omitempty"`
	DeliveryUpdatedAt  *time.Time                 `json:"delivery_updated_at,omitempty"`
}

type NotificationPage struct {
	Items       []NotificationRecord `json:"items"`
	NextCursor  string               `json:"next_cursor,omitempty"`
	UnreadCount int64                `json:"unread_count"`
}

type NotificationSettingsView struct {
	InAppEnabled  bool      `json:"in_app_enabled"`
	ExternalEmail string    `json:"external_email"`
	EmailHigh     bool      `json:"email_high"`
	EmailMedium   bool      `json:"email_medium"`
	ETag          string    `json:"etag"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NotificationSettingsInput struct {
	ExternalEmail string `json:"external_email"`
	EmailHigh     bool   `json:"email_high"`
	EmailMedium   bool   `json:"email_medium"`
	ETag          string `json:"etag"`
}

type OperationalAlertFilter struct {
	State, Severity, Cursor string
	Limit                   int
}

type OperationalAlertRecord struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	Severity        string    `json:"severity"`
	DetailCode      string    `json:"detail_code"`
	SubjectType     string    `json:"subject_type,omitempty"`
	SubjectID       string    `json:"subject_id,omitempty"`
	SubjectName     string    `json:"subject_name,omitempty"`
	State           string    `json:"state"`
	ETag            string    `json:"etag"`
	OccurrenceCount int       `json:"occurrence_count"`
	FirstSeenAt     time.Time `json:"first_seen_at"`
	LastSeenAt      time.Time `json:"last_seen_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type OperationalAlertPage struct {
	Items      []OperationalAlertRecord `json:"items"`
	NextCursor string                   `json:"next_cursor,omitempty"`
}

type OperationalAlertStateInput struct {
	State string `json:"state"`
	ETag  string `json:"etag"`
}

func validateNotificationPage(limit int, cursor string) (int, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > notificationPageLimit || len(cursor) > 128 {
		return 0, apperrors.ErrBadRequest
	}
	return limit, nil
}

func encodeNotificationCursor(lastSeenAt time.Time, id string) string {
	raw := strconv.FormatInt(lastSeenAt.UTC().UnixMicro(), 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeNotificationCursor(raw string) (time.Time, string, error) {
	if raw == "" {
		return time.Time{}, "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return time.Time{}, "", apperrors.ErrBadRequest
	}
	timestamp, idValue, ok := strings.Cut(string(decoded), "|")
	micros, err := strconv.ParseInt(timestamp, 10, 64)
	if !ok || err != nil || micros <= 0 || idValue == "" || len(idValue) > 30 {
		return time.Time{}, "", apperrors.ErrBadRequest
	}
	return time.UnixMicro(micros).UTC(), idValue, nil
}

func (s *Service) ListNotifications(ctx context.Context, actor string, filter NotificationFilter) (*NotificationPage, error) {
	limit, err := validateNotificationPage(filter.Limit, filter.Cursor)
	if err != nil || (filter.Severity != "" && !slices.Contains([]string{"high", "medium"}, filter.Severity)) {
		return nil, apperrors.ErrBadRequest
	}
	cursorTime, cursorID, err := decodeNotificationCursor(filter.Cursor)
	if err != nil {
		return nil, err
	}
	page := &NotificationPage{Items: []NotificationRecord{}}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		access, err := loadQuotaInboxAccess(tx, actor)
		if err != nil {
			return err
		}
		records := make([]NotificationRecord, 0, 2*(limit+1))
		if access.Operational {
			query := operationalInboxQuery(tx, actor)
			if filter.UnreadOnly {
				query = query.Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false})
			}
			if filter.Severity != "" {
				query = query.Where("severity = ?", filter.Severity)
			}
			if filter.Cursor != "" {
				query = query.Where("last_seen_at < ? OR (last_seen_at = ? AND id < ?)", cursorTime, cursorTime, cursorID)
			}
			var rows []entity.Notification
			if err := query.Order("last_seen_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
				return err
			}
			occurrenceIDs := make([]string, 0, len(rows))
			for _, row := range rows {
				if row.RecipientID == actor {
					occurrenceIDs = append(occurrenceIDs, row.LatestOccurrenceID)
				}
			}
			deliveriesByOccurrence := map[string]entity.NotificationDeliveryIntent{}
			if len(occurrenceIDs) > 0 {
				var deliveries []entity.NotificationDeliveryIntent
				if err := tx.Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, actor)).Where("occurrence_id IN ?", occurrenceIDs).Find(&deliveries).Error; err != nil {
					return err
				}
				for _, delivery := range deliveries {
					if delivery.RecipientID == actor && slices.Contains(occurrenceIDs, delivery.OccurrenceID) {
						deliveriesByOccurrence[delivery.OccurrenceID] = delivery
					}
				}
			}
			for _, row := range rows {
				if row.RecipientID != actor {
					continue
				}
				record := notificationRecord(row)
				if delivery := deliveriesByOccurrence[row.LatestOccurrenceID]; delivery.ID != "" {
					record.DeliveryStatus, record.DeliveryCode = delivery.Status, delivery.ResultCode
					record.DeliveryAttempts, record.DeliveryUpdatedAt = delivery.Attempts, &delivery.UpdatedAt
				}
				records = append(records, record)
			}
			if err := operationalInboxQuery(tx, actor).Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).Count(&page.UnreadCount).Error; err != nil {
				return err
			}
		}
		if filter.Severity != "medium" {
			query := quotaInboxQuery(tx, access)
			if filter.UnreadOnly {
				query = query.Where("quota_notification_inboxes.read_at IS NULL")
			}
			if filter.Cursor != "" {
				query = query.Where("quota_notification_inboxes.created_at < ? OR (quota_notification_inboxes.created_at = ? AND quota_notification_inboxes.id < ?)", cursorTime, cursorTime, cursorID)
			}
			var rows []quotaInboxRow
			if err := query.Select(quotaInboxSelect).Order("quota_notification_inboxes.created_at DESC, quota_notification_inboxes.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if validQuotaInboxRow(row, access) {
					records = append(records, quotaNotificationRecord(row))
				}
			}
		}
		var quotaUnread int64
		if err := quotaInboxQuery(tx, access).Where("quota_notification_inboxes.read_at IS NULL").Count(&quotaUnread).Error; err != nil {
			return err
		}
		page.UnreadCount += quotaUnread
		page.Items, page.NextCursor = mergeNotificationRecords(records, limit)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, catalogError(err)
	}
	return page, nil
}

func operationalInboxQuery(tx *gorm.DB, actor string) *gorm.DB {
	return tx.Model(&entity.Notification{}).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, actor))
}

func mergeNotificationRecords(records []NotificationRecord, limit int) ([]NotificationRecord, string) {
	slices.SortFunc(records, func(a, b NotificationRecord) int {
		if compared := b.LastSeenAt.Compare(a.LastSeenAt); compared != 0 {
			return compared
		}
		return strings.Compare(b.ID, a.ID)
	})
	if len(records) > limit {
		last := records[limit-1]
		return records[:limit], encodeNotificationCursor(last.LastSeenAt, last.ID)
	}
	return records, ""
}

func notificationRecord(row entity.Notification) NotificationRecord {
	return NotificationRecord{
		ID: row.ID, AlertID: row.AlertID, Kind: row.Kind, Severity: row.Severity,
		DetailCode: row.DetailCode, SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectName: row.SubjectName,
		OccurrenceCount: row.OccurrenceCount, Read: row.Read,
		FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, ReadAt: row.ReadAt,
	}
}

func (s *Service) MarkNotificationRead(ctx context.Context, actor, notificationID string) (*NotificationRecord, error) {
	if notificationID == "" || len(notificationID) > 30 {
		return nil, apperrors.ErrBadRequest
	}
	var record NotificationRecord
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		access, err := loadQuotaInboxAccess(tx, actor)
		if err != nil {
			return err
		}
		var quotaRow quotaInboxRow
		err = quotaInboxQuery(tx, access).Where(database.ExactText(tx, clause.Column{Table: "quota_notification_inboxes", Name: "id"}, notificationID)).Select(quotaInboxSelect).Take(&quotaRow).Error
		if err == nil && validQuotaInboxRow(quotaRow, access) {
			if quotaRow.ReadAt == nil {
				now := time.Now().UTC()
				if err := tx.Model(&entity.QuotaNotificationInbox{}).Where(database.ExactText(tx, clause.Column{Name: "id"}, notificationID)).Where(database.ExactText(tx, clause.Column{Name: "recipient_id"}, actor)).Update("read_at", now).Error; err != nil {
					return err
				}
				quotaRow.ReadAt = &now
			}
			record = quotaNotificationRecord(quotaRow)
			return nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !access.Operational {
			return apperrors.ErrNotFound
		}
		var row entity.Notification
		err = operationalInboxQuery(tx, actor).Where(database.ExactText(tx, clause.Column{Name: "id"}, notificationID)).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (row.ID != notificationID || row.RecipientID != actor) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		if !row.Read {
			now := time.Now().UTC()
			if err := operationalInboxQuery(tx, actor).Where(database.ExactText(tx, clause.Column{Name: "id"}, notificationID)).Updates(map[string]any{"read": true, "read_at": now}).Error; err != nil {
				return err
			}
			row.Read, row.ReadAt = true, &now
		}
		record = notificationRecord(row)
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, catalogError(err)
	}
	return &record, nil
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, actor string) error {
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		access, err := loadQuotaInboxAccess(tx, actor)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if access.Operational {
			if err := operationalInboxQuery(tx, actor).Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).Updates(map[string]any{"read": true, "read_at": now}).Error; err != nil {
				return err
			}
		}
		return quotaInboxMutationQuery(tx, access).Where("read_at IS NULL").Update("read_at", now).Error
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	return catalogError(err)
}

func notificationSettingsView(row entity.NotificationSetting) NotificationSettingsView {
	return NotificationSettingsView{InAppEnabled: true, ExternalEmail: row.ExternalEmail, EmailHigh: row.EmailHigh, EmailMedium: row.EmailMedium, ETag: row.ETag, UpdatedAt: row.UpdatedAt}
}

func defaultNotificationSettings(db *gorm.DB, actor string) (entity.NotificationSetting, error) {
	var row entity.NotificationSetting
	err := db.First(&row, "user_id = ?", actor).Error
	if err == nil {
		return row, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return row, err
	}
	var user entity.User
	if err := db.Where("id = ? AND disabled = ?", actor, false).First(&user).Error; err != nil {
		return row, err
	}
	return entity.NotificationSetting{UserID: actor, ExternalEmail: user.Email, ETag: "0"}, nil
}

func (s *Service) GetNotificationSettings(ctx context.Context, actor string) (*NotificationSettingsView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	row, err := defaultNotificationSettings(db, actor)
	if err != nil {
		return nil, catalogError(err)
	}
	view := notificationSettingsView(row)
	return &view, nil
}

func (s *Service) WriteNotificationSettings(ctx context.Context, actor string, input NotificationSettingsInput) (*NotificationSettingsView, error) {
	input.ExternalEmail = strings.TrimSpace(input.ExternalEmail)
	if input.ETag == "" || (input.EmailHigh || input.EmailMedium) && !smtpclient.ValidAddress(input.ExternalEmail) || input.ExternalEmail != "" && !smtpclient.ValidAddress(input.ExternalEmail) {
		return nil, apperrors.ErrBadRequest
	}
	var saved entity.NotificationSetting
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actor, "system.write"); err != nil {
			return err
		}
		current, err := defaultNotificationSettings(tx, actor)
		if err != nil {
			return err
		}
		if current.ETag != input.ETag {
			return catalogConflict
		}
		etag, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		saved = entity.NotificationSetting{UserID: actor, ExternalEmail: input.ExternalEmail, EmailHigh: input.EmailHigh, EmailMedium: input.EmailMedium, ETag: etag, UpdatedAt: now}
		if current.ETag == "0" {
			if err := tx.Create(&saved).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) {
					return catalogConflict
				}
				return err
			}
		} else {
			result := tx.Model(&entity.NotificationSetting{}).
				Where("user_id = ? AND e_tag = ?", actor, input.ETag).
				Updates(map[string]any{
					"external_email": saved.ExternalEmail,
					"email_high":     saved.EmailHigh,
					"email_medium":   saved.EmailMedium,
					"e_tag":          saved.ETag,
					"updated_at":     saved.UpdatedAt,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return catalogConflict
			}
		}
		return appendAudit(tx, actor, "notification.settings.update", "notification_setting", actor)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	view := notificationSettingsView(saved)
	return &view, nil
}

func operationalAlertRecord(row entity.OperationalAlert) OperationalAlertRecord {
	return OperationalAlertRecord{ID: row.ID, Kind: row.Kind, Severity: row.Severity, DetailCode: row.DetailCode, SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectName: row.SubjectName, State: row.State, ETag: row.ETag, OccurrenceCount: row.OccurrenceCount, FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, UpdatedAt: row.UpdatedAt}
}

func (s *Service) ListOperationalAlerts(ctx context.Context, actor string, filter OperationalAlertFilter) (*OperationalAlertPage, error) {
	limit, err := validateNotificationPage(filter.Limit, filter.Cursor)
	if err != nil || (filter.State != "" && !slices.Contains([]string{"open", "handling", "resolved"}, filter.State)) || (filter.Severity != "" && !slices.Contains([]string{"high", "medium"}, filter.Severity)) {
		return nil, apperrors.ErrBadRequest
	}
	cursorTime, cursorID, err := decodeNotificationCursor(filter.Cursor)
	if err != nil {
		return nil, err
	}
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return s.reconcileNotificationSources(tx) }); err != nil {
		return nil, catalogError(err)
	}
	query := db.Model(&entity.OperationalAlert{})
	if filter.State != "" {
		query = query.Where("state = ?", filter.State)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Cursor != "" {
		query = query.Where("last_seen_at < ? OR (last_seen_at = ? AND id < ?)", cursorTime, cursorTime, cursorID)
	}
	var rows []entity.OperationalAlert
	if err := query.Order("last_seen_at DESC, id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	page := &OperationalAlertPage{Items: make([]OperationalAlertRecord, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		page.NextCursor = encodeNotificationCursor(rows[limit-1].LastSeenAt, rows[limit-1].ID)
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, operationalAlertRecord(row))
	}
	return page, nil
}

func (s *Service) UpdateOperationalAlertState(ctx context.Context, actor, alertID string, input OperationalAlertStateInput) (*OperationalAlertRecord, error) {
	if alertID == "" || input.ETag == "" || !slices.Contains([]string{"open", "handling", "resolved"}, input.State) {
		return nil, apperrors.ErrBadRequest
	}
	var saved entity.OperationalAlert
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeGovernance(tx, actor, "system.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&saved, "id = ?", alertID).Error; err != nil {
			return err
		}
		if saved.ETag != input.ETag {
			return catalogConflict
		}
		etag, err := id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		if err := tx.Model(&saved).Updates(map[string]any{"state": input.State, "e_tag": etag, "updated_at": now}).Error; err != nil {
			return err
		}
		saved.State, saved.ETag, saved.UpdatedAt = input.State, etag, now
		return appendAudit(tx, actor, "operational_alert.state", "operational_alert", alertID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	record := operationalAlertRecord(saved)
	return &record, nil
}
