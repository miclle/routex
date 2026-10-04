package service

import (
	"maps"
	"strconv"
	"time"
)

// Journal counters can exceed JavaScript's exact integer range; only persisted
// policy maxima use the existing safe-number policy contract.
type MemberKeyQuotaWindow struct {
	Covered       bool              `json:"covered"`
	TokensUsed    string            `json:"tokens_used"`
	TokensHeld    string            `json:"tokens_held"`
	TokensUnknown string            `json:"tokens_unknown"`
	MoneyUsed     map[string]string `json:"money_used"`
	MoneyHeld     map[string]string `json:"money_held"`
	MoneyUnknown  string            `json:"money_unknown"`
}

type MemberKeyQuotaUsage struct {
	AsOf          *time.Time            `json:"as_of"`
	Activated     bool                  `json:"activated"`
	CoverageStart *time.Time            `json:"coverage_start"`
	TimeZone      string                `json:"time_zone"`
	Active        *MemberKeyQuotaWindow `json:"active"`
	Minute        *MemberKeyQuotaWindow `json:"minute"`
	FiveHours     *MemberKeyQuotaWindow `json:"five_hours"`
	SevenDays     *MemberKeyQuotaWindow `json:"seven_days"`
	Month         *MemberKeyQuotaWindow `json:"month"`
}

func memberKeyCounter(value *int64) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if *value < 0 {
		return nil, runtimeUnavailable
	}
	text := strconv.FormatInt(*value, 10)
	return &text, nil
}

func memberKeyQuotaWindow(value *QuotaWindowRecord) (*MemberKeyQuotaWindow, error) {
	if value == nil {
		return nil, nil
	}
	if value.TokensUsed < 0 || value.TokensHeld < 0 || value.TokensUnknown < 0 || value.MoneyUnknown < 0 {
		return nil, runtimeUnavailable
	}
	return &MemberKeyQuotaWindow{Covered: value.Covered, TokensUsed: strconv.FormatInt(value.TokensUsed, 10), TokensHeld: strconv.FormatInt(value.TokensHeld, 10), TokensUnknown: strconv.FormatInt(value.TokensUnknown, 10), MoneyUsed: maps.Clone(value.MoneyUsed), MoneyHeld: maps.Clone(value.MoneyHeld), MoneyUnknown: strconv.FormatInt(value.MoneyUnknown, 10)}, nil
}

func memberKeyQuotaUsage(value *QuotaUsageRecord) (*MemberKeyQuotaUsage, error) {
	if value == nil {
		return nil, nil
	}
	result := &MemberKeyQuotaUsage{AsOf: value.AsOf, Activated: value.Activated, CoverageStart: value.CoverageStart, TimeZone: value.TimeZone}
	for _, pair := range []struct {
		input  *QuotaWindowRecord
		output **MemberKeyQuotaWindow
	}{{value.Active, &result.Active}, {value.Minute, &result.Minute}, {value.FiveHours, &result.FiveHours}, {value.SevenDays, &result.SevenDays}, {value.Month, &result.Month}} {
		converted, err := memberKeyQuotaWindow(pair.input)
		if err != nil {
			return nil, err
		}
		*pair.output = converted
	}
	return result, nil
}
