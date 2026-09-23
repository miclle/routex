package service

import (
	"context"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
)

type QuotaSettingsRecord struct {
	TimeZone      string     `json:"time_zone"`
	ETag          string     `json:"etag"`
	Activated     bool       `json:"activated"`
	CoverageStart *time.Time `json:"coverage_start"`
	Editable      bool       `json:"editable"`
}
type QuotaSettingsInput struct {
	TimeZone string `json:"time_zone"`
	Reason   string `json:"reason"`
}

func (s *Service) GetQuotaSettings(ctx context.Context, actor string) (*QuotaSettingsRecord, error) {
	permissions, err := permissionsFor(s.authDB(ctx), actor)
	if err != nil {
		return nil, catalogError(err)
	}
	if !slices.Contains(permissions, "system.read") && !slices.Contains(permissions, "limits.settings.write") {
		return nil, apperrors.ErrForbidden
	}
	var setting entity.QuotaSetting
	if err := s.authDB(ctx).First(&setting, 1).Error; err != nil {
		return nil, catalogError(err)
	}
	result := &QuotaSettingsRecord{TimeZone: setting.TimeZone, ETag: setting.ETag}
	if s.recorder != nil {
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return nil, runtimeUnavailable
		}
		result.Activated = status.Active
		result.CoverageStart = status.CoverageStart
		result.Editable = !status.Active && !setting.AccountingStarted
	}
	return result, nil
}
func (s *Service) WriteQuotaSettings(ctx context.Context, actor, etag string, input QuotaSettingsInput) (*QuotaSettingsRecord, error) {
	zone, reason := strings.TrimSpace(input.TimeZone), strings.TrimSpace(input.Reason)
	if _, err := time.LoadLocation(zone); err != nil || zone == "" || len(zone) > 100 || len(reason) == 0 || len(reason) > 2000 || etag == "" {
		return nil, apperrors.ErrBadRequest
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	if s.recorder == nil {
		return nil, runtimeUnavailable
	}
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		if err := authorizeGovernance(tx, actor, "limits.settings.write"); err != nil {
			return err
		}
		var setting entity.QuotaSetting
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error; err != nil {
			return err
		}
		if setting.ETag != etag {
			if setting.PreviousETag == etag && setting.ActorID == actor && setting.Reason == reason && setting.TimeZone == zone {
				return nil
			}
			return errLimitConflict
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if (status.Active && zone != status.TimeZone) || (setting.AccountingStarted && zone != setting.TimeZone) {
			return errLimitConflict
		}
		before := setting.TimeZone
		revision, err := id.NewPrefixed("qst")
		if err != nil {
			return err
		}
		setting.TimeZone = zone
		setting.PreviousETag = etag
		setting.ETag = revision
		setting.ActorID = actor
		setting.Reason = reason
		if err := tx.Save(&setting).Error; err != nil {
			return err
		}
		return appendQuotaAudit(tx, actor, "quota.settings.update", "quota_settings", "1", map[string]any{"time_zone": before}, map[string]any{"time_zone": zone, "etag": revision, "reason": reason})
	})
	if err != nil {
		return nil, catalogError(err)
	}
	if s.runtime != nil {
		s.runtime.deniedLimits.Store("quota_settings", s.runtime.epoch.Add(1))
	}
	if err := s.RefreshRuntime(ctx); err != nil {
		return nil, err
	}
	return s.GetQuotaSettings(ctx, actor)
}
func (s *Service) validateQuotaJournal(ctx context.Context, queue *eventqueue.Queue) error {
	var setting entity.QuotaSetting
	if err := s.authDB(ctx).First(&setting, 1).Error; err != nil {
		return err
	}
	if setting.TimeZone == "" {
		return eventqueue.ErrInvalid
	}
	if _, err := time.LoadLocation(setting.TimeZone); err != nil {
		return err
	}
	status, err := queue.QuotaStatus()
	if err != nil {
		return err
	}
	if status.Active && status.TimeZone != setting.TimeZone {
		return eventqueue.ErrQuotaConflict
	}
	return nil
}

func (s *Service) ensureQuotaActive(zone string, now time.Time) error {
	s.recorder.quotaActivation.Lock()
	defer s.recorder.quotaActivation.Unlock()
	status, err := s.recorder.queue.QuotaStatus()
	if err != nil {
		return err
	}
	if status.Active {
		if status.TimeZone != zone {
			return eventqueue.ErrQuotaConflict
		}
		return nil
	}
	// The persisted intent freezes configuration before first journal activation.
	// A crash here is retryable; no upstream can have been dispatched yet.
	if s.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), runtimeRefreshTimeout)
		defer cancel()
		err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
			var setting entity.QuotaSetting
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error; err != nil {
				return err
			}
			if setting.TimeZone != zone {
				return eventqueue.ErrQuotaConflict
			}
			if setting.AccountingStarted {
				return nil
			}
			return tx.Model(&setting).Update("AccountingStarted", true).Error
		})
		if err != nil {
			return runtimeUnavailable
		}
	}
	return s.recorder.queue.EnableQuota(zone, now)
}
