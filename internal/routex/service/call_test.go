package service

import (
	"testing"
	"time"
)

func TestCallClassifications(t *testing.T) {
	for _, status := range []string{"success", "error", "canceled"} {
		if !validCallStatus(status) {
			t.Errorf("status %s rejected", status)
		}
	}
	for _, status := range []string{"", "pending", "arbitrary upstream body"} {
		if validCallStatus(status) {
			t.Errorf("unexpected status %s accepted", status)
		}
	}
	if safeCallError("Bearer supersecret") != "upstream_error" || safeCallError("upstream_timeout") != "upstream_timeout" {
		t.Fatal("call error classification exposed arbitrary data")
	}
	for _, value := range []string{"req_01abc", "att_test"} {
		if !safeCallID.MatchString(value) {
			t.Errorf("valid ID rejected: %s", value)
		}
	}
	for _, value := range []string{"", "../../secret", "request\nheader"} {
		if safeCallID.MatchString(value) {
			t.Errorf("invalid ID accepted: %q", value)
		}
	}
}

func TestCallHasOneAttribution(t *testing.T) {
	now := time.Now()
	for _, owner := range []struct {
		user, project string
		valid         bool
	}{{"usr_test", "", true}, {"", "prj_test", true}, {"", "", false}, {"usr_test", "prj_test", false}} {
		fact := CallFact{RequestID: "req_attribution", UserID: owner.user, ProjectID: owner.project, Protocol: "openai_chat", Status: "success", StartedAt: now, CompletedAt: now}
		if (validateCallFact(fact) == nil) != owner.valid {
			t.Errorf("incorrect attribution result for %+v", owner)
		}
	}
}
