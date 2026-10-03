package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
	"strings"
	"unicode"
	"unicode/utf8"
)

type teamModelRequestAuditDetail struct {
	TeamID          string  `json:"team_id"`
	TeamName        string  `json:"team_name"`
	ApplicantUserID string  `json:"applicant_user_id"`
	ModelID         string  `json:"model_id"`
	ModelName       string  `json:"model_name"`
	Status          string  `json:"request_status"`
	DecisionID      *string `json:"decision_id"`
	Action          string  `json:"action"`
	Reason          string  `json:"reason"`
}

func appendTeamModelRequestAudit(tx *gorm.DB, actorID, action string, row entity.TeamModelRequest, reason string) error {
	detail := teamModelRequestAuditDetail{TeamID: row.TeamID, TeamName: row.TeamName, ApplicantUserID: row.ApplicantUserID, ModelID: row.ModelID, ModelName: row.ModelName, Status: row.Status, DecisionID: row.DecisionID, Action: action, Reason: reason}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	raw := string(encoded)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "team.model_request." + action, ResourceType: "team_model_request", ResourceID: row.ID, DetailsJSON: &raw}).Error
}

func teamModelRequestAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON == nil || row.ResourceType != "team_model_request" || !personalModelID(row.ResourceID, "tmr") {
		return nil, false
	}
	var detail teamModelRequestAuditDetail
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || !safeTeamSessionID(detail.TeamID) || !safeTeamSessionID(detail.ApplicantUserID) || !safeTeamSessionID(detail.ModelID) || !teamModelAuditName(detail.TeamName, 100) || !teamModelAuditName(detail.ModelName, 128) || strings.TrimSpace(detail.Reason) != detail.Reason || !personalModelReason(detail.Reason, detail.Action == "create" || detail.Action == "reject" || detail.Action == "cancel") {
		return nil, false
	}
	statuses := map[string]string{"create": entity.TeamModelRequestPending, "approve": entity.TeamModelRequestApproved, "reject": entity.TeamModelRequestRejected, "withdraw": entity.TeamModelRequestWithdrawn, "cancel": entity.TeamModelRequestCancelled}
	status, known := statuses[detail.Action]
	if !known || status != detail.Status || row.Action != "team.model_request."+detail.Action {
		return nil, false
	}
	if detail.Action == "create" || detail.Action == "cancel" {
		if detail.DecisionID != nil {
			return nil, false
		}
	} else if detail.DecisionID == nil || !credentialReplacementRequestID.MatchString(*detail.DecisionID) {
		return nil, false
	}
	if detail.Action == "withdraw" && detail.Reason != "" {
		return nil, false
	}
	return detail, true
}
func teamModelAuditName(value string, limit int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && !strings.ContainsFunc(value, unicode.IsControl)
}
