package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"sort"
	"strconv"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// InsightsProviderQualityInput selects completed immutable attempts in [From, To).
// It deliberately has no catalog, user, Key, or final logical-call selector.
type InsightsProviderQualityInput struct{ From, To, Timezone string }

type InsightsProviderQualityCounts struct {
	Attempts           int64  `json:"attempts"`
	Eligible           int64  `json:"eligible"`
	Excluded           int64  `json:"excluded"`
	CredentialRejected int64  `json:"credential_rejected"`
	Successes          int64  `json:"successes"`
	SuccessRateBPS     *int   `json:"success_rate_bps"`
	RateLimited        int64  `json:"rate_limited"`
	ServerErrors       int64  `json:"server_errors"`
	KnownDuration      int64  `json:"known_duration"`
	UnknownDuration    int64  `json:"unknown_duration"`
	P95DurationMS      *int64 `json:"p95_duration_ms"`
}
type InsightsProviderQualityRow struct {
	ProviderID string `json:"provider_id"`
	Name       string `json:"name"`
	InsightsProviderQualityCounts
}
type InsightsProviderQualityReport struct {
	Source             string                        `json:"source"`
	From               string                        `json:"from"`
	To                 string                        `json:"to"`
	Timezone           string                        `json:"timezone"`
	QueriedAt          time.Time                     `json:"queried_at"`
	LatestCompletedAt  *time.Time                    `json:"latest_completed_at"`
	MayLag             bool                          `json:"may_lag"`
	TotalAttempts      int64                         `json:"total_attempts"`
	UnknownAttribution int64                         `json:"unknown_attribution"`
	Summary            InsightsProviderQualityCounts `json:"summary"`
	Providers          []InsightsProviderQualityRow  `json:"providers"`
}

func insightsProviderQualityRange(input InsightsProviderQualityInput, now time.Time) (usagePlan, error) {
	if len(input.From) > 64 || len(input.To) > 64 || !insightsAIAbsolute.MatchString(input.From) || !insightsAIAbsolute.MatchString(input.To) {
		return usagePlan{}, apperrors.ErrBadRequest
	}
	from, err := time.Parse(time.RFC3339Nano, input.From)
	if err != nil {
		return usagePlan{}, apperrors.ErrBadRequest
	}
	to, err := time.Parse(time.RFC3339Nano, input.To)
	if err != nil || input.Timezone == "" {
		return usagePlan{}, apperrors.ErrBadRequest
	}
	return planUsage(UsageFilter{From: &from, To: &to, Timezone: input.Timezone}, now)
}

// V5 froze attempt timestamps at precision6. Both supported databases store
// microsecond instants, and a driver may truncate a nanosecond query parameter.
// Compare the exact stored grid with adjusted operators instead of changing the
// reviewed range or accidentally excluding a row just before a fractional end.
func insightsProviderQualityBounds(from, to time.Time) (lower, upper time.Time, lowerPredicate, upperPredicate string) {
	lower, upper = from.Truncate(time.Microsecond), to.Truncate(time.Microsecond)
	lowerPredicate, upperPredicate = "completed_at >= ?", "completed_at < ?"
	if lower.Before(from) {
		lowerPredicate = "completed_at > ?"
	}
	if upper.Before(to) {
		upperPredicate = "completed_at <= ?"
	}
	return
}
func authorizeInsightsProviderQuality(db *gorm.DB, actor string) error {
	if err := authorizeUsage(db, actor, "admin", ""); err != nil {
		return err
	}
	return authorizeGovernance(db, actor, "providers.read")
}

func (s *Service) InsightsProviderQuality(ctx context.Context, actor string, input InsightsProviderQualityInput) (*InsightsProviderQualityReport, error) {
	now := time.Now().UTC()
	plan, err := insightsProviderQualityRange(input, now)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows := []entity.CallAttempt{}
	lower, upper, lowerPredicate, upperPredicate := insightsProviderQualityBounds(plan.current.from, plan.current.to)
	// One complete bounded snapshot; no current catalog or final Call joins can
	// fill missing historical attribution or rewrite historical Provider names.
	err = s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		if err := authorizeInsightsProviderQuality(tx, actor); err != nil {
			return err
		}
		return tx.Select("id", "provider_id", "provider_name", "status", "work_evidence", "failure_class", "http_status", "duration_ms", "completed_at").
			Where(lowerPredicate, lower).Where(upperPredicate, upper).
			Order("completed_at ASC, id ASC").Limit(providerQualityMaxAttempts + 1).Find(&rows).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		if ctx.Err() != nil {
			return nil, usageExportUnavailable
		}
		return nil, catalogError(err)
	}
	result, err := aggregateInsightsProviderQuality(ctx, rows, InsightsProviderQualityInput{From: plan.current.from.UTC().Format(time.RFC3339Nano), To: plan.current.to.UTC().Format(time.RFC3339Nano), Timezone: plan.location.String()}, now)
	if err != nil {
		return nil, err
	}
	// Recheck current independent authority after the snapshot and aggregation.
	if err := authorizeInsightsProviderQuality(s.authDB(ctx), actor); err != nil {
		return nil, catalogError(err)
	}
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	return result, nil
}

type insightsQualityAccumulator struct {
	counts    InsightsProviderQualityCounts
	durations []int64
}

