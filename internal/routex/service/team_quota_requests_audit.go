package service

import (
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
)

func appendTeamQuotaRequestAudit(tx *gorm.DB, actorID, action string, row entity.TeamQuotaRequest, step entity.TeamQuotaRequestStep, reason string) error {
	raw, err := json.Marshal(struct {
		TeamID          string `json:"team_id"`
		ApplicantUserID string `json:"applicant_user_id"`
		Dimension       string `json:"dimension"`
		TargetValue     string `json:"target_value"`
		Currency        string `json:"currency"`
		StepID          string `json:"step_id"`
		Stage           string `json:"stage"`
		RequestStatus   string `json:"request_status"`
		Reason          string `json:"reason"`
	}{row.TeamID, row.ApplicantUserID, row.Dimension, row.TargetValue, row.Currency, step.ID, step.Stage, row.Status, reason})
	if err != nil {
		return err
	}
	auditID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	details := string(raw)
	return tx.Create(&entity.AuditEvent{ID: auditID, ActorID: actorID, Action: action, ResourceType: "team_quota_request", ResourceID: row.ID, DetailsJSON: &details, CreatedAt: time.Now().UTC()}).Error
}
