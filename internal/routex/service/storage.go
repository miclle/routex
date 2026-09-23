package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/objectstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func WithStoragePolicy(allowPrivate bool) Option {
	return func(s *Service) { s.allowPrivateStorage = allowPrivate }
}

type StorageAuthInput struct {
	Action    string `json:"action"`
	AccessKey string `json:"access_key,omitempty"`
	SecretKey string `json:"secret_key,omitempty"`
}
type StorageInput struct {
	Enabled  bool             `json:"enabled"`
	Endpoint string           `json:"endpoint"`
	Region   string           `json:"region"`
	Bucket   string           `json:"bucket"`
	Prefix   string           `json:"prefix"`
	Auth     StorageAuthInput `json:"auth"`
	ETag     string           `json:"etag"`
}
type StorageRevisionView struct {
	ID                    string     `json:"id"`
	Endpoint              string     `json:"endpoint"`
	Region                string     `json:"region"`
	Bucket                string     `json:"bucket"`
	Prefix                string     `json:"prefix"`
	CredentialsConfigured bool       `json:"credentials_configured"`
	VerifiedAt            *time.Time `json:"verified_at"`
	CreatedAt             time.Time  `json:"created_at"`
}
type StorageView struct {
	Enabled   bool                  `json:"enabled"`
	ETag      string                `json:"etag"`
	Revision  *StorageRevisionView  `json:"revision"`
	Revisions []StorageRevisionView `json:"revisions"`
}

