package service

import (
	"context"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"gorm.io/gorm"
)

type systemInstanceStop struct {
	done chan struct{}
	err  error
}

func (s *Service) stopCredentialSourceProcess(ctx context.Context, lease *systemInstanceLease) error {
	// Heartbeat remains renewable during drain, not a substitute for that drain.
	// On any failed exit cancel it; never persist a positive closed proof then.
	defer lease.stopOnce.Do(lease.cancel)
	if err := s.credentialSources.joinProcess(ctx); err != nil {
		return err
	}
	lease.stopOnce.Do(lease.cancel)
	select {
	case <-lease.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	updates := systemInstanceResourceUpdates(s.instanceResources(lease.storagePath))
	updates["heartbeat_revision"] = gorm.Expr("heartbeat_revision + ?", 1)
	updates["retired_at"], updates["retired_by"] = nil, ""
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		s.instanceMu.RLock()
		exact := s.instance == lease
		s.instanceMu.RUnlock()
		if !exact || !s.credentialSources.processJoined() {
			return vaultUnavailable
		}
		var process entity.CredentialSourceProcess
		if err := personalExact(vaultDB(tx), "process_id", lease.id).Take(&process).Error; err != nil {
			return err
		}
		var instance entity.SystemInstance
		if err := personalExact(vaultDB(tx), "id", lease.id).Take(&instance).Error; err != nil {
			return err
		}
		if !sourceProcessValid(process) || process.ProcessID != lease.id || process.Generation != rootHash(lease.id+":"+lease.token) || !process.Birth.Equal(lease.startedAt) || !process.RegisteredAt.Equal(lease.startedAt) || instance.ID != lease.id || instance.LeaseToken != lease.token || !instance.StartedAt.Equal(lease.startedAt) || instance.Role != systemInstanceRole || instance.RetiredAt != nil {
			return vaultUnavailable
		}
		s.instanceMu.RLock()
		exact = s.instance == lease
		s.instanceMu.RUnlock()
		if !exact || !s.credentialSources.processJoined() {
			return vaultUnavailable
		}
		if process.ClosedAt != nil {
			if instance.StoppedAt == nil || !instance.StoppedAt.Equal(*process.ClosedAt) {
				return vaultUnavailable
			}
			return nil // Exact durable reconciliation after an uncertain commit only.
		}
		// Waiting for governance or identity reads must not retain earlier lease
		// authority. Existing durable closure reconciles above without a new time.
		observedAt := s.instanceNow().UTC()
		if instance.StoppedAt != nil || !instance.LeaseExpiresAt.After(observedAt) || observedAt.Before(process.RegisteredAt) {
			return vaultUnavailable
		}
		now := observedAt.Truncate(time.Microsecond)
		updates["last_heartbeat_at"], updates["lease_expires_at"], updates["stopped_at"] = now, now, now
		result := personalExact(vaultDB(tx).Model(&entity.CredentialSourceProcess{}), "process_id", lease.id).Where("generation = ? AND birth = ? AND closed_at IS NULL", process.Generation, process.Birth).UpdateColumn("closed_at", now)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return vaultUnavailable
		}
		result = personalExact(vaultDB(tx).Model(&entity.SystemInstance{}), "id", lease.id).Where("lease_token = ? AND stopped_at IS NULL", lease.token).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return vaultUnavailable
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.instanceMu.Lock()
	if s.instance == lease {
		s.instance = nil
	}
	s.instanceMu.Unlock()
	return nil
}
