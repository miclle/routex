package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestTeamModelCandidateAndWorkspaceExposeOnlyPurposeFacts(t *testing.T) {
	for _, value := range []struct {
		value any
		keys  []string
	}{
		{TeamModelRequestTeam{}, []string{"id", "name", "membership_id"}},
		{TeamModelRequestCandidate{}, []string{"id", "name", "team_id", "status", "created_at", "protocols", "input_capabilities", "team_granted", "pending_request", "own_pending_request_id", "review_etag"}},
		{TeamModelRequestWorkspace{}, []string{"team_id", "name", "status", "models", "model_count", "can_review_requests"}},
	} {
		data, err := json.Marshal(value.value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != len(value.keys) {
			t.Fatal("purpose response exposed private Team/member/catalog facts", string(data))
		}
		for _, key := range value.keys {
			if _, ok := fields[key]; !ok {
				t.Fatal("declared purpose fact missing", key)
			}
		}
	}
	data, _ := json.Marshal(TeamModelRequestDetail{ApplicationStatus: "unavailable", AllowedActions: []string{}})
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	for _, key := range []string{"current_team", "current_model", "current_membership_matches", "current_granted", "runtime_applied"} {
		if string(fields[key]) != "null" {
			t.Fatal("unknown live authority became a false current fact", key, string(data))
		}
	}
}
func TestTeamModelCandidatesUseLiteralBoundedSearchAndLegacyCursors(t *testing.T) {
	filter, pattern, err := normalizeTeamModelCandidateFilter(TeamModelRequestCandidateFilter{Query: "  A%_!  ", Cursor: "mdl_legacy"})
	if err != nil || pattern != "%a!%!_!!%" || filter.Limit != 50 || filter.Cursor != "mdl_legacy" {
		t.Fatal("candidate search expanded wildcards or broke stored resource cursor", filter, pattern, err)
	}
	for _, invalid := range []TeamModelRequestCandidateFilter{{Query: strings.Repeat("a", 201)}, {Query: string([]byte{0xff})}, {Limit: 51}, {Limit: -1}, {Cursor: "mdl_legacy "}, {Cursor: strings.Repeat("a", 31)}} {
		if _, _, err := normalizeTeamModelCandidateFilter(invalid); err == nil {
			t.Fatal("unsafe candidate query accepted", invalid)
		}
	}
	if !reflect.DeepEqual(filter, TeamModelRequestCandidateFilter{Query: "  A%_!  ", Cursor: "mdl_legacy", Limit: 50}) {
		t.Fatal("literal candidate query intent changed", filter)
	}
}
