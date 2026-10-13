package service

import (
	"math/big"
	"regexp"
	"strconv"
	"time"

	"github.com/miclle/routex/pkg/eventqueue"
	"github.com/miclle/routex/pkg/limits"
)

type InsightsQuotaInput struct {
	Dimension, ScopeKind, ScopeID, Cursor string
	Limit                                 int
}

type InsightsQuotaScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type InsightsQuotaReport struct {
	Kind          string             `json:"kind"`
	Dimension     string             `json:"dimension"`
	Currency      string             `json:"currency"`
	Scope         InsightsQuotaScope `json:"scope"`
	AsOf          *string            `json:"as_of"`
	CoverageStart *string            `json:"coverage_start"`
	TimeZone      string             `json:"time_zone"`
	BaselineFrom  *string            `json:"baseline_from"`
	BaselineTo    *string            `json:"baseline_to"`
	MonthFrom     *string            `json:"month_from"`
	MonthReset    *string            `json:"month_reset"`
	BaselineBasis string             `json:"baseline_basis"`
	Items         []InsightsQuotaRow `json:"items"`
	NextCursor    *string            `json:"next_cursor"`
}

type InsightsQuotaRow struct {
	ID                      string  `json:"id"`
	Name                    string  `json:"name"`
	Status                  string  `json:"status"`
	SharedKeyCount          int     `json:"shared_key_count"`
	LocalCap                *string `json:"local_cap"`
	Behavior                string  `json:"behavior"`
	MonthSettled            *string `json:"month_settled"`
	BaselineSettled         *string `json:"baseline_settled"`
	BaselineHeld            *string `json:"baseline_held"`
	MonthHeld               *string `json:"month_held"`
	ActiveHeld              *string `json:"active_held"`
	MonthUnknown            *string `json:"month_unknown"`
	BaselineUnknown         *string `json:"baseline_unknown"`
	MonthCovered            bool    `json:"month_covered"`
	BaselineCovered         bool    `json:"baseline_covered"`
	UtilizationPercent      *string `json:"utilization_percent"`
	Level                   string  `json:"level"`
	EstimateAt              *string `json:"estimate_at"`
	EstimateStatus          string  `json:"estimate_status"`
	EstimateWithinSevenDays bool    `json:"estimate_within_seven_days"`
}

var insightsQuotaDecimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,59})(\.[0-9]{1,18})?$`)

func insightsQuotaText(value string) *string { return &value }
func insightsQuotaTime(value time.Time) *string {
	return insightsQuotaText(value.UTC().Format(time.RFC3339Nano))
}

func insightsQuotaCalendar(asOf time.Time, zone string) (time.Time, time.Time, bool) {
	location, err := time.LoadLocation(zone)
	if err != nil || zone == "" || zone == "Local" || asOf.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	local := asOf.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location)
	return start.UTC(), start.AddDate(0, 1, 0).UTC(), true
}

// Rates use settled actual amounts for admissions in (AsOf-168h, AsOf].
// The quota journal expires an admission at exactly its 168-hour boundary;
// active reservations and unknown terminal dimensions never become settled zero.
func insightsQuotaAmount(value eventqueue.QuotaUsage, dimension, currency string) (used, held, unknown string, valid bool) {
	if dimension == "tokens" {
		if value.TokensUsed < 0 || value.TokensHeld < 0 || value.TokensUnknown < 0 {
			return "", "", "", false
		}
		return strconv.FormatInt(value.TokensUsed, 10), strconv.FormatInt(value.TokensHeld, 10), strconv.FormatInt(value.TokensUnknown, 10), true
	}
	if value.MoneyUnknown < 0 {
		return "", "", "", false
	}
	amount := func(values map[string]string) (string, bool) {
		result := "0"
		for code, text := range values {
			if code != currency || !insightsQuotaDecimal.MatchString(text) {
				return "", false
			}
			result = text
		}
		return result, true
	}
	used, valid = amount(value.MoneyUsed)
	if !valid {
		return "", "", "", false
	}
	held, valid = amount(value.MoneyHeld)
	return used, held, strconv.FormatInt(value.MoneyUnknown, 10), valid
}

func insightsQuotaPolicy(row *InsightsQuotaRow, policy limits.Policy, dimension, currency string) {
	row.Behavior = limits.StoredMonthlyBehavior(policy.TokensMonthBehavior)
	if dimension == "tokens" {
		if policy.TokensMonth != nil {
			row.LocalCap = insightsQuotaText(strconv.FormatInt(*policy.TokensMonth, 10))
		}
		return
	}
	row.Behavior = limits.StoredMonthlyBehavior(policy.MoneyMonthBehavior)
	row.LocalCap = policy.MoneyMonth
	if row.LocalCap != nil && policy.Currency != currency {
		row.EstimateStatus = "unavailable"
	}
}

func insightsQuotaProject(row InsightsQuotaRow, policy limits.Policy, dimension, currency string, created time.Time, proof *eventqueue.QuotaUsageAccountProof) InsightsQuotaRow {
	row.Level, row.EstimateStatus = "unknown", "unavailable"
	row.EstimateWithinSevenDays = false
	insightsQuotaPolicy(&row, policy, dimension, currency)
	if proof == nil || !proof.Registered || proof.CreatedAt.IsZero() || !proof.CreatedAt.Equal(created) {
		return row
	}
	usage := proof.Usage
	month, reset, valid := insightsQuotaCalendar(usage.AsOf, usage.TimeZone)
	if !valid || created.IsZero() || created.After(usage.AsOf) || usage.CoverageStart.IsZero() || usage.CoverageStart.After(usage.AsOf) {
		return row
	}
	row.MonthCovered = !month.Before(usage.CoverageStart) || !created.Before(usage.CoverageStart)
	baseline := usage.AsOf.Add(-7 * 24 * time.Hour)
	row.BaselineCovered = !baseline.Before(usage.CoverageStart) && !created.After(baseline)
	mUsed, mHeld, mUnknown, mValid := insightsQuotaAmount(usage.Month, dimension, currency)
	bUsed, bHeld, bUnknown, bValid := insightsQuotaAmount(usage.SevenDays, dimension, currency)
	_, aHeld, aUnknown, aValid := insightsQuotaAmount(usage.Active, dimension, currency)
	if !mValid || !bValid || !aValid || dimension == "money" && row.LocalCap != nil && policy.Currency != currency {
		return row
	}
	row.MonthSettled, row.MonthHeld, row.MonthUnknown = insightsQuotaText(mUsed), insightsQuotaText(mHeld), insightsQuotaText(mUnknown)
	row.BaselineSettled, row.BaselineUnknown, row.ActiveHeld = insightsQuotaText(bUsed), insightsQuotaText(bUnknown), insightsQuotaText(aHeld)
	row.BaselineHeld = insightsQuotaText(bHeld)
	if aUnknown != "0" {
		row.ActiveHeld = nil
	}
	if row.LocalCap == nil {
		row.EstimateStatus = "no_local_cap"
		return row
	}
	cap, ok := new(big.Rat).SetString(*row.LocalCap)
	if !ok || cap.Sign() < 0 {
		return row
	}
	if cap.Sign() == 0 {
		row.EstimateStatus = "zero_cap"
		return row
	}
	if !row.MonthCovered {
		row.EstimateStatus = "incomplete_coverage"
		return row
	}
	monthHeld, _ := new(big.Rat).SetString(mHeld)
	if mUnknown != "0" || monthHeld.Sign() != 0 {
		row.EstimateStatus = "unknown_usage"
		return row
	}
	settled, _ := new(big.Rat).SetString(mUsed)
	percent := new(big.Rat).Mul(new(big.Rat).Quo(settled, cap), big.NewRat(100, 1))
	row.UtilizationPercent = insightsQuotaText(percent.FloatString(2))
	row.Level = "normal"
	if percent.Cmp(big.NewRat(80, 1)) >= 0 {
		row.Level = "near"
	}
	if percent.Cmp(big.NewRat(90, 1)) >= 0 {
		row.Level = "critical"
	}
	if settled.Cmp(cap) >= 0 {
		row.Level, row.EstimateStatus = "reached", "already_reached"
		return row
	}
	if row.Status != "active" {
		row.EstimateStatus = "inactive"
		return row
	}
	if !row.BaselineCovered {
		row.EstimateStatus = "incomplete_coverage"
		return row
	}
	baselineHeld, _ := new(big.Rat).SetString(bHeld)
	if bUnknown != "0" || baselineHeld.Sign() != 0 {
		row.EstimateStatus = "unknown_usage"
		return row
	}
	baselineUsed, _ := new(big.Rat).SetString(bUsed)
	if baselineUsed.Sign() == 0 {
		row.EstimateStatus = "zero_rate"
		return row
	}
	// Ceil the exact rational duration to one nanosecond. Compare to the reset
	// before converting to time.Duration, so huge amounts cannot overflow it.
	nanos := new(big.Rat).Quo(new(big.Rat).Mul(new(big.Rat).Sub(cap, settled), new(big.Rat).SetInt64(int64(7*24*time.Hour))), baselineUsed)
	whole, remainder := new(big.Int), new(big.Int)
	whole.QuoRem(nanos.Num(), nanos.Denom(), remainder)
	if remainder.Sign() != 0 {
		whole.Add(whole, big.NewInt(1))
	}
	if whole.Cmp(big.NewInt(int64(reset.Sub(usage.AsOf)))) >= 0 {
		row.EstimateStatus = "after_reset"
		return row
	}
	eta := usage.AsOf.Add(time.Duration(whole.Int64()))
	row.EstimateAt = insightsQuotaTime(eta)
	row.EstimateWithinSevenDays = !eta.After(usage.AsOf.Add(7 * 24 * time.Hour))
	row.EstimateStatus = "estimated"
	return row
}
