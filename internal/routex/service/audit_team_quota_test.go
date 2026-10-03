package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamQuotaAuditProjection(t *testing.T) {
	raw := `{"team_id":"tea_example","applicant_user_id":"usr_example","dimension":"money","target_value":"2.000000000000000001","currency":"USD","step_id":"qst_example","stage":"team_owner","request_status":"pending_quota_admin","reason":"Reviewed increase","submitted_snapshot":{"secret":"must-not-leak"},"credential":"must-not-leak"}`
	row := entity.AuditEvent{Action: "team.quota_request.approve", ResourceType: "team_quota_request", ResourceID: "qrq_example", DetailsJSON: &raw}
	result := auditRecord(row)
	if result.Changes == nil || strings.Contains(string(result.Changes), "must-not-leak") ||
		!strings.Contains(string(result.Changes), `"target_value":"2.000000000000000001"`) ||
		!strings.Contains(string(result.Changes), `"request_status":"pending_quota_admin"`) {
		t.Fatalf("unsafe or imprecise request audit: %s", result.Changes)
	}
	var original map[string]any
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{
		"team_id": "usr_example", "applicant_user_id": "tea_example", "dimension": "rates", "target_value": "-1",
		"currency": "", "step_id": "qrq_example", "stage": "future", "request_status": "pending_team_owner", "reason": "bad\nreason",
	} {
		t.Run(field, func(t *testing.T) {
			modified := make(map[string]any, len(original))
			for key, item := range original {
				modified[key] = item
			}
			modified[field] = value
			encoded, err := json.Marshal(modified)
			if err != nil {
				t.Fatal(err)
			}
			invalid := string(encoded)
			row.DetailsJSON = &invalid
			if auditRecord(row).Changes != nil {
				t.Fatal("invalid request audit exposed")
			}
		})
	}
	row.DetailsJSON = &raw
	row.ResourceType = "team"
	if auditRecord(row).Changes != nil {
		t.Fatal("unexpected resource accepted")
	}
}
