package service

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/pricing"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func personalKeyMonthlyWarnings(row entity.ResourceLimit, root entity.APIKey, owner entity.User, frame *eventqueue.QuotaUsageProofBatch, currency string) []entity.PersonalKeyQuotaWarningObservation {
	account := limitAccount("key", root.ID)
	if frame == nil || !frame.Active || root.UserID != owner.ID || root.CreatedAt.IsZero() || owner.CreatedAt.IsZero() || root.CreatedAt.Before(owner.CreatedAt) || owner.CreatedAt.After(frame.AsOf) || row.ScopeKind != "key" || row.ScopeID != root.ID || !validCatalogLabel(root.Name) {
		return nil
	}
	proof, ok := frame.Accounts[account]
	if !ok || !proof.Registered || proof.CreatedAt.IsZero() || !proof.CreatedAt.Equal(root.CreatedAt) {
		return nil
	}
	usage := proof.Usage
	if !usage.AsOf.Equal(frame.AsOf) || !usage.CoverageStart.Equal(frame.CoverageStart) || usage.TimeZone != frame.TimeZone || !validMonthlyQuotaFacts(row, root.CreatedAt, &usage, 30) {
		return nil
	}
	policy, err := policyFromRow(row)
	if err != nil {
		return nil
	}
	loc, err := time.LoadLocation(usage.TimeZone)
	if err != nil {
		return nil
	}
	local := usage.AsOf.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	covered := start
	if root.CreatedAt.After(covered) {
		covered = root.CreatedAt
	}
	if covered.Before(usage.CoverageStart) {
		return nil
	}
	base := entity.PersonalKeyQuotaWarningObservation{RootKeyID: root.ID, RootKeyName: root.Name, OwnerID: owner.ID, OwnerCreatedAt: owner.CreatedAt.UTC(), ResourceCreatedAt: root.CreatedAt.UTC(), PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ThresholdGeneration: personalKeyQuotaWarningGeneration}
	result := []entity.PersonalKeyQuotaWarningObservation{}
	// Independent settled coverage: a hold in one dimension never blocks the other.
	if policy.TokensMonth != nil && *policy.TokensMonth > 0 && usage.Month.TokensUsed >= 0 && usage.Month.TokensUnknown == 0 && usage.Month.TokensHeld == 0 && usage.Active.TokensHeld == 0 && usage.Active.TokensUnknown == 0 {
		v := base
		v.Dimension = "tokens"
		v.Limit = strconv.FormatInt(*policy.TokensMonth, 10)
		v.Settled = strconv.FormatInt(usage.Month.TokensUsed, 10)
		v.Level, v.Threshold = quotaWarningLevel(v.Settled, v.Limit)
		if v.Level != "" {
			result = append(result, v)
		}
	}
	if policy.MoneyMonth != nil && pricing.Currency(currency) && policy.Currency == currency && usage.Month.MoneyUnknown == 0 && usage.Active.MoneyUnknown == 0 && zeroWarningMoney(usage.Month.MoneyHeld) && zeroWarningMoney(usage.Active.MoneyHeld) {
		amount := "0"
		for currency, value := range usage.Month.MoneyUsed {
			if currency != policy.Currency {
				return result
			}
			amount = value
		}
		v := base
		v.Dimension = "money"
		v.Currency = policy.Currency
		v.Limit = *policy.MoneyMonth
		v.Settled = amount
		v.Level, v.Threshold = quotaWarningLevel(amount, v.Limit)
		if v.Level != "" {
			result = append(result, v)
		}
	}
	return result
}
func zeroWarningMoney(values map[string]string) bool {
	for _, v := range values {
		if !quotaNotificationDecimal.MatchString(v) {
			return false
		}
		n, ok := new(big.Rat).SetString(v)
		if !ok || n.Sign() != 0 {
			return false
		}
	}
	return true
}
func samePersonalKeyWarningIdentity(a, b entity.PersonalKeyQuotaWarningObservation) bool {
	return a.RootKeyID == b.RootKeyID && a.OwnerID == b.OwnerID && a.OwnerCreatedAt.Equal(b.OwnerCreatedAt) && a.ResourceCreatedAt.Equal(b.ResourceCreatedAt) && a.Dimension == b.Dimension && a.MonthStart.Equal(b.MonthStart) && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.Level == b.Level && a.Threshold == b.Threshold && a.ThresholdGeneration == b.ThresholdGeneration
}
func persistPersonalKeyQuotaWarning(tx *gorm.DB, v entity.PersonalKeyQuotaWarningObservation) error {
	var old entity.PersonalKeyQuotaWarningObservation
	err := tx.Where("root_key_id = ? AND owner_id = ? AND owner_created_at = ? AND resource_created_at = ? AND dimension = ? AND month_start = ? AND policy_revision = ? AND currency = ? AND level = ? AND threshold_generation = ?", v.RootKeyID, v.OwnerID, v.OwnerCreatedAt, v.ResourceCreatedAt, v.Dimension, v.MonthStart, v.PolicyRevision, v.Currency, v.Level, v.ThresholdGeneration).Take(&old).Error
	if err == nil {
		if !samePersonalKeyWarningIdentity(old, v) {
			return errQuotaNotificationIdentity
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	v.ID, err = id.NewPrefixed("kwo")
	if err != nil {
		return err
	}
	if err = tx.Create(&v).Error; err != nil {
		return err
	}
	inboxID, err := id.NewPrefixed("kwi")
	if err != nil {
		return err
	}
	return tx.Create(&entity.PersonalKeyQuotaWarningInbox{ID: inboxID, ObservationID: v.ID, RecipientID: v.OwnerID, RecipientCreatedAt: v.OwnerCreatedAt, CreatedAt: v.AsOf}).Error
}
func (s *Service) observeMonthlyPersonalKeyQuotaWarning(ctx context.Context, kind, rootID string) error {
	if kind != "key" || !memberKeyID.MatchString(rootID) || s.recorder == nil || s.recorder.queue == nil || s.runtime == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, runtimeRefreshTimeout)
	defer cancel()
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var seed entity.APIKey
		err := tx.Select("id,user_id").Where("id = ?", rootID).Where(database.ExactText(tx, clause.Column{Name: "id"}, rootID)).Take(&seed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if seed.ID != rootID {
			return nil
		}
		var owner entity.User
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "created_at", "disabled", "offboarded_at", "approval_application_id").Where("id = ?", seed.UserID).Where(database.ExactText(tx, clause.Column{Name: "id"}, seed.UserID)).Take(&owner).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if owner.ID != seed.UserID {
			return nil
		}
		apps, err := loadRegistrationApplications(tx, []entity.User{owner})
		if err != nil {
			return err
		}
		_, admitted := registrationAdmission(owner, apps)
		if !admitted.Eligible {
			return nil
		}
		var keys []entity.APIKey
		if err = personalKeyWarningChainQuery(tx, owner.ID).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&keys).Error; err != nil {
			return err
		}
		roots, complete := personalKeyWarningRoots(keys, owner.ID)
		if !complete {
			return errQuotaNotificationIdentity
		}
		if roots[rootID] != rootID {
			return nil
		}
		var root entity.APIKey
		for _, k := range keys {
			if k.ID == rootID {
				root = k
				break
			}
		}
		var row entity.ResourceLimit
		err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("scope_kind = ? AND scope_id = ?", "key", rootID).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, "key")).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, rootID)).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var calendar entity.QuotaSetting
		var prices entity.PricingSetting
		if err = tx.First(&calendar, 1).Error; err != nil {
			return err
		}
		if err = tx.First(&prices, 1).Error; err != nil {
			return err
		}
		auth := s.runtime.auth.Load()
		if !s.personalKeyWarningApplied(ctx, auth, owner, apps, keys, rootID, row, policy, calendar, prices.PlatformCurrency) {
			return nil
		}
		frame, err := s.recorder.queue.AccountQuotaUsageProofBatch([]string{limitAccount("key", rootID)}, time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if frame.TimeZone != calendar.TimeZone {
			return nil
		}
		for _, v := range personalKeyMonthlyWarnings(row, root, owner, frame, prices.PlatformCurrency) {
			if err = persistPersonalKeyQuotaWarning(tx, v); err != nil {
				return err
			}
		}
		if !s.personalKeyWarningApplied(ctx, auth, owner, apps, keys, rootID, row, policy, calendar, prices.PlatformCurrency) {
			return runtimeUnavailable
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
}
