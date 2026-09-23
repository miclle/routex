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
	if strings.Contains(string(encoded), "do-not-leak") || !strings.Contains(string(row.Changes), `"before":{"rpm":1`) {
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
}
