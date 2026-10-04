package service

import (
	"bytes"
	"context"
	"encoding/csv"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// v1 is rectangular: metadata appears once, and child amount/pricing rows carry
// only their parent locator and their own facts. Blank cells mean absent/null.
var usageCSVColumns = []string{
	"row_type", "schema_version", "text_encoding", "scope", "scope_id", "queried_at", "source", "may_lag", "latest_selected_completed_at", "timezone", "granularity", "available_dimensions",
	"filter_period", "filter_from", "filter_to", "filter_granularity", "filter_timezone", "filter_compare", "filter_model_id", "filter_key_id", "filter_status", "filter_protocol", "filter_stream", "filter_user_id", "filter_project_id", "filter_team_id", "filter_provider_id", "filter_provider_model_id", "filter_connection_id",
	"period", "period_from", "period_to", "section", "dimension", "entity_id", "entity_name", "unknown_identity", "bucket_start", "bucket_end",
	"requests", "successes", "errors", "canceled", "success_rate", "average_duration_ms", "input_value", "input_known", "input_unknown_calls", "output_value", "output_known", "output_unknown_calls", "total_value", "total_known", "total_unknown_calls", "unknown_amount_calls", "currency", "amount", "amount_calls", "pricing_status", "pricing_status_calls",
}

// Every exact or untrusted text value gets exactly one reversible apostrophe.
// This encoding does not promise automatic type handling by spreadsheet software.
func usageCSVText(value string) string    { return "'" + value }
func usageCSVTime(value time.Time) string { return usageCSVText(value.UTC().Format(time.RFC3339Nano)) }
func usageCSVOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return usageCSVTime(*value)
}
func usageCSVOptionalText(value string) string {
	if value == "" {
		return ""
	}
	return usageCSVText(value)
}
func usageCSVOptionalBool(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}
func usageCSVFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'g', -1, 64)
}

type usageCSVBuffer struct {
	bytes.Buffer
	ctx context.Context
}

func (b *usageCSVBuffer) Write(data []byte) (int, error) {
	if b.ctx.Err() != nil {
		return 0, usageExportUnavailable
	}
	if len(data) > usageExportBytes-b.Len() {
		return 0, usageExportTooLarge
	}
	return b.Buffer.Write(data)
}

type usageCSVEncoder struct {
	ctx     context.Context
	writer  *csv.Writer
	columns map[string]int
}

