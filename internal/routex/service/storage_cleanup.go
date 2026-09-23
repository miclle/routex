package service

import (
	"context"
	"errors"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/objectstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) markStorageDeletion(ctx context.Context, objectID, version string) error {
	return s.markStorageDeletionAfter(ctx, objectID, version, 0)
}
func (s *Service) markStorageDeletionAfter(ctx context.Context, objectID, version string, delay time.Duration) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	updates := map[string]any{"state": "delete_pending", "next_cleanup_at": time.Now().UTC().Add(delay)}
	if version != "" {
		updates["version_id"] = version
	}
	return s.authDB(persist).Model(&entity.StorageObject{}).Where("id = ? AND state <> ?", objectID, "deleted").Updates(updates).Error
}

// StartStorageCleanup starts a single-process bounded worker. The returned stop
// joins it; callers stop it after draining HTTP and before closing the DB pool.
func (s *Service) StartStorageCleanup(ctx context.Context) (func(), error) {
	var rows []entity.StorageObject
	if err := s.authDB(ctx).Select("id").Limit(1).Find(&rows).Error; err != nil {
		return nil, catalogError(err)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			_ = s.FlushStorageCleanup(run, 8)
			select {
			case <-run.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return func() { cancel(); <-done }, nil
}

// FlushStorageCleanup only acts on durable, generated object identities. It does
// not list buckets or discover/delete objects outside recorded cleanup intent.
func (s *Service) FlushStorageCleanup(ctx context.Context, limit int) error {
	if limit < 1 || limit > 32 {
		return objectstore.ErrConfig
	}
	for range limit {
		var row entity.StorageObject
		claimed := false
		now := time.Now().UTC()
		err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockGovernance(tx); err != nil {
				return err
			}
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("((state = ? AND next_cleanup_at <= ?) OR (state = ? AND created_at <= ?))", "delete_pending", now, "uploading", now.Add(-time.Minute)).Where("lease_until IS NULL OR lease_until <= ?", now).Order("next_cleanup_at, id").First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			token, err := id.NewPrefixed("lck")
			if err != nil {
				return err
			}
			until := now.Add(30 * time.Second)
			row.LeaseToken, row.LeaseUntil = token, &until
			row.State = "delete_pending"
			row.CleanupAttempts++
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			claimed = true
			return nil
		})
		if err != nil {
			return catalogError(err)
		}
		if !claimed {
			return nil
		}
		run, cancel := context.WithTimeout(ctx, 20*time.Second)
		err = s.deleteStorageObject(run, row)
		cancel()
		updates := map[string]any{"lease_token": "", "lease_until": nil, "cleanup_code": "", "state": "deleted"}
		if err != nil {
			updates["state"] = "delete_pending"
			updates["cleanup_code"] = "delete_failed"
			if !row.UploadConfirmed && errors.Is(err, objectstore.ErrNotFound) {
				updates["cleanup_code"] = "upload_uncertain"
			}
			updates["next_cleanup_at"] = time.Now().UTC().Add(time.Duration(min(300, 5*(1<<min(row.CleanupAttempts, 6)))) * time.Second)
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		saveErr := s.authDB(persist).Model(&entity.StorageObject{}).Where("id = ? AND lease_token = ?", row.ID, row.LeaseToken).Updates(updates).Error
		cancel()
		if saveErr != nil {
			return catalogError(saveErr)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}
func (s *Service) deleteStorageObject(ctx context.Context, row entity.StorageObject) error {
	var revision entity.StorageRevision
	if err := s.authDB(ctx).First(&revision, "id = ?", row.RevisionID).Error; err != nil {
		return err
	}
	config, err := s.storageConfig(revision)
	if err != nil {
		return err
	}
	client, err := objectstore.New(config, s.allowPrivateStorage)
	if err != nil {
		return err
	}
	defer client.Close()
	if !row.UploadConfirmed {
		if _, err := client.Head(ctx, row.ID, row.VersionID); err != nil {
			return err
		}
	}
	return client.Delete(ctx, row.ID, row.VersionID)
}

func (s *Service) confirmStorageUpload(ctx context.Context, objectID, version string) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.authDB(persist).Model(&entity.StorageObject{}).Where("id = ?", objectID).Updates(map[string]any{"upload_confirmed": true, "version_id": version}).Error
}

// abandonStorageUpload is only used before any external Put was attempted.
func (s *Service) abandonStorageUpload(ctx context.Context, objectID string) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.authDB(persist).Model(&entity.StorageObject{}).Where("id = ? AND state = ?", objectID, "uploading").Update("state", "deleted").Error
}
