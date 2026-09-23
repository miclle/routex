package service

import "testing"

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
