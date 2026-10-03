package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestPersonalModelAuditKeepsOnlyValidatedFacts(t *testing.T) {
	requestID := "mar_01m41800000000000000000001"
	userID := "usr_01m41800000000000000000002"
	modelID := "mdl_01m41800000000000000000003"
	decisionID := "5b7f3b55-c33b-4a89-aef0-768ab4d558de"
	for action, status := range map[string]string{"create": "pending", "approve": "approved", "reject": "rejected", "withdraw": "withdrawn", "cancel": "cancelled"} {
		t.Run(action, func(t *testing.T) {
			details := map[string]any{"applicant_user_id": userID, "model_id": modelID, "model_name": "Public Model", "request_status": status, "decision_id": nil, "action": action, "reason": "Reviewed access", "secret": "must-not-surface"}
			if action != "create" && action != "cancel" {
				details["decision_id"] = decisionID
			}
			encoded, _ := json.Marshal(details)
			raw := string(encoded)
			row := entity.AuditEvent{Action: "personal.model_request." + action, ResourceType: "personal_model_request", ResourceID: requestID, DetailsJSON: &raw}
			projection, ok := personalModelAuditProjection(row)
			if !ok {
				t.Fatal("known recorded fact rejected")
			}
			visible, _ := json.Marshal(projection)
			if strings.Contains(string(visible), "secret") {
				t.Fatal("arbitrary payload exposed")
			}
			details["request_status"] = "unknown"
			encoded, _ = json.Marshal(details)
			raw = string(encoded)
			if _, ok = personalModelAuditProjection(row); ok {
				t.Fatal("unknown status fabricated known fact")
			}
			details["request_status"] = status
			details["model_id"] = strings.ToUpper(modelID)
			encoded, _ = json.Marshal(details)
			raw = string(encoded)
			if _, ok = personalModelAuditProjection(row); ok {
				t.Fatal("noncanonical Model identity accepted")
			}
			details["model_id"] = modelID
			details["reason"] = "unsafe\nreason"
			encoded, _ = json.Marshal(details)
			raw = string(encoded)
			if _, ok = personalModelAuditProjection(row); ok {
				t.Fatal("unvalidated control reason exposed")
			}
		})
	}
	if _, ok := personalModelAuditProjection(entity.AuditEvent{Action: "personal.model_request.create", ResourceType: "personal_model_request", ResourceID: requestID}); ok {
		t.Fatal("missing historical details reconstructed")
	}
}
