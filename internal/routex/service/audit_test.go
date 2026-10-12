package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestAuditFilterValidation(t *testing.T) {
	for _, f := range []AuditFilter{{Range: "all"}, {Category: "unknown"}, {Cursor: "bad cursor"}, {Cursor: "invalid"}, {Cursor: "usr_01m36yee4gkbns18pfcqqc75a3"}, {Cursor: "aud_zzzzzzzzzzzzzzzzzzzzzzzzzz"}, {Query: strings.Repeat("界", 101)}, {Query: "bad\x00value"}} {
		if _, _, err := validateAuditFilter(f); err == nil {
			t.Fatalf("accepted invalid filter %+v", f)
		}
	}
	f, d, err := validateAuditFilter(AuditFilter{Query: "  %_!Name  ", Cursor: "aud_01m36yee4gkbns18pfcqqc75a3", Range: "24h", Category: "models"})
	if err != nil || f.Query != "%_!Name" || d.Hours() != 24 {
		t.Fatal("invalid normalized query")
	}
}
func TestAuditDetailsAllowlist(t *testing.T) {
	raw := `{"before":{"rpm":1,"secret":"do-not-leak"},"after":{"rpm":2,"password":"do-not-leak"},"reason":"reviewed","etag":"revision","credential":"do-not-leak"}`
	row := auditRecord(entity.AuditEvent{Action: "limits.update", DetailsJSON: &raw})
	encoded, _ := json.Marshal(row)
	if strings.Contains(string(encoded), "do-not-leak") || !strings.Contains(string(row.Changes), `"rpm":1`) || !strings.Contains(string(row.Changes), `"rpm":2`) {
		t.Fatalf("unexpected safe projection %s", encoded)
	}
	if row.Source != nil || row.IP != nil || row.RequestID != nil || row.Result != "committed" {
		t.Fatal("historical metadata was fabricated")
	}
	row = auditRecord(entity.AuditEvent{Action: "credential.update", DetailsJSON: &raw})
	if row.Changes != nil {
		t.Fatal("unknown detail schema exposed")
	}
	raw = "{broken"
	row = auditRecord(entity.AuditEvent{Action: "limits.update", DetailsJSON: &raw})
	if row.Changes != nil {
		t.Fatal("malformed detail exposed")
	}
	raw = `{"revision":7,"hostname":"do-not-leak","database_url":"do-not-leak"}`
	row = auditRecord(entity.AuditEvent{Action: "system.instance.cleanup", ResourceID: "ins_retired", DetailsJSON: &raw})
	encoded, _ = json.Marshal(row)
	if strings.Contains(string(encoded), "do-not-leak") || !strings.Contains(string(row.Changes), `"revision":7`) || row.ResourceID != "ins_retired" {
		t.Fatalf("unexpected system cleanup projection %s", encoded)
	}
	raw = `{"revision":0}`
	if auditRecord(entity.AuditEvent{Action: "system.instance.cleanup", DetailsJSON: &raw}).Changes != nil {
		t.Fatal("invalid cleanup revision exposed")
	}
}

func TestCredentialMetadataAuditProjection(t *testing.T) {
	raw := `{"before":{"name":"Original","priority":10,"ciphertext":"do-not-leak"},"after":{"name":"Renamed","priority":0,"secret":"do-not-leak","enabled":true},"reason":"Reviewed ordering","provider_response":"do-not-leak"}`
	record := auditRecord(entity.AuditEvent{Action: "credential.metadata.update", DetailsJSON: &raw})
	if string(record.Changes) != `{"before":{"name":"Original","priority":10},"after":{"name":"Renamed","priority":0},"reason":"Reviewed ordering"}` {
		t.Fatalf("unexpected metadata projection %s", record.Changes)
	}
	if record.Source != nil || record.IP != nil || record.RequestID != nil {
		t.Fatal("metadata projection invented unrecorded request facts")
	}
	for _, invalid := range []string{
		`{"before":{"name":"Original"},"after":{"name":"Renamed","priority":0},"reason":"Reviewed"}`,
		`{"before":{"name":"Original","priority":0},"after":{"name":"Renamed","priority":10001},"reason":"Reviewed"}`,
		`{"before":{"name":"Original","priority":0},"after":{"name":"Renamed","priority":0},"reason":""}`,
		`{"before":{"name":"Original","priority":0},"after":{"name":"Renamed","priority":0},"reason":"bad\nreason"}`,
		`{"before":{"name":"Original","priority":0},"after":{"name":"Renamed","priority":0},"reason":42}`,
	} {
		if auditRecord(entity.AuditEvent{Action: "credential.metadata.update", DetailsJSON: &invalid}).Changes != nil {
			t.Fatalf("accepted malformed metadata audit %s", invalid)
		}
	}
}

