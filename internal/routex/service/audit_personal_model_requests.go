package service

import (
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/miclle/routex/internal/routex/entity"
)

type personalModelAuditDetail struct {
	ApplicantUserID string  `json:"applicant_user_id"`
	ModelID         string  `json:"model_id"`
	ModelName       string  `json:"model_name"`
	Status          string  `json:"request_status"`
	DecisionID      *string `json:"decision_id"`
	Action          string  `json:"action"`
	Reason          string  `json:"reason"`
}

func personalModelAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.DetailsJSON == nil || row.ResourceType != "personal_model_request" || !personalModelID(row.ResourceID, "mar") {
		return nil, false
	}
	var detail personalModelAuditDetail
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || !personalModelID(detail.ApplicantUserID, "usr") || !personalModelID(detail.ModelID, "mdl") || detail.ModelName == "" || !utf8.ValidString(detail.ModelName) || utf8.RuneCountInString(detail.ModelName) > 128 || strings.ContainsFunc(detail.ModelName, unicode.IsControl) || strings.TrimSpace(detail.Reason) != detail.Reason || !personalModelReason(detail.Reason, detail.Action == "create" || detail.Action == "reject" || detail.Action == "cancel") {
		return nil, false
	}
	statuses := map[string]string{"create": entity.PersonalModelRequestPending, "approve": entity.PersonalModelRequestApproved, "reject": entity.PersonalModelRequestRejected, "withdraw": entity.PersonalModelRequestWithdrawn, "cancel": entity.PersonalModelRequestCancelled}
	status, known := statuses[detail.Action]
	if !known || status != detail.Status || row.Action != "personal.model_request."+detail.Action {
		return nil, false
	}
	if detail.Action == "create" || detail.Action == "cancel" {
		if detail.DecisionID != nil {
			return nil, false
		}
	} else if detail.DecisionID == nil || !credentialReplacementRequestID.MatchString(*detail.DecisionID) {
		return nil, false
	}
	return detail, true
}
