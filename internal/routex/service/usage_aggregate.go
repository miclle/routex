package service

import (
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

type usageSum struct {
	known   big.Int
	unknown int64
}

func (s *usageSum) add(value *int64) {
	if value == nil {
		s.unknown++
		return
	}
	s.known.Add(&s.known, big.NewInt(*value))
}
func (s *usageSum) result() UsageCount {
	known := s.known.String()
	result := UsageCount{Known: known, UnknownCalls: s.unknown}
	if s.unknown == 0 {
		result.Value = &known
	}
	return result
}

type usageMoney struct {
	amount big.Rat
	calls  int64
}
type usageAccumulator struct {
	requests, successes, errors, canceled, unknownMoney int64
	duration                                            big.Int
	input, output, total                                usageSum
	money                                               map[string]*usageMoney
	priceStates                                         map[string]int64
}

func (a *usageAccumulator) add(row entity.CallRecord) error {
	if row.DurationMS < 0 || (row.InputTokens != nil && *row.InputTokens < 0) || (row.OutputTokens != nil && *row.OutputTokens < 0) {
		return apperrors.ErrInternal
	}
	a.requests++
	switch row.Status {
	case "success":
		a.successes++
	case "error":
		a.errors++
	case "canceled":
		a.canceled++
	default:
		return apperrors.ErrInternal
	}
	a.duration.Add(&a.duration, big.NewInt(row.DurationMS))
	a.input.add(row.InputTokens)
	a.output.add(row.OutputTokens)
	if row.InputTokens == nil || row.OutputTokens == nil {
		a.total.unknown++
	}
	for _, counter := range []*int64{row.InputTokens, row.OutputTokens} {
		if counter != nil {
			a.total.known.Add(&a.total.known, big.NewInt(*counter))
		}
	}
	if a.priceStates == nil {
		a.priceStates = map[string]int64{}
	}
	state := usagePricingStatus(row.PricingStatus)
	a.priceStates[state]++
	if state != "priced" || row.ChargeAmount == nil || row.ChargeCurrency == nil {
		a.unknownMoney++
		return nil
	}
	if !chargeAmountPattern.MatchString(*row.ChargeAmount) || !pricing.Currency(*row.ChargeCurrency) {
		return apperrors.ErrInternal
	}
	amount, ok := new(big.Rat).SetString(*row.ChargeAmount)
	if !ok {
		return apperrors.ErrInternal
	}
	if a.money == nil {
		a.money = map[string]*usageMoney{}
	}
	currency := *row.ChargeCurrency
	if a.money[currency] == nil {
		a.money[currency] = &usageMoney{}
	}
	a.money[currency].amount.Add(&a.money[currency].amount, amount)
	a.money[currency].calls++
	return nil
}
func (a *usageAccumulator) result() UsageStats {
	result := UsageStats{Requests: a.requests, Successes: a.successes, Errors: a.errors, Canceled: a.canceled, Tokens: UsageTokens{Input: a.input.result(), Output: a.output.result(), Total: a.total.result()}, Amounts: []UsageAmount{}, UnknownAmountCalls: a.unknownMoney, PricingStatuses: map[string]int64{}}
	for state, count := range a.priceStates {
		result.PricingStatuses[state] = count
	}
	if a.requests > 0 {
		rate := float64(a.successes) / float64(a.requests)
		average, _ := new(big.Rat).SetFrac(&a.duration, big.NewInt(a.requests)).Float64()
		result.SuccessRate = &rate
		result.AverageDurationMS = &average
	}
	for currency, money := range a.money {
		amount := strings.TrimRight(strings.TrimRight(money.amount.FloatString(18), "0"), ".")
		result.Amounts = append(result.Amounts, UsageAmount{Currency: currency, Amount: amount, Calls: money.calls})
	}
	sort.Slice(result.Amounts, func(i, j int) bool { return result.Amounts[i].Currency < result.Amounts[j].Currency })
	return result
}

type usageGrouped struct {
	name     string
	latest   time.Time
	latestID string
	acc      usageAccumulator
}

func addUsageGroup(groups map[string]*usageGrouped, groupID, name string, row entity.CallRecord) error {
	group := groups[groupID]
	if group == nil {
		if len(groups) >= usageGroupLimit {
			return usageTooLarge
		}
		group = &usageGrouped{}
		groups[groupID] = group
	}
	if row.StartedAt.After(group.latest) || (row.StartedAt.Equal(group.latest) && row.RequestID > group.latestID) {
		group.name = name
		group.latest = row.StartedAt
		group.latestID = row.RequestID
	}
	return group.acc.add(row)
}
func usageGroups(groups map[string]*usageGrouped) []UsageGroup {
	result := make([]UsageGroup, 0, len(groups))
	for id, group := range groups {
		result = append(result, UsageGroup{ID: id, Name: group.name, Unknown: id == "", Stats: group.acc.result()})
	}
	sort.Slice(result, func(i, j int) bool {
		a, _ := new(big.Int).SetString(result[i].Stats.Tokens.Total.Known, 10)
		b, _ := new(big.Int).SetString(result[j].Stats.Tokens.Total.Known, 10)
		if cmp := a.Cmp(b); cmp != 0 {
			return cmp > 0
		}
		if result[i].Stats.Requests != result[j].Stats.Requests {
			return result[i].Stats.Requests > result[j].Stats.Requests
		}
		return result[i].ID < result[j].ID
	})
	return result
}
func aggregateUsage(rows []entity.CallRecord, period usageRange, plan usagePlan, admin bool) (UsagePeriod, error) {
	buckets, err := usageBuckets(period, plan.location, plan.grain)
	if err != nil {
		return UsagePeriod{}, err
	}
	summary := usageAccumulator{}
	trend := make([]usageAccumulator, len(buckets))
	models, keys, providerModels, connections := map[string]*usageGrouped{}, map[string]*usageGrouped{}, map[string]*usageGrouped{}, map[string]*usageGrouped{}
	for _, row := range rows {
		if row.StartedAt.Before(period.from) || !row.StartedAt.Before(period.to) {
			continue
		}
		if err := summary.add(row); err != nil {
			return UsagePeriod{}, err
		}
		index := sort.Search(len(buckets), func(i int) bool { return buckets[i].End.After(row.StartedAt) })
		if index == len(buckets) {
			return UsagePeriod{}, apperrors.ErrInternal
		}
		if err := trend[index].add(row); err != nil {
			return UsagePeriod{}, err
		}
		if err := addUsageGroup(models, row.ModelID, row.ModelName, row); err != nil {
			return UsagePeriod{}, err
		}
		if err := addUsageGroup(keys, row.KeyID, "", row); err != nil {
			return UsagePeriod{}, err
		}
		if admin {
			if err := addUsageGroup(providerModels, row.ProviderModelID, "", row); err != nil {
				return UsagePeriod{}, err
			}
			if err := addUsageGroup(connections, row.ConnectionID, "", row); err != nil {
				return UsagePeriod{}, err
			}
		}
	}
	for index := range buckets {
		buckets[index].Stats = trend[index].result()
	}
	result := UsagePeriod{From: period.from, To: period.to, Summary: summary.result(), Trend: buckets, Models: usageGroups(models), Keys: usageGroups(keys)}
	if admin {
		result.ProviderModels = usageGroups(providerModels)
		result.Connections = usageGroups(connections)
	}
	return result, nil
}

func usagePricingStatus(status string) string {
	switch status {
	case "not_captured", "unsupported", "not_final", "unknown_usage", "invalid_usage", "missing_price", "invalid_configuration", "priced":
		return status
	default:
		return "not_captured"
	}
}
