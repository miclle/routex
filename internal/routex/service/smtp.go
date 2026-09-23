package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/smtpclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func WithSMTPPolicy(allowPrivate bool) Option {
	return func(s *Service) { s.allowPrivateSMTP = allowPrivate }
}

type SMTPAuthInput struct {
	Action   string `json:"action"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}
type SMTPInput struct {
	Enabled  bool          `json:"enabled"`
	Host     string        `json:"host"`
	Port     int           `json:"port"`
	Security string        `json:"security"`
	Auth     SMTPAuthInput `json:"auth"`
	ETag     string        `json:"etag"`
}
type SMTPSenderInput struct {
	SenderName  string `json:"sender_name"`
	SenderEmail string `json:"sender_email"`
	ReplyTo     string `json:"reply_to"`
	ETag        string `json:"etag"`
}
type SMTPView struct {
	Enabled        bool          `json:"enabled"`
	Host           string        `json:"host"`
	Port           int           `json:"port"`
	Security       string        `json:"security"`
	AuthConfigured bool          `json:"auth_configured"`
	SenderName     string        `json:"sender_name"`
	SenderEmail    string        `json:"sender_email"`
	ReplyTo        string        `json:"reply_to"`
	ETag           string        `json:"etag"`
	UpdatedAt      time.Time     `json:"updated_at"`
	LastTest       *SMTPTestView `json:"last_test"`
}

var smtpValidationFailed = &apperrors.Error{Code: 422, Message: "SMTP connection verification failed"}

func smtpView(row entity.SMTPSetting) SMTPView {
	return SMTPView{Enabled: row.Enabled, Host: row.Host, Port: row.Port, Security: row.Security, AuthConfigured: row.AuthCiphertext != "", SenderName: row.SenderName, SenderEmail: row.SenderEmail, ReplyTo: row.ReplyTo, ETag: row.ETag, UpdatedAt: row.UpdatedAt}
}
func (s *Service) SMTPSettings(ctx context.Context, actor string) (*SMTPView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "smtp.read"); err != nil {
		return nil, err
	}
	var row entity.SMTPSetting
	if err := db.First(&row, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	result := smtpView(row)
	var last entity.SMTPTest
	if err := db.Order("started_at DESC, request_id DESC").Limit(1).Find(&last).Error; err != nil {
		return nil, catalogError(err)
	}
	if last.RequestID != "" {
		value := smtpTestView(last)
		result.LastTest = &value
	}
	return &result, nil
}
func (s *Service) smtpConfig(row entity.SMTPSetting) (smtpclient.Config, error) {
	config := smtpclient.Config{Host: row.Host, Port: row.Port, Security: row.Security}
	if row.AuthCiphertext != "" {
		if s.secrets == nil {
			return config, secretStoreUnavailable
		}
		plaintext, err := s.secrets.Open("smtp:1:"+row.SecretGeneration, row.AuthCiphertext)
		if err != nil {
			return config, secretStoreUnavailable
		}
		var value struct{ Username, Password string }
		if json.Unmarshal([]byte(plaintext), &value) != nil {
			return config, secretStoreUnavailable
		}
		config.Username, config.Password = value.Username, value.Password
	}
	if smtpclient.Validate(config, s.allowPrivateSMTP) != nil {
		return config, apperrors.ErrBadRequest
	}
	return config, nil
}
func (s *Service) prepareSMTP(row entity.SMTPSetting, input SMTPInput) (entity.SMTPSetting, error) {
	input.Host = strings.ToLower(strings.TrimSpace(input.Host))
	if input.ETag == "" || input.Host == "" {
		return row, apperrors.ErrBadRequest
	}
	changedEndpoint := row.Host != input.Host || row.Port != input.Port || row.Security != input.Security
	row.Enabled, row.Host, row.Port, row.Security = input.Enabled, input.Host, input.Port, input.Security
	switch input.Auth.Action {
	case "", "keep":
		if input.Auth.Username != "" || input.Auth.Password != "" || (changedEndpoint && row.AuthCiphertext != "") {
			return row, apperrors.ErrBadRequest
		}
	case "remove":
		if input.Auth.Username != "" || input.Auth.Password != "" {
			return row, apperrors.ErrBadRequest
		}
		row.AuthCiphertext, row.SecretGeneration = "", ""
	case "replace":
		if s.secrets == nil {
			return row, secretStoreUnavailable
		}
		config := smtpclient.Config{Host: row.Host, Port: row.Port, Security: row.Security, Username: input.Auth.Username, Password: input.Auth.Password}
		if input.Auth.Username == "" || smtpclient.Validate(config, s.allowPrivateSMTP) != nil {
			return row, apperrors.ErrBadRequest
		}
		generation, err := id.NewPrefixed("sec")
		if err != nil {
			return row, apperrors.ErrInternal
		}
		value, _ := json.Marshal(struct{ Username, Password string }{input.Auth.Username, input.Auth.Password})
		ciphertext, err := s.secrets.Seal("smtp:1:"+generation, string(value))
		if err != nil {
			return row, secretStoreUnavailable
		}
		row.SecretGeneration, row.AuthCiphertext = generation, ciphertext
	default:
		return row, apperrors.ErrBadRequest
	}
	if smtpclient.Validate(smtpclient.Config{Host: row.Host, Port: row.Port, Security: row.Security}, s.allowPrivateSMTP) != nil {
		return row, apperrors.ErrBadRequest
	}
	if row.Security == "NONE" && row.AuthCiphertext != "" {
		return row, apperrors.ErrBadRequest
	}
	return row, nil
}
func (s *Service) WriteSMTPSettings(ctx context.Context, actor string, input SMTPInput) (*SMTPView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "smtp.write"); err != nil {
		return nil, err
	}
	var original entity.SMTPSetting
	if err := db.First(&original, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	if original.ETag != input.ETag {
		return nil, catalogConflict
	}
	next, err := s.prepareSMTP(original, input)
	if err != nil {
		return nil, err
	}
	changed := !original.Enabled || original.Host != next.Host || original.Port != next.Port || original.Security != next.Security || original.AuthCiphertext != next.AuthCiphertext
	if next.Enabled && changed {
		config, err := s.smtpConfig(next)
		if err != nil {
			return nil, err
		}
		if result := smtpclient.New(s.allowPrivateSMTP).Check(ctx, config); result.Status != "verified" {
			return nil, smtpValidationFailed
		}
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "smtp.write"); err != nil {
			return err
		}
		var current entity.SMTPSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, 1).Error; err != nil {
			return err
		}
		if current.ETag != input.ETag {
			return catalogConflict
		}
		next.LastTestStartedAt = current.LastTestStartedAt
		var err error
		next.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&next).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "smtp.update", "smtp_setting", "1")
	})
	if err != nil {
		return nil, catalogError(err)
	}
	view := smtpView(next)
	return &view, nil
}
func (s *Service) WriteSMTPSender(ctx context.Context, actor string, input SMTPSenderInput) (*SMTPView, error) {
	input.SenderName = strings.TrimSpace(input.SenderName)
	input.SenderEmail = strings.TrimSpace(input.SenderEmail)
	input.ReplyTo = strings.TrimSpace(input.ReplyTo)
	if input.ETag == "" || !smtpclient.ValidSenderName(input.SenderName) || !smtpclient.ValidAddress(input.SenderEmail) || (input.ReplyTo != "" && !smtpclient.ValidAddress(input.ReplyTo)) {
		return nil, apperrors.ErrBadRequest
	}
	var row entity.SMTPSetting
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "smtp.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, 1).Error; err != nil {
			return err
		}
		if row.ETag != input.ETag {
			return catalogConflict
		}
		row.SenderName, row.SenderEmail, row.ReplyTo = input.SenderName, input.SenderEmail, input.ReplyTo
		var err error
		row.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "smtp.sender", "smtp_setting", "1")
	})
	if err != nil {
		return nil, catalogError(err)
	}
	view := smtpView(row)
	return &view, nil
}
