package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"gorm.io/gorm"
)

type insightsAITeamProof struct {
	ID        string
	CreatedAt time.Time
}

// Cursor belongs only to the caller's reviewed page. It contains actor-bound
// pagination state and is deliberately excluded from the upstream projection.
type InsightsAITeamQuotaCapture struct {
	Source        string    `json:"source"`
	QueriedAt     time.Time `json:"queried_at"`
	Cursor        string    `json:"cursor"`
	Limit         int       `json:"limit"`
	HasMore       bool      `json:"has_more"`
	Timezone      string    `json:"timezone"`
	AsOf          *string   `json:"as_of"`
	CoverageStart *string   `json:"coverage_start"`
	BaselineFrom  *string   `json:"baseline_from"`
	BaselineTo    *string   `json:"baseline_to"`
	MonthFrom     *string   `json:"month_from"`
	MonthReset    *string   `json:"month_reset"`
	BaselineBasis string    `json:"baseline_basis"`
	Dimension     string    `json:"dimension,omitempty"`
	Currency      string    `json:"currency,omitempty"`
}

type insightsAITeamQuotaRow struct {
	Label                   string  `json:"label"`
	Status                  string  `json:"status"`
	LocalCap                *string `json:"local_cap"`
	Behavior                string  `json:"behavior"`
	MonthSettled            *string `json:"month_settled"`
	BaselineSettled         *string `json:"baseline_settled"`
	BaselineHeld            *string `json:"baseline_held"`
	MonthHeld               *string `json:"month_held"`
	ActiveHeld              *string `json:"active_held"`
	MonthUnknown            *string `json:"month_unknown"`
	BaselineUnknown         *string `json:"baseline_unknown"`
	MonthCovered            bool    `json:"month_covered"`
	BaselineCovered         bool    `json:"baseline_covered"`
	UtilizationPercent      *string `json:"utilization_percent"`
	Level                   string  `json:"level"`
	EstimateAt              *string `json:"estimate_at"`
	EstimateStatus          string  `json:"estimate_status"`
	EstimateWithinSevenDays bool    `json:"estimate_within_seven_days"`
	LocalCapCurrency        *string `json:"local_cap_currency,omitempty"`
}

func validateInsightsAITeamQuota(input InsightsAIInput) error {
	if (input.Report != "team_quota_tokens" && input.Report != "team_quota_money") || !insightsAIModelID.MatchString(input.ModelID) || input.Language != "en" && input.Language != "zh" || !validInsightsAIQuestion(input.Question) || input.From != "" || input.To != "" || input.Timezone != "" || input.ScopeKind != "" || input.ScopeID != "" {
		return apperrors.ErrBadRequest
	}
	if len(input.Cursor) > 160 || !utf8.ValidString(input.Cursor) || input.Limit < 1 || input.Limit > 50 {
		return apperrors.ErrBadRequest
	}
	// The real actor-bound cursor is validated again by InsightsQuota. Validation
	// here checks the closed transport bound without interpreting another actor.
	return nil
}

func insightsAIQuotaStamp(value *string) bool {
	if value == nil {
		return true
	}
	if !insightsAIAbsolute.MatchString(*value) {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, *value)
	return err == nil
}

