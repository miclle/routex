package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/miclle/routex/internal/routex/entity"
)

func TestDefaultLimitAuditProjection(t *testing.T) {
	unlimited := `{"tokens_5h":null,"tokens_7d":null,"tokens_month":null,"tpm":null,"money_month":null,"currency":"","rpm":null,"concurrency":null}`
	ipPolicy := strings.TrimSuffix(unlimited, "}") + `,"ip_mode":"allowlist","ip_ranges":["127.0.0.1/32"]}`
	policy := `{"tokens_5h":0,"tokens_7d":null,"tokens_month":100,"tpm":20,"money_month":"0.001","currency":"USD","rpm":1,"concurrency":2,"secret":"hidden"}`
	review := strings.Repeat("a", 64)
	for _, fixture := range []struct {
		name, action, kind, id, raw string
	}{
		{"rule", "limits.defaults.update", "default_limit", "user", `{"before":` + unlimited + `,"after":` + policy + `,"rule_etag":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","reason":"Reviewed future rules","secret":"hidden"}`},
		{"apply", "limits.default.apply", "team", "tea_example", `{"after":` + policy + `,"default_rule_etag":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","secret":"hidden"}`},
		{"reset", "limits.default.reset", "user", "usr_example", `{"before":` + ipPolicy + `,"after":` + strings.TrimSuffix(ipPolicy, "}") + `,"secret":"hidden"},"default_rule_etag":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","reset_review_etag":"` + review + `","reason":"Reviewed reset","secret":"hidden"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			row := entity.AuditEvent{Action: fixture.action, ResourceType: fixture.kind, ResourceID: fixture.id, DetailsJSON: &fixture.raw}
			projected := auditRecord(row).Changes
			if len(projected) == 0 || strings.Contains(string(projected), "secret") || strings.Contains(string(projected), "hidden") {
				t.Fatalf("unsafe or missing default policy projection: %s", projected)
			}
			if fixture.name == "reset" && !strings.Contains(string(projected), "127.0.0.1/32") {
				t.Fatal("reset audit discarded preserved IP policy")
			}
		})
	}
}

func TestDefaultLimitAuditRejectsMalformedFacts(t *testing.T) {
	review := strings.Repeat("a", 64)
	ipPolicy := `{"tokens_5h":null,"tokens_7d":null,"tokens_month":0,"tpm":null,"money_month":null,"currency":"","rpm":null,"concurrency":null,"ip_mode":"allowlist","ip_ranges":["127.0.0.1/32"]}`
	base := `{"before":` + ipPolicy + `,"after":` + ipPolicy + `,"default_rule_etag":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","reset_review_etag":"` + review + `","reason":"Reviewed reset"}`
	var original map[string]any
	if err := json.Unmarshal([]byte(base), &original); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		name, field string
		value       any
	}{
		{"missing before", "before", nil},
		{"missing after", "after", nil},
		{"unsafe integer", "after", map[string]any{"tokens_month": 9007199254740992}},
		{"invalid decimal", "after", map[string]any{"money_month": "1e4", "currency": "USD"}},
		{"IP removal", "after", map[string]any{"ip_mode": "none", "ip_ranges": []string{}}},
		{"IP replacement", "after", map[string]any{"ip_mode": "allowlist", "ip_ranges": []string{"192.0.2.0/24"}}},
		{"missing provenance", "default_rule_etag", ""},
		{"unbounded provenance", "default_rule_etag", strings.Repeat("a", 65)},
		{"malformed review", "reset_review_etag", "qpl_old"},
		{"empty reason", "reason", ""},
		{"control reason", "reason", "reset\nsecret"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			modified := make(map[string]any, len(original))
			for field, value := range original {
				modified[field] = value
			}
			if changes, ok := fixture.value.(map[string]any); ok && fixture.field == "after" {
				originalPolicy := original["after"].(map[string]any)
				replacement := make(map[string]any, len(originalPolicy))
				for field, value := range originalPolicy {
					replacement[field] = value
				}
				for field, value := range changes {
					replacement[field] = value
				}
				modified[fixture.field] = replacement
			} else {
				modified[fixture.field] = fixture.value
			}
			encoded, err := json.Marshal(modified)
			if err != nil {
				t.Fatal(err)
			}
			raw := string(encoded)
			row := entity.AuditEvent{Action: "limits.default.reset", ResourceType: "user", ResourceID: "usr_example", DetailsJSON: &raw}
			if auditRecord(row).Changes != nil {
				t.Fatal("malformed reset facts were exposed")
			}
		})
	}
	for _, identity := range []struct{ kind, id string }{{"team_member", "usr_example"}, {"project", "prj_example"}, {"user", "USR_EXAMPLE"}, {"team", "usr_example"}} {
		row := entity.AuditEvent{Action: "limits.default.reset", ResourceType: identity.kind, ResourceID: identity.id, DetailsJSON: &base}
		if auditRecord(row).Changes != nil {
			t.Fatalf("unsupported default-reset target exposed: %s/%s", identity.kind, identity.id)
		}
	}
}