func storageRevisionView(row entity.StorageRevision) StorageRevisionView {
	return StorageRevisionView{ID: row.ID, Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket, Prefix: row.Prefix, CredentialsConfigured: row.AuthCiphertext != "", VerifiedAt: row.VerifiedAt, CreatedAt: row.CreatedAt}
}
func (s *Service) StorageSettings(ctx context.Context, actor string) (*StorageView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "storage.read"); err != nil {
		return nil, err
	}
	var setting entity.StorageSetting
	if err := db.First(&setting, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	view := &StorageView{Enabled: setting.Enabled, ETag: setting.ETag, Revisions: []StorageRevisionView{}}
	if setting.ActiveRevisionID != nil {
		var row entity.StorageRevision
		if err := db.First(&row, "id = ?", *setting.ActiveRevisionID).Error; err != nil {
			return nil, catalogError(err)
		}
		value := storageRevisionView(row)
		view.Revision = &value
	}
	var rows []entity.StorageRevision
	if err := db.Where("verified_at IS NOT NULL").Order("created_at DESC, id DESC").Limit(20).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	for _, row := range rows {
		view.Revisions = append(view.Revisions, storageRevisionView(row))
	}
	return view, nil
}
func (s *Service) storageConfig(row entity.StorageRevision) (objectstore.Config, error) {
	config := objectstore.Config{Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket, Prefix: row.Prefix}
	if s.secrets == nil || row.AuthCiphertext == "" {
		return config, secretStoreUnavailable
	}
	plain, err := s.secrets.Open("storage:"+row.ID+":"+row.SecretGeneration, row.AuthCiphertext)
	if err != nil {
		return config, secretStoreUnavailable
	}
	var auth struct{ AccessKey, SecretKey string }
	if json.Unmarshal([]byte(plain), &auth) != nil {
		return config, secretStoreUnavailable
	}
	config.AccessKey, config.SecretKey = auth.AccessKey, auth.SecretKey
	if objectstore.Validate(config, s.allowPrivateStorage) != nil {
		return config, apperrors.ErrBadRequest
	}
	return config, nil
}
func (s *Service) prepareStorage(original entity.StorageRevision, actor string, input StorageInput) (entity.StorageRevision, error) {
	row := entity.StorageRevision{Endpoint: strings.TrimRight(strings.TrimSpace(input.Endpoint), "/"), Region: strings.TrimSpace(input.Region), Bucket: strings.TrimSpace(input.Bucket), Prefix: strings.TrimSpace(input.Prefix), CreatedBy: actor}
	var err error
	row.ID, err = id.NewPrefixed("str")
	if err != nil {
		return row, apperrors.ErrInternal
	}
	var access, secret string
	switch input.Auth.Action {
	case "", "keep":
		if input.Auth.AccessKey != "" || input.Auth.SecretKey != "" {
			return row, apperrors.ErrBadRequest
		}
		if original.AuthCiphertext != "" {
			if row.Endpoint != original.Endpoint {
				return row, apperrors.ErrBadRequest
			}
			config, err := s.storageConfig(original)
			if err != nil {
				return row, err
			}
			access, secret = config.AccessKey, config.SecretKey
		}
	case "replace":
		access, secret = input.Auth.AccessKey, input.Auth.SecretKey
		if access == "" || secret == "" {
			return row, apperrors.ErrBadRequest
		}
	case "remove":
		if input.Enabled || input.Auth.AccessKey != "" || input.Auth.SecretKey != "" {
			return row, apperrors.ErrBadRequest
		}
	default:
		return row, apperrors.ErrBadRequest
	}
	// Empty authentication is a disabled draft only; validate its endpoint using
	// inert placeholders without ever sending them to a storage service.
	config := objectstore.Config{Endpoint: row.Endpoint, Region: row.Region, Bucket: row.Bucket, Prefix: row.Prefix, AccessKey: access, SecretKey: secret}
	if access == "" {
		if input.Enabled {
			return row, apperrors.ErrBadRequest
		}
		config.AccessKey, config.SecretKey = "validation", "validation"
	}
	if objectstore.Validate(config, s.allowPrivateStorage) != nil {
		return row, apperrors.ErrBadRequest
	}
	if access != "" {
		if s.secrets == nil {
			return row, secretStoreUnavailable
		}
		row.SecretGeneration, err = id.NewPrefixed("sec")
		if err != nil {
			return row, apperrors.ErrInternal
		}
		encoded, _ := json.Marshal(struct{ AccessKey, SecretKey string }{access, secret})
		row.AuthCiphertext, err = s.secrets.Seal("storage:"+row.ID+":"+row.SecretGeneration, string(encoded))
		if err != nil {
			return row, secretStoreUnavailable
		}
	}
	return row, nil
}
func (s *Service) WriteStorageSettings(ctx context.Context, actor string, input StorageInput) (*StorageView, error) {
	if input.ETag == "" {
		return nil, apperrors.ErrBadRequest
	}
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "storage.write"); err != nil {
		return nil, err
	}
	var setting entity.StorageSetting
	if err := db.First(&setting, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	if setting.ETag != input.ETag {
		return nil, catalogConflict
	}
	var original entity.StorageRevision
	if setting.ActiveRevisionID != nil {
		if err := db.First(&original, "id = ?", *setting.ActiveRevisionID).Error; err != nil {
			return nil, catalogError(err)
		}
	}
	// Disabling the existing configuration is always possible without decrypting.
	unchanged := original.ID != "" && strings.TrimRight(strings.TrimSpace(input.Endpoint), "/") == original.Endpoint && strings.TrimSpace(input.Region) == original.Region && strings.TrimSpace(input.Bucket) == original.Bucket && strings.TrimSpace(input.Prefix) == original.Prefix && (input.Auth.Action == "" || input.Auth.Action == "keep") && input.Auth.AccessKey == "" && input.Auth.SecretKey == ""
	if !input.Enabled && unchanged {
		return s.activateStorage(ctx, actor, input.ETag, original, false, "storage.disable")
	}
	next, err := s.prepareStorage(original, actor, input)
	if err != nil {
		return nil, err
	}
	// The candidate's encrypted descriptor is durable before external writes,
	// enabling cleanup without publishing it as the active configuration.
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "storage.write"); err != nil {
			return err
		}
		var current entity.StorageSetting
		if err := tx.First(&current, 1).Error; err != nil {
			return err
		}
		if current.ETag != input.ETag {
			return catalogConflict
		}
		if err := tx.Create(&next).Error; err != nil {
			return err
		}
		if input.Enabled {
			return appendAudit(tx, actor, "storage.verification", "storage_revision", next.ID)
		}
		return nil
	}); err != nil {
		return nil, catalogError(err)
	}
	if input.Enabled {
		result, err := s.probeStorage(ctx, actor, next)
		if err != nil {
			return nil, err
		}
		if !result.Success {
			return nil, &apperrors.Error{Code: 422, Message: "storage verification failed"}
		}
		now := time.Now().UTC()
		next.VerifiedAt = &now
		if err := db.Model(&next).Update("verified_at", now).Error; err != nil {
			return nil, catalogError(err)
		}
	}
	return s.activateStorage(ctx, actor, input.ETag, next, input.Enabled, "storage.update")
}
func (s *Service) activateStorage(ctx context.Context, actor, etag string, next entity.StorageRevision, enabled bool, action string) (*StorageView, error) {
	var setting entity.StorageSetting
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "storage.write"); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error; err != nil {
			return err
		}
		if setting.ETag != etag {
			return catalogConflict
		}
		setting.ActiveRevisionID = &next.ID
		setting.Enabled = enabled
		var err error
		setting.ETag, err = id.NewPrefixed("rev")
		if err != nil {
			return err
		}
		if err := tx.Save(&setting).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, action, "storage_revision", next.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	value := storageRevisionView(next)
	return &StorageView{Enabled: enabled, ETag: setting.ETag, Revision: &value, Revisions: []StorageRevisionView{}}, nil
}