func projectInsightsAITeamQuota(report *InsightsQuotaReport, input InsightsAIInput, observed time.Time) (*InsightsAIResponse, []byte, error) {
	if validateInsightsAITeamQuota(input) != nil || report == nil || report.Kind != "team_quota" || !insightsAIQuotaReportDimension(report, input.Report) || report.Scope.Kind != "readable_teams" || report.Scope.ID != "" || report.BaselineBasis != "settled_admissions_168h" || observed.IsZero() || len(report.Items) > input.Limit || len(report.teamProofs) != len(report.Items) {
		return nil, nil, insightsAIUnavailable
	}
	if _, err := time.LoadLocation(report.TimeZone); err != nil || report.TimeZone == "" || report.TimeZone == "Local" {
		return nil, nil, insightsAIUnavailable
	}
	for _, stamp := range []*string{report.AsOf, report.CoverageStart, report.BaselineFrom, report.BaselineTo, report.MonthFrom, report.MonthReset} {
		if !insightsAIQuotaStamp(stamp) {
			return nil, nil, insightsAIUnavailable
		}
	}
	if !insightsAITeamQuotaFrame(report) {
		return nil, nil, insightsAIUnavailable
	}
	capture := &InsightsAITeamQuotaCapture{Source: "local_quota_journal", QueriedAt: observed, Cursor: input.Cursor, Limit: input.Limit, HasMore: report.NextCursor != nil, Timezone: report.TimeZone, AsOf: report.AsOf, CoverageStart: report.CoverageStart, BaselineFrom: report.BaselineFrom, BaselineTo: report.BaselineTo, MonthFrom: report.MonthFrom, MonthReset: report.MonthReset, BaselineBasis: report.BaselineBasis}
	if report.Dimension == "money" {
		capture.Dimension, capture.Currency = report.Dimension, report.Currency
	}
	view := &InsightsAIResponse{Completion: "unknown", QuotaCapture: capture, Labels: []InsightsAILabel{}, teamProofs: append([]insightsAITeamProof(nil), report.teamProofs...)}
	// This is a closed projection, not serialization of the full report. Names,
	// canonical IDs, cursor, membership and other resource domains never enter it.
	projection := struct {
		Scope         string                   `json:"scope"`
		Dimension     string                   `json:"dimension"`
		Currency      string                   `json:"currency,omitempty"`
		Timezone      string                   `json:"timezone"`
		QueriedAt     time.Time                `json:"queried_at"`
		AsOf          *string                  `json:"as_of"`
		CoverageStart *string                  `json:"coverage_start"`
		BaselineFrom  *string                  `json:"baseline_from"`
		BaselineTo    *string                  `json:"baseline_to"`
		MonthFrom     *string                  `json:"month_from"`
		MonthReset    *string                  `json:"month_reset"`
		BaselineBasis string                   `json:"baseline_basis"`
		HasMore       bool                     `json:"has_more"`
		Teams         []insightsAITeamQuotaRow `json:"teams"`
	}{"current_page", report.Dimension, report.Currency, report.TimeZone, observed, report.AsOf, report.CoverageStart, report.BaselineFrom, report.BaselineTo, report.MonthFrom, report.MonthReset, report.BaselineBasis, report.NextCursor != nil, []insightsAITeamQuotaRow{}}
	seen := map[string]bool{}
	for i, row := range report.Items {
		if !safeTeamSessionID(row.ID) || seen[row.ID] || len(row.Name) > 1024 || !utf8.ValidString(row.Name) || report.teamProofs[i].ID != row.ID || report.teamProofs[i].CreatedAt.IsZero() || !slices.Contains([]string{"active", "disabled", "archived"}, row.Status) || !slices.Contains([]string{"stop", "alert_only"}, row.Behavior) || !slices.Contains([]string{"normal", "near", "critical", "reached", "unknown"}, row.Level) || !slices.Contains([]string{"estimated", "already_reached", "no_local_cap", "zero_cap", "inactive", "unavailable", "incomplete_coverage", "unknown_usage", "zero_rate", "after_reset"}, row.EstimateStatus) || !insightsAIQuotaStamp(row.EstimateAt) {
			return nil, nil, insightsAIUnavailable
		}
		if !insightsAIQuotaRowAmounts(row, report.Dimension, report.Currency) {
			return nil, nil, insightsAIUnavailable
		}
		seen[row.ID] = true
		label := fmt.Sprintf("T%d", i+1)
		id := row.ID
		view.Labels = append(view.Labels, InsightsAILabel{Label: label, TeamID: &id, Name: row.Name, Unknown: false})
		projection.Teams = append(projection.Teams, insightsAIQuotaRowView(row, report.Dimension, label))
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return nil, nil, insightsAIUnavailable
	}
	metadata, err := json.Marshal(view)
	if err != nil {
		return nil, nil, insightsAIUnavailable
	}
	if len(raw) > insightsAIProjectionLimit || len(metadata) > insightsAILabelLimit {
		return nil, nil, insightsAITooLarge
	}
	return view, raw, nil
}

