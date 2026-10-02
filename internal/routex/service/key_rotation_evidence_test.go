package service

import (
	"testing"
	"time"
)

func TestReplacementKeyCallEvidenceExactValues(t *testing.T) {
	created := time.Now().UTC()
	valid := replacementKeyCallEvidence{RequestID: "req_exact", AttemptRequestID: "req_exact", UserID: "usr_owner", KeyID: "key_exact", CallStatus: "success", AttemptStatus: "success", NativeCompletionEvidence: "completed", StartedAt: created, AttemptNumber: 1}
	if !valid.matches("usr_owner", "", "key_exact", created) {
		t.Fatal("exact native completion rejected")
	}
	for name, mutate := range map[string]func(*replacementKeyCallEvidence){
		"request association": func(e *replacementKeyCallEvidence) { e.AttemptRequestID = "REQ_EXACT" },
		"owner":               func(e *replacementKeyCallEvidence) { e.UserID = "USR_OWNER" },
		"project":             func(e *replacementKeyCallEvidence) { e.ProjectID = "prj_other" },
		"key":                 func(e *replacementKeyCallEvidence) { e.KeyID = "KEY_EXACT" },
		"call case":           func(e *replacementKeyCallEvidence) { e.CallStatus = "SUCCESS" },
		"attempt case":        func(e *replacementKeyCallEvidence) { e.AttemptStatus = "SUCCESS" },
		"marker case":         func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "COMPLETED" },
		"legacy marker":       func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "unknown" },
		"blocked":             func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "blocked" },
		"handoff":             func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "handoff" },
		"incomplete":          func(e *replacementKeyCallEvidence) { e.NativeCompletionEvidence = "incomplete" },
		"canceled":            func(e *replacementKeyCallEvidence) { e.AttemptStatus = "canceled" },
		"failed call":         func(e *replacementKeyCallEvidence) { e.CallStatus = "error" },
		"precreation":         func(e *replacementKeyCallEvidence) { e.StartedAt = created.Add(-time.Microsecond) },
		"legacy ordinal":      func(e *replacementKeyCallEvidence) { e.AttemptNumber = 0 },
		"invalid ordinal":     func(e *replacementKeyCallEvidence) { e.AttemptNumber = 33 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if candidate.matches("usr_owner", "", "key_exact", created) {
				t.Fatal("corrupt or incomplete proof promoted")
			}
		})
	}
	project := valid
	project.UserID, project.ProjectID = "", "prj_owner"
	if !project.matches("", "prj_owner", "key_exact", created) || project.matches("usr_owner", "", "key_exact", created) {
		t.Fatal("project/personal proof attribution crossed scope")
	}
}

func TestReplacementKeyCallEvidenceRejectsInvalidScope(t *testing.T) {
	for _, scope := range []struct {
		user, project, key string
		created            time.Time
	}{
		{"", "", "key_exact", time.Now()},
		{"usr_owner", "prj_owner", "key_exact", time.Now()},
		{"usr_owner", "", "", time.Now()},
		{"usr_owner", "", "key_exact", time.Time{}},
	} {
		// Invalid immutable context must reject before any database lookup.
		if ok, err := hasCompletedReplacementKeyCall(nil, scope.user, scope.project, scope.key, scope.created); ok || err != nil {
			t.Fatal("invalid proof scope accepted")
		}
	}
}
