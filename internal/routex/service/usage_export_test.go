package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func usageExportZeroStats() UsageStats { var accumulator usageAccumulator; return accumulator.result() }

func usageExportFixture() *UsageReport {
	now := time.Date(2026, 10, 4, 8, 9, 10, 123456789, time.UTC)
	zero := usageExportZeroStats()
	known := "18014398509481986"
	rate, duration := 0.5, 0.0
	stats := UsageStats{Requests: 2, Successes: 1, Errors: 1, SuccessRate: &rate, AverageDurationMS: &duration,
		Tokens:  UsageTokens{Input: UsageCount{Value: &known, Known: known}, Output: UsageCount{Known: "0", UnknownCalls: 1}, Total: UsageCount{Known: known, UnknownCalls: 1}},
		Amounts: []UsageAmount{{Currency: "USD", Amount: "1.000000000000000001", Calls: 1}, {Currency: "EUR", Amount: "0", Calls: 1}}, PricingStatuses: map[string]int64{"unpriced": 1, "priced": 1}, UnknownAmountCalls: 1}
	from := now.Add(-24 * time.Hour)
	current := UsagePeriod{From: from, To: now, Summary: stats,
		Trend:  []UsageBucket{{Start: from, End: now, Stats: stats}},
		Models: []UsageGroup{{ID: "mdl_exact", Name: "  =SUM(1,2)\t\n'original", Stats: stats}},
		Keys:   []UsageGroup{{ID: "", Unknown: true, Stats: zero}}}
	return &UsageReport{Timezone: "UTC", Granularity: "day", QueriedAt: now, LatestCompletedAt: &now, Source: "persisted_call_records", MayLag: true, Current: current,
		Previous: &UsagePeriod{From: from.Add(-24 * time.Hour), To: from, Summary: zero, Trend: []UsageBucket{{Start: from.Add(-24 * time.Hour), End: from, Stats: zero}}}, AvailableDimensions: []string{"model", "key"}}
}
func usageExportRows(t *testing.T, data []byte) []map[string]string {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	const frozenHeader = "row_type,schema_version,text_encoding,scope,scope_id,queried_at,source,may_lag,latest_selected_completed_at,timezone,granularity,available_dimensions,filter_period,filter_from,filter_to,filter_granularity,filter_timezone,filter_compare,filter_model_id,filter_key_id,filter_status,filter_protocol,filter_stream,filter_user_id,filter_project_id,filter_team_id,filter_provider_id,filter_provider_model_id,filter_connection_id,period,period_from,period_to,section,dimension,entity_id,entity_name,unknown_identity,bucket_start,bucket_end,requests,successes,errors,canceled,success_rate,average_duration_ms,input_value,input_known,input_unknown_calls,output_value,output_known,output_unknown_calls,total_value,total_known,total_unknown_calls,unknown_amount_calls,currency,amount,amount_calls,pricing_status,pricing_status_calls"
	if !reflect.DeepEqual(records[0], strings.Split(frozenHeader, ",")) {
		t.Fatal("schema changed", records[0])
	}
	rows := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != len(usageCSVColumns) {
			t.Fatal("nonrectangular row")
		}
		row := map[string]string{}
		for i, column := range usageCSVColumns {
			row[column] = record[i]
		}
		rows = append(rows, row)
	}
	return rows
}
func TestUsageExportExactRectangularSchemaAndStableChildren(t *testing.T) {
	report := usageExportFixture()
	from, to := report.Current.From, report.Current.To
	stream := false
	filter := UsageFilter{From: &from, To: &to, Timezone: "UTC", Granularity: "day", Compare: true, Stream: &stream, ModelID: "mdl_exact"}
	encoded, err := encodeUsageCSV(context.Background(), "personal", "usr_self", filter, report)
	if err != nil {
		t.Fatal(err)
	}
	again, err := encodeUsageCSV(context.Background(), "personal", "usr_self", filter, report)
	if err != nil || !bytes.Equal(encoded, again) {
		t.Fatal("encoding is nondeterministic", err)
	}
	rows := usageExportRows(t, encoded)
	meta := rows[0]
	if meta["row_type"] != "metadata" || meta["schema_version"] != "routex_usage_v1" || meta["text_encoding"] != "apostrophe_text_v1" || meta["scope_id"] != "'usr_self" || meta["queried_at"] != "'2026-10-04T08:09:10.123456789Z" || meta["latest_selected_completed_at"] != meta["queried_at"] || meta["filter_stream"] != "false" || meta["filter_from"] != usageCSVTime(from) || meta["filter_to"] != usageCSVTime(to) || meta["filter_compare"] != "true" || meta["filter_model_id"] != "'mdl_exact" {
		t.Fatal("metadata lost exact capture/filter", meta)
	}
	first := rows[1]
	if first["row_type"] != "stats" || first["section"] != "summary" || first["input_value"] != "'18014398509481986" || first["output_value"] != "" || first["output_known"] != "'0" || first["output_unknown_calls"] != "1" || first["total_known"] != "'18014398509481986" || first["total_value"] != "" || first["requests"] != "2" || first["average_duration_ms"] != "0" {
		t.Fatal("precision or null/zero/coverage lost", first)
	}
	if rows[2]["currency"] != "'EUR" || rows[2]["amount"] != "'0" || rows[3]["currency"] != "'USD" || rows[3]["amount"] != "'1.000000000000000001" || rows[4]["pricing_status"] != "'priced" || rows[5]["pricing_status"] != "'unpriced" {
		t.Fatal("child ordering or exact decimals lost", rows[2:6])
	}
	currentFinished := false
	sawUnknown, sawUnsafe := false, false
	for _, row := range rows[1:] {
		if row["period"] == "previous" {
			currentFinished = true
		} else if currentFinished {
			t.Fatal("current returned after previous")
		}
		if row["row_type"] == "amount" || row["row_type"] == "pricing_status" {
			for _, field := range []string{"requests", "successes", "errors", "canceled", "input_value", "input_known", "input_unknown_calls", "output_known", "total_known", "success_rate", "unknown_amount_calls"} {
				if row[field] != "" {
					t.Fatal("child duplicated facts", field, row)
				}
			}
			if row["period_from"] == "" || row["period_to"] == "" || row["section"] == "" {
				t.Fatal("child missing parent locator", row)
			}
		}
		if row["dimension"] == "model" {
			sawUnsafe = true
			if strings.TrimPrefix(row["entity_name"], "'") != report.Current.Models[0].Name {
				t.Fatal("name not reversible", row)
			}
		}
		if row["dimension"] == "key" {
			sawUnknown = true
			if row["entity_id"] != "'" || row["unknown_identity"] != "true" {
				t.Fatal("unknown identity fabricated", row)
			}
		}
	}
	if !sawUnknown || !sawUnsafe {
		t.Fatal("authorized groups omitted")
	}
}
func TestUsageExportTextEncodingAlwaysAddsExactlyOnePrefix(t *testing.T) {
	for _, value := range []string{"", "0", "9007199254740993", "0.123456789012345678", "=1+1", " +1", "\t@command", "\r-1", "\n=1", "'already", "''twice", "safe,quoted\nline", "https://example.test"} {
		if encoded := usageCSVText(value); encoded != "'"+value || strings.TrimPrefix(encoded, "'") != value {
			t.Fatal("text not reversible", value, encoded)
		}
	}
}
func TestUsageExportScopeCannotExposeUnexpectedGroups(t *testing.T) {
	for _, scope := range []string{"personal", "project", "team", "admin"} {
		report := usageExportFixture()
		target := "resource_self"
		filter := UsageFilter{Compare: true}
		if scope == "team" {
			report.TeamID = target
			report.Current.Keys = nil
		}
		if scope == "admin" {
			target = ""
			filter.TeamID = "tea_exact"
			report.Current.Keys = nil
		}
		report.AvailableDimensions = usageDimensions(scope, filter)
		data, err := encodeUsageCSV(context.Background(), scope, target, filter, report)
		if err != nil {
			t.Fatal(scope, err)
		}
		rows := usageExportRows(t, data)
		for _, row := range rows {
			if (scope == "team" || scope == "admin") && row["dimension"] == "key" {
				t.Fatal("Team attribution exposed Keys")
			}
		}
		private := "provider"
		if scope == "admin" {
			private = "key"
		}
		if private == "key" {
			report.Current.Keys = []UsageGroup{{ID: "key_private", Name: "private", Stats: usageExportZeroStats()}}
		} else {
			report.Current.Providers = []UsageGroup{{ID: "prv_private", Name: "private", Stats: usageExportZeroStats()}}
		}
		data, err = encodeUsageCSV(context.Background(), scope, target, filter, report)
		if !errors.Is(err, apperrors.ErrInternal) || data != nil {
			t.Fatal("private mismatch silently exported", scope, err)
		}
	}
	report := usageExportFixture()
	report.AvailableDimensions = append(report.AvailableDimensions, "user")
	if data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, report); err == nil || data != nil {
		t.Fatal("unexpected available dimension accepted")
	}
	report = usageExportFixture()
	report.TeamID = "tea_foreign"
	if data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, report); err == nil || data != nil {
		t.Fatal("foreign report context accepted")
	}
}
func TestUsageExportEmptyReportRetainsSummaryBucketsAndNulls(t *testing.T) {
	report := usageExportFixture()
	zero := usageExportZeroStats()
	report.Previous = nil
	report.LatestCompletedAt = nil
	report.Current.Summary = zero
	report.Current.Trend[0].Stats = zero
	report.Current.Models = nil
	report.Current.Keys = nil
	data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{}, report)
	if err != nil {
		t.Fatal(err)
	}
	rows := usageExportRows(t, data)
	if len(rows) != 3 || rows[0]["latest_selected_completed_at"] != "" || rows[1]["requests"] != "0" || rows[1]["input_value"] != "'0" || rows[1]["success_rate"] != "" || rows[2]["section"] != "trend" {
		t.Fatal("empty report omitted/fabricated facts", rows)
	}
}
func TestUsageExportOneCaptureDeadlineAndFailureHaveNoFile(t *testing.T) {
	calls := 0
	report := usageExportFixture()
	result, err := exportUsageCapture(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, func(ctx context.Context) (*UsageReport, error) {
		calls++
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 0 || remaining > 5*time.Second {
			t.Fatal("missing total deadline")
		}
		return report, nil
	})
	if err != nil || result == nil || calls != 1 {
		t.Fatal("capture count/result", calls, err)
	}
	for _, failure := range []error{apperrors.ErrForbidden, usageTooLarge, context.Canceled, context.DeadlineExceeded} {
		result, err := exportUsageCapture(context.Background(), "personal", "usr_self", UsageFilter{}, func(context.Context) (*UsageReport, error) { return nil, failure })
		expected := failure
		if errors.Is(failure, context.Canceled) || errors.Is(failure, context.DeadlineExceeded) {
			expected = usageExportUnavailable
		}
		if result != nil || !errors.Is(err, expected) {
			t.Fatal("capture failure produced a file", failure, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls = 0
	result, err = exportUsageCapture(ctx, "personal", "usr_self", UsageFilter{}, func(context.Context) (*UsageReport, error) { calls++; return report, nil })
	if calls != 0 || result != nil || !errors.Is(err, usageExportUnavailable) {
		t.Fatal("canceled capture ran")
	}
	ctx, cancel = context.WithCancel(context.Background())
	result, err = exportUsageCapture(ctx, "personal", "usr_self", UsageFilter{Compare: true}, func(context.Context) (*UsageReport, error) { cancel(); return report, nil })
	if result != nil || !errors.Is(err, usageExportUnavailable) {
		t.Fatal("late capture exported after cancellation")
	}
}
func TestUsageExportByteAndReportBoundsReturnNoPartialFile(t *testing.T) {
	report := usageExportFixture()
	report.Current.Models[0].Name = strings.Repeat("=", usageExportBytes)
	data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, report)
	if data != nil || !errors.Is(err, usageExportTooLarge) {
		t.Fatal("byte ceiling returned partial CSV", err)
	}
	for _, mutate := range []func(*UsageReport){
		func(r *UsageReport) { r.Current.Trend = make([]UsageBucket, usageBucketLimit+1) },
		func(r *UsageReport) { r.Current.Models = make([]UsageGroup, usageGroupLimit+1) },
		func(r *UsageReport) { r.Current.Summary.Requests = usageRowLimit + 1 },
	} {
		report = usageExportFixture()
		mutate(report)
		data, err = encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, report)
		if data != nil || !errors.Is(err, usageExportTooLarge) {
			t.Fatal("report bound bypassed", err)
		}
	}
	buffer := usageCSVBuffer{ctx: context.Background()}
	if _, err := buffer.Write(make([]byte, usageExportBytes)); err != nil {
		t.Fatal(err)
	}
	if n, err := buffer.Write([]byte("x")); n != 0 || !errors.Is(err, usageExportTooLarge) || buffer.Len() != usageExportBytes {
		t.Fatal("byte writer overshot ceiling", n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	buffer.ctx = ctx
	if _, err := buffer.Write(nil); !errors.Is(err, usageExportUnavailable) {
		t.Fatal("writer ignored cancellation")
	}
}
func TestUsageExportInvalidScopeAndComparisonRejected(t *testing.T) {
	report := usageExportFixture()
	for _, test := range []struct {
		scope, target string
		filter        UsageFilter
		status        int
	}{
		{"personal", "usr_self", UsageFilter{UserID: "usr_other", Compare: true}, http.StatusBadRequest},
		{"team", "tea_exact", UsageFilter{KeyID: "key_private", Compare: true}, http.StatusBadRequest},
		{"admin", "", UsageFilter{TeamID: "tea_exact", KeyID: "key_private", Compare: true}, http.StatusBadRequest},
	} {
		_, err := encodeUsageCSV(context.Background(), test.scope, test.target, test.filter, report)
		var api *apperrors.Error
		if !errors.As(err, &api) || api.Code != test.status {
			t.Fatal("invalid scope filter accepted", err)
		}
	}
	if data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{}, report); data != nil || err == nil {
		t.Fatal("comparison mismatch accepted")
	}
}

// Cancellation becomes observable after output has begun, without a timer race.
type usageExportCanceledDuringEncoding struct {
	context.Context
	checks int
}

func (ctx *usageExportCanceledDuringEncoding) Err() error {
	ctx.checks++
	if ctx.checks >= 8 {
		return context.Canceled
	}
	return nil
}
func TestUsageExportCancellationDuringEncodingDiscardsCompleteBuffer(t *testing.T) {
	ctx := &usageExportCanceledDuringEncoding{Context: context.Background()}
	data, err := encodeUsageCSV(ctx, "personal", "usr_self", UsageFilter{Compare: true}, usageExportFixture())
	if data != nil || !errors.Is(err, usageExportUnavailable) || ctx.checks < 8 {
		t.Fatal("encoding ignored midstream cancellation", err, ctx.checks)
	}
}

func TestUsageExportUnsafeTextRoundTripsThroughCSV(t *testing.T) {
	for _, name := range []string{"=1+1", "+SUM(1,2)", "-1", "@name", "   =1", "\t=1", "\r=1", "\n=1", "'already", "''twice", "comma,quote\"and\nline", "plain name"} {
		report := usageExportFixture()
		report.Current.Models[0].Name = name
		data, err := encodeUsageCSV(context.Background(), "personal", "usr_self", UsageFilter{Compare: true}, report)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range usageExportRows(t, data) {
			if row["dimension"] == "model" && row["entity_name"] != "'"+name {
				t.Fatal("text altered or formula prefix missing", name, row["entity_name"])
			}
		}
	}
}
func TestUsageExportAllAuthorizedPlatformDistributionsAndFilters(t *testing.T) {
	report := usageExportFixture()
	report.AvailableDimensions = usageDimensions("admin", UsageFilter{})
	stats := report.Current.Summary
	report.Current.Providers = []UsageGroup{{ID: "prv_exact", Name: "provider", Stats: stats}}
	report.Current.ProviderModels = []UsageGroup{{ID: "pmd_exact", Name: "upstream", Stats: stats}}
	report.Current.Connections = []UsageGroup{{ID: "con_exact", Name: "connection", Stats: stats}}
	filter := UsageFilter{Compare: true, UserID: "usr_personal", ModelID: "mdl_exact", KeyID: "key_exact", ProviderID: "prv_exact", ProviderModelID: "pmd_exact", ConnectionID: "con_exact"}
	data, err := encodeUsageCSV(context.Background(), "admin", "", filter, report)
	if err != nil {
		t.Fatal(err)
	}
	rows := usageExportRows(t, data)
	if rows[0]["scope"] != "platform" || rows[0]["scope_id"] != "" || rows[0]["filter_user_id"] != "'usr_personal" || rows[0]["filter_provider_model_id"] != "'pmd_exact" {
		t.Fatal("platform metadata changed", rows[0])
	}
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		if row["dimension"] != "" {
			seen[row["dimension"]] = true
		}
	}
	for _, dimension := range report.AvailableDimensions {
		if !seen[dimension] {
			t.Fatal("authorized distribution omitted", dimension)
		}
	}
}
func TestUsageExportServicesInheritExistingFilterAndCalendarRejections(t *testing.T) {
	s := &Service{}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(367 * 24 * time.Hour)
	calls := []func(UsageFilter) (*UsageCSVExport, error){
		func(f UsageFilter) (*UsageCSVExport, error) {
			return s.ExportPersonalUsage(context.Background(), "usr_self", f)
		},
		func(f UsageFilter) (*UsageCSVExport, error) {
			return s.ExportProjectUsage(context.Background(), "usr_self", "prj_exact", f)
		},
		func(f UsageFilter) (*UsageCSVExport, error) {
			return s.ExportTeamUsage(context.Background(), "usr_self", "tea_exact", f)
		},
		func(f UsageFilter) (*UsageCSVExport, error) {
			return s.ExportAdminUsage(context.Background(), "usr_self", f)
		},
	}
	for _, call := range calls {
		for _, filter := range []UsageFilter{{Period: "invalid"}, {Timezone: "Missing/Location"}, {Granularity: "minute"}, {From: &from, To: &to}, {Period: "7d", From: &from, To: &to}, {ModelID: "unsafe/alias"}} {
			result, err := call(filter)
			if result != nil || err == nil {
				t.Fatal("existing report validation bypassed", filter, err)
			}
		}
	}
}