// The projection preserves one server-owned frame, including fully unavailable
// observations. It never combines a known calendar with missing journal time.
func insightsAITeamQuotaFrame(report *InsightsQuotaReport) bool {
	values := []*string{report.CoverageStart, report.BaselineFrom, report.BaselineTo, report.MonthFrom, report.MonthReset}
	if report.AsOf == nil {
		for _, value := range values {
			if value != nil {
				return false
			}
		}
		return true
	}
	asOf, err := time.Parse(time.RFC3339Nano, *report.AsOf)
	if err != nil {
		return false
	}
	times := make([]time.Time, len(values))
	for i, value := range values {
		if value == nil {
			return false
		}
		parsed, err := time.Parse(time.RFC3339Nano, *value)
		if err != nil {
			return false
		}
		times[i] = parsed
	}
	return !times[0].After(asOf) && times[1].Equal(asOf.Add(-168*time.Hour)) && times[2].Equal(asOf) && !times[3].After(asOf) && times[4].After(asOf)
}

// Exact captured Team birth and current scoped read authority are independent of
// the report's directory page and of the actor's personal inference grant.
func (s *Service) ReauthorizeInsightsAIResult(ctx context.Context, identity *PersonalSessionIdentity, modelID, report string, view *InsightsAIResponse) error {
	if err := s.ReauthorizeInsightsAIReport(ctx, identity, modelID, report); err != nil {
		return err
	}
	if report == "key_quota_tokens" || report == "key_quota_money" {
		if view == nil || view.KeyQuotaCapture == nil || !insightsAIQuotaCaptureDimension(&view.KeyQuotaCapture.InsightsAITeamQuotaCapture, report) {
			return insightsAIUnavailable
		}
		return s.reauthorizeInsightsAIKeyQuota(ctx, identity, view)
	}
	if report != "team_quota_tokens" && report != "team_quota_money" {
		return nil
	}
	if view == nil || !insightsAIQuotaCaptureDimension(view.QuotaCapture, report) || view.Capture != nil || view.KeyQuotaCapture != nil || len(view.teamProofs) != len(view.Labels) || len(view.teamProofs) > 50 {
		return insightsAIUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := s.authDB(ctx).Transaction(func(tx *gorm.DB) error {
		for i, proof := range view.teamProofs {
			if view.Labels[i].TeamID == nil || *view.Labels[i].TeamID != proof.ID {
				return insightsAIUnavailable
			}
			current, err := loadTeamLimitContext(tx, identity.UserID, proof.ID, "", false)
			if err != nil {
				return err
			}
			if current.Actor.ID != identity.UserID || !current.Actor.CreatedAt.Equal(identity.user.CreatedAt) || current.Team.ID != proof.ID || !current.Team.CreatedAt.Equal(proof.CreatedAt) {
				return apperrors.ErrNotFound
			}
		}
		return ctx.Err()
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return catalogError(err)
}

func insightsAITeamQuotaChatBody(model, language string, projection []byte, question *string) ([]byte, error) {
	instruction := "Explain only the supplied current-page Team Token quota aggregates, referring to Teams only by T labels. No global totals or extrapolation to other pages. Preserve null/unknown coverage, stored own monthly cap, Stop versus Alert only, settled amounts and separate reservations. Quote only supplied estimates: settled admissions in trailing 168 hours at a constant rate until monthly reset; these are not demand predictions, parent allowance or actual blocking. Do not calculate new thresholds, forecasts or quota policy. No identities, tools, links, writes or actions. Return concise plain-text English advice."
	if language == "zh" {
		instruction = "仅解释当前页的团队 Token 配额聚合，以 T 标签指代团队。不得推断全局总数或其他页面。保留空值、未知覆盖、本级月限额、停止与仅提醒行为、已结算用量和独立预留。只能引用提供的估计：按过去 168 小时获准请求的已结算用量，以恒定速率估计至月重置；不代表需求预测、父级额度或实际阻断。不得计算新阈值、预测或配额策略。不得推断身份或使用工具、链接、写入与操作。用简洁中文纯文本回答。"
	} else if language != "en" {
		return nil, apperrors.ErrBadRequest
	}
	return insightsAIChatBodyWithInstruction(model, language, instruction, "T", projection, question)
}
