package service

import (
	"context"
	"encoding/base64"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

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

type NotificationRecord struct {
	ID                string     `json:"id"`
	AlertID           string     `json:"alert_id"`
	Kind              string     `json:"kind"`
	Severity          string     `json:"severity"`
	DetailCode        string     `json:"detail_code"`
	OccurrenceCount   int        `json:"occurrence_count"`
	Read              bool       `json:"read"`
	FirstSeenAt       time.Time  `json:"first_seen_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	ReadAt            *time.Time `json:"read_at"`
	DeliveryStatus    string     `json:"delivery_status,omitempty"`
	DeliveryCode      string     `json:"delivery_code,omitempty"`
	DeliveryAttempts  int        `json:"delivery_attempts,omitempty"`
	DeliveryUpdatedAt *time.Time `json:"delivery_updated_at,omitempty"`
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
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	query := db.Where("recipient_id = ?", actor)
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
		return nil, catalogError(err)
	}
	page := &NotificationPage{Items: make([]NotificationRecord, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		page.NextCursor = encodeNotificationCursor(rows[limit-1].LastSeenAt, rows[limit-1].ID)
		rows = rows[:limit]
	}
	occurrenceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		occurrenceIDs = append(occurrenceIDs, row.LatestOccurrenceID)
	}
	deliveriesByOccurrence := map[string]entity.NotificationDeliveryIntent{}
	if len(occurrenceIDs) > 0 {
		var deliveries []entity.NotificationDeliveryIntent
		if err := db.Where("recipient_id = ? AND occurrence_id IN ?", actor, occurrenceIDs).Find(&deliveries).Error; err != nil {
			return nil, catalogError(err)
		}
		for _, delivery := range deliveries {
			deliveriesByOccurrence[delivery.OccurrenceID] = delivery
		}
	}
	for _, row := range rows {
		record := notificationRecord(row)
		if delivery := deliveriesByOccurrence[row.LatestOccurrenceID]; delivery.ID != "" {
			record.DeliveryStatus, record.DeliveryCode = delivery.Status, delivery.ResultCode
			record.DeliveryAttempts, record.DeliveryUpdatedAt = delivery.Attempts, &delivery.UpdatedAt
		}
		page.Items = append(page.Items, record)
	}
	if err := db.Model(&entity.Notification{}).Where("recipient_id = ?", actor).Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).Count(&page.UnreadCount).Error; err != nil {
		return nil, catalogError(err)
	}
	return page, nil
}

func notificationRecord(row entity.Notification) NotificationRecord {
	return NotificationRecord{
		ID: row.ID, AlertID: row.AlertID, Kind: row.Kind, Severity: row.Severity,
		DetailCode: row.DetailCode, OccurrenceCount: row.OccurrenceCount, Read: row.Read,
		FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, ReadAt: row.ReadAt,
	}
}

func (s *Service) MarkNotificationRead(ctx context.Context, actor, notificationID string) (*NotificationRecord, error) {
	if notificationID == "" || len(notificationID) > 30 {
		return nil, apperrors.ErrBadRequest
	}
	now := time.Now().UTC()
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	result := db.Model(&entity.Notification{}).Where("id = ? AND recipient_id = ?", notificationID, actor).Updates(map[string]any{"read": true, "read_at": now})
	if result.Error != nil {
		return nil, catalogError(result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, apperrors.ErrNotFound
	}
	var row entity.Notification
	if err := db.Where("id = ? AND recipient_id = ?", notificationID, actor).First(&row).Error; err != nil {
		return nil, catalogError(err)
	}
	record := notificationRecord(row)
	return &record, nil
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, actor string) error {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return catalogError(err)
	}
	now := time.Now().UTC()
	return catalogError(db.Model(&entity.Notification{}).Where("recipient_id = ?", actor).Where(clause.Eq{Column: clause.Column{Name: "read"}, Value: false}).Updates(map[string]any{"read": true, "read_at": now}).Error)
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
	if input.ETag == "" || !smtpclient.ValidAddress(input.ExternalEmail) {
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
	return OperationalAlertRecord{ID: row.ID, Kind: row.Kind, Severity: row.Severity, DetailCode: row.DetailCode, State: row.State, ETag: row.ETag, OccurrenceCount: row.OccurrenceCount, FirstSeenAt: row.FirstSeenAt, LastSeenAt: row.LastSeenAt, UpdatedAt: row.UpdatedAt}
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
