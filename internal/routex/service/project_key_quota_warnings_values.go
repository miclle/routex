package service

import (
	"strconv"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/pricing"
)

func projectKeyMonthlyWarnings(row entity.ResourceLimit, root entity.ProjectKey, project entity.Project, frame *eventqueue.QuotaUsageProofBatch, currency string) []entity.ProjectKeyQuotaWarningObservation {
	account := limitAccount("key", root.ID)
	if frame == nil || !frame.Active || root.ProjectID != project.ID || root.CreatedAt.IsZero() || project.CreatedAt.IsZero() || root.CreatedAt.Before(project.CreatedAt) || project.CreatedAt.After(frame.AsOf) || row.ScopeKind != "key" || row.ScopeID != root.ID || !validCatalogLabel(root.Name) {
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
	base := entity.ProjectKeyQuotaWarningObservation{RootKeyID: root.ID, RootKeyName: root.Name, ProjectID: project.ID, ProjectCreatedAt: project.CreatedAt.UTC(), ResourceCreatedAt: root.CreatedAt.UTC(), PolicyRevision: row.ETag, MonthStart: start.UTC(), MonthEnd: end.UTC(), TimeZone: usage.TimeZone, AsOf: usage.AsOf.UTC().Truncate(time.Microsecond), CoverageStart: usage.CoverageStart.UTC().Truncate(time.Microsecond), ThresholdGeneration: projectKeyQuotaWarningGeneration}
	result := []entity.ProjectKeyQuotaWarningObservation{}
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
