package service

import (
	"encoding/json"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/pricing"
)

func teamQuotaAuditProjection(row entity.AuditEvent) (any, bool) {
	var detail struct {
		TeamID          string `json:"team_id"`
		ApplicantUserID string `json:"applicant_user_id"`
		Dimension       string `json:"dimension"`
		TargetValue     string `json:"target_value"`
		Currency        string `json:"currency"`
		StepID          string `json:"step_id"`
		Stage           string `json:"stage"`
		RequestStatus   string `json:"request_status"`
		Reason          string `json:"reason"`
	}
	if row.DetailsJSON == nil || json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil ||
		row.ResourceType != "team_quota_request" || !safeTeamSessionID(row.ResourceID) || !strings.HasPrefix(row.ResourceID, "qrq_") ||
		!safeTeamSessionID(detail.TeamID) || !strings.HasPrefix(detail.TeamID, "tea_") ||
		!safeTeamSessionID(detail.ApplicantUserID) || !strings.HasPrefix(detail.ApplicantUserID, "usr_") ||
		!safeTeamSessionID(detail.StepID) || !strings.HasPrefix(detail.StepID, "qst_") ||
		!validTeamQuotaDimension(detail.Dimension) ||
		(detail.Stage != entity.TeamQuotaStageOwner && detail.Stage != entity.TeamQuotaStageAdmin) ||
		strings.TrimSpace(detail.Reason) != detail.Reason ||
		!teamQuotaReason(detail.Reason, row.Action == "team.quota_request.create" || row.Action == "team.quota_request.reject") {
		return nil, false
	}
	if _, err := canonicalTeamQuotaTarget(detail.Dimension, detail.TargetValue); err != nil ||
		detail.Dimension == "tokens" && detail.Currency != "" || detail.Dimension == "money" && !pricing.Currency(detail.Currency) {
		return nil, false
	}
	switch row.Action {
	case "team.quota_request.create":
		if !teamQuotaPending(detail.RequestStatus) {
			return nil, false
		}
	case "team.quota_request.approve":
		if detail.RequestStatus != entity.TeamQuotaRequestApproved &&
			(detail.Stage != entity.TeamQuotaStageOwner || detail.RequestStatus != entity.TeamQuotaRequestPendingAdmin) {
			return nil, false
		}
	case "team.quota_request.reject":
		if detail.RequestStatus != entity.TeamQuotaRequestRejected {
			return nil, false
		}
	case "team.quota_request.withdraw":
		if detail.RequestStatus != entity.TeamQuotaRequestWithdrawn {
			return nil, false
		}
	default:
		return nil, false
	}
	return detail, true
}
