package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"math/big"
)

const quotaWarningGeneration = "personal-monthly-80-90-v1"

// Exact multiplication uses arbitrary precision; no float or int64 percentage
// product may round an 18-place amount or overflow a large settled counter.
func quotaWarningLevel(settled, cap string) (string, int) {
	if !quotaNotificationDecimal.MatchString(settled) || !quotaNotificationDecimal.MatchString(cap) {
		return "", 0
	}
	used, ok := new(big.Rat).SetString(settled)
	limit, valid := new(big.Rat).SetString(cap)
	if !ok || !valid || limit.Sign() <= 0 || used.Sign() < 0 || used.Cmp(limit) >= 0 {
		return "", 0
	}
	scaled := new(big.Rat).Mul(used, big.NewRat(100, 1))
	if scaled.Cmp(new(big.Rat).Mul(limit, big.NewRat(90, 1))) >= 0 {
		return "critical", 90
	}
	if scaled.Cmp(new(big.Rat).Mul(limit, big.NewRat(80, 1))) >= 0 {
		return "near", 80
	}
	return "", 0
}

func monthlyQuotaWarnings(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, currency string) []entity.QuotaWarningObservation {
	if row.ScopeKind != "user" || !validMonthlyQuotaFacts(row, created, usage, 30) {
		return nil
	}
	policy, err := policyFromRow(row)
	if err != nil {
		return nil
	}
	location, err := time.LoadLocation(usage.TimeZone)
	if err != nil {
		return nil
	}
	local := usage.AsOf.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	end := start.AddDate(0, 1, 0)
	coveredFrom := start
	if created.After(coveredFrom) {
		coveredFrom = created
	}
	if coveredFrom.Before(usage.CoverageStart) {
		return nil
	}
	base := entity.QuotaWarningObservation{OwnerID: row.ScopeID, PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ResourceCreatedAt: created.UTC().Truncate(time.Microsecond), ThresholdGeneration: quotaWarningGeneration}
	result := []entity.QuotaWarningObservation{}
	add := func(dimension, cap, settled, denomination string) {
		level, threshold := quotaWarningLevel(settled, cap)
		if level == "" {
			return
		}
		value := base
		value.Dimension = dimension
		value.Limit = cap
		value.Settled = settled
		value.Currency = denomination
		value.Level = level
		value.Threshold = threshold
		result = append(result, value)
	}
	if policy.TokensMonth != nil && *policy.TokensMonth > 0 && usage.Month.TokensUnknown == 0 && usage.Month.TokensUsed >= 0 {
		add("tokens", strconv.FormatInt(*policy.TokensMonth, 10), strconv.FormatInt(usage.Month.TokensUsed, 10), "")
	}
	if policy.MoneyMonth != nil && pricing.Currency(currency) && policy.Currency == currency && usage.Month.MoneyUnknown == 0 {
		amount := "0"
		for code, value := range usage.Month.MoneyUsed {
			if code != currency {
				return result
			}
			amount = value
		}
		add("money", *policy.MoneyMonth, amount, currency)
	}
	return result
}

func sameQuotaWarningIdentity(a, b entity.QuotaWarningObservation) bool {
	return a.OwnerID == b.OwnerID && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt)
}
func quotaWarningIdentityQuery(tx *gorm.DB, observation entity.QuotaWarningObservation) *gorm.DB {
	return tx.Model(&entity.QuotaWarningObservation{}).
		Where(database.ExactText(tx, clause.Column{Name: "owner_id"}, observation.OwnerID)).
		Where(database.ExactText(tx, clause.Column{Name: "dimension"}, observation.Dimension)).
		Where("month_start = ?", observation.MonthStart).
		Where("resource_created_at = ?", observation.ResourceCreatedAt).
		Where(database.ExactText(tx, clause.Column{Name: "policy_revision"}, observation.PolicyRevision)).
		Where(database.ExactText(tx, clause.Column{Name: "currency"}, observation.Currency)).
		Where(database.ExactText(tx, clause.Column{Name: "level"}, observation.Level)).
		Where(database.ExactText(tx, clause.Column{Name: "threshold_generation"}, observation.ThresholdGeneration))
}
func persistQuotaWarning(tx *gorm.DB, observation entity.QuotaWarningObservation) error {
	var existing entity.QuotaWarningObservation
	err := quotaWarningIdentityQuery(tx, observation).Take(&existing).Error
	if err == nil {
		if !sameQuotaWarningIdentity(existing, observation) {
			return errQuotaNotificationIdentity
		}
		return nil // Original snapshot and its read state are never rewritten.
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	observation.ID, err = id.NewPrefixed("qwo")
	if err != nil {
		return err
	}
	if err = tx.Create(&observation).Error; err != nil {
		return err
	}
	inboxID, err := id.NewPrefixed("qwi")
	if err != nil {
		return err
	}
	return tx.Create(&entity.QuotaWarningInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: observation.OwnerID, CreatedAt: observation.AsOf}).Error
}

func (s *Service) quotaWarningApplied(ctx context.Context, row entity.ResourceLimit, user entity.User, applications map[string]entity.RegistrationApprovalApplication, policy limits.Policy, calendar entity.QuotaSetting, currency string) bool {
	if ctx.Err() != nil || s.runtime == nil || row.ScopeKind != "user" || row.ScopeID != user.ID || user.CreatedAt.IsZero() {
		return false
	}
	auth := s.runtime.auth.Load()
	return auth != nil && auth.Quota != nil && auth.Quota.Setting.ETag == calendar.ETag && auth.Quota.Setting.AccountingStarted == calendar.AccountingStarted && s.registrationAdvisoryPublished(auth, user, applications) && s.quotaNotificationResourceApplied(row, user.CreatedAt, policy, calendar.TimeZone, currency) && ctx.Err() == nil && s.runtime.auth.Load() == auth
}
func (s *Service) observeMonthlyQuotaWarning(ctx context.Context, kind, ownerID string) error {
	if kind != "user" || !safeTeamSessionID(ownerID) || s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var user entity.User
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "id"}, ownerID)).Take(&user).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if user.ID != ownerID || user.Disabled || user.OffboardedAt != nil {
			return nil
		}
		applications, err := loadRegistrationApplications(tx, []entity.User{user})
		if err != nil {
			return err
		}
		admitted, _ := registrationAdmission(user, applications)
		if !admitted.AdmissionEligible {
			return nil
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "user")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, ownerID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.ScopeKind != "user" || row.ScopeID != ownerID {
			return nil
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		if err = tx.Take(&calendar, 1).Error; err != nil {
			return err
		}
		var pricingSetting entity.PricingSetting
		if err = tx.Take(&pricingSetting, 1).Error; err != nil {
			return err
		}
		if !s.quotaWarningApplied(ctx, row, user, applications, policy, calendar, pricingSetting.PlatformCurrency) {
			return nil
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount("user", ownerID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != calendar.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		for _, observation := range monthlyQuotaWarnings(row, user.CreatedAt, usage, pricingSetting.PlatformCurrency) {
			if err = persistQuotaWarning(tx, observation); err != nil {
				return err
			}
		}
		if !s.quotaWarningApplied(ctx, row, user, applications, policy, calendar, pricingSetting.PlatformCurrency) {
			return runtimeUnavailable
		}
		return nil
	})
}
