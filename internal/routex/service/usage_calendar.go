package service

import (
	"time"
	_ "time/tzdata" // Keep IANA calendar queries available in the single-binary deployment.

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

const usageRowLimit = 10000
const usageBucketLimit = 1000
const usageGroupLimit = 500

var usageTooLarge = &apperrors.Error{Code: 422, Message: "usage range is too large; narrow the time range or filters"}

type usageRange struct{ from, to time.Time }
type usagePlan struct {
	current  usageRange
	previous *usageRange
	location *time.Location
	grain    string
}

func planUsage(filter UsageFilter, now time.Time) (usagePlan, error) {
	plan := usagePlan{}
	zone := filter.Timezone
	if zone == "" {
		zone = "UTC"
	}
	if len(zone) > 100 || zone == "Local" {
		return plan, apperrors.ErrBadRequest
	}
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return plan, apperrors.ErrBadRequest
	}
	plan.location = loc
	now = now.In(loc)
	if (filter.From == nil) != (filter.To == nil) {
		return plan, apperrors.ErrBadRequest
	}
	if filter.From != nil {
		if filter.Period != "" {
			return plan, apperrors.ErrBadRequest
		}
		plan.current = usageRange{filter.From.In(loc), filter.To.In(loc)}
	} else {
		plan.current.to = now
		switch filter.Period {
		case "today":
			plan.current.from = usageCalendarStart(loc, now.Year(), now.Month(), now.Day())
		case "24h":
			plan.current.from = now.Add(-24 * time.Hour)
		case "7d":
			plan.current.from = now.Add(-7 * 24 * time.Hour)
		case "90d":
			plan.current.from = now.Add(-90 * 24 * time.Hour)
		case "year":
			plan.current.from = usageCalendarStart(loc, now.Year(), time.January, 1)
		case "", "month":
			plan.current.from = usageCalendarStart(loc, now.Year(), now.Month(), 1)
		default:
			return plan, apperrors.ErrBadRequest
		}
	}
	span := plan.current.to.Sub(plan.current.from)
	if span <= 0 || span > 366*24*time.Hour || plan.current.from.Year() < 1 || plan.current.to.Year() > 9999 {
		return plan, apperrors.ErrBadRequest
	}
	plan.grain = filter.Granularity
	if plan.grain == "" || plan.grain == "auto" {
		switch {
		case span <= 48*time.Hour:
			plan.grain = "hour"
		case span <= 90*24*time.Hour:
			plan.grain = "day"
		default:
			plan.grain = "week"
		}
	}
	if plan.grain != "hour" && plan.grain != "day" && plan.grain != "week" && plan.grain != "month" {
		return plan, apperrors.ErrBadRequest
	}
	if filter.Compare {
		previous := usageRange{plan.current.from.Add(-span), plan.current.from}
		if previous.from.Year() < 1 {
			return plan, apperrors.ErrBadRequest
		}
		plan.previous = &previous
	}
	for _, period := range []*usageRange{&plan.current, plan.previous} {
		if period == nil {
			continue
		}
		if _, err := usageBuckets(*period, loc, plan.grain); err != nil {
			return plan, err
		}
	}
	return plan, nil
}

// Locate the first instant of the local date. Searching the calendar boundary
// handles zones whose DST transition skips midnight, as well as repeated hours.
func usageCalendarStart(loc *time.Location, year int, month time.Month, day int) time.Time {
	target := time.Date(year, month, day, 12, 0, 0, 0, time.UTC)
	year, month, day = target.Date()
	approximate := time.Date(year, month, day, 0, 0, 0, 0, loc)
	low, high := approximate.Add(-48*time.Hour).Unix(), approximate.Add(48*time.Hour).Unix()
	dateNumber := func(value time.Time) int { y, m, d := value.In(loc).Date(); return y*10000 + int(m)*100 + d }
	wanted := year*10000 + int(month)*100 + day
	for low < high {
		mid := low + (high-low)/2
		if dateNumber(time.Unix(mid, 0)) < wanted {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return time.Unix(low, 0).In(loc)
}
func usageBucketStart(value time.Time, loc *time.Location, grain string) time.Time {
	local := value.In(loc)
	if grain == "hour" {
		return local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
	}
	year, month, day := local.Date()
	if grain == "week" {
		delta := (int(local.Weekday()) + 6) % 7
		day -= delta
	}
	if grain == "month" {
		day = 1
	}
	return usageCalendarStart(loc, year, month, day)
}
func nextUsageBucket(start time.Time, loc *time.Location, grain string) time.Time {
	if grain == "hour" {
		return start.Add(time.Hour)
	}
	year, month, day := start.In(loc).Date()
	switch grain {
	case "day":
		day++
	case "week":
		day += 7
	case "month":
		month++
		day = 1
	}
	return usageCalendarStart(loc, year, month, day)
}
func usageBuckets(period usageRange, loc *time.Location, grain string) ([]UsageBucket, error) {
	result := []UsageBucket{}
	for start := usageBucketStart(period.from, loc, grain); start.Before(period.to); {
		if len(result) >= usageBucketLimit {
			return nil, usageTooLarge
		}
		end := nextUsageBucket(start, loc, grain)
		if !end.After(start) {
			return nil, apperrors.ErrBadRequest
		}
		result = append(result, UsageBucket{Start: start, End: end})
		start = end
	}
	return result, nil
}