func TestCredentialDeletionAuditProjection(t *testing.T) {
	credentialID := "crd_01m36yee4gkbns18pfcqqc75a3"
	raw := `{"before":{"id":"` + credentialID + `","connection_id":"con_recorded","name":"Retired","priority":0,"ciphertext":"do-not-leak"},"after":{"absent":true,"secret":"do-not-leak"},"reason":"Retired configuration","request_body":"do-not-leak"}`
	record := auditRecord(entity.AuditEvent{Action: "credential.delete", ResourceID: credentialID, DetailsJSON: &raw})
	if record.Changes == nil || strings.Contains(string(record.Changes), "do-not-leak") || !strings.Contains(string(record.Changes), `"absent":true`) {
		t.Fatalf("unsafe or missing deletion audit %s", record.Changes)
	}
	for _, invalid := range []string{
		strings.Replace(raw, `"absent":true`, `"absent":false`, 1),
		strings.Replace(raw, `"priority":0`, `"priority":-1`, 1),
		strings.Replace(raw, `"reason":"Retired configuration"`, `"reason":""`, 1),
		strings.Replace(raw, credentialID, "crd_unknown", 1),
	} {
		if auditRecord(entity.AuditEvent{Action: "credential.delete", ResourceID: credentialID, DetailsJSON: &invalid}).Changes != nil {
			t.Fatal("accepted malformed deletion audit")
		}
	}
	if auditRecord(entity.AuditEvent{Action: "credential.delete", ResourceID: "another_resource", DetailsJSON: &raw}).Changes != nil {
		t.Fatal("deletion audit attributed to an unrelated resource")
	}
}

func TestCredentialReplacementAuditProjection(t *testing.T) {
	resultID := "crd_01m36yee4gkbns18pfcqqc75a3"
	sourceID := "crd_01m36yee4gkbns18pfcqqc75a4"
	raw := `{"source_id":"` + sourceID + `","connection_id":"con_recorded","name":"Replacement","priority":0,"reason":"Reviewed replacement","secret":"do-not-leak","ciphertext":"do-not-leak","request_hash":"do-not-leak","request_body":"do-not-leak"}`
	row := entity.AuditEvent{Action: "credential.replacement.create", ResourceType: "credential", ResourceID: resultID, DetailsJSON: &raw}
	record := auditRecord(row)
	expected := `{"before":{"absent":true},"after":{"source_id":"` + sourceID + `","connection_id":"con_recorded","name":"Replacement","priority":0},"reason":"Reviewed replacement"}`
	if string(record.Changes) != expected {
		t.Fatalf("unexpected replacement projection %s", record.Changes)
	}
	if record.Source != nil || record.IP != nil || record.RequestID != nil {
		t.Fatal("replacement projection invented request facts")
	}
	for _, invalid := range []string{
		strings.Replace(raw, sourceID, resultID, 1),
		strings.Replace(raw, sourceID, "crd_unknown", 1),
		strings.Replace(raw, `"priority":0`, `"priority":10001`, 1),
		strings.Replace(raw, `"priority":0,`, "", 1),
		strings.Replace(raw, `"reason":"Reviewed replacement"`, `"reason":"bad\nreason"`, 1),
		strings.Replace(raw, `"connection_id":"con_recorded"`, `"connection_id":""`, 1),
		strings.Replace(raw, `"name":"Replacement"`, `"name":""`, 1),
	} {
		row.DetailsJSON = &invalid
		if auditRecord(row).Changes != nil {
			t.Fatal("malformed replacement audit exposed")
		}
	}
	row.DetailsJSON = &raw
	row.ResourceType = "api_key"
	if auditRecord(row).Changes != nil {
		t.Fatal("replacement audit attributed to wrong resource type")
	}
	row.ResourceType = "credential"
	row.ResourceID = "crd_unknown"
	if auditRecord(row).Changes != nil {
		t.Fatal("replacement audit attributed to invalid result")
	}
}

