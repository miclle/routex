package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/objectstore"
	"gorm.io/gorm"
)

type AttachmentView struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	MIME      string    `json:"mime"`
	Size      int64     `json:"size"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
}

func attachmentView(row entity.StorageObject) AttachmentView {
	return AttachmentView{ID: row.ID, Name: row.Name, MIME: row.MIME, Size: row.Size, State: row.State, CreatedAt: row.CreatedAt}
}
func activeAttachmentOwner(db *gorm.DB, actor string) error {
	var user entity.User
	if err := db.First(&user, "id = ? AND disabled = ?", actor, false).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apperrors.ErrUnauthorized
		}
		return apperrors.ErrInternal
	}
	return nil
}
func (s *Service) UploadAttachment(ctx context.Context, actor, name string, data []byte) (*AttachmentView, error) {
	name = strings.TrimSpace(name)
	mime, err := objectstore.ValidateAttachment(name, data)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	objectID, err := id.NewPrefixed("obj")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	sum := sha256.Sum256(data)
	row := entity.StorageObject{ID: objectID, OwnerID: actor, Purpose: "attachment", State: "uploading", Name: name, MIME: mime, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), NextCleanupAt: time.Now().UTC()}
	var revision entity.StorageRevision
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := lockActiveKeyOwner(tx, actor); err != nil {
			return err
		}
		var setting entity.StorageSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		if !setting.Enabled || setting.ActiveRevisionID == nil {
			return runtimeUnavailable
		}
		if err := tx.First(&revision, "id = ? AND verified_at IS NOT NULL", *setting.ActiveRevisionID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&entity.StorageObject{}).Where("owner_id = ? AND purpose = ? AND state <> ?", actor, "attachment", "deleted").Count(&count).Error; err != nil {
			return err
		}
		if count >= 128 {
			return &apperrors.Error{Code: 429, Message: "attachment storage limit reached"}
		}
		row.RevisionID = revision.ID
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "attachment.upload", "attachment", row.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	config, err := s.storageConfig(revision)
	if err != nil {
		_ = s.abandonStorageUpload(ctx, row.ID)
		return nil, err
	}
	client, err := objectstore.New(config, s.allowPrivateStorage)
	if err != nil {
		_ = s.abandonStorageUpload(ctx, row.ID)
		return nil, runtimeUnavailable
	}
	defer client.Close()
	run, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	version, err := client.Put(run, row.ID, mime, data)
	if err != nil {
		_ = s.markStorageDeletion(ctx, row.ID, version)
		return nil, runtimeUnavailable
	}
	if err := s.confirmStorageUpload(ctx, row.ID, version); err != nil {
		return nil, runtimeUnavailable
	}
	// Verify stored bytes and ownership before publishing a readable attachment.
	object, err := client.Get(run, row.ID, version)
	if err == nil {
		actual := sha256.Sum256(object.Data)
		if object.OwnerID != row.ID || actual != sum {
			err = objectstore.ErrConflict
		}
	}
	if err != nil {
		_ = s.markStorageDeletion(ctx, row.ID, version)
		return nil, runtimeUnavailable
	}
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := lockActiveKeyOwner(tx, actor); err != nil {
			return err
		}
		result := tx.Model(&entity.StorageObject{}).Where("id = ? AND state = ?", row.ID, "uploading").Updates(map[string]any{"state": "ready", "version_id": version})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return catalogConflict
		}
		return nil
	})
	if err != nil {
		_ = s.markStorageDeletion(ctx, row.ID, version)
		return nil, catalogError(err)
	}
	row.State, row.VersionID = "ready", version
	view := attachmentView(row)
	return &view, nil
}
func (s *Service) ownedAttachment(ctx context.Context, actor, objectID string) (entity.StorageObject, error) {
	db := s.authDB(ctx)
	if err := activeAttachmentOwner(db, actor); err != nil {
		return entity.StorageObject{}, err
	}
	var row entity.StorageObject
	if err := db.First(&row, "id = ? AND owner_id = ? AND purpose = ?", objectID, actor, "attachment").Error; err != nil {
		return row, catalogError(err)
	}
	return row, nil
}
func (s *Service) Attachment(ctx context.Context, actor, objectID string) (*AttachmentView, error) {
	row, err := s.ownedAttachment(ctx, actor, objectID)
	if err != nil {
		return nil, err
	}
	view := attachmentView(row)
	return &view, nil
}
func (s *Service) AttachmentContent(ctx context.Context, actor, objectID string) (*AttachmentView, []byte, error) {
	row, err := s.ownedAttachment(ctx, actor, objectID)
	if err != nil {
		return nil, nil, err
	}
	if row.State != "ready" {
		return nil, nil, apperrors.ErrNotFound
	}
	var revision entity.StorageRevision
	if err := s.authDB(ctx).First(&revision, "id = ?", row.RevisionID).Error; err != nil {
		return nil, nil, catalogError(err)
	}
	config, err := s.storageConfig(revision)
	if err != nil {
		return nil, nil, err
	}
	client, err := objectstore.New(config, s.allowPrivateStorage)
	if err != nil {
		return nil, nil, runtimeUnavailable
	}
	defer client.Close()
	run, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	object, err := client.Get(run, row.ID, row.VersionID)
	if err != nil {
		return nil, nil, runtimeUnavailable
	}
	sum := sha256.Sum256(object.Data)
	if object.OwnerID != row.ID || int64(len(object.Data)) != row.Size || hex.EncodeToString(sum[:]) != row.SHA256 {
		return nil, nil, runtimeUnavailable
	}
	current, err := s.ownedAttachment(ctx, actor, objectID)
	if err != nil {
		return nil, nil, err
	}
	if current.State != "ready" {
		return nil, nil, apperrors.ErrNotFound
	}
	view := attachmentView(current)
	return &view, object.Data, nil
}
func (s *Service) DeleteAttachment(ctx context.Context, actor, objectID string) (*AttachmentView, error) {
	var row entity.StorageObject
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := lockActiveKeyOwner(tx, actor); err != nil {
			return err
		}
		if err := tx.First(&row, "id = ? AND owner_id = ? AND purpose = ?", objectID, actor, "attachment").Error; err != nil {
			return err
		}
		if row.State == "deleted" || row.State == "delete_pending" {
			return nil
		}
		wasUploading := row.State == "uploading"
		row.State = "delete_pending"
		row.NextCleanupAt = time.Now().UTC()
		if wasUploading {
			row.NextCleanupAt = row.CreatedAt.Add(time.Minute)
		}
		if err := tx.Save(&row).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "attachment.delete", "attachment", row.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	view := attachmentView(row)
	return &view, nil
}
