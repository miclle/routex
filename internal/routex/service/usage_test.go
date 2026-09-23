package service

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func usageTestPointer[T any](value T) *T { return &value }
func TestUsageExactCoverage(t *testing.T) {
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	plan, err := planUsage(UsageFilter{From: &start, To: &end}, end)
	if err != nil {
		t.Fatal(err)
	}
	base := entity.CallRecord{RequestID: "req_a", ModelID: "mdl_same", ModelName: "Old", KeyID: "key_a", Status: "success", StartedAt: start, DurationMS: 100, InputTokens: usageTestPointer(int64(math.MaxInt64)), OutputTokens: usageTestPointer(int64(0)), CallPricingFields: entity.CallPricingFields{PricingStatus: "priced", ChargeAmount: usageTestPointer("0.1"), ChargeCurrency: usageTestPointer("USD")}}
	second := base
	second.RequestID = "req_b"
	second.StartedAt = start.Add(time.Hour)
	second.ModelName = "New"
	second.InputTokens = usageTestPointer(int64(2))
	second.ChargeAmount = usageTestPointer("0.2")
	third := base
	third.RequestID = "req_c"
	third.InputTokens = nil
	third.OutputTokens = usageTestPointer(int64(3))
	third.Status = "error"
	third.PricingStatus = "unknown_usage"
	third.ChargeAmount = nil
	third.ChargeCurrency = nil
	fourth := base
	fourth.RequestID = "req_d"
	fourth.InputTokens = usageTestPointer(int64(0))
	fourth.ChargeCurrency = usageTestPointer("CNY")
	fourth.ChargeAmount = usageTestPointer("0")
	fourth.KeyID = ""
	fourth.ModelID = ""
	fourth.ModelName = ""
	report, err := aggregateUsage([]entity.CallRecord{base, second, third, fourth}, plan.current, plan, true)
	if err != nil {
		t.Fatal(err)
	}
	stats := report.Summary
	if stats.Requests != 4 || stats.Successes != 3 || stats.Errors != 1 || stats.SuccessRate == nil || *stats.SuccessRate != .75 || stats.AverageDurationMS == nil || *stats.AverageDurationMS != 100 {
		t.Fatalf("unexpected summary: %+v", stats)
	}
	if stats.Tokens.Input.Value != nil || stats.Tokens.Input.Known != "9223372036854775809" || stats.Tokens.Input.UnknownCalls != 1 || stats.Tokens.Total.Known != "9223372036854775812" || stats.Tokens.Total.Value != nil || stats.Tokens.Output.Value == nil || *stats.Tokens.Output.Value != "3" {
		t.Fatalf("coverage/precision lost: %+v", stats.Tokens)
	}
	if len(stats.Amounts) != 2 || stats.Amounts[0].Currency != "CNY" || stats.Amounts[0].Amount != "0" || stats.Amounts[0].Calls != 1 || stats.Amounts[1].Amount != "0.3" || stats.UnknownAmountCalls != 1 {
		t.Fatalf("money conflated: %+v", stats)
	}
	if len(report.Models) != 2 || report.Models[0].ID != "mdl_same" || report.Models[0].Name != "New" || !report.Models[1].Unknown || report.Models[0].Stats.Requests != 3 {
		t.Fatalf("historical model grouping changed: %+v", report.Models)
	}
	if len(report.Trend) != 3 || report.Trend[2].Stats.Requests != 0 || report.Trend[2].Stats.Tokens.Total.Value == nil || *report.Trend[2].Stats.Tokens.Total.Value != "0" || report.Trend[2].Stats.SuccessRate != nil || len(report.Trend[2].Stats.Amounts) != 0 {
		t.Fatal("empty bucket differs from known zero contract")
	}
	personal, err := aggregateUsage([]entity.CallRecord{base}, plan.current, plan, false)
	if err != nil || len(personal.ProviderModels) != 0 || len(personal.Connections) != 0 {
		t.Fatal("personal aggregate exposed routes")
	}
	negative := base
	negative.InputTokens = usageTestPointer(int64(-1))
	if _, err := aggregateUsage([]entity.CallRecord{negative}, plan.current, plan, false); err == nil {
		t.Fatal("negative persisted usage accepted")
	}
	corrupt := base
	corrupt.ChargeAmount = usageTestPointer("secret")
	if _, err := aggregateUsage([]entity.CallRecord{corrupt}, plan.current, plan, false); err == nil {
		t.Fatal("invalid receipt was accepted")
	}
}
func TestUsageCalendarBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, from, to string
		hours          int
	}{{"spring", "2026-03-08T00:00:00-05:00", "2026-03-09T00:00:00-04:00", 23}, {"fall", "2026-11-01T00:00:00-04:00", "2026-11-02T00:00:00-05:00", 25}} {
		t.Run(test.name, func(t *testing.T) {
			from, _ := time.Parse(time.RFC3339, test.from)
			to, _ := time.Parse(time.RFC3339, test.to)
			plan, err := planUsage(UsageFilter{From: &from, To: &to, Timezone: "America/New_York", Granularity: "hour", Compare: true}, to)
			if err != nil {
				t.Fatal(err)
			}
			buckets, err := usageBuckets(plan.current, plan.location, plan.grain)
			if err != nil || len(buckets) != test.hours {
				t.Fatalf("DST hour count %d: %v", len(buckets), err)
			}
			for i := 1; i < len(buckets); i++ {
				if !buckets[i].Start.Equal(buckets[i-1].End) {
					t.Fatal("DST gap or overlap")
				}
			}
			if plan.previous.to.Sub(plan.previous.from) != to.Sub(from) {
				t.Fatal("comparison changed elapsed duration")
			}
			day, err := usageBuckets(plan.current, plan.location, "day")
			if err != nil || len(day) != 1 || day[0].End.Sub(day[0].Start) != time.Duration(test.hours)*time.Hour {
				t.Fatal("calendar day became fixed24h")
			}
		})
	}
	loc, err := time.LoadLocation("America/Havana")
	if err != nil {
		t.Fatal(err)
	}
	skipped := usageCalendarStart(loc, 2024, time.March, 10)
	if skipped.Day() != 10 || skipped.Hour() != 1 {
		t.Fatalf("skipped midnight failed: %v", skipped)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, shanghai)
	plan, err := planUsage(UsageFilter{Period: "today", Timezone: "Asia/Shanghai"}, now)
	if err != nil || plan.current.from.Hour() != 0 || plan.current.from.Day() != 23 {
		t.Fatal("today ignored timezone")
	}
	monday := usageBucketStart(now, shanghai, "week")
	if monday.Weekday() != time.Monday || monday.Day() != 21 {
		t.Fatal("week did not start Monday")
	}
	leap := usageCalendarStart(shanghai, 2024, time.February, 1)
	if nextUsageBucket(leap, shanghai, "month").Sub(leap) != 29*24*time.Hour {
		t.Fatal("month lost leap day")
	}
}
func TestUsageBoundsAndFilters(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(365 * 24 * time.Hour)
	for _, filter := range []UsageFilter{{From: &from}, {From: &from, To: &from}, {From: &from, To: &to, Period: "year"}, {From: &from, To: &to, Granularity: "minute"}, {Timezone: "Mars/Olympus"}, {Timezone: "Local"}, {Period: "forever"}, {From: &from, To: &to, Granularity: "hour"}} {
		if _, err := planUsage(filter, to); err == nil {
			t.Fatalf("invalid/oversize range accepted: %+v", filter)
		}
	}
	if _, err := planUsage(UsageFilter{From: &from, To: &to}, to); err != nil {
		t.Fatal(err)
	}
	for _, filter := range []UsageFilter{{UserID: "usr_other"}, {ProjectID: "prj_other"}, {ConnectionID: "con_other"}, {ProviderModelID: "pmd_other"}, {Status: "failed"}, {Protocol: "anything"}, {KeyID: "key_%"}} {
		if err := validateUsageFilter(filter, false); err == nil {
			t.Fatal("unsafe personal filter accepted")
		}
	}
	if err := validateUsageFilter(UsageFilter{UserID: "usr_a", ProjectID: "prj_a"}, true); err == nil {
		t.Fatal("ambiguous principal accepted")
	}
	end := from.Add(time.Hour)
	plan, err := planUsage(UsageFilter{From: &from, To: &end}, end)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]entity.CallRecord, usageGroupLimit+1)
	for i := range rows {
		rows[i] = entity.CallRecord{RequestID: fmt.Sprintf("req_%d", i), ModelID: fmt.Sprintf("mdl_%d", i), Status: "success", StartedAt: from}
	}
	if _, err := aggregateUsage(rows, plan.current, plan, false); err != usageTooLarge {
		t.Fatalf("high cardinality result silently truncated: %v", err)
	}
}
