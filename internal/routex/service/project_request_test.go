package service

import (
	"reflect"
	"testing"
)

func TestNormalizeProjectRequest(t *testing.T) {
	input := ProjectRequestInput{RequestID: "req_models", ModelIDs: []string{"mdl_b", "mdl_a"}, Reason: "  New workload  "}
	got, err := normalizeProjectRequest(input)
	if err != nil || got.Reason != "New workload" || !reflect.DeepEqual(got.ModelIDs, []string{"mdl_a", "mdl_b"}) {
		t.Fatalf("normalize: %v", err)
	}
	if input.ModelIDs[0] != "mdl_b" {
		t.Fatal("mutated caller input")
	}
	for _, bad := range []ProjectRequestInput{
		{RequestID: "req_models", Reason: "Reason"},
		{RequestID: "req_models", ModelIDs: []string{"mdl_a", "mdl_a"}, Reason: "Reason"},
		{RequestID: "req_models", ModelIDs: []string{"mdl_a"}, Reason: " \t "},
		{RequestID: "invalid request id", ModelIDs: []string{"mdl_a"}, Reason: "Reason"},
		{RequestID: "req_models", ModelIDs: []string{"mdl_a"}, Reason: "Reason\x00"},
	} {
		if _, err := normalizeProjectRequest(bad); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}
func TestProjectRequestDecisionRequiresExplicitAction(t *testing.T) {
	for _, input := range []ProjectRequestDecision{{Action: "reject"}, {Action: "MODEL_ACCESS"}, {Action: ""}} {
		if _, err := projectRequestDecisionStatus(input); err == nil {
			t.Fatal("invalid decision accepted")
		}
	}
	for _, action := range []string{"approve", "reject", "withdraw"} {
		if _, err := projectRequestDecisionStatus(ProjectRequestDecision{Action: action, Reason: "Reason"}); err != nil {
			t.Fatal(err)
		}
	}
}
