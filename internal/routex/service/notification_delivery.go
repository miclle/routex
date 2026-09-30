package service

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	notificationDeliveryLease       = 30 * time.Second
	notificationDeliveryPoll        = 5 * time.Second
	notificationDeliveryMaxAttempts = 3
)

type NotificationDeliveryFilter struct {
	Status, Cursor string
	Limit          int
}

type NotificationDeliveryRecord struct {
	ID          string     `json:"id"`
	RecipientID string     `json:"recipient_id"`
	Kind        string     `json:"kind"`
	Severity    string     `json:"severity"`
	DetailCode  string     `json:"detail_code"`
	SubjectType string     `json:"subject_type,omitempty"`
	SubjectID   string     `json:"subject_id,omitempty"`
	SubjectName string     `json:"subject_name,omitempty"`
	Status      string     `json:"status"`
	ResultCode  string     `json:"result_code"`
	Attempts    int        `json:"attempts"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type NotificationDeliveryPage struct {
	Items      []NotificationDeliveryRecord `json:"items"`
	NextCursor string                       `json:"next_cursor,omitempty"`
}

func notificationDeliveryRecord(row entity.NotificationDeliveryIntent) NotificationDeliveryRecord {
	return NotificationDeliveryRecord{
		ID: row.ID, RecipientID: row.RecipientID, Kind: row.Kind, Severity: row.Severity,
		DetailCode: row.DetailCode, SubjectType: row.SubjectType, SubjectID: row.SubjectID, SubjectName: row.SubjectName,
		Status: row.Status, ResultCode: row.ResultCode,
		Attempts: row.Attempts, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, CompletedAt: row.CompletedAt,
	}
}

func (s *Service) ListNotificationDeliveries(ctx context.Context, actor string, filter NotificationDeliveryFilter) (*NotificationDeliveryPage, error) {
	limit, err := validateNotificationPage(filter.Limit, filter.Cursor)
	statuses := []string{"pending", "retry", "sending", "accepted", "unknown", "failed"}
	if err != nil || len(filter.Cursor) > 30 || (filter.Status != "" && !slices.Contains(statuses, filter.Status)) {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "system.read"); err != nil {
		return nil, catalogError(err)
	}
	query := db.Model(&entity.NotificationDeliveryIntent{})
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Cursor != "" {
		query = query.Where("id < ?", filter.Cursor)
	}
	var rows []entity.NotificationDeliveryIntent
	if err := query.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	page := &NotificationDeliveryPage{Items: make([]NotificationDeliveryRecord, 0, min(limit, len(rows)))}
	if len(rows) > limit {
		page.NextCursor = rows[limit-1].ID
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, notificationDeliveryRecord(row))
	}
	return page, nil
}

// StartNotificationDelivery starts a bounded worker. The returned stop function
// cancels and joins it before callers close the database pool.
func (s *Service) StartNotificationDelivery(ctx context.Context) (func(), error) {
	var rows []entity.NotificationDeliveryIntent
	if err := s.authDB(ctx).Select("id").Limit(1).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(notificationDeliveryPoll)
		defer ticker.Stop()
		for {
			_, _ = s.FlushNotificationDelivery(run, 20)
			select {
			case <-run.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

func (s *Service) FlushNotificationDelivery(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, apperrors.ErrBadRequest
	}
	now := time.Now().UTC()
	if err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error { return s.reconcileNotificationSources(tx) }); err != nil {
		return 0, catalogError(err)
	}
	if err := s.authDB(ctx).Model(&entity.NotificationDeliveryIntent{}).
		Where("status = ? AND lease_until IS NOT NULL AND lease_until <= ?", "sending", now).
		Updates(map[string]any{"status": "unknown", "result_code": "lease_expired", "lease_token": "", "lease_until": nil, "updated_at": now, "completed_at": now}).Error; err != nil {
		return 0, catalogError(err)
	}
	completed := 0
	for completed < limit {
		intent, ok, err := s.claimNotificationDelivery(ctx, now)
		if err != nil {
			return completed, catalogError(err)
		}
		if !ok {
			return completed, nil
		}
		if err := s.executeNotificationDelivery(ctx, intent); err != nil {
			return completed, catalogError(err)
		}
		completed++
		now = time.Now().UTC()
	}
	return completed, nil
}

func (s *Service) claimNotificationDelivery(ctx context.Context, now time.Time) (entity.NotificationDeliveryIntent, bool, error) {
	var claimed entity.NotificationDeliveryIntent
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		var row entity.NotificationDeliveryIntent
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND next_attempt_at <= ?", []string{"pending", "retry"}, now).
			Order("next_attempt_at, id").First(&row).Error
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		leaseToken, err := id.NewPrefixed("lck")
		if err != nil {
			return err
		}
		leaseUntil := now.Add(notificationDeliveryLease)
		result := tx.Model(&row).Where("status IN ?", []string{"pending", "retry"}).Updates(map[string]any{
			"status": "sending", "attempts": gorm.Expr("attempts + 1"), "lease_token": leaseToken,
			"lease_until": leaseUntil, "result_code": "", "updated_at": now,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		row.Status, row.LeaseToken, row.LeaseUntil, row.UpdatedAt = "sending", leaseToken, &leaseUntil, now
		row.Attempts++
		claimed = row
		return nil
	})
	return claimed, claimed.ID != "", err
}

func (s *Service) executeNotificationDelivery(ctx context.Context, intent entity.NotificationDeliveryIntent) error {
	now := time.Now().UTC()
	if err := authorizeGovernance(s.authDB(ctx), intent.RecipientID, "system.read"); err != nil {
		return s.finishNotificationDelivery(ctx, intent, "failed", "recipient_ineligible", now, true)
	}
	var smtp entity.SMTPSetting
	if err := s.authDB(ctx).First(&smtp, 1).Error; err != nil {
		return s.finishNotificationDelivery(ctx, intent, "failed", "configuration_unavailable", now, true)
	}
	if !smtp.Enabled || smtp.ETag != intent.SMTPETag {
		return s.finishNotificationDelivery(ctx, intent, "failed", "configuration_changed", now, true)
	}
	config, err := s.smtpConfig(smtp)
	if err != nil {
		return s.finishNotificationDelivery(ctx, intent, "failed", "configuration_unavailable", now, true)
	}
	message := smtpclient.Message{
		From: smtp.SenderEmail, Name: smtp.SenderName, ReplyTo: smtp.ReplyTo, To: intent.RecipientEmail,
		RequestID: intent.ID, Subject: "RouteX operational alert: " + intent.Severity,
		Body: notificationDeliveryBody(intent),
	}
	result := smtpclient.New(s.allowPrivateSMTP).Send(ctx, config, message)
	now = time.Now().UTC()
	switch result.Status {
	case "accepted":
		return s.finishNotificationDelivery(ctx, intent, "accepted", "accepted", now, true)
	case "unknown":
		return s.finishNotificationDelivery(ctx, intent, "unknown", notificationDeliveryResultCode(result.Code), now, true)
	default:
		if intent.Attempts >= notificationDeliveryMaxAttempts {
			return s.finishNotificationDelivery(ctx, intent, "failed", notificationDeliveryResultCode(result.Code), now, true)
		}
		return s.finishNotificationDelivery(ctx, intent, "retry", notificationDeliveryResultCode(result.Code), now, false)
	}
}

func notificationDeliveryBody(intent entity.NotificationDeliveryIntent) string {
	body := fmt.Sprintf("Kind: %s\nDetail: %s\nSeverity: %s", intent.Kind, intent.DetailCode, intent.Severity)
	if validNotificationSubject(intent.SubjectType, intent.SubjectID, intent.SubjectName) && intent.SubjectType != "" {
		body += fmt.Sprintf("\nSubject type: %s\nSubject ID: %s\nSubject name: %s", intent.SubjectType, intent.SubjectID, intent.SubjectName)
	}
	return body
}

func notificationDeliveryResultCode(code string) string {
	allowed := []string{"invalid_configuration", "connect_failed", "greeting_failed", "tls_failed", "auth_failed", "sender_failed", "recipient_failed", "data_failed", "acceptance_failed", "timed_out", "canceled"}
	if slices.Contains(allowed, code) {
		return code
	}
	return "delivery_failed"
}

func (s *Service) finishNotificationDelivery(ctx context.Context, intent entity.NotificationDeliveryIntent, status, resultCode string, now time.Time, terminal bool) error {
	updates := map[string]any{"status": status, "result_code": resultCode, "lease_token": "", "lease_until": nil, "updated_at": now}
	if terminal {
		updates["completed_at"] = now
	} else {
		updates["next_attempt_at"] = now.Add(time.Duration(intent.Attempts) * 30 * time.Second)
	}
	result := s.authDB(ctx).Model(&entity.NotificationDeliveryIntent{}).
		Where("id = ? AND status = ? AND lease_token = ?", intent.ID, "sending", intent.LeaseToken).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("notification delivery lease was replaced")
	}
	return nil
}
