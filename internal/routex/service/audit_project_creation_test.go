package service

import (
	"encoding/json"
	"github.com/miclle/routex/internal/routex/entity"
	"strings"
	"testing"
)

func TestProjectCreationAuditProjectionIsBounded(t *testing.T) {
	valid := projectCreationAuditDetail{CreationID: "11111111-1111-4111-8111-111111111145", ProjectID: "prj_audit", ManagerCount: 1, ModelCount: 2, Reason: "Reviewed initial resources", InitialRequestIDs: []string{"pmr_initial"}}
	encoded, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	row := entity.AuditEvent{Action: "project.creation.commit", ResourceType: "project", ResourceID: valid.ProjectID, DetailsJSON: &raw}
	projection, ok := projectCreationAuditProjection(row)
	if !ok {
		t.Fatal("valid typed receipt audit omitted")
	}
	body, err := json.Marshal(projection)
	if err != nil || !strings.Contains(string(body), valid.CreationID) {
		t.Fatal("typed creation reference lost", err)
	}
	raw = raw[:len(raw)-1] + `,"unrecorded_private_json":{"secret":"must_not_render"}}`
	projection, ok = projectCreationAuditProjection(row)
	if !ok {
		t.Fatal("unknown historical keys should not enter projection")
	}
	body, err = json.Marshal(projection)
	if err != nil || strings.Contains(string(body), "must_not_render") {
		t.Fatal("arbitrary JSON exposed", err)
	}
	for _, mode := range []string{"wrong_action", "wrong_resource", "wrong_target", "wrong_creation", "negative_count", "too_many_requests", "non_request_id", "duplicate_requests", "invalid_reason", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			detail := valid
			value := row
			switch mode {
			case "wrong_action":
				value.Action = "resource.create"
			case "wrong_resource":
				value.ResourceType = "team"
			case "wrong_target":
				value.ResourceID = "prj_other"
			case "wrong_creation":
				detail.CreationID = "other"
			case "negative_count":
				detail.ModelCount = -1
			case "too_many_requests":
				detail.InitialRequestIDs = []string{"pmr_1", "pmr_2", "pmr_3", "pmr_4"}
			case "non_request_id":
				detail.InitialRequestIDs = []string{"usr_other"}
			case "duplicate_requests":
				detail.InitialRequestIDs = []string{"pmr_1", "pmr_1"}
			case "invalid_reason":
				detail.Reason = strings.Repeat("a", 2001)
			}
			body, err := json.Marshal(detail)
			if err != nil {
				t.Fatal(err)
			}
			raw := string(body)
			if mode == "malformed" {
				raw = "{"
			}
			value.DetailsJSON = &raw
			if _, ok := projectCreationAuditProjection(value); ok {
				t.Fatal("invalid stored audit accepted")
			}
		})
	}
}
