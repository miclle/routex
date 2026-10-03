package service

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestTeamRolesStrictInputAndEmptyReplacement(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{}`, `{"role_ids":null,"reason":"Clear"}`, `{"role_ids":[1],"reason":"Typed"}`, `{"role_ids":[],"reason":null}`, `{"role_ids":[],"reason":"One","reason":"Two"}`, `{"role_ids":[],"reason":"Clear","user_id":"usr_other"}`, `{"role_ids":[],"role_ids":[],"reason":"Duplicate"}`} {
		var input TeamRoleInput
		if json.Unmarshal([]byte(raw), &input) == nil {
			t.Fatal("accepted ambiguous role assignment", raw)
		}
	}
	var input TeamRoleInput
	if err := json.Unmarshal([]byte(`{"role_ids":[],"reason":"Clear assignment"}`), &input); err != nil || input.RoleIDs == nil || len(input.RoleIDs) != 0 {
		t.Fatal("explicit clear lost", input, err)
	}
}
func TestTeamRolesUnionNeverContributesPlatformDimensions(t *testing.T) {
	roles := []TeamRoleSummary{{ID: "rol_one", TeamActions: []string{"teams.write", "teams.tokens.write", "projects.write", "roles.write"}}, {ID: "rol_two", TeamActions: []string{"teams.models.write", "teams.write", "teams.money.write", "teams.quota_requests.read_all"}}}
	before, _ := json.Marshal(roles)
	if got := teamRoleActionUnion(roles); !reflect.DeepEqual(got, []string{"teams.models.write", "teams.write"}) {
		t.Fatal("Team role expanded its allowlist", got)
	}
	after, _ := json.Marshal(roles)
	if string(before) != string(after) {
		t.Fatal("union mutated role summaries")
	}
	if got := teamRoleActionUnion(nil); got == nil || len(got) != 0 {
		t.Fatal("empty union must be explicit array", got)
	}
}
func TestTeamRolesCandidateCursorBindsActorTargetQueryAndDefinitions(t *testing.T) {
	cursor := encodeTeamRoleCursor("rol_candidate", "usr_admin", "tea_one", "literal%_", "review_one")
	if got, err := decodeTeamRoleCursor(cursor, "usr_admin", "tea_one", "literal%_", "review_one"); err != nil || got != "rol_candidate" {
		t.Fatal(got, err)
	}
	for _, scope := range [][4]string{{"usr_other", "tea_one", "literal%_", "review_one"}, {"usr_admin", "tea_other", "literal%_", "review_one"}, {"usr_admin", "tea_one", "other", "review_one"}, {"usr_admin", "tea_one", "literal%_", "review_changed"}} {
		if _, err := decodeTeamRoleCursor(cursor, scope[0], scope[1], scope[2], scope[3]); err == nil {
			t.Fatal("reused stale/cross-scope role cursor", scope)
		}
	}
}

func TestTeamRolesAssignmentGenerationSurvivesPersistedPrecisionAndClockABA(t *testing.T) {
	previous := time.Date(2026, 10, 3, 12, 0, 0, 123456000, time.UTC)
	for _, now := range []time.Time{previous, previous.Add(-time.Hour), previous.Add(time.Microsecond), previous.Add(time.Second)} {
		next := teamRoleAssignmentGeneration(previous, now)
		if !next.After(previous) || !next.Equal(next.Truncate(time.Millisecond)) {
			t.Fatal("assignment generation failed to advance at portable persisted precision", previous, now, next)
		}
		repeated := teamRoleAssignmentGeneration(next, now)
		if !repeated.After(next) {
			t.Fatal("assignment ABA reused a saved generation", next, repeated)
		}
	}
}
