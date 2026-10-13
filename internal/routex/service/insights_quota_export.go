package service

import (
	"context"
	"encoding/csv"
	"slices"
	"strconv"
	"time"
)

func (s *Service) ExportInsightsQuota(ctx context.Context, actor string, input InsightsQuotaInput, keys bool) (*UsageCSVExport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	report, err := s.InsightsQuota(ctx, actor, input, keys)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeInsightsQuotaCSV(ctx, report)
	if err != nil {
		return nil, err
	}
	return &UsageCSVExport{CSV: encoded}, nil
}

func encodeInsightsQuotaCSV(ctx context.Context, report *InsightsQuotaReport) ([]byte, error) {
	buffer := &usageCSVBuffer{ctx: ctx}
	writer := csv.NewWriter(buffer)
	columns := []string{"row_type", "schema_version", "text_encoding", "kind", "dimension", "currency", "scope_kind", "scope_id", "as_of", "coverage_start", "time_zone", "baseline_basis", "baseline_from", "baseline_to", "month_from", "month_reset", "page_accounts", "next_cursor", "id", "name", "status", "shared_key_count", "local_cap", "behavior", "month_settled", "baseline_settled", "baseline_held", "month_held", "active_held", "month_unknown", "baseline_unknown", "month_covered", "baseline_covered", "utilization_percent", "level", "estimate_at", "estimate_status", "estimate_within_seven_days"}
	if err := writer.Write(columns); err != nil {
		return nil, err
	}
	optional := func(value *string) string {
		if value == nil {
			return ""
		}
		return usageCSVText(*value)
	}
	base := []string{"metadata", "1", "apostrophe-prefixed-text", report.Kind, report.Dimension, report.Currency, report.Scope.Kind, usageCSVOptionalText(report.Scope.ID), optional(report.AsOf), optional(report.CoverageStart), usageCSVText(report.TimeZone), report.BaselineBasis, optional(report.BaselineFrom), optional(report.BaselineTo), optional(report.MonthFrom), optional(report.MonthReset), strconv.Itoa(len(report.Items)), optional(report.NextCursor)}
	metadata := append(slices.Clone(base), make([]string, len(columns)-len(base))...)
	if err := writer.Write(metadata); err != nil {
		return nil, err
	}
	for _, row := range report.Items {
		values := slices.Clone(base)
		values[0] = "account"
		values = append(values, usageCSVText(row.ID), usageCSVText(row.Name), row.Status, strconv.Itoa(row.SharedKeyCount), optional(row.LocalCap), row.Behavior, optional(row.MonthSettled), optional(row.BaselineSettled), optional(row.BaselineHeld), optional(row.MonthHeld), optional(row.ActiveHeld), optional(row.MonthUnknown), optional(row.BaselineUnknown), strconv.FormatBool(row.MonthCovered), strconv.FormatBool(row.BaselineCovered), optional(row.UtilizationPercent), row.Level, optional(row.EstimateAt), row.EstimateStatus, strconv.FormatBool(row.EstimateWithinSevenDays))
		if err := writer.Write(values); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	return buffer.Bytes(), nil
}
