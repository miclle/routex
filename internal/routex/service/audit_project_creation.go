package service

import (
	"encoding/json"
	"strings"

	"github.com/miclle/routex/internal/routex/entity"
	"github.com/miclle/routex/pkg/id"
	"gorm.io/gorm"
)

type projectCreationAuditDetail struct {
	Reason            string   `json:"reason"`
	CreationID        string   `json:"creation_id"`
	ProjectID         string   `json:"project_id"`
	ManagerCount      int      `json:"manager_count"`
	ModelCount        int      `json:"model_count"`
	InitialRequestIDs []string `json:"initial_request_ids"`
}

func appendProjectCreationAudit(tx *gorm.DB, actorID string, receipt entity.ProjectCreationReceipt) error {
	snapshot, err := readProjectCreationSnapshot(receipt)
	if err != nil {
		return err
	}
	detail := projectCreationAuditDetail{Reason: snapshot.Reason, CreationID: receipt.CreationID, ProjectID: receipt.ProjectID, ManagerCount: len(snapshot.Managers), ModelCount: len(snapshot.ModelIDs), InitialRequestIDs: snapshot.InitialRequestIDs}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	eventID, err := id.NewPrefixed("aud")
	if err != nil {
		return err
	}
	value := string(raw)
	return tx.Create(&entity.AuditEvent{ID: eventID, ActorID: actorID, Action: "project.creation.commit", ResourceType: "project", ResourceID: receipt.ProjectID, DetailsJSON: &value}).Error
}

func projectCreationAuditProjection(row entity.AuditEvent) (any, bool) {
	if row.Action != "project.creation.commit" || row.ResourceType != "project" || row.DetailsJSON == nil {
		return nil, false
	}
	var detail projectCreationAuditDetail
	if json.Unmarshal([]byte(*row.DetailsJSON), &detail) != nil || !credentialReplacementRequestID.MatchString(detail.CreationID) || detail.ProjectID != row.ResourceID || !strings.HasPrefix(detail.ProjectID, "prj_") || !safeTeamSessionID(detail.ProjectID) || detail.ManagerCount < 1 || detail.ManagerCount > 1000 || detail.ModelCount < 0 || detail.ModelCount > 1000 || len(detail.InitialRequestIDs) > 3 || !validResourceDescription(detail.Reason) {
		return nil, false
	}
	seen := map[string]bool{}
	for _, requestID := range detail.InitialRequestIDs {
		if !strings.HasPrefix(requestID, "pmr_") || !safeTeamSessionID(requestID) || seen[requestID] {
			return nil, false
		}
		seen[requestID] = true
	}
	return detail, true
}
