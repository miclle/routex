package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestTeamRoleAuditProjection(t *testing.T) {
	raw := `{"team_id":"tea_example","before":{"role_ids":["rol_admin"],"team_actions":["teams.write","teams.models.write"],"secret":"hidden"},"after":{"role_ids":[],"team_actions":[]},"reason":"Remove Team delegation","credential":"hidden"}`
	row := entity.AuditEvent{Action: "team.roles.replace", ResourceType: "teams", ResourceID: "tea_example", DetailsJSON: &raw}
	changes := auditRecord(row).Changes
	if changes == nil || strings.Contains(string(changes), "hidden") ||
		!strings.Contains(string(changes), `"role_ids":["rol_admin"]`) || !strings.Contains(string(changes), `"after":{"role_ids":[],"team_actions":[]}`) {
		t.Fatalf("unsafe or incomplete Team role audit: %s", changes)
	}
	var valid map[string]any
	if err := json.Unmarshal([]byte(raw), &valid); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{
		"team_id": "tea_other",
		"reason":  "invalid\nreason",
		"before":  map[string]any{"role_ids": []string{"usr_other"}, "team_actions": []string{}},
		"after":   map[string]any{"role_ids": []string{"rol_admin"}, "team_actions": []string{"teams.tokens.write"}},
	} {
		t.Run(field, func(t *testing.T) {
			modified := make(map[string]any, len(valid))
			for key, item := range valid {
				modified[key] = item
			}
			modified[field] = value
			encoded, err := json.Marshal(modified)
			if err != nil {
				t.Fatal(err)
			}
			malformed := string(encoded)
			row.DetailsJSON = &malformed
			if auditRecord(row).Changes != nil {
				t.Fatal("malformed role audit exposed")
			}
		})
	}
	for _, values := range []teamRoleAuditValues{
		{},
		{RoleIDs: []string{"rol_admin", "rol_admin"}, TeamActions: []string{}},
		{RoleIDs: []string{"rol_admin"}, TeamActions: []string{"teams.write", "teams.write"}},
		{RoleIDs: []string{}, TeamActions: []string{"teams.write"}},
	} {
		if validTeamRoleAuditValues(values) {
			t.Fatal("invalid role set accepted", values)
		}
	}
}
