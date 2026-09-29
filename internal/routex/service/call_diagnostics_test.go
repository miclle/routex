package service

import (
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestCallAttemptDiagnosticsValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := CallFact{
		RequestID: "req_diagnostics", UserID: "usr_diagnostics", KeyID: "key_diagnostics",
		ModelID: "mdl_diagnostics", ModelName: "diagnostics", ProviderModelID: "pmd_diagnostics",
		ConnectionID: "con_diagnostics", RouteStopReason: "unsafe_to_replay",
		Protocol: entity.ProtocolOpenAIChat, Status: "error", StartedAt: now, CompletedAt: now,
		Attempts: []CallAttempt{{
			ID: "att_diagnostics", ProviderModelID: "pmd_diagnostics", ConnectionID: "con_diagnostics",
			AttemptNumber: 1, Status: "error", FailureClass: "connection_failure", WorkEvidence: "unknown",
			EvidenceCode: "transport_ambiguous", StartedAt: now, CompletedAt: now,
		}},
	}
	if err := validateCallFact(valid); err != nil {
		t.Fatalf("valid diagnostics rejected: %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*CallFact)
	}{
		{"stop reason", func(f *CallFact) { f.RouteStopReason = "provider-secret" }},
		{"attempt number", func(f *CallFact) { f.Attempts[0].AttemptNumber = 33 }},
		{"failure class", func(f *CallFact) { f.Attempts[0].FailureClass = "provider-secret" }},
		{"work evidence", func(f *CallFact) { f.Attempts[0].WorkEvidence = "provider-secret" }},
		{"evidence code", func(f *CallFact) { f.Attempts[0].EvidenceCode = "provider-secret" }},
		{"success mismatch", func(f *CallFact) { f.Attempts[0].FailureClass = "success" }},
		{"unsafe output", func(f *CallFact) { f.Attempts[0].WorkEvidence, f.Attempts[0].OutputStarted = "not_sent", true }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fact := valid
			fact.Attempts = append([]CallAttempt(nil), valid.Attempts...)
			test.mutate(&fact)
			if err := validateCallFact(fact); err == nil {
				t.Fatal("invalid diagnostics accepted")
			}
		})
	}
}

func TestCallAttemptDiagnosticsLegacyNormalization(t *testing.T) {
	fact := CallFact{Attempts: []CallAttempt{{Status: "error"}, {Status: "success"}}}
	normalizeCallAttemptEvidence(&fact)
	if fact.Attempts[0].AttemptNumber != 1 || fact.Attempts[0].FailureClass != "permanent_failure" || fact.Attempts[0].WorkEvidence != "unknown" {
		t.Fatalf("legacy failure normalization unsafe: %+v", fact.Attempts[0])
	}
	if fact.Attempts[1].AttemptNumber != 2 || fact.Attempts[1].FailureClass != "success" || fact.Attempts[1].WorkEvidence != "completed" {
		t.Fatalf("legacy success normalization wrong: %+v", fact.Attempts[1])
	}
}