func TestCredentialRetirementAuditProjection(t *testing.T) {
	sourceID := "crd_01m36yee4gkbns18pfcqqc75a3"
	replacementID := "crd_01m36yee4gkbns18pfcqqc75a4"
	raw := `{"source_id":"` + sourceID + `","replacement_id":"` + replacementID + `","connection_id":"con_recorded","request_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","snapshot_id":"cfg_01m36yee4gkbns18pfcqqc75a3","evidence_attempt_id":"att_native_completed","reason":"Reviewed cutover","before":{"enabled":true,"secret":"do-not-leak"},"after":{"enabled":false},"request_body":"do-not-leak","key":"do-not-leak"}`
	row := entity.AuditEvent{Action: "credential.retire", ResourceType: "credential", ResourceID: sourceID, DetailsJSON: &raw}
	record := auditRecord(row)
	if record.Changes == nil || strings.Contains(string(record.Changes), "do-not-leak") || !strings.Contains(string(record.Changes), `"evidence_attempt_id":"att_native_completed"`) {
		t.Fatalf("unsafe or absent retirement audit: %s", record.Changes)
	}
	if record.Source != nil || record.IP != nil || record.RequestID != nil {
		t.Fatal("retirement audit invented unrecorded inference or network metadata")
	}
	for _, invalid := range []string{
		strings.Replace(raw, replacementID, sourceID, 1),
		strings.Replace(raw, `"enabled":true`, `"enabled":false`, 1),
		strings.Replace(raw, `"after":{"enabled":false}`, `"after":{}`, 1),
		strings.Replace(raw, `"reason":"Reviewed cutover"`, `"reason":"bad\nreason"`, 1),
		strings.Replace(raw, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "not-a-request-id", 1),
		strings.Replace(raw, "att_native_completed", "invalid attempt", 1),
		strings.Replace(raw, "cfg_01m36yee4gkbns18pfcqqc75a3", "cfg_invalid", 1),
	} {
		row.DetailsJSON = &invalid
		if auditRecord(row).Changes != nil {
			t.Fatal("accepted malformed retirement audit")
		}
	}
	row.DetailsJSON = &raw
	row.ResourceID = replacementID
	if auditRecord(row).Changes != nil {
		t.Fatal("retirement audit attributed to replacement")
	}
}

func TestTeamLimitAuditScopeIsBoundedAndTyped(t *testing.T) {
	raw := `{"team_id":"tea_one","before":{"ip_mode":"none","rpm":1},"after":{"ip_mode":"none","rpm":2,"secret":"do-not-leak"},"reason":"Reviewed","etag":"lim_one","ciphertext":"do-not-leak"}`
	row := auditRecord(entity.AuditEvent{Action: "limits.update", ResourceType: "team_member", ResourceID: "usr_one", DetailsJSON: &raw})
	if row.Changes == nil || !strings.Contains(string(row.Changes), `"team_id":"tea_one"`) || strings.Contains(string(row.Changes), "do-not-leak") {
		t.Fatal("child audit lost Team provenance or leaked unrelated fields", string(row.Changes))
	}
	raw = strings.Replace(raw, "tea_one", "https://secret.invalid", 1)
	row = auditRecord(entity.AuditEvent{Action: "limits.update", ResourceType: "team_member", ResourceID: "usr_one", DetailsJSON: &raw})
	if row.Changes != nil {
		t.Fatal("arbitrary Team scope exposed")
	}
}

