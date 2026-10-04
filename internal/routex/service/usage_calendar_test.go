package service

import (
	"testing"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func TestUsageThirtyDayPresetUsesExactServerElapsedWindow(t *testing.T) {
	for _, test := range []struct {
		name, zone, now, from string
		transitionHours       int
	}{
		{"UTC fractional server instant", "UTC", "2026-10-04T12:34:56.123456789Z", "2026-09-04T12:34:56.123456789Z", 0},
		{"spring transition", "America/New_York", "2026-03-20T12:00:00-04:00", "2026-02-18T11:00:00-05:00", 23},
		{"fall transition", "America/New_York", "2026-11-15T12:00:00-05:00", "2026-10-16T13:00:00-04:00", 25},
		{"leap February", "UTC", "2024-03-01T09:00:00Z", "2024-01-31T09:00:00Z", 0},
		{"non UTC year boundary", "Asia/Shanghai", "2026-01-15T08:30:00+08:00", "2025-12-16T08:30:00+08:00", 0},
		{"server offset differs from display zone", "UTC", "2026-10-04T12:34:56.123456789-03:30", "2026-09-04T16:04:56.123456789Z", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339Nano, test.now)
			if err != nil {
				t.Fatal(err)
			}
			from, err := time.Parse(time.RFC3339Nano, test.from)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := planUsage(UsageFilter{Period: "30d", Timezone: test.zone, Compare: true}, now)
			if err != nil {
				t.Fatal("server thirty-day preset rejected", err)
			}
			const elapsed = 30 * 24 * time.Hour
			if !plan.current.from.Equal(from) || !plan.current.to.Equal(now) || plan.current.to.Sub(plan.current.from) != elapsed || plan.grain != "day" || plan.location.String() != test.zone {
				t.Fatal("preset rounded the server instant or used calendar subtraction", plan)
			}
			if plan.previous == nil || !plan.previous.to.Equal(from) || !plan.previous.from.Equal(from.Add(-elapsed)) || plan.previous.to.Sub(plan.previous.from) != elapsed {
				t.Fatal("comparison was not the immediately preceding elapsed window", plan.previous)
			}
			buckets, err := usageBuckets(plan.current, plan.location, plan.grain)
			if err != nil || len(buckets) == 0 || buckets[0].Start.After(from) || buckets[len(buckets)-1].End.Before(now) {
				t.Fatal("calendar trend lost the exact range edges", buckets, err)
			}
			transitionFound := test.transitionHours == 0
			for i, bucket := range buckets {
				if i > 0 && !bucket.Start.Equal(buckets[i-1].End) {
					t.Fatal("display calendar introduced a gap or overlap")
				}
				if bucket.End.Sub(bucket.Start) == time.Duration(test.transitionHours)*time.Hour {
					transitionFound = true
				}
			}
			if !transitionFound {
				t.Fatal("elapsed preset changed existing DST-aware calendar buckets")
			}
		})
	}
}

func TestUsageThirtyDayPresetPreservesDefaultsAndValidation(t *testing.T) {
	now := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	plan, err := planUsage(UsageFilter{Period: "30d", Granularity: "hour"}, now)
	if err != nil || plan.location != time.UTC || plan.previous != nil || plan.grain != "hour" {
		t.Fatal("preset changed explicit grain or implicit zone/comparison", plan, err)
	}
	buckets, err := usageBuckets(plan.current, plan.location, plan.grain)
	if err != nil || len(buckets) != 720 || !buckets[0].Start.Equal(plan.current.from) || !buckets[len(buckets)-1].End.Equal(now) {
		t.Fatal("valid hourly range lost its existing bucket bounds", len(buckets), err)
	}
	month, err := planUsage(UsageFilter{}, now.Add(time.Hour))
	if err != nil || !month.current.from.Equal(now) || month.grain != "hour" {
		t.Fatal("explicit thirty-day support changed the calendar-month default", month, err)
	}
	from := now.Add(-time.Hour)
	for _, filter := range []UsageFilter{
		{Period: "30D"}, {Period: "030d"}, {Period: "30d "}, {Period: "30days"},
		{Period: "30d", From: &from, To: &now},
		{Period: "30d", From: &from},
		{Period: "30d", To: &now},
		{Period: "30d", Timezone: "Local"},
		{Period: "30d", Timezone: "Mars/Olympus"},
		{Period: "30d", Granularity: "minute"},
	} {
		if _, err := planUsage(filter, now); err != apperrors.ErrBadRequest {
			t.Fatal("preset weakened existing filter validation", filter, err)
		}
	}
	early := time.Date(1, time.February, 15, 0, 0, 0, 0, time.UTC)
	if _, err := planUsage(UsageFilter{Period: "30d"}, early); err != nil {
		t.Fatal("valid early-year current range rejected", err)
	}
	if _, err := planUsage(UsageFilter{Period: "30d", Compare: true}, early); err != apperrors.ErrBadRequest {
		t.Fatal("comparison accepted a range outside supported years", err)
	}
}
