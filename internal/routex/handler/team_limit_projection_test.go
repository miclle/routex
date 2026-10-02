package handler

import (
	"encoding/json"
	"testing"
)

func TestTeamLimitWorkspaceProjection(t *testing.T) {
	record := TeamResponse{ResourceLimitWorkspaceOnly: true, ID: "tea_scope", Name: "Team", Description: "Scoped policy workspace", Status: "active", Members: []ResourcePersonResponse{{UserID: "usr_private", Email: "private@example.invalid"}}, ModelIDs: []string{"mdl_private"}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 5 || string(fields["resource_limit_workspace_only"]) != "true" {
		t.Fatal("minimal Team limit projection", string(raw))
	}
	for _, key := range []string{"members", "model_ids", "created_at"} {
		if _, exists := fields[key]; exists {
			t.Fatal("limits-only authority exposed resource detail", key)
		}
	}
	record.ResourceLimitWorkspaceOnly = false
	raw, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["members"]; !exists {
		t.Fatal("full Team response lost authorized relationships")
	}
}
