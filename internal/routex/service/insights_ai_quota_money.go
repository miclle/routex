package service

import (
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

func insightsAIQuotaDimension(report string) string {
	switch report {
	case "team_quota_tokens", "key_quota_tokens":
		return "tokens"
	case "team_quota_money", "key_quota_money":
		return "money"
	default:
		return ""
	}
}

func insightsAIQuotaReportDimension(report *InsightsQuotaReport, kind string) bool {
	dimension := insightsAIQuotaDimension(kind)
	return dimension != "" && report.Dimension == dimension &&
		(dimension == "tokens" && report.Currency == "" || dimension == "money" && pricing.Currency(report.Currency))
}

func insightsAIQuotaCaptureDimension(capture *InsightsAITeamQuotaCapture, kind string) bool {
	if capture == nil {
		return false
	}
	switch insightsAIQuotaDimension(kind) {
	case "tokens":
		return capture.Dimension == "" && capture.Currency == ""
	case "money":
		return capture.Dimension == "money" && pricing.Currency(capture.Currency)
	default:
		return false
	}
}

func insightsAIQuotaRowAmounts(row InsightsQuotaRow, dimension, currency string) bool {
	for _, value := range []*string{row.LocalCap, row.MonthSettled, row.BaselineSettled, row.BaselineHeld, row.MonthHeld, row.ActiveHeld} {
		if value != nil && (dimension == "tokens" && !insightsAIInteger.MatchString(*value) || dimension == "money" && !insightsQuotaDecimal.MatchString(*value)) {
			return false
		}
	}
	// Unknown values count unsettled requests; they are never decimal amounts.
	for _, value := range []*string{row.MonthUnknown, row.BaselineUnknown} {
		if value != nil && !insightsAIInteger.MatchString(*value) {
			return false
		}
	}
	if row.UtilizationPercent != nil && !insightsQuotaDecimal.MatchString(*row.UtilizationPercent) {
		return false
	}
	if dimension == "money" && row.LocalCap != nil {
		if !pricing.Currency(row.capCurrency) {
			return false
		}
		if row.capCurrency != currency {
			// A retained foreign-denomination cap is disclosed exactly, but the
			// existing report cannot supply comparable usage or an estimate.
			return row.EstimateStatus == "unavailable" && row.Level == "unknown" &&
				row.MonthSettled == nil && row.BaselineSettled == nil && row.BaselineHeld == nil &&
				row.MonthHeld == nil && row.ActiveHeld == nil && row.MonthUnknown == nil &&
				row.BaselineUnknown == nil && row.UtilizationPercent == nil &&
				row.EstimateAt == nil && !row.EstimateWithinSevenDays
		}
	}
	return true
}

func insightsAIQuotaRowView(row InsightsQuotaRow, dimension, label string) insightsAITeamQuotaRow {
	result := insightsAITeamQuotaRow{
		Label: label, Status: row.Status, LocalCap: row.LocalCap, Behavior: row.Behavior,
		MonthSettled: row.MonthSettled, BaselineSettled: row.BaselineSettled,
		BaselineHeld: row.BaselineHeld, MonthHeld: row.MonthHeld, ActiveHeld: row.ActiveHeld,
		MonthUnknown: row.MonthUnknown, BaselineUnknown: row.BaselineUnknown,
		MonthCovered: row.MonthCovered, BaselineCovered: row.BaselineCovered,
		UtilizationPercent: row.UtilizationPercent, Level: row.Level,
		EstimateAt: row.EstimateAt, EstimateStatus: row.EstimateStatus,
		EstimateWithinSevenDays: row.EstimateWithinSevenDays,
	}
	if dimension == "money" && row.LocalCap != nil {
		result.LocalCapCurrency = insightsQuotaText(row.capCurrency)
	}
	return result
}

func insightsAIQuotaMoneyChatBody(model, language, kind string, projection []byte, question *string) ([]byte, error) {
	label, subject := "T", "Team"
	if kind == "key" {
		label, subject = "K", "shared root-Key account"
	} else if kind != "team" {
		return nil, apperrors.ErrBadRequest
	}
	instruction := "Explain only the supplied current-page " + subject + " Money quota aggregates, referring to accounts only by " + label + " labels. No identities, global totals or other pages. Amounts are exact decimal strings in the supplied currency; each stored own cap has its separately recorded local_cap_currency. Never convert, relabel, sum different currencies or infer missing amounts. A mismatched cap currency remains unavailable. Preserve null/unknown coverage, explicit zero versus no own cap, Stop versus Alert only, settled amounts and separate reservations. Rotation descendants share their recorded root account; shared_key_count is only the supplied count. Quote only supplied estimates from settled admissions in trailing 168 hours at a constant rate until monthly reset; these are not demand predictions, parent allowance or actual blocking. Do not calculate new thresholds, forecasts or quota policy. No tools, links, writes or actions. Return concise plain-text English advice."
	if language == "zh" {
		instruction = "仅解释当前页的金额配额聚合，以 " + label + " 标签指代账户，不得推断身份、全局总数或其他页面。金额是所提供币种的精确十进制字符串；本级限额使用单独记录的 local_cap_currency。不得换算、改标币种、合计不同币种或推算缺失金额；限额币种不匹配时保持不可用。保留空值、未知覆盖、显式零与未设置本级限额、停止与仅提醒行为、已结算金额和独立预留。轮换后继共享已记录的根账户；shared_key_count 仅是提供的数量。只能引用已有估计：按过去 168 小时获准请求的已结算用量，以恒定速率估计至月重置；不代表需求预测、父级额度或实际阻断。不得计算新阈值、预测或配额策略，不得使用工具、链接、写入或操作。用简洁中文纯文本回答。"
	} else if language != "en" {
		return nil, apperrors.ErrBadRequest
	}
	return insightsAIChatBodyWithInstruction(model, language, instruction, label, projection, question)
}