func TestRuntimePublicationAuditProjection(t *testing.T) {
	const requestID = "6f739b12-12ce-4a20-8da7-000000000001"
	raw := `{"request_id":"` + requestID + `","reason":"Reviewed publication 原因","outcome":"claimed"}`
	row := entity.AuditEvent{ActorID: "usr_01m36yee4gkbns18pfcqqc75a3", Action: "runtime.publication.claim", ResourceType: "system_instance", ResourceID: "ins_01m36yee4gkbns18pfcqqc75a3", DetailsJSON: &raw}
	for _, pair := range []struct{ action, outcome string }{{"runtime.publication.claim", "claimed"}, {"runtime.publication.finalize", "refresh_returned_nil"}, {"runtime.publication.finalize", "refresh_returned_error"}} {
		t.Run(pair.outcome, func(t *testing.T) {
			value := strings.Replace(raw, `"claimed"`, `"`+pair.outcome+`"`, 1)
			event := row
			event.Action = pair.action
			event.DetailsJSON = &value
			record := auditRecord(event)
			want := `{"kind":"runtime_publication","command_request_id":"` + requestID + `","reason":"Reviewed publication 原因","outcome":"` + pair.outcome + `"}`
			if string(record.Changes) != want || record.Result != "committed" || record.RequestID != nil || record.Source != nil || record.IP != nil {
				t.Fatalf("incorrect historical projection: %s", record.Changes)
			}
		})
	}
	invalid := []string{
		raw + `{}`, `{broken`, `[]`, `null`,
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":"refresh_returned_nil"`, 1),
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":"pending"`, 1),
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":null`, 1),
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":true`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":""`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":" leading"`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":"trailing "`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":"bad\nreason"`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":"\ud800"`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":42`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因"`, `"reason":"`+strings.Repeat("界", 1001)+`"`, 1),
		strings.Replace(raw, requestID, strings.ToUpper(requestID), 1),
		strings.Replace(raw, requestID, "6f739b12-12ce-5a20-8da7-000000000001", 1),
		strings.Replace(raw, `"request_id":"`+requestID+`"`, `"request_id":null`, 1),
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":"claimed","secret":"do-not-leak"`, 1),
		strings.Replace(raw, `"outcome":"claimed"`, `"outcome":"claimed","outcome":"claimed"`, 1),
		strings.Replace(raw, `"reason"`, `"Reason"`, 1),
		strings.Replace(raw, `"reason":"Reviewed publication 原因",`, "", 1),
		strings.Replace(raw, "Reviewed publication 原因", string([]byte{255}), 1),
		strings.Repeat(" ", 64*1024) + raw,
	}
	for i, value := range invalid {
		event := row
		event.DetailsJSON = &value
		if got := auditRecord(event); got.Changes != nil || got.RequestID != nil {
			t.Fatalf("exposed invalid publication detail %d", i)
		}
	}
	for _, mutate := range []func(*entity.AuditEvent){
		func(e *entity.AuditEvent) { e.Action = "runtime.publication.finalize" },
		func(e *entity.AuditEvent) { e.Action = "runtime.publication.unknown" },
		func(e *entity.AuditEvent) { e.ResourceType = "model" },
		func(e *entity.AuditEvent) { e.ResourceID = "ins_invalid" },
		func(e *entity.AuditEvent) { e.ActorID = "usr_invalid space" },
	} {
		event := row
		mutate(&event)
		if auditRecord(event).Changes != nil {
			t.Fatal("exposed wrongly attributed publication detail")
		}
	}
	// Preserve the writer's complete 1000-rune bound without byte truncation.
	boundary := strings.Replace(raw, "Reviewed publication 原因", strings.Repeat("界", 1000), 1)
	row.DetailsJSON = &boundary
	if auditRecord(row).Changes == nil {
		t.Fatal("redacted valid bounded Unicode reason")
	}
}