func (e *usageCSVEncoder) row() []string                          { return make([]string, len(usageCSVColumns)) }
func (e *usageCSVEncoder) put(row []string, column, value string) { row[e.columns[column]] = value }
func (e *usageCSVEncoder) write(row []string) error {
	if e.ctx.Err() != nil {
		return usageExportUnavailable
	}
	if err := e.writer.Write(row); err != nil {
		return err
	}
	e.writer.Flush()
	return e.writer.Error()
}
func usageCSVGroups(period *UsagePeriod) map[string][]UsageGroup {
	return map[string][]UsageGroup{"model": period.Models, "key": period.Keys, "provider": period.Providers, "provider_model": period.ProviderModels, "connection": period.Connections}
}
func validateUsageCSVReport(scope, targetID string, filter UsageFilter, report *UsageReport) error {
	if scope != "personal" && scope != "project" && scope != "team" && scope != "admin" {
		return apperrors.ErrInternal
	}
	if err := validateUsageFilter(filter, scope == "admin"); err != nil {
		return err
	}
	if scope == "team" && filter.KeyID != "" {
		return apperrors.ErrBadRequest
	}
	if report == nil || (scope == "team" && report.TeamID != targetID) || (scope != "team" && report.TeamID != "") || filter.Compare != (report.Previous != nil) {
		return apperrors.ErrInternal
	}
	allowed := usageDimensions(scope, filter)
	if !slices.Equal(report.AvailableDimensions, allowed) {
		return apperrors.ErrInternal
	}
	periods := []*UsagePeriod{&report.Current}
	if report.Previous != nil {
		periods = append(periods, report.Previous)
	}
	var requests int64
	for _, period := range periods {
		if !period.From.Before(period.To) {
			return apperrors.ErrInternal
		}
		requests += period.Summary.Requests
		if len(period.Trend) > usageBucketLimit {
			return usageExportTooLarge
		}
		for dimension, groups := range usageCSVGroups(period) {
			if len(groups) != 0 && !slices.Contains(allowed, dimension) {
				return apperrors.ErrInternal
			}
			if len(groups) > usageGroupLimit {
				return usageExportTooLarge
			}
		}
	}
	if requests > usageRowLimit {
		return usageExportTooLarge
	}
	return nil
}
func encodeUsageCSV(ctx context.Context, scope, targetID string, filter UsageFilter, report *UsageReport) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	if err := validateUsageCSVReport(scope, targetID, filter, report); err != nil {
		return nil, err
	}
	buffer := &usageCSVBuffer{ctx: ctx}
	e := usageCSVEncoder{ctx: ctx, writer: csv.NewWriter(buffer), columns: make(map[string]int, len(usageCSVColumns))}
	for index, column := range usageCSVColumns {
		e.columns[column] = index
	}
	if err := e.write(usageCSVColumns); err != nil {
		return nil, err
	}
	metadata := e.row()
	values := map[string]string{
		"row_type": "metadata", "schema_version": "routex_usage_v1", "text_encoding": "apostrophe_text_v1", "scope": scope,
		"scope_id": usageCSVOptionalText(targetID), "queried_at": usageCSVTime(report.QueriedAt), "source": usageCSVText(report.Source), "may_lag": strconv.FormatBool(report.MayLag), "latest_selected_completed_at": usageCSVOptionalTime(report.LatestCompletedAt),
		"timezone": usageCSVText(report.Timezone), "granularity": usageCSVText(report.Granularity), "available_dimensions": usageCSVText(strings.Join(report.AvailableDimensions, ",")),
		"filter_period": usageCSVOptionalText(filter.Period), "filter_from": usageCSVOptionalTime(filter.From), "filter_to": usageCSVOptionalTime(filter.To), "filter_granularity": usageCSVOptionalText(filter.Granularity), "filter_timezone": usageCSVOptionalText(filter.Timezone), "filter_compare": strconv.FormatBool(filter.Compare),
		"filter_model_id": usageCSVOptionalText(filter.ModelID), "filter_key_id": usageCSVOptionalText(filter.KeyID), "filter_status": usageCSVOptionalText(filter.Status), "filter_protocol": usageCSVOptionalText(filter.Protocol), "filter_stream": usageCSVOptionalBool(filter.Stream), "filter_user_id": usageCSVOptionalText(filter.UserID), "filter_project_id": usageCSVOptionalText(filter.ProjectID), "filter_team_id": usageCSVOptionalText(filter.TeamID), "filter_provider_id": usageCSVOptionalText(filter.ProviderID), "filter_provider_model_id": usageCSVOptionalText(filter.ProviderModelID), "filter_connection_id": usageCSVOptionalText(filter.ConnectionID),
	}
	if scope == "admin" {
		values["scope"] = "platform"
	}
	for column, value := range values {
		e.put(metadata, column, value)
	}
	if err := e.write(metadata); err != nil {
		return nil, err
	}
	if err := e.period("current", &report.Current, report.AvailableDimensions); err != nil {
		return nil, err
	}
	if report.Previous != nil {
		if err := e.period("previous", report.Previous, report.AvailableDimensions); err != nil {
			return nil, err
		}
	}
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	return bytes.Clone(buffer.Bytes()), nil
}
func (e *usageCSVEncoder) period(name string, period *UsagePeriod, dimensions []string) error {
	base := e.row()
	for column, value := range map[string]string{"period": name, "period_from": usageCSVTime(period.From), "period_to": usageCSVTime(period.To), "section": "summary"} {
		e.put(base, column, value)
	}
	if err := e.stats(base, period.Summary); err != nil {
		return err
	}
	for _, bucket := range period.Trend {
		row := slices.Clone(base)
		e.put(row, "section", "trend")
		e.put(row, "bucket_start", usageCSVTime(bucket.Start))
		e.put(row, "bucket_end", usageCSVTime(bucket.End))
		if err := e.stats(row, bucket.Stats); err != nil {
			return err
		}
	}
	groups := usageCSVGroups(period)
	for _, dimension := range dimensions {
		for _, group := range groups[dimension] {
			row := slices.Clone(base)
			for column, value := range map[string]string{"section": "group", "dimension": dimension, "entity_id": usageCSVText(group.ID), "entity_name": usageCSVText(group.Name), "unknown_identity": strconv.FormatBool(group.Unknown)} {
				e.put(row, column, value)
			}
			if err := e.stats(row, group.Stats); err != nil {
				return err
			}
		}
	}
	return nil
}
func (e *usageCSVEncoder) stats(locator []string, stats UsageStats) error {
	if err := validateUsageCSVStats(stats); err != nil {
		return err
	}
	row := slices.Clone(locator)
	e.put(row, "row_type", "stats")
	for column, value := range map[string]int64{"requests": stats.Requests, "successes": stats.Successes, "errors": stats.Errors, "canceled": stats.Canceled, "unknown_amount_calls": stats.UnknownAmountCalls} {
		e.put(row, column, strconv.FormatInt(value, 10))
	}
	e.put(row, "success_rate", usageCSVFloat(stats.SuccessRate))
	e.put(row, "average_duration_ms", usageCSVFloat(stats.AverageDurationMS))
	for _, counter := range []struct {
		prefix string
		value  UsageCount
	}{{"input", stats.Tokens.Input}, {"output", stats.Tokens.Output}, {"total", stats.Tokens.Total}} {
		if counter.value.Value != nil {
			e.put(row, counter.prefix+"_value", usageCSVText(*counter.value.Value))
		}
		e.put(row, counter.prefix+"_known", usageCSVText(counter.value.Known))
		e.put(row, counter.prefix+"_unknown_calls", strconv.FormatInt(counter.value.UnknownCalls, 10))
	}
	if err := e.write(row); err != nil {
		return err
	}
	amounts := slices.Clone(stats.Amounts)
	sort.Slice(amounts, func(i, j int) bool { return amounts[i].Currency < amounts[j].Currency })
	for _, amount := range amounts {
		child := slices.Clone(locator)
		for column, value := range map[string]string{"row_type": "amount", "currency": usageCSVText(amount.Currency), "amount": usageCSVText(amount.Amount), "amount_calls": strconv.FormatInt(amount.Calls, 10)} {
			e.put(child, column, value)
		}
		if err := e.write(child); err != nil {
			return err
		}
	}
	statuses := make([]string, 0, len(stats.PricingStatuses))
	for status := range stats.PricingStatuses {
		statuses = append(statuses, status)
	}
	slices.Sort(statuses)
	for _, status := range statuses {
		child := slices.Clone(locator)
		e.put(child, "row_type", "pricing_status")
		e.put(child, "pricing_status", usageCSVText(status))
		e.put(child, "pricing_status_calls", strconv.FormatInt(stats.PricingStatuses[status], 10))
		if err := e.write(child); err != nil {
			return err
		}
	}
	return nil
}
func validateUsageCSVStats(stats UsageStats) error {
	for _, value := range []int64{stats.Requests, stats.Successes, stats.Errors, stats.Canceled, stats.UnknownAmountCalls, stats.Tokens.Input.UnknownCalls, stats.Tokens.Output.UnknownCalls, stats.Tokens.Total.UnknownCalls} {
		if value < 0 || value > usageRowLimit || value > stats.Requests {
			return apperrors.ErrInternal
		}
	}
	if stats.Successes+stats.Errors+stats.Canceled != stats.Requests {
		return apperrors.ErrInternal
	}
	for _, value := range []*float64{stats.SuccessRate, stats.AverageDurationMS} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return apperrors.ErrInternal
		}
	}
	if stats.SuccessRate != nil && *stats.SuccessRate > 1 {
		return apperrors.ErrInternal
	}
	for _, amount := range stats.Amounts {
		if amount.Calls < 0 || amount.Calls > stats.Requests {
			return apperrors.ErrInternal
		}
	}
	for _, count := range stats.PricingStatuses {
		if count < 0 || count > stats.Requests {
			return apperrors.ErrInternal
		}
	}
	return nil
}
