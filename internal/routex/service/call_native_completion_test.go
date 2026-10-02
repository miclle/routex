package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestCallNativeCompletionValidation(t *testing.T) {
	now := time.Now().UTC()
	for _, status := range []string{"success", "error", "canceled"} {
		for _, marker := range []string{"", "unknown", "completed", "handoff", "blocked", "incomplete", "COMPLETED", "Completed", " completed", "completed ", "completed\n", "completed\x00", "future", strings.Repeat("x", 21)} {
			fact := CallFact{RequestID: "req_native_validation", UserID: "usr_historical", Protocol: entity.ProtocolOpenAIChat, Status: status, StartedAt: now, CompletedAt: now, Attempts: []CallAttempt{{ID: "att_native_validation", Status: status, NativeCompletionEvidence: marker, StartedAt: now, CompletedAt: now, FinalUsageKnown: true}}}
			want := marker == "" || callAttemptNativeCompletionEvidence[marker]
			if got := validateCallFact(fact) == nil; got != want {
				t.Errorf("status=%s marker=%q valid=%v want=%v", status, marker, got, want)
			}
			if fact.Attempts[0].NativeCompletionEvidence != marker || fact.Attempts[0].AttemptNumber != 0 {
				t.Fatal("validation mutated caller's attempts")
			}
		}
	}
}

func TestCallNativeCompletionLegacyNormalization(t *testing.T) {
	fact := CallFact{Attempts: []CallAttempt{
		{Status: "success", WorkEvidence: "completed", FinalUsageKnown: true},
		{Status: "error", NativeCompletionEvidence: "completed"},
		{Status: "canceled", NativeCompletionEvidence: "completed"},
	}}
	normalizeCallAttemptEvidence(&fact)
	if fact.Attempts[0].NativeCompletionEvidence != "unknown" {
		t.Fatal("status, work, or usage invented native completion")
	}
	for _, attempt := range fact.Attempts[1:] {
		if attempt.NativeCompletionEvidence != "completed" || attempt.Status == "success" {
			t.Fatal("native marker changed cancellation/failure status or was discarded")
		}
	}
}

func TestCallNativeCompletionJournalCompatibility(t *testing.T) {
	var old CallFact
	if err := json.Unmarshal([]byte(`{"Attempts":[{"Status":"success","WorkEvidence":"completed","FinalUsageKnown":true}]}`), &old); err != nil {
		t.Fatal(err)
	}
	normalizeCallAttemptEvidence(&old)
	if old.Attempts[0].NativeCompletionEvidence != "unknown" {
		t.Fatal("legacy journal was retroactively promoted")
	}
	for _, marker := range []string{"unknown", "completed", "handoff", "blocked", "incomplete"} {
		fact := CallFact{Attempts: []CallAttempt{{Status: "canceled", NativeCompletionEvidence: marker}}}
		encoded, err := json.Marshal(fact)
		if err != nil {
			t.Fatal(err)
		}
		var replayed CallFact
		if err := json.Unmarshal(encoded, &replayed); err != nil {
			t.Fatal(err)
		}
		normalizeCallAttemptEvidence(&replayed)
		if replayed.Attempts[0].NativeCompletionEvidence != marker || replayed.Attempts[0].Status != "canceled" {
			t.Fatal("native outcome or independent status lost in journal roundtrip")
		}
	}
}