func (a *insightsQualityAccumulator) add(row entity.CallAttempt) {
	a.counts.Attempts++
	switch {
	case row.WorkEvidence == "not_sent" || row.Status == "canceled":
		a.counts.Excluded++
	case row.FailureClass == "credential_rejected":
		a.counts.CredentialRejected++
	default:
		a.counts.Eligible++
		if row.Status == "success" {
			a.counts.Successes++
		}
		if row.HTTPStatus == 429 || row.FailureClass == "rate_limited" {
			a.counts.RateLimited++
		}
		if row.HTTPStatus >= 500 && row.HTTPStatus < 600 {
			a.counts.ServerErrors++
		}
		if row.DurationMS != nil && *row.DurationMS >= 0 {
			a.durations = append(a.durations, *row.DurationMS)
		}
	}
}
func (a *insightsQualityAccumulator) finish() InsightsProviderQualityCounts {
	result := a.counts
	result.KnownDuration = int64(len(a.durations))
	result.UnknownDuration = result.Eligible - result.KnownDuration
	if result.Eligible > 0 {
		rate := int(result.Successes * 10000 / result.Eligible)
		result.SuccessRateBPS = &rate
	}
	if len(a.durations) > 0 {
		sort.Slice(a.durations, func(i, j int) bool { return a.durations[i] < a.durations[j] })
		p95 := a.durations[providerQualityP95Offset(int64(len(a.durations)))]
		result.P95DurationMS = &p95
	}
	return result
}
func aggregateInsightsProviderQuality(ctx context.Context, rows []entity.CallAttempt, input InsightsProviderQualityInput, now time.Time) (*InsightsProviderQualityReport, error) {
	if len(rows) > providerQualityMaxAttempts {
		return nil, providerQualityTooLarge
	}
	result := &InsightsProviderQualityReport{Source: "persisted_call_attempts", From: input.From, To: input.To, Timezone: input.Timezone, QueriedAt: now, MayLag: true, TotalAttempts: int64(len(rows)), Providers: []InsightsProviderQualityRow{}}
	type group struct {
		name, lastID string
		latest       time.Time
		accumulator  insightsQualityAccumulator
	}
	groups := map[string]*group{}
	var summary insightsQualityAccumulator
	for _, row := range rows {
		if ctx.Err() != nil {
			return nil, usageExportUnavailable
		}
		if result.LatestCompletedAt == nil || row.CompletedAt.After(*result.LatestCompletedAt) {
			stamp := row.CompletedAt
			result.LatestCompletedAt = &stamp
		}
		if row.ProviderID == "" {
			result.UnknownAttribution++
			continue
		}
		current := groups[row.ProviderID]
		if current == nil {
			if len(groups) >= usageGroupLimit {
				return nil, providerQualityTooLarge
			}
			current = &group{}
			groups[row.ProviderID] = current
		}
		if current.latest.IsZero() || row.CompletedAt.After(current.latest) || row.CompletedAt.Equal(current.latest) && row.ID > current.lastID {
			current.name, current.latest, current.lastID = row.ProviderName, row.CompletedAt, row.ID
		}
		current.accumulator.add(row)
		summary.add(row)
	}
	result.Summary = summary.finish()
	for providerID, current := range groups {
		result.Providers = append(result.Providers, InsightsProviderQualityRow{ProviderID: providerID, Name: current.name, InsightsProviderQualityCounts: current.accumulator.finish()})
	}
	sort.Slice(result.Providers, func(i, j int) bool { return result.Providers[i].ProviderID < result.Providers[j].ProviderID })
	return result, nil
}

func (s *Service) ExportInsightsProviderQuality(ctx context.Context, actor string, input InsightsProviderQualityInput) (*UsageCSVExport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	report, err := s.InsightsProviderQuality(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeInsightsProviderQualityCSV(ctx, report)
	if err != nil {
		return nil, err
	}
	if err := authorizeInsightsProviderQuality(s.authDB(ctx), actor); err != nil {
		return nil, catalogError(err)
	}
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	return &UsageCSVExport{CSV: encoded}, nil
}
func encodeInsightsProviderQualityCSV(ctx context.Context, report *InsightsProviderQualityReport) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writeSafeCSVRecord(writer, []string{"row_type", "source", "from", "to", "timezone", "queried_at", "may_lag", "total_attempts", "unknown_attribution", "provider_id", "recorded_name", "attempts", "eligible", "excluded", "credential_rejected", "successes", "success_rate_bps", "rate_limited", "server_errors", "known_duration", "unknown_duration", "p95_duration_ms", "latest_selected_attempt_end"}); err != nil {
		return nil, err
	}
	for _, row := range append([]InsightsProviderQualityRow{{InsightsProviderQualityCounts: report.Summary}}, report.Providers...) {
		if ctx.Err() != nil {
			return nil, usageExportUnavailable
		}
		number := func(value int64) string { return strconv.FormatInt(value, 10) }
		rate, p95 := "", ""
		if row.SuccessRateBPS != nil {
			rate = strconv.Itoa(*row.SuccessRateBPS)
		}
		if row.P95DurationMS != nil {
			p95 = number(*row.P95DurationMS)
		}
		latest := ""
		if report.LatestCompletedAt != nil {
			latest = report.LatestCompletedAt.Format(time.RFC3339Nano)
		}
		rowType := "provider"
		if row.ProviderID == "" {
			rowType = "summary"
		}
		err := writeSafeCSVRecord(writer, []string{rowType, report.Source, report.From, report.To, report.Timezone, report.QueriedAt.Format(time.RFC3339Nano), strconv.FormatBool(report.MayLag), number(report.TotalAttempts), number(report.UnknownAttribution), row.ProviderID, row.Name, number(row.Attempts), number(row.Eligible), number(row.Excluded), number(row.CredentialRejected), number(row.Successes), rate, number(row.RateLimited), number(row.ServerErrors), number(row.KnownDuration), number(row.UnknownDuration), p95, latest})
		if err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	if buffer.Len() > usageExportBytes {
		return nil, usageExportTooLarge
	}
	if ctx.Err() != nil {
		return nil, usageExportUnavailable
	}
	return buffer.Bytes(), nil
}
