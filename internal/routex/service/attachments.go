package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/objectstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const attachmentReadyTTL = time.Hour

type AttachmentView struct {
	ID                     string     `json:"id"`
	Name                   string     `json:"name"`
	MIME                   string     `json:"mime"`
	Size                   int64      `json:"size"`
	State                  string     `json:"state"`
	CreatedAt              time.Time  `json:"created_at"`
	AttachmentTeamID       string     `json:"attachment_team_id,omitempty"`
	AttachmentMembershipID string     `json:"attachment_membership_id,omitempty"`
	CreatorUserID          string     `json:"creator_user_id,omitempty"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
}

type attachmentOwner struct {
	Kind                string
	ID                  string
	CreatorUserID       string
	CreatorMembershipID string
}

type attachmentAuthorization func(*gorm.DB) error

func attachmentView(row entity.StorageObject) AttachmentView {
	view := AttachmentView{ID: row.ID, Name: row.Name, MIME: row.MIME, Size: row.Size, State: row.State, CreatedAt: row.CreatedAt}
	if row.OwnerKind == entity.StorageOwnerTeam && row.CreatorUserID != nil && row.CreatorMembershipID != nil {
		view.AttachmentTeamID, view.AttachmentMembershipID, view.CreatorUserID, view.ExpiresAt = row.OwnerID, *row.CreatorMembershipID, *row.CreatorUserID, row.ExpiresAt
	}
	return view
}

func activeAttachmentOwner(db *gorm.DB, actor string) error {
	_, err := registrationAdmittedUser(db, actor, false)
	return err
}

func projectAttachmentManager(db *gorm.DB, actor, projectID string, requireActive, lock bool) error {
	if _, err := registrationAdmittedUser(db, actor, lock); err != nil {
		return err
	}
	var managers int64
	if err := db.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND m.user_id = ? AND u.disabled = ?", projectID, actor, false).Count(&managers).Error; err != nil {
		return err
	}
	if managers != 1 {
		return apperrors.ErrNotFound
	}
	var project entity.Project
	query := db
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := query.First(&project, "id = ?", projectID).Error; err != nil {
		return err
	}
	if requireActive && project.Status != entity.ResourceActive {
		return apperrors.ErrNotFound
	}
	return nil
}

func activeProjectAttachmentOwner(db *gorm.DB, projectID string) error {
	var project entity.Project
	if err := db.First(&project, "id = ? AND status = ?", projectID, entity.ResourceActive).Error; err != nil {
		return err
	}
	var managers int64
	if err := db.Table("project_managers m").Joins("JOIN users u ON u.id = m.user_id").Where("m.project_id = ? AND u.disabled = ?", projectID, false).Count(&managers).Error; err != nil {
		return err
	}
	if managers == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (s *Service) UploadAttachment(ctx context.Context, actor, name string, data []byte) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerUser, ID: actor}
	return s.uploadAttachment(ctx, actor, owner, name, data, func(tx *gorm.DB) error {
		return lockActiveKeyOwner(tx, actor)
	})
}

func (s *Service) UploadProjectAttachment(ctx context.Context, actor, projectID, name string, data []byte) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerProject, ID: projectID}
	return s.uploadAttachment(ctx, actor, owner, name, data, func(tx *gorm.DB) error {
		return projectAttachmentManager(tx, actor, projectID, true, true)
	})
}

func (s *Service) uploadAttachment(ctx context.Context, actor string, owner attachmentOwner, name string, data []byte, authorize attachmentAuthorization) (*AttachmentView, error) {
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
	createdAt := time.Now().UTC()
	if owner.Kind == entity.StorageOwnerTeam {
		// Released MySQL storage timestamps have millisecond precision. Match
		// that common precision before fixing an immutable one-hour deadline.
		createdAt = createdAt.Truncate(time.Millisecond)
	}
	row := entity.StorageObject{ID: objectID, OwnerKind: owner.Kind, OwnerID: owner.ID, Purpose: "attachment", State: "uploading", Name: name, MIME: mime, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]), NextCleanupAt: createdAt.Add(attachmentReadyTTL), CreatedAt: createdAt}
	if owner.Kind == entity.StorageOwnerTeam {
		deadline := createdAt.Add(attachmentReadyTTL)
		row.CreatorUserID, row.CreatorMembershipID, row.ExpiresAt = &owner.CreatorUserID, &owner.CreatorMembershipID, &deadline
	}
	var revision entity.StorageRevision
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorize(tx); err != nil {
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
		if err := tx.Model(&entity.StorageObject{}).Where("owner_kind = ? AND owner_id = ? AND purpose = ? AND state <> ?", owner.Kind, owner.ID, "attachment", "deleted").Count(&count).Error; err != nil {
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
		return nil, attachmentError(owner, err)
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
	if owner.Kind == entity.StorageOwnerTeam {
		if err := authorize(s.authDB(run)); err != nil {
			_ = s.abandonStorageUpload(ctx, row.ID)
			return nil, attachmentError(owner, err)
		}
	}
	version, err := client.Put(run, row.ID, mime, data)
	if err != nil {
		_ = s.markStorageDeletion(ctx, row.ID, version)
		return nil, runtimeUnavailable
	}
	if err := s.confirmStorageUpload(ctx, row.ID, version); err != nil {
		return nil, runtimeUnavailable
	}
	if owner.Kind == entity.StorageOwnerTeam {
		if err := authorize(s.authDB(run)); err != nil {
			_ = s.markStorageDeletion(ctx, row.ID, version)
			return nil, attachmentError(owner, err)
		}
	}
	object, err := client.Get(run, row.ID, version)
	if err == nil {
		if owner.Kind == entity.StorageOwnerTeam {
			row.VersionID = version
			if !storedAttachmentMatches(row, object) {
				err = objectstore.ErrConflict
			}
		}
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
		if err := authorize(tx); err != nil {
			return err
		}
		if owner.Kind == entity.StorageOwnerTeam && (row.ExpiresAt == nil || !row.ExpiresAt.After(time.Now().UTC())) {
			return apperrors.ErrNotFound
		}
		result := attachmentScopeQuery(tx.Model(&entity.StorageObject{}), owner, row.ID).Where("state = ?", "uploading").Updates(map[string]any{"state": "ready", "version_id": version})
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
		return nil, attachmentError(owner, err)
	}
	row.State, row.VersionID = "ready", version
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) scopedAttachment(ctx context.Context, owner attachmentOwner, objectID string, authorize attachmentAuthorization) (entity.StorageObject, error) {
	db := s.authDB(ctx)
	if err := authorize(db); err != nil {
		return entity.StorageObject{}, attachmentError(owner, err)
	}
	var row entity.StorageObject
	if err := attachmentScopeQuery(db, owner, objectID).First(&row).Error; err != nil {
		return row, attachmentError(owner, err)
	}
	if owner.Kind == entity.StorageOwnerTeam && !teamAttachmentMatches(row, owner) {
		return entity.StorageObject{}, apperrors.ErrNotFound
	}
	return row, nil
}

func (s *Service) Attachment(ctx context.Context, actor, objectID string) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerUser, ID: actor}
	row, err := s.scopedAttachment(ctx, owner, objectID, func(db *gorm.DB) error { return activeAttachmentOwner(db, actor) })
	if err != nil {
		return nil, err
	}
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) ProjectAttachment(ctx context.Context, actor, projectID, objectID string) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerProject, ID: projectID}
	row, err := s.scopedAttachment(ctx, owner, objectID, func(db *gorm.DB) error {
		return projectAttachmentManager(db, actor, projectID, false, false)
	})
	if err != nil {
		return nil, err
	}
	view := attachmentView(row)
	return &view, nil
}

func (s *Service) AttachmentContent(ctx context.Context, actor, objectID string) (*AttachmentView, []byte, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerUser, ID: actor}
	row, data, err := s.readScopedAttachment(ctx, owner, objectID, func(db *gorm.DB) error { return activeAttachmentOwner(db, actor) })
	if err != nil {
		return nil, nil, err
	}
	view := attachmentView(row)
	return &view, data, nil
}

func (s *Service) ProjectAttachmentContent(ctx context.Context, actor, projectID, objectID string) (*AttachmentView, []byte, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerProject, ID: projectID}
	row, data, err := s.readScopedAttachment(ctx, owner, objectID, func(db *gorm.DB) error {
		return projectAttachmentManager(db, actor, projectID, false, false)
	})
	if err != nil {
		return nil, nil, err
	}
	view := attachmentView(row)
	return &view, data, nil
}

// readScopedAttachment resolves immutable storage metadata, verifies the exact
// stored object, and repeats current scope authorization after remote I/O.
func (s *Service) readScopedAttachment(ctx context.Context, owner attachmentOwner, objectID string, authorize attachmentAuthorization) (entity.StorageObject, []byte, error) {
	row, err := s.scopedAttachment(ctx, owner, objectID, authorize)
	if err != nil {
		return entity.StorageObject{}, nil, err
	}
	if !attachmentReadableAt(row, time.Now().UTC()) {
		return entity.StorageObject{}, nil, apperrors.ErrNotFound
	}
	var revision entity.StorageRevision
	if err := s.authDB(ctx).First(&revision, "id = ?", row.RevisionID).Error; err != nil {
		return entity.StorageObject{}, nil, attachmentError(owner, err)
	}
	config, err := s.storageConfig(revision)
	if err != nil {
		return entity.StorageObject{}, nil, err
	}
	client, err := objectstore.New(config, s.allowPrivateStorage)
	if err != nil {
		return entity.StorageObject{}, nil, runtimeUnavailable
	}
	defer client.Close()
	run, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if owner.Kind == entity.StorageOwnerTeam {
		if err := authorize(s.authDB(run)); err != nil {
			return entity.StorageObject{}, nil, attachmentError(owner, err)
		}
	}
	object, err := client.Get(run, row.ID, row.VersionID)
	if owner.Kind == entity.StorageOwnerTeam {
		if authErr := authorize(s.authDB(run)); authErr != nil {
			return entity.StorageObject{}, nil, attachmentError(owner, authErr)
		}
	}
	if err != nil {
		return entity.StorageObject{}, nil, runtimeUnavailable
	}
	if !storedAttachmentMatches(row, object) {
		return entity.StorageObject{}, nil, runtimeUnavailable
	}
	current, err := s.scopedAttachment(ctx, owner, objectID, authorize)
	if err != nil {
		return entity.StorageObject{}, nil, err
	}
	if !attachmentReadableAt(current, time.Now().UTC()) {
		return entity.StorageObject{}, nil, apperrors.ErrNotFound
	}
	return current, object.Data, nil
}

func attachmentReadableAt(row entity.StorageObject, now time.Time) bool {
	if row.State != "ready" {
		return false
	}
	if row.OwnerKind == entity.StorageOwnerTeam {
		return row.ExpiresAt != nil && row.ExpiresAt.Equal(row.CreatedAt.Add(attachmentReadyTTL)) && row.ExpiresAt.After(now)
	}
	return row.Purpose != "attachment" || row.NextCleanupAt.After(now) || row.CreatedAt.After(now.Add(-attachmentReadyTTL))
}

func storedAttachmentMatches(row entity.StorageObject, object objectstore.Object) bool {
	sum := sha256.Sum256(object.Data)
	return object.OwnerID == row.ID &&
		object.VersionID == row.VersionID &&
		object.Size == row.Size &&
		int64(len(object.Data)) == row.Size &&
		hex.EncodeToString(sum[:]) == row.SHA256
}

func (s *Service) DeleteAttachment(ctx context.Context, actor, objectID string) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerUser, ID: actor}
	return s.deleteAttachment(ctx, actor, owner, objectID, func(tx *gorm.DB) error { return lockActiveKeyOwner(tx, actor) })
}

func (s *Service) DeleteProjectAttachment(ctx context.Context, actor, projectID, objectID string) (*AttachmentView, error) {
	owner := attachmentOwner{Kind: entity.StorageOwnerProject, ID: projectID}
	return s.deleteAttachment(ctx, actor, owner, objectID, func(tx *gorm.DB) error {
		return projectAttachmentManager(tx, actor, projectID, false, true)
	})
}

func (s *Service) deleteAttachment(ctx context.Context, actor string, owner attachmentOwner, objectID string, authorize attachmentAuthorization) (*AttachmentView, error) {
	var row entity.StorageObject
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorize(tx); err != nil {
			return err
		}
		if err := attachmentScopeQuery(tx, owner, objectID).First(&row).Error; err != nil {
			return err
		}
		if owner.Kind == entity.StorageOwnerTeam && !teamAttachmentMatches(row, owner) {
			return apperrors.ErrNotFound
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
		return nil, attachmentError(owner, err)
	}
	view := attachmentView(row)
	return &view, nil
}
