package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamModelAuditOnlyExposesValidatedHistoricalFacts(t *testing.T) {
	for action, status := range map[string]string{"create": "pending", "approve": "approved", "reject": "rejected", "withdraw": "withdrawn", "cancel": "cancelled"} {
		t.Run(action, func(t *testing.T) {
			details := map[string]any{"team_id": "tem_legacy", "team_name": "Shared Team", "applicant_user_id": "usr_legacy", "model_id": "mdl_legacy", "model_name": "Public Model", "request_status": status, "decision_id": nil, "action": action, "reason": "Reviewed request", "secret": "private", "applicant_membership_id": "tmm_private"}
			if action != "create" && action != "cancel" {
				details["decision_id"] = "5b7f3b55-c33b-4a89-aef0-768ab4d558de"
			}
			if action == "withdraw" {
				details["reason"] = ""
			}
			row := entity.AuditEvent{Action: "team.model_request." + action, ResourceType: "team_model_request", ResourceID: "tmr_01m41800000000000000000001"}
			encode := func() {
				encoded, err := json.Marshal(details)
				if err != nil {
					t.Fatal(err)
				}
				raw := string(encoded)
				row.DetailsJSON = &raw
			}
			encode()
			projection, ok := teamModelRequestAuditProjection(row)
			if !ok {
				t.Fatal("recorded safe historical identities rejected")
			}
			visible, _ := json.Marshal(projection)
			if strings.Contains(string(visible), "private") || strings.Contains(string(visible), "membership") {
				t.Fatal("arbitrary/private metadata exposed")
			}
			details["request_status"] = "unknown"
			encode()
			if _, ok := teamModelRequestAuditProjection(row); ok {
				t.Fatal("unknown status projected")
			}
			details["request_status"] = status
			details["reason"] = "unsafe\nreason"
			encode()
			if _, ok := teamModelRequestAuditProjection(row); ok {
				t.Fatal("unsafe reason projected")
			}
			details["reason"] = ""
			details["team_name"] = "unsafe\nname"
			encode()
			if _, ok := teamModelRequestAuditProjection(row); ok {
				t.Fatal("unsafe Team name projected")
			}
		})
	}
	if _, ok := teamModelRequestAuditProjection(entity.AuditEvent{Action: "team.model_request.create", ResourceType: "team_model_request", ResourceID: "tmr_01m41800000000000000000001"}); ok {
		t.Fatal("missing historical facts fabricated")
	}
}
