package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/objectstore"
	"gorm.io/gorm"
)

type StorageStage struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}
type StorageProbe struct {
	Success        bool           `json:"success"`
	CleanupPending bool           `json:"cleanup_pending"`
	Stages         []StorageStage `json:"stages"`
}

func (s *Service) probeStorage(ctx context.Context, actor string, revision entity.StorageRevision) (*StorageProbe, error) {
	config, err := s.storageConfig(revision)
	if err != nil {
		return nil, err
	}
	client, err := objectstore.New(config, s.allowPrivateStorage)
	if err != nil {
		return nil, apperrors.ErrBadRequest
	}
	defer client.Close()
	objectID, err := id.NewPrefixed("obj")
	if err != nil {
		return nil, apperrors.ErrInternal
	}
	row := entity.StorageObject{ID: objectID, OwnerID: actor, RevisionID: revision.ID, Purpose: "probe", State: "uploading", NextCleanupAt: time.Now().UTC()}
	if err := s.authDB(ctx).Create(&row).Error; err != nil {
		return nil, catalogError(err)
	}
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		_ = s.abandonStorageUpload(ctx, row.ID)
		return nil, apperrors.ErrInternal
	}
	run, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := &StorageProbe{Stages: []StorageStage{}}
	stage := func(name string, fn func() error) bool {
		start := time.Now()
		err := fn()
		state := "passed"
		if err != nil {
			state = "failed"
		}
		result.Stages = append(result.Stages, StorageStage{Name: name, Status: state, DurationMS: time.Since(start).Milliseconds()})
		return err == nil
	}
	var version string
	passed := stage("put", func() error {
		var err error
		version, err = client.Put(run, row.ID, "application/octet-stream", payload)
		return err
	})
	if passed {
		if err := s.confirmStorageUpload(ctx, row.ID, version); err != nil {
			return nil, runtimeUnavailable
		}
	}
	if err := s.markStorageDeletionAfter(ctx, row.ID, version, time.Minute); err != nil {
		return nil, runtimeUnavailable
	}
	if passed {
		passed = stage("get", func() error {
			object, err := client.Get(run, row.ID, version)
			if err != nil {
				return err
			}
			if object.OwnerID != row.ID || !bytes.Equal(object.Data, payload) {
				return objectstore.ErrConflict
			}
			return nil
		})
	}
	// Cleanup has its own bounded lifetime even when the requesting caller leaves.
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer stop()
	deleted := stage("delete", func() error {
		if !passed {
			if _, err := client.Head(cleanup, row.ID, version); err != nil {
				return err
			}
		}
		return client.Delete(cleanup, row.ID, version)
	})
	if deleted {
		if err := s.authDB(cleanup).Model(&row).Updates(map[string]any{"state": "deleted", "cleanup_code": ""}).Error; err != nil {
			return nil, runtimeUnavailable
		}
	} else {
		result.CleanupPending = true
	}
	result.Success = passed && deleted
	return result, nil
}
func (s *Service) TestStorage(ctx context.Context, actor, etag string) (*StorageProbe, error) {
	var revision entity.StorageRevision
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "storage.test"); err != nil {
			return err
		}
		var setting entity.StorageSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		if etag == "" || etag != setting.ETag {
			return catalogConflict
		}
		if setting.ActiveRevisionID == nil {
			return apperrors.ErrBadRequest
		}
		if err := tx.First(&revision, "id = ?", *setting.ActiveRevisionID).Error; err != nil {
			return err
		}
		return appendAudit(tx, actor, "storage.test", "storage_revision", revision.ID)
	})
	if err != nil {
		return nil, catalogError(err)
	}
	return s.probeStorage(ctx, actor, revision)
}

type StorageRollbackInput struct {
	RevisionID string `json:"revision_id"`
	ETag       string `json:"etag"`
	Enabled    bool   `json:"enabled"`
}

func (s *Service) RollbackStorage(ctx context.Context, actor string, input StorageRollbackInput) (*StorageView, error) {
	db := s.authDB(ctx)
	if err := authorizeGovernance(db, actor, "storage.write"); err != nil {
		return nil, err
	}
	var setting entity.StorageSetting
	if err := db.First(&setting, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	if input.ETag == "" || setting.ETag != input.ETag {
		return nil, catalogConflict
	}
	var revision entity.StorageRevision
	if err := db.First(&revision, "id = ? AND verified_at IS NOT NULL", input.RevisionID).Error; err != nil {
		return nil, catalogError(err)
	}
	if input.Enabled {
		result, err := s.probeStorage(ctx, actor, revision)
		if err != nil {
			return nil, err
		}
		if !result.Success {
			return nil, &apperrors.Error{Code: 422, Message: "storage verification failed"}
		}
	}
	return s.activateStorage(ctx, actor, input.ETag, revision, input.Enabled, "storage.rollback")
}
