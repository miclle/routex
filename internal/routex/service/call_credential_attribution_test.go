package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestCallCredentialAttributionValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := CallFact{SnapshotID: "cfg_parent", RequestID: "req_attribution", UserID: "usr_historical", Protocol: entity.ProtocolOpenAIChat, Status: "success", StartedAt: now, CompletedAt: now, Attempts: []CallAttempt{{ID: "att_attribution", CredentialID: "crd_historical", SnapshotID: "cfg_attempt", Status: "success", StartedAt: now, CompletedAt: now}}}
	for _, field := range []string{"credential", "snapshot"} {
		for _, value := range []string{"", "historical-ID_1", strings.Repeat("a", 30), strings.Repeat("a", 31), "bad/id", "a\nb", " 名称 ", "a.b", "a\x00b"} {
			fact := valid
			fact.Attempts = append([]CallAttempt(nil), valid.Attempts...)
			if field == "credential" {
				fact.Attempts[0].CredentialID = value
			} else {
				fact.Attempts[0].SnapshotID = value
			}
			want := value == "" || value == "historical-ID_1" || len(value) == 30
			if got := validateCallFact(fact) == nil; got != want {
				t.Errorf("%s %q valid=%v want=%v", field, value, got, want)
			}
		}
	}
}

func TestCallCredentialAttributionLegacyJournalUnknown(t *testing.T) {
	// This is the pre-V32 JSON shape used by durable fact journals. Parsing it
	// must not infer attempt attribution from a parent snapshot or evidence.
	var fact CallFact
	if err := json.Unmarshal([]byte(`{"SnapshotID":"cfg_parent","Attempts":[{"ID":"att_legacy","Status":"success","FinalUsageKnown":true}]}`), &fact); err != nil {
		t.Fatal(err)
	}
	normalizeCallAttemptEvidence(&fact)
	if fact.Attempts[0].CredentialID != "" || fact.Attempts[0].SnapshotID != "" {
		t.Fatal("legacy journal inferred attribution")
	}
	fact.Attempts[0].CredentialID, fact.Attempts[0].SnapshotID = "crd_exact", "cfg_exact"
	encoded, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	var replayed CallFact
	if err := json.Unmarshal(encoded, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Attempts[0].CredentialID != "crd_exact" || replayed.Attempts[0].SnapshotID != "cfg_exact" {
		t.Fatal("journal roundtrip lost exact attribution")
	}
}
