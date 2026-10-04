package service

import (
	"context"
	"errors"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/miclle/routex/internal/routex/database"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/id"
	"github.com/miclle/routex/pkg/limits"
	"github.com/miclle/routex/pkg/pricing"
)

var quotaNotificationDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,59})(\.[0-9]{1,18})?$`)
var errQuotaNotificationIdentity = errors.New("quota notification identity mismatch")

// monthlyQuotaObservations observes current settled levels only. Neither a
// reservation rejection nor an outstanding hold establishes settled exhaustion.
func monthlyQuotaObservations(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, platformCurrency string) []entity.QuotaNotificationObservation {
	if row.ScopeKind != "user" && row.ScopeKind != "project" && row.ScopeKind != "team" || !validMonthlyQuotaFacts(row, created, usage, 30) {
		return nil
	}
	return monthlyQuotaSettledObservations(row, created, usage, platformCurrency)
}

func validMonthlyQuotaFacts(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, maxScope int) bool {
	return row.ScopeID != "" && len(row.ScopeID) <= maxScope && safeCallID.MatchString(row.ScopeID) && row.ETag != "" && len(row.ETag) <= 64 && safeCallID.MatchString(row.ETag) && usage != nil && !usage.AsOf.IsZero() && !usage.CoverageStart.IsZero() && !created.IsZero() && !created.After(usage.AsOf) && !usage.CoverageStart.After(usage.AsOf) && usage.TimeZone != "" && usage.TimeZone != "Local" && len(usage.TimeZone) <= 100
}

func monthlyQuotaSettledObservations(row entity.ResourceLimit, created time.Time, usage *eventqueue.AccountQuotaUsage, platformCurrency string) []entity.QuotaNotificationObservation {
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
	effectiveStart := start
	if created.After(effectiveStart) {
		effectiveStart = created
	}
	if effectiveStart.Before(usage.CoverageStart) {
		return nil
	}
	base := entity.QuotaNotificationObservation{ScopeKind: row.ScopeKind, ScopeID: row.ScopeID, PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ResourceCreatedAt: created.UTC().Truncate(time.Microsecond)}
	result := []entity.QuotaNotificationObservation{}
	if policy.TokensMonth != nil && usage.Month.TokensUnknown == 0 && usage.Month.TokensUsed >= 0 && usage.Month.TokensUsed >= *policy.TokensMonth {
		observation := base
		observation.Dimension = "tokens"
		observation.Limit = strconv.FormatInt(*policy.TokensMonth, 10)
		observation.Settled = strconv.FormatInt(usage.Month.TokensUsed, 10)
		result = append(result, observation)
	}
	if policy.MoneyMonth != nil && pricing.Currency(platformCurrency) && policy.Currency == platformCurrency && usage.Month.MoneyUnknown == 0 {
		amount := "0"
		for currency, settled := range usage.Month.MoneyUsed {
			if currency != policy.Currency {
				return result
			}
			amount = settled
		}
		if quotaNotificationDecimal.MatchString(amount) {
			used, usedOK := new(big.Rat).SetString(amount)
			limit, limitOK := new(big.Rat).SetString(*policy.MoneyMonth)
			if usedOK && limitOK && used.Cmp(limit) >= 0 {
				observation := base
				observation.Dimension = "money"
				observation.Currency = policy.Currency
				observation.Limit = *policy.MoneyMonth
				observation.Settled = amount
				result = append(result, observation)
			}
		}
	}
	return result
}

func (s *Service) quotaNotificationPolicyApplied(row entity.ResourceLimit, policy limits.Policy, timeZone, currency string) bool {
	return s.quotaNotificationApplied(row, nil, policy, timeZone, currency)
}

// The Team creation basis must match the applied resource generation, not only
// the account policy. No historical observation grants current Team authority.
func (s *Service) quotaNotificationResourceApplied(row entity.ResourceLimit, created time.Time, policy limits.Policy, timeZone, currency string) bool {
	return s.quotaNotificationApplied(row, &created, policy, timeZone, currency)
}

func (s *Service) quotaNotificationApplied(row entity.ResourceLimit, created *time.Time, policy limits.Policy, timeZone, currency string) bool {
	if s.runtime == nil {
		return false
	}
	auth := s.runtime.auth.Load()
	account := limitAccount(row.ScopeKind, row.ScopeID)
	if auth == nil || !time.Now().Before(auth.ValidUntil) || auth.Quota == nil || auth.Quota.Revisions[account] != row.ETag || auth.Quota.Setting.TimeZone != timeZone || runtimeDenied(&s.runtime.deniedLimits, account) || runtimeDenied(&s.runtime.deniedLimits, "quota_settings") {
		return false
	}
	if row.ScopeKind == "team" {
		team, exists := auth.Teams[row.ScopeID]
		if !exists || runtimeDenied(&s.runtime.deniedTeams, row.ScopeID) || created != nil && (created.IsZero() || !team.CreatedAt.Equal(*created)) {
			return false
		}
	}
	published, err := limits.Normalize(auth.LimitPolicies[account])
	return err == nil && reflect.DeepEqual(published, policy) && (policy.MoneyMonth == nil || auth.Quota.Currency == currency)
}

func sameQuotaObservationIdentity(a, b entity.QuotaNotificationObservation) bool {
	return a.ScopeKind == b.ScopeKind && a.ScopeID == b.ScopeID && a.Dimension == b.Dimension && a.PolicyRevision == b.PolicyRevision && a.Currency == b.Currency && a.MonthStart.Equal(b.MonthStart) && sameQuotaMemberProof(a, b)
}

func persistQuotaNotification(tx *gorm.DB, observation entity.QuotaNotificationObservation, recipients []string) error {
	var existing entity.QuotaNotificationObservation
	err := tx.Where("scope_kind = ? AND scope_id = ? AND dimension = ? AND month_start = ? AND policy_revision = ? AND currency = ?", observation.ScopeKind, observation.ScopeID, observation.Dimension, observation.MonthStart, observation.PolicyRevision, observation.Currency).Take(&existing).Error
	if err == nil {
		if !sameQuotaObservationIdentity(existing, observation) {
			return errQuotaNotificationIdentity
		}
		// The original snapshot and recipient set are immutable, including read state.
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}
	observation.ID, err = id.NewPrefixed("qob")
	if err != nil {
		return err
	}
	if err := tx.Create(&observation).Error; err != nil {
		return err
	}
	for _, recipient := range recipients {
		if recipient == "" || len(recipient) > 30 || !safeCallID.MatchString(recipient) {
			return errQuotaNotificationIdentity
		}
		inboxID, err := id.NewPrefixed("qni")
		if err != nil {
			return err
		}
		if err := tx.Create(&entity.QuotaNotificationInbox{ID: inboxID, ObservationID: observation.ID, RecipientID: recipient, CreatedAt: observation.AsOf}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) observeMonthlyQuotaNotification(ctx context.Context, kind, scopeID string) error {
	if kind != "user" && kind != "project" && kind != "team" || scopeID == "" || len(scopeID) > 30 || !safeCallID.MatchString(scopeID) {
		return nil
	}
	if s.recorder == nil || s.runtime == nil {
		return nil
	}
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockGovernance(tx); err != nil {
			return err
		}
		var created time.Time
		var scopeName string
		switch kind {
		case "user":
			var user entity.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "created_at", "disabled", "offboarded_at").Where(database.ExactText(tx, clause.Column{Name: "id"}, scopeID)).First(&user).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			if user.ID != scopeID || user.Disabled || user.OffboardedAt != nil {
				return nil
			}
			created = user.CreatedAt
		case "team":
			var team entity.Team
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "created_at", "status").Where(database.ExactText(tx, clause.Column{Name: "id"}, scopeID)).First(&team).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			if team.ID != scopeID || team.Status != entity.ResourceActive || !validCatalogLabel(team.Name) {
				return nil
			}
			created, scopeName = team.CreatedAt, team.Name
		default:
			var project entity.Project
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "name", "created_at", "status").Where(database.ExactText(tx, clause.Column{Name: "id"}, scopeID)).First(&project).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			} else if err != nil {
				return err
			}
			if project.ID != scopeID || project.Status != entity.ResourceActive {
				return nil
			}
			created = project.CreatedAt
			scopeName = project.Name
			if !validCatalogLabel(scopeName) {
				return nil
			}
		}
		var row entity.ResourceLimit
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(database.ExactText(tx, clause.Column{Name: "scope_kind"}, kind)).Where(database.ExactText(tx, clause.Column{Name: "scope_id"}, scopeID)).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		} else if err != nil {
			return err
		}
		if row.ScopeKind != kind || row.ScopeID != scopeID {
			return nil
		}
		policy, err := policyFromRow(row)
		if err != nil {
			return nil
		}
		var setting entity.QuotaSetting
		if err := tx.First(&setting, 1).Error; err != nil {
			return err
		}
		var pricingSetting entity.PricingSetting
		if err := tx.First(&pricingSetting, 1).Error; err != nil {
			return err
		}
		if !s.quotaNotificationResourceApplied(row, created, policy, setting.TimeZone, pricingSetting.PlatformCurrency) {
			return nil
		}
		status, err := s.recorder.queue.QuotaStatus()
		if err != nil {
			return runtimeUnavailable
		}
		if !status.Active {
			return nil
		}
		usage, err := s.recorder.queue.AccountQuotaUsage(limitAccount(kind, scopeID), time.Now())
		if err != nil {
			return runtimeUnavailable
		}
		if usage.TimeZone != setting.TimeZone || status.TimeZone != usage.TimeZone || status.CoverageStart == nil || !status.CoverageStart.Equal(usage.CoverageStart) {
			return nil
		}
		observations := monthlyQuotaObservations(row, created, usage, pricingSetting.PlatformCurrency)
		if len(observations) == 0 {
			return nil
		}
		recipients, err := quotaNotificationRecipients(tx, kind, scopeID)
		if err != nil {
			return err
		}
		for _, observation := range observations {
			observation.ScopeName = scopeName
			if err := persistQuotaNotification(tx, observation, recipients); err != nil {
				return err
			}
		}
		// Publication may expire while a resource lock is awaited. Roll back instead
		// of turning a saved but unapplied revision into a member notification.
		if !s.quotaNotificationResourceApplied(row, created, policy, setting.TimeZone, pricingSetting.PlatformCurrency) {
			return runtimeUnavailable
		}
		return nil
	})
}
