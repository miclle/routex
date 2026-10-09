package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestConnectionDiagnosticStrictSelectedCredential(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"credential_id":null}`, `{"credential_id":12}`, `{"Credential_id":"crd_target"}`, `{"credential_id":"crd_target","credential_id":"crd_other"}`, `{"credential_id":"crd_target","\u0063redential_id":"crd_other"}`, `{"credential_id":"crd_target","secret":"not-accepted"}`, `{"credential_id":"CRD_TARGET"}`, `{"credential_id":"crd_target "}`, `{"credential_id":"con_target"}`, `{"credential_id":"crd_\ud800"}`, `{"credential_id":"crd_target"} {}`, strings.Repeat(" ", 4*1024+1), string([]byte{'{', '"', 'c', 'r', 'e', 'd', 'e', 'n', 't', 'i', 'a', 'l', '_', 'i', 'd', '"', ':', '"', 255, '"', '}'})} {
		t.Run(raw, func(t *testing.T) {
			var got ConnectionDiagnosticInput
			if json.Unmarshal([]byte(raw), &got) == nil {
				t.Fatal("ambiguous or unbounded selection accepted")
			}
		})
	}
	var got ConnectionDiagnosticInput
	if err := json.Unmarshal([]byte(`{"credential_id":"crd_target"}`), &got); err != nil || got.CredentialID != "crd_target" {
		t.Fatal(got, err)
	}
}

func TestConnectionDiagnosticProofRejectsRebornAndRevisedSelection(t *testing.T) {
	original := connectionDiagnosticSnapshot{review: ConnectionMetadataRecord{ETag: "review"}, credential: entity.ProviderCredential{ID: "crd_target", ConnectionID: "con_target", CreatedAt: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)}, source: "retained-source", transport: "stored-transport"}
	for name, change := range map[string]func(*connectionDiagnosticSnapshot){
		"review":            func(s *connectionDiagnosticSnapshot) { s.review.ETag = "revised-review" },
		"credential_id":     func(s *connectionDiagnosticSnapshot) { s.credential.ID = "crd_other" },
		"credential_parent": func(s *connectionDiagnosticSnapshot) { s.credential.ConnectionID = "con_other" },
		"credential_birth": func(s *connectionDiagnosticSnapshot) {
			s.credential.CreatedAt = s.credential.CreatedAt.Add(time.Microsecond)
		},
		"source":    func(s *connectionDiagnosticSnapshot) { s.source = "replaced-source" },
		"transport": func(s *connectionDiagnosticSnapshot) { s.transport = "replaced-egress" },
	} {
		t.Run(name, func(t *testing.T) {
			current := original
			change(&current)
			if original.same(current) {
				t.Fatal("obsolete selected source accepted")
			}
		})
	}
	if !original.same(original) {
		t.Fatal("unchanged captured proof rejected")
	}
}

func TestConnectionDiagnosticResultHasOnlyTransientPublicFacts(t *testing.T) {
	count := 0
	result := ConnectionDiagnosticResult{ConnectionID: "con_target", CredentialID: "crd_target", Outcome: "passed", Scope: "model_discovery", DiscoveredModelCount: &count, CheckedAt: time.Date(2026, 10, 9, 1, 2, 3, 123456000, time.UTC)}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || len(fields) != 6 {
		t.Fatal(string(raw), err)
	}
	want := map[string]string{"connection_id": `"con_target"`, "credential_id": `"crd_target"`, "outcome": `"passed"`, "scope": `"model_discovery"`, "discovered_model_count": "0", "checked_at": `"2026-10-09T01:02:03.123456Z"`}
	got := map[string]string{}
	for name, value := range fields {
		got[name] = string(value)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}
