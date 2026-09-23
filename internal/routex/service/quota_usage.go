package service

import (
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

type QuotaWindowRecord struct {
	Covered       bool              `json:"covered"`
	TokensUsed    int64             `json:"tokens_used"`
	TokensHeld    int64             `json:"tokens_held"`
	TokensUnknown int64             `json:"tokens_unknown"`
	MoneyUsed     map[string]string `json:"money_used"`
	MoneyHeld     map[string]string `json:"money_held"`
	MoneyUnknown  int64             `json:"money_unknown"`
}
type QuotaUsageRecord struct {
	AsOf          *time.Time         `json:"as_of"`
	Activated     bool               `json:"activated"`
	CoverageStart *time.Time         `json:"coverage_start"`
	TimeZone      string             `json:"time_zone"`
	Active        *QuotaWindowRecord `json:"active"`
	Minute        *QuotaWindowRecord `json:"minute"`
	FiveHours     *QuotaWindowRecord `json:"five_hours"`
	SevenDays     *QuotaWindowRecord `json:"seven_days"`
	Month         *QuotaWindowRecord `json:"month"`
}

func quotaWindowRecord(value eventqueue.QuotaUsage, covered bool) *QuotaWindowRecord {
	return &QuotaWindowRecord{Covered: covered, TokensUsed: value.TokensUsed, TokensHeld: value.TokensHeld, TokensUnknown: value.TokensUnknown, MoneyUsed: value.MoneyUsed, MoneyHeld: value.MoneyHeld, MoneyUnknown: value.MoneyUnknown}
}
func (s *Service) resourceQuotaUsage(db *gorm.DB, resolved resolvedLimitTarget) (*QuotaUsageRecord, error) {
	status, err := s.recorder.queue.QuotaStatus()
	if err != nil {
		return nil, runtimeUnavailable
	}
	result := &QuotaUsageRecord{Activated: status.Active, CoverageStart: status.CoverageStart, TimeZone: status.TimeZone}
	if !status.Active {
		var setting entity.QuotaSetting
		if err := db.First(&setting, 1).Error; err != nil {
			return nil, err
		}
		result.TimeZone = setting.TimeZone
		return result, nil
	}
	now := time.Now()
	created, err := quotaAccountCreated(db, resolved.kind, resolved.id, resolved.parentKind == "project")
	if err != nil {
		return nil, err
	}
	usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount(resolved.kind, resolved.id), now)
	if err != nil {
		return nil, runtimeUnavailable
	}
	now = usage.AsOf
	result.AsOf = &now
	location, err := time.LoadLocation(usage.TimeZone)
	if err != nil {
		return nil, runtimeUnavailable
	}
	local := now.In(location)
	month := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	covered := func(start time.Time) bool {
		return !created.Before(usage.CoverageStart) || !start.Before(usage.CoverageStart)
	}
	result.Active = quotaWindowRecord(usage.Active, true)
	result.Minute = quotaWindowRecord(usage.Minute, covered(now.Add(-time.Minute)))
	result.FiveHours = quotaWindowRecord(usage.FiveHours, covered(now.Add(-5*time.Hour)))
	result.SevenDays = quotaWindowRecord(usage.SevenDays, covered(now.Add(-7*24*time.Hour)))
	result.Month = quotaWindowRecord(usage.Month, covered(month))
	return result, nil
}
func effectiveQuotaValues(stored, parent limits.Policy) EffectiveLimitValues {
	currency := stored.Currency
	if parent.MoneyMonth != nil {
		currency = parent.Currency
	}
	return EffectiveLimitValues{RPM: limits.Minimum(parent.RPM, stored.RPM), Concurrency: limits.Minimum(parent.Concurrency, stored.Concurrency), Tokens5H: limits.Minimum(parent.Tokens5H, stored.Tokens5H), Tokens7D: limits.Minimum(parent.Tokens7D, stored.Tokens7D), TokensMonth: limits.Minimum(parent.TokensMonth, stored.TokensMonth), TPM: limits.Minimum(parent.TPM, stored.TPM), MoneyMonth: limits.MoneyMinimum(parent.MoneyMonth, stored.MoneyMonth), Currency: currency}
}
func (s *Service) guardQuotaCurrencyChange(tx *gorm.DB) error {
	var count int64
	if err := tx.Model(&entity.ResourceLimit{}).Where("money_month IS NOT NULL").Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errLimitConflict
	}
	var setting entity.QuotaSetting
	if err := tx.First(&setting, 1).Error; err != nil {
		return err
	}
	if s.recorder == nil {
		if setting.AccountingStarted {
			return runtimeUnavailable
		}
		return nil
	}
	held, err := s.recorder.queue.HasLiveMoneyHolds(time.Now())
	if err != nil {
		return runtimeUnavailable
	}
	if held {
		return errLimitConflict
	}
	return nil
}
