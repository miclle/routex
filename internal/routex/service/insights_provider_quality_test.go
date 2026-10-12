package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestInsightsProviderQualityImmutableAttempts(t *testing.T) {
	now := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	duration := int64(120)
	// Provider IDs remain exact even when names match. Unknown attribution must
	// never borrow a final Call's Provider or contribute to the success rate.
	rows := []entity.CallAttempt{
		{ID: "a", ProviderID: "prv_A", ProviderName: "Old", Status: "success", WorkEvidence: "completed", DurationMS: &duration, CompletedAt: now},
		{ID: "b", ProviderID: "prv_A", ProviderName: "=historical", Status: "error", WorkEvidence: "rejected_without_work", FailureClass: "rate_limited", HTTPStatus: 429, CompletedAt: now},
		{ID: "c", ProviderID: "prv_A", Status: "canceled", HTTPStatus: 503, CompletedAt: now.Add(-time.Second)},
		{ID: "d", ProviderID: "prv_A", WorkEvidence: "not_sent", FailureClass: "credential_rejected", CompletedAt: now.Add(-time.Second)},
		{ID: "e", ProviderID: "prv_A", FailureClass: "credential_rejected", WorkEvidence: "rejected_without_work", CompletedAt: now.Add(-time.Second)},
		{ID: "f", ProviderID: "prv_a", ProviderName: "=historical", Status: "error", HTTPStatus: 503, CompletedAt: now},
		{ID: "g", Status: "success", DurationMS: &duration, CompletedAt: now},
	}
	input := InsightsProviderQualityInput{From: now.Add(-time.Hour).Format(time.RFC3339Nano), To: now.Add(time.Second).Format(time.RFC3339Nano), Timezone: "UTC"}
	result, err := aggregateInsightsProviderQuality(context.Background(), rows, input, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalAttempts != 7 || result.UnknownAttribution != 1 || len(result.Providers) != 2 || result.Summary.Attempts != 6 || result.Summary.Eligible != 3 || result.Summary.Successes != 1 || result.Summary.Excluded != 2 || result.Summary.CredentialRejected != 1 || *result.Summary.SuccessRateBPS != 3333 || result.Summary.RateLimited != 1 || result.Summary.ServerErrors != 1 || result.Summary.KnownDuration != 1 || result.Summary.UnknownDuration != 2 || *result.Summary.P95DurationMS != 120 {
		t.Fatalf("incorrect aggregate: %+v", result)
	}
	if result.Providers[0].ProviderID != "prv_A" || result.Providers[0].Name != "=historical" || *result.Providers[0].SuccessRateBPS != 5000 {
		t.Fatalf("attribution/name tie-break changed: %+v", result.Providers)
	}
	// Source ordering does not choose a different historical label or percentile.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	again, err := aggregateInsightsProviderQuality(context.Background(), rows, input, now)
	if err != nil || !reflect.DeepEqual(result, again) {
		t.Fatal("order-dependent aggregate", err)
	}
	data, err := encodeInsightsProviderQualityCSV(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(records) != 4 || records[2][10] != "'=historical" {
		t.Fatal("CSV capture/formula escape", records, err)
	}
	if strings.Contains(string(data), "credential_id") || strings.Contains(string(data), "request_id") {
		t.Fatal("private attribution exported")
	}
}
func TestInsightsProviderQualityCompleteBoundsAndUnknown(t *testing.T) {
	now := time.Now().UTC()
	input := InsightsProviderQualityInput{From: now.Add(-time.Hour).Format(time.RFC3339Nano), To: now.Format(time.RFC3339Nano), Timezone: "UTC"}
	unknown, err := aggregateInsightsProviderQuality(context.Background(), []entity.CallAttempt{{CompletedAt: now}}, input, now)
	if err != nil || unknown.UnknownAttribution != 1 || unknown.Summary.SuccessRateBPS != nil || unknown.Summary.P95DurationMS != nil || len(unknown.Providers) != 0 {
		t.Fatal("unknown became zero/attributed", err)
	}
	empty, err := aggregateInsightsProviderQuality(context.Background(), nil, input, now)
	if err != nil || empty.LatestCompletedAt != nil || empty.Providers == nil {
		t.Fatal("empty capture", err)
	}
	if _, err := aggregateInsightsProviderQuality(context.Background(), make([]entity.CallAttempt, providerQualityMaxAttempts+1), input, now); err != providerQualityTooLarge {
		t.Fatal("truncated attempts", err)
	}
	rows := make([]entity.CallAttempt, usageGroupLimit+1)
	for i := range rows {
		rows[i].ProviderID = fmt.Sprintf("prv_%03d", i)
	}
	if _, err := aggregateInsightsProviderQuality(context.Background(), rows, input, now); err != providerQualityTooLarge {
		t.Fatal("truncated groups", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := aggregateInsightsProviderQuality(ctx, rows[:1], input, now); err != usageExportUnavailable {
		t.Fatal("cancellation ignored", err)
	}
	if _, err := encodeInsightsProviderQualityCSV(ctx, empty); err != usageExportUnavailable {
		t.Fatal("CSV cancellation ignored", err)
	}
}
func TestInsightsProviderQualityExactRange(t *testing.T) {
	now := time.Now().UTC()
	valid := InsightsProviderQualityInput{From: "2026-10-01T08:00:00+08:00", To: "2026-10-02T00:00:00Z", Timezone: "Asia/Shanghai"}
	plan, err := insightsProviderQualityRange(valid, now)
	if err != nil || plan.current.from.UTC().Format(time.RFC3339) != "2026-10-01T00:00:00Z" {
		t.Fatal("offset/range", err)
	}
	future := InsightsProviderQualityInput{From: "2090-01-01T08:00:00.123456789+08:00", To: "2090-01-01T00:00:00.123456790Z", Timezone: "UTC"}
	futurePlan, err := insightsProviderQualityRange(future, now)
	if err != nil || futurePlan.current.to.Sub(futurePlan.current.from) != time.Nanosecond || futurePlan.current.from.UTC().Format(time.RFC3339Nano) != "2090-01-01T00:00:00.123456789Z" {
		t.Fatal("future bounds clamped or rounded", err)
	}
	for _, input := range []InsightsProviderQualityInput{
		{From: "2026-10-01T00:00:00+24:00", To: valid.To, Timezone: "UTC"},
		{From: "2026-10-01T00:00:00,1Z", To: valid.To, Timezone: "UTC"},
		{From: "2026-10-01", To: valid.To, Timezone: "UTC"},
		{From: valid.To, To: valid.From, Timezone: "UTC"},
		{From: valid.From, To: "2028-10-01T00:00:00Z", Timezone: "UTC"},
		{From: valid.From, To: valid.To, Timezone: "Local"},
		{From: valid.From, To: valid.To, Timezone: ""},
	} {
		if _, err := insightsProviderQualityRange(input, now); err == nil {
			t.Fatalf("invalid range accepted: %+v", input)
		}
	}
}

func TestInsightsProviderQualityP95UsesKnownEligibleFullAttempts(t *testing.T) {
	rows := make([]entity.CallAttempt, 0, 22)
	for i := int64(1); i <= 20; i++ {
		duration := i
		rows = append(rows, entity.CallAttempt{ProviderID: "prv_quality", Status: "success", DurationMS: &duration, NativeCompletionEvidence: "handoff"})
	}
	huge := int64(999999)
	rows = append(rows, entity.CallAttempt{ProviderID: "prv_quality", Status: "canceled", DurationMS: &huge}, entity.CallAttempt{ProviderID: "prv_quality", FailureClass: "credential_rejected", DurationMS: &huge})
	result, err := aggregateInsightsProviderQuality(context.Background(), rows, InsightsProviderQualityInput{}, time.Now())
	if err != nil || result.Summary.Eligible != 20 || result.Summary.Excluded != 1 || result.Summary.CredentialRejected != 1 || result.Summary.KnownDuration != 20 || *result.Summary.P95DurationMS != 19 || *result.Summary.SuccessRateBPS != 10000 {
		t.Fatal("eligible full-attempt percentile/classification", result, err)
	}
	// Successful attempt status does not establish native completed evidence;
	// the report contains no retirement/completion claim for these handoff rows.
}

func TestInsightsProviderQualitySubMicrosecondStoredBoundaries(t *testing.T) {
	stored := time.Date(2026, 10, 12, 0, 0, 0, 123456000, time.UTC)
	for _, delta := range []time.Duration{0, time.Nanosecond, 999 * time.Nanosecond, time.Microsecond} {
		from, to := stored.Add(delta), stored.Add(delta+time.Nanosecond)
		lower, upper, lowerPredicate, upperPredicate := insightsProviderQualityBounds(from, to)
		for _, row := range []time.Time{stored.Add(-time.Microsecond), stored, stored.Add(time.Microsecond)} {
			lowerMatch := !row.Before(lower)
			if lowerPredicate == "completed_at > ?" {
				lowerMatch = row.After(lower)
			}
			upperMatch := row.Before(upper)
			if upperPredicate == "completed_at <= ?" {
				upperMatch = !row.After(upper)
			}
			if (lowerMatch && upperMatch) != (!row.Before(from) && row.Before(to)) {
				t.Fatal("stored boundary mismatch", delta, row)
			}
		}
	}
}
