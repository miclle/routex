package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SMTPTestInput struct {
	ETag      string `json:"etag"`
	Recipient string `json:"recipient"`
	RequestID string `json:"request_id"`
}
type SMTPTestView struct {
	RequestID  string `json:"request_id"`
	ConfigETag string `json:"config_etag"`
	smtpclient.Result
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

var smtpTestRateLimited = &apperrors.Error{Code: 429, Message: "SMTP test cooldown is active"}

func smtpTestView(row entity.SMTPTest) SMTPTestView {
	result := SMTPTestView{RequestID: row.RequestID, ConfigETag: row.ConfigETag, StartedAt: row.StartedAt, CompletedAt: row.CompletedAt, Result: smtpclient.Result{Status: row.Status, Stages: []smtpclient.Stage{}}}
	if row.ResultJSON != "" {
		_ = json.Unmarshal([]byte(row.ResultJSON), &result.Result)
	}
	if result.Status == "pending" && time.Since(row.StartedAt) >= 30*time.Second {
		result.Status = "unknown"
		result.Code = "interrupted"
	}
	return result
}
func (s *Service) TestSMTP(ctx context.Context, actor string, input SMTPTestInput) (*SMTPTestView, error) {
	input.Recipient = strings.TrimSpace(input.Recipient)
	if input.ETag == "" || !smtpclient.ValidAddress(input.Recipient) || !smtpclient.ValidRequestID(input.RequestID) {
		return nil, apperrors.ErrBadRequest
	}
	digest := sha256.Sum256([]byte(input.Recipient))
	recipientDigest := hex.EncodeToString(digest[:])
	requestDigest := sha256.Sum256([]byte(input.RequestID))
	requestHash := hex.EncodeToString(requestDigest[:])
	var setting entity.SMTPSetting
	var row entity.SMTPTest
	existing := false
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "smtp.test"); err != nil {
			return err
		}
		err := tx.First(&row, "request_hash = ?", requestHash).Error
		if err == nil {
			if row.RequestID != input.RequestID || row.ActorID != actor || row.ConfigETag != input.ETag || row.RecipientDigest != recipientDigest {
				return catalogConflict
			}
			existing = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error; err != nil {
			return err
		}
		if setting.ETag != input.ETag {
			return catalogConflict
		}
		if !setting.Enabled {
			return catalogConflict
		}
		message := smtpclient.Message{From: setting.SenderEmail, Name: setting.SenderName, ReplyTo: setting.ReplyTo, To: input.Recipient, RequestID: input.RequestID}
		if message.Validate() != nil {
			return apperrors.ErrBadRequest
		}
		now := time.Now().UTC()
		if setting.LastTestStartedAt != nil && now.Sub(*setting.LastTestStartedAt) < 30*time.Second {
			return smtpTestRateLimited
		}
		testID, err := id.NewPrefixed("smt")
		if err != nil {
			return err
		}
		row = entity.SMTPTest{ID: testID, RequestID: input.RequestID, RequestHash: requestHash, ActorID: actor, ConfigETag: setting.ETag, RecipientDigest: recipientDigest, Status: "pending", StartedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		if err := tx.Model(&setting).Update("last_test_started_at", now).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "smtp.test", "smtp_test", row.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if existing {
		view := smtpTestView(row)
		return &view, nil
	}
	result := smtpclient.Result{Status: "failed", Code: "configuration_unavailable", Stages: []smtpclient.Stage{}}
	config, configErr := s.smtpConfig(setting)
	if configErr == nil {
		result = smtpclient.New(s.allowPrivateSMTP).SendTest(ctx, config, smtpclient.Message{From: setting.SenderEmail, Name: setting.SenderName, ReplyTo: setting.ReplyTo, To: input.Recipient, RequestID: input.RequestID})
	}
	encoded, _ := json.Marshal(result)
	completed := time.Now().UTC()
	row.Status, row.ResultJSON, row.CompletedAt = result.Status, string(encoded), &completed
	// A disconnect cannot erase the receipt or allow a retry to send a second mail.
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.authDB(persist).Model(&entity.SMTPTest{}).Where("id = ? AND status = ?", row.ID, "pending").Updates(map[string]any{"status": row.Status, "result_json": row.ResultJSON, "completed_at": completed}).Error; err != nil {
		return nil, runtimeUnavailable
	}
	view := smtpTestView(row)
	return &view, nil
}
